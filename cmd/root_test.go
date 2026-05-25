package cmd

import (
	"context"
	"os"
	"testing"

	"github.com/sebastienrousseau/corral/internal/engine"
)

func TestExecute(t *testing.T) {
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()
	os.Stdout = os.NewFile(0, os.DevNull)
	os.Stderr = os.NewFile(0, os.DevNull)

	var exitCode int
	oldOsExit := osExit
	defer func() { osExit = oldOsExit }()
	osExit = func(code int) {
		exitCode = code
	}

	rootCmd.SetArgs([]string{})
	err := rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for missing args, got nil")
	}

	oldEngineRun := engineRun
	defer func() { engineRun = oldEngineRun }()
	engineRun = func(ctx context.Context, opts engine.RunOptions) {}

	rootCmd.SetArgs([]string{"owner", "basedir", "10"})
	_ = rootCmd.Execute()

	oldConcurrency := concurrency
	oldLimit := limit
	oldProtocol := protocol
	oldOutput := output
	oldAuthMode := authMode
	oldVisibility := visibility
	oldCloneDepth := cloneDepth
	oldRetryMax := retryMax
	oldRetryMin := retryMinBackoff
	oldRetryMaxB := retryMaxBackoff
	defer func() {
		concurrency = oldConcurrency
		limit = oldLimit
		protocol = oldProtocol
		output = oldOutput
		authMode = oldAuthMode
		visibility = oldVisibility
		cloneDepth = oldCloneDepth
		retryMax = oldRetryMax
		retryMinBackoff = oldRetryMin
		retryMaxBackoff = oldRetryMaxB
	}()

	exitCode = 0
	rootCmd.SetArgs([]string{"owner", "basedir", "abc"})
	_ = rootCmd.Execute()
	if exitCode != 1 {
		t.Errorf("Expected exit code 1 for invalid limit argument, got %d", exitCode)
	}

	rootCmd.SetArgs([]string{"owner"})
	concurrency = 0
	err = rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for invalid concurrency")
	}

	concurrency = 1
	limit = -1
	err = rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for negative limit")
	}

	limit = 1000
	protocol = "ftp"
	err = rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for invalid protocol")
	}

	protocol = "https"
	output = "xml"
	err = rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for invalid output")
	}

	output = "text"
	authMode = "bad"
	err = rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for invalid auth mode")
	}

	authMode = "auto"
	visibility = "secret"
	err = rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for invalid visibility")
	}

	visibility = "all"
	cloneDepth = -1
	err = rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for invalid clone depth")
	}

	cloneDepth = 0
	retryMax = -1
	err = rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for invalid retry max")
	}

	retryMax = 1
	retryMinBackoff = 0
	err = rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for invalid retry min backoff")
	}

	retryMinBackoff = oldRetryMin
	retryMaxBackoff = oldRetryMin / 2
	err = rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for invalid retry backoff ordering")
	}

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"corral"}
	rootCmd.SetArgs([]string{})
	exitCode = 0
	Execute()
	if exitCode != 1 {
		t.Errorf("Expected Execute() to exit with code 1 when no args provided, got %d", exitCode)
	}

	os.Args = []string{"corral", "-h"}
	rootCmd.SetArgs([]string{"-h"})
	exitCode = 0
	Execute()
	if exitCode != 0 {
		t.Errorf("Expected Execute() to succeed with code 0 when help provided, got %d", exitCode)
	}
}

func TestParseCSV(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"go", 1},
		{"go, rust", 2},
		{"go, , rust", 2},
	}
	for _, tc := range cases {
		got := parseCSV(tc.in)
		if len(got) != tc.want {
			t.Fatalf("parseCSV(%q) len=%d want=%d", tc.in, len(got), tc.want)
		}
	}
}
