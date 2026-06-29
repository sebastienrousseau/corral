package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
