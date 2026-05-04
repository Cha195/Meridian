package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestMigrationBasicSwap verifies that the warming cache is promoted to active
// once it accumulates enough requests and meets the hit-rate threshold.
func TestMigrationBasicSwap(t *testing.T) {
	active := NewSieveCache(10 * 1024 * 1024)
	warming := NewLRUCache(10 * 1024 * 1024)

	var swappedOld, swappedNew string
	mc := NewMigratingCache(active,
		WithThreshold(0.8),
		WithMinWarmupReqs(20),
		WithOnSwap(func(old, new string) {
			swappedOld = old
			swappedNew = new
		}),
	)
	defer mc.Destroy()

	mc.StartMigration(warming)

	// Set then Get — both caches receive every write and every read.
	// After minWarmupReqs=20 gets, both hit rates are equal → swap fires.
	for i := 0; i < 40; i++ {
		key := fmt.Sprintf("key%d", i%20)
		mc.Set(key, makeEntry(key, []byte("v")), time.Hour)
		mc.Get(key)
	}

	if mc.Name() != "lru" {
		t.Fatalf("expected active=lru after swap, got %q (swapped: %q→%q)", mc.Name(), swappedOld, swappedNew)
	}
	if swappedOld != "sieve" || swappedNew != "lru" {
		t.Fatalf("onSwap received wrong names: old=%q new=%q", swappedOld, swappedNew)
	}
}

// TestMigrationThresholdNotMet verifies that a warming cache with a much lower
// hit rate than the active cache is never promoted.
func TestMigrationThresholdNotMet(t *testing.T) {
	// Pre-populate active directly so it is warm before migration starts.
	active := NewLRUCache(10 * 1024 * 1024)
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key%d", i)
		active.Set(key, makeEntry(key, []byte("hello")), time.Hour)
	}

	// warming stays empty throughout the test — it will miss every Get.
	warming := NewLRUCache(10 * 1024 * 1024)

	mc := NewMigratingCache(active,
		WithThreshold(0.9),
		WithMinWarmupReqs(10),
	)
	defer mc.Destroy()

	mc.StartMigration(warming)

	// Only Gets (no Sets) — active hits on every key, warming misses on every key.
	for i := 0; i < 50; i++ {
		mc.Get(fmt.Sprintf("key%d", i%20))
	}

	st := mc.MigrationStatus()
	if !st.IsWarming {
		t.Fatal("expected still in warming state (threshold should not be met)")
	}
	if st.WarmingHitRate >= st.ActiveHitRate*mc.threshold {
		t.Fatalf("warming hit rate %.3f should be below threshold (%.3f × %.3f = %.3f)",
			st.WarmingHitRate, st.ActiveHitRate, mc.threshold, st.ActiveHitRate*mc.threshold)
	}
}

// TestMigrationAbort verifies that aborting a migration leaves active unchanged
// and clears the warming pointer.
func TestMigrationAbort(t *testing.T) {
	active := NewSieveCache(10 * 1024 * 1024)
	mc := NewMigratingCache(active)
	defer mc.Destroy()

	mc.StartMigration(NewLRUCache(10 * 1024 * 1024))
	if !mc.MigrationStatus().IsWarming {
		t.Fatal("expected warming state after StartMigration")
	}

	mc.AbortMigration()

	st := mc.MigrationStatus()
	if st.IsWarming {
		t.Fatal("expected normal state after AbortMigration")
	}
	if st.WarmingName != "" {
		t.Fatalf("expected empty warming name after abort, got %q", st.WarmingName)
	}
	if mc.Name() != "sieve" {
		t.Fatalf("active should still be sieve after abort, got %s", mc.Name())
	}
}

// TestMigrationNoDroppedRequests runs concurrent Get/Set against a live
// migration and verifies no panics or data races occur.
// Run with: go test -race ./internal/cache/...
func TestMigrationNoDroppedRequests(t *testing.T) {
	active := NewLRUCache(10 * 1024 * 1024)
	mc := NewMigratingCache(active,
		WithThreshold(0.8),
		WithMinWarmupReqs(50),
	)
	defer mc.Destroy()

	// Seed active before migration starts.
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key%d", i)
		mc.Set(key, makeEntry(key, []byte("v")), time.Hour)
	}

	mc.StartMigration(NewLRUCache(10 * 1024 * 1024))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				key := fmt.Sprintf("key%d", (id+j)%20)
				mc.Set(key, makeEntry(key, []byte("v")), time.Hour)
				entry, status := mc.Get(key)
				if status == CacheHIT && entry == nil {
					t.Errorf("HIT returned nil entry for key %s", key)
				}
			}
		}(i)
	}
	wg.Wait()
}

// TestMigrationStatusReporting verifies that MigrationStatus returns accurate
// field values while a migration is in progress.
func TestMigrationStatusReporting(t *testing.T) {
	active := NewSieveCache(10 * 1024 * 1024)
	warming := NewLRUCache(10 * 1024 * 1024)

	// minWarmupReqs large enough that no swap fires during this test.
	mc := NewMigratingCache(active,
		WithThreshold(0.9),
		WithMinWarmupReqs(10000),
	)
	defer mc.Destroy()

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		mc.Set(key, makeEntry(key, []byte("v")), time.Hour)
	}

	mc.StartMigration(warming)

	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key%d", i%10)
		mc.Set(key, makeEntry(key, []byte("v")), time.Hour)
		mc.Get(key)
	}

	st := mc.MigrationStatus()
	if st.ActiveName != "sieve" {
		t.Fatalf("expected active=sieve, got %q", st.ActiveName)
	}
	if st.WarmingName != "lru" {
		t.Fatalf("expected warming=lru, got %q", st.WarmingName)
	}
	if !st.IsWarming {
		t.Fatal("expected IsWarming=true")
	}
	if st.WarmingReqs == 0 {
		t.Fatal("expected warmingReqs > 0")
	}
	if st.Threshold != 0.9 {
		t.Fatalf("expected threshold=0.9, got %f", st.Threshold)
	}
}

// TestMigrationWritesToBoth verifies that a Set during migration lands in
// both the active and warming caches.
func TestMigrationWritesToBoth(t *testing.T) {
	active := NewSieveCache(10 * 1024 * 1024)
	warming := NewLRUCache(10 * 1024 * 1024)

	mc := NewMigratingCache(active, WithMinWarmupReqs(10000))
	defer mc.Destroy()

	mc.StartMigration(warming)

	mc.Set("k1", makeEntry("k1", []byte("hello")), time.Hour)

	if _, s := active.Get("k1"); s != CacheHIT {
		t.Fatalf("expected HIT in active cache, got %v", s)
	}
	if _, s := warming.Get("k1"); s != CacheHIT {
		t.Fatalf("expected HIT in warming cache, got %v", s)
	}
}
