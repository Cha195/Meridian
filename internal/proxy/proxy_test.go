package proxy

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cha195/meridian/internal/config"
)

func newTestConfig(originURL string) *config.Config {
	return &config.Config{
		Node: config.NodeConfig{ID: "test-node", Listen: ":0"},
		Projects: []config.ProjectConfig{
			{
				ID:     "proj_test",
				Origin: originURL,
				Hosts:  []string{"test.example.com"},
				Cache: config.CacheConfig{
					Policy:     "sieve",
					DefaultTTL: 3600,
					MaxSizeMB:  64,
					Rules: []config.CacheRule{
						{Path: "/api/*", TTL: 0},
					},
				},
			},
		},
	}
}

func TestProxyBasicForward(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from origin"))
	}))
	defer origin.Close()

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	req.Host = "test.example.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "hello from origin" {
		t.Errorf("expected 'hello from origin', got %q", rec.Body.String())
	}
}

func TestProxyCacheHit(t *testing.T) {
	callCount := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("cached response"))
	}))
	defer origin.Close()

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	req1 := httptest.NewRequest(http.MethodGet, "/cacheable", nil)
	req1.Host = "test.example.com"
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	if callCount != 1 {
		t.Errorf("expected 1 origin call after first request, got %d", callCount)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/cacheable", nil)
	req2.Host = "test.example.com"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if callCount != 1 {
		t.Errorf("expected origin called only once (cache HIT), got %d calls", callCount)
	}
	if rec2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("expected X-Cache: HIT header on second request")
	}
	if rec2.Body.String() != "cached response" {
		t.Errorf("expected cached body, got %q", rec2.Body.String())
	}
}

func TestProxyCacheNoPOST(t *testing.T) {
	callCount := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("post response"))
	}))
	defer origin.Close()

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/submit", nil)
		req.Host = "test.example.com"
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	if callCount != 3 {
		t.Errorf("POST should never cache: expected 3 origin calls, got %d", callCount)
	}
}

func TestProxyHostMatching(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer origin.Close()

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "unknown.host.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown host, got %d", rec.Code)
	}
}

func TestProxyTTLRules(t *testing.T) {
	callCount := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("api response"))
	}))
	defer origin.Close()

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		req.Host = "test.example.com"
		handler.ServeHTTP(httptest.NewRecorder(), req)
		time.Sleep(time.Millisecond)
	}

	if callCount != 3 {
		t.Errorf("/api/* with ttl:0 should not cache: expected 3 origin calls, got %d", callCount)
	}
}

// --- Cache safety tests ---

func TestCacheBypassWithAuthHeader(t *testing.T) {
	callCount := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("private data"))
	}))
	defer origin.Close()

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/profile", nil)
		req.Host = "test.example.com"
		req.Header.Set("Authorization", "Bearer token123")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	if callCount != 2 {
		t.Errorf("authorized requests should not cache: expected 2 origin calls, got %d", callCount)
	}
}

func TestCacheBypassWithCookie(t *testing.T) {
	callCount := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("user session data"))
	}))
	defer origin.Close()

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
		req.Host = "test.example.com"
		req.Header.Set("Cookie", "session=abc123")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	if callCount != 2 {
		t.Errorf("cookie requests should not cache (no public override): expected 2 origin calls, got %d", callCount)
	}
}

func TestCacheWithPublicOverride(t *testing.T) {
	callCount := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("public despite cookie"))
	}))
	defer origin.Close()

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/public-asset", nil)
		req.Host = "test.example.com"
		req.Header.Set("Cookie", "session=abc123")
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	if callCount != 1 {
		t.Errorf("Cache-Control: public should allow caching despite Cookie: expected 1 origin call, got %d", callCount)
	}
}

func TestVaryHeaderCacheSeparation(t *testing.T) {
	callCount := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Vary", "Accept-Language")
		w.WriteHeader(http.StatusOK)
		lang := r.Header.Get("Accept-Language")
		if lang == "" {
			lang = "en"
		}
		w.Write([]byte("content in " + lang))
	}))
	defer origin.Close()

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	doRequest := func(lang string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/products", nil)
		req.Host = "test.example.com"
		req.Header.Set("Accept-Language", lang)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Request 1: en — MISS
	doRequest("en")
	if callCount != 1 {
		t.Fatalf("expected 1 origin call after first en request, got %d", callCount)
	}

	// Request 2: en — HIT
	rec := doRequest("en")
	if callCount != 1 {
		t.Errorf("second en request should be a HIT, got %d origin calls", callCount)
	}
	if rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("expected X-Cache: HIT for second en request")
	}

	// Request 3: fr — MISS (different Vary value)
	doRequest("fr")
	if callCount != 2 {
		t.Errorf("fr request should be a MISS (different Vary key), got %d origin calls", callCount)
	}

	// Request 4: fr — HIT
	rec = doRequest("fr")
	if callCount != 2 {
		t.Errorf("second fr request should be a HIT, got %d origin calls", callCount)
	}
	if rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("expected X-Cache: HIT for second fr request")
	}
}

func TestVaryWildcardNotCached(t *testing.T) {
	callCount := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Vary", "*")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("unique every time"))
	}))
	defer origin.Close()

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/dynamic", nil)
		req.Host = "test.example.com"
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	if callCount != 3 {
		t.Errorf("Vary: * should never cache: expected 3 origin calls, got %d", callCount)
	}
}

func TestPathRuleBypassesEverything(t *testing.T) {
	callCount := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("health ok"))
	}))
	defer origin.Close()

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.Host = "test.example.com"
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	if callCount != 3 {
		t.Errorf("path rule ttl:0 should bypass cache: expected 3 origin calls, got %d", callCount)
	}
}

func TestStaleWhileRevalidate(t *testing.T) {
	var callCount atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fresh content"))
	}))
	defer origin.Close()

	cfg := &config.Config{
		Node: config.NodeConfig{ID: "test-node", Listen: ":0"},
		Projects: []config.ProjectConfig{
			{
				ID:     "proj_swr",
				Origin: origin.URL,
				Hosts:  []string{"swr.example.com"},
				Cache: config.CacheConfig{
					Policy:               "w-tinylfu",
					DefaultTTL:           1,
					MaxSizeMB:            64,
					StaleWhileRevalidate: 60,
				},
			},
		},
	}

	handler, err := NewProxyHandler(cfg, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	// Request 1: MISS — fetches from origin.
	req1 := httptest.NewRequest(http.MethodGet, "/page", nil)
	req1.Host = "swr.example.com"
	handler.ServeHTTP(httptest.NewRecorder(), req1)

	if callCount.Load() != 1 {
		t.Fatalf("expected 1 origin call after first request, got %d", callCount.Load())
	}

	// Wait for TTL to expire (1 second) but stay within stale window (60 seconds).
	time.Sleep(1500 * time.Millisecond)

	// Request 2: should get the stale cached response and trigger background revalidation.
	req2 := httptest.NewRequest(http.MethodGet, "/page", nil)
	req2.Host = "swr.example.com"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Body.String() != "fresh content" {
		t.Fatalf("expected stale response body, got %q", rec2.Body.String())
	}

	// Wait for the background revalidation goroutine to complete.
	time.Sleep(500 * time.Millisecond)

	if callCount.Load() != 2 {
		t.Fatalf("expected 2 origin calls (initial + revalidation), got %d", callCount.Load())
	}

	// Request 3: should be a fresh HIT from the revalidated entry.
	req3 := httptest.NewRequest(http.MethodGet, "/page", nil)
	req3.Host = "swr.example.com"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	if callCount.Load() != 2 {
		t.Fatalf("expected no additional origin call (fresh HIT), got %d", callCount.Load())
	}
	if rec3.Header().Get("X-Cache") != "HIT" {
		t.Errorf("expected X-Cache: HIT after revalidation, got %q", rec3.Header().Get("X-Cache"))
	}
}
