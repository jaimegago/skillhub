package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewSearchPlugins returns the search_plugins tool declaration.
func NewSearchPlugins() Tool {
	return Tool{
		Name:        "search_plugins",
		Description: "Perform a case-insensitive substring match across plugin names and descriptions in all configured marketplace sources. Returns matching plugins with name, description, and version.",
		InputSchema: emptySchema,
		Handler:     handleSearchPlugins,
	}
}

func handleSearchPlugins(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return notImplemented()
}
