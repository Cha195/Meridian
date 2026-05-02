package proxy

import (
	"net/http"
	"net/http/httptest"
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

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil)
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

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	// First request — MISS, origin called
	req1 := httptest.NewRequest(http.MethodGet, "/cacheable", nil)
	req1.Host = "test.example.com"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if callCount != 1 {
		t.Errorf("expected 1 origin call after first request, got %d", callCount)
	}

	// Second request — HIT, origin not called again
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

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/submit", nil)
		req.Host = "test.example.com"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
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

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil)
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

	handler, err := NewProxyHandler(newTestConfig(origin.URL), nil)
	if err != nil {
		t.Fatalf("NewProxyHandler failed: %v", err)
	}

	// /api/* has TTL 0 — should never cache
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
		req.Host = "test.example.com"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		time.Sleep(time.Millisecond)
	}

	if callCount != 3 {
		t.Errorf("/api/* with ttl:0 should not cache: expected 3 origin calls, got %d", callCount)
	}
}
