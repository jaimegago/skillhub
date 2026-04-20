// Package marketplace fetches and caches marketplace.json from configured sources.
package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jaime-gago/skillhub/internal/config"
)

const cacheTTL = time.Hour

// Index is the parsed contents of a marketplace.json file.
type Index struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Plugins     []PluginEntry `json:"plugins"`
}

// PluginEntry is one entry from the plugins array in marketplace.json.
// Source is retained as raw JSON for future tools that need to inspect it.
type PluginEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Version     string          `json:"version,omitempty"`
	Author      *Author         `json:"author,omitempty"`
	Homepage    string          `json:"homepage,omitempty"`
	Keywords    []string        `json:"keywords,omitempty"`
	Category    string          `json:"category,omitempty"`
	License     string          `json:"license,omitempty"`
	Source      json.RawMessage `json:"source,omitempty"`
}

// Author holds plugin author information.
type Author struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// FetchResult is returned by Fetch for one marketplace source.
// If Index is non-nil, data is available (possibly from cache).
// Error is non-empty when the remote fetch failed; Index may still be
// non-nil when a stale cached copy was used as a fallback.
type FetchResult struct {
	Index  *Index
	Cached bool
	Error  string
}

type cacheMeta struct {
	FetchedAt time.Time `json:"fetchedAt"`
}

// Fetch retrieves the marketplace index for src. cacheDir is the root cache
// directory from config.CacheDir(). When refresh is true the local cache is
// bypassed and a fresh fetch is always attempted.
func Fetch(ctx context.Context, src config.MarketplaceSource, cacheDir string, refresh bool) FetchResult {
	key := sourceKey(src.URL)
	dir := filepath.Join(cacheDir, "marketplaces", key)
	dataPath := filepath.Join(dir, "marketplace.json")
	metaPath := filepath.Join(dir, "meta.json")

	if !refresh && cacheIsFresh(metaPath) {
		if idx := loadCachedIndex(dataPath); idx != nil {
			return FetchResult{Index: idx, Cached: true}
		}
	}

	url := rawURL(src)
	idx, err := fetchRemote(ctx, url, src.CredentialEnvVar)
	if err != nil {
		if stale := loadCachedIndex(dataPath); stale != nil {
			return FetchResult{
				Index:  stale,
				Cached: true,
				Error:  fmt.Sprintf("fetch failed (using stale cache): %s", err.Error()),
			}
		}
		return FetchResult{Error: fmt.Sprintf("marketplace unreachable: %s", err.Error())}
	}

	writeCache(dir, dataPath, metaPath, idx) //nolint:errcheck
	return FetchResult{Index: idx}
}

// rawURL constructs the URL used to fetch marketplace.json from a source.
// github: transforms https://github.com/owner/repo → raw.githubusercontent.com URL.
// gitlab: transforms https://gitlab.com/owner/repo → GitLab raw file URL.
// generic (or unknown): uses src.URL directly as the full URL to marketplace.json.
// Per the Claude Code plugin-marketplace convention, marketplace.json lives under
// .claude-plugin/ in the repository root, so github and gitlab paths include that prefix.
func rawURL(src config.MarketplaceSource) string {
	ref := src.Ref
	if ref == "" {
		ref = "main"
	}
	u := strings.TrimSuffix(strings.TrimSpace(src.URL), "/")
	switch src.GitHostType {
	case "github":
		path := strings.TrimPrefix(u, "https://github.com/")
		return "https://raw.githubusercontent.com/" + path + "/" + ref + "/.claude-plugin/marketplace.json"
	case "gitlab":
		path := strings.TrimPrefix(u, "https://gitlab.com/")
		return "https://gitlab.com/" + path + "/-/raw/" + ref + "/.claude-plugin/marketplace.json"
	default:
		return u
	}
}

// sourceKey returns an 8-hex-char cache directory name derived from the URL.
func sourceKey(url string) string {
	h := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x", h[:4])
}

func cacheIsFresh(metaPath string) bool {
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return false
	}
	var m cacheMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	return time.Since(m.FetchedAt) <= cacheTTL
}

func loadCachedIndex(dataPath string) *Index {
	data, err := os.ReadFile(dataPath)
	if err != nil {
		return nil
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil
	}
	return &idx
}

func fetchRemote(ctx context.Context, url, credEnvVar string) (*Index, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if credEnvVar != "" {
		if token := os.Getenv(credEnvVar); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return &idx, nil
}

func writeCache(dir, dataPath, metaPath string, idx *Index) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dataPath, data, 0o644); err != nil {
		return err
	}
	meta := cacheMeta{FetchedAt: time.Now().UTC()}
	metaData, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, metaData, 0o644)
}
