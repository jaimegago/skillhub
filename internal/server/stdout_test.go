package server_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jaimegago/skillhub/internal/server"
)

// TestNoStdoutWriteDuringInit verifies that constructing the server does not
// write anything to the MCP stdout stream before the first client message.
// Any write here would corrupt the MCP framing.
func TestNoStdoutWriteDuringInit(t *testing.T) {
	mcpOut := &bytes.Buffer{}
	_ = server.New(strings.NewReader(""), mcpOut, &bytes.Buffer{}, "test")
	if mcpOut.Len() > 0 {
		t.Fatalf("server.New wrote %d bytes to MCP stdout: %q", mcpOut.Len(), mcpOut.String())
	}
}
