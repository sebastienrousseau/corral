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
	os.Stdout = os.NewFile(0, os.DevNull)
	os.Stderr = os.NewFile(0, os.DevNull)

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
