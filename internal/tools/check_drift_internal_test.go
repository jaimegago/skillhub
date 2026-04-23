package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/fetch"
)

// fakeTreeFetcher is a test double for TreeFetcher that returns a
// caller-supplied local directory and SHA without touching the network.
type fakeTreeFetcher struct {
	dir     string
	sha     string
	called  bool
	lastSrc fetch.PluginSource
}

func (f *fakeTreeFetcher) FetchPluginTree(_ context.Context, src fetch.PluginSource, _ string, _ bool) (string, string, error) {
	f.called = true
	f.lastSrc = src
	return f.dir, f.sha, nil
}

// newCheckDriftHandlerForTest constructs a checkDriftHandler with the given fetcher.
func newCheckDriftHandlerForTest(fetcher TreeFetcher) *checkDriftHandler {
	return &checkDriftHandler{fetcher: fetcher}
}

// callDriftHandler calls h.handle and returns a *mcp.CallToolResult, marshalling
// success output into TextContent the same way callCheckDrift does in
// check_drift_test.go.
func callDriftHandler(t *testing.T, h *checkDriftHandler, input CheckDriftInput) *mcp.CallToolResult {
	t.Helper()
	res, out, err := h.handle(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if res != nil {
		return res
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}
}

func driftResultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("empty Content in result")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

func assertDriftErrCode(t *testing.T, result *mcp.CallToolResult, code string) {
	t.Helper()
	text := driftResultText(t, result)
	if !strings.Contains(text, code) {
		t.Errorf("expected error code %q in result, got: %s", code, text)
	}
}

func parseDriftOutput(t *testing.T, result *mcp.CallToolResult) CheckDriftOutput {
	t.Helper()
	text := driftResultText(t, result)
	var out CheckDriftOutput
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("unmarshal CheckDriftOutput: %v\nraw: %s", err, text)
	}
	return out
}

// makePlugin creates a temp dir with .claude-plugin/plugin.json and a skills/
// directory. Each key in skills maps to a sub-map of filename→content.
func makePlugin(t *testing.T, manifest string, skills map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()

	pluginDir := filepath.Join(root, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude-plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}

	// Always create skills/ so the handler doesn't short-circuit to "no-skills-dir".
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}

	for skillName, files := range skills {
		for rel, content := range files {
			full := filepath.Join(root, "skills", skillName, rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir for %s/%s: %v", skillName, rel, err)
			}
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s/%s: %v", skillName, rel, err)
			}
		}
	}
	return root
}

// makeUpstream creates a directory tree mirroring an upstream plugin root,
// with a skills/ sub-directory populated from the given map.
func makeUpstream(t *testing.T, skills map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for skillName, files := range skills {
		for rel, content := range files {
			full := filepath.Join(root, "skills", skillName, rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir %s/%s: %v", skillName, rel, err)
			}
			if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
				t.Fatalf("write %s/%s: %v", skillName, rel, err)
			}
		}
	}
	return root
}

// serveMarketplace starts an httptest server returning the given JSON body.
func serveMarketplace(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// withFakeConfig sets CLAUDE_PLUGIN_DATA to point at a temp dir with the
// given config.yaml content. Restores the env after the test.
func withFakeConfig(t *testing.T, yaml string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("CLAUDE_PLUGIN_DATA", dir)
}

// githubMarketplace returns minimal marketplace JSON with a single github-source
// plugin entry, served via httptest, and configures the env to point at it.
// Returns the handler and test server (already cleaned up via t.Cleanup).
func setupGithubMarketplace(t *testing.T, upstream string, sha string) (*checkDriftHandler, *fakeTreeFetcher) {
	t.Helper()
	const mktJSON = `{"name":"test-market","plugins":[{"name":"my-plugin","source":{"source":"github","repo":"owner/repo","ref":"main"}}]}`
	srv := serveMarketplace(t, mktJSON)
	withFakeConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")
	fake := &fakeTreeFetcher{dir: upstream, sha: sha}
	return newCheckDriftHandlerForTest(fake), fake
}

const upToDateManifest = `{"name":"myplugin","x-skillhub-upstream":{"marketplace":"test-market","plugin":"my-plugin"}}`

// --- happy-path tests (all use the fake fetcher) ---

func TestCheckDrift_UpToDate(t *testing.T) {
	files := map[string]map[string]string{
		"skill-a": {"SKILL.md": "content-a", "lib.sh": "#!/bin/sh\necho hello"},
		"skill-b": {"SKILL.md": "content-b"},
	}
	localRoot := makePlugin(t, upToDateManifest, files)
	upstreamRoot := makeUpstream(t, files)

	h, _ := setupGithubMarketplace(t, upstreamRoot, "abc123sha")
	out := parseDriftOutput(t, callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot}))

	if out.PluginStatus != "up-to-date" {
		t.Errorf("plugin_status = %q, want up-to-date", out.PluginStatus)
	}
	if len(out.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(out.Skills))
	}
	for _, s := range out.Skills {
		if s.Status != "up-to-date" {
			t.Errorf("skill %q status = %q, want up-to-date", s.Skill, s.Status)
		}
		if len(s.DriftedFiles) != 0 || len(s.LocalOnlyFiles) != 0 || len(s.UpstreamOnlyFiles) != 0 {
			t.Errorf("skill %q: unexpected diff entries", s.Skill)
		}
	}
}

func TestCheckDrift_DriftedOneByte(t *testing.T) {
	localFiles := map[string]map[string]string{
		"skill-a": {"SKILL.md": "content-a-MODIFIED"},
		"skill-b": {"SKILL.md": "content-b"},
	}
	upstreamFiles := map[string]map[string]string{
		"skill-a": {"SKILL.md": "content-a"},
		"skill-b": {"SKILL.md": "content-b"},
	}
	localRoot := makePlugin(t, upToDateManifest, localFiles)
	upstreamRoot := makeUpstream(t, upstreamFiles)

	h, _ := setupGithubMarketplace(t, upstreamRoot, "def456sha")
	out := parseDriftOutput(t, callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot}))

	if out.PluginStatus != "drifted" {
		t.Errorf("plugin_status = %q, want drifted", out.PluginStatus)
	}

	skillMap := map[string]SkillDriftResult{}
	for _, s := range out.Skills {
		skillMap[s.Skill] = s
	}

	sa := skillMap["skill-a"]
	if sa.Status != "drifted" {
		t.Errorf("skill-a status = %q, want drifted", sa.Status)
	}
	if len(sa.DriftedFiles) == 0 {
		t.Error("skill-a: expected drifted_files")
	} else if sa.DriftedFiles[0] != "SKILL.md" {
		t.Errorf("skill-a drifted_files[0] = %q, want SKILL.md", sa.DriftedFiles[0])
	}

	if sb := skillMap["skill-b"]; sb.Status != "up-to-date" {
		t.Errorf("skill-b status = %q, want up-to-date", sb.Status)
	}
}

func TestCheckDrift_LocalOnlyFile(t *testing.T) {
	localFiles := map[string]map[string]string{
		"skill-a": {"SKILL.md": "content", "extra.sh": "#!/bin/sh"},
	}
	upstreamFiles := map[string]map[string]string{
		"skill-a": {"SKILL.md": "content"},
	}
	localRoot := makePlugin(t, upToDateManifest, localFiles)
	upstreamRoot := makeUpstream(t, upstreamFiles)

	h, _ := setupGithubMarketplace(t, upstreamRoot, "abc")
	out := parseDriftOutput(t, callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot}))

	if out.PluginStatus != "drifted" {
		t.Errorf("plugin_status = %q, want drifted", out.PluginStatus)
	}
	if len(out.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(out.Skills))
	}
	s := out.Skills[0]
	if s.Status != "drifted" {
		t.Errorf("skill-a status = %q, want drifted", s.Status)
	}
	if len(s.LocalOnlyFiles) == 0 {
		t.Error("expected local_only_files")
	} else if s.LocalOnlyFiles[0] != "extra.sh" {
		t.Errorf("local_only_files[0] = %q, want extra.sh", s.LocalOnlyFiles[0])
	}
}

func TestCheckDrift_UpstreamOnlyFile(t *testing.T) {
	localFiles := map[string]map[string]string{
		"skill-a": {"SKILL.md": "content"},
	}
	upstreamFiles := map[string]map[string]string{
		"skill-a": {"SKILL.md": "content", "new.sh": "#!/bin/sh"},
	}
	localRoot := makePlugin(t, upToDateManifest, localFiles)
	upstreamRoot := makeUpstream(t, upstreamFiles)

	h, _ := setupGithubMarketplace(t, upstreamRoot, "abc")
	out := parseDriftOutput(t, callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot}))

	if out.PluginStatus != "drifted" {
		t.Errorf("plugin_status = %q, want drifted", out.PluginStatus)
	}
	s := out.Skills[0]
	if s.Status != "drifted" {
		t.Errorf("status = %q, want drifted", s.Status)
	}
	if len(s.UpstreamOnlyFiles) == 0 {
		t.Error("expected upstream_only_files")
	} else if s.UpstreamOnlyFiles[0] != "new.sh" {
		t.Errorf("upstream_only_files[0] = %q, want new.sh", s.UpstreamOnlyFiles[0])
	}
}

func TestCheckDrift_SkillMissingLocal_ViaOverride(t *testing.T) {
	// Local plugin has a skills/ dir but no skill-a subdir.
	// input.Skill="skill-a" forces the handler to check that specific skill.
	// Upstream has skills/skill-a — result should be "missing-local".
	localRoot := makePlugin(t, upToDateManifest, map[string]map[string]string{})
	upstreamRoot := makeUpstream(t, map[string]map[string]string{
		"skill-a": {"SKILL.md": "content"},
	})

	h, _ := setupGithubMarketplace(t, upstreamRoot, "abc")
	out := parseDriftOutput(t, callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot, Skill: "skill-a"}))

	if len(out.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(out.Skills))
	}
	if out.Skills[0].Status != "missing-local" {
		t.Errorf("status = %q, want missing-local", out.Skills[0].Status)
	}
	if out.PluginStatus != "drifted" {
		t.Errorf("plugin_status = %q, want drifted", out.PluginStatus)
	}
}

func TestCheckDrift_SkillMissingUpstream(t *testing.T) {
	// Local skill exists; upstream tree has no skills/skill-a dir.
	localRoot := makePlugin(t, upToDateManifest, map[string]map[string]string{
		"skill-a": {"SKILL.md": "content"},
	})
	upstreamRoot := t.TempDir() // no skills/ at all

	h, _ := setupGithubMarketplace(t, upstreamRoot, "abc")
	out := parseDriftOutput(t, callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot}))

	if len(out.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(out.Skills))
	}
	if out.Skills[0].Status != "missing-upstream" {
		t.Errorf("status = %q, want missing-upstream", out.Skills[0].Status)
	}
	if out.PluginStatus != "drifted" {
		t.Errorf("plugin_status = %q, want drifted", out.PluginStatus)
	}
}

func TestCheckDrift_SingleSkillFilter(t *testing.T) {
	files := map[string]map[string]string{
		"skill-a": {"SKILL.md": "content-a"},
		"skill-b": {"SKILL.md": "content-b"},
	}
	localRoot := makePlugin(t, upToDateManifest, files)
	upstreamRoot := makeUpstream(t, files)

	h, _ := setupGithubMarketplace(t, upstreamRoot, "abc")
	out := parseDriftOutput(t, callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot, Skill: "skill-a"}))

	if len(out.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d: %v", len(out.Skills), out.Skills)
	}
	if out.Skills[0].Skill != "skill-a" {
		t.Errorf("skill = %q, want skill-a", out.Skills[0].Skill)
	}
	if out.Skills[0].Status != "up-to-date" {
		t.Errorf("status = %q, want up-to-date", out.Skills[0].Status)
	}
}

// TestCheckDrift_GithubSourceChain verifies the full
// marketplace.Fetch → parsePluginSource → TreeFetcher chain for a github-source
// entry. The fake fetcher records the PluginSource it receives so we can assert
// that parsePluginSource's github branch populated the fields correctly.
func TestCheckDrift_GithubSourceChain(t *testing.T) {
	const mktJSON = `{"name":"test-market","plugins":[{"name":"my-plugin","source":{"source":"github","repo":"owner/repo","ref":"main"}}]}`
	srv := serveMarketplace(t, mktJSON)
	withFakeConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	upstreamRoot := t.TempDir()
	const fixedSHA = "deadbeef0123456789012345678901234567abcd"
	fake := &fakeTreeFetcher{dir: upstreamRoot, sha: fixedSHA}
	h := newCheckDriftHandlerForTest(fake)

	localRoot := makePlugin(t, upToDateManifest, map[string]map[string]string{})

	result := callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot})

	if !fake.called {
		t.Fatal("TreeFetcher.FetchPluginTree was not called")
	}
	if fake.lastSrc.RepoURL != "https://github.com/owner/repo" {
		t.Errorf("RepoURL = %q, want https://github.com/owner/repo", fake.lastSrc.RepoURL)
	}
	if fake.lastSrc.Ref != "main" {
		t.Errorf("Ref = %q, want main", fake.lastSrc.Ref)
	}
	if fake.lastSrc.Subpath != "" {
		t.Errorf("Subpath = %q, want empty", fake.lastSrc.Subpath)
	}
	if fake.lastSrc.SHA != "" {
		t.Errorf("SHA = %q, want empty (no pinned SHA in fixture)", fake.lastSrc.SHA)
	}

	out := parseDriftOutput(t, result)
	if out.CommitSHA != fixedSHA {
		t.Errorf("CommitSHA = %q, want %q", out.CommitSHA, fixedSHA)
	}

	// No local skills → no upstream skills dir → skills slice is empty → up-to-date.
	if out.PluginStatus != "up-to-date" {
		t.Errorf("plugin_status = %q, want up-to-date", out.PluginStatus)
	}
}

// assertDriftErrCode is kept to suppress the "declared but not used" error
// for the helper; it will be used if error-path tests are added to this file.
var _ = assertDriftErrCode

// TestCheckDrift_UnreadableFile: local has readable A and unreadable B (chmod
// 0o000); upstream has identical A and B. B is excluded from comparison and
// surfaces as a warning. Skill and plugin are up-to-date.
func TestCheckDrift_UnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0o000 not supported on Windows")
	}

	localFiles := map[string]map[string]string{
		"skill-a": {"a.sh": "content-a", "b.sh": "content-b"},
	}
	upstreamFiles := map[string]map[string]string{
		"skill-a": {"a.sh": "content-a", "b.sh": "content-b"},
	}
	localRoot := makePlugin(t, upToDateManifest, localFiles)
	upstreamRoot := makeUpstream(t, upstreamFiles)

	unreadablePath := filepath.Join(localRoot, "skills", "skill-a", "b.sh")
	if err := os.Chmod(unreadablePath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadablePath, 0o644) })

	// Skip if we can still read the file (e.g. running as root).
	if _, err := os.ReadFile(unreadablePath); err == nil {
		t.Skip("file still readable after chmod 0o000 (running as root?)")
	}

	h, _ := setupGithubMarketplace(t, upstreamRoot, "abc")
	out := parseDriftOutput(t, callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot}))

	if out.PluginStatus != "up-to-date" {
		t.Errorf("plugin_status = %q, want up-to-date", out.PluginStatus)
	}
	if !out.HasWarnings {
		t.Error("expected has_warnings=true")
	}
	if len(out.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(out.Skills))
	}
	s := out.Skills[0]
	if s.Status != "up-to-date" {
		t.Errorf("skill status = %q, want up-to-date", s.Status)
	}
	if len(s.DriftedFiles) != 0 || len(s.LocalOnlyFiles) != 0 || len(s.UpstreamOnlyFiles) != 0 {
		t.Errorf("unexpected drift entries: drifted=%v local_only=%v upstream_only=%v",
			s.DriftedFiles, s.LocalOnlyFiles, s.UpstreamOnlyFiles)
	}
	wantWarn := "unreadable: local/b.sh"
	found := false
	for _, w := range s.Warnings {
		if w == wantWarn {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning %q in %v", wantWarn, s.Warnings)
	}
}

// TestCheckDrift_UnreadableCausesSkip: local has readable A (matches upstream)
// and unreadable B. Upstream has A only. B is excluded from comparison so there
// is no false local_only entry.
func TestCheckDrift_UnreadableCausesSkip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0o000 not supported on Windows")
	}

	localFiles := map[string]map[string]string{
		"skill-a": {"a.sh": "content-a", "b.sh": "content-b"},
	}
	upstreamFiles := map[string]map[string]string{
		"skill-a": {"a.sh": "content-a"},
	}
	localRoot := makePlugin(t, upToDateManifest, localFiles)
	upstreamRoot := makeUpstream(t, upstreamFiles)

	unreadablePath := filepath.Join(localRoot, "skills", "skill-a", "b.sh")
	if err := os.Chmod(unreadablePath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadablePath, 0o644) })

	if _, err := os.ReadFile(unreadablePath); err == nil {
		t.Skip("file still readable after chmod 0o000 (running as root?)")
	}

	h, _ := setupGithubMarketplace(t, upstreamRoot, "abc")
	out := parseDriftOutput(t, callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot}))

	if out.PluginStatus != "up-to-date" {
		t.Errorf("plugin_status = %q, want up-to-date", out.PluginStatus)
	}
	if len(out.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(out.Skills))
	}
	s := out.Skills[0]
	if s.Status != "up-to-date" {
		t.Errorf("skill status = %q, want up-to-date", s.Status)
	}
	if len(s.LocalOnlyFiles) != 0 {
		t.Errorf("unexpected local_only_files: %v", s.LocalOnlyFiles)
	}
	wantWarn := "unreadable: local/b.sh"
	found := false
	for _, w := range s.Warnings {
		if w == wantWarn {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning %q in %v", wantWarn, s.Warnings)
	}
}

// TestCheckDrift_SymlinkIgnored: a symlink exists only in the local skill.
// Upstream does not have that path. No local_only entry; symlink surfaces as a
// warning.
func TestCheckDrift_SymlinkIgnored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on Windows")
	}

	localFiles := map[string]map[string]string{
		"skill-a": {"a.sh": "content-a"},
	}
	upstreamFiles := map[string]map[string]string{
		"skill-a": {"a.sh": "content-a"},
	}
	localRoot := makePlugin(t, upToDateManifest, localFiles)
	upstreamRoot := makeUpstream(t, upstreamFiles)

	linkPath := filepath.Join(localRoot, "skills", "skill-a", "link.sh")
	if err := os.Symlink("/dev/null", linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	h, _ := setupGithubMarketplace(t, upstreamRoot, "abc")
	out := parseDriftOutput(t, callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot}))

	if out.PluginStatus != "up-to-date" {
		t.Errorf("plugin_status = %q, want up-to-date", out.PluginStatus)
	}
	if len(out.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(out.Skills))
	}
	s := out.Skills[0]
	if s.Status != "up-to-date" {
		t.Errorf("skill status = %q, want up-to-date", s.Status)
	}
	if len(s.LocalOnlyFiles) != 0 {
		t.Errorf("unexpected local_only_files: %v (symlink must not appear as drift)", s.LocalOnlyFiles)
	}
	wantWarn := "symlink ignored: local/link.sh"
	found := false
	for _, w := range s.Warnings {
		if w == wantWarn {
			found = true
		}
	}
	if !found {
		t.Errorf("expected warning %q in %v", wantWarn, s.Warnings)
	}
}

// TestCheckDrift_SymlinkBothSides: same symlink path on both local and upstream.
// No drift; two warnings (one per side).
func TestCheckDrift_SymlinkBothSides(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on Windows")
	}

	localFiles := map[string]map[string]string{
		"skill-a": {"a.sh": "content-a"},
	}
	upstreamFiles := map[string]map[string]string{
		"skill-a": {"a.sh": "content-a"},
	}
	localRoot := makePlugin(t, upToDateManifest, localFiles)
	upstreamRoot := makeUpstream(t, upstreamFiles)

	localLink := filepath.Join(localRoot, "skills", "skill-a", "link.sh")
	if err := os.Symlink("/dev/null", localLink); err != nil {
		t.Fatalf("local symlink: %v", err)
	}
	upstreamLink := filepath.Join(upstreamRoot, "skills", "skill-a", "link.sh")
	if err := os.Symlink("/dev/null", upstreamLink); err != nil {
		t.Fatalf("upstream symlink: %v", err)
	}

	h, _ := setupGithubMarketplace(t, upstreamRoot, "abc")
	out := parseDriftOutput(t, callDriftHandler(t, h, CheckDriftInput{PluginPath: localRoot}))

	if out.PluginStatus != "up-to-date" {
		t.Errorf("plugin_status = %q, want up-to-date", out.PluginStatus)
	}
	if len(out.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(out.Skills))
	}
	s := out.Skills[0]
	if s.Status != "up-to-date" {
		t.Errorf("skill status = %q, want up-to-date", s.Status)
	}
	if len(s.DriftedFiles) != 0 || len(s.LocalOnlyFiles) != 0 || len(s.UpstreamOnlyFiles) != 0 {
		t.Errorf("unexpected drift entries: %+v", s)
	}
	warnSet := make(map[string]bool, len(s.Warnings))
	for _, w := range s.Warnings {
		warnSet[w] = true
	}
	for _, want := range []string{"symlink ignored: local/link.sh", "symlink ignored: upstream/link.sh"} {
		if !warnSet[want] {
			t.Errorf("expected warning %q in %v", want, s.Warnings)
		}
	}
}
