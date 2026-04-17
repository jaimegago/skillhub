package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewListAvailablePlugins returns the list_available_plugins tool declaration.
func NewListAvailablePlugins() Tool {
	return Tool{
		Name:        "list_available_plugins",
		Description: "Read all configured marketplace sources and return plugins that are not currently installed, with name, description, and version for each.",
		InputSchema: emptySchema,
		Handler:     handleListAvailablePlugins,
	}
}

func handleListAvailablePlugins(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return notImplemented()
}
