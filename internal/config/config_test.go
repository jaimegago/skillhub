package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPathPluginMode(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", "/tmp/plugin-data")
	got := configPath()
	want := "/tmp/plugin-data/config.yaml"
	if got != want {
		t.Errorf("configPath() = %q, want %q", got, want)
	}
}

func TestConfigPathXDG(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", "")
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
	got := configPath()
	want := "/tmp/xdg-config/skillhub/config.yaml"
	if got != want {
		t.Errorf("configPath() = %q, want %q", got, want)
	}
}

func TestConfigPathFallback(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	got := configPath()
	want := filepath.Join(home, ".config", "skillhub", "config.yaml")
	if got != want {
		t.Errorf("configPath() = %q, want %q", got, want)
	}
}

func TestCacheDirPluginMode(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", "/tmp/plugin-data")
	got := CacheDir()
	want := "/tmp/plugin-data/cache"
	if got != want {
		t.Errorf("CacheDir() = %q, want %q", got, want)
	}
}

func TestCacheDirXDG(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", "")
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-cache")
	got := CacheDir()
	want := "/tmp/xdg-cache/skillhub"
	if got != want {
		t.Errorf("CacheDir() = %q, want %q", got, want)
	}
}

func TestCacheDirFallback(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", "")
	t.Setenv("XDG_CACHE_HOME", "")
	home, _ := os.UserHomeDir()
	got := CacheDir()
	want := filepath.Join(home, ".cache", "skillhub")
	if got != want {
		t.Errorf("CacheDir() = %q, want %q", got, want)
	}
}
