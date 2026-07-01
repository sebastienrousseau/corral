package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightSummaryShowsOwnerAndAbsolutePath(t *testing.T) {
	got := preflightSummary("sebastienrousseau", ".")
	if !strings.Contains(got, "Owner:    sebastienrousseau") {
		t.Errorf("summary missing owner line: %q", got)
	}
	// Extract the Target: line and confirm the path is absolute in a
	// platform-portable way (POSIX starts with '/', Windows starts with
	// a drive letter like C:\ — both satisfy filepath.IsAbs).
	targetLine := ""
	for _, line := range strings.Split(got, "\n") {
		if after, ok := strings.CutPrefix(line, "Target:"); ok {
			targetLine = strings.TrimSpace(after)
			break
		}
	}
	if targetLine == "" {
		t.Fatalf("summary missing Target: line: %q", got)
	}
	if !filepath.IsAbs(targetLine) {
		t.Errorf("summary should absolute-ise target so a relative path is obvious: got %q from %q", targetLine, got)
	}
}

func TestPreflightConfirmSkipsPromptWithYesFlag(t *testing.T) {
	var w bytes.Buffer
	ok := preflightConfirm(&w, strings.NewReader(""), true, "sebastienrousseau", "/tmp/nowhere-real", true, false)
	if !ok {
		t.Error("--yes must always proceed")
	}
	if strings.Contains(w.String(), "Continue?") {
		t.Error("prompt must not appear when --yes is set")
	}
}

func TestPreflightConfirmSkipsPromptWithDryRun(t *testing.T) {
	var w bytes.Buffer
	ok := preflightConfirm(&w, strings.NewReader(""), true, "sebastienrousseau", "/tmp/nowhere-real", false, true)
	if !ok {
		t.Error("--dry-run implies no destructive action, must skip prompt")
	}
	if strings.Contains(w.String(), "Continue?") {
		t.Error("prompt must not appear during dry-run")
	}
}

func TestPreflightConfirmSkipsPromptOnNonTTY(t *testing.T) {
	var w bytes.Buffer
	ok := preflightConfirm(&w, strings.NewReader(""), false, "sebastienrousseau", "/tmp/nowhere-real", false, false)
	if !ok {
		t.Error("non-TTY (scripted) invocations must proceed without prompting")
	}
	if strings.Contains(w.String(), "Continue?") {
		t.Error("prompt must not appear on non-TTY")
	}
}

func TestPreflightConfirmSkipsPromptWhenTargetExists(t *testing.T) {
	dir := t.TempDir()
	var w bytes.Buffer
	ok := preflightConfirm(&w, strings.NewReader(""), true, "sebastienrousseau", dir, false, false)
	if !ok {
		t.Error("existing target must proceed without prompting")
	}
	if strings.Contains(w.String(), "Continue?") {
		t.Error("prompt must not appear when target already exists")
	}
}

// The "typo" scenario the user hit — target doesn't exist yet and stdin
// is a TTY. Must prompt; must proceed on "y"; must abort on "n" or EOF.
func TestPreflightConfirmPromptsOnFreshTarget(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "will-not-exist")

	cases := []struct {
		name   string
		input  string
		wantOK bool
	}{
		{"y proceeds", "y\n", true},
		{"yes proceeds", "yes\n", true},
		{"Y proceeds (case-insensitive)", "Y\n", true},
		{"n aborts", "n\n", false},
		{"empty line aborts (default deny)", "\n", false},
		{"anything else aborts", "maybe\n", false},
		{"EOF aborts", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var w bytes.Buffer
			got := preflightConfirm(&w, strings.NewReader(tc.input), true, "i", baseDir, false, false)
			if got != tc.wantOK {
				t.Errorf("input=%q: got %v, want %v", tc.input, got, tc.wantOK)
			}
			if !strings.Contains(w.String(), "Continue?") {
				t.Errorf("expected prompt to appear in output, got: %q", w.String())
			}
		})
	}
}

// A pipe-write failure on the banner must not hang the caller —
// exercises the io.Writer error path indirectly by using a writer
// that always fails.
func TestPreflightConfirmToleratesBrokenWriter(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "still-fresh")
	ok := preflightConfirm(errorWriter{}, strings.NewReader("y\n"), true, "i", baseDir, false, false)
	if !ok {
		t.Error("broken stdout should not block confirmation")
	}
}

// errorWriter returns an error on every write, simulating a closed pipe.
type errorWriter struct{}

func (errorWriter) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

// runPreflight itself is a thin wrapper — exercise it with all flags on
// so the file-under-test compile-time reference to the closure is
// covered without needing to fake stdin.
func TestRunPreflightWrapper(t *testing.T) {
	// Force --yes so the wrapper never blocks on a nonexistent stdin
	// when go test runs headless.
	oldYes := assumeYes
	defer func() { assumeYes = oldYes }()
	assumeYes = true

	// Capture stderr momentarily so the banner doesn't pollute the
	// test output.
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	got := runPreflight("sebastienrousseau", t.TempDir())
	_ = w.Close()
	_, _ = io.ReadAll(r)
	if !got {
		t.Error("runPreflight with --yes should proceed")
	}
}
