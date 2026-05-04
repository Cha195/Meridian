package cache

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	stateNormal  int32 = 0
	stateWarming int32 = 1
)

// MigrationStatus is a snapshot of the migration's current state.
type MigrationStatus struct {
	ActiveName     string
	WarmingName    string
	ActiveHitRate  float64
	WarmingHitRate float64
	WarmingReqs    int64
	Threshold      float64
	IsWarming      bool
}

// MigratingCache wraps two CachePolicy implementations and manages a live
// blue-green migration from one to the other.
//
// Every Get reads from active (serving real traffic) and shadow-reads from
// warming (building up hit-rate data). Every Set writes to both. When
// warming's hit rate reaches threshold × active's hit rate and at least
// minWarmupReqs have been processed, the pointers swap atomically and the
// old cache is destroyed asynchronously.
type MigratingCache struct {
	active  CachePolicy
	warming CachePolicy

	state atomic.Int32

	warmingHits atomic.Int64
	warmingReqs atomic.Int64
	activeHits  atomic.Int64
	activeReqs  atomic.Int64

	threshold     float64
	minWarmupReqs int64

	mu     sync.RWMutex
	onSwap func(old, new string)
}

type MigratingCacheOption func(*MigratingCache)

func WithThreshold(t float64) MigratingCacheOption {
	return func(mc *MigratingCache) { mc.threshold = t }
}

func WithMinWarmupReqs(n int64) MigratingCacheOption {
	return func(mc *MigratingCache) { mc.minWarmupReqs = n }
}

func WithOnSwap(fn func(old, new string)) MigratingCacheOption {
	return func(mc *MigratingCache) { mc.onSwap = fn }
}

func NewMigratingCache(active CachePolicy, opts ...MigratingCacheOption) *MigratingCache {
	mc := &MigratingCache{
		active:        active,
		threshold:     0.90,
		minWarmupReqs: 1000,
	}
	for _, opt := range opts {
		opt(mc)
	}
	return mc
}

func (mc *MigratingCache) Get(key string) (*CacheEntry, CacheStatus) {
	mc.mu.RLock()
	active := mc.active
	warming := mc.warming
	mc.mu.RUnlock()

	entry, status := active.Get(key)
	mc.activeReqs.Add(1)
	if status == CacheHIT || status == CacheSTALE {
		mc.activeHits.Add(1)
	}

	if warming != nil {
		_, wStatus := warming.Get(key)
		mc.warmingReqs.Add(1)
		if wStatus == CacheHIT || wStatus == CacheSTALE {
			mc.warmingHits.Add(1)
		}
		mc.maybeSwap()
	}

	return entry, status
}

func (mc *MigratingCache) Set(key string, entry *CacheEntry, ttl time.Duration) {
	mc.mu.RLock()
	active := mc.active
	warming := mc.warming
	mc.mu.RUnlock()

	active.Set(key, entry, ttl)
	if warming != nil {
		warming.Set(key, entry, ttl)
	}
}

func (mc *MigratingCache) Delete(key string) {
	mc.mu.RLock()
	active := mc.active
	warming := mc.warming
	mc.mu.RUnlock()

	active.Delete(key)
	if warming != nil {
		warming.Delete(key)
	}
}

func (mc *MigratingCache) Purge(pattern string) int {
	mc.mu.RLock()
	active := mc.active
	warming := mc.warming
	mc.mu.RUnlock()

	n := active.Purge(pattern)
	if warming != nil {
		warming.Purge(pattern)
	}
	return n
}

func (mc *MigratingCache) Size() int64 {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.active.Size()
}

func (mc *MigratingCache) Len() int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.active.Len()
}

func (mc *MigratingCache) Stats() CacheStats {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.active.Stats()
}

func (mc *MigratingCache) Name() string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.active.Name()
}

func (mc *MigratingCache) Destroy() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.active.Destroy()
	if mc.warming != nil {
		mc.warming.Destroy()
		mc.warming = nil
	}
}

// StartMigration installs newPolicy as the warming cache and begins
// tracking hit rates. Any in-progress migration is aborted first.
func (mc *MigratingCache) StartMigration(newPolicy CachePolicy) {
	mc.mu.Lock()
	old := mc.warming
	mc.warming = newPolicy
	mc.warmingHits.Store(0)
	mc.warmingReqs.Store(0)
	mc.activeHits.Store(0)
	mc.activeReqs.Store(0)
	mc.state.Store(stateWarming)
	mc.mu.Unlock()

	if old != nil {
		go old.Destroy()
	}
}

// AbortMigration cancels an in-progress migration and destroys the warming cache.
func (mc *MigratingCache) AbortMigration() {
	mc.mu.Lock()
	old := mc.warming
	mc.warming = nil
	mc.state.Store(stateNormal)
	mc.warmingHits.Store(0)
	mc.warmingReqs.Store(0)
	mc.activeHits.Store(0)
	mc.activeReqs.Store(0)
	mc.mu.Unlock()

	if old != nil {
		go old.Destroy()
	}
}

// MigrationStatus returns a snapshot of the migration's current state.
func (mc *MigratingCache) MigrationStatus() MigrationStatus {
	mc.mu.RLock()
	activeName := mc.active.Name()
	warmingName := ""
	if mc.warming != nil {
		warmingName = mc.warming.Name()
	}
	mc.mu.RUnlock()

	aReqs := mc.activeReqs.Load()
	aHits := mc.activeHits.Load()
	wReqs := mc.warmingReqs.Load()
	wHits := mc.warmingHits.Load()

	var activeRate, warmingRate float64
	if aReqs > 0 {
		activeRate = float64(aHits) / float64(aReqs)
	}
	if wReqs > 0 {
		warmingRate = float64(wHits) / float64(wReqs)
	}

	return MigrationStatus{
		ActiveName:     activeName,
		WarmingName:    warmingName,
		ActiveHitRate:  activeRate,
		WarmingHitRate: warmingRate,
		WarmingReqs:    wReqs,
		Threshold:      mc.threshold,
		IsWarming:      mc.state.Load() == stateWarming,
	}
}

// maybeSwap checks whether the warming cache has reached the hit-rate
// threshold and, if so, atomically promotes it to active.
func (mc *MigratingCache) maybeSwap() {
	if mc.warmingReqs.Load() < mc.minWarmupReqs {
		return
	}

	aReqs := mc.activeReqs.Load()
	aHits := mc.activeHits.Load()
	wReqs := mc.warmingReqs.Load()
	wHits := mc.warmingHits.Load()

	var activeRate, warmingRate float64
	if aReqs > 0 {
		activeRate = float64(aHits) / float64(aReqs)
	}
	if wReqs > 0 {
		warmingRate = float64(wHits) / float64(wReqs)
	}

	if warmingRate < activeRate*mc.threshold {
		return
	}

	mc.mu.Lock()
	// Double-check: another goroutine may have already swapped or aborted.
	if mc.warming == nil || mc.state.Load() != stateWarming {
		mc.mu.Unlock()
		return
	}

	old := mc.active
	oldName := old.Name()
	newName := mc.warming.Name()
	mc.active = mc.warming
	mc.warming = nil
	mc.state.Store(stateNormal)
	mc.activeHits.Store(0)
	mc.activeReqs.Store(0)
	mc.warmingHits.Store(0)
	mc.warmingReqs.Store(0)
	mc.mu.Unlock()

	if mc.onSwap != nil {
		mc.onSwap(oldName, newName)
	}
	go old.Destroy()
}
