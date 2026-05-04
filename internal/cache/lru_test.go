package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLRUBasicHitMiss(t *testing.T) {
	c := NewLRUCache(10 * 1024 * 1024)
	defer c.Destroy()

	c.Set("k1", makeEntry("k1", []byte("hello")), time.Hour)

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

// TestLRUEvictionOrder verifies that the least-recently-used item is evicted first.
// Each item is 100 bytes; maxSize=250 fits 2. Setting a third triggers eviction of the LRU tail.
func TestLRUEvictionOrder(t *testing.T) {
	c := NewLRUCache(250)
	defer c.Destroy()

	c.Set("A", body(100), time.Hour)
	c.Set("B", body(100), time.Hour) // order: B(head) → A(tail)
	c.Get("A")                       // access A: A(head) → B(tail)
	c.Set("C", body(100), time.Hour) // triggers eviction; B is tail (LRU)

	if _, s := c.Get("A"); s == CacheMISS {
		t.Fatal("A was recently accessed and should survive eviction")
	}
	if _, s := c.Get("B"); s != CacheMISS {
		t.Fatalf("B (LRU tail) should have been evicted, got %v", s)
	}
	if _, s := c.Get("C"); s == CacheMISS {
		t.Fatal("C should be present after insertion")
	}
}

func TestLRUTTLExpiry(t *testing.T) {
	c := NewLRUCache(10 * 1024 * 1024)
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

func TestLRUConcurrency(t *testing.T) {
	c := NewLRUCache(10 * 1024 * 1024)
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

func TestLRUStats(t *testing.T) {
	c := NewLRUCache(10 * 1024 * 1024)
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
	if c.Name() != "lru" {
		t.Fatalf("unexpected name: %s", c.Name())
	}
}

func TestLRUPurge(t *testing.T) {
	c := NewLRUCache(10 * 1024 * 1024)
	defer c.Destroy()

	c.Set("img_logo", makeEntry("img_logo", []byte("img")), time.Hour)
	c.Set("img_icon", makeEntry("img_icon", []byte("img")), time.Hour)
	c.Set("api_data", makeEntry("api_data", []byte("data")), time.Hour)

	n := c.Purge("img_*")
	if n != 2 {
		t.Fatalf("expected 2 purged, got %d", n)
	}
	if _, s := c.Get("api_data"); s != CacheHIT {
		t.Fatal("api_data should still be present")
	}
}
