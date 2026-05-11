package config

import (
	"os"
	"testing"
)

// minimalCluster returns a one-node ClusterConfig valid for the given selfID.
func minimalCluster(selfID string) ClusterConfig {
	return ClusterConfig{
		Nodes: []NodeLocation{
			{ID: selfID, Lat: 39.0481, Lng: -77.4728, Addr: "http://localhost:9090"},
		},
	}
}

func TestLoadConfig(t *testing.T) {
	content := `
node:
  id: us-east-1
  listen: ":8080"
  geoip_db: /path/to/GeoLite2-City.mmdb

database:
  conn_string: "postgres://meridian:pass@localhost:5432/meridian?sslmode=disable"

cluster:
  nodes:
    - id: us-east-1
      lat: 39.0481
      lng: -77.4728
      addr: "http://localhost:9090"

projects:
  - id: proj_test
    api_key: gv_live_sk_test
    origin: https://example.com
    hosts:
      - example.com
    cache:
      policy: lru
      default_ttl: 3600
      max_size_mb: 512
`

	tmpfile, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(content); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpfile.Close()

	cfg, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Node.ID != "us-east-1" {
		t.Errorf("expected node ID 'us-east-1', got %q", cfg.Node.ID)
	}
	if len(cfg.Projects) != 1 {
		t.Errorf("expected 1 project, got %d", len(cfg.Projects))
	}
	if cfg.Projects[0].Origin != "https://example.com" {
		t.Errorf("expected origin 'https://example.com', got %q", cfg.Projects[0].Origin)
	}
	if len(cfg.Cluster.Nodes) != 1 {
		t.Errorf("expected 1 cluster node, got %d", len(cfg.Cluster.Nodes))
	}
	if cfg.Database.ConnString != "postgres://meridian:pass@localhost:5432/meridian?sslmode=disable" {
		t.Errorf("expected database conn_string to be parsed, got %q", cfg.Database.ConnString)
	}
}

func TestValidateConfigMissingNodeID(t *testing.T) {
	cfg := &Config{
		Node:    NodeConfig{ID: ""},
		Cluster: minimalCluster("us-east-1"),
		Projects: []ProjectConfig{
			{ID: "proj1", Origin: "https://example.com"},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("expected validation error for missing node ID")
	}
}

func TestValidateConfigMissingProjectOrigin(t *testing.T) {
	cfg := &Config{
		Node:    NodeConfig{ID: "us-east-1"},
		Cluster: minimalCluster("us-east-1"),
		Projects: []ProjectConfig{
			{ID: "proj1", Origin: ""},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("expected validation error for missing project origin")
	}
}

func TestValidateConfigInvalidCachePolicy(t *testing.T) {
	cfg := &Config{
		Node:    NodeConfig{ID: "us-east-1"},
		Cluster: minimalCluster("us-east-1"),
		Projects: []ProjectConfig{
			{
				ID:     "proj1",
				Origin: "https://example.com",
				Cache:  CacheConfig{Policy: "invalid-policy"},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("expected validation error for invalid cache policy")
	}
}

func TestValidateConfigDuplicateHosts(t *testing.T) {
	cfg := &Config{
		Node:    NodeConfig{ID: "us-east-1"},
		Cluster: minimalCluster("us-east-1"),
		Projects: []ProjectConfig{
			{
				ID:     "proj1",
				Origin: "https://example1.com",
				Hosts:  []string{"shared.com"},
				Cache:  CacheConfig{Policy: "lru"},
			},
			{
				ID:     "proj2",
				Origin: "https://example2.com",
				Hosts:  []string{"shared.com"},
				Cache:  CacheConfig{Policy: "lru"},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("expected validation error for duplicate hosts")
	}
}

// --- Cluster validation tests ---

func TestValidateClusterNoNodes(t *testing.T) {
	cfg := &Config{
		Node:    NodeConfig{ID: "us-east-1"},
		Cluster: ClusterConfig{},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("expected error when cluster has no nodes")
	}
}

func TestValidateClusterNodeNotSelf(t *testing.T) {
	cfg := &Config{
		Node: NodeConfig{ID: "us-east-1"},
		Cluster: ClusterConfig{
			Nodes: []NodeLocation{
				{ID: "eu-west-1", Lat: 53.3331, Lng: -6.2489},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("expected error when node.id is not present in cluster.nodes")
	}
}

func TestValidateClusterDuplicateNodeIDs(t *testing.T) {
	cfg := &Config{
		Node: NodeConfig{ID: "us-east-1"},
		Cluster: ClusterConfig{
			Nodes: []NodeLocation{
				{ID: "us-east-1", Lat: 39.0481, Lng: -77.4728},
				{ID: "us-east-1", Lat: 35.6762, Lng: 139.6503},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("expected error for duplicate node IDs in cluster")
	}
}

func TestValidateClusterInvalidLat(t *testing.T) {
	cfg := &Config{
		Node: NodeConfig{ID: "bad-node"},
		Cluster: ClusterConfig{
			Nodes: []NodeLocation{
				{ID: "bad-node", Lat: 999, Lng: 0},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("expected error for lat out of range")
	}
}

func TestValidateClusterInvalidLng(t *testing.T) {
	cfg := &Config{
		Node: NodeConfig{ID: "bad-node"},
		Cluster: ClusterConfig{
			Nodes: []NodeLocation{
				{ID: "bad-node", Lat: 0, Lng: 999},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Error("expected error for lng out of range")
	}
}

func TestValidateConfigValid(t *testing.T) {
	cfg := &Config{
		Node: NodeConfig{ID: "us-east-1", Listen: ":8080"},
		Cluster: ClusterConfig{
			Nodes: []NodeLocation{
				{ID: "us-east-1", Lat: 39.0481, Lng: -77.4728},
				{ID: "eu-west-1", Lat: 53.3331, Lng: -6.2489},
			},
		},
		Projects: []ProjectConfig{
			{
				ID:     "proj1",
				Origin: "https://example.com",
				Cache:  CacheConfig{Policy: "sieve"},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err != nil {
		t.Errorf("expected no validation error for valid config, got: %v", err)
	}
}
