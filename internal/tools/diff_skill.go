package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/config"
	skerrors "github.com/jaimegago/skillhub/internal/errors"
	"github.com/jaimegago/skillhub/internal/marketplace"
)

// DiffSkillInput is the typed input for the diff_skill tool.
type DiffSkillInput struct {
	PluginPath  string `json:"plugin_path"           jsonschema:"Absolute path to the locally-installed plugin root (directory containing .claude-plugin/plugin.json)"`
	Skill       string `json:"skill"                 jsonschema:"Skill directory name to diff (e.g. 'my-skill'); required"`
	Marketplace string `json:"marketplace,omitempty" jsonschema:"Marketplace name override; must be paired with plugin — bypasses x-skillhub-upstream in the local manifest"`
	Plugin      string `json:"plugin,omitempty"      jsonschema:"Plugin name override within the marketplace; must be paired with marketplace"`
	Refresh     bool   `json:"refresh,omitempty"     jsonschema:"Force re-fetch from marketplace index and plugin tree, ignoring all caches"`
	Limit       int    `json:"limit,omitempty"       jsonschema:"Maximum diff lines per page; 0 returns all lines"`
	Cursor      string `json:"cursor,omitempty"      jsonschema:"Opaque cursor from a previous response; omit or leave empty for the first page"`
}

// DiffSkillOutput is the typed output for the diff_skill tool.
type DiffSkillOutput struct {
	PluginPath          string `json:"plugin_path"`
	PluginName          string `json:"plugin_name,omitempty"`
	Skill               string `json:"skill"`
	UpstreamMarketplace string `json:"upstream_marketplace,omitempty"`
	UpstreamPlugin      string `json:"upstream_plugin,omitempty"`
	CommitSHA           string `json:"commit_sha,omitempty"`
	// Status mirrors check_drift SkillDriftResult statuses.
	Status     string `json:"status"` // up-to-date | drifted | missing-local | missing-upstream
	Diff       string `json:"diff"`
	TotalLines int    `json:"total_lines"`
	Truncated  bool   `json:"truncated"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// diffSkillHandler holds the dependencies for the diff_skill tool handler.
type diffSkillHandler struct {
	fetcher TreeFetcher
}

// NewDiffSkill returns the diff_skill tool declaration.
func NewDiffSkill() Tool {
	const desc = "Return a unified diff between the local version of a named skill and its canonical marketplace version. Output may be large; use limit and cursor for pagination."
	return Tool{
		Name:        "diff_skill",
		Description: desc,
		Register: func(s *mcp.Server) {
			mcp.AddTool(s, &mcp.Tool{
				Name:        "diff_skill",
				Description: desc,
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			}, HandleDiffSkill)
		},
	}
}

// HandleDiffSkill is the generic typed handler for the diff_skill tool.
func HandleDiffSkill(ctx context.Context, req *mcp.CallToolRequest, input DiffSkillInput) (*mcp.CallToolResult, DiffSkillOutput, error) {
	return (&diffSkillHandler{fetcher: realTreeFetcher{}}).handle(ctx, req, input)
}

func (h *diffSkillHandler) handle(ctx context.Context, _ *mcp.CallToolRequest, input DiffSkillInput) (*mcp.CallToolResult, DiffSkillOutput, error) {
	// Step 1: validate required fields.
	pluginRoot := strings.TrimSpace(input.PluginPath)
	if pluginRoot == "" {
		return errResult(skerrors.ErrInvalidInput, "missing required parameter: plugin_path", ""), DiffSkillOutput{}, nil
	}
	skillName := strings.TrimSpace(input.Skill)
	if skillName == "" {
		return errResult(skerrors.ErrInvalidInput, "missing required parameter: skill", ""), DiffSkillOutput{}, nil
	}

	// Step 2: resolve plugin_path (mirrors check_drift).
	if !filepath.IsAbs(pluginRoot) {
		wd, err := os.Getwd()
		if err != nil {
			return errResult(skerrors.ErrInvalidInput, "cannot resolve relative path", err.Error()), DiffSkillOutput{}, nil
		}
		pluginRoot = filepath.Join(wd, pluginRoot)
	}

	info, err := os.Stat(pluginRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult(skerrors.ErrPluginNotFound, "path does not exist", pluginRoot), DiffSkillOutput{}, nil
		}
		return errResult(skerrors.ErrPluginNotFound, "cannot stat path", err.Error()), DiffSkillOutput{}, nil
	}
	if !info.IsDir() {
		return errResult(skerrors.ErrInvalidManifest, "path is not a directory", pluginRoot), DiffSkillOutput{}, nil
	}

	// Step 3: read and parse plugin.json.
	manifestPath := filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")
	if _, err := os.Stat(manifestPath); err != nil {
		return errResult(skerrors.ErrPluginNotFound, "directory does not contain .claude-plugin/plugin.json", pluginRoot), DiffSkillOutput{}, nil
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return errResult(skerrors.ErrPluginNotFound, "cannot read plugin.json", err.Error()), DiffSkillOutput{}, nil
	}
	var rawManifest rawPluginManifest
	if err := json.Unmarshal(data, &rawManifest); err != nil {
		return errResult(skerrors.ErrInvalidManifest, "failed to parse plugin.json", err.Error()), DiffSkillOutput{}, nil
	}

	// Step 4: determine upstream (mirrors check_drift).
	marketplaceName := strings.TrimSpace(input.Marketplace)
	pluginName := strings.TrimSpace(input.Plugin)
	hasMarketplace := marketplaceName != ""
	hasPlugin := pluginName != ""
	if hasMarketplace != hasPlugin {
		return errResult(skerrors.ErrInvalidManifest,
			"marketplace and plugin overrides must be provided together; provide both or neither",
			fmt.Sprintf("marketplace=%q plugin=%q", marketplaceName, pluginName),
		), DiffSkillOutput{}, nil
	}
	if !hasMarketplace {
		var decl manifestUpstreamDecl
		if err := json.Unmarshal(data, &decl); err == nil && decl.XSkillhubUpstream != nil {
			mktEmpty := decl.XSkillhubUpstream.Marketplace == ""
			plugEmpty := decl.XSkillhubUpstream.Plugin == ""
			switch {
			case mktEmpty && plugEmpty:
				// Both absent — treat as not present.
			case mktEmpty || plugEmpty:
				return errResult(skerrors.ErrInvalidManifest,
					"x-skillhub-upstream requires both marketplace and plugin fields",
					"",
				), DiffSkillOutput{}, nil
			default:
				marketplaceName = decl.XSkillhubUpstream.Marketplace
				pluginName = decl.XSkillhubUpstream.Plugin
			}
		}
	}
	if marketplaceName == "" || pluginName == "" {
		return nil, DiffSkillOutput{
			PluginPath: pluginRoot,
			PluginName: rawManifest.Name,
			Skill:      skillName,
			Status:     "missing-upstream",
			Diff:       "",
		}, nil
	}

	// Step 5: load config and locate the matching marketplace source.
	cfg, err := config.Load()
	if err != nil {
		return errResult(skerrors.ErrMarketplaceUnreachable, "failed to load config", err.Error()), DiffSkillOutput{}, nil
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
		), DiffSkillOutput{}, nil
	}

	// Step 6: locate the plugin entry within the index.
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
		), DiffSkillOutput{}, nil
	}

	// Step 7: parse source and fetch the upstream plugin tree.
	ps, parseErr := parsePluginSource(entry.Source, matchedSrc)
	if errors.Is(parseErr, errNpmSource) {
		return errResult(skerrors.ErrUnsupportedSource,
			"npm plugin source is not supported for diff in v1",
			pluginName,
		), DiffSkillOutput{}, nil
	}
	if parseErr != nil {
		return errResult(skerrors.ErrInvalidManifest, "failed to parse plugin source descriptor", parseErr.Error()), DiffSkillOutput{}, nil
	}

	upstreamRoot, sha, fetchErr := h.fetcher.FetchPluginTree(ctx, ps, cacheDir, input.Refresh)
	if fetchErr != nil {
		return errResult(skerrors.ErrFetchFailed, "failed to fetch plugin tree", fetchErr.Error()), DiffSkillOutput{}, nil
	}

	// Step 8: check skill directories exist.
	localSkillDir := filepath.Join(pluginRoot, "skills", skillName)
	upstreamSkillDir := filepath.Join(upstreamRoot, "skills", skillName)

	localExists := dirExists(localSkillDir)
	upstreamExists := dirExists(upstreamSkillDir)

	base := DiffSkillOutput{
		PluginPath:          pluginRoot,
		PluginName:          rawManifest.Name,
		Skill:               skillName,
		UpstreamMarketplace: marketplaceName,
		UpstreamPlugin:      pluginName,
		CommitSHA:           sha,
	}

	switch {
	case !localExists && !upstreamExists:
		base.Status = "missing-upstream"
		return nil, base, nil
	case !localExists:
		base.Status = "missing-local"
		return nil, base, nil
	case !upstreamExists:
		base.Status = "missing-upstream"
		return nil, base, nil
	}

	// Step 9: generate the unified diff.
	raw, err := unifiedDiff(ctx, upstreamSkillDir, localSkillDir, pluginRoot)
	if err != nil {
		return errResult(skerrors.ErrFetchFailed, "failed to generate diff", err.Error()), DiffSkillOutput{}, nil
	}

	if raw == "" {
		base.Status = "up-to-date"
		return nil, base, nil
	}
	base.Status = "drifted"

	// Step 10: apply pagination.
	lines := strings.Split(raw, "\n")
	// Remove the trailing empty element that Split adds after a final newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	base.TotalLines = len(lines)

	page, nextCursor := paginateDiffLines(lines, input.Limit, input.Cursor)
	base.Diff = strings.Join(page, "\n")
	if len(page) > 0 {
		base.Diff += "\n"
	}
	base.NextCursor = nextCursor
	base.Truncated = nextCursor != ""

	return nil, base, nil
}

// unifiedDiff runs `git diff --no-index` between upstreamDir and localDir,
// returning the diff text with absolute paths replaced by plugin-root-relative ones.
// An empty string means the directories are identical.
func unifiedDiff(ctx context.Context, upstreamDir, localDir, pluginRoot string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "--", upstreamDir, localDir)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	if err := cmd.Run(); err != nil {
		// exit code 1 from `git diff` means "files differ" — that is not an error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return cleanDiffPaths(buf.String(), upstreamDir, localDir, pluginRoot), nil
		}
		return "", fmt.Errorf("git diff --no-index: %w: %s", err, buf.String())
	}
	return "", nil // exit 0 = identical
}

// cleanDiffPaths replaces absolute upstream/local paths in diff headers with
// readable relative labels ("upstream/skills/name" and "local/skills/name").
func cleanDiffPaths(diff, upstreamDir, localDir, pluginRoot string) string {
	// Build the label suffixes relative to pluginRoot for readability.
	upstreamRel := relOrAbs(upstreamDir, pluginRoot)
	localRel := relOrAbs(localDir, pluginRoot)

	diff = strings.ReplaceAll(diff, upstreamDir, "upstream/"+upstreamRel)
	diff = strings.ReplaceAll(diff, localDir, "local/"+localRel)
	return diff
}

func relOrAbs(path, base string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// paginateDiffLines slices lines using limit+cursor pagination.
// limit=0 returns all lines. The cursor is a decimal line-offset string.
func paginateDiffLines(lines []string, limit int, cursor string) ([]string, string) {
	if limit <= 0 {
		return lines, ""
	}
	offset := 0
	if cursor != "" {
		if n, err := strconv.Atoi(cursor); err == nil && n > 0 {
			offset = n
		}
	}
	total := len(lines)
	if offset >= total {
		return []string{}, ""
	}
	end := offset + limit
	if end >= total {
		return lines[offset:], ""
	}
	return lines[offset:end], strconv.Itoa(end)
}
