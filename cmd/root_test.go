package cmd

import (
	"os"
	"testing"
)

func TestExecute(t *testing.T) {
	// Redirect stdout and stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()
	os.Stdout = os.NewFile(0, os.DevNull)
	os.Stderr = os.NewFile(0, os.DevNull)

	// Mock osExit
	var exitCode int
	oldOsExit := osExit
	defer func() { osExit = oldOsExit }()
	osExit = func(code int) {
		exitCode = code
	}

	// Test missing args
	rootCmd.SetArgs([]string{})
	err := rootCmd.Execute()
	if err == nil {
		t.Errorf("Expected error for missing args, got nil")
	}

	// Test successful limit argument
	oldEngineRun := engineRun
	defer func() { engineRun = oldEngineRun }()
	engineRun = func(owner, baseDir string, limit, concurrency int, dryRun, orphans bool, protocol string, doSync, recurseSubmodules bool) {
	}

	rootCmd.SetArgs([]string{"owner", "basedir", "10"})
	_ = rootCmd.Execute()
	exitCode = 0
	rootCmd.SetArgs([]string{"owner", "basedir", "abc"})
	_ = rootCmd.Execute()
	if exitCode != 1 {
		t.Errorf("Expected exit code 1 for invalid limit argument, got %d", exitCode)
	}

	// Test Execute function directly (which calls rootCmd.Execute())
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"corral"} // No args means rootCmd.Execute() will fail
	rootCmd.SetArgs([]string{})
	exitCode = 0
	Execute()
	if exitCode != 1 {
		t.Errorf("Expected Execute() to exit with code 1 when no args provided, got %d", exitCode)
	}

	// Test Execute function successfully
	os.Args = []string{"corral", "-h"}
	rootCmd.SetArgs([]string{"-h"})
	exitCode = 0
	Execute()
	if exitCode != 0 {
		t.Errorf("Expected Execute() to succeed with code 0 when help provided, got %d", exitCode)
	}
}
