// Package main is the skillhub CLI entry point.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jaime-gago/skillhub/internal/server"
)

var version = "0.1.0"

func main() {
	root := &cobra.Command{
		Use:          "skillhub",
		Short:        "MCP server for authoring and maintaining Claude Code plugins and skills",
		SilenceUsage: true,
	}

	root.AddCommand(&cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server on stdio",
		RunE: func(_ *cobra.Command, _ []string) error {
			srv := server.New(os.Stdin, os.Stdout, os.Stderr, version)
			return srv.Run(context.Background())
		},
	})

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print build version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(version)
		},
	})

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
