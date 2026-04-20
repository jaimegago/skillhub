package tools_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaime-gago/skillhub/internal/tools"
)

// listFixture is a minimal marketplace.json payload for tool-level tests.
const listFixtureA = `{
  "name": "alpha",
  "owner": {"name": "Alpha Team"},
  "plugins": [
    {"name": "alpha-one", "description": "First alpha plugin", "category": "dev",
     "source": {"source": "github", "repo": "alpha/one"}},
    {"name": "alpha-two", "description": "Second alpha plugin",
     "author": {"name": "Alice"},
     "source": {"source": "github", "repo": "alpha/two"}}
  ]
}`

const listFixtureB = `{
  "name": "beta",
  "owner": {"name": "Beta Team"},
  "plugins": [
    {"name": "beta-one", "description": "Only beta plugin",
     "source": {"source": "url", "url": "https://example.com/beta.git"}}
  ]
}`

func serveListFixture(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func callListPlugins(t *testing.T, input tools.ListAvailablePluginsInput) tools.ListAvailablePluginsOutput {
	t.Helper()
	res, out, err := tools.HandleListAvailablePlugins(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if res != nil {
		// Handler returned an error result; surface the text for diagnosis.
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				t.Fatalf("handler returned error result: %s", tc.Text)
			}
		}
		t.Fatal("handler returned non-nil result (error path)")
	}
	return out
}

// withEnvConfig sets CLAUDE_PLUGIN_DATA to a temp directory and writes a
// config.yaml referencing the given marketplace URLs. It restores the env
// after the test.
func withEnvConfig(t *testing.T, yaml string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/config.yaml", []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("CLAUDE_PLUGIN_DATA", dir)
}

func TestListAvailablePlugins_NoConfig(t *testing.T) {
	// CLAUDE_PLUGIN_DATA points to empty dir → no config.yaml → empty result.
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())

	out := callListPlugins(t, tools.ListAvailablePluginsInput{})

	if len(out.Plugins) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(out.Plugins))
	}
	if len(out.Sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(out.Sources))
	}
}

func TestListAvailablePlugins_TwoSources(t *testing.T) {
	srvA := serveListFixture(t, listFixtureA)
	srvB := serveListFixture(t, listFixtureB)

	withEnvConfig(t, "marketplaceSources:\n"+
		"  - url: "+srvA.URL+"\n    gitHostType: generic\n"+
		"  - url: "+srvB.URL+"\n    gitHostType: generic\n")

	out := callListPlugins(t, tools.ListAvailablePluginsInput{})

	if len(out.Plugins) != 3 {
		t.Errorf("expected 3 plugins, got %d", len(out.Plugins))
	}
	if len(out.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(out.Sources))
	}

	// Verify alpha-two's author was flattened.
	for _, p := range out.Plugins {
		if p.Name == "alpha-two" && p.Author != "Alice" {
			t.Errorf("alpha-two.Author = %q, want %q", p.Author, "Alice")
		}
	}

	// Verify marketplace attribution.
	for _, p := range out.Plugins {
		switch p.Name {
		case "alpha-one", "alpha-two":
			if p.Marketplace != "alpha" {
				t.Errorf("%s.Marketplace = %q, want alpha", p.Name, p.Marketplace)
			}
		case "beta-one":
			if p.Marketplace != "beta" {
				t.Errorf("beta-one.Marketplace = %q, want beta", p.Marketplace)
			}
		}
	}
}

func TestListAvailablePlugins_FilterByName(t *testing.T) {
	srvA := serveListFixture(t, listFixtureA)
	srvB := serveListFixture(t, listFixtureB)

	withEnvConfig(t, "marketplaceSources:\n"+
		"  - url: "+srvA.URL+"\n    gitHostType: generic\n"+
		"  - url: "+srvB.URL+"\n    gitHostType: generic\n")

	out := callListPlugins(t, tools.ListAvailablePluginsInput{Marketplace: "beta"})

	if len(out.Plugins) != 1 {
		t.Errorf("expected 1 plugin for 'beta' filter, got %d", len(out.Plugins))
	}
	if len(out.Sources) != 1 {
		t.Errorf("expected 1 source for 'beta' filter, got %d", len(out.Sources))
	}
	if out.Sources[0].Name != "beta" {
		t.Errorf("source.Name = %q, want beta", out.Sources[0].Name)
	}
}

func TestListAvailablePlugins_PartialFailure(t *testing.T) {
	srvA := serveListFixture(t, listFixtureA)
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srvBad.Close)

	withEnvConfig(t, "marketplaceSources:\n"+
		"  - url: "+srvA.URL+"\n    gitHostType: generic\n"+
		"  - url: "+srvBad.URL+"\n    gitHostType: generic\n")

	out := callListPlugins(t, tools.ListAvailablePluginsInput{})

	// Plugins from alpha should still be present.
	if len(out.Plugins) != 2 {
		t.Errorf("expected 2 plugins (from working source), got %d", len(out.Plugins))
	}
	// Both sources should appear in the status list.
	if len(out.Sources) != 2 {
		t.Errorf("expected 2 source entries, got %d", len(out.Sources))
	}
	// The failing source should have a non-empty Error.
	errCount := 0
	for _, s := range out.Sources {
		if s.Error != "" {
			errCount++
		}
	}
	if errCount != 1 {
		t.Errorf("expected 1 source with error, got %d", errCount)
	}
}
