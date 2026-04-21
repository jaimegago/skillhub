package fetch_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaime-gago/skillhub/internal/fetch"
)

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found on PATH")
	}
}

// makeFixtureRepo initialises a bare git repository, populates it with files,
// and returns a file:// URL and the initial commit SHA. Both are cleaned up
// via TempDir when the test exits.
func makeFixtureRepo(t *testing.T, files map[string]string) (repoURL, sha string) {
	t.Helper()
	base := t.TempDir()
	workDir := filepath.Join(base, "work")
	bareDir := filepath.Join(base, "bare.git")

	gitFixture(t, "", "git", "init", workDir)
	gitFixture(t, workDir, "git", "config", "user.email", "ci@test")
	gitFixture(t, workDir, "git", "config", "user.name", "CI")

	for rel, content := range files {
		fullPath := filepath.Join(workDir, rel)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	gitFixture(t, workDir, "git", "add", ".")
	gitFixture(t, workDir, "git", "commit", "-m", "initial")

	out, err := exec.Command("git", "-C", workDir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}

	// Bare repo is the clean origin that tests clone from.
	gitFixture(t, "", "git", "clone", "--bare", workDir, bareDir)

	return "file://" + bareDir, strings.TrimSpace(string(out))
}

// makeFixtureRepoTagged is like makeFixtureRepo but also creates a lightweight
// tag on the initial commit.
func makeFixtureRepoTagged(t *testing.T, files map[string]string, tag string) (repoURL, sha string) {
	t.Helper()
	base := t.TempDir()
	workDir := filepath.Join(base, "work")
	bareDir := filepath.Join(base, "bare.git")

	gitFixture(t, "", "git", "init", workDir)
	gitFixture(t, workDir, "git", "config", "user.email", "ci@test")
	gitFixture(t, workDir, "git", "config", "user.name", "CI")

	for rel, content := range files {
		fullPath := filepath.Join(workDir, rel)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	gitFixture(t, workDir, "git", "add", ".")
	gitFixture(t, workDir, "git", "commit", "-m", "initial")
	gitFixture(t, workDir, "git", "tag", tag)

	out, err := exec.Command("git", "-C", workDir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}

	gitFixture(t, "", "git", "clone", "--bare", workDir, bareDir)

	return "file://" + bareDir, strings.TrimSpace(string(out))
}

func gitFixture(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", args, out)
	}
}

func TestFetchPluginTree_SubpathHappyPath(t *testing.T) {
	skipIfNoGit(t)

	repoURL, wantSHA := makeFixtureRepo(t, map[string]string{
		"plugins/myplugin/plugin.json": `{"name":"myplugin"}`,
		"plugins/myplugin/README.md":   "# myplugin",
		"other-stuff/data.txt":         "not part of the plugin",
	})

	src := fetch.PluginSource{RepoURL: repoURL, Subpath: "plugins/myplugin"}
	treePath, sha, err := fetch.PluginTree(context.Background(), src, t.TempDir(), false)
	if err != nil {
		t.Fatalf("FetchPluginTree: %v", err)
	}
	if sha != wantSHA {
		t.Errorf("sha = %q, want %q", sha, wantSHA)
	}
	if len(sha) != 40 {
		t.Errorf("sha length = %d, want 40", len(sha))
	}

	// treePath must point to the subtree root, not the clone root.
	pluginJSON := filepath.Join(treePath, "plugin.json")
	if _, err := os.Stat(pluginJSON); err != nil {
		t.Errorf("plugin.json not found at expected subtree root %s: %v", treePath, err)
	}

	// other-stuff/ is in a sibling directory; sparse-checkout must exclude it.
	cloneRoot := filepath.Dir(filepath.Dir(treePath)) // treePath = cloneRoot/plugins/myplugin
	otherStuff := filepath.Join(cloneRoot, "other-stuff")
	if _, err := os.Stat(otherStuff); err == nil {
		t.Error("other-stuff/ should not be materialised by the sparse-checkout")
	}
}

func TestFetchPluginTree_ExplicitRef(t *testing.T) {
	skipIfNoGit(t)
	const tag = "v1.0.0"

	repoURL, wantSHA := makeFixtureRepoTagged(t, map[string]string{
		"plugin.json": `{"name":"tagged"}`,
	}, tag)

	src := fetch.PluginSource{RepoURL: repoURL, Ref: tag}
	_, sha, err := fetch.PluginTree(context.Background(), src, t.TempDir(), false)
	if err != nil {
		t.Fatalf("FetchPluginTree with ref %q: %v", tag, err)
	}
	if sha != wantSHA {
		t.Errorf("sha = %q, want %q", sha, wantSHA)
	}
}

func TestFetchPluginTree_PinnedSHA(t *testing.T) {
	skipIfNoGit(t)

	repoURL, wantSHA := makeFixtureRepo(t, map[string]string{
		"plugin.json": `{"name":"pinned"}`,
	})

	src := fetch.PluginSource{RepoURL: repoURL, SHA: wantSHA}
	_, sha, err := fetch.PluginTree(context.Background(), src, t.TempDir(), false)
	if err != nil {
		t.Fatalf("FetchPluginTree with pinned sha: %v", err)
	}
	if sha != wantSHA {
		t.Errorf("sha = %q, want %q", sha, wantSHA)
	}
}

func TestFetchPluginTree_CacheHit(t *testing.T) {
	skipIfNoGit(t)

	repoURL, _ := makeFixtureRepo(t, map[string]string{
		"plugin.json": `{"name":"cached"}`,
	})

	cacheDir := t.TempDir()
	src := fetch.PluginSource{RepoURL: repoURL}

	path1, sha1, err := fetch.PluginTree(context.Background(), src, cacheDir, false)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	// Sentinel file inside the clone proves the directory was not evicted.
	sentinelPath := filepath.Join(path1, ".test-sentinel")
	if err := os.WriteFile(sentinelPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	path2, sha2, err := fetch.PluginTree(context.Background(), src, cacheDir, false)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if path1 != path2 {
		t.Errorf("path changed on cache hit: %q → %q", path1, path2)
	}
	if sha1 != sha2 {
		t.Errorf("sha changed on cache hit: %q → %q", sha1, sha2)
	}
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Error("cache was evicted but should have been reused (sentinel file gone)")
	}
}

func TestFetchPluginTree_RefreshBypassesCache(t *testing.T) {
	skipIfNoGit(t)

	repoURL, _ := makeFixtureRepo(t, map[string]string{
		"plugin.json": `{"name":"refresh"}`,
	})

	cacheDir := t.TempDir()
	src := fetch.PluginSource{RepoURL: repoURL}

	path1, _, err := fetch.PluginTree(context.Background(), src, cacheDir, false)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	sentinelPath := filepath.Join(path1, ".test-sentinel")
	if err := os.WriteFile(sentinelPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	_, _, err = fetch.PluginTree(context.Background(), src, cacheDir, true)
	if err != nil {
		t.Fatalf("refresh fetch: %v", err)
	}
	if _, err := os.Stat(sentinelPath); err == nil {
		t.Error("expected cache to be evicted on refresh, but sentinel still exists")
	}
}

func TestFetchPluginTree_MissingSubpath(t *testing.T) {
	skipIfNoGit(t)

	repoURL, _ := makeFixtureRepo(t, map[string]string{
		"plugin.json": `{"name":"top-level"}`,
	})

	src := fetch.PluginSource{RepoURL: repoURL, Subpath: "nonexistent/subpath"}
	_, _, err := fetch.PluginTree(context.Background(), src, t.TempDir(), false)
	if err == nil {
		t.Fatal("expected error for nonexistent subpath, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent/subpath") {
		t.Errorf("error should mention the missing subpath, got: %v", err)
	}
}

func TestFetchPluginTree_UnreachableRepo(t *testing.T) {
	skipIfNoGit(t)

	src := fetch.PluginSource{RepoURL: "file:///this/path/does/not/exist"}
	_, _, err := fetch.PluginTree(context.Background(), src, t.TempDir(), false)
	if err == nil {
		t.Fatal("expected error for unreachable repo URL, got nil")
	}
}

// TestFetchPluginTree_CorruptedCacheRecovers verifies that a cache directory
// with a valid .git/ but no materialised subpath (simulating a clone that died
// between clone and sparse-checkout) is detected, evicted, and re-cloned.
func TestFetchPluginTree_CorruptedCacheRecovers(t *testing.T) {
	skipIfNoGit(t)

	repoURL, _ := makeFixtureRepo(t, map[string]string{
		"plugins/myplugin/plugin.json": `{"name":"myplugin"}`,
	})

	cacheDir := t.TempDir()
	src := fetch.PluginSource{RepoURL: repoURL, Subpath: "plugins/myplugin"}

	// Populate the cache with a successful fetch.
	treePath, _, err := fetch.PluginTree(context.Background(), src, cacheDir, false)
	if err != nil {
		t.Fatalf("initial fetch: %v", err)
	}

	// Simulate a clone that died before sparse-checkout completed: remove the
	// materialised subtree while leaving .git/ intact.
	if err := os.RemoveAll(treePath); err != nil {
		t.Fatalf("remove subtree to simulate corruption: %v", err)
	}
	if _, err := os.Stat(treePath); err == nil {
		t.Fatal("subtree should be gone after removal")
	}

	// PluginTree must detect the missing subtree, evict the corrupted clone,
	// re-clone, and return the subtree path with the expected files present.
	treePath2, _, err := fetch.PluginTree(context.Background(), src, cacheDir, false)
	if err != nil {
		t.Fatalf("recovery fetch: %v", err)
	}
	pluginJSON := filepath.Join(treePath2, "plugin.json")
	if _, err := os.Stat(pluginJSON); err != nil {
		t.Errorf("plugin.json not found after recovery: %v", err)
	}
}

// TestFetchPluginTree_ImmutableSHACache verifies that refresh=true does not
// evict a pinned-SHA cache entry.
func TestFetchPluginTree_ImmutableSHACache(t *testing.T) {
	skipIfNoGit(t)

	repoURL, wantSHA := makeFixtureRepo(t, map[string]string{
		"plugin.json": `{"name":"immutable"}`,
	})

	cacheDir := t.TempDir()
	src := fetch.PluginSource{RepoURL: repoURL, SHA: wantSHA}

	path1, sha1, err := fetch.PluginTree(context.Background(), src, cacheDir, false)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	sentinelPath := filepath.Join(path1, ".test-sentinel")
	if err := os.WriteFile(sentinelPath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	// refresh=true must not evict an immutable SHA-pinned cache.
	path2, sha2, err := fetch.PluginTree(context.Background(), src, cacheDir, true)
	if err != nil {
		t.Fatalf("refresh fetch: %v", err)
	}
	if path1 != path2 {
		t.Errorf("path changed: %q → %q", path1, path2)
	}
	if sha2 != wantSHA || sha1 != sha2 {
		t.Errorf("sha = %q, want %q", sha2, wantSHA)
	}
	if _, err := os.Stat(sentinelPath); err != nil {
		t.Error("immutable SHA cache was evicted by refresh=true (sentinel gone)")
	}
}
