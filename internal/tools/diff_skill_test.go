package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/tools"
)

func callDiffSkillExpectError(t *testing.T, input tools.DiffSkillInput) string {
	t.Helper()
	res, _, err := tools.HandleDiffSkill(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("handler returned unexpected Go error: %v", err)
	}
	if res == nil {
		t.Fatal("expected error result, got nil")
	}
	if len(res.Content) == 0 {
		t.Fatal("error result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("error result content is not TextContent")
	}
	return tc.Text
}

func TestDiffSkill_MissingPluginPath(t *testing.T) {
	text := callDiffSkillExpectError(t, tools.DiffSkillInput{Skill: "my-skill"})
	if !strings.Contains(text, "INVALID_INPUT") {
		t.Errorf("expected INVALID_INPUT, got: %s", text)
	}
}

func TestDiffSkill_MissingSkill(t *testing.T) {
	text := callDiffSkillExpectError(t, tools.DiffSkillInput{PluginPath: "/some/path"})
	if !strings.Contains(text, "INVALID_INPUT") {
		t.Errorf("expected INVALID_INPUT, got: %s", text)
	}
}

func TestDiffSkill_PathNotExist(t *testing.T) {
	text := callDiffSkillExpectError(t, tools.DiffSkillInput{
		PluginPath: "/nonexistent/path/to/plugin",
		Skill:      "my-skill",
	})
	if !strings.Contains(text, "PLUGIN_NOT_FOUND") {
		t.Errorf("expected PLUGIN_NOT_FOUND, got: %s", text)
	}
}

func TestDiffSkill_PartialOverride_MarketplaceOnly(t *testing.T) {
	root := makeMinimalPlugin(t, `{"name":"myplugin"}`)
	text := callDiffSkillExpectError(t, tools.DiffSkillInput{
		PluginPath:  root,
		Skill:       "my-skill",
		Marketplace: "alpha",
	})
	if !strings.Contains(text, "INVALID_MANIFEST") {
		t.Errorf("expected INVALID_MANIFEST, got: %s", text)
	}
}

func TestDiffSkill_PartialOverride_PluginOnly(t *testing.T) {
	root := makeMinimalPlugin(t, `{"name":"myplugin"}`)
	text := callDiffSkillExpectError(t, tools.DiffSkillInput{
		PluginPath: root,
		Skill:      "my-skill",
		Plugin:     "myplugin",
	})
	if !strings.Contains(text, "INVALID_MANIFEST") {
		t.Errorf("expected INVALID_MANIFEST, got: %s", text)
	}
}
