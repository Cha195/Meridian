package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cha195/meridian/internal/cache"
	"github.com/cha195/meridian/internal/config"
	"github.com/cha195/meridian/internal/geo"
)

func testAPIServerWithCache(t *testing.T) *APIServer {
	t.Helper()
	cfg := &config.Config{
		Node: config.NodeConfig{ID: "us-east-1", Listen: ":8080"},
		Cluster: config.ClusterConfig{
			Nodes: []config.NodeLocation{
				{ID: "us-east-1", Lat: 39.04, Lng: -77.47},
			},
		},
		Projects: []config.ProjectConfig{
			{
				ID:     "proj_test",
				APIKey: "test-key-123",
				Origin: "https://example.com",
				Cache:  config.CacheConfig{Policy: "sieve", MaxSizeMB: 10},
			},
		},
	}

	cs, err := geo.NewClusterState("us-east-1", cfg.Cluster.Nodes)
	if err != nil {
		t.Fatal(err)
	}

	sieveCache, err := cache.NewPolicy("sieve", 10*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	mc := cache.NewMigratingCache(sieveCache)
	caches := map[string]*cache.MigratingCache{"proj_test": mc}

	return NewAPIServer(cfg, nil, cs, nil, caches)
}

func TestCachePoliciesAuth(t *testing.T) {
	s := testAPIServerWithCache(t)

	req := httptest.NewRequest("GET", "/api/v1/cache/policies", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", w.Code)
	}
}

func TestCachePoliciesValid(t *testing.T) {
	s := testAPIServerWithCache(t)

	req := httptest.NewRequest("GET", "/api/v1/cache/policies", nil)
	req.Header.Set("Authorization", "Bearer test-key-123")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	policies, ok := body["policies"].([]any)
	if !ok || len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %v", body)
	}

	p := policies[0].(map[string]any)
	if p["project_id"] != "proj_test" {
		t.Errorf("expected proj_test, got %v", p["project_id"])
	}
	if p["active_policy"] != "sieve" {
		t.Errorf("expected sieve, got %v", p["active_policy"])
	}
}

func TestCacheMigrateStartStop(t *testing.T) {
	s := testAPIServerWithCache(t)

	// Start migration
	body, _ := json.Marshal(map[string]string{"project_id": "proj_test", "policy": "lru"})
	req := httptest.NewRequest("POST", "/api/v1/cache/migrate", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key-123")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Check status
	req = httptest.NewRequest("GET", "/api/v1/cache/migration/status", nil)
	req.Header.Set("Authorization", "Bearer test-key-123")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var statuses map[string]any
	json.NewDecoder(w.Body).Decode(&statuses)
	projStatus, ok := statuses["proj_test"].(map[string]any)
	if !ok {
		t.Fatal("expected proj_test in migration statuses")
	}
	if projStatus["IsWarming"] != true {
		t.Error("expected migration to be active")
	}

	// Abort migration
	body, _ = json.Marshal(map[string]string{"project_id": "proj_test"})
	req = httptest.NewRequest("POST", "/api/v1/cache/migrate/abort", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key-123")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify migration stopped
	req = httptest.NewRequest("GET", "/api/v1/cache/migration/status", nil)
	req.Header.Set("Authorization", "Bearer test-key-123")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	json.NewDecoder(w.Body).Decode(&statuses)
	projStatus = statuses["proj_test"].(map[string]any)
	if projStatus["IsWarming"] != false {
		t.Error("expected migration to be stopped after abort")
	}
}

// twoProjectAPIServer returns an APIServer with two isolated projects (A and B),
// each with its own API key and MigratingCache. Used to verify cross-tenant
// isolation: a key for project A must not see, modify, or migrate project B.
func twoProjectAPIServer(t *testing.T) *APIServer {
	t.Helper()
	cfg := &config.Config{
		Node: config.NodeConfig{ID: "us-east-1", Listen: ":8080"},
		Cluster: config.ClusterConfig{
			Nodes: []config.NodeLocation{
				{ID: "us-east-1", Lat: 39.04, Lng: -77.47},
			},
		},
		Projects: []config.ProjectConfig{
			{ID: "proj_a", APIKey: "key-a", Origin: "https://a.example.com",
				Cache: config.CacheConfig{Policy: "sieve", MaxSizeMB: 10}},
			{ID: "proj_b", APIKey: "key-b", Origin: "https://b.example.com",
				Cache: config.CacheConfig{Policy: "lru", MaxSizeMB: 10}},
		},
	}

	cs, err := geo.NewClusterState("us-east-1", cfg.Cluster.Nodes)
	if err != nil {
		t.Fatal(err)
	}

	caches := make(map[string]*cache.MigratingCache, 2)
	for _, p := range cfg.Projects {
		policy, err := cache.NewPolicy(p.Cache.Policy, int64(p.Cache.MaxSizeMB)*1024*1024)
		if err != nil {
			t.Fatal(err)
		}
		caches[p.ID] = cache.NewMigratingCache(policy)
	}

	return NewAPIServer(cfg, nil, cs, nil, caches)
}

// TestCrossTenantIsolation verifies that an API key for project A only ever
// sees, modifies, or migrates project A's cache — never project B's.
func TestCrossTenantIsolation(t *testing.T) {
	s := twoProjectAPIServer(t)

	// Pre-load proj_b's migration so we can verify proj_a's key cannot see it.
	bMC := s.caches["proj_b"]
	lruWarm, _ := cache.NewPolicy("lru", 10*1024*1024)
	bMC.StartMigration(lruWarm)
	defer bMC.AbortMigration()

	doRequest := func(method, path, key string, body []byte) *httptest.ResponseRecorder {
		var reader *bytes.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		} else {
			reader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		return w
	}

	// 1. GET /cache/policies as proj_a → must contain ONLY proj_a, never proj_b.
	w := doRequest("GET", "/api/v1/cache/policies", "key-a", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("policies: expected 200, got %d", w.Code)
	}
	var polBody map[string]any
	json.NewDecoder(w.Body).Decode(&polBody)
	policies := polBody["policies"].([]any)
	if len(policies) != 1 {
		t.Fatalf("policies: expected exactly 1 entry, got %d (cross-tenant leak)", len(policies))
	}
	if policies[0].(map[string]any)["project_id"] != "proj_a" {
		t.Fatalf("policies: expected proj_a only, got %v", policies[0])
	}

	// 2. GET /cache/migration/status as proj_a → must NOT contain proj_b.
	w = doRequest("GET", "/api/v1/cache/migration/status", "key-a", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("migration status: expected 200, got %d", w.Code)
	}
	var statuses map[string]any
	json.NewDecoder(w.Body).Decode(&statuses)
	if _, ok := statuses["proj_b"]; ok {
		t.Fatal("migration status: proj_a's key leaked proj_b's status (cross-tenant leak)")
	}
	if _, ok := statuses["proj_a"]; !ok {
		t.Fatal("migration status: proj_a should be in the response")
	}

	// 3. POST /cache/purge as proj_a → must purge ONLY proj_a's cache.
	//    Pre-seed both caches with a known key to detect a stray purge.
	makeEntry := func() *cache.CacheEntry {
		return &cache.CacheEntry{
			Response:  cache.CachedResponse{StatusCode: 200, Body: []byte("v")},
			SizeBytes: 1,
		}
	}
	s.caches["proj_a"].Set("img_a", makeEntry(), time.Hour)
	s.caches["proj_b"].Set("img_b", makeEntry(), time.Hour)

	purgeBody, _ := json.Marshal(map[string]string{"pattern": "img_*"})
	w = doRequest("POST", "/api/v1/cache/purge", "key-a", purgeBody)
	if w.Code != http.StatusOK {
		t.Fatalf("purge: expected 200, got %d", w.Code)
	}
	if _, status := s.caches["proj_a"].Get("img_a"); status != cache.CacheMISS {
		t.Error("purge: proj_a's img_a should have been purged")
	}
	if _, status := s.caches["proj_b"].Get("img_b"); status != cache.CacheHIT {
		t.Fatal("purge: proj_b's img_b was wrongly purged by proj_a's key (cross-tenant leak)")
	}

	// 4. POST /cache/migrate as proj_a → must affect proj_a only, never proj_b.
	//    proj_a starts with "sieve"; we'll migrate to "lru".
	if name := s.caches["proj_a"].Name(); name != "sieve" {
		t.Fatalf("setup: expected proj_a active=sieve, got %s", name)
	}
	migrateBody, _ := json.Marshal(map[string]string{"policy": "w-tinylfu"})
	w = doRequest("POST", "/api/v1/cache/migrate", "key-a", migrateBody)
	if w.Code != http.StatusOK {
		t.Fatalf("migrate: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// proj_a must now be warming.
	if !s.caches["proj_a"].MigrationStatus().IsWarming {
		t.Error("migrate: proj_a should be warming after migrate")
	}
	// proj_b must still be in its prior migration state (the lruWarm we set up earlier).
	bStatus := s.caches["proj_b"].MigrationStatus()
	if bStatus.WarmingName != "lru" {
		t.Errorf("migrate: proj_b's migration was disturbed by proj_a's migrate (warming=%q, expected lru)", bStatus.WarmingName)
	}

	// 5. POST /cache/migrate/abort as proj_a → must abort proj_a only.
	w = doRequest("POST", "/api/v1/cache/migrate/abort", "key-a", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("abort: expected 200, got %d", w.Code)
	}
	if s.caches["proj_a"].MigrationStatus().IsWarming {
		t.Error("abort: proj_a's migration should be aborted")
	}
	if !s.caches["proj_b"].MigrationStatus().IsWarming {
		t.Error("abort: proj_b's migration was wrongly aborted by proj_a's key (cross-tenant leak)")
	}
}
