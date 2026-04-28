package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/tools"
)

func callSearchPlugins(t *testing.T, input tools.SearchPluginsInput) tools.SearchPluginsOutput {
	t.Helper()
	res, out, err := tools.HandleSearchPlugins(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if res != nil {
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				t.Fatalf("handler returned error result: %s", tc.Text)
			}
		}
		t.Fatal("handler returned non-nil result (error path)")
	}
	return out
}

func callSearchPluginsExpectError(t *testing.T, input tools.SearchPluginsInput) string {
	t.Helper()
	res, _, err := tools.HandleSearchPlugins(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("handler returned unexpected Go error: %v", err)
	}
	if res == nil {
		t.Fatal("expected error result, got nil")
	}
	if len(res.Content) == 0 {
		t.Fatal("error result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("error result content is not TextContent")
	}
	return tc.Text
}

func TestSearchPlugins_EmptyQueryReturnsError(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())

	text := callSearchPluginsExpectError(t, tools.SearchPluginsInput{Query: ""})
	if !strings.Contains(text, "INVALID_INPUT") {
		t.Errorf("expected INVALID_INPUT error, got: %s", text)
	}
}

func TestSearchPlugins_WhitespaceQueryReturnsError(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())

	text := callSearchPluginsExpectError(t, tools.SearchPluginsInput{Query: "   "})
	if !strings.Contains(text, "INVALID_INPUT") {
		t.Errorf("expected INVALID_INPUT error, got: %s", text)
	}
}

func TestSearchPlugins_NoConfig(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())

	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "anything"})

	if len(out.Plugins) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(out.Plugins))
	}
	if out.Query != "anything" {
		t.Errorf("expected Query echoed back, got %q", out.Query)
	}
}

func TestSearchPlugins_MatchByName(t *testing.T) {
	srv := serveListFixture(t, listFixtureA)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "alpha-one"})

	if len(out.Plugins) != 1 {
		t.Fatalf("expected 1 match, got %d", len(out.Plugins))
	}
	if out.Plugins[0].Name != "alpha-one" {
		t.Errorf("expected alpha-one, got %s", out.Plugins[0].Name)
	}
}

func TestSearchPlugins_MatchByDescription(t *testing.T) {
	srv := serveListFixture(t, listFixtureA)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	// "Second" only appears in alpha-two's description.
	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "second"})

	if len(out.Plugins) != 1 {
		t.Fatalf("expected 1 match, got %d", len(out.Plugins))
	}
	if out.Plugins[0].Name != "alpha-two" {
		t.Errorf("expected alpha-two, got %s", out.Plugins[0].Name)
	}
}

func TestSearchPlugins_CaseInsensitive(t *testing.T) {
	srv := serveListFixture(t, listFixtureA)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "ALPHA"})

	if len(out.Plugins) != 2 {
		t.Errorf("expected 2 matches for 'ALPHA', got %d", len(out.Plugins))
	}
}

func TestSearchPlugins_MatchAcrossSources(t *testing.T) {
	srvA := serveListFixture(t, listFixtureA)
	srvB := serveListFixture(t, listFixtureB)

	withEnvConfig(t, "marketplaceSources:\n"+
		"  - url: "+srvA.URL+"\n    gitHostType: generic\n"+
		"  - url: "+srvB.URL+"\n    gitHostType: generic\n")

	// "plugin" appears in every description.
	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "plugin"})

	if len(out.Plugins) != 3 {
		t.Errorf("expected 3 matches across both sources, got %d", len(out.Plugins))
	}
}

func TestSearchPlugins_NoMatch(t *testing.T) {
	srv := serveListFixture(t, listFixtureA)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "zzznomatch"})

	if len(out.Plugins) != 0 {
		t.Errorf("expected 0 matches, got %d", len(out.Plugins))
	}
	if out.Total != 0 {
		t.Errorf("expected Total=0, got %d", out.Total)
	}
	if out.Truncated {
		t.Error("expected Truncated=false")
	}
}

func TestSearchPlugins_MarketplaceFilter(t *testing.T) {
	srvA := serveListFixture(t, listFixtureA)
	srvB := serveListFixture(t, listFixtureB)

	withEnvConfig(t, "marketplaceSources:\n"+
		"  - url: "+srvA.URL+"\n    gitHostType: generic\n"+
		"  - url: "+srvB.URL+"\n    gitHostType: generic\n")

	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "plugin", Marketplace: "beta"})

	if len(out.Plugins) != 1 {
		t.Fatalf("expected 1 match in beta only, got %d", len(out.Plugins))
	}
	if out.Plugins[0].Marketplace != "beta" {
		t.Errorf("expected marketplace=beta, got %s", out.Plugins[0].Marketplace)
	}
}

func TestSearchPlugins_LimitAndTruncation(t *testing.T) {
	srv := serveListFixture(t, buildLargeFixture("big", 60))
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	// All 60 plugins contain "plugin" in their names.
	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "plugin", Limit: 10})

	if len(out.Plugins) != 10 {
		t.Errorf("expected 10 results (limit), got %d", len(out.Plugins))
	}
	if !out.Truncated {
		t.Error("expected Truncated=true")
	}
	if out.Total != 60 {
		t.Errorf("expected Total=60, got %d", out.Total)
	}
}

func TestSearchPlugins_DefaultLimit(t *testing.T) {
	srv := serveListFixture(t, buildLargeFixture("big", 60))
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "plugin"})

	if len(out.Plugins) != 50 {
		t.Errorf("expected 50 results (default limit), got %d", len(out.Plugins))
	}
	if !out.Truncated {
		t.Error("expected Truncated=true")
	}
}

func TestSearchPlugins_QueryEchoedInOutput(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())

	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "myterm"})

	if out.Query != "myterm" {
		t.Errorf("expected Query=%q, got %q", "myterm", out.Query)
	}
}

func TestSearchPlugins_MatchByCategory(t *testing.T) {
	const fixture = `{
  "name": "cattest",
  "owner": {"name": "Test"},
  "plugins": [
    {"name": "tool-a", "description": "A tool", "category": "devtools",
     "source": {"source": "github", "repo": "t/a"}},
    {"name": "tool-b", "description": "Another tool", "category": "productivity",
     "source": {"source": "github", "repo": "t/b"}}
  ]
}`
	srv := serveListFixture(t, fixture)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "devtools"})

	if len(out.Plugins) != 1 {
		t.Fatalf("expected 1 match on category, got %d", len(out.Plugins))
	}
	if out.Plugins[0].Name != "tool-a" {
		t.Errorf("expected tool-a, got %s", out.Plugins[0].Name)
	}
}

func TestSearchPlugins_MatchByKeyword(t *testing.T) {
	const fixture = `{
  "name": "kwtest",
  "owner": {"name": "Test"},
  "plugins": [
    {"name": "kw-a", "description": "Has keywords", "keywords": ["linting", "format"],
     "source": {"source": "github", "repo": "t/a"}},
    {"name": "kw-b", "description": "No keywords",
     "source": {"source": "github", "repo": "t/b"}}
  ]
}`
	srv := serveListFixture(t, fixture)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callSearchPlugins(t, tools.SearchPluginsInput{Query: "linting"})

	if len(out.Plugins) != 1 {
		t.Fatalf("expected 1 match on keyword, got %d", len(out.Plugins))
	}
	if out.Plugins[0].Name != "kw-a" {
		t.Errorf("expected kw-a, got %s", out.Plugins[0].Name)
	}
}
