package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/config"
	skerrors "github.com/jaimegago/skillhub/internal/errors"
	"github.com/jaimegago/skillhub/internal/marketplace"
)

// ProposeSkillChangesInput is the typed input for the propose_skill_changes tool.
type ProposeSkillChangesInput struct {
	PluginPath  string `json:"plugin_path"           jsonschema:"Absolute path to the locally-installed plugin root (directory containing .claude-plugin/plugin.json)"`
	Skill       string `json:"skill"                 jsonschema:"Skill directory name to propose changes for"`
	Marketplace string `json:"marketplace,omitempty" jsonschema:"Marketplace name override; must be paired with plugin"`
	Plugin      string `json:"plugin,omitempty"      jsonschema:"Plugin name override within the marketplace; must be paired with marketplace"`
	DryRun      bool   `json:"dry_run,omitempty"     jsonschema:"Preview changes without creating a PR; returns diff and proposed branch name"`
	Title       string `json:"title,omitempty"       jsonschema:"PR title; auto-generated from plugin and skill names when omitted"`
	Body        string `json:"body,omitempty"        jsonschema:"PR description body; auto-generated when omitted"`
	Branch      string `json:"branch,omitempty"      jsonschema:"Branch name to create; auto-generated as skillhub/propose-{skill}-{timestamp} when omitted"`
}

// ProposeSkillChangesOutput is the typed output for the propose_skill_changes tool.
type ProposeSkillChangesOutput struct {
	PluginPath          string `json:"plugin_path"`
	PluginName          string `json:"plugin_name,omitempty"`
	Skill               string `json:"skill"`
	UpstreamMarketplace string `json:"upstream_marketplace,omitempty"`
	UpstreamPlugin      string `json:"upstream_plugin,omitempty"`
	CommitSHA           string `json:"commit_sha,omitempty"`
	// Status is one of: proposed | dry-run | nothing-to-propose | missing-upstream
	Status string `json:"status"`
	Branch string `json:"branch,omitempty"`
	PRURL  string `json:"pr_url,omitempty"`
	Diff   string `json:"diff,omitempty"`
	DryRun bool   `json:"dry_run"`
}

// githubAPI is the interface for GitHub API calls used by propose_skill_changes.
// Swapped out in tests.
type githubAPI interface {
	GetUser(ctx context.Context) (string, error)
	EnsureFork(ctx context.Context, owner, repo string) (string, error)
	GetFileSHA(ctx context.Context, owner, repo, branch, path string) (string, error)
	UpsertFile(ctx context.Context, owner, repo, branch, path, message string, content []byte, sha string) error
	GetDefaultBranch(ctx context.Context, owner, repo string) (string, error)
	GetBranchSHA(ctx context.Context, owner, repo, branch string) (string, error)
	CreateBranch(ctx context.Context, owner, repo, branch, fromSHA string) error
	CreatePR(ctx context.Context, baseOwner, baseRepo, headOwner, headBranch, title, body, base string) (string, error)
}

// proposeSkillHandler holds dependencies for propose_skill_changes.
type proposeSkillHandler struct {
	fetcher TreeFetcher
	// ghClientFor returns a githubAPI for the given token and base URL.
	// Overridable in tests.
	ghClientFor func(token, baseURL string) githubAPI
}

// NewProposeSkillChanges returns the propose_skill_changes tool declaration.
func NewProposeSkillChanges() Tool {
	const desc = "Open a merge request against the configured marketplace source repository proposing the local skill changes. Set dry_run to preview without creating the MR."
	return Tool{
		Name:        "propose_skill_changes",
		Description: desc,
		Register: func(s *mcp.Server) {
			mcp.AddTool(s, &mcp.Tool{
				Name:        "propose_skill_changes",
				Description: desc,
			}, HandleProposeSkillChanges)
		},
	}
}

// HandleProposeSkillChanges is the generic typed handler for propose_skill_changes.
func HandleProposeSkillChanges(ctx context.Context, req *mcp.CallToolRequest, input ProposeSkillChangesInput) (*mcp.CallToolResult, ProposeSkillChangesOutput, error) {
	h := &proposeSkillHandler{
		fetcher:     realTreeFetcher{},
		ghClientFor: func(token, baseURL string) githubAPI { return &realGitHubClient{token: token, baseURL: baseURL} },
	}
	return h.handle(ctx, req, input)
}

func (h *proposeSkillHandler) handle(ctx context.Context, _ *mcp.CallToolRequest, input ProposeSkillChangesInput) (*mcp.CallToolResult, ProposeSkillChangesOutput, error) {
	// Step 1: validate required fields.
	pluginRoot := strings.TrimSpace(input.PluginPath)
	if pluginRoot == "" {
		return errResult(skerrors.ErrInvalidInput, "missing required parameter: plugin_path", ""), ProposeSkillChangesOutput{}, nil
	}
	skillName := strings.TrimSpace(input.Skill)
	if skillName == "" {
		return errResult(skerrors.ErrInvalidInput, "missing required parameter: skill", ""), ProposeSkillChangesOutput{}, nil
	}

	// Step 2: resolve plugin_path.
	if !filepath.IsAbs(pluginRoot) {
		wd, err := os.Getwd()
		if err != nil {
			return errResult(skerrors.ErrInvalidInput, "cannot resolve relative path", err.Error()), ProposeSkillChangesOutput{}, nil
		}
		pluginRoot = filepath.Join(wd, pluginRoot)
	}
	info, err := os.Stat(pluginRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult(skerrors.ErrPluginNotFound, "path does not exist", pluginRoot), ProposeSkillChangesOutput{}, nil
		}
		return errResult(skerrors.ErrPluginNotFound, "cannot stat path", err.Error()), ProposeSkillChangesOutput{}, nil
	}
	if !info.IsDir() {
		return errResult(skerrors.ErrInvalidManifest, "path is not a directory", pluginRoot), ProposeSkillChangesOutput{}, nil
	}

	// Step 3: read and parse plugin.json.
	manifestPath := filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")
	if _, err := os.Stat(manifestPath); err != nil {
		return errResult(skerrors.ErrPluginNotFound, "directory does not contain .claude-plugin/plugin.json", pluginRoot), ProposeSkillChangesOutput{}, nil
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return errResult(skerrors.ErrPluginNotFound, "cannot read plugin.json", err.Error()), ProposeSkillChangesOutput{}, nil
	}
	var rawManifest rawPluginManifest
	if err := json.Unmarshal(data, &rawManifest); err != nil {
		return errResult(skerrors.ErrInvalidManifest, "failed to parse plugin.json", err.Error()), ProposeSkillChangesOutput{}, nil
	}

	// Step 4: determine upstream.
	marketplaceName := strings.TrimSpace(input.Marketplace)
	pluginName := strings.TrimSpace(input.Plugin)
	hasMarketplace := marketplaceName != ""
	hasPlugin := pluginName != ""
	if hasMarketplace != hasPlugin {
		return errResult(skerrors.ErrInvalidManifest,
			"marketplace and plugin overrides must be provided together; provide both or neither",
			fmt.Sprintf("marketplace=%q plugin=%q", marketplaceName, pluginName),
		), ProposeSkillChangesOutput{}, nil
	}
	if !hasMarketplace {
		var decl manifestUpstreamDecl
		if err := json.Unmarshal(data, &decl); err == nil && decl.XSkillhubUpstream != nil {
			mktEmpty := decl.XSkillhubUpstream.Marketplace == ""
			plugEmpty := decl.XSkillhubUpstream.Plugin == ""
			switch {
			case mktEmpty && plugEmpty:
			case mktEmpty || plugEmpty:
				return errResult(skerrors.ErrInvalidManifest,
					"x-skillhub-upstream requires both marketplace and plugin fields", "",
				), ProposeSkillChangesOutput{}, nil
			default:
				marketplaceName = decl.XSkillhubUpstream.Marketplace
				pluginName = decl.XSkillhubUpstream.Plugin
			}
		}
	}
	if marketplaceName == "" || pluginName == "" {
		return nil, ProposeSkillChangesOutput{
			PluginPath: pluginRoot,
			PluginName: rawManifest.Name,
			Skill:      skillName,
			Status:     "missing-upstream",
			DryRun:     input.DryRun,
		}, nil
	}

	// Step 5: load config and find the marketplace source.
	cfg, err := config.Load()
	if err != nil {
		return errResult(skerrors.ErrMarketplaceUnreachable, "failed to load config", err.Error()), ProposeSkillChangesOutput{}, nil
	}
	cacheDir := config.CacheDir()

	var matchedSrc config.MarketplaceSource
	var matchedIndex *marketplace.Index
	for _, src := range cfg.MarketplaceSources {
		res := marketplace.Fetch(ctx, src, cacheDir, false)
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
		return errResult(skerrors.ErrMarketplaceNotConfigured, "no configured marketplace source matches", marketplaceName), ProposeSkillChangesOutput{}, nil
	}

	// Step 6: locate plugin entry.
	var entry *marketplace.PluginEntry
	for i := range matchedIndex.Plugins {
		if matchedIndex.Plugins[i].Name == pluginName {
			entry = &matchedIndex.Plugins[i]
			break
		}
	}
	if entry == nil {
		return errResult(skerrors.ErrPluginNotFound,
			fmt.Sprintf("plugin %q not found in marketplace %q", pluginName, marketplaceName), "",
		), ProposeSkillChangesOutput{}, nil
	}

	// Step 7: parse source and fetch upstream tree.
	ps, parseErr := parsePluginSource(entry.Source, matchedSrc)
	if errors.Is(parseErr, errNpmSource) {
		return errResult(skerrors.ErrUnsupportedSource, "npm plugin source is not supported", pluginName), ProposeSkillChangesOutput{}, nil
	}
	if parseErr != nil {
		return errResult(skerrors.ErrInvalidManifest, "failed to parse plugin source descriptor", parseErr.Error()), ProposeSkillChangesOutput{}, nil
	}

	// Only GitHub-hosted plugin repos are supported for live PR creation.
	// Check the plugin's own RepoURL rather than the marketplace index host type.
	if !input.DryRun {
		if _, _, err := parseGitHubOwnerRepo(ps.RepoURL); err != nil {
			return errResult(skerrors.ErrUnsupportedSource,
				"live PR creation is only supported for GitHub-hosted plugins; use dry_run=true to preview",
				ps.RepoURL,
			), ProposeSkillChangesOutput{}, nil
		}
	}

	upstreamRoot, sha, fetchErr := h.fetcher.FetchPluginTree(ctx, ps, cacheDir, false)
	if fetchErr != nil {
		return errResult(skerrors.ErrFetchFailed, "failed to fetch plugin tree", fetchErr.Error()), ProposeSkillChangesOutput{}, nil
	}

	// Step 9: compute the diff (detect what changed).
	localSkillDir := filepath.Join(pluginRoot, "skills", skillName)
	upstreamSkillDir := filepath.Join(upstreamRoot, "skills", skillName)

	if !dirExists(localSkillDir) {
		return errResult(skerrors.ErrSkillNotFound, fmt.Sprintf("skill %q not found in local plugin", skillName), ""), ProposeSkillChangesOutput{}, nil
	}

	diffText, diffErr := unifiedDiff(ctx, upstreamSkillDir, localSkillDir, pluginRoot)
	if diffErr != nil {
		return errResult(skerrors.ErrFetchFailed, "failed to generate diff", diffErr.Error()), ProposeSkillChangesOutput{}, nil
	}

	base := ProposeSkillChangesOutput{
		PluginPath:          pluginRoot,
		PluginName:          rawManifest.Name,
		Skill:               skillName,
		UpstreamMarketplace: marketplaceName,
		UpstreamPlugin:      pluginName,
		CommitSHA:           sha,
		DryRun:              input.DryRun,
		Diff:                diffText,
	}

	if diffText == "" {
		base.Status = "nothing-to-propose"
		return nil, base, nil
	}

	branchName := strings.TrimSpace(input.Branch)
	if branchName == "" {
		branchName = fmt.Sprintf("skillhub/propose-%s-%d", skillName, time.Now().Unix())
	}
	base.Branch = branchName

	if input.DryRun {
		base.Status = "dry-run"
		return nil, base, nil
	}

	// Step 10: live GitHub PR creation.
	token := ""
	if matchedSrc.CredentialEnvVar != "" {
		token = os.Getenv(matchedSrc.CredentialEnvVar)
	}
	if token == "" {
		return errResult(skerrors.ErrAuthFailed,
			fmt.Sprintf("GitHub token not found: set the %q environment variable", matchedSrc.CredentialEnvVar),
			"",
		), ProposeSkillChangesOutput{}, nil
	}

	// Parse owner/repo from the plugin's own RepoURL (already verified as GitHub above).
	upstreamOwner, upstreamRepo, parseURLErr := parseGitHubOwnerRepo(ps.RepoURL)
	if parseURLErr != nil {
		return errResult(skerrors.ErrInvalidManifest, "failed to parse GitHub URL from marketplace source", parseURLErr.Error()), ProposeSkillChangesOutput{}, nil
	}

	gh := h.ghClientFor(token, "https://api.github.com")

	// Get the authenticated user's login (fork owner).
	forkOwner, userErr := gh.GetUser(ctx)
	if userErr != nil {
		return errResult(skerrors.ErrAuthFailed, "failed to get GitHub user", userErr.Error()), ProposeSkillChangesOutput{}, nil
	}

	// Ensure fork exists.
	if _, forkErr := gh.EnsureFork(ctx, upstreamOwner, upstreamRepo); forkErr != nil {
		return errResult(skerrors.ErrFetchFailed, "failed to ensure fork exists", forkErr.Error()), ProposeSkillChangesOutput{}, nil
	}

	// Find the default branch of the upstream repo.
	defaultBranch, branchErr := gh.GetDefaultBranch(ctx, upstreamOwner, upstreamRepo)
	if branchErr != nil {
		return errResult(skerrors.ErrFetchFailed, "failed to get default branch", branchErr.Error()), ProposeSkillChangesOutput{}, nil
	}

	// Get the SHA of the default branch tip in the fork.
	baseSHA, shaErr := gh.GetBranchSHA(ctx, forkOwner, upstreamRepo, defaultBranch)
	if shaErr != nil {
		return errResult(skerrors.ErrFetchFailed, "failed to get branch SHA from fork", shaErr.Error()), ProposeSkillChangesOutput{}, nil
	}

	// Create the proposal branch on the fork.
	if createErr := gh.CreateBranch(ctx, forkOwner, upstreamRepo, branchName, baseSHA); createErr != nil {
		return errResult(skerrors.ErrFetchFailed, "failed to create branch on fork", createErr.Error()), ProposeSkillChangesOutput{}, nil
	}

	// Compute the path prefix in the upstream repo for this skill.
	// Convention: skills live at skills/{skillName}/ relative to the plugin subpath.
	skillSubpath := ""
	if ps.Subpath != "" {
		skillSubpath = ps.Subpath + "/skills/" + skillName
	} else {
		skillSubpath = "skills/" + skillName
	}

	// Upload each changed file.
	localFiles, _, _ := walkDirContents(localSkillDir)
	commitMsg := fmt.Sprintf("feat(skill): propose changes to %s/%s", pluginName, skillName)
	for rel, content := range localFiles {
		remotePath := skillSubpath + "/" + rel
		existingSHA, _ := gh.GetFileSHA(ctx, forkOwner, upstreamRepo, branchName, remotePath)
		if uploadErr := gh.UpsertFile(ctx, forkOwner, upstreamRepo, branchName, remotePath, commitMsg, content, existingSHA); uploadErr != nil {
			return errResult(skerrors.ErrFetchFailed, fmt.Sprintf("failed to upload file %s", rel), uploadErr.Error()), ProposeSkillChangesOutput{}, nil
		}
	}

	// Open the PR.
	prTitle := strings.TrimSpace(input.Title)
	if prTitle == "" {
		prTitle = fmt.Sprintf("Propose changes to %s/%s", pluginName, skillName)
	}
	prBody := strings.TrimSpace(input.Body)
	if prBody == "" {
		prBody = fmt.Sprintf("Proposed skill changes from local plugin `%s`.\n\nGenerated by [skillhub](https://github.com/jaimegago/skillhub).", rawManifest.Name)
	}

	prURL, prErr := gh.CreatePR(ctx, upstreamOwner, upstreamRepo, forkOwner, branchName, prTitle, prBody, defaultBranch)
	if prErr != nil {
		return errResult(skerrors.ErrFetchFailed, "failed to create pull request", prErr.Error()), ProposeSkillChangesOutput{}, nil
	}

	base.Status = "proposed"
	base.PRURL = prURL
	return nil, base, nil
}

// parseGitHubOwnerRepo extracts owner and repo name from a GitHub URL.
// Handles https://github.com/owner/repo and https://github.com/owner/repo.git forms.
// Returns an error for any non-GitHub URL.
func parseGitHubOwnerRepo(rawURL string) (owner, repo string, err error) {
	const prefix = "https://github.com/"
	u := strings.TrimSuffix(rawURL, ".git")
	if !strings.HasPrefix(u, prefix) {
		return "", "", fmt.Errorf("not a GitHub URL: %q", rawURL)
	}
	u = strings.TrimPrefix(u, prefix)
	parts := strings.SplitN(u, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("cannot parse GitHub owner/repo from URL %q", rawURL)
	}
	return parts[0], parts[1], nil
}

// --- realGitHubClient ---

type realGitHubClient struct {
	token   string
	baseURL string
}

func (c *realGitHubClient) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(req)
}

func (c *realGitHubClient) GetUser(ctx context.Context) (string, error) {
	resp, err := c.do(ctx, "GET", "/user", nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GET /user: %s", resp.Status)
	}
	var v struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.Login, nil
}

func (c *realGitHubClient) EnsureFork(ctx context.Context, owner, repo string) (string, error) {
	body := strings.NewReader(`{}`)
	resp, err := c.do(ctx, "POST", fmt.Sprintf("/repos/%s/%s/forks", owner, repo), body)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	// 202 = fork created/exists
	if resp.StatusCode != 202 && resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("POST /repos/.../forks: %s: %s", resp.Status, b)
	}
	var v struct {
		FullName string `json:"full_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.FullName, nil
}

func (c *realGitHubClient) GetDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	resp, err := c.do(ctx, "GET", fmt.Sprintf("/repos/%s/%s", owner, repo), nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GET /repos/%s/%s: %s", owner, repo, resp.Status)
	}
	var v struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.DefaultBranch, nil
}

func (c *realGitHubClient) GetBranchSHA(ctx context.Context, owner, repo, branch string) (string, error) {
	resp, err := c.do(ctx, "GET", fmt.Sprintf("/repos/%s/%s/git/ref/heads/%s", owner, repo, branch), nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GET ref heads/%s: %s", branch, resp.Status)
	}
	var v struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.Object.SHA, nil
}

func (c *realGitHubClient) CreateBranch(ctx context.Context, owner, repo, branch, fromSHA string) error {
	payload, _ := json.Marshal(map[string]string{
		"ref": "refs/heads/" + branch,
		"sha": fromSHA,
	})
	resp, err := c.do(ctx, "POST", fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo), strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST git/refs: %s: %s", resp.Status, b)
	}
	return nil
}

func (c *realGitHubClient) GetFileSHA(ctx context.Context, owner, repo, branch, path string) (string, error) {
	resp, err := c.do(ctx, "GET", fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", owner, repo, path, branch), nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == 404 {
		return "", nil // file doesn't exist yet
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GET contents/%s: %s", path, resp.Status)
	}
	var v struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.SHA, nil
}

func (c *realGitHubClient) UpsertFile(ctx context.Context, owner, repo, branch, path, message string, content []byte, sha string) error {
	m := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  branch,
	}
	if sha != "" {
		m["sha"] = sha
	}
	payload, _ := json.Marshal(m)
	resp, err := c.do(ctx, "PUT", fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path), strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT contents/%s: %s: %s", path, resp.Status, b)
	}
	return nil
}

func (c *realGitHubClient) CreatePR(ctx context.Context, baseOwner, baseRepo, headOwner, headBranch, title, body, base string) (string, error) {
	payload, _ := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
		"head":  headOwner + ":" + headBranch,
		"base":  base,
	})
	resp, err := c.do(ctx, "POST", fmt.Sprintf("/repos/%s/%s/pulls", baseOwner, baseRepo), strings.NewReader(string(payload)))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("POST /pulls: %s: %s", resp.Status, b)
	}
	var v struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.HTMLURL, nil
}
