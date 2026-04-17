package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaime-gago/skillhub/internal/tools"
)

// makeMinimalPlugin creates a temp directory with a minimal .claude-plugin/plugin.json.
func makeMinimalPlugin(t *testing.T, manifest string) string {
	t.Helper()
	root := t.TempDir()
	pluginDir := filepath.Join(root, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir .claude-plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	return root
}

// callDescribePlugin is a helper that calls the handler with the given path argument.
func callDescribePlugin(t *testing.T, path string) *mcp.CallToolResult {
	t.Helper()
	args, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	req := &mcp.CallToolRequest{}
	req.Params = &mcp.CallToolParamsRaw{Arguments: args}
	tool := tools.NewDescribePlugin()
	result, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	return result
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
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

func assertErrCode(t *testing.T, result *mcp.CallToolResult, code string) {
	t.Helper()
	text := resultText(t, result)
	if !strings.Contains(text, code) {
		t.Errorf("expected error code %q in result, got: %s", code, text)
	}
}

func TestDescribePlugin_MissingParam(t *testing.T) {
	tool := tools.NewDescribePlugin()
	result, err := tool.Handler(context.Background(), new(mcp.CallToolRequest))
	if err != nil {
		t.Fatal(err)
	}
	assertErrCode(t, result, "INVALID_MANIFEST")
}

func TestDescribePlugin_PathNotExist(t *testing.T) {
	result := callDescribePlugin(t, "/this/path/does/not/exist/at/all")
	assertErrCode(t, result, "PLUGIN_NOT_FOUND")
}

func TestDescribePlugin_PathIsFile(t *testing.T) {
	f, err := os.CreateTemp("", "notadir")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name()) //nolint:errcheck

	result := callDescribePlugin(t, f.Name())
	assertErrCode(t, result, "INVALID_MANIFEST")
}

func TestDescribePlugin_NoPluginJson(t *testing.T) {
	dir := t.TempDir()
	result := callDescribePlugin(t, dir)
	assertErrCode(t, result, "INVALID_MANIFEST")
}

func TestDescribePlugin_MinimalManifest(t *testing.T) {
	const manifest = `{"name":"myplugin","version":"1.0.0","description":"test plugin"}`
	root := makeMinimalPlugin(t, manifest)

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Path     string `json:"path"`
		Manifest struct {
			Name        string `json:"name"`
			Version     string `json:"version"`
			Description string `json:"description"`
		} `json:"manifest"`
		Components struct {
			Skills     []any `json:"skills"`
			Agents     []any `json:"agents"`
			McpServers []any `json:"mcpServers"`
			Hooks      []any `json:"hooks"`
			Commands   []any `json:"commands"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if out.Manifest.Name != "myplugin" {
		t.Errorf("name = %q, want %q", out.Manifest.Name, "myplugin")
	}
	if out.Manifest.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", out.Manifest.Version, "1.0.0")
	}
	if out.Path != root {
		t.Errorf("path = %q, want %q", out.Path, root)
	}
	// All component slices must be non-null (empty arrays, not null).
	if out.Components.Skills == nil {
		t.Error("skills should be [] not null")
	}
	if out.Components.McpServers == nil {
		t.Error("mcpServers should be [] not null")
	}
}

func TestDescribePlugin_WithSkills(t *testing.T) {
	const manifest = `{"name":"myplugin"}`
	root := makeMinimalPlugin(t, manifest)

	skillDir := filepath.Join(root, "skills", "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const skillMd = "---\nname: My Skill\ndescription: Does something useful\n---\n\nBody text here."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMd), 0o644); err != nil {
		t.Fatal(err)
	}

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Components struct {
			Skills []struct {
				Dir         string `json:"dir"`
				Name        string `json:"name"`
				Description string `json:"description"`
				Error       string `json:"error"`
			} `json:"skills"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if len(out.Components.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(out.Components.Skills))
	}
	s := out.Components.Skills[0]
	if s.Dir != "my-skill" {
		t.Errorf("skill dir = %q, want %q", s.Dir, "my-skill")
	}
	if s.Name != "My Skill" {
		t.Errorf("skill name = %q, want %q", s.Name, "My Skill")
	}
	if s.Error != "" {
		t.Errorf("unexpected skill error: %s", s.Error)
	}
}

func TestDescribePlugin_SkillMissingSkillMd(t *testing.T) {
	const manifest = `{"name":"myplugin"}`
	root := makeMinimalPlugin(t, manifest)

	// Skill dir exists but no SKILL.md.
	if err := os.MkdirAll(filepath.Join(root, "skills", "broken-skill"), 0o755); err != nil {
		t.Fatal(err)
	}

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Components struct {
			Skills []struct {
				Dir   string `json:"dir"`
				Error string `json:"error"`
			} `json:"skills"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if len(out.Components.Skills) != 1 {
		t.Fatalf("expected 1 skill entry, got %d", len(out.Components.Skills))
	}
	if out.Components.Skills[0].Error == "" {
		t.Error("expected error field set for skill with missing SKILL.md")
	}
}

func TestDescribePlugin_InlineMcpServers(t *testing.T) {
	const manifest = `{
		"name": "myplugin",
		"mcpServers": {
			"myserver": {"command": "/usr/bin/node", "args": ["server.js"]}
		}
	}`
	root := makeMinimalPlugin(t, manifest)

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Components struct {
			McpServers []struct {
				Name    string   `json:"name"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"mcpServers"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if len(out.Components.McpServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(out.Components.McpServers))
	}
	srv := out.Components.McpServers[0]
	if srv.Name != "myserver" {
		t.Errorf("server name = %q, want %q", srv.Name, "myserver")
	}
	if srv.Command != "/usr/bin/node" {
		t.Errorf("command = %q, want %q", srv.Command, "/usr/bin/node")
	}
}

func TestDescribePlugin_HooksFile(t *testing.T) {
	const manifest = `{"name":"myplugin"}`
	root := makeMinimalPlugin(t, manifest)

	hooksDir := filepath.Join(root, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const hooksJSON = `{
		"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo pre"}]}],
		"PostToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": "echo post"}]}]
	}`
	if err := os.WriteFile(filepath.Join(hooksDir, "hooks.json"), []byte(hooksJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Components struct {
			Hooks []struct {
				Event string `json:"event"`
				Count int    `json:"count"`
			} `json:"hooks"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if len(out.Components.Hooks) != 2 {
		t.Fatalf("expected 2 hook events, got %d", len(out.Components.Hooks))
	}
	for _, h := range out.Components.Hooks {
		if h.Count != 1 {
			t.Errorf("hook %q: count = %d, want 1", h.Event, h.Count)
		}
	}
}

func TestDescribePlugin_SelfDescribing(t *testing.T) {
	// The skillhub repo itself is a valid plugin — smoke-test against it.
	repoRoot, err := filepath.Abs("../../../")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".claude-plugin", "plugin.json")); err != nil {
		t.Skip("not running from skillhub repo root, skipping self-describing test")
	}

	result := callDescribePlugin(t, repoRoot)
	text := resultText(t, result)

	if strings.Contains(text, "PLUGIN_NOT_FOUND") || strings.Contains(text, "INVALID_MANIFEST") {
		t.Errorf("self-describing test failed: %s", text)
	}

	var out struct {
		Manifest struct {
			Name string `json:"name"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if out.Manifest.Name == "" {
		t.Error("expected non-empty manifest name for skillhub self-describe")
	}
}
