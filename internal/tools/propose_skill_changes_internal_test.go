package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeGitHubClient records calls made to it and returns configurable responses.
type fakeGitHubClient struct {
	user          string
	forkFullName  string
	defaultBranch string
	branchSHA     string
	prURL         string
	fileSHAs      map[string]string // path → SHA for existing files
	upsertedFiles map[string][]byte // path → content written
	createdBranch string
	createPRCalls int
	ghErr         error // if non-nil, all calls return this
}

func newFakeGH() *fakeGitHubClient {
	return &fakeGitHubClient{
		user:          "testuser",
		forkFullName:  "testuser/myplugin",
		defaultBranch: "main",
		branchSHA:     "abc123sha",
		prURL:         "https://github.com/upstream/myplugin/pull/1",
		fileSHAs:      map[string]string{},
		upsertedFiles: map[string][]byte{},
	}
}

func (f *fakeGitHubClient) GetUser(_ context.Context) (string, error) { return f.user, f.ghErr }
func (f *fakeGitHubClient) EnsureFork(_ context.Context, _, _ string) (string, error) {
	return f.forkFullName, f.ghErr
}
func (f *fakeGitHubClient) GetDefaultBranch(_ context.Context, _, _ string) (string, error) {
	return f.defaultBranch, f.ghErr
}
func (f *fakeGitHubClient) GetBranchSHA(_ context.Context, _, _, _ string) (string, error) {
	return f.branchSHA, f.ghErr
}
func (f *fakeGitHubClient) CreateBranch(_ context.Context, _, _, branch, _ string) error {
	f.createdBranch = branch
	return f.ghErr
}
func (f *fakeGitHubClient) GetFileSHA(_ context.Context, _, _, _, path string) (string, error) {
	return f.fileSHAs[path], f.ghErr
}
func (f *fakeGitHubClient) UpsertFile(_ context.Context, _, _, _, path, _ string, content []byte, _ string) error {
	f.upsertedFiles[path] = content
	return f.ghErr
}
func (f *fakeGitHubClient) CreatePR(_ context.Context, _, _, _, _, _, _, _ string) (string, error) {
	f.createPRCalls++
	return f.prURL, f.ghErr
}

// newProposeHandler constructs a proposeSkillHandler with injected dependencies.
func newProposeHandler(fetcher TreeFetcher, gh githubAPI) *proposeSkillHandler {
	return &proposeSkillHandler{
		fetcher:     fetcher,
		ghClientFor: func(_, _ string) githubAPI { return gh },
	}
}

// callProposeOK calls h.handle and fails if the handler returns an error result.
func callProposeOK(t *testing.T, h *proposeSkillHandler, input ProposeSkillChangesInput) ProposeSkillChangesOutput {
	t.Helper()
	res, out, err := h.handle(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("handler returned unexpected Go error: %v", err)
	}
	if res != nil {
		text := ""
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				text = tc.Text
			}
		}
		t.Fatalf("handler returned error result: %s", text)
	}
	return out
}

// callProposeErrResult calls h.handle and returns the error result text.
// Fails if the handler unexpectedly succeeds.
func callProposeErrResult(t *testing.T, h *proposeSkillHandler, input ProposeSkillChangesInput) string {
	t.Helper()
	res, _, err := h.handle(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("handler returned unexpected Go error: %v", err)
	}
	if res == nil {
		t.Fatal("expected error result, got success")
	}
	if len(res.Content) == 0 {
		t.Fatal("error result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent in error result")
	}
	return tc.Text
}

// serveProposeMarketplace serves a marketplace JSON for a GitHub-hosted plugin
// (source.source="github"). Uses generic gitHostType in config so test server
// is queried directly.
func serveProposeGitHubMarketplace(t *testing.T, mktName, pluginName, ghOwner string) *httptest.Server {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"owner":{"name":"Test"},`+
		`"plugins":[{"name":%q,"source":{"source":"github","repo":%q}}]}`,
		mktName, pluginName, ghOwner+"/"+pluginName)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serveProposeGenericMarketplace serves a marketplace for a non-GitHub URL plugin.
func serveProposeGenericMarketplace(t *testing.T, mktName, pluginName, pluginURL string) *httptest.Server {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"owner":{"name":"Test"},`+
		`"plugins":[{"name":%q,"source":{"source":"url","url":%q}}]}`,
		mktName, pluginName, pluginURL)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// withProposeConfig writes a config.yaml using gitHostType=generic so test servers
// are queried directly by marketplace.Fetch. credEnv is the credential env var name.
func withProposeConfig(t *testing.T, mktURL, credEnv string) {
	t.Helper()
	dir := t.TempDir()
	yaml := fmt.Sprintf("marketplaceSources:\n  - url: %s\n    gitHostType: generic\n", mktURL)
	if credEnv != "" {
		yaml += "    credentialEnvVar: " + credEnv + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("CLAUDE_PLUGIN_DATA", dir)
}

// --- tests ---

func TestProposeSkillChanges_MissingUpstreamDecl(t *testing.T) {
	localRoot := makePlugin(t, `{"name":"myplugin"}`, map[string]map[string]string{})
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())

	h := newProposeHandler(&fakeDiffFetcher{}, newFakeGH())
	out := callProposeOK(t, h, ProposeSkillChangesInput{PluginPath: localRoot, Skill: "my-skill"})

	if out.Status != "missing-upstream" {
		t.Errorf("expected missing-upstream, got %q", out.Status)
	}
}

func TestProposeSkillChanges_NothingToPropose(t *testing.T) {
	content := "same content\n"
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": content}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": content}})

	srv := serveProposeGitHubMarketplace(t, "alpha", "myplugin", "upstream")
	withProposeConfig(t, srv.URL, "GH_TOKEN")
	t.Setenv("GH_TOKEN", "fake-token")

	h := newProposeHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha1"}, newFakeGH())
	out := callProposeOK(t, h, ProposeSkillChangesInput{PluginPath: localRoot, Skill: "my-skill"})

	if out.Status != "nothing-to-propose" {
		t.Errorf("expected nothing-to-propose, got %q", out.Status)
	}
}

func TestProposeSkillChanges_DryRun(t *testing.T) {
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local version\n"}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream version\n"}})

	srv := serveProposeGitHubMarketplace(t, "alpha", "myplugin", "upstream")
	withProposeConfig(t, srv.URL, "")

	gh := newFakeGH()
	h := newProposeHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha2"}, gh)
	out := callProposeOK(t, h, ProposeSkillChangesInput{
		PluginPath: localRoot,
		Skill:      "my-skill",
		DryRun:     true,
	})

	if out.Status != "dry-run" {
		t.Errorf("expected dry-run, got %q", out.Status)
	}
	if out.PRURL != "" {
		t.Errorf("expected empty PRURL on dry-run, got %q", out.PRURL)
	}
	if out.Diff == "" {
		t.Error("expected non-empty Diff on dry-run")
	}
	if gh.createPRCalls != 0 {
		t.Errorf("expected no GitHub API calls on dry-run, got %d CreatePR calls", gh.createPRCalls)
	}
	if !strings.HasPrefix(out.Branch, "skillhub/propose-my-skill-") {
		t.Errorf("expected branch prefix 'skillhub/propose-my-skill-', got %q", out.Branch)
	}
}

func TestProposeSkillChanges_DryRunCustomBranch(t *testing.T) {
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local\n"}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream\n"}})

	srv := serveProposeGitHubMarketplace(t, "alpha", "myplugin", "upstream")
	withProposeConfig(t, srv.URL, "")

	h := newProposeHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha3"}, newFakeGH())
	out := callProposeOK(t, h, ProposeSkillChangesInput{
		PluginPath: localRoot,
		Skill:      "my-skill",
		DryRun:     true,
		Branch:     "my-custom-branch",
	})

	if out.Branch != "my-custom-branch" {
		t.Errorf("expected custom branch name, got %q", out.Branch)
	}
}

func TestProposeSkillChanges_LiveMode_CreatesFiles(t *testing.T) {
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local\n", "sub/helper.sh": "#!/bin/sh\n"}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream\n"}})

	srv := serveProposeGitHubMarketplace(t, "alpha", "myplugin", "upstream")
	withProposeConfig(t, srv.URL, "GH_TOKEN")
	t.Setenv("GH_TOKEN", "fake-token")

	gh := newFakeGH()
	h := newProposeHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha4"}, gh)
	out := callProposeOK(t, h, ProposeSkillChangesInput{
		PluginPath: localRoot,
		Skill:      "my-skill",
		Branch:     "test-branch",
	})

	if out.Status != "proposed" {
		t.Errorf("expected proposed, got %q", out.Status)
	}
	if out.PRURL != gh.prURL {
		t.Errorf("expected PR URL %q, got %q", gh.prURL, out.PRURL)
	}
	if len(gh.upsertedFiles) != 2 {
		t.Errorf("expected 2 files uploaded, got %d: %v", len(gh.upsertedFiles), gh.upsertedFiles)
	}
	if gh.createdBranch != "test-branch" {
		t.Errorf("expected branch test-branch, got %q", gh.createdBranch)
	}
	if gh.createPRCalls != 1 {
		t.Errorf("expected 1 CreatePR call, got %d", gh.createPRCalls)
	}
}

func TestProposeSkillChanges_LiveMode_NonGitHubReturnsError(t *testing.T) {
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local\n"}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream\n"}})

	// Use a non-GitHub plugin source URL.
	srv := serveProposeGenericMarketplace(t, "alpha", "myplugin", "https://gitlab.com/upstream/myplugin")
	withProposeConfig(t, srv.URL, "")

	text := callProposeErrResult(t, newProposeHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha5"}, newFakeGH()),
		ProposeSkillChangesInput{PluginPath: localRoot, Skill: "my-skill"})

	if !strings.Contains(text, "UNSUPPORTED_SOURCE") {
		t.Errorf("expected UNSUPPORTED_SOURCE, got: %s", text)
	}
}

func TestProposeSkillChanges_LiveMode_MissingTokenReturnsError(t *testing.T) {
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local\n"}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream\n"}})

	srv := serveProposeGitHubMarketplace(t, "alpha", "myplugin", "upstream")
	withProposeConfig(t, srv.URL, "GH_TOKEN")
	t.Setenv("GH_TOKEN", "") // deliberately empty

	text := callProposeErrResult(t, newProposeHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha6"}, newFakeGH()),
		ProposeSkillChangesInput{PluginPath: localRoot, Skill: "my-skill"})

	if !strings.Contains(text, "AUTH_FAILED") {
		t.Errorf("expected AUTH_FAILED, got: %s", text)
	}
}

func TestProposeSkillChanges_LiveMode_ResultHasDiff(t *testing.T) {
	localRoot := makePlugin(t,
		`{"name":"myplugin","x-skillhub-upstream":{"marketplace":"alpha","plugin":"myplugin"}}`,
		map[string]map[string]string{"my-skill": {"SKILL.md": "local\n"}},
	)
	upstreamRoot := makeUpstreamTree(t, map[string]map[string]string{"my-skill": {"SKILL.md": "upstream\n"}})

	srv := serveProposeGitHubMarketplace(t, "alpha", "myplugin", "upstream")
	withProposeConfig(t, srv.URL, "GH_TOKEN")
	t.Setenv("GH_TOKEN", "fake-token")

	gh := newFakeGH()
	h := newProposeHandler(&fakeDiffFetcher{dir: upstreamRoot, sha: "sha7"}, gh)
	out := callProposeOK(t, h, ProposeSkillChangesInput{PluginPath: localRoot, Skill: "my-skill"})

	// Live mode should still populate the diff field for visibility.
	if out.Diff == "" {
		t.Error("expected Diff to be populated in live mode output")
	}

	// Verify the JSON round-trip is clean.
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	var decoded ProposeSkillChangesOutput
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal output: %v\nraw: %s", err, b)
	}
}
