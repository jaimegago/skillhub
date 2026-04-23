// Package main is the skillhub CLI entry point.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jaime-gago/skillhub/internal/server"
)

var (
	version = "0.1.0"
	commit  = "unknown"
	date    = "unknown"
)

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
		Short: "Print version, commit, and build date",
		Long: `Print build metadata on three lines:

  version <semver>
  commit  <short-sha>
  date    <UTC ISO-8601>

Each line is a key and a value separated by a single tab, so downstream
tools can parse with: awk '/^version/{print $2}'.`,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("version\t%s\n", version)
			fmt.Printf("commit\t%s\n", commit)
			fmt.Printf("date\t%s\n", date)
		},
	})

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
