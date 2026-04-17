package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewCheckDrift returns the check_drift tool declaration.
func NewCheckDrift() Tool {
	return Tool{
		Name:        "check_drift",
		Description: "Detect whether a locally installed plugin skill has diverged from its canonical version in a configured marketplace source. Returns drift status and a summary of changes.",
		InputSchema: emptySchema,
		Handler:     handleCheckDrift,
	}
}

func handleCheckDrift(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return notImplemented()
}
