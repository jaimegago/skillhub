package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// callDescribePlugin calls HandleDescribePlugin directly and returns a
// *mcp.CallToolResult that tests can inspect via resultText / assertErrCode.
//
// For error returns (non-nil result from handler): the result is returned as-is
// with Content already populated by errResult.
// For success (nil result from handler): output is marshaled into a TextContent
// block, matching what the SDK would produce on the server path.
func callDescribePlugin(t *testing.T, path string) *mcp.CallToolResult {
	t.Helper()
	res, out, err := tools.HandleDescribePlugin(
		context.Background(),
		&mcp.CallToolRequest{},
		tools.DescribePluginInput{Path: path},
	)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if res != nil {
		return res
	}
	// Success path: simulate SDK serialization of the typed output.
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}
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
	res, _, err := tools.HandleDescribePlugin(
		context.Background(),
		&mcp.CallToolRequest{},
		tools.DescribePluginInput{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertErrCode(t, res, "INVALID_MANIFEST")
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

// FIX 3: directory without plugin.json now returns PLUGIN_NOT_FOUND.
func TestDescribePlugin_NoPluginJson(t *testing.T) {
	dir := t.TempDir()
	result := callDescribePlugin(t, dir)
	assertErrCode(t, result, "PLUGIN_NOT_FOUND")
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

func TestDescribePlugin_InlineLspServers(t *testing.T) {
	const manifest = `{
		"name": "myplugin",
		"lspServers": {
			"gopls": {
				"command": "gopls",
				"args": ["serve"],
				"extensionToLanguage": {".go": "go"}
			}
		}
	}`
	root := makeMinimalPlugin(t, manifest)

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Components struct {
			LspServers []struct {
				Name                string            `json:"name"`
				Command             string            `json:"command"`
				ExtensionToLanguage map[string]string `json:"extensionToLanguage"`
			} `json:"lspServers"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if len(out.Components.LspServers) != 1 {
		t.Fatalf("expected 1 LSP server, got %d", len(out.Components.LspServers))
	}
	srv := out.Components.LspServers[0]
	if srv.Name != "gopls" {
		t.Errorf("name = %q, want %q", srv.Name, "gopls")
	}
	if srv.Command != "gopls" {
		t.Errorf("command = %q, want %q", srv.Command, "gopls")
	}
	if srv.ExtensionToLanguage[".go"] != "go" {
		t.Errorf("extensionToLanguage[.go] = %q, want %q", srv.ExtensionToLanguage[".go"], "go")
	}
}

func TestDescribePlugin_LspServersFile(t *testing.T) {
	const manifest = `{"name": "myplugin"}`
	root := makeMinimalPlugin(t, manifest)

	const lspJSON = `{
		"ts": {
			"command": "typescript-language-server",
			"args": ["--stdio"],
			"extensionToLanguage": {".ts": "typescript", ".tsx": "typescriptreact"}
		}
	}`
	if err := os.WriteFile(filepath.Join(root, ".lsp.json"), []byte(lspJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Components struct {
			LspServers []struct {
				Name    string `json:"name"`
				Command string `json:"command"`
			} `json:"lspServers"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if len(out.Components.LspServers) != 1 {
		t.Fatalf("expected 1 LSP server, got %d", len(out.Components.LspServers))
	}
	if out.Components.LspServers[0].Name != "ts" {
		t.Errorf("name = %q, want %q", out.Components.LspServers[0].Name, "ts")
	}
	if out.Components.LspServers[0].Command != "typescript-language-server" {
		t.Errorf("command = %q, want %q", out.Components.LspServers[0].Command, "typescript-language-server")
	}
}

func TestDescribePlugin_MonitorsFile(t *testing.T) {
	const manifest = `{"name": "myplugin"}`
	root := makeMinimalPlugin(t, manifest)

	if err := os.MkdirAll(filepath.Join(root, "monitors"), 0o755); err != nil {
		t.Fatal(err)
	}
	const monitorsJSON = `[
		{"name": "error-log", "command": "tail -F ./logs/error.log", "description": "Application error log"},
		{"name": "deploy-status", "command": "./scripts/poll.sh", "description": "Deployment status", "when": "on-skill-invoke:deploy"}
	]`
	if err := os.WriteFile(filepath.Join(root, "monitors", "monitors.json"), []byte(monitorsJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Components struct {
			Monitors []struct {
				Name        string `json:"name"`
				Command     string `json:"command"`
				Description string `json:"description"`
				When        string `json:"when"`
			} `json:"monitors"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if len(out.Components.Monitors) != 2 {
		t.Fatalf("expected 2 monitors, got %d", len(out.Components.Monitors))
	}
	var found bool
	for _, m := range out.Components.Monitors {
		if m.Name == "deploy-status" {
			found = true
			if m.When != "on-skill-invoke:deploy" {
				t.Errorf("when = %q, want %q", m.When, "on-skill-invoke:deploy")
			}
			if m.Description != "Deployment status" {
				t.Errorf("description = %q, want %q", m.Description, "Deployment status")
			}
		}
	}
	if !found {
		t.Error("deploy-status monitor not found")
	}
}

func TestDescribePlugin_InlineMonitors(t *testing.T) {
	const manifest = `{
		"name": "myplugin",
		"monitors": [
			{"name": "status", "command": "echo ok", "description": "Status check"}
		]
	}`
	root := makeMinimalPlugin(t, manifest)

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Components struct {
			Monitors []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"monitors"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if len(out.Components.Monitors) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(out.Components.Monitors))
	}
	if out.Components.Monitors[0].Name != "status" {
		t.Errorf("name = %q, want %q", out.Components.Monitors[0].Name, "status")
	}
	if out.Components.Monitors[0].Description != "Status check" {
		t.Errorf("description = %q, want %q", out.Components.Monitors[0].Description, "Status check")
	}
}

func TestDescribePlugin_Executables(t *testing.T) {
	const manifest = `{"name": "myplugin"}`
	root := makeMinimalPlugin(t, manifest)

	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"my-tool", "helper"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\necho hello"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Subdirectory should be excluded.
	if err := os.MkdirAll(filepath.Join(binDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Components struct {
			Executables []struct {
				File string `json:"file"`
			} `json:"executables"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if len(out.Components.Executables) != 2 {
		t.Fatalf("expected 2 executables, got %d: %v", len(out.Components.Executables), out.Components.Executables)
	}
	files := map[string]bool{}
	for _, e := range out.Components.Executables {
		files[e.File] = true
	}
	for _, want := range []string{"my-tool", "helper"} {
		if !files[want] {
			t.Errorf("executable %q not found in result", want)
		}
	}
}

func TestDescribePlugin_SkillsPagination(t *testing.T) {
	const manifest = `{"name":"bigplugin"}`
	root := makeMinimalPlugin(t, manifest)

	const total = 30
	for i := 0; i < total; i++ {
		dir := filepath.Join(root, "skills", fmt.Sprintf("skill-%02d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		md := fmt.Sprintf("---\nname: Skill %02d\ndescription: Skill number %d\n---\n", i, i)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Page 1: limit 20, no cursor.
	res1, out1, err := tools.HandleDescribePlugin(
		context.Background(),
		&mcp.CallToolRequest{},
		tools.DescribePluginInput{Path: root, SkillsLimit: 20},
	)
	if err != nil {
		t.Fatalf("page 1 unexpected error: %v", err)
	}
	if res1 != nil {
		t.Fatalf("page 1 expected success, got error result")
	}
	if out1.SkillsTotal != total {
		t.Errorf("page 1 SkillsTotal = %d, want %d", out1.SkillsTotal, total)
	}
	if len(out1.Components.Skills) != 20 {
		t.Errorf("page 1 skill count = %d, want 20", len(out1.Components.Skills))
	}
	if out1.SkillsNextCursor == "" {
		t.Fatal("page 1 expected non-empty SkillsNextCursor")
	}

	// Page 2: use cursor from page 1.
	res2, out2, err := tools.HandleDescribePlugin(
		context.Background(),
		&mcp.CallToolRequest{},
		tools.DescribePluginInput{Path: root, SkillsLimit: 20, SkillsCursor: out1.SkillsNextCursor},
	)
	if err != nil {
		t.Fatalf("page 2 unexpected error: %v", err)
	}
	if res2 != nil {
		t.Fatalf("page 2 expected success, got error result")
	}
	if len(out2.Components.Skills) != 10 {
		t.Errorf("page 2 skill count = %d, want 10", len(out2.Components.Skills))
	}
	if out2.SkillsNextCursor != "" {
		t.Errorf("page 2 expected empty SkillsNextCursor (last page), got %q", out2.SkillsNextCursor)
	}
	if out2.SkillsTotal != total {
		t.Errorf("page 2 SkillsTotal = %d, want %d", out2.SkillsTotal, total)
	}

	// No-limit call still returns all skills.
	_, out0, err := tools.HandleDescribePlugin(
		context.Background(),
		&mcp.CallToolRequest{},
		tools.DescribePluginInput{Path: root},
	)
	if err != nil {
		t.Fatalf("no-limit unexpected error: %v", err)
	}
	if len(out0.Components.Skills) != total {
		t.Errorf("no-limit skill count = %d, want %d", len(out0.Components.Skills), total)
	}
	if out0.SkillsNextCursor != "" {
		t.Errorf("no-limit expected empty SkillsNextCursor, got %q", out0.SkillsNextCursor)
	}
}

// FIX 2: use runtime.Caller(0) to locate this test file, then walk up to find
// the module root (directory containing go.mod). This is deterministic
// regardless of working directory and never skips.
func TestDescribePlugin_SelfDescribing(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	// Walk up from the test file's directory until we find go.mod.
	dir := filepath.Dir(thisFile)
	repoRoot := ""
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			repoRoot = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if repoRoot == "" {
		t.Fatal("could not locate go.mod walking up from test file")
	}

	// The skillhub repo itself is a valid plugin — smoke-test against it.
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

func TestDescribePlugin_OutputStyles(t *testing.T) {
	const manifest = `{"name":"myplugin"}`
	root := makeMinimalPlugin(t, manifest)

	stylesDir := filepath.Join(root, "output-styles")
	if err := os.MkdirAll(stylesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const styleMd = "---\nname: Concise\ndescription: Terse, minimal prose\n---\n\nBody text."
	if err := os.WriteFile(filepath.Join(stylesDir, "concise.md"), []byte(styleMd), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-.md file and directory should be excluded.
	if err := os.WriteFile(filepath.Join(stylesDir, "README.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stylesDir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Components struct {
			OutputStyles []struct {
				File        string `json:"file"`
				Name        string `json:"name"`
				Description string `json:"description"`
				Error       string `json:"error"`
			} `json:"outputStyles"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if len(out.Components.OutputStyles) != 1 {
		t.Fatalf("expected 1 output style, got %d", len(out.Components.OutputStyles))
	}
	s := out.Components.OutputStyles[0]
	if s.File != "concise.md" {
		t.Errorf("file = %q, want %q", s.File, "concise.md")
	}
	if s.Name != "Concise" {
		t.Errorf("name = %q, want %q", s.Name, "Concise")
	}
	if s.Description != "Terse, minimal prose" {
		t.Errorf("description = %q, want %q", s.Description, "Terse, minimal prose")
	}
	if s.Error != "" {
		t.Errorf("unexpected error: %s", s.Error)
	}
}

func TestDescribePlugin_OutputStyles_Empty(t *testing.T) {
	const manifest = `{"name":"myplugin"}`
	root := makeMinimalPlugin(t, manifest)

	result := callDescribePlugin(t, root)
	text := resultText(t, result)

	var out struct {
		Components struct {
			OutputStyles []any `json:"outputStyles"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if out.Components.OutputStyles == nil {
		t.Error("outputStyles should be [] not null")
	}
	if len(out.Components.OutputStyles) != 0 {
		t.Errorf("expected 0 output styles, got %d", len(out.Components.OutputStyles))
	}
}
