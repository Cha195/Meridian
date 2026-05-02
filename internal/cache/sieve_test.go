package cache

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func makeEntry(key string, body []byte) *CacheEntry {
	return &CacheEntry{
		Key: key,
		Response: CachedResponse{
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": []string{"text/plain"}},
			Body:       body,
		},
		SizeBytes: int64(len(body)),
	}
}

func TestSieveBasicHitMiss(t *testing.T) {
	cache := NewSieveCache(1024)
	defer cache.Destroy()

	entry := makeEntry("key1", []byte("value1"))
	cache.Set("key1", entry, time.Hour)

	result, status := cache.Get("key1")
	if status != CacheHIT {
		t.Errorf("expected HIT, got %v", status)
	}
	if result.Key != "key1" {
		t.Errorf("expected key1, got %s", result.Key)
	}

	result, status = cache.Get("missing")
	if status != CacheMISS {
		t.Errorf("expected MISS, got %v", status)
	}
	if result != nil {
		t.Errorf("expected nil for missing key")
	}
}

func TestSieveEviction(t *testing.T) {
	cache := NewSieveCache(150)
	defer cache.Destroy()

	cache.Set("a", makeEntry("a", make([]byte, 40)), time.Hour)
	cache.Set("b", makeEntry("b", make([]byte, 40)), time.Hour)
	cache.Set("c", makeEntry("c", make([]byte, 40)), time.Hour)

	if cache.Len() != 3 {
		t.Errorf("expected 3 entries, got %d", cache.Len())
	}

	cache.Set("d", makeEntry("d", make([]byte, 40)), time.Hour)

	if cache.Len() != 3 {
		t.Errorf("expected 3 entries after eviction, got %d", cache.Len())
	}

	_, status := cache.Get("a")
	if status == CacheHIT {
		t.Error("expected 'a' to be evicted (oldest unvisited)")
	}
}

func TestSieveVisitedBitProtection(t *testing.T) {
	cache := NewSieveCache(120)
	defer cache.Destroy()

	cache.Set("a", makeEntry("a", make([]byte, 40)), time.Hour)
	cache.Set("b", makeEntry("b", make([]byte, 40)), time.Hour)
	cache.Set("c", makeEntry("c", make([]byte, 40)), time.Hour)

	cache.Get("a")

	cache.Set("d", makeEntry("d", make([]byte, 40)), time.Hour)

	_, aStatus := cache.Get("a")
	if aStatus != CacheHIT {
		t.Error("expected 'a' to survive (visited bit protection)")
	}

	_, bStatus := cache.Get("b")
	if bStatus == CacheHIT {
		t.Error("expected 'b' to be evicted (unvisited, older than protected 'a')")
	}
}

func TestSieveHandWraparound(t *testing.T) {
	cache := NewSieveCache(150)
	defer cache.Destroy()

	cache.Set("a", makeEntry("a", make([]byte, 40)), time.Hour)
	cache.Set("b", makeEntry("b", make([]byte, 40)), time.Hour)
	cache.Set("c", makeEntry("c", make([]byte, 40)), time.Hour)

	cache.Get("a")
	cache.Get("b")
	cache.Get("c")

	cache.Set("d", makeEntry("d", make([]byte, 40)), time.Hour)

	stats := cache.Stats()
	if stats.Evictions == 0 {
		t.Error("expected at least one eviction during wraparound")
	}
}

func TestSieveTTLExpiry(t *testing.T) {
	cache := NewSieveCache(1024)
	defer cache.Destroy()

	entry := makeEntry("expiring", []byte("value"))
	cache.Set("expiring", entry, 5*time.Millisecond)

	result, status := cache.Get("expiring")
	if status != CacheHIT {
		t.Fatal("expected HIT before expiry")
	}

	time.Sleep(10 * time.Millisecond)

	result, status = cache.Get("expiring")
	if status != CacheMISS {
		t.Errorf("expected MISS after expiry, got %v", status)
	}
	if result != nil {
		t.Error("expected nil after expiry")
	}
}

func TestSieveConcurrency(t *testing.T) {
	cache := NewSieveCache(10240)
	defer cache.Destroy()

	var wg sync.WaitGroup
	wg.Add(100)

	for i := 0; i < 100; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				key := "key" + string(rune(j%10))
				entry := makeEntry(key, make([]byte, 50))
				cache.Set(key, entry, time.Hour)
				cache.Get(key)
			}
		}(i)
	}

	wg.Wait()

	stats := cache.Stats()
	if stats.Hits == 0 && stats.Misses == 0 {
		t.Error("expected some hits or misses")
	}
}

func TestSievePurgePattern(t *testing.T) {
	cache := NewSieveCache(10240)
	defer cache.Destroy()

	cache.Set("/blog/a", makeEntry("/blog/a", []byte("content")), time.Hour)
	cache.Set("/blog/b", makeEntry("/blog/b", []byte("content")), time.Hour)
	cache.Set("/api/c", makeEntry("/api/c", []byte("content")), time.Hour)

	count := cache.Purge("/blog/*")
	if count != 2 {
		t.Errorf("expected 2 purged entries, got %d", count)
	}

	_, status := cache.Get("/blog/a")
	if status == CacheHIT {
		t.Error("expected /blog/a to be purged")
	}

	_, status = cache.Get("/api/c")
	if status != CacheHIT {
		t.Error("expected /api/c to survive purge")
	}
}

func TestSieveStats(t *testing.T) {
	cache := NewSieveCache(1024)
	defer cache.Destroy()

	entry := makeEntry("key", []byte("value"))
	cache.Set("key", entry, time.Hour)

	cache.Get("key")
	cache.Get("key")
	cache.Get("missing")

	stats := cache.Stats()
	if stats.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
	if stats.HitRate != 2.0/3.0 {
		t.Errorf("expected hit rate 0.666, got %f", stats.HitRate)
	}
}
