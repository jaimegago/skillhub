package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaime-gago/skillhub/internal/config"
	skerrors "github.com/jaime-gago/skillhub/internal/errors"
	"github.com/jaime-gago/skillhub/internal/fetch"
	"github.com/jaime-gago/skillhub/internal/marketplace"
)

// errNpmSource is returned by parsePluginSource when the source kind is npm.
var errNpmSource = errors.New("npm source")

// TreeFetcher abstracts the remote plugin tree fetch in HandleCheckDrift,
// allowing tests to inject a fake without touching the network.
type TreeFetcher interface {
	FetchPluginTree(ctx context.Context, src fetch.PluginSource, cacheRoot string, refresh bool) (string, string, error)
}

type realTreeFetcher struct{}

func (realTreeFetcher) FetchPluginTree(ctx context.Context, src fetch.PluginSource, cacheRoot string, refresh bool) (string, string, error) {
	return fetch.PluginTree(ctx, src, cacheRoot, refresh)
}

// checkDriftHandler holds the dependencies for the check_drift tool handler.
type checkDriftHandler struct {
	fetcher TreeFetcher
}

// CheckDriftInput is the typed input for the check_drift tool.
type CheckDriftInput struct {
	PluginPath  string `json:"plugin_path"           jsonschema:"Absolute path to the locally-installed plugin root (directory containing .claude-plugin/plugin.json); relative paths resolve against the server process working directory"`
	Skill       string `json:"skill,omitempty"       jsonschema:"Single skill directory name to check; omit to check every skill under the local plugin's skills/ directory"`
	Marketplace string `json:"marketplace,omitempty" jsonschema:"Marketplace name override; must be paired with plugin — bypasses the x-skillhub-upstream field in the local manifest"`
	Plugin      string `json:"plugin,omitempty"      jsonschema:"Plugin name override within the marketplace; must be paired with marketplace"`
	Refresh     bool   `json:"refresh,omitempty"     jsonschema:"Force re-fetch from marketplace index and plugin tree, ignoring all caches"`
}

// CheckDriftOutput is the typed output for the check_drift tool.
type CheckDriftOutput struct {
	PluginPath          string             `json:"plugin_path"`
	PluginName          string             `json:"plugin_name,omitempty"`
	UpstreamMarketplace string             `json:"upstream_marketplace,omitempty"`
	UpstreamPlugin      string             `json:"upstream_plugin,omitempty"`
	CommitSHA           string             `json:"commit_sha,omitempty"`
	PluginStatus        string             `json:"plugin_status"`
	Skills              []SkillDriftResult `json:"skills"`
}

// SkillDriftResult reports the drift outcome for a single skill directory.
type SkillDriftResult struct {
	Skill             string   `json:"skill"`
	Status            string   `json:"status"` // up-to-date | drifted | missing-local | missing-upstream
	DriftedFiles      []string `json:"drifted_files,omitempty"`
	LocalOnlyFiles    []string `json:"local_only_files,omitempty"`
	UpstreamOnlyFiles []string `json:"upstream_only_files,omitempty"`
}

// manifestUpstreamDecl reads x-skillhub-upstream from a plugin.json without
// modifying rawPluginManifest.
type manifestUpstreamDecl struct {
	XSkillhubUpstream *struct {
		Marketplace string `json:"marketplace"`
		Plugin      string `json:"plugin"`
	} `json:"x-skillhub-upstream"`
}

// pluginSourceDescriptor is the JSON object form of a marketplace plugin source.
type pluginSourceDescriptor struct {
	Source string `json:"source"` // github | url | git-subdir | npm
	Repo   string `json:"repo"`   // github: "owner/repo"
	URL    string `json:"url"`    // url, git-subdir: clone URL
	Path   string `json:"path"`   // git-subdir: subpath within repo
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
}

// NewCheckDrift returns the check_drift tool declaration.
func NewCheckDrift() Tool {
	return Tool{
		Name:        "check_drift",
		Description: "Detect whether a locally installed plugin skill has diverged from its canonical version in a configured marketplace source. Returns drift status and a summary of changes.",
		Register: func(s *mcp.Server) {
			mcp.AddTool(s, &mcp.Tool{
				Name:        "check_drift",
				Description: "Detect whether a locally installed plugin skill has diverged from its canonical version in a configured marketplace source. Returns drift status and a summary of changes.",
			}, HandleCheckDrift)
		},
	}
}

// HandleCheckDrift is the generic typed handler for the check_drift tool.
func HandleCheckDrift(ctx context.Context, req *mcp.CallToolRequest, input CheckDriftInput) (*mcp.CallToolResult, CheckDriftOutput, error) {
	return (&checkDriftHandler{fetcher: realTreeFetcher{}}).handle(ctx, req, input)
}

func (h *checkDriftHandler) handle(ctx context.Context, _ *mcp.CallToolRequest, input CheckDriftInput) (*mcp.CallToolResult, CheckDriftOutput, error) {
	// Step 1: resolve plugin_path.
	pluginRoot := strings.TrimSpace(input.PluginPath)
	if pluginRoot == "" {
		return errResult(skerrors.ErrInvalidManifest, "missing required parameter: plugin_path", ""), CheckDriftOutput{}, nil
	}
	if !filepath.IsAbs(pluginRoot) {
		wd, err := os.Getwd()
		if err != nil {
			return errResult(skerrors.ErrInvalidManifest, "cannot resolve relative path", err.Error()), CheckDriftOutput{}, nil
		}
		pluginRoot = filepath.Join(wd, pluginRoot)
	}

	info, err := os.Stat(pluginRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult(skerrors.ErrPluginNotFound, "path does not exist", pluginRoot), CheckDriftOutput{}, nil
		}
		return errResult(skerrors.ErrPluginNotFound, "cannot stat path", err.Error()), CheckDriftOutput{}, nil
	}
	if !info.IsDir() {
		return errResult(skerrors.ErrInvalidManifest, "path is not a directory", pluginRoot), CheckDriftOutput{}, nil
	}

	// Step 2: read and parse plugin.json.
	manifestPath := filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")
	if _, err := os.Stat(manifestPath); err != nil {
		return errResult(skerrors.ErrPluginNotFound, "directory does not contain .claude-plugin/plugin.json", pluginRoot), CheckDriftOutput{}, nil
	}

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return errResult(skerrors.ErrPluginNotFound, "cannot read plugin.json", err.Error()), CheckDriftOutput{}, nil
	}

	var rawManifest rawPluginManifest
	if err := json.Unmarshal(data, &rawManifest); err != nil {
		return errResult(skerrors.ErrInvalidManifest, "failed to parse plugin.json", err.Error()), CheckDriftOutput{}, nil
	}

	// Step 3: determine upstream — input overrides take precedence over x-skillhub-upstream.
	marketplaceName := strings.TrimSpace(input.Marketplace)
	pluginName := strings.TrimSpace(input.Plugin)

	hasMarketplace := marketplaceName != ""
	hasPlugin := pluginName != ""

	if hasMarketplace != hasPlugin {
		return errResult(skerrors.ErrInvalidManifest,
			"marketplace and plugin overrides must be provided together; provide both or neither",
			fmt.Sprintf("marketplace=%q plugin=%q", marketplaceName, pluginName),
		), CheckDriftOutput{}, nil
	}

	if !hasMarketplace {
		// Fall back to x-skillhub-upstream extension field.
		var decl manifestUpstreamDecl
		if err := json.Unmarshal(data, &decl); err == nil && decl.XSkillhubUpstream != nil {
			mktEmpty := decl.XSkillhubUpstream.Marketplace == ""
			plugEmpty := decl.XSkillhubUpstream.Plugin == ""
			switch {
			case mktEmpty && plugEmpty:
				// Both fields absent — treat the declaration as if it were not present.
			case mktEmpty || plugEmpty:
				return errResult(skerrors.ErrInvalidManifest,
					"x-skillhub-upstream requires both marketplace and plugin fields",
					"",
				), CheckDriftOutput{}, nil
			default:
				marketplaceName = decl.XSkillhubUpstream.Marketplace
				pluginName = decl.XSkillhubUpstream.Plugin
			}
		}
	}

	if marketplaceName == "" || pluginName == "" {
		return nil, CheckDriftOutput{
			PluginPath:   pluginRoot,
			PluginName:   rawManifest.Name,
			PluginStatus: "missing-upstream",
			Skills:       []SkillDriftResult{},
		}, nil
	}

	// Step 4: load config and locate the matching marketplace source.
	cfg, err := config.Load()
	if err != nil {
		return errResult(skerrors.ErrMarketplaceUnreachable, "failed to load config", err.Error()), CheckDriftOutput{}, nil
	}

	cacheDir := config.CacheDir()

	var matchedSrc config.MarketplaceSource
	var matchedIndex *marketplace.Index

	for _, src := range cfg.MarketplaceSources {
		res := marketplace.Fetch(ctx, src, cacheDir, input.Refresh)
		if res.Index == nil {
			continue
		}
		if res.Index.Name == marketplaceName {
			matchedSrc = src
			matchedIndex = res.Index
			break
		}
	}

	if matchedIndex == nil {
		return errResult(skerrors.ErrMarketplaceNotConfigured,
			"no configured marketplace source matches",
			marketplaceName,
		), CheckDriftOutput{}, nil
	}

	// Step 5: locate the plugin entry within the marketplace index.
	var entry *marketplace.PluginEntry
	for i := range matchedIndex.Plugins {
		if matchedIndex.Plugins[i].Name == pluginName {
			entry = &matchedIndex.Plugins[i]
			break
		}
	}
	if entry == nil {
		return errResult(skerrors.ErrPluginNotFound,
			fmt.Sprintf("plugin %q not found in marketplace %q", pluginName, marketplaceName),
			"",
		), CheckDriftOutput{}, nil
	}

	// Step 6: parse the plugin source descriptor into a fetch.PluginSource.
	ps, parseErr := parsePluginSource(entry.Source, matchedSrc)
	if errors.Is(parseErr, errNpmSource) {
		return errResult(skerrors.ErrUnsupportedSource,
			"npm plugin source is not supported for drift detection in v1",
			pluginName,
		), CheckDriftOutput{}, nil
	}
	if parseErr != nil {
		return errResult(skerrors.ErrInvalidManifest, "failed to parse plugin source descriptor", parseErr.Error()), CheckDriftOutput{}, nil
	}

	// Step 7: materialise the upstream plugin tree via sparse shallow clone.
	upstreamRoot, sha, fetchErr := h.fetcher.FetchPluginTree(ctx, ps, cacheDir, input.Refresh)
	if fetchErr != nil {
		return errResult(skerrors.ErrFetchFailed, "failed to fetch plugin tree", fetchErr.Error()), CheckDriftOutput{}, nil
	}

	// Step 8: enumerate skills and run per-skill drift comparison.
	localSkillsDir := filepath.Join(pluginRoot, "skills")
	if _, statErr := os.Stat(localSkillsDir); statErr != nil {
		return nil, CheckDriftOutput{
			PluginPath:          pluginRoot,
			PluginName:          rawManifest.Name,
			UpstreamMarketplace: marketplaceName,
			UpstreamPlugin:      pluginName,
			CommitSHA:           sha,
			PluginStatus:        "no-skills-dir",
			Skills:              []SkillDriftResult{},
		}, nil
	}

	var skillNames []string
	if input.Skill != "" {
		skillNames = []string{input.Skill}
	} else {
		entries, readErr := os.ReadDir(localSkillsDir)
		if readErr != nil {
			return errResult(skerrors.ErrPluginNotFound, "cannot enumerate skills directory", readErr.Error()), CheckDriftOutput{}, nil
		}
		for _, e := range entries {
			if e.IsDir() {
				skillNames = append(skillNames, e.Name())
			}
		}
	}

	upstreamSkillsDir := filepath.Join(upstreamRoot, "skills")
	skills := make([]SkillDriftResult, 0, len(skillNames))
	for _, skillName := range skillNames {
		result := checkSkillDrift(
			filepath.Join(localSkillsDir, skillName),
			filepath.Join(upstreamSkillsDir, skillName),
		)
		result.Skill = skillName
		skills = append(skills, result)
	}

	return nil, CheckDriftOutput{
		PluginPath:          pluginRoot,
		PluginName:          rawManifest.Name,
		UpstreamMarketplace: marketplaceName,
		UpstreamPlugin:      pluginName,
		CommitSHA:           sha,
		PluginStatus:        computePluginStatus(skills),
		Skills:              skills,
	}, nil
}

// computePluginStatus derives the plugin-level status from per-skill results.
// Returns "drifted" if any skill is drifted, missing-local, or missing-upstream;
// "up-to-date" otherwise (including when there are no skills).
func computePluginStatus(skills []SkillDriftResult) string {
	for _, s := range skills {
		switch s.Status {
		case "drifted", "missing-local", "missing-upstream":
			return "drifted"
		}
	}
	return "up-to-date"
}

// parsePluginSource converts a marketplace PluginEntry.Source (raw JSON) into a
// fetch.PluginSource. Returns errNpmSource when the source kind is npm.
func parsePluginSource(raw json.RawMessage, mktSrc config.MarketplaceSource) (fetch.PluginSource, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return fetch.PluginSource{}, fmt.Errorf("plugin source field is absent")
	}

	// String form: "./relative/path" — path within the marketplace repo itself.
	var str string
	if json.Unmarshal(raw, &str) == nil {
		if strings.HasPrefix(str, "./") {
			return fetch.PluginSource{
				RepoURL:          mktSrc.URL,
				Subpath:          strings.TrimPrefix(str, "./"),
				Ref:              mktSrc.Ref,
				CredentialEnvVar: mktSrc.CredentialEnvVar,
			}, nil
		}
		return fetch.PluginSource{}, fmt.Errorf("string source %q does not start with ./", str)
	}

	// Object form.
	var desc pluginSourceDescriptor
	if err := json.Unmarshal(raw, &desc); err != nil {
		return fetch.PluginSource{}, fmt.Errorf("parse source descriptor: %w", err)
	}

	switch desc.Source {
	case "npm":
		return fetch.PluginSource{}, errNpmSource
	case "github":
		return fetch.PluginSource{
			RepoURL: "https://github.com/" + desc.Repo,
			Ref:     desc.Ref,
			SHA:     desc.SHA,
		}, nil
	case "url":
		return fetch.PluginSource{
			RepoURL: desc.URL,
			Ref:     desc.Ref,
			SHA:     desc.SHA,
		}, nil
	case "git-subdir":
		return fetch.PluginSource{
			RepoURL: desc.URL,
			Subpath: desc.Path,
			Ref:     desc.Ref,
			SHA:     desc.SHA,
		}, nil
	default:
		return fetch.PluginSource{}, fmt.Errorf("unknown plugin source kind: %q", desc.Source)
	}
}

// checkSkillDrift compares a local skill directory against an upstream skill
// directory byte-for-byte. The Skill field is populated by the caller.
func checkSkillDrift(localDir, upstreamDir string) SkillDriftResult {
	localExists := dirExists(localDir)
	upstreamExists := dirExists(upstreamDir)

	switch {
	case !localExists && !upstreamExists:
		return SkillDriftResult{Status: "missing-upstream"}
	case !localExists:
		return SkillDriftResult{Status: "missing-local"}
	case !upstreamExists:
		return SkillDriftResult{Status: "missing-upstream"}
	}

	localFiles := walkDirContents(localDir)
	upstreamFiles := walkDirContents(upstreamDir)

	var drifted, localOnly, upstreamOnly []string

	for rel, localData := range localFiles {
		if upstreamData, ok := upstreamFiles[rel]; !ok {
			localOnly = append(localOnly, rel)
		} else if !bytes.Equal(localData, upstreamData) {
			drifted = append(drifted, rel)
		}
	}
	for rel := range upstreamFiles {
		if _, ok := localFiles[rel]; !ok {
			upstreamOnly = append(upstreamOnly, rel)
		}
	}

	if len(drifted) == 0 && len(localOnly) == 0 && len(upstreamOnly) == 0 {
		return SkillDriftResult{Status: "up-to-date"}
	}

	result := SkillDriftResult{Status: "drifted"}
	if len(drifted) > 0 {
		sort.Strings(drifted)
		result.DriftedFiles = drifted
	}
	if len(localOnly) > 0 {
		sort.Strings(localOnly)
		result.LocalOnlyFiles = localOnly
	}
	if len(upstreamOnly) > 0 {
		sort.Strings(upstreamOnly)
		result.UpstreamOnlyFiles = upstreamOnly
	}
	return result
}

func dirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// walkDirContents returns a map of slash-separated relative path → file bytes
// for every file under dir. Unreadable files are silently skipped.
// TODO: file read errors and symlinks are silently swallowed; a future revision should surface these as per-skill errors rather than dropping files from the comparison map.
func walkDirContents(dir string) map[string][]byte {
	files := map[string][]byte{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	return files
}
