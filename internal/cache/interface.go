package cache

import (
	"net/http"
	"time"
)

type CacheStatus int

const (
	CacheHIT CacheStatus = iota
	CacheMISS
	CacheSTALE
	CacheBYPASS
)

type CachedResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

type CacheEntry struct {
	Key           string
	Response      CachedResponse
	CreatedAt     time.Time
	ExpiresAt     time.Time
	StaleDeadline time.Time
	AccessCount   int64
	SizeBytes     int64
}

type CacheStats struct {
	Hits       int64
	Misses     int64
	Evictions  int64
	HitRate    float64
}

type CachePolicy interface {
	Get(key string) (*CacheEntry, CacheStatus)
	Set(key string, entry *CacheEntry, ttl time.Duration)
	Delete(key string)
	Purge(pattern string) int
	Size() int64
	Len() int
	Stats() CacheStats
	Name() string
	Destroy()
}
