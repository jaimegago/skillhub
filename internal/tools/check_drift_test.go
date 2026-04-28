package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/tools"
)

// callCheckDrift invokes HandleCheckDrift and returns a result that tests can
// inspect via resultText / assertErrCode, following the same pattern as
// callDescribePlugin.
func callCheckDrift(t *testing.T, input tools.CheckDriftInput) *mcp.CallToolResult {
	t.Helper()
	res, out, err := tools.HandleCheckDrift(
		context.Background(),
		&mcp.CallToolRequest{},
		input,
	)
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

// --- path validation ---

func TestCheckDrift_MissingPluginPath(t *testing.T) {
	res, _, err := tools.HandleCheckDrift(
		context.Background(),
		&mcp.CallToolRequest{},
		tools.CheckDriftInput{},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertErrCode(t, res, "INVALID_MANIFEST")
}

func TestCheckDrift_PathNotExist(t *testing.T) {
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: "/this/path/does/not/exist/at/all"})
	assertErrCode(t, result, "PLUGIN_NOT_FOUND")
}

func TestCheckDrift_NoPluginJson(t *testing.T) {
	dir := t.TempDir()
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: dir})
	assertErrCode(t, result, "PLUGIN_NOT_FOUND")
}

func TestCheckDrift_MalformedManifest(t *testing.T) {
	root := makeMinimalPlugin(t, `not valid json`)
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: root})
	assertErrCode(t, result, "INVALID_MANIFEST")
}

// --- upstream override validation ---

func TestCheckDrift_PartialOverride_MarketplaceOnly(t *testing.T) {
	root := makeMinimalPlugin(t, `{"name":"myplugin"}`)
	result := callCheckDrift(t, tools.CheckDriftInput{
		PluginPath:  root,
		Marketplace: "some-market",
	})
	assertErrCode(t, result, "INVALID_MANIFEST")
}

func TestCheckDrift_PartialOverride_PluginOnly(t *testing.T) {
	root := makeMinimalPlugin(t, `{"name":"myplugin"}`)
	result := callCheckDrift(t, tools.CheckDriftInput{
		PluginPath: root,
		Plugin:     "some-plugin",
	})
	assertErrCode(t, result, "INVALID_MANIFEST")
}

// --- upstream resolution ---

func TestCheckDrift_MissingUpstream_NoDecl(t *testing.T) {
	root := makeMinimalPlugin(t, `{"name":"myplugin"}`)
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: root})
	text := resultText(t, result)

	var out struct {
		PluginStatus string `json:"plugin_status"`
		Skills       []any  `json:"skills"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if out.PluginStatus != "missing-upstream" {
		t.Errorf("plugin_status = %q, want %q", out.PluginStatus, "missing-upstream")
	}
	if out.Skills == nil {
		t.Error("skills should be [] not null")
	}
}

func TestCheckDrift_UpstreamDecl_MissingPluginField(t *testing.T) {
	// x-skillhub-upstream present but plugin field is empty.
	const manifest = `{"name":"myplugin","x-skillhub-upstream":{"marketplace":"test-market","plugin":""}}`
	root := makeMinimalPlugin(t, manifest)
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: root})
	assertErrCode(t, result, "INVALID_MANIFEST")
}

func TestCheckDrift_UpstreamDecl_MissingMarketplaceField(t *testing.T) {
	// x-skillhub-upstream present but marketplace field is empty.
	const manifest = `{"name":"myplugin","x-skillhub-upstream":{"marketplace":"","plugin":"my-plugin"}}`
	root := makeMinimalPlugin(t, manifest)
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: root})
	assertErrCode(t, result, "INVALID_MANIFEST")
}

func TestCheckDrift_UpstreamDecl_BothFieldsEmpty(t *testing.T) {
	// x-skillhub-upstream present but both fields are empty — treat as absent,
	// not as an error. Plugin should report missing-upstream, not INVALID_MANIFEST.
	const manifest = `{"name":"myplugin","x-skillhub-upstream":{"marketplace":"","plugin":""}}`
	root := makeMinimalPlugin(t, manifest)
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: root})
	text := resultText(t, result)

	var out struct {
		PluginStatus string `json:"plugin_status"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v\nraw: %s", err, text)
	}
	if out.PluginStatus != "missing-upstream" {
		t.Errorf("plugin_status = %q, want missing-upstream", out.PluginStatus)
	}
}

// --- marketplace resolution (uses httptest server via serveListFixture + withEnvConfig) ---

func TestCheckDrift_NoMarketplaceConfigured(t *testing.T) {
	// CLAUDE_PLUGIN_DATA → empty dir, no config.yaml → zero MarketplaceSources.
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())

	const manifest = `{"name":"myplugin","x-skillhub-upstream":{"marketplace":"test-market","plugin":"my-plugin"}}`
	root := makeMinimalPlugin(t, manifest)
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: root})
	assertErrCode(t, result, "MARKETPLACE_NOT_CONFIGURED")
}

func TestCheckDrift_MarketplaceNameMismatch(t *testing.T) {
	srv := serveListFixture(t, `{"name":"other-market","plugins":[]}`)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	const manifest = `{"name":"myplugin","x-skillhub-upstream":{"marketplace":"test-market","plugin":"my-plugin"}}`
	root := makeMinimalPlugin(t, manifest)
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: root})
	assertErrCode(t, result, "MARKETPLACE_NOT_CONFIGURED")
}

func TestCheckDrift_PluginNotFoundInMarketplace(t *testing.T) {
	srv := serveListFixture(t, `{"name":"test-market","plugins":[{"name":"other-plugin","source":"./plugins/other"}]}`)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	const manifest = `{"name":"myplugin","x-skillhub-upstream":{"marketplace":"test-market","plugin":"my-plugin"}}`
	root := makeMinimalPlugin(t, manifest)
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: root})
	assertErrCode(t, result, "PLUGIN_NOT_FOUND")
}

func TestCheckDrift_NpmSource(t *testing.T) {
	const mktJSON = `{"name":"test-market","plugins":[{"name":"my-plugin","source":{"source":"npm","package":"@foo/my-plugin"}}]}`
	srv := serveListFixture(t, mktJSON)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	const manifest = `{"name":"myplugin","x-skillhub-upstream":{"marketplace":"test-market","plugin":"my-plugin"}}`
	root := makeMinimalPlugin(t, manifest)
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: root})
	assertErrCode(t, result, "UNSUPPORTED_SOURCE")
}

func TestCheckDrift_WithOverrides_PluginNotFound(t *testing.T) {
	srv := serveListFixture(t, `{"name":"test-market","plugins":[]}`)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	// No x-skillhub-upstream; overrides supplied directly.
	root := makeMinimalPlugin(t, `{"name":"myplugin"}`)
	result := callCheckDrift(t, tools.CheckDriftInput{
		PluginPath:  root,
		Marketplace: "test-market",
		Plugin:      "nonexistent",
	})
	assertErrCode(t, result, "PLUGIN_NOT_FOUND")
}

func TestCheckDrift_UnknownSourceKind(t *testing.T) {
	const mktJSON = `{"name":"test-market","plugins":[{"name":"my-plugin","source":{"source":"sftp","url":"sftp://example.com/repo.git"}}]}`
	srv := serveListFixture(t, mktJSON)
	withEnvConfig(t, "marketplaceSources:\n  - url: "+srv.URL+"\n    gitHostType: generic\n")

	const manifest = `{"name":"myplugin","x-skillhub-upstream":{"marketplace":"test-market","plugin":"my-plugin"}}`
	root := makeMinimalPlugin(t, manifest)
	result := callCheckDrift(t, tools.CheckDriftInput{PluginPath: root})
	assertErrCode(t, result, "INVALID_MANIFEST")
}
