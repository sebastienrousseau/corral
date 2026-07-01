package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sebastienrousseau/corral/internal/mcp"
	"github.com/spf13/cobra"
)

var (
	mcpRoot            string
	mcpEnableMutations bool
)

// mcpCmd registers the `corralctl mcp` subcommand. It runs a Model
// Context Protocol server over stdio so AI coding agents (Claude Code,
// Cursor, Cline, Codex CLI, etc.) can introspect the local Corral-
// organised workspace without making any network calls.
//
// Stdio is reserved for the JSON-RPC protocol stream by the MCP spec;
// every diagnostic this command emits goes to stderr.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run the corral-mcp server (Model Context Protocol over stdio).",
	Long: `Start the Corral MCP server on stdio.

The server exposes the local Corral-organised workspace (cloned
repositories under the configured base directory) to AI coding agents
through five read-only tools and four resources. No network calls are
made and the GitHub API is not contacted.

Tools:
  corral_list_repos        - filter clones by visibility/language/name
  corral_find_repo         - resolve a fuzzy name to one clone
  corral_get_repo_metadata - detailed info incl. current branch
  corral_status_summary    - aggregate counts by visibility + language
  corral_workspace_index   - full workspace index as JSON

Resources:
  corral://workspace/index
  corral://repo/{owner}/{name}/state
  corral://repo/{owner}/{name}/tree
  corral://repo/{owner}/{name}/file/{path}

Install in Claude Code:
  claude mcp add corral -- corralctl mcp

Install in Cursor / Cline (mcp.json snippet):
  {
    "mcpServers": {
      "corral": {
        "command": "corralctl",
        "args": ["mcp"]
      }
    }
  }`,
	RunE: runMCP,
}

// mcpServer is the subset of the internal/mcp.Server API runMCP touches.
// Extracted as an interface so the unit test can stand up a stub without
// spinning up a real stdio server that would block forever on the
// test's os.Stdin.
type mcpServer interface {
	Root() string
	MutationsEnabled() bool
	ServeStdio() error
}

// mcpNewServer is indirected through a package var so unit tests can
// exercise runMCP's validation, wiring, and error-propagation paths
// without depending on the mcp-go library's stdio loop. Production
// callers get the real constructor.
var mcpNewServer = func(opts mcp.ServerOptions) (mcpServer, error) {
	return mcp.NewServer(opts)
}

func runMCP(cmd *cobra.Command, args []string) error {
	root := mcpRoot
	if root == "" {
		root = baseDir
	}
	if root == "" {
		root = defaultBaseDir()
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolving root %q: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("root %q is not accessible: %w", abs, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("root %q is not a directory", abs)
	}

	srv, err := mcpNewServer(mcp.ServerOptions{
		Root:            abs,
		Version:         Version,
		EnableMutations: mcpEnableMutations,
	})
	if err != nil {
		return fmt.Errorf("constructing mcp server: %w", err)
	}

	// Startup banner on stderr — stdout is the protocol stream.
	fmt.Fprintf(os.Stderr, "corral-mcp v%s starting; root=%s mutations=%t\n",
		Version, srv.Root(), srv.MutationsEnabled())

	if err := srv.ServeStdio(); err != nil {
		return fmt.Errorf("mcp server: %w", err)
	}
	return nil
}

func init() {
	mcpCmd.Flags().StringVar(&mcpRoot, "root", "", "absolute path the server sandboxes itself to (defaults to --base-dir, then $HOME/Code)")
	mcpCmd.Flags().BoolVar(&mcpEnableMutations, "enable-mutations", false, "unlock write tools (reserved for Phase 3; no-op in v0.0.8)")
	rootCmd.AddCommand(mcpCmd)
}
