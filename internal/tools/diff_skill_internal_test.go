package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/fetch"
)

// fakeDiffFetcher is a test double for TreeFetcher that serves a caller-supplied
// directory as the upstream tree.
type fakeDiffFetcher struct {
	dir string
	sha string
	err error
}

func (f *fakeDiffFetcher) FetchPluginTree(_ context.Context, _ fetch.PluginSource, _ string, _ bool) (string, string, error) {
	return f.dir, f.sha, f.err
}

// newDiffHandler constructs a diffSkillHandler backed by the given fetcher.
func newDiffHandler(fetcher TreeFetcher) *diffSkillHandler {
	return &diffSkillHandler{fetcher: fetcher}
}

// callDiffHandler calls h.handle and returns the raw result plus the typed output.
func callDiffHandler(t *testing.T, h *diffSkillHandler, input DiffSkillInput) (*mcp.CallToolResult, DiffSkillOutput) {
	t.Helper()
	res, out, err := h.handle(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	return res, out
}

// makeUpstreamTree creates an upstream tree directory under a temp root with the
// given skills map (skillName → filename → content).
func makeUpstreamTree(t *testing.T, skills map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for skillName, files := range skills {
		for rel, content := range files {
			full := filepath.Join(root, "skills", skillName, rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
			}
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s: %v", full, err)
			}
		}
	}
	return root
}

// serveDiffMarketplace starts an httptest server that returns a marketplace index
// with a single github plugin entry.
func serveDiffMarketplace(t *testing.T, mktName, pluginName string) *httptest.Server {
	t.Helper()
	body := `{"name":"` + mktName + `","owner":{"name":"Test"},` +
		`"plugins":[{"name":"` + pluginName + `",` +
		`"source":{"source":"github","repo":"test/` + pluginName + `"}}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// withDiffEnvConfig writes a config.yaml pointing at the given marketplace URL and
// sets CLAUDE_PLUGIN_DATA so config.Load() picks it up.
func withDiffEnvConfig(t *testing.T, mktURL string) {
	t.Helper()
	dir := t.TempDir()
	yaml := "marketplaceSources:\n  - url: " + mktURL + "\n    gitHostType: generic\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("CLAUDE_PLUGIN_DATA", dir)
}

// --- tests ---

func TestDiffSkill_UpToDate(t *testing.T) {
	content := "line1\nline2\n"
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": content}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": content}})

	srv := serveDiffMarketplace(t, "alpha", "myplugin")
	withDiffEnvConfig(t, srv.URL)

	h := newDiffHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "abc123"})
	res, out := callDiffHandler(t, h, DiffSkillInput{
		PluginPath: localRoot,
		Skill:      "my-skill",
	})

	if res != nil {
		b, _ := json.Marshal(res.Content)
		t.Fatalf("expected success result, got error: %s", b)
	}
	if out.Status != "up-to-date" {
		t.Errorf("expected status=up-to-date, got %q", out.Status)
	}
	if out.Diff != "" {
		t.Errorf("expected empty diff for identical files, got: %s", out.Diff)
	}
	if out.CommitSHA != "abc123" {
		t.Errorf("expected commit_sha=abc123, got %q", out.CommitSHA)
	}
}

func TestDiffSkill_Drifted(t *testing.T) {
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local version\n"}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream version\n"}})

	srv := serveDiffMarketplace(t, "alpha", "myplugin")
	withDiffEnvConfig(t, srv.URL)

	h := newDiffHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "def456"})
	res, out := callDiffHandler(t, h, DiffSkillInput{
		PluginPath: localRoot,
		Skill:      "my-skill",
	})

	if res != nil {
		b, _ := json.Marshal(res.Content)
		t.Fatalf("expected success result, got error: %s", b)
	}
	if out.Status != "drifted" {
		t.Errorf("expected status=drifted, got %q", out.Status)
	}
	if out.Diff == "" {
		t.Error("expected non-empty diff for changed file")
	}
	if !strings.Contains(out.Diff, "-upstream version") {
		t.Errorf("diff should contain removed upstream line, got:\n%s", out.Diff)
	}
	if !strings.Contains(out.Diff, "+local version") {
		t.Errorf("diff should contain added local line, got:\n%s", out.Diff)
	}
}

func TestDiffSkill_MissingLocal(t *testing.T) {
	// Local plugin has no skill directory.
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream\n"}})

	srv := serveDiffMarketplace(t, "alpha", "myplugin")
	withDiffEnvConfig(t, srv.URL)

	h := newDiffHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha1"})
	_, out := callDiffHandler(t, h, DiffSkillInput{PluginPath: localRoot, Skill: "my-skill"})

	if out.Status != "missing-local" {
		t.Errorf("expected missing-local, got %q", out.Status)
	}
}

func TestDiffSkill_MissingUpstream(t *testing.T) {
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local\n"}},
	)
	// Upstream tree has no skill directory for "my-skill".
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{})

	srv := serveDiffMarketplace(t, "alpha", "myplugin")
	withDiffEnvConfig(t, srv.URL)

	h := newDiffHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha2"})
	_, out := callDiffHandler(t, h, DiffSkillInput{PluginPath: localRoot, Skill: "my-skill"})

	if out.Status != "missing-upstream" {
		t.Errorf("expected missing-upstream, got %q", out.Status)
	}
}

func TestDiffSkill_MissingUpstreamDecl(t *testing.T) {
	// No x-skillhub-upstream and no overrides → missing-upstream.
	localRoot := makePlugin(t, `{"name":"myplugin"}`, map[string]map[string]string{})
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())

	h := newDiffHandler(&fakeDiffFetcher{})
	_, out := callDiffHandler(t, h, DiffSkillInput{PluginPath: localRoot, Skill: "my-skill"})

	if out.Status != "missing-upstream" {
		t.Errorf("expected missing-upstream for no decl, got %q", out.Status)
	}
}

func TestDiffSkill_Pagination(t *testing.T) {
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local content\n"}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream content\n"}})

	srv := serveDiffMarketplace(t, "alpha", "myplugin")
	withDiffEnvConfig(t, srv.URL)

	h := newDiffHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha3"})

	// First page: 3 lines.
	_, out1 := callDiffHandler(t, h, DiffSkillInput{
		PluginPath: localRoot,
		Skill:      "my-skill",
		Limit:      3,
	})
	if out1.Status != "drifted" {
		t.Fatalf("expected drifted, got %q", out1.Status)
	}
	if out1.TotalLines <= 3 {
		t.Fatalf("expected >3 total lines for a real diff, got %d", out1.TotalLines)
	}
	if out1.NextCursor == "" {
		t.Error("expected NextCursor to be set on first page")
	}
	if !out1.Truncated {
		t.Error("expected Truncated=true on first page")
	}

	// Second page using the cursor.
	_, out2 := callDiffHandler(t, h, DiffSkillInput{
		PluginPath: localRoot,
		Skill:      "my-skill",
		Limit:      3,
		Cursor:     out1.NextCursor,
	})
	if out1.Diff == out2.Diff {
		t.Error("expected different content on second page")
	}
}

func TestDiffSkill_PaginationAllLines(t *testing.T) {
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local\n"}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream\n"}})

	srv := serveDiffMarketplace(t, "alpha", "myplugin")
	withDiffEnvConfig(t, srv.URL)

	h := newDiffHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha4"})

	// limit=0 returns all lines with no truncation.
	_, out := callDiffHandler(t, h, DiffSkillInput{
		PluginPath: localRoot,
		Skill:      "my-skill",
		Limit:      0,
	})
	if out.Truncated {
		t.Error("expected Truncated=false for limit=0")
	}
	if out.NextCursor != "" {
		t.Errorf("expected no NextCursor for limit=0, got %q", out.NextCursor)
	}
}

func TestDiffSkill_DiffContainsRelativePaths(t *testing.T) {
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local\n"}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream\n"}})

	srv := serveDiffMarketplace(t, "alpha", "myplugin")
	withDiffEnvConfig(t, srv.URL)

	h := newDiffHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha5"})
	_, out := callDiffHandler(t, h, DiffSkillInput{PluginPath: localRoot, Skill: "my-skill"})

	// Absolute paths should be replaced with human-readable labels.
	if strings.Contains(out.Diff, localRoot) {
		t.Errorf("diff contains absolute local path %q — should be replaced with label", localRoot)
	}
	if strings.Contains(out.Diff, upstreamRoot) {
		t.Errorf("diff contains absolute upstream path %q — should be replaced with label", upstreamRoot)
	}
	if !strings.Contains(out.Diff, "local/") {
		t.Errorf("expected diff to contain 'local/' label, got:\n%s", out.Diff)
	}
	if !strings.Contains(out.Diff, "upstream/") {
		t.Errorf("expected diff to contain 'upstream/' label, got:\n%s", out.Diff)
	}
}

func TestDiffSkill_WithOverrides(t *testing.T) {
	// No x-skillhub-upstream in manifest; use explicit marketplace+plugin overrides.
	localRoot := makePlugin(t,
		`{"name":"myplugin"}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local\n"}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream\n"}})

	srv := serveDiffMarketplace(t, "alpha", "myplugin")
	withDiffEnvConfig(t, srv.URL)

	h := newDiffHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha6"})
	_, out := callDiffHandler(t, h, DiffSkillInput{
		PluginPath:  localRoot,
		Skill:       "my-skill",
		Marketplace: "alpha",
		Plugin:      "myplugin",
	})

	if out.Status != "drifted" {
		t.Errorf("expected drifted with overrides, got %q", out.Status)
	}
}
