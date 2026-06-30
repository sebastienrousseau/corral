package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeFakeRepo creates a directory layout that looks like a corral-
// managed clone: <base>/<vis>/<lang>/<name>/.git with an optional
// .git/config to seed the remote URL and an optional sidecar.
func makeFakeRepo(t *testing.T, base, vis, lang, name, originURL, sidecar string) string {
	t.Helper()
	repo := filepath.Join(base, vis, lang, name)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	if originURL != "" {
		cfg := "[remote \"origin\"]\n\turl = " + originURL + "\n"
		if err := os.WriteFile(filepath.Join(repo, ".git", "config"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if sidecar != "" {
		if err := os.WriteFile(filepath.Join(repo, stateFileName), []byte(sidecar), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func TestScanFindsExpectedLayout(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "https://github.com/o/alpha.git", `{"last_synced_at":"2026-06-30T00:00:00Z"}`)
	makeFakeRepo(t, base, "Public", "rust", "beta", "", "")
	makeFakeRepo(t, base, "Private", "python", "gamma", "", "")

	idx, err := Scan(base)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(idx.Repos) != 3 {
		t.Fatalf("expected 3 repos, got %d: %+v", len(idx.Repos), idx.Repos)
	}
	// Sorted by RelPath: Private/python/gamma, Public/go/alpha, Public/rust/beta
	want := []string{"Private/python/gamma", "Public/go/alpha", "Public/rust/beta"}
	for i, r := range idx.Repos {
		if r.RelPath != want[i] {
			t.Errorf("repo[%d] RelPath = %q, want %q", i, r.RelPath, want[i])
		}
	}
	// First entry has no state, second has remote+state.
	alpha := idx.Repos[1]
	if alpha.RemoteURL != "https://github.com/o/alpha.git" {
		t.Errorf("alpha remote = %q", alpha.RemoteURL)
	}
	if alpha.State == nil || alpha.State.LastSyncedAt == "" {
		t.Errorf("alpha state not parsed: %+v", alpha.State)
	}
	if alpha.Visibility != "Public" || alpha.Language != "go" {
		t.Errorf("alpha vis/lang wrong: %q/%q", alpha.Visibility, alpha.Language)
	}
}

func TestScanRejectsNonDirectoryRoot(t *testing.T) {
	tmp, err := os.CreateTemp("", "mcp_root_*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_ = tmp.Close()

	_, err = Scan(tmp.Name())
	if err == nil {
		t.Fatal("expected error scanning a file as root")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected 'not a directory' message, got %v", err)
	}
}

func TestScanTolerates_UnreadableSubtree(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "ok", "", "")
	// Drop a regular file at the path the walker would treat as a
	// candidate parent — confirms a per-entry error doesn't abort the
	// whole scan.
	if err := os.WriteFile(filepath.Join(base, "garbage"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	idx, err := Scan(base)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(idx.Repos) != 1 || idx.Repos[0].Name != "ok" {
		t.Errorf("expected one clone 'ok', got %+v", idx.Repos)
	}
}

func TestScanHonoursMaxDepth(t *testing.T) {
	base := t.TempDir()
	// Construct a path deeper than maxIndexDepth. The repo should NOT
	// be picked up because the walker stops descending.
	deep := filepath.Join(base, "a", "b", "c", "d", "e", "deep")
	if err := os.MkdirAll(filepath.Join(deep, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	idx, err := Scan(base)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if len(idx.Repos) != 0 {
		t.Errorf("expected depth limit to hide deep repo, got %+v", idx.Repos)
	}
}

func TestIndexFindUniqueAndAmbiguous(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	makeFakeRepo(t, base, "Private", "go", "alpha", "", "") // intentional dupe
	makeFakeRepo(t, base, "Public", "rust", "beta", "", "")

	idx, err := Scan(base)
	if err != nil {
		t.Fatal(err)
	}

	// Unique by bare name when only one match.
	match, err := idx.Find("beta")
	if err != nil {
		t.Fatalf("Find(beta) err: %v", err)
	}
	if match.RelPath != "Public/rust/beta" {
		t.Errorf("Find(beta) returned %s", match.RelPath)
	}

	// Ambiguous bare name surfaces all candidates.
	_, err = idx.Find("alpha")
	if !errors.Is(err, ErrAmbiguous) {
		t.Errorf("Find(alpha) expected ErrAmbiguous, got %v", err)
	}

	// Path suffix disambiguates.
	match, err = idx.Find("Public/go/alpha")
	if err != nil {
		t.Fatalf("Find(Public/go/alpha) err: %v", err)
	}
	if match.Visibility != "Public" {
		t.Errorf("expected Public match, got %s", match.Visibility)
	}

	// Unknown returns ErrRepoNotFound.
	_, err = idx.Find("does-not-exist")
	if !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("expected ErrRepoNotFound, got %v", err)
	}

	// Empty query is treated as not-found.
	_, err = idx.Find("")
	if !errors.Is(err, ErrRepoNotFound) {
		t.Errorf("expected ErrRepoNotFound on empty query, got %v", err)
	}
}

func TestSafePathRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	// Create a file inside and outside the root.
	if err := os.WriteFile(filepath.Join(base, "inside.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir() // entirely separate root
	if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	idx := &Index{Root: base}

	// Inside path is allowed.
	if _, err := idx.SafePath(filepath.Join(base, "inside.txt")); err != nil {
		t.Errorf("inside path should be allowed: %v", err)
	}
	// Relative inside path is allowed and joined to root.
	if _, err := idx.SafePath("inside.txt"); err != nil {
		t.Errorf("relative inside path should be allowed: %v", err)
	}
	// Outside absolute path is rejected.
	if _, err := idx.SafePath(filepath.Join(outside, "outside.txt")); err == nil {
		t.Error("absolute outside path should be rejected")
	}
	// .. traversal is rejected.
	if _, err := idx.SafePath(filepath.Join(base, "..", "escape")); err == nil {
		t.Error("traversal via .. should be rejected")
	}
}
