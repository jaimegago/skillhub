package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaime-gago/skillhub/internal/tools"
)

func TestCheckDriftNotImplemented(t *testing.T) {
	tool := tools.NewCheckDrift()
	result, err := tool.Handler(context.Background(), new(mcp.CallToolRequest))
	if err != nil {
		t.Fatal(err)
	}
	assertNotImplemented(t, result)
}

// assertNotImplemented is a shared helper used by all stub tests.
func assertNotImplemented(t *testing.T, result *mcp.CallToolResult) {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty Content in result")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "NOT_IMPLEMENTED") {
		t.Errorf("expected NOT_IMPLEMENTED in content, got %q", text.Text)
	}
}
