package cache

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMigrationBasicSwap verifies that the warming cache is promoted to active
// once it accumulates enough requests and meets the hit-rate threshold.
func TestMigrationBasicSwap(t *testing.T) {
	active := NewSieveCache(10 * 1024 * 1024)
	warming := NewLRUCache(10 * 1024 * 1024)

	var swappedOld, swappedNew, swappedOutcome string
	mc := NewMigratingCache(active,
		WithThreshold(0.8),
		WithMinWarmupReqs(20),
		WithOnSwap(func(old, new, outcome string) {
			swappedOld = old
			swappedNew = new
			swappedOutcome = outcome
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
		t.Fatalf("expected active=lru after swap, got %q (swapped: %q→%q outcome=%q)",
			mc.Name(), swappedOld, swappedNew, swappedOutcome)
	}
	if swappedOld != "sieve" || swappedNew != "lru" {
		t.Fatalf("onSwap received wrong names: old=%q new=%q", swappedOld, swappedNew)
	}
	if swappedOutcome != "swapped" {
		t.Fatalf("expected outcome %q, got %q", "swapped", swappedOutcome)
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

// ---------------------------------------------------------------------------
// Safeguard tests
// ---------------------------------------------------------------------------

// TestMigrationTimeoutAbort verifies that a migration is automatically
// aborted when maxWarmupDuration elapses without the threshold being met.
func TestMigrationTimeoutAbort(t *testing.T) {
	active := NewLRUCache(10 * 1024 * 1024)
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key%d", i)
		active.Set(key, makeEntry(key, []byte("v")), time.Hour)
	}

	var outcome string
	mc := NewMigratingCache(active,
		WithThreshold(1.1),                      // impossible — prevents auto-swap
		WithMinWarmupReqs(10),
		WithMaxWarmupDuration(50*time.Millisecond),
		WithEvalInterval(10*time.Second),         // large — no stagnation during test
		WithMaxStagnantEvals(100),
		WithOnSwap(func(_, _, o string) { outcome = o }),
	)
	defer mc.Destroy()

	mc.StartMigration(NewLRUCache(10 * 1024 * 1024))

	// Feed enough Gets to pass minWarmupReqs (warming misses all — never populated).
	for i := 0; i < 15; i++ {
		mc.Get(fmt.Sprintf("key%d", i%20))
	}

	// Wait past maxWarmupDuration.
	time.Sleep(100 * time.Millisecond)

	// One more Get to trigger maybeSwap → timeout fires.
	mc.Get("key0")

	if mc.MigrationStatus().IsWarming {
		t.Fatal("expected migration to be aborted after timeout")
	}
	if outcome != "aborted:timeout" {
		t.Fatalf("expected outcome %q, got %q", "aborted:timeout", outcome)
	}
	if mc.Name() != "lru" {
		t.Fatalf("active should be unchanged after abort, got %s", mc.Name())
	}
}

// TestMigrationStagnationAbort verifies that a migration is aborted when the
// warming hit rate stops improving across maxStagnantEvals eval windows.
func TestMigrationStagnationAbort(t *testing.T) {
	active := NewLRUCache(10 * 1024 * 1024)
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key%d", i)
		active.Set(key, makeEntry(key, []byte("v")), time.Hour)
	}

	var outcome string
	mc := NewMigratingCache(active,
		WithThreshold(1.1),                       // impossible — prevents auto-swap
		WithMinWarmupReqs(10),
		WithMaxWarmupDuration(10*time.Minute),     // large — no timeout during test
		WithEvalInterval(10*time.Millisecond),
		WithMaxStagnantEvals(3),
		WithOnSwap(func(_, _, o string) { outcome = o }),
	)
	defer mc.Destroy()

	// warming starts empty and never receives Sets — it misses every Get.
	mc.StartMigration(NewLRUCache(10 * 1024 * 1024))

	// Feed enough Gets to pass minWarmupReqs.
	for i := 0; i < 15; i++ {
		mc.Get(fmt.Sprintf("key%d", i%20))
	}

	// Trigger maxStagnantEvals+1 evaluation windows, sleeping between each.
	for i := 0; i < 5; i++ {
		time.Sleep(15 * time.Millisecond) // > evalInterval=10ms
		mc.Get(fmt.Sprintf("key%d", i%20))
		if !mc.MigrationStatus().IsWarming {
			break
		}
	}

	if mc.MigrationStatus().IsWarming {
		t.Fatal("expected migration to be aborted after stagnation")
	}
	if outcome != "aborted:stagnation" {
		t.Fatalf("expected outcome %q, got %q", "aborted:stagnation", outcome)
	}
}

// TestMigrationStagnationIsTimeGated feeds 1000 requests in a tight loop and
// verifies that stagnation is not counted once-per-request.
func TestMigrationStagnationIsTimeGated(t *testing.T) {
	active := NewLRUCache(10 * 1024 * 1024)
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key%d", i)
		active.Set(key, makeEntry(key, []byte("v")), time.Hour)
	}

	mc := NewMigratingCache(active,
		WithThreshold(1.1),
		WithMinWarmupReqs(100),
		WithMaxWarmupDuration(10*time.Minute),
		WithEvalInterval(200*time.Millisecond), // much longer than the loop
		WithMaxStagnantEvals(3),
	)
	defer mc.Destroy()

	// warming starts empty — every Get misses.
	mc.StartMigration(NewLRUCache(10 * 1024 * 1024))

	// 1000 Gets in a tight loop; should complete in <<200ms.
	for i := 0; i < 1000; i++ {
		mc.Get(fmt.Sprintf("key%d", i%20))
	}

	st := mc.MigrationStatus()
	if !st.IsWarming {
		t.Fatal("migration should still be active (no abort should have fired)")
	}
	// At most one stagnation eval can fire if the loop crosses an interval boundary;
	// per-request counting would have yielded stagnantEvals >> maxStagnantEvals.
	if st.StagnantEvals >= mc.maxStagnantEvals {
		t.Fatalf("stagnantEvals=%d should be < maxStagnantEvals=%d (time-gating failed)",
			st.StagnantEvals, mc.maxStagnantEvals)
	}
}

// TestForceSwapSucceeds verifies that ForceSwap promotes the warming cache
// when minWarmupReqs and the fill-ratio floor are satisfied.
func TestForceSwapSucceeds(t *testing.T) {
	active := NewSieveCache(10 * 1024 * 1024)
	warming := NewLRUCache(10 * 1024 * 1024)

	var outcome string
	mc := NewMigratingCache(active,
		WithThreshold(1.1),       // impossible — prevents auto-swap
		WithMinWarmupReqs(20),
		WithMaxWarmupDuration(10*time.Minute),
		WithEvalInterval(10*time.Second),
		WithOnSwap(func(_, _, o string) { outcome = o }),
	)
	defer mc.Destroy()

	mc.StartMigration(warming)

	// 20 Set+Get pairs across 10 distinct keys.
	// Both caches receive every Set, so warming.Len()==active.Len()==10.
	// warmingReqs reaches minWarmupReqs=20.
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key%d", i%10)
		mc.Set(key, makeEntry(key, []byte("v")), time.Hour)
		mc.Get(key)
	}

	// fillRatio = warming.Len() / (active.Len() + warming.Len()) = 10/20 = 0.5
	if err := mc.ForceSwap(0.1); err != nil {
		t.Fatalf("ForceSwap(0.1) should succeed, got: %v", err)
	}
	if mc.Name() != "lru" {
		t.Fatalf("expected active=lru after force swap, got %s", mc.Name())
	}
	if outcome != "forced" {
		t.Fatalf("expected outcome %q, got %q", "forced", outcome)
	}
}

// TestForceSwapRejectsUnderfilled verifies that ForceSwap refuses when the
// warming cache has not accumulated enough entries relative to active.
func TestForceSwapRejectsUnderfilled(t *testing.T) {
	active := NewSieveCache(10 * 1024 * 1024)
	warming := NewLRUCache(10 * 1024 * 1024)

	mc := NewMigratingCache(active,
		WithThreshold(1.1),
		WithMinWarmupReqs(20),
		WithMaxWarmupDuration(10*time.Minute),
		WithEvalInterval(10*time.Second),
	)
	defer mc.Destroy()

	mc.StartMigration(warming)

	// Same Set+Get loop — warmingReqs=20, fillRatio=0.5.
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key%d", i%10)
		mc.Set(key, makeEntry(key, []byte("v")), time.Hour)
		mc.Get(key)
	}

	// fillRatio=0.5 < 0.99 → must be rejected.
	err := mc.ForceSwap(0.99)
	if err == nil {
		t.Fatal("ForceSwap(0.99) should fail when fillRatio=0.5")
	}
	if !strings.Contains(err.Error(), "filled") {
		t.Fatalf("unexpected error message: %v", err)
	}
	// Active should be unchanged.
	if mc.Name() != "sieve" {
		t.Fatalf("active should still be sieve, got %s", mc.Name())
	}
}

// TestForceSwapRejectsNoMigration verifies that ForceSwap returns an error
// when no migration is in progress.
func TestForceSwapRejectsNoMigration(t *testing.T) {
	mc := NewMigratingCache(NewSieveCache(10 * 1024 * 1024))
	defer mc.Destroy()

	err := mc.ForceSwap(0.3)
	if err == nil {
		t.Fatal("ForceSwap should fail when no migration is in progress")
	}
	if !strings.Contains(err.Error(), "no migration") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestMigrationStagnationResetsOnImprovement verifies that stagnantEvals resets
// to 0 when the warming hit rate genuinely improves, and resumes counting from
// 0 (not from the prior count) on the next flat window.
func TestMigrationStagnationResetsOnImprovement(t *testing.T) {
	// Pre-populate active directly so it is warm before migration starts.
	active := NewLRUCache(10 * 1024 * 1024)
	for i := 0; i < 20; i++ {
		active.Set(fmt.Sprintf("key%d", i), makeEntry(fmt.Sprintf("key%d", i), []byte("v")), time.Hour)
	}

	const evalMs = 20 * time.Millisecond

	mc := NewMigratingCache(active,
		WithThreshold(1.1),            // impossible — prevents auto-swap
		WithMinWarmupReqs(10),
		WithMaxWarmupDuration(10*time.Minute),
		WithEvalInterval(evalMs),
		WithMaxStagnantEvals(10),      // large — no abort during this test
	)
	defer mc.Destroy()

	mc.StartMigration(NewLRUCache(10 * 1024 * 1024))

	// Phase 1: warming is empty — all Gets miss.
	for i := 0; i < 15; i++ {
		mc.Get(fmt.Sprintf("key%d", i%20))
	}
	time.Sleep(evalMs + 5*time.Millisecond)
	mc.Get("key0") // trigger eval → improvement = 0−0 = 0 → stagnantEvals++

	st := mc.MigrationStatus()
	if st.StagnantEvals < 1 {
		t.Fatalf("phase 1: expected stagnantEvals >= 1, got %d", st.StagnantEvals)
	}

	// Phase 2: populate warming via mc.Set, then Get — hit rate improves.
	for i := 0; i < 20; i++ {
		mc.Set(fmt.Sprintf("key%d", i), makeEntry(fmt.Sprintf("key%d", i), []byte("v")), time.Hour)
	}
	for i := 0; i < 20; i++ {
		mc.Get(fmt.Sprintf("key%d", i)) // warming now hits
	}
	time.Sleep(evalMs + 5*time.Millisecond)
	mc.Get("key0") // trigger eval → improvement > 0 → stagnantEvals resets to 0

	if mc.MigrationStatus().StagnantEvals != 0 {
		t.Fatalf("phase 2: expected stagnantEvals=0 after improvement, got %d",
			mc.MigrationStatus().StagnantEvals)
	}

	// Phase 3: feed misses only — hit rate stops improving.
	for i := 0; i < 20; i++ {
		mc.Get(fmt.Sprintf("newkey%d", i)) // not in warming
	}
	time.Sleep(evalMs + 5*time.Millisecond)
	mc.Get("newkey99") // trigger eval → improvement < 0 → stagnantEvals++ from 0

	if mc.MigrationStatus().StagnantEvals != 1 {
		t.Fatalf("phase 3: expected stagnantEvals=1 (resumed from 0), got %d",
			mc.MigrationStatus().StagnantEvals)
	}
}

// TestMigrationManualAbortOutcome verifies that calling AbortMigration fires
// the callback with outcome "aborted:manual" and the correct policy names.
func TestMigrationManualAbortOutcome(t *testing.T) {
	active := NewSieveCache(10 * 1024 * 1024)

	var cbOld, cbNew, cbOutcome string
	mc := NewMigratingCache(active,
		WithMinWarmupReqs(10000),
		WithOnSwap(func(old, new, outcome string) {
			cbOld = old
			cbNew = new
			cbOutcome = outcome
		}),
	)
	defer mc.Destroy()

	mc.StartMigration(NewLRUCache(10 * 1024 * 1024))
	mc.AbortMigration()

	if cbOutcome != "aborted:manual" {
		t.Fatalf("expected outcome %q, got %q", "aborted:manual", cbOutcome)
	}
	if cbOld != "sieve" {
		t.Fatalf("expected old policy %q, got %q", "sieve", cbOld)
	}
	if cbNew != "lru" {
		t.Fatalf("expected new policy %q, got %q", "lru", cbNew)
	}
}

// TestForceSwapRejectsInsufficientRequests verifies that ForceSwap refuses
// when the warming cache has not yet processed minWarmupReqs requests.
func TestForceSwapRejectsInsufficientRequests(t *testing.T) {
	mc := NewMigratingCache(NewSieveCache(10 * 1024 * 1024),
		WithThreshold(1.1),
		WithMinWarmupReqs(100),
	)
	defer mc.Destroy()

	mc.StartMigration(NewLRUCache(10 * 1024 * 1024))
	// No requests fed — warmingReqs = 0.

	err := mc.ForceSwap(0.3)
	if err == nil {
		t.Fatal("ForceSwap should fail when warmingReqs < minWarmupReqs")
	}
	if !strings.Contains(err.Error(), "enough requests") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
