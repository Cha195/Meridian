package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cha195/meridian/internal/cache"
	"github.com/cha195/meridian/internal/config"
	"github.com/cha195/meridian/internal/geo"
)

const maxCacheBodySize = 10 * 1024 * 1024 // 10MB

type ProxyHandler struct {
	projects   map[string]*config.ProjectConfig
	geoLocator *geo.GeoLocator
	caches     map[string]cache.CachePolicy
}

func NewProxyHandler(cfg *config.Config, geoLocator *geo.GeoLocator) (*ProxyHandler, error) {
	h := &ProxyHandler{
		projects:   make(map[string]*config.ProjectConfig),
		geoLocator: geoLocator,
		caches:     make(map[string]cache.CachePolicy),
	}

	for i := range cfg.Projects {
		proj := &cfg.Projects[i]

		maxSize := int64(proj.Cache.MaxSizeMB) * 1024 * 1024
		if maxSize == 0 {
			maxSize = 512 * 1024 * 1024
		}

		policy := proj.Cache.Policy
		if policy == "" {
			policy = "sieve"
		}

		c, err := cache.NewPolicy(policy, maxSize)
		if err != nil {
			return nil, fmt.Errorf("project %q: %w", proj.ID, err)
		}
		h.caches[proj.ID] = c

		for _, host := range proj.Hosts {
			h.projects[host] = proj
		}
	}

	return h, nil
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	host := r.Host
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		host = hostOnly
	}

	proj, ok := h.projects[host]
	if !ok {
		http.Error(w, "unknown host", http.StatusNotFound)
		return
	}

	clientIP := extractClientIP(r)
	var geoResult geo.GeoResult
	if h.geoLocator != nil {
		geoResult = h.geoLocator.Lookup(clientIP)
	}

	cacheKey := buildCacheKey(proj.ID, r)
	c := h.caches[proj.ID]

	entry, status := c.Get(cacheKey)

	switch status {
	case cache.CacheHIT:
		writeFromCache(w, entry)
		logRequest(r, proj.ID, "HIT", http.StatusOK, time.Since(start), geoResult)
		return

	case cache.CacheSTALE:
		writeFromCache(w, entry)
		go h.revalidate(r, proj, c, cacheKey)
		logRequest(r, proj.ID, "STALE", http.StatusOK, time.Since(start), geoResult)
		return
	}

	// MISS: proxy to origin
	rec := &responseRecorder{
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}

	originURL, err := url.Parse(proj.Origin)
	if err != nil {
		http.Error(w, "invalid origin", http.StatusBadGateway)
		return
	}

	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = originURL.Scheme
			req.URL.Host = originURL.Host
			req.Host = originURL.Host
			if clientIP != "" {
				req.Header.Set("X-Forwarded-For", clientIP)
			}
		},
	}
	rp.ServeHTTP(rec, r)

	if isCacheable(r, rec, proj) {
		ttl := getTTL(r.URL.Path, proj)
		if ttl > 0 {
			body := rec.body.Bytes()
			headers := rec.header.Clone()
			ce := &cache.CacheEntry{
				Key: cacheKey,
				Response: cache.CachedResponse{
					StatusCode: rec.statusCode,
					Headers:    headers,
					Body:       body,
				},
				SizeBytes: int64(len(body)),
			}
			staleExtra := time.Duration(proj.Cache.StaleWhileRevalidate) * time.Second
			ce.StaleDeadline = time.Now().Add(ttl).Add(staleExtra)
			c.Set(cacheKey, ce, ttl)
		}
	}

	for k, vals := range rec.header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(rec.statusCode)
	w.Write(rec.body.Bytes())

	logRequest(r, proj.ID, "MISS", rec.statusCode, time.Since(start), geoResult)
}

func (h *ProxyHandler) revalidate(r *http.Request, proj *config.ProjectConfig, c cache.CachePolicy, cacheKey string) {
	req := r.Clone(r.Context())

	originURL, err := url.Parse(proj.Origin)
	if err != nil {
		return
	}

	rec := &responseRecorder{
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}

	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = originURL.Scheme
			req.URL.Host = originURL.Host
			req.Host = originURL.Host
		},
	}
	rp.ServeHTTP(rec, req)

	if isCacheable(r, rec, proj) {
		ttl := getTTL(r.URL.Path, proj)
		if ttl > 0 {
			body := rec.body.Bytes()
			ce := &cache.CacheEntry{
				Key: cacheKey,
				Response: cache.CachedResponse{
					StatusCode: rec.statusCode,
					Headers:    rec.header.Clone(),
					Body:       body,
				},
				SizeBytes: int64(len(body)),
			}
			c.Set(cacheKey, ce, ttl)
		}
	}
}

func isCacheable(req *http.Request, rec *responseRecorder, proj *config.ProjectConfig) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}

	switch rec.statusCode {
	case http.StatusOK, http.StatusMovedPermanently, http.StatusNotFound:
	default:
		return false
	}

	cc := rec.header.Get("Cache-Control")
	if strings.Contains(cc, "no-store") || strings.Contains(cc, "private") {
		return false
	}

	if int64(rec.body.Len()) > maxCacheBodySize {
		return false
	}

	return true
}

func getTTL(path string, proj *config.ProjectConfig) time.Duration {
	for _, rule := range proj.Cache.Rules {
		matched, err := filepath.Match(rule.Path, path)
		if err == nil && matched {
			return time.Duration(rule.TTL) * time.Second
		}
	}
	return time.Duration(proj.Cache.DefaultTTL) * time.Second
}

func buildCacheKey(projectID string, r *http.Request) string {
	queryKeys := make([]string, 0, len(r.URL.Query()))
	for k := range r.URL.Query() {
		queryKeys = append(queryKeys, k)
	}
	sort.Strings(queryKeys)

	parts := make([]string, 0, len(queryKeys))
	for _, k := range queryKeys {
		parts = append(parts, k+"="+r.URL.Query().Get(k))
	}

	query := strings.Join(parts, "&")
	return projectID + ":" + r.Method + ":" + r.URL.Path + "?" + query
}

func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeFromCache(w http.ResponseWriter, entry *cache.CacheEntry) {
	for k, vals := range entry.Response.Headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Cache", "HIT")
	w.WriteHeader(entry.Response.StatusCode)
	w.Write(entry.Response.Body)
}

func logRequest(r *http.Request, projectID, cacheStatus string, statusCode int, elapsed time.Duration, gr geo.GeoResult) {
	country := gr.Country
	if country == "" {
		country = "unknown"
	}
	log.Printf("%s %s [%s] project=%s cache=%s status=%d elapsed=%s country=%s",
		r.Method, r.URL.Path, r.Host, projectID, cacheStatus, statusCode, elapsed.Round(time.Millisecond), country)
}

// responseRecorder captures origin response for caching.
type responseRecorder struct {
	header     http.Header
	statusCode int
	body       bytes.Buffer
}

func (rr *responseRecorder) Header() http.Header {
	return rr.header
}

func (rr *responseRecorder) WriteHeader(statusCode int) {
	rr.statusCode = statusCode
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	return rr.body.Write(b)
}

// Ensure responseRecorder satisfies io.Writer for httputil internals.
var _ io.Writer = (*responseRecorder)(nil)
