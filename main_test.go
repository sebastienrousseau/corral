// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
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
