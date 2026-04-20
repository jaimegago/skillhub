// Package config handles loading and validating skillhub configuration.
package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// MarketplaceSource describes one configured plugin registry.
type MarketplaceSource struct {
	URL              string `yaml:"url"`
	GitHostType      string `yaml:"gitHostType"`      // github | gitlab | generic
	Ref              string `yaml:"ref"`              // branch/tag; defaults to "main" when empty
	CredentialEnvVar string `yaml:"credentialEnvVar"` // env var read at invocation; never stored
}

// Config holds skillhub's runtime configuration.
type Config struct {
	MarketplaceSources []MarketplaceSource `yaml:"marketplaceSources"`
}

// configPath returns the path to config.yaml.
//
// Plugin mode (CLAUDE_PLUGIN_DATA set): ${CLAUDE_PLUGIN_DATA}/config.yaml
// Standalone: ${XDG_CONFIG_HOME}/skillhub/config.yaml
//
//	(fallback: ~/.config/skillhub/config.yaml)
func configPath() string {
	if d := os.Getenv("CLAUDE_PLUGIN_DATA"); d != "" {
		return filepath.Join(d, "config.yaml")
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "skillhub", "config.yaml")
}

// CacheDir returns the directory for transient cached data.
//
// Plugin mode (CLAUDE_PLUGIN_DATA set): ${CLAUDE_PLUGIN_DATA}/cache/
// Standalone: ${XDG_CACHE_HOME}/skillhub/
//
//	(fallback: ~/.cache/skillhub/)
func CacheDir() string {
	if d := os.Getenv("CLAUDE_PLUGIN_DATA"); d != "" {
		return filepath.Join(d, "cache")
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "skillhub")
}

// Load reads the config file from the resolved path. Returns an empty Config
// if the file does not exist — that is valid (no marketplace sources configured).
func Load() (*Config, error) {
	path := configPath()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
