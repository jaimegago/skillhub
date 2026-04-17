package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewProposeSkillChanges returns the propose_skill_changes tool declaration.
func NewProposeSkillChanges() Tool {
	return Tool{
		Name:        "propose_skill_changes",
		Description: "Open a merge request against the configured marketplace source repository proposing the local skill changes. Set dry_run to preview without creating the MR.",
		InputSchema: emptySchema,
		Handler:     handleProposeSkillChanges,
	}
}

func handleProposeSkillChanges(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return notImplemented()
}
