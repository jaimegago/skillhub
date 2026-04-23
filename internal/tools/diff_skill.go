package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewDiffSkill returns the diff_skill tool declaration.
//
// Pagination note (verified 2026-04-23): anthropic/maxResultSizeChars does not exist
// in the MCP spec or go-sdk. The only available behavioral hint is ReadOnlyHint (set
// below). Application-level pagination (input: limit+cursor, output: nextCursor+total)
// must be wired into the typed input/output structs when this tool is implemented —
// see describe_plugin for the established pattern.
func NewDiffSkill() Tool {
	const desc = "Return a unified diff between the local version of a named skill and its canonical marketplace version. Output may be large; prefer piping to a pager."
	return Tool{
		Name: "diff_skill",
		Register: func(s *mcp.Server) {
			s.AddTool(&mcp.Tool{
				Name:        "diff_skill",
				Description: desc,
				InputSchema: emptySchema,
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			}, HandleDiffSkill)
		},
	}
}

// HandleDiffSkill is the low-level handler for diff_skill. Exported so tests can call
// it directly without going through the MCP server.
func HandleDiffSkill(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return notImplemented()
}
