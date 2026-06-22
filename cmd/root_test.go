package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sebastienrousseau/corral/internal/engine"
	"github.com/spf13/cobra"
)

func TestDefaultBaseDir(t *testing.T) {
	old := userHomeDir
	defer func() { userHomeDir = old }()

	userHomeDir = func() (string, error) { return "/home/example", nil }
	if got, want := defaultBaseDir(), filepath.Join("/home/example", "Code"); got != want {
		t.Errorf("defaultBaseDir() = %q, want %q", got, want)
	}

	userHomeDir = func() (string, error) { return "", fmt.Errorf("no home") }
	if got, want := defaultBaseDir(), filepath.Join(".", "Code"); got != want {
		t.Errorf("defaultBaseDir() fallback = %q, want %q", got, want)
	}
}

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

	// Cover the retryMaxBackoff <= 0 branch.
	retryMinBackoff = oldRetryMin
	retryMaxBackoff = 0
	err = rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for non-positive retry max backoff")
	}
	retryMaxBackoff = oldRetryMaxB

	// Cover the Run branch where the positional limit argument is negative.
	exitCode = 0
	rootCmd.SetArgs([]string{"owner", "basedir", "--", "-5"})
	_ = rootCmd.Execute()
	if exitCode != 1 {
		t.Errorf("Expected exit code 1 for negative limit argument, got %d", exitCode)
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

func TestCmdContext(t *testing.T) {
	// nil command returns context.Background().
	if got := cmdContext(nil); got == nil {
		t.Fatal("cmdContext(nil) returned nil context")
	}

	// Command with an explicitly set context returns that context.
	type ctxKey string
	const key ctxKey = "k"
	want := context.WithValue(context.Background(), key, "v")
	c := &cobra.Command{}
	c.SetContext(want)
	if got := cmdContext(c); got.Value(key) != "v" {
		t.Fatalf("cmdContext did not return the command's context")
	}

	// Command whose Context() returns context.Background by default.
	fresh := &cobra.Command{}
	if got := cmdContext(fresh); got == nil {
		t.Fatal("cmdContext(fresh) returned nil context")
	}
}

func TestExecuteContext(t *testing.T) {
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
	osExit = func(code int) { exitCode = code }

	oldEngineRun := engineRun
	defer func() { engineRun = oldEngineRun }()
	engineRun = func(ctx context.Context, opts engine.RunOptions) {}

	// Error path: missing required args makes Execute return an error,
	// triggering osExit(1). Reset the help flag in case a prior test left it
	// set, which would otherwise short-circuit into help output.
	if hf := rootCmd.Flags().Lookup("help"); hf != nil {
		if err := hf.Value.Set("false"); err != nil {
			t.Fatalf("failed to reset help flag: %v", err)
		}
		hf.Changed = false
	}
	rootCmd.SetArgs([]string{})
	exitCode = 0
	ExecuteContext(context.Background())
	if exitCode != 1 {
		t.Errorf("Expected exit code 1 for missing args, got %d", exitCode)
	}

	// nil context path: ExecuteContext should substitute context.Background()
	// and execute successfully. The nil is held in a variable so static
	// analysis does not flag the intentional nil-context argument.
	var nilCtx context.Context
	rootCmd.SetArgs([]string{"-h"})
	exitCode = 0
	ExecuteContext(nilCtx)
	if exitCode != 0 {
		t.Errorf("Expected exit code 0 for help with nil context, got %d", exitCode)
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
