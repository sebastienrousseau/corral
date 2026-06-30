package mcp

import (
	"strings"
	"testing"
)

func TestNewServerRejectsBadOptions(t *testing.T) {
	cases := []struct {
		name string
		opts ServerOptions
		want string
	}{
		{
			name: "empty root",
			opts: ServerOptions{},
			want: "Root must not be empty",
		},
		{
			name: "relative root",
			opts: ServerOptions{Root: "relative/path"},
			want: "must be an absolute path",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewServer(tc.opts)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error to contain %q, got %v", tc.want, err)
			}
		})
	}
}

func TestNewServerAcceptsAbsoluteRoot(t *testing.T) {
	tmp := t.TempDir()
	srv, err := NewServer(ServerOptions{Root: tmp, Version: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.Root() != tmp {
		t.Errorf("Root() = %q, want %q", srv.Root(), tmp)
	}
	if srv.MutationsEnabled() {
		t.Error("mutations should default to false")
	}
}

func TestNewServerDefaultsVersionToDev(t *testing.T) {
	tmp := t.TempDir()
	srv, err := NewServer(ServerOptions{Root: tmp})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.opts.Version != "dev" {
		t.Errorf("Version default = %q, want 'dev'", srv.opts.Version)
	}
}

func TestNewServerMutationsToggle(t *testing.T) {
	tmp := t.TempDir()
	srv, err := NewServer(ServerOptions{Root: tmp, EnableMutations: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !srv.MutationsEnabled() {
		t.Error("expected MutationsEnabled to reflect option")
	}
}

func TestIsAbsolutePath(t *testing.T) {
	cases := map[string]bool{
		"":           false,
		"foo":        false,
		"./foo":      false,
		"/foo":       true,
		"/":          true,
		"C:\\Users":  true,
		"D:/foo":     true,
		"C:foo":      false, // missing separator after colon
		"~/Code":     false, // ~ is shell-expanded; we want literal abs
	}
	for in, want := range cases {
		if got := isAbsolutePath(in); got != want {
			t.Errorf("isAbsolutePath(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDescribeRepo(t *testing.T) {
	r := RepoEntry{
		Name:       "corral",
		Visibility: "Public",
		Language:   "go",
		RelPath:    "Public/go/corral",
		RemoteURL:  "https://github.com/seb/corral.git",
		State:      &StateRecord{LastSyncedAt: "2026-06-30T10:00:00Z"},
	}
	got := describeRepo(r)
	for _, want := range []string{"Public/go/corral", "[public/go]", "github.com/seb/corral", "last_synced=2026-06-30"} {
		if !strings.Contains(got, want) {
			t.Errorf("describeRepo missing %q: %q", want, got)
		}
	}
}

func TestJSONResult(t *testing.T) {
	res := jsonResult(map[string]any{"a": 1})
	if res == nil {
		t.Fatal("jsonResult returned nil")
	}
	if res.IsError {
		t.Error("expected non-error result")
	}
}

func TestJSONResultErrorOnMarshalFailure(t *testing.T) {
	oldMarshal := jsonMarshalIndent
	defer func() { jsonMarshalIndent = oldMarshal }()
	jsonMarshalIndent = func(v any) ([]byte, error) {
		return nil, errMarshal
	}
	res := jsonResult(map[string]any{"a": 1})
	if !res.IsError {
		t.Error("expected error result when marshal fails")
	}
}

var errMarshal = &stubErr{msg: "stub marshal failure"}

type stubErr struct{ msg string }

func (e *stubErr) Error() string { return e.msg }
