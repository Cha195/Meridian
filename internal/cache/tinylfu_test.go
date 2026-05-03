package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// body returns a CacheEntry whose body is n bytes (used for size-based tests).
func body(n int) *CacheEntry {
	return &CacheEntry{
		Response:  CachedResponse{Body: make([]byte, n)},
		SizeBytes: int64(n),
	}
}

func TestWTinyLFUBasicHitMiss(t *testing.T) {
	c := NewWTinyLFUCache(10 * 1024 * 1024)
	defer c.Destroy()

	e := makeEntry("k1", []byte("hello"))
	c.Set("k1", e, time.Hour)

	got, status := c.Get("k1")
	if status != CacheHIT {
		t.Fatalf("expected HIT, got %v", status)
	}
	if string(got.Response.Body) != "hello" {
		t.Fatalf("unexpected body: %s", got.Response.Body)
	}

	_, status = c.Get("missing")
	if status != CacheMISS {
		t.Fatalf("expected MISS, got %v", status)
	}
}

// TestWTinyLFUFrequencyAdmission verifies that the TinyLFU gate keeps a
// high-frequency item (A) over a low-frequency item (B) when a new item (C)
// is admitted and the cache is full.
//
// Layout: maxSize=300, each item=100b → window=3b, main=297b (fits 2 items).
//
//  Step 1: Set A, Get A ×9  → A in protected, freq=10
//  Step 2: Set B             → B in probation, freq=1
//  Step 3: Set C             → main full (200+100>297); gate compares C(1) vs B(1):
//                              C wins (>=), B evicted, C admitted to probation.
func TestWTinyLFUFrequencyAdmission(t *testing.T) {
	c := NewWTinyLFUCache(300)
	defer c.Destroy()

	c.Set("A", body(100), time.Hour)
	for i := 0; i < 9; i++ {
		c.Get("A")
	}
	c.Set("B", body(100), time.Hour)
	c.Set("C", body(100), time.Hour) // triggers eviction

	_, aStatus := c.Get("A")
	if aStatus == CacheMISS {
		t.Fatal("A (high frequency, in protected) should not have been evicted")
	}
	_, bStatus := c.Get("B")
	if bStatus != CacheMISS {
		t.Fatalf("B (low frequency, probation tail) should have been evicted by C, got %v", bStatus)
	}
	_, cStatus := c.Get("C")
	if cStatus == CacheMISS {
		t.Fatal("C should have been admitted")
	}
}

// TestWTinyLFUSegmentPromotion verifies that accessing an item in probation
// promotes it to protected, shielding it from eviction while probation items
// are still vulnerable.
func TestWTinyLFUSegmentPromotion(t *testing.T) {
	// maxSize=300, items=100b: window=3b, main=297b (fits 2 items).
	c := NewWTinyLFUCache(300)
	defer c.Destroy()

	// item1 set then accessed → promoted to protected.
	c.Set("item1", body(100), time.Hour)
	c.Get("item1") // probation → protected

	// item2 fills probation.
	c.Set("item2", body(100), time.Hour) // probation=[item2], protected=[item1]

	// item3 triggers eviction: C(freq=1) vs item2(freq=1) → item2 evicted.
	c.Set("item3", body(100), time.Hour)

	// item1 in protected should survive.
	if _, s := c.Get("item1"); s == CacheMISS {
		t.Fatal("item1 in protected should have survived eviction")
	}
	// item2 in probation tail should be evicted.
	if _, s := c.Get("item2"); s != CacheMISS {
		t.Fatalf("item2 in probation should have been evicted, got %v", s)
	}
}

func TestWTinyLFUConcurrency(t *testing.T) {
	c := NewWTinyLFUCache(10 * 1024 * 1024)
	defer c.Destroy()

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", i%20)
			c.Set(key, makeEntry(key, []byte("v")), time.Hour)
			c.Get(key)
		}(i)
	}
	wg.Wait()
}

func TestWTinyLFUTTLExpiry(t *testing.T) {
	c := NewWTinyLFUCache(10 * 1024 * 1024)
	defer c.Destroy()

	c.Set("expiring", makeEntry("expiring", []byte("data")), 50*time.Millisecond)

	if _, s := c.Get("expiring"); s != CacheHIT {
		t.Fatal("expected HIT before TTL")
	}

	time.Sleep(100 * time.Millisecond)

	if _, s := c.Get("expiring"); s != CacheMISS {
		t.Fatalf("expected MISS after TTL, got %v", s)
	}
}

func TestWTinyLFUPurge(t *testing.T) {
	c := NewWTinyLFUCache(10 * 1024 * 1024)
	defer c.Destroy()

	c.Set("img_logo", makeEntry("img_logo", []byte("img")), time.Hour)
	c.Set("img_icon", makeEntry("img_icon", []byte("img")), time.Hour)
	c.Set("api_data", makeEntry("api_data", []byte("data")), time.Hour)

	n := c.Purge("img_*")
	if n != 2 {
		t.Fatalf("expected 2 purged, got %d", n)
	}

	if _, s := c.Get("img_logo"); s != CacheMISS {
		t.Fatal("img_logo should be gone after purge")
	}
	if _, s := c.Get("img_icon"); s != CacheMISS {
		t.Fatal("img_icon should be gone after purge")
	}
	if _, s := c.Get("api_data"); s != CacheHIT {
		t.Fatal("api_data should still be present")
	}
}

func TestWTinyLFUStats(t *testing.T) {
	c := NewWTinyLFUCache(10 * 1024 * 1024)
	defer c.Destroy()

	c.Set("k", makeEntry("k", []byte("v")), time.Hour)
	c.Get("k")       // hit
	c.Get("k")       // hit
	c.Get("missing") // miss

	st := c.Stats()
	if st.Hits != 2 {
		t.Fatalf("expected 2 hits, got %d", st.Hits)
	}
	if st.Misses != 1 {
		t.Fatalf("expected 1 miss, got %d", st.Misses)
	}
	want := 2.0 / 3.0
	if st.HitRate < want-0.01 || st.HitRate > want+0.01 {
		t.Fatalf("expected hit rate ~%.2f, got %.4f", want, st.HitRate)
	}
	if c.Name() != "w-tinylfu" {
		t.Fatalf("unexpected name: %s", c.Name())
	}
}
