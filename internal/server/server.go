// Package server wires the MCP server and registers all tool handlers.
package server

import (
	"context"
	"io"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jaimegago/skillhub/internal/tools"
)

const serverInstructions = `skillhub manages Claude Code plugins and skills. Use these tools when you need to check whether a local skill has drifted from its marketplace source, generate a diff, propose upstream changes, discover available plugins, or inspect a plugin's manifest and skill contents.`

// Server wraps the MCP server with explicit I/O injection so that no code
// below main.go references os.Stdin, os.Stdout, or os.Stderr directly.
type Server struct {
	mcpServer *mcp.Server
	mcpIn     io.Reader
	mcpOut    io.Writer
	log       *slog.Logger
}

// nopWriteCloser adapts io.Writer to io.WriteCloser with a no-op Close.
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// New constructs and wires a Server. mcpIn/mcpOut are the MCP stdio streams;
// logSink is the structured log destination (os.Stderr in production).
// No bytes are written to mcpOut during construction.
func New(mcpIn io.Reader, mcpOut io.Writer, logSink io.Writer, version string) *Server {
	logger := slog.New(slog.NewTextHandler(logSink, nil))

	s := mcp.NewServer(&mcp.Implementation{
		Name:    "skillhub",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})

	for _, t := range tools.Registry {
		t.Register(s)
	}

	return &Server{
		mcpServer: s,
		mcpIn:     mcpIn,
		mcpOut:    mcpOut,
		log:       logger,
	}
}

// ToolNames returns the names of all registered tools in registry order.
// Used by tests to verify the server's tool set matches tools.Registry.
func (s *Server) ToolNames() []string {
	names := make([]string, len(tools.Registry))
	for i, t := range tools.Registry {
		names[i] = t.Name
	}
	return names
}

// Run starts the MCP server on the injected stdio streams. Blocks until ctx
// is cancelled or the client closes the connection. Do not call in tests.
func (s *Server) Run(ctx context.Context) error {
	s.log.Info("starting skillhub MCP server")
	t := &mcp.IOTransport{
		Reader: io.NopCloser(s.mcpIn),
		Writer: nopWriteCloser{s.mcpOut},
	}
	return s.mcpServer.Run(ctx, t)
}
