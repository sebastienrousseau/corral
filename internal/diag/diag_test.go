// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package diag

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// capture installs a buffer as the diagnostic sink for one test and restores
// the previous level and sink afterwards, so tests cannot leak verbosity into
// each other.
func capture(t *testing.T, l Level) *bytes.Buffer {
	t.Helper()
	previous := CurrentLevel()
	var buf bytes.Buffer
	SetOutput(&buf)
	SetLevel(l)
	t.Cleanup(func() {
		SetLevel(previous)
		SetOutput(nil)
	})
	return &buf
}

func TestLevelNames(t *testing.T) {
	cases := map[Level]string{
		LevelError: "error",
		LevelWarn:  "warn",
		LevelInfo:  "info",
		LevelDebug: "debug",
	}
	for level, want := range cases {
		if got := level.String(); got != want {
			t.Errorf("Level(%d).String() = %q, want %q", int(level), got, want)
		}
	}
	if got := Level(99).String(); !strings.Contains(got, "99") {
		t.Errorf("an out-of-range level rendered as %q, which names no level", got)
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    Level
		wantErr bool
	}{
		{"error", LevelError, false},
		{"ERROR", LevelError, false},
		{"warn", LevelWarn, false},
		{"warning", LevelWarn, false},
		{" Warn ", LevelWarn, false},
		{"info", LevelInfo, false},
		{"", LevelInfo, false},
		{"debug", LevelDebug, false},
		{"verbose", LevelInfo, true},
	}
	for _, tc := range cases {
		got, err := ParseLevel(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseLevel(%q) accepted an unknown level", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q) failed: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestDefaultLevelShowsWhatCorralAlwaysShowed(t *testing.T) {
	buf := capture(t, LevelInfo)

	Errorf("clone failed: %v", errors.New("network unreachable"))
	Warnf("not migrating %s", "~/Code/go/tools")
	Infof("fetching repositories")
	Debugf("state sidecar absent")

	out := buf.String()
	for _, want := range []string{"ERROR: clone failed", "WARN: not migrating", "INFO: fetching"} {
		if !strings.Contains(out, want) {
			t.Errorf("default level dropped %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "DEBUG") {
		t.Errorf("debug output appeared at the default level:\n%s", out)
	}
}

func TestLevelsFilterFromTheBottomUp(t *testing.T) {
	cases := []struct {
		level   Level
		visible []string
		hidden  []string
	}{
		{LevelError, []string{"ERROR"}, []string{"WARN", "INFO", "DEBUG"}},
		{LevelWarn, []string{"ERROR", "WARN"}, []string{"INFO", "DEBUG"}},
		{LevelInfo, []string{"ERROR", "WARN", "INFO"}, []string{"DEBUG"}},
		{LevelDebug, []string{"ERROR", "WARN", "INFO", "DEBUG"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.level.String(), func(t *testing.T) {
			buf := capture(t, tc.level)
			Errorf("e")
			Warnf("w")
			Infof("i")
			Debugf("d")
			out := buf.String()
			for _, want := range tc.visible {
				if !strings.Contains(out, want) {
					t.Errorf("level %s hid %s:\n%s", tc.level, want, out)
				}
			}
			for _, unwanted := range tc.hidden {
				if strings.Contains(out, unwanted) {
					t.Errorf("level %s showed %s:\n%s", tc.level, unwanted, out)
				}
			}
		})
	}
}

func TestEnabledMatchesWhatIsEmitted(t *testing.T) {
	capture(t, LevelWarn)
	if !Enabled(LevelError) || !Enabled(LevelWarn) {
		t.Error("Enabled denied a level that is emitted at warn")
	}
	if Enabled(LevelInfo) || Enabled(LevelDebug) {
		t.Error("Enabled permitted a level that is filtered at warn")
	}
}

func TestEmitWritesOneTrimmedLine(t *testing.T) {
	buf := capture(t, LevelInfo)
	Infof("trailing newlines are the caller's habit, not the format's\n\n")

	out := buf.String()
	if strings.Count(out, "\n") != 1 {
		t.Fatalf("expected exactly one line, got %q", out)
	}
	if !strings.HasSuffix(out, "format's\n") {
		t.Fatalf("trailing newlines were not trimmed: %q", out)
	}
}

func TestSetOutputNilRestoresStderr(t *testing.T) {
	previous := CurrentLevel()
	t.Cleanup(func() { SetLevel(previous); SetOutput(nil) })

	SetOutput(&bytes.Buffer{})
	SetOutput(nil)

	mu.RLock()
	restored := output
	mu.RUnlock()
	if restored != io.Writer(os.Stderr) {
		t.Fatalf("SetOutput(nil) left the sink at %#v, want os.Stderr", restored)
	}
}
