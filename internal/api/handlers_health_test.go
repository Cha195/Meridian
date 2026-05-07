package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cha195/meridian/internal/config"
	"github.com/cha195/meridian/internal/geo"
)

func testAPIServer(t *testing.T) *APIServer {
	t.Helper()
	cfg := &config.Config{
		Node: config.NodeConfig{ID: "us-east-1", Listen: ":8080"},
		Cluster: config.ClusterConfig{
			Nodes: []config.NodeLocation{
				{ID: "us-east-1", Lat: 39.04, Lng: -77.47},
				{ID: "eu-west-1", Lat: 51.50, Lng: -0.12},
			},
		},
		Projects: []config.ProjectConfig{
			{ID: "proj_test", APIKey: "test-key-123", Origin: "https://example.com"},
		},
	}

	cs, err := geo.NewClusterState("us-east-1", cfg.Cluster.Nodes)
	if err != nil {
		t.Fatal(err)
	}

	return NewAPIServer(cfg, nil, cs, nil, nil)
}

func TestHealthz(t *testing.T) {
	s := testAPIServer(t)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("expected status ok, got %s", body["status"])
	}
}

func TestReadyzWithoutGeoIP(t *testing.T) {
	s := testAPIServer(t)

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 without geoip, got %d", w.Code)
	}
}

func TestClusterStatusRequiresAuth(t *testing.T) {
	s := testAPIServer(t)

	req := httptest.NewRequest("GET", "/api/v1/cluster/status", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestClusterStatusAuthenticated(t *testing.T) {
	s := testAPIServer(t)

	req := httptest.NewRequest("GET", "/api/v1/cluster/status", nil)
	req.Header.Set("Authorization", "Bearer test-key-123")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["self"] != "us-east-1" {
		t.Errorf("expected self=us-east-1, got %v", body["self"])
	}
	nodes, ok := body["nodes"].([]any)
	if !ok || len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %v", body["nodes"])
	}
}
