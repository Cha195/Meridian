package config

import (
	"os"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	content := `
node:
  id: us-east-1
  listen: ":8080"
  geoip_db: /path/to/GeoLite2-City.mmdb

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
}

func TestValidateConfigMissingNodeID(t *testing.T) {
	cfg := &Config{
		Node: NodeConfig{ID: ""},
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
		Node: NodeConfig{ID: "us-east-1"},
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
		Node: NodeConfig{ID: "us-east-1"},
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
		Node: NodeConfig{ID: "us-east-1"},
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
