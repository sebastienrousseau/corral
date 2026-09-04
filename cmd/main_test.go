// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points every XDG directory at a throwaway location, for the
// same reason internal/mcp does.
//
// This package builds a real MCP server in the help-text gate, and that
// server creates its on-disk symbol cache under $XDG_CACHE_HOME. Without
// this the suite writes into the developer's own ~/.cache — which it
// did, and which is how this was found.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "corral-cmd-xdg-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating a throwaway XDG root: %v\n", err)
		os.Exit(1)
	}
	for _, v := range []string{"XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		if err := os.Setenv(v, dir); err != nil {
			fmt.Fprintf(os.Stderr, "setting %s: %v\n", v, err)
			os.Exit(1)
		}
	}

	code := m.Run()

	// Removed after the run rather than with a defer, because os.Exit
	// does not run deferred functions.
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
