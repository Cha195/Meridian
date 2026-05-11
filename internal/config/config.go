package config

import (
	"fmt"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Node     NodeConfig       `yaml:"node"`
	Cluster  ClusterConfig    `yaml:"cluster"`
	Database DatabaseConfig   `yaml:"database"`
	Projects []ProjectConfig  `yaml:"projects"`
}

type DatabaseConfig struct {
	ConnString string `yaml:"conn_string"`
}

type NodeConfig struct {
	ID      string `yaml:"id"`
	Listen  string `yaml:"listen"`
	GeoIPDB string `yaml:"geoip_db"`
}

type NodeLocation struct {
	ID   string  `yaml:"id"`
	Lat  float64 `yaml:"lat"`
	Lng  float64 `yaml:"lng"`
	Addr string  `yaml:"addr"`
}

type ClusterConfig struct {
	Nodes       []NodeLocation    `yaml:"nodes"`
	HealthCheck HealthCheckConfig `yaml:"health_check"`
}

type HealthCheckConfig struct {
	Interval      string `yaml:"interval"`
	Timeout       string `yaml:"timeout"`
	FailThreshold int    `yaml:"fail_threshold"`
}

type ProjectConfig struct {
	ID     string      `yaml:"id"`
	APIKey string      `yaml:"api_key"`
	Origin string      `yaml:"origin"`
	Hosts  []string    `yaml:"hosts"`
	Cache  CacheConfig `yaml:"cache"`
}

type CacheConfig struct {
	Policy               string      `yaml:"policy"`
	DefaultTTL           int         `yaml:"default_ttl"`
	MaxSizeMB            int         `yaml:"max_size_mb"`
	StaleWhileRevalidate int         `yaml:"stale_while_revalidate"`
	Rules                []CacheRule `yaml:"rules"`
}

type CacheRule struct {
	Path string `yaml:"path"`
	TTL  int    `yaml:"ttl"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}

func ValidateConfig(cfg *Config) error {
	if cfg.Node.ID == "" {
		return fmt.Errorf("node.id is required")
	}

	// Cluster validation
	if len(cfg.Cluster.Nodes) == 0 {
		return fmt.Errorf("cluster.nodes must have at least 1 entry")
	}

	seenNodeIDs := make(map[string]bool)
	selfInCluster := false

	for _, node := range cfg.Cluster.Nodes {
		if seenNodeIDs[node.ID] {
			return fmt.Errorf("duplicate node ID %q in cluster.nodes", node.ID)
		}
		seenNodeIDs[node.ID] = true

		if node.Lat < -90 || node.Lat > 90 {
			return fmt.Errorf("cluster node %q: lat %g is out of range [-90, 90]", node.ID, node.Lat)
		}
		if node.Lng < -180 || node.Lng > 180 {
			return fmt.Errorf("cluster node %q: lng %g is out of range [-180, 180]", node.ID, node.Lng)
		}

		if node.ID == cfg.Node.ID {
			selfInCluster = true
		}
	}

	if !selfInCluster {
		return fmt.Errorf("node.id %q must be present in cluster.nodes", cfg.Node.ID)
	}

	// Project validation
	validPolicies := []string{"sieve", "w-tinylfu", "lru"}
	seenHosts := make(map[string]bool)

	for _, proj := range cfg.Projects {
		if proj.Origin == "" {
			return fmt.Errorf("project %q: origin is required", proj.ID)
		}

		if proj.Cache.Policy != "" && !slices.Contains(validPolicies, proj.Cache.Policy) {
			return fmt.Errorf("project %q: invalid cache policy %q", proj.ID, proj.Cache.Policy)
		}

		for _, host := range proj.Hosts {
			if seenHosts[host] {
				return fmt.Errorf("host %q is registered in multiple projects", host)
			}
			seenHosts[host] = true
		}
	}

	return nil
}
