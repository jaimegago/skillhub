// Package fetch fetches a plugin's file tree from a git repository using a
// sparse shallow partial clone.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const cacheTTL = time.Hour

// PluginSource describes a git-backed plugin location. It can represent all
// four git-backed Claude Code plugin source kinds:
//
//   - relative: a path within a marketplace repo — caller resolves by passing
//     the marketplace repo URL as RepoURL and the relative path as Subpath.
//   - github: owner/repo shorthand — caller expands to https://github.com/owner/repo.
//   - url: arbitrary git URL — pass as RepoURL with empty Subpath.
//   - git-subdir: url plus path within repo — pass as RepoURL + Subpath.
//
// The npm source kind is out of scope; callers handle it before reaching this package.
type PluginSource struct {
	RepoURL          string // Git clone URL
	Subpath          string // Path within repo to plugin subtree; empty = repo root
	Ref              string // Branch or tag; empty = remote default branch
	SHA              string // Full 40-char pinned commit SHA; takes precedence over Ref
	CredentialEnvVar string // Env var holding an access token for private repos
}

type cacheMeta struct {
	FetchedAt time.Time `json:"fetchedAt"`
}

// PluginTree materialises the file tree for a plugin and returns the absolute
// path to the plugin subtree, the resolved commit SHA, and any error.
//
// cacheRoot should be the value returned by config.CacheDir(). refresh=true
// bypasses the 1-hour TTL for ref-based caches but does not evict an
// immutable SHA-pinned cache — those are always reused once present.
func PluginTree(ctx context.Context, src PluginSource, cacheRoot string, refresh bool) (string, string, error) {
	pinnedSHA := len(src.SHA) == 40
	cloneDir := filepath.Join(cacheRoot, "plugin-trees", urlKey(src.RepoURL), cacheSegmentFor(src, pinnedSHA))
	metaPath := filepath.Join(cloneDir, ".skillhub-meta.json")

	token := ""
	if src.CredentialEnvVar != "" {
		token = os.Getenv(src.CredentialEnvVar)
	}

	if isValidClone(cloneDir) {
		if pinnedSHA {
			// Immutable: pinned-SHA caches are reused regardless of refresh.
			sha, err := resolveHEAD(ctx, cloneDir, token)
			if err != nil {
				return "", "", fmt.Errorf("read cached sha for %s: %w", src.RepoURL, err)
			}
			if subtreePresent(cloneDir, src.Subpath) {
				return pluginTreePath(cloneDir, src.Subpath), sha, nil
			}
			// Subtree absent: clone died between clone and sparse-checkout. Evict.
		} else if !refresh && cacheIsFresh(metaPath) {
			sha, err := resolveHEAD(ctx, cloneDir, token)
			if err != nil {
				return "", "", fmt.Errorf("read cached sha for %s: %w", src.RepoURL, err)
			}
			if subtreePresent(cloneDir, src.Subpath) {
				return pluginTreePath(cloneDir, src.Subpath), sha, nil
			}
			// Subtree absent: clone died between clone and sparse-checkout. Evict.
		}
		// Stale, refresh requested, or incomplete clone: evict and re-clone.
		if err := os.RemoveAll(cloneDir); err != nil {
			return "", "", fmt.Errorf("evict stale cache for %s: %w", src.RepoURL, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(cloneDir), 0o755); err != nil {
		return "", "", fmt.Errorf("create cache parent for %s: %w", src.RepoURL, err)
	}

	if err := cloneRepo(ctx, src, cloneDir, token, pinnedSHA); err != nil {
		return "", "", err
	}

	if src.Subpath != "" {
		out, err := runGit(ctx, cloneDir, token, "sparse-checkout", "set", src.Subpath)
		if err != nil {
			return "", "", fmt.Errorf("sparse-checkout set %q in %s: %w\n%s",
				src.Subpath, src.RepoURL, err, strings.TrimSpace(string(out)))
		}
	} else {
		// No subpath: disable sparse-checkout so the full working tree is
		// materialised. --filter=blob:none on the clone still applies, so
		// blob content is fetched lazily on access.
		out, err := runGit(ctx, cloneDir, token, "sparse-checkout", "disable")
		if err != nil {
			return "", "", fmt.Errorf("sparse-checkout disable in %s: %w\n%s",
				src.RepoURL, err, strings.TrimSpace(string(out)))
		}
	}

	if pinnedSHA {
		// Omits --depth=1 so the full commit graph is available — any reachable
		// SHA can be checked out. --filter=blob:none still defers blob downloads
		// to checkout time, so the initial transfer is commits+trees only, not
		// full blob history.
		out, err := runGit(ctx, cloneDir, token, "checkout", src.SHA)
		if err != nil {
			return "", "", fmt.Errorf("checkout %s in %s: %w\n%s",
				src.SHA[:8], src.RepoURL, err, strings.TrimSpace(string(out)))
		}
	}

	// Verify the subpath was materialised; missing paths produce a clear error
	// rather than a silent empty directory.
	tp := pluginTreePath(cloneDir, src.Subpath)
	if src.Subpath != "" {
		if _, err := os.Stat(tp); err != nil {
			return "", "", fmt.Errorf("subpath %q not found in %s after sparse-checkout", src.Subpath, src.RepoURL)
		}
	}

	sha, err := resolveHEAD(ctx, cloneDir, token)
	if err != nil {
		return "", "", fmt.Errorf("resolve HEAD in %s: %w", src.RepoURL, err)
	}

	if !pinnedSHA {
		_ = writeMeta(metaPath)
	}

	return tp, sha, nil
}

// cloneRepo runs git clone with the appropriate flags for shallow vs full fetch.
// Ref-based fetches use --depth=1 to avoid downloading history. Pinned-SHA
// fetches omit --depth=1 so the full commit graph is available — any reachable
// SHA can be checked out. --filter=blob:none still defers blob downloads to
// checkout time, so the initial transfer is commits+trees only, not full blob
// history.
func cloneRepo(ctx context.Context, src PluginSource, cloneDir, token string, pinnedSHA bool) error {
	args := []string{"clone", "--filter=blob:none", "--sparse"}
	if !pinnedSHA {
		args = append(args, "--depth=1")
	}
	if src.Ref != "" && !pinnedSHA {
		args = append(args, "--branch", src.Ref)
	}
	args = append(args, src.RepoURL, cloneDir)

	out, err := runGit(ctx, "", token, args...)
	if err != nil {
		return fmt.Errorf("clone %s: %w\n%s", src.RepoURL, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runGit runs git with -c core.autocrlf=false -c core.eol=lf prepended to
// every invocation to prevent silent line-ending transformations on Windows.
// inDir sets -C when non-empty. When token is non-empty an inline credential
// helper is injected via -c credential.helper; the token is passed through an
// ephemeral child-process env var and is never written to disk.
func runGit(ctx context.Context, inDir, token string, subcmdArgs ...string) ([]byte, error) {
	args := []string{"-c", "core.autocrlf=false", "-c", "core.eol=lf"}
	if inDir != "" {
		args = append(args, "-C", inDir)
	}
	if token != "" {
		// SKILLHUB_GIT_TOKEN is an internal parent→child env channel used only
		// by this package. It is unrelated to the caller's CredentialEnvVar
		// field, which names the user-configured env var holding the real token;
		// this is just the name the credential helper sub-shell reads from.
		// Nothing is written to disk; the variable exists only for the lifetime
		// of this git invocation.
		helper := `!f() { echo "username=x-access-token"; echo "password=$SKILLHUB_GIT_TOKEN"; }; f`
		args = append(args, "-c", "credential.helper="+helper)
	}
	args = append(args, subcmdArgs...)

	cmd := exec.CommandContext(ctx, "git", args...)
	if token != "" {
		cmd.Env = append(os.Environ(), "SKILLHUB_GIT_TOKEN="+token)
	}
	return cmd.CombinedOutput()
}

func resolveHEAD(ctx context.Context, cloneDir, token string) (string, error) {
	out, err := runGit(ctx, cloneDir, token, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// urlKey returns an 8-hex-char directory name derived from the repo URL,
// matching the convention used by the marketplace package.
func urlKey(url string) string {
	h := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x", h[:4])
}

// cacheSegmentFor returns the cache sub-directory name for a given source.
// Slashes in ref names are replaced with underscores to avoid nested paths and
// name collisions (e.g. "feature/foo" → "feature_foo").
// TODO(v2): underscore substitution can collide — "feature/foo" and
// "feature_foo" map to the same segment for the same repo. Negligible in v1;
// a future revisit should consider hashing the ref instead.
func cacheSegmentFor(src PluginSource, pinnedSHA bool) string {
	if pinnedSHA {
		return src.SHA
	}
	seg := src.Ref
	if seg == "" {
		seg = "HEAD"
	}
	return strings.ReplaceAll(seg, "/", "_")
}

func isValidClone(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// subtreePresent reports whether the plugin subtree has been materialised inside
// cloneDir. When subpath is empty the clone root itself is the plugin root and
// is always considered present (isValidClone already confirmed the dir exists).
func subtreePresent(cloneDir, subpath string) bool {
	if subpath == "" {
		return true
	}
	_, err := os.Stat(filepath.Join(cloneDir, filepath.FromSlash(subpath)))
	return err == nil
}

// pluginTreePath returns the absolute path to the plugin subtree. When subpath
// is empty the clone root is the plugin root; callers can walk it directly.
func pluginTreePath(cloneDir, subpath string) string {
	if subpath == "" {
		return cloneDir
	}
	return filepath.Join(cloneDir, filepath.FromSlash(subpath))
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

func writeMeta(metaPath string) error {
	m := cacheMeta{FetchedAt: time.Now().UTC()}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0o644)
}
