// Package tools implements all MCP tool handlers exposed by the skillhub server.
package tools

import (
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	skerrors "github.com/jaime-gago/skillhub/internal/errors"
)

// Tool bundles an MCP tool declaration with its handler.
//
// Stubs set Handler (low-level mcp.ToolHandler) and InputSchema.
// Fully-implemented tools set Register instead, which calls mcp.AddTool with
// typed In/Out parameters so the SDK infers the JSON schema automatically.
// Exactly one of Handler or Register must be non-nil for each Tool entry.
type Tool struct {
	Name        string
	Description string
	InputSchema any
	Handler     mcp.ToolHandler
	Register    func(*mcp.Server)
}

// Registry is the single source of truth for all skillhub tools.
// server.go iterates this to register handlers; server_test.go compares against it.
var Registry = []Tool{
	NewCheckDrift(),
	NewDiffSkill(),
	NewProposeSkillChanges(),
	NewListAvailablePlugins(),
	NewSearchPlugins(),
	NewRecommendPlugins(),
	NewDescribePlugin(),
}

// notImplemented returns the canonical stub result. All stubs call this.
// Remove this function when the last stub is replaced with a real implementation.
func notImplemented() (*mcp.CallToolResult, error) {
	e := &skerrors.SkillhubError{
		Code:    skerrors.ErrNotImplemented,
		Message: "not implemented",
	}
	b, _ := json.Marshal(e)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, nil
}

// emptySchema is a minimal valid JSON Schema used by all stubs.
// Replace with a typed schema when each tool's real implementation lands.
var emptySchema = map[string]any{"type": "object"}
