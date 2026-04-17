package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewDiffSkill returns the diff_skill tool declaration.
// TODO(large-output): declare size annotation and paginate when implemented.
// Verify exact annotation field name (likely anthropic/maxResultSizeChars) against
// current Claude Code docs before adding — do not assume spec provenance.
func NewDiffSkill() Tool {
	return Tool{
		Name:        "diff_skill",
		Description: "Return a unified diff between the local version of a named skill and its canonical marketplace version. Output may be large; prefer piping to a pager.",
		InputSchema: emptySchema,
		Handler:     handleDiffSkill,
	}
}

func handleDiffSkill(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return notImplemented()
}
