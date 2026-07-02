// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"strings"
	"testing"
	"time"
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

// TestScanCache verifies the workspace-index cache actually amortises
// filesystem walks and that invalidateScanCache() forces a re-scan.
// The behaviour matters because a v0.0.10 goal was to remove the
// per-tool-call FS walk overhead on 100+ clone workspaces.
func TestScanCache(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")

	srv := newTestServer(t, base)

	first, err := srv.scan()
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if len(first.Repos) != 1 {
		t.Fatalf("expected 1 repo in first scan, got %d", len(first.Repos))
	}

	// Add a new clone on disk. The cache must NOT reflect it — that's
	// the whole point of caching.
	makeFakeRepo(t, base, "Public", "rust", "beta", "", "")
	cached, err := srv.scan()
	if err != nil {
		t.Fatalf("cached scan: %v", err)
	}
	if len(cached.Repos) != 1 {
		t.Errorf("expected cached scan to still show 1 repo, got %d", len(cached.Repos))
	}
	// Same *Index pointer means we hit the cache, not a re-walk.
	if cached != first {
		t.Error("expected identical Index pointer on cache hit")
	}

	// After explicit invalidation, the next call sees the new clone.
	srv.invalidateScanCache()
	fresh, err := srv.scan()
	if err != nil {
		t.Fatalf("post-invalidate scan: %v", err)
	}
	if len(fresh.Repos) != 2 {
		t.Errorf("expected 2 repos after invalidate, got %d", len(fresh.Repos))
	}
}

// TestScanCacheExpires guards the TTL contract: after scanTTL elapses,
// the cache must let a caller see filesystem changes without an
// explicit invalidate call.
func TestScanCacheExpires(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")

	srv := newTestServer(t, base)
	first, err := srv.scan()
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	_ = first

	// Rewind the cache's expiry timestamp to simulate scanTTL elapsing
	// without actually sleeping in the test.
	srv.scanMu.Lock()
	srv.scanExpires = time.Now().Add(-time.Second)
	srv.scanMu.Unlock()

	makeFakeRepo(t, base, "Public", "rust", "beta", "", "")
	fresh, err := srv.scan()
	if err != nil {
		t.Fatalf("post-expiry scan: %v", err)
	}
	if len(fresh.Repos) != 2 {
		t.Errorf("expected 2 repos after TTL expiry, got %d", len(fresh.Repos))
	}
}
