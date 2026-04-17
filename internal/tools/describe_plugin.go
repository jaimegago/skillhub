package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	skerrors "github.com/jaime-gago/skillhub/internal/errors"
)

// TODO(large-output): declare size annotation and paginate when implemented.
// Verify exact annotation field name (likely anthropic/maxResultSizeChars) against
// current Claude Code docs before adding — do not assume spec provenance.

// DescribePluginInput is the typed input for the describe_plugin tool.
type DescribePluginInput struct {
	Path string `json:"path" jsonschema:"Absolute path to the plugin root directory; relative paths are resolved against the server process working directory"`
}

// DescribePluginOutput is the typed output for the describe_plugin tool.
type DescribePluginOutput struct {
	Path       string           `json:"path"`
	Manifest   manifestSummary  `json:"manifest"`
	Components pluginComponents `json:"components"`
}

// NewDescribePlugin returns the describe_plugin tool declaration.
// It uses the generic mcp.AddTool registration path so the SDK infers the
// JSON schema from DescribePluginInput / DescribePluginOutput automatically.
func NewDescribePlugin() Tool {
	return Tool{
		Name:        "describe_plugin",
		Description: "Return structured metadata and shallow component enumeration for a local Claude Code plugin. Inspects the manifest plus skills, agents, MCP servers, hooks, and commands. No marketplace lookup; pass 2 scope only.",
		Register: func(s *mcp.Server) {
			mcp.AddTool(s, &mcp.Tool{
				Name:        "describe_plugin",
				Description: "Return structured metadata and shallow component enumeration for a local Claude Code plugin. Inspects the manifest plus skills, agents, MCP servers, hooks, and commands. No marketplace lookup; pass 2 scope only.",
			}, HandleDescribePlugin)
		},
	}
}

// rawPluginManifest mirrors the plugin.json fields used during parsing.
type rawPluginManifest struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Author      json.RawMessage `json:"author"`
	License     string          `json:"license"`
	Homepage    string          `json:"homepage"`
	Repository  json.RawMessage `json:"repository"`
	Skills      json.RawMessage `json:"skills"`
	Agents      json.RawMessage `json:"agents"`
	Hooks       json.RawMessage `json:"hooks"`
	Commands    json.RawMessage `json:"commands"`
	McpServers  json.RawMessage `json:"mcpServers"`
}

// manifestSummary is the subset of manifest fields included in the result.
type manifestSummary struct {
	Name        string          `json:"name"`
	Version     string          `json:"version,omitempty"`
	Description string          `json:"description,omitempty"`
	Author      json.RawMessage `json:"author,omitempty"`
	License     string          `json:"license,omitempty"`
	Homepage    string          `json:"homepage,omitempty"`
	Repository  json.RawMessage `json:"repository,omitempty"`
}

type skillInfo struct {
	Dir         string `json:"dir"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
}

type agentInfo struct {
	File        string `json:"file"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
}

type mcpServerInfo struct {
	Name    string   `json:"name"`
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
}

type hookEventInfo struct {
	Event string `json:"event"`
	Count int    `json:"count"`
}

type commandInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
}

type pluginComponents struct {
	Skills     []skillInfo     `json:"skills"`
	Agents     []agentInfo     `json:"agents"`
	McpServers []mcpServerInfo `json:"mcpServers"`
	Hooks      []hookEventInfo `json:"hooks"`
	Commands   []commandInfo   `json:"commands"`
	// TODO(components): enumerate output_styles
	// TODO(components): enumerate lsp_servers
	// TODO(components): enumerate monitors
	// TODO(components): enumerate executables
}

// HandleDescribePlugin is the generic typed handler for the describe_plugin tool.
// Error cases return a non-nil *mcp.CallToolResult with Content pre-set (the SDK
// keeps it as-is). Success cases return nil so the SDK serializes output into
// StructuredContent + TextContent automatically.
func HandleDescribePlugin(_ context.Context, _ *mcp.CallToolRequest, input DescribePluginInput) (*mcp.CallToolResult, DescribePluginOutput, error) {
	pluginRoot := strings.TrimSpace(input.Path)
	if pluginRoot == "" {
		return errResult(skerrors.ErrInvalidManifest, "missing required parameter: path", ""), DescribePluginOutput{}, nil
	}

	if !filepath.IsAbs(pluginRoot) {
		wd, err := os.Getwd()
		if err != nil {
			return errResult(skerrors.ErrInvalidManifest, "cannot resolve relative path", err.Error()), DescribePluginOutput{}, nil
		}
		pluginRoot = filepath.Join(wd, pluginRoot)
	}

	info, err := os.Stat(pluginRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult(skerrors.ErrPluginNotFound, "path does not exist", pluginRoot), DescribePluginOutput{}, nil
		}
		return errResult(skerrors.ErrPluginNotFound, "cannot stat path", err.Error()), DescribePluginOutput{}, nil
	}
	if !info.IsDir() {
		return errResult(skerrors.ErrInvalidManifest, "path is not a directory", pluginRoot), DescribePluginOutput{}, nil
	}

	manifestPath := filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")
	if _, err := os.Stat(manifestPath); err != nil {
		// FIX 3: directory exists but lacks plugin manifest → ErrPluginNotFound
		return errResult(skerrors.ErrPluginNotFound, "directory does not contain .claude-plugin/plugin.json", pluginRoot), DescribePluginOutput{}, nil
	}

	raw, manifest, err := readManifest(manifestPath)
	if err != nil {
		return errResult(skerrors.ErrInvalidManifest, "failed to parse plugin.json", err.Error()), DescribePluginOutput{}, nil
	}

	output := DescribePluginOutput{
		Path:     pluginRoot,
		Manifest: manifest,
		Components: pluginComponents{
			Skills:     enumerateSkills(pluginRoot, raw),
			Agents:     enumerateAgents(pluginRoot, raw),
			McpServers: enumerateMcpServers(pluginRoot, raw),
			Hooks:      enumerateHooks(pluginRoot, raw),
			Commands:   enumerateCommands(pluginRoot, raw),
		},
	}

	return nil, output, nil
}

func readManifest(path string) (rawPluginManifest, manifestSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return rawPluginManifest{}, manifestSummary{}, err
	}
	var raw rawPluginManifest
	if err := json.Unmarshal(data, &raw); err != nil {
		return rawPluginManifest{}, manifestSummary{}, err
	}
	summary := manifestSummary{
		Name:        raw.Name,
		Version:     raw.Version,
		Description: raw.Description,
		License:     raw.License,
		Homepage:    raw.Homepage,
	}
	if len(raw.Author) > 0 && string(raw.Author) != "null" {
		summary.Author = raw.Author
	}
	if len(raw.Repository) > 0 && string(raw.Repository) != "null" {
		summary.Repository = raw.Repository
	}
	return raw, summary, nil
}

// parseFrontmatter extracts name and description from YAML frontmatter in a
// markdown file. Returns an error if the file has no frontmatter or it fails
// to parse; callers record the error but do not abort enumeration.
func parseFrontmatter(data []byte) (name, description string, err error) {
	s := string(data)
	if !strings.HasPrefix(s, "---") {
		return "", "", fmt.Errorf("no YAML frontmatter")
	}
	// Find the closing delimiter, skipping the opening one.
	rest := s[3:]
	end := strings.Index(rest, "---")
	if end < 0 {
		return "", "", fmt.Errorf("unclosed frontmatter")
	}
	var fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return "", "", err
	}
	return fm.Name, fm.Description, nil
}

// resolveDir returns the directory for a component type. If the manifest
// declares a path in rawField, that path is used (resolved relative to
// pluginRoot); otherwise defaultDir is returned.
func resolveDir(pluginRoot string, rawField json.RawMessage, defaultDir string) string {
	if len(rawField) > 0 && string(rawField) != "null" {
		var s string
		if json.Unmarshal(rawField, &s) == nil && s != "" {
			if filepath.IsAbs(s) {
				return s
			}
			return filepath.Join(pluginRoot, s)
		}
	}
	return filepath.Join(pluginRoot, defaultDir)
}

func enumerateSkills(pluginRoot string, m rawPluginManifest) []skillInfo {
	base := resolveDir(pluginRoot, m.Skills, "skills")
	entries, err := os.ReadDir(base)
	if err != nil {
		return []skillInfo{}
	}
	var skills []skillInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		si := skillInfo{Dir: e.Name()}
		data, err := os.ReadFile(filepath.Join(base, e.Name(), "SKILL.md"))
		if err != nil {
			si.Error = fmt.Sprintf("SKILL.md not found: %s", err.Error())
		} else if name, desc, err := parseFrontmatter(data); err != nil {
			si.Error = fmt.Sprintf("frontmatter parse error: %s", err.Error())
		} else {
			si.Name = name
			si.Description = desc
		}
		skills = append(skills, si)
	}
	if skills == nil {
		return []skillInfo{}
	}
	return skills
}

func enumerateAgents(pluginRoot string, m rawPluginManifest) []agentInfo {
	base := resolveDir(pluginRoot, m.Agents, "agents")
	entries, err := os.ReadDir(base)
	if err != nil {
		return []agentInfo{}
	}
	var agents []agentInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		ai := agentInfo{File: e.Name()}
		data, err := os.ReadFile(filepath.Join(base, e.Name()))
		if err != nil {
			ai.Error = fmt.Sprintf("cannot read agent file: %s", err.Error())
		} else if name, desc, err := parseFrontmatter(data); err != nil {
			ai.Error = fmt.Sprintf("frontmatter parse error: %s", err.Error())
		} else {
			ai.Name = name
			ai.Description = desc
		}
		agents = append(agents, ai)
	}
	if agents == nil {
		return []agentInfo{}
	}
	return agents
}

type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func mcpMapToList(m map[string]mcpServerEntry) []mcpServerInfo {
	list := make([]mcpServerInfo, 0, len(m))
	for name, e := range m {
		args := e.Args
		if args == nil {
			args = []string{}
		}
		list = append(list, mcpServerInfo{Name: name, Command: e.Command, Args: args})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

func mcpServersFromFile(path string) []mcpServerInfo {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f struct {
		McpServers map[string]mcpServerEntry `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return nil
	}
	return mcpMapToList(f.McpServers)
}

func enumerateMcpServers(pluginRoot string, m rawPluginManifest) []mcpServerInfo {
	if len(m.McpServers) > 0 && string(m.McpServers) != "null" {
		// Check if it's a relative-path string pointing to a JSON file.
		var pathStr string
		if json.Unmarshal(m.McpServers, &pathStr) == nil && pathStr != "" {
			abs := pathStr
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(pluginRoot, pathStr)
			}
			if list := mcpServersFromFile(abs); list != nil {
				return list
			}
			return []mcpServerInfo{}
		}
		// Inline object.
		var servers map[string]mcpServerEntry
		if json.Unmarshal(m.McpServers, &servers) == nil {
			return mcpMapToList(servers)
		}
	}

	// Fall back to .mcp.json at plugin root.
	if list := mcpServersFromFile(filepath.Join(pluginRoot, ".mcp.json")); list != nil {
		return list
	}
	return []mcpServerInfo{}
}

func enumerateHooks(pluginRoot string, m rawPluginManifest) []hookEventInfo {
	var raw map[string]json.RawMessage

	if len(m.Hooks) > 0 && string(m.Hooks) != "null" {
		var pathStr string
		if json.Unmarshal(m.Hooks, &pathStr) == nil && pathStr != "" {
			abs := pathStr
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(pluginRoot, pathStr)
			}
			data, err := os.ReadFile(abs)
			if err == nil {
				json.Unmarshal(data, &raw) //nolint:errcheck
			}
		} else {
			json.Unmarshal(m.Hooks, &raw) //nolint:errcheck
		}
	}

	if raw == nil {
		data, err := os.ReadFile(filepath.Join(pluginRoot, "hooks", "hooks.json"))
		if err == nil {
			json.Unmarshal(data, &raw) //nolint:errcheck
		}
	}

	if len(raw) == 0 {
		return []hookEventInfo{}
	}

	list := make([]hookEventInfo, 0, len(raw))
	for event, val := range raw {
		count := 1
		var arr []json.RawMessage
		if json.Unmarshal(val, &arr) == nil {
			count = len(arr)
		}
		list = append(list, hookEventInfo{Event: event, Count: count})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Event < list[j].Event })
	return list
}

func enumerateCommands(pluginRoot string, m rawPluginManifest) []commandInfo {
	base := resolveDir(pluginRoot, m.Commands, "commands")
	entries, err := os.ReadDir(base)
	if err != nil {
		return []commandInfo{}
	}
	var commands []commandInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		ci := commandInfo{Name: name}
		data, err := os.ReadFile(filepath.Join(base, e.Name()))
		if err != nil {
			ci.Error = fmt.Sprintf("cannot read command file: %s", err.Error())
		} else if _, desc, err := parseFrontmatter(data); err == nil && desc != "" {
			ci.Description = desc
		}
		commands = append(commands, ci)
	}
	if commands == nil {
		return []commandInfo{}
	}
	return commands
}

// errResult constructs a tool result containing a SkillhubError JSON payload.
func errResult(code skerrors.ErrorCode, msg, detail string) *mcp.CallToolResult {
	e := &skerrors.SkillhubError{Code: code, Message: msg, Detail: detail}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: e.JSON()}},
	}
}
