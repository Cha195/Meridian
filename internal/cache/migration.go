package cache

import (
	"fmt"
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
	ElapsedSeconds float64
	StagnantEvals  int
}

// MigratingCache wraps two CachePolicy implementations and manages a live
// blue-green migration from one to the other.
//
// Every Get reads from active (serving real traffic) and shadow-reads from
// warming (building up hit-rate data). Every Set writes to both. When
// warming's hit rate reaches threshold × active's hit rate and at least
// minWarmupReqs have been processed, the pointers swap atomically and the
// old cache is destroyed asynchronously.
//
// Three automatic safeguards prevent the migration from running forever:
//   - Timeout: abort if maxWarmupDuration elapses without a swap.
//   - Stagnation: abort if the warming hit rate stops improving across
//     maxStagnantEvals consecutive evalInterval windows.
//   - ForceSwap: operator-triggered immediate swap once the warming cache
//     is sufficiently filled.
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

	// safeguard parameters (immutable after construction)
	maxWarmupDuration time.Duration
	evalInterval      time.Duration
	maxStagnantEvals  int

	// migration state — all fields below are protected by mu
	migrationStarted time.Time
	lastEvalTime     time.Time
	lastEvalRate     float64
	stagnantEvals    int

	mu     sync.RWMutex
	onSwap func(old, new, outcome string)
}

type MigratingCacheOption func(*MigratingCache)

func WithThreshold(t float64) MigratingCacheOption {
	return func(mc *MigratingCache) { mc.threshold = t }
}

func WithMinWarmupReqs(n int64) MigratingCacheOption {
	return func(mc *MigratingCache) { mc.minWarmupReqs = n }
}

// WithOnSwap registers a callback that fires on every migration outcome.
// outcome is one of: "swapped", "forced", "aborted:timeout",
// "aborted:stagnation", "aborted:manual".
func WithOnSwap(fn func(old, new, outcome string)) MigratingCacheOption {
	return func(mc *MigratingCache) { mc.onSwap = fn }
}

func WithMaxWarmupDuration(d time.Duration) MigratingCacheOption {
	return func(mc *MigratingCache) { mc.maxWarmupDuration = d }
}

func WithEvalInterval(d time.Duration) MigratingCacheOption {
	return func(mc *MigratingCache) { mc.evalInterval = d }
}

func WithMaxStagnantEvals(n int) MigratingCacheOption {
	return func(mc *MigratingCache) { mc.maxStagnantEvals = n }
}

func NewMigratingCache(active CachePolicy, opts ...MigratingCacheOption) *MigratingCache {
	mc := &MigratingCache{
		active:            active,
		threshold:         0.90,
		minWarmupReqs:     1000,
		maxWarmupDuration: 10 * time.Minute,
		evalInterval:      10 * time.Second,
		maxStagnantEvals:  5,
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

	if warming != nil {
		// Clone before either cache stores the pointer. If we cloned after
		// active.Set, a concurrent active.Get could write AccessCount while
		// we're still reading the struct — two caches sharing one pointer
		// means two independent mutexes protecting the same memory.
		clone := *entry
		active.Set(key, entry, ttl)
		warming.Set(key, &clone, ttl)
	} else {
		active.Set(key, entry, ttl)
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
	now := time.Now()
	mc.warming = newPolicy
	mc.migrationStarted = now
	mc.lastEvalTime = now
	mc.lastEvalRate = 0
	mc.stagnantEvals = 0
	mc.mu.Unlock()

	mc.warmingHits.Store(0)
	mc.warmingReqs.Store(0)
	mc.activeHits.Store(0)
	mc.activeReqs.Store(0)
	mc.state.Store(stateWarming)

	if old != nil {
		go old.Destroy()
	}
}

// AbortMigration cancels an in-progress migration.
func (mc *MigratingCache) AbortMigration() {
	mc.doAbort("manual")
}

// ForceSwap immediately promotes the warming cache to active, bypassing the
// hit-rate threshold. minFillPercent (0.0–1.0) is a safety floor: warming.Len
// must be at least that fraction of (active.Len + warming.Len) before the
// swap is allowed.
func (mc *MigratingCache) ForceSwap(minFillPercent float64) error {
	mc.mu.Lock()
	if mc.warming == nil {
		mc.mu.Unlock()
		return fmt.Errorf("no migration in progress")
	}
	wReqs := mc.warmingReqs.Load()
	if wReqs < mc.minWarmupReqs {
		mc.mu.Unlock()
		return fmt.Errorf("warming cache has not processed enough requests yet (need %d, have %d)",
			mc.minWarmupReqs, wReqs)
	}
	aLen := mc.active.Len()
	wLen := mc.warming.Len()
	mc.mu.Unlock()

	total := aLen + wLen
	var fillRatio float64
	if total > 0 {
		fillRatio = float64(wLen) / float64(total)
	}
	if fillRatio < minFillPercent {
		return fmt.Errorf("warming cache is only %.0f%% filled, need at least %.0f%%",
			fillRatio*100, minFillPercent*100)
	}
	mc.doSwap("forced")
	return nil
}

// MigrationStatus returns a snapshot of the migration's current state.
func (mc *MigratingCache) MigrationStatus() MigrationStatus {
	mc.mu.RLock()
	activeName := mc.active.Name()
	warmingName := ""
	if mc.warming != nil {
		warmingName = mc.warming.Name()
	}
	started := mc.migrationStarted
	stagnantEvals := mc.stagnantEvals
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

	var elapsed float64
	if !started.IsZero() {
		elapsed = time.Since(started).Seconds()
	}

	return MigrationStatus{
		ActiveName:     activeName,
		WarmingName:    warmingName,
		ActiveHitRate:  activeRate,
		WarmingHitRate: warmingRate,
		WarmingReqs:    wReqs,
		Threshold:      mc.threshold,
		IsWarming:      mc.state.Load() == stateWarming,
		ElapsedSeconds: elapsed,
		StagnantEvals:  stagnantEvals,
	}
}

// maybeSwap runs the three safety checks on every Get while a migration is active.
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

	// CHECK 1: threshold met → swap immediately (doSwap double-checks under lock).
	if warmingRate >= activeRate*mc.threshold {
		mc.doSwap("swapped")
		return
	}

	// Checks 2 and 3 read/write time-based state fields protected by mu.
	mc.mu.Lock()
	if mc.warming == nil {
		// Concurrently aborted or swapped.
		mc.mu.Unlock()
		return
	}

	// CHECK 2: timeout → abort if the migration has been running too long.
	if time.Since(mc.migrationStarted) > mc.maxWarmupDuration {
		mc.mu.Unlock()
		mc.doAbort("timeout")
		return
	}

	// CHECK 3: stagnation — time-gated to at most once per evalInterval.
	// Without this gate, 5 stagnant "checks" would fire in microseconds at
	// high RPS; the gate ensures each check represents a real time window.
	if time.Since(mc.lastEvalTime) >= mc.evalInterval {
		mc.lastEvalTime = time.Now()
		improvement := warmingRate - mc.lastEvalRate
		mc.lastEvalRate = warmingRate
		if improvement < 0.001 {
			mc.stagnantEvals++
			if mc.stagnantEvals >= mc.maxStagnantEvals {
				mc.mu.Unlock()
				mc.doAbort("stagnation")
				return
			}
		} else {
			mc.stagnantEvals = 0
		}
	}
	mc.mu.Unlock()
}

// doSwap atomically promotes warming to active. outcome is "swapped" or "forced".
func (mc *MigratingCache) doSwap(outcome string) {
	mc.mu.Lock()
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
	mc.stagnantEvals = 0
	mc.lastEvalRate = 0
	mc.migrationStarted = time.Time{}
	mc.mu.Unlock()

	mc.activeHits.Store(0)
	mc.activeReqs.Store(0)
	mc.warmingHits.Store(0)
	mc.warmingReqs.Store(0)

	if mc.onSwap != nil {
		mc.onSwap(oldName, newName, outcome)
	}
	go old.Destroy()
}

// doAbort destroys the warming cache and cancels the migration.
func (mc *MigratingCache) doAbort(reason string) {
	mc.mu.Lock()
	if mc.warming == nil {
		mc.mu.Unlock()
		return
	}
	activeName := mc.active.Name()
	warmingName := mc.warming.Name()
	old := mc.warming
	mc.warming = nil
	mc.state.Store(stateNormal)
	mc.stagnantEvals = 0
	mc.lastEvalRate = 0
	mc.migrationStarted = time.Time{}
	mc.mu.Unlock()

	mc.activeHits.Store(0)
	mc.activeReqs.Store(0)
	mc.warmingHits.Store(0)
	mc.warmingReqs.Store(0)

	go old.Destroy()
	if mc.onSwap != nil {
		mc.onSwap(activeName, warmingName, "aborted:"+reason)
	}
}
