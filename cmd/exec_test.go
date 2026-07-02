// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecCmdRun(t *testing.T) {
	// Create a temporary base directory
	base := t.TempDir()

	// Create some mock local repositories
	// Standard layout: public/go/repo1
	repo1Path := filepath.Join(base, "public", "go", "repo1")
	if err := os.MkdirAll(filepath.Join(repo1Path, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}

	// layout: private/rust/repo2
	repo2Path := filepath.Join(base, "private", "rust", "repo2")
	if err := os.MkdirAll(filepath.Join(repo2Path, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	// Configure flags
	baseDir = base
	execConcurrency = 2
	execVisibility = "all"
	execLanguages = ""
	execExcludeLangs = ""
	dryRun = false

	// Run with dryRun = true
	dryRun = true
	execCmd.Run(execCmd, []string{"echo test"})

	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "Would execute \"echo test\"") {
		t.Errorf("expected dry-run output, got: %s", output)
	}
	if !strings.Contains(output, "repo1") || !strings.Contains(output, "repo2") {
		t.Errorf("expected repo names in dry-run, got: %s", output)
	}

	// Test filters
	repos, err := findLocalRepos(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	// Test languages filter
	execLanguages = "go"
	filtered := filterLocalRepos(repos)
	if len(filtered) != 1 || !strings.Contains(filtered[0], "repo1") {
		t.Errorf("expected only repo1 with language go filter, got %v", filtered)
	}

	// Test exclude languages filter
	execLanguages = ""
	execExcludeLangs = "rust"
	filtered = filterLocalRepos(repos)
	if len(filtered) != 1 || !strings.Contains(filtered[0], "repo1") {
		t.Errorf("expected only repo1 with exclude rust filter, got %v", filtered)
	}

	// Test visibility filter
	execExcludeLangs = ""
	execVisibility = "private"
	filtered = filterLocalRepos(repos)
	if len(filtered) != 1 || !strings.Contains(filtered[0], "repo2") {
		t.Errorf("expected only repo2 with visibility private filter, got %v", filtered)
	}
}

// captureStdout runs fn while os.Stdout is redirected to a pipe, returning the
// captured output. Restores the original stdout even when fn panics.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan struct{})
	defer func() {
		os.Stdout = old
		<-done
	}()

	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()

	fn()
	_ = w.Close()
	<-done
	// Re-open the pipe semantics: reset done so the deferred restore can proceed.
	done = make(chan struct{}, 1)
	done <- struct{}{}
	return buf.String()
}

// TestRunExecCommandsSuccess covers the happy path: a short, harmless command
// runs in every supplied repo and produces a per-repo header. Without this
// test, runExecCommands had 0% coverage despite being a flagship subcommand.
func TestRunExecCommandsSuccess(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available; this test relies on POSIX shell semantics")
	}
	base := t.TempDir()

	// Two repos, both with a .git/ so findLocalRepos picks them up if called.
	a := filepath.Join(base, "public", "go", "alpha")
	b := filepath.Join(base, "public", "rust", "beta")
	for _, p := range []string{a, b} {
		if err := os.MkdirAll(filepath.Join(p, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
	}

	out := captureStdout(t, func() {
		runExecCommands(context.Background(), []string{a, b}, "echo hello", 2)
	})

	for _, name := range []string{"alpha", "beta"} {
		if !strings.Contains(out, "--- ["+name+"] ---") {
			t.Errorf("expected per-repo header for %q in output: %q", name, out)
		}
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected command stdout in captured output, got %q", out)
	}
}

// TestRunExecCommandsReportsFailure ensures a non-zero exit status surfaces in
// the per-repo output instead of being silently swallowed.
func TestRunExecCommandsReportsFailure(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh not available")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "public", "go", "fails")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		runExecCommands(context.Background(), []string{repo}, "exit 7", 1)
	})

	if !strings.Contains(out, "Command failed") {
		t.Errorf("expected 'Command failed' message for non-zero exit, got %q", out)
	}
}

// TestRunExecCommandsHonoursContextCancellation pre-cancels the context and
// asserts the worker pool drains without spawning any subprocess output.
// Without this guard, an aborted run could leak workers.
func TestRunExecCommandsHonoursContextCancellation(t *testing.T) {
	base := t.TempDir()
	var paths []string
	for i := 0; i < 4; i++ {
		p := filepath.Join(base, "public", "go", "r"+string(rune('a'+i)))
		if err := os.MkdirAll(filepath.Join(p, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		runExecCommands(ctx, paths, "sleep 5", 2)
		close(done)
	}()

	select {
	case <-done:
		// Workers exited promptly via the ctx.Done() branch.
	case <-time.After(2 * time.Second):
		t.Fatal("runExecCommands did not honour ctx cancellation within 2s")
	}
}

// TestRunExecCommandsEmpty exercises the no-repos path: workers should exit
// immediately when the jobs channel is empty.
func TestRunExecCommandsEmpty(t *testing.T) {
	done := make(chan struct{})
	go func() {
		runExecCommands(context.Background(), nil, "echo x", 1)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runExecCommands hung on empty input")
	}
}

// TestExecCmdPreRunEValidation ensures the cobra-level guards reject bad
// flag combinations before any work happens.
func TestExecCmdPreRunEValidation(t *testing.T) {
	cases := []struct {
		name    string
		setup   func()
		wantErr string
	}{
		{
			name:    "zero concurrency",
			setup:   func() { execConcurrency = 0; execVisibility = "all" },
			wantErr: "--concurrency",
		},
		{
			name:    "invalid visibility",
			setup:   func() { execConcurrency = 1; execVisibility = "shared" },
			wantErr: "--visibility",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			err := execCmd.PreRunE(execCmd, []string{"echo"})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error to mention %q, got %v", tc.wantErr, err)
			}
		})
	}
	// Restore good defaults so later tests don't inherit broken state.
	execConcurrency = 4
	execVisibility = "all"
}

// TestExecCmdNoMatchingRepos covers the "no repos found" early return.
func TestExecCmdNoMatchingRepos(t *testing.T) {
	baseDir = t.TempDir()
	execConcurrency = 1
	execVisibility = "all"
	execLanguages = ""
	execExcludeLangs = ""
	dryRun = false

	out := captureStdout(t, func() {
		execCmd.Run(execCmd, []string{"echo x"})
	})
	if !strings.Contains(out, "No matching repositories found") {
		t.Errorf("expected no-match message, got %q", out)
	}
}

// TestExecCmdScanError covers the findLocalRepos failure branch by pointing
// baseDir at a path that's a file rather than a directory.
func TestExecCmdScanError(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "exec_scan_*")
	if err != nil {
		t.Fatal(err)
	}
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	// findLocalRepos uses WalkDir; on a regular file it short-circuits with nil
	// (the walker swallows per-entry errors), returning an empty slice. The
	// outer Run then takes the "no matching repos" branch — assert that, plus
	// the lack of crash on a non-directory baseDir.
	baseDir = tmpFile.Name()
	execConcurrency = 1
	execVisibility = "all"
	execLanguages = ""
	execExcludeLangs = ""
	dryRun = false

	out := captureStdout(t, func() {
		execCmd.Run(execCmd, []string{"echo x"})
	})
	if !strings.Contains(out, "No matching repositories found") {
		t.Errorf("expected no-match message for file baseDir, got %q", out)
	}
}
