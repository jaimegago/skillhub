package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jaimegago/skillhub/internal/config"
)

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(testdataPath(name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func serveFixture(t *testing.T, name string) (*httptest.Server, config.MarketplaceSource) {
	t.Helper()
	body := mustReadFixture(t, name)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, config.MarketplaceSource{URL: srv.URL, GitHostType: "generic"}
}

func TestFetch_HappyPath(t *testing.T) {
	srv, src := serveFixture(t, "valid.json")
	_ = srv
	result := Fetch(context.Background(), src, t.TempDir(), false)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Cached {
		t.Error("expected Cached=false on first fetch")
	}
	if result.Index == nil {
		t.Fatal("Index is nil")
	}
	if result.Index.Name != "test-marketplace" {
		t.Errorf("Name = %q, want %q", result.Index.Name, "test-marketplace")
	}
	if len(result.Index.Plugins) != 2 {
		t.Errorf("Plugins count = %d, want 2", len(result.Index.Plugins))
	}
	p := result.Index.Plugins[0]
	if p.Name != "full-plugin" {
		t.Errorf("plugin[0].Name = %q, want %q", p.Name, "full-plugin")
	}
	if p.Author == nil || p.Author.Name != "Plugin Author" {
		t.Error("plugin[0].Author not parsed correctly")
	}
	if p.Category != "development" {
		t.Errorf("plugin[0].Category = %q, want %q", p.Category, "development")
	}
}

func TestFetch_CacheHit(t *testing.T) {
	calls := 0
	body := mustReadFixture(t, "valid.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	src := config.MarketplaceSource{URL: srv.URL, GitHostType: "generic"}
	cacheDir := t.TempDir()

	// First fetch populates cache.
	r1 := Fetch(context.Background(), src, cacheDir, false)
	if r1.Error != "" {
		t.Fatalf("first fetch error: %s", r1.Error)
	}

	// Second fetch should hit cache, not call the server.
	r2 := Fetch(context.Background(), src, cacheDir, false)
	if r2.Error != "" {
		t.Fatalf("second fetch error: %s", r2.Error)
	}
	if !r2.Cached {
		t.Error("expected Cached=true on second fetch")
	}
	if calls != 1 {
		t.Errorf("server called %d times, want 1", calls)
	}
}

func TestFetch_RefreshBypassesCache(t *testing.T) {
	calls := 0
	body := mustReadFixture(t, "valid.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	src := config.MarketplaceSource{URL: srv.URL, GitHostType: "generic"}
	cacheDir := t.TempDir()

	Fetch(context.Background(), src, cacheDir, false)
	r2 := Fetch(context.Background(), src, cacheDir, true)

	if r2.Cached {
		t.Error("expected Cached=false when refresh=true")
	}
	if calls != 2 {
		t.Errorf("server called %d times, want 2", calls)
	}
}

func TestFetch_FetchErrorNoCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	src := config.MarketplaceSource{URL: srv.URL, GitHostType: "generic"}

	result := Fetch(context.Background(), src, t.TempDir(), false)

	if result.Error == "" {
		t.Error("expected non-empty Error on fetch failure")
	}
	if result.Index != nil {
		t.Error("expected nil Index on fetch failure with no cache")
	}
}

func TestFetch_FetchErrorStaleCache(t *testing.T) {
	// Serve valid.json first to populate cache, then switch to 404.
	body := mustReadFixture(t, "valid.json")
	serveOK := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if serveOK {
			_, _ = w.Write(body)
		} else {
			http.Error(w, "gone", http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(srv.Close)
	src := config.MarketplaceSource{URL: srv.URL, GitHostType: "generic"}
	cacheDir := t.TempDir()

	// Populate cache.
	Fetch(context.Background(), src, cacheDir, false)

	// Expire the cache by backdating meta.json.
	key := sourceKey(src.URL)
	metaPath := filepath.Join(cacheDir, "marketplaces", key, "meta.json")
	expired := cacheMeta{FetchedAt: time.Now().Add(-2 * cacheTTL)}
	metaData, _ := json.Marshal(expired)
	if err := os.WriteFile(metaPath, metaData, 0o644); err != nil {
		t.Fatalf("backdate meta: %v", err)
	}

	// Now make the server fail.
	serveOK = false
	result := Fetch(context.Background(), src, cacheDir, false)

	if result.Index == nil {
		t.Fatal("expected stale Index on fetch failure with expired cache")
	}
	if !result.Cached {
		t.Error("expected Cached=true when using stale fallback")
	}
	if result.Error == "" {
		t.Error("expected non-empty Error when using stale fallback")
	}
}

func TestFetch_MalformedJSON(t *testing.T) {
	body := mustReadFixture(t, "malformed.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	src := config.MarketplaceSource{URL: srv.URL, GitHostType: "generic"}

	result := Fetch(context.Background(), src, t.TempDir(), false)

	if result.Error == "" {
		t.Error("expected error on malformed JSON")
	}
	if result.Index != nil {
		t.Error("expected nil Index on parse failure")
	}
}

func TestFetch_OfficialSnapshot(t *testing.T) {
	srv, src := serveFixture(t, "official_snapshot.json")
	_ = srv
	result := Fetch(context.Background(), src, t.TempDir(), false)

	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Index == nil {
		t.Fatal("Index is nil")
	}
	if result.Index.Name != "claude-plugins-official" {
		t.Errorf("Name = %q", result.Index.Name)
	}
	if len(result.Index.Plugins) != 5 {
		t.Errorf("Plugins count = %d, want 5", len(result.Index.Plugins))
	}

	byName := make(map[string]PluginEntry, len(result.Index.Plugins))
	for _, p := range result.Index.Plugins {
		byName[p.Name] = p
	}

	// adlc: url source, no author, has category
	adlc := byName["adlc"]
	if adlc.Category != "development" {
		t.Errorf("adlc.Category = %q", adlc.Category)
	}
	if adlc.Author != nil {
		t.Error("adlc should have no author")
	}

	// agent-sdk-dev: relative-path source, has author
	sdk := byName["agent-sdk-dev"]
	if sdk.Author == nil || sdk.Author.Name != "Anthropic" {
		t.Errorf("agent-sdk-dev.Author = %v", sdk.Author)
	}

	// ai-firstify: git-subdir source, no category
	aif := byName["ai-firstify"]
	if aif.Category != "" {
		t.Errorf("ai-firstify.Category should be empty, got %q", aif.Category)
	}
	if len(aif.Source) == 0 {
		t.Error("ai-firstify.Source should be non-empty raw JSON")
	}
}

func TestRawURL_GitHub(t *testing.T) {
	src := config.MarketplaceSource{
		URL:         "https://github.com/acme/plugins",
		GitHostType: "github",
		Ref:         "stable",
	}
	got := rawURL(src)
	want := "https://raw.githubusercontent.com/acme/plugins/stable/.claude-plugin/marketplace.json"
	if got != want {
		t.Errorf("rawURL = %q, want %q", got, want)
	}
}

func TestRawURL_GitHubDefaultRef(t *testing.T) {
	src := config.MarketplaceSource{
		URL:         "https://github.com/acme/plugins",
		GitHostType: "github",
	}
	got := rawURL(src)
	want := "https://raw.githubusercontent.com/acme/plugins/main/.claude-plugin/marketplace.json"
	if got != want {
		t.Errorf("rawURL = %q, want %q", got, want)
	}
}

func TestRawURL_GitLab(t *testing.T) {
	src := config.MarketplaceSource{
		URL:         "https://gitlab.com/acme/plugins",
		GitHostType: "gitlab",
		Ref:         "develop",
	}
	got := rawURL(src)
	want := "https://gitlab.com/acme/plugins/-/raw/develop/.claude-plugin/marketplace.json"
	if got != want {
		t.Errorf("rawURL = %q, want %q", got, want)
	}
}

func TestRawURL_Generic(t *testing.T) {
	src := config.MarketplaceSource{
		URL:         "https://internal.example.com/path/to/marketplace.json",
		GitHostType: "generic",
	}
	got := rawURL(src)
	want := "https://internal.example.com/path/to/marketplace.json"
	if got != want {
		t.Errorf("rawURL = %q, want %q", got, want)
	}
}
