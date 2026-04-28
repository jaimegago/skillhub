// Package tools implements all MCP tool handlers exposed by the skillhub server.
package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

// Tool bundles an MCP tool name, description, and its Register function.
// Register calls mcp.AddTool with typed In/Out parameters so the SDK infers
// the JSON schema automatically.
type Tool struct {
	Name        string
	Description string
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
