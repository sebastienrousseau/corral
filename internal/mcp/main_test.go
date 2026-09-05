// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"fmt"
	"os"
	"testing"

	"go.uber.org/goleak"
)

// TestMain points every XDG directory this package consults at a
// throwaway location.
//
// It is here because of a real mistake: the on-disk symbol cache defaults
// to $XDG_CACHE_HOME/corral/symbols, and the first test run after it was
// added wrote eight entries into the developer's own ~/.cache. Two things
// are wrong with that. A test suite must not modify the machine it runs
// on; and a cache that persists between runs makes a test asserting a
// miss pass or fail depending on what an earlier run happened to leave
// behind.
//
// Doing it here rather than in the harness covers the thirty-odd tests
// that build a Server directly, which a harness cannot reach — and it
// keeps working for the next default that reads an XDG variable.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "corral-mcp-xdg-")
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

	// goleak.Find rather than goleak.VerifyTestMain, because this TestMain
	// has cleanup of its own and VerifyTestMain exits the process itself.
	//
	// Only checked on an otherwise-passing run: a failing test can abandon
	// a goroutine legitimately — it returned early — and reporting that as
	// a leak on top of the real failure buries the thing worth reading.
	if code == 0 {
		if err := goleak.Find(); err != nil {
			fmt.Fprintf(os.Stderr, "goroutine leak: %v\n", err)
			code = 1
		}
	}
	os.Exit(code)
}
