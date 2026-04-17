package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewRecommendPlugins returns the recommend_plugins tool declaration.
func NewRecommendPlugins() Tool {
	return Tool{
		Name:        "recommend_plugins",
		Description: "Given free-text context describing a task or need, rank uninstalled plugins from configured marketplace sources by relevance. Returns an ordered list with name, description, and relevance rationale.",
		InputSchema: emptySchema,
		Handler:     handleRecommendPlugins,
	}
}

func handleRecommendPlugins(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return notImplemented()
}
