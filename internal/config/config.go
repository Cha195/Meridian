package config

import (
	"fmt"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Node     NodeConfig       `yaml:"node"`
	Projects []ProjectConfig `yaml:"projects"`
}

type NodeConfig struct {
	ID      string `yaml:"id"`
	Listen  string `yaml:"listen"`
	GeoIPDB string `yaml:"geoip_db"`
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
