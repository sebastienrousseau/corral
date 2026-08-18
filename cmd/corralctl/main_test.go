// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestMainExec(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"corralctl", "-h"}

	// Redirect stdout/stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()
	os.Stdout = mustDevNull(t)
	os.Stderr = mustDevNull(t)

	main()
}

func TestMainGitResolutionFailure(t *testing.T) {
	oldResolve, oldExecute, oldExit := resolveGitBinary, executeContext, exitMain
	t.Cleanup(func() { resolveGitBinary, executeContext, exitMain = oldResolve, oldExecute, oldExit })
	resolveGitBinary = func() error { return errors.New("git missing") }
	executeContext = func(context.Context) { t.Fatal("execute must not run") }
	exitCode := 0
	exitMain = func(code int) { exitCode = code }
	main()
	if exitCode != 1 {
		t.Fatalf("exit code = %d", exitCode)
	}
}

func TestMainDelegatesContext(t *testing.T) {
	oldResolve, oldExecute, oldExit := resolveGitBinary, executeContext, exitMain
	t.Cleanup(func() { resolveGitBinary, executeContext, exitMain = oldResolve, oldExecute, oldExit })
	resolveGitBinary = func() error { return nil }
	called := false
	executeContext = func(ctx context.Context) { called = ctx != nil }
	exitMain = func(code int) { t.Fatalf("unexpected exit %d", code) }
	main()
	if !called {
		t.Fatal("ExecuteContext was not called")
	}
}

// mustDevNull opens /dev/null for writing and closes it when the test ends.
// See the note on the cmd package's copy: `os.NewFile(0, os.DevNull)` wraps
// stdin rather than opening anything, and its finalizer closes fd 0.
func mustDevNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
