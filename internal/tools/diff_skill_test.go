package tools_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaime-gago/skillhub/internal/tools"
)

func TestDiffSkillNotImplemented(t *testing.T) {
	tool := tools.NewDiffSkill()
	result, err := tool.Handler(context.Background(), new(mcp.CallToolRequest))
	if err != nil {
		t.Fatal(err)
	}
	assertNotImplemented(t, result)
}
