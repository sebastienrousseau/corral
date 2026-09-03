// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// timeoutCmd builds a throwaway command carrying just the timeout flags, so
// a test can say which of them the user "typed" without touching the real
// command tree.
//
// The flags bind to throwaway locals, never to the package variables:
// DurationVar assigns the default the moment it is called, which would
// silently undo whatever the test had set.
func timeoutCmd(typed ...string) *cobra.Command {
	var req, total, dep time.Duration
	c := &cobra.Command{Use: "probe"}
	fs := pflag.NewFlagSet("probe", pflag.ContinueOnError)
	fs.DurationVar(&req, "api-request-timeout", 30*time.Second, "")
	fs.DurationVar(&total, "api-total-timeout", 10*time.Minute, "")
	fs.DurationVar(&dep, "api-timeout", 0, "")
	c.Flags().AddFlagSet(fs)
	for _, name := range typed {
		_ = c.Flags().Set(name, c.Flags().Lookup(name).Value.String())
	}
	return c
}

func captureWarning(t *testing.T) *string {
	t.Helper()
	var got string
	old := deprecationWarning
	deprecationWarning = func(format string, args ...any) {
		got = format
		_ = args
	}
	t.Cleanup(func() { deprecationWarning = old })
	return &got
}

// TestDeprecatedAPITimeoutSuppliesBothHalves covers the migration path. The
// flag was documented as a per-request deadline and applied as both, so
// anyone who raised it did so to buy a bigger budget overall — setting both
// halves preserves exactly what they got.
func TestDeprecatedAPITimeoutSuppliesBothHalves(t *testing.T) {
	old := []time.Duration{apiRequestTimeout, apiTotalTimeout, apiTimeout}
	t.Cleanup(func() { apiRequestTimeout, apiTotalTimeout, apiTimeout = old[0], old[1], old[2] })

	warned := captureWarning(t)
	cmd := timeoutCmd()
	apiRequestTimeout, apiTotalTimeout, apiTimeout = 30*time.Second, 10*time.Minute, 45*time.Second

	if err := resolveAPITimeouts(cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if apiRequestTimeout != 45*time.Second {
		t.Errorf("request timeout = %s, want 45s", apiRequestTimeout)
	}
	if apiTotalTimeout != 45*time.Second {
		t.Errorf("total timeout = %s, want 45s", apiTotalTimeout)
	}
	if !strings.Contains(*warned, "deprecated") {
		t.Errorf("no deprecation warning was emitted, got %q", *warned)
	}
}

// TestExplicitFlagsBeatTheDeprecatedOne: a user mid-migration may pass both.
// What they typed explicitly must win.
func TestExplicitFlagsBeatTheDeprecatedOne(t *testing.T) {
	old := []time.Duration{apiRequestTimeout, apiTotalTimeout, apiTimeout}
	t.Cleanup(func() { apiRequestTimeout, apiTotalTimeout, apiTimeout = old[0], old[1], old[2] })
	captureWarning(t)

	cmd := timeoutCmd("api-total-timeout")
	apiRequestTimeout, apiTotalTimeout, apiTimeout = 30*time.Second, 20*time.Minute, 45*time.Second

	if err := resolveAPITimeouts(cmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if apiTotalTimeout != 20*time.Minute {
		t.Errorf("an explicit --api-total-timeout was overwritten: got %s, want 20m", apiTotalTimeout)
	}
	// The half the user did not type still takes the deprecated value.
	if apiRequestTimeout != 45*time.Second {
		t.Errorf("request timeout = %s, want 45s from the deprecated flag", apiRequestTimeout)
	}
}

func TestResolveAPITimeoutsRejectsIncoherentValues(t *testing.T) {
	old := []time.Duration{apiRequestTimeout, apiTotalTimeout, apiTimeout}
	t.Cleanup(func() { apiRequestTimeout, apiTotalTimeout, apiTimeout = old[0], old[1], old[2] })
	captureWarning(t)

	tests := []struct {
		name             string
		req, total, dep  time.Duration
		wantErrSubstring string
	}{
		{"negative deprecated", 30 * time.Second, time.Minute, -1, "--api-timeout must be > 0"},
		{"zero request", 0, time.Minute, 0, "--api-request-timeout must be > 0"},
		{"negative request", -1, time.Minute, 0, "--api-request-timeout must be > 0"},
		{"zero total", 30 * time.Second, 0, 0, "--api-total-timeout must be > 0"},
		{"total below request", time.Minute, time.Second, 0, "must be >= --api-request-timeout"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := timeoutCmd()
			apiRequestTimeout, apiTotalTimeout, apiTimeout = tc.req, tc.total, tc.dep
			err := resolveAPITimeouts(cmd)
			if err == nil {
				t.Fatalf("expected an error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstring) {
				t.Errorf("error %q lacks %q", err, tc.wantErrSubstring)
			}
		})
	}
}

// TestResolveAPITimeoutsAcceptsANilCommand covers the defensive branch: the
// validator is reachable from tests and embedders with no cobra command in
// hand, and must treat that as "the user typed nothing".
func TestResolveAPITimeoutsAcceptsANilCommand(t *testing.T) {
	old := []time.Duration{apiRequestTimeout, apiTotalTimeout, apiTimeout}
	t.Cleanup(func() { apiRequestTimeout, apiTotalTimeout, apiTimeout = old[0], old[1], old[2] })
	captureWarning(t)

	apiRequestTimeout, apiTotalTimeout, apiTimeout = 30*time.Second, 10*time.Minute, 0
	if err := resolveAPITimeouts(nil); err != nil {
		t.Fatalf("a nil command must be accepted: %v", err)
	}
}

// TestDeprecationWarningGoesToStderr pins the invariant that makes
// --output json pipeable: stdout carries the selected format, and a
// diagnostic on it corrupts the stream.
func TestDeprecationWarningGoesToStderr(t *testing.T) {
	oldErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	stdout := captureStdout(t, func() {
		deprecationWarning("corralctl: %s is deprecated", "--api-timeout")
	})
	_ = w.Close()
	os.Stderr = oldErr
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if stdout != "" {
		t.Errorf("the deprecation warning reached stdout: %q", stdout)
	}
	if !strings.Contains(buf.String(), "deprecated") {
		t.Errorf("stderr = %q, want the warning", buf.String())
	}
}
