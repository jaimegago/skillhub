package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/tools"
)

func callRecommend(t *testing.T, input tools.RecommendPluginsInput) tools.RecommendPluginsOutput {
	t.Helper()
	res, out, err := tools.HandleRecommendPlugins(context.Background(), &mcp.CallToolRequest{}, input)
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

func callRecommendExpectError(t *testing.T, input tools.RecommendPluginsInput) string {
	t.Helper()
	res, _, err := tools.HandleRecommendPlugins(context.Background(), &mcp.CallToolRequest{}, input)
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

func TestRecommendPlugins_EmptyContextReturnsError(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())

	text := callRecommendExpectError(t, tools.RecommendPluginsInput{Context: ""})
	if !strings.Contains(text, "INVALID_INPUT") {
		t.Errorf("expected INVALID_INPUT, got: %s", text)
	}
}

func TestRecommendPlugins_OnlyStopWordsReturnsError(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())

	text := callRecommendExpectError(t, tools.RecommendPluginsInput{Context: "the a an is"})
	if !strings.Contains(text, "INVALID_INPUT") {
		t.Errorf("expected INVALID_INPUT for stop-word-only context, got: %s", text)
	}
}

func TestRecommendPlugins_NoConfig(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())

	out := callRecommend(t, tools.RecommendPluginsInput{Context: "linting code quality"})

	if len(out.Recommendations) != 0 {
		t.Errorf("expected 0 recommendations, got %d", len(out.Recommendations))
	}
	if out.Context != "linting code quality" {
		t.Errorf("expected Context echoed back, got %q", out.Context)
	}
}

func TestRecommendPlugins_RelevantMatchRankedFirst(t *testing.T) {
	const fixture = `{
  "name": "alpha",
  "owner": {"name": "Test"},
  "plugins": [
    {"name": "lint-helper", "description": "Linting and code quality tool",
     "keywords": ["linting","quality"], "source": {"source":"github","repo":"t/a"}},
    {"name": "image-tool", "description": "Resize and crop images",
     "source": {"source":"github","repo":"t/b"}},
    {"name": "format-tool", "description": "Code formatting and linting",
     "source": {"source":"github","repo":"t/c"}}
  ]
}`
	srv := serveListFixture(t, fixture)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callRecommend(t, tools.RecommendPluginsInput{Context: "linting code quality"})

	if len(out.Recommendations) == 0 {
		t.Fatal("expected at least 1 recommendation")
	}
	// lint-helper should score higher than format-tool since it matches more query tokens.
	if out.Recommendations[0].Name == "image-tool" {
		t.Errorf("image-tool should not be the top recommendation for a linting query")
	}
	// image-tool has no overlap — must be excluded.
	for _, r := range out.Recommendations {
		if r.Name == "image-tool" {
			t.Errorf("image-tool should have score 0 and be excluded from results")
		}
	}
}

func TestRecommendPlugins_ZeroScoreExcluded(t *testing.T) {
	const fixture = `{
  "name": "alpha",
  "owner": {"name": "Test"},
  "plugins": [
    {"name": "unrelated", "description": "Something completely different",
     "source": {"source":"github","repo":"t/a"}}
  ]
}`
	srv := serveListFixture(t, fixture)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callRecommend(t, tools.RecommendPluginsInput{Context: "linting formatter"})

	if len(out.Recommendations) != 0 {
		t.Errorf("expected 0 recommendations for no-overlap query, got %d", len(out.Recommendations))
	}
}

func TestRecommendPlugins_ScoreDecreasingOrder(t *testing.T) {
	// alpha-linter matches all 3 query tokens (linting, alpha, code) → highest score.
	// beta-formatter matches 2 (linting, code) → lower score.
	// unrelated matches 0 → excluded.
	const fixture = `{
  "name": "alpha",
  "owner": {"name": "Test"},
  "plugins": [
    {"name": "alpha-linter", "description": "Linting for alpha code projects",
     "keywords": ["linting","alpha"], "source": {"source":"github","repo":"t/a"}},
    {"name": "beta-formatter", "description": "Code linting formatter",
     "source": {"source":"github","repo":"t/b"}},
    {"name": "unrelated", "description": "Something else entirely",
     "source": {"source":"github","repo":"t/c"}}
  ]
}`
	srv := serveListFixture(t, fixture)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callRecommend(t, tools.RecommendPluginsInput{Context: "linting alpha code"})

	if len(out.Recommendations) < 2 {
		t.Fatalf("expected at least 2 recommendations, got %d", len(out.Recommendations))
	}
	for i := 1; i < len(out.Recommendations); i++ {
		if out.Recommendations[i].Score > out.Recommendations[i-1].Score {
			t.Errorf("recommendations not sorted by score: position %d (%.3f) > position %d (%.3f)",
				i, out.Recommendations[i].Score, i-1, out.Recommendations[i-1].Score)
		}
	}
}

func TestRecommendPlugins_RationalePresent(t *testing.T) {
	const fixture = `{
  "name": "alpha",
  "owner": {"name": "Test"},
  "plugins": [
    {"name": "go-linter", "description": "Linting for Go projects",
     "source": {"source":"github","repo":"t/a"}}
  ]
}`
	srv := serveListFixture(t, fixture)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callRecommend(t, tools.RecommendPluginsInput{Context: "go linting"})

	if len(out.Recommendations) == 0 {
		t.Fatal("expected a recommendation")
	}
	if out.Recommendations[0].Rationale == "" {
		t.Error("expected non-empty Rationale")
	}
}

func TestRecommendPlugins_DefaultLimit(t *testing.T) {
	srv := serveListFixture(t, buildLargeFixture("big", 60))
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	// All 60 plugins have "plugin" in their name and description.
	out := callRecommend(t, tools.RecommendPluginsInput{Context: "plugin"})

	if len(out.Recommendations) != 10 {
		t.Errorf("expected 10 (default limit), got %d", len(out.Recommendations))
	}
	if !out.Truncated {
		t.Error("expected Truncated=true")
	}
	if out.Total != 60 {
		t.Errorf("expected Total=60, got %d", out.Total)
	}
}

func TestRecommendPlugins_ExplicitLimit(t *testing.T) {
	srv := serveListFixture(t, buildLargeFixture("big", 60))
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callRecommend(t, tools.RecommendPluginsInput{Context: "plugin", Limit: 5})

	if len(out.Recommendations) != 5 {
		t.Errorf("expected 5, got %d", len(out.Recommendations))
	}
}

func TestRecommendPlugins_MarketplaceFilter(t *testing.T) {
	srvA := serveListFixture(t, listFixtureA)
	srvB := serveListFixture(t, listFixtureB)

	withEnvConfig(t, "marketplaceSources:\n"+
		"  - url: "+srvA.URL+"\n    gitHostType: generic\n"+
		"  - url: "+srvB.URL+"\n    gitHostType: generic\n")

	out := callRecommend(t, tools.RecommendPluginsInput{Context: "plugin", Marketplace: "beta"})

	for _, r := range out.Recommendations {
		if r.Marketplace != "beta" {
			t.Errorf("expected all results from beta, got marketplace=%q for %s", r.Marketplace, r.Name)
		}
	}
}

func TestRecommendPlugins_MatchByKeyword(t *testing.T) {
	const fixture = `{
  "name": "kwtest",
  "owner": {"name": "Test"},
  "plugins": [
    {"name": "kw-tool", "description": "A general tool",
     "keywords": ["typescript","bundler"], "source": {"source":"github","repo":"t/a"}},
    {"name": "other-tool", "description": "Something else",
     "source": {"source":"github","repo":"t/b"}}
  ]
}`
	srv := serveListFixture(t, fixture)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	out := callRecommend(t, tools.RecommendPluginsInput{Context: "typescript bundler project"})

	if len(out.Recommendations) == 0 {
		t.Fatal("expected recommendation matching keywords")
	}
	if out.Recommendations[0].Name != "kw-tool" {
		t.Errorf("expected kw-tool first, got %s", out.Recommendations[0].Name)
	}
}
