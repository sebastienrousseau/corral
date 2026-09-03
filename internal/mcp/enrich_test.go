// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// enrichWorkspace lays out n repositories under a Corral-shaped tree.
func enrichWorkspace(t *testing.T, n int) string {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("repo-%04d", i)
		dir := filepath.Join(root, "Public", "go", name)
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		cfg := fmt.Sprintf("[remote \"origin\"]\n\turl = https://github.com/acme/%s.git\n", name)
		if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestScanIsOrderStable is the property the pipeline must not break.
//
// Workers append in whatever order they finish, so determinism comes from
// the sort rather than from arrival order. If that sort were ever removed,
// two identical scans would disagree and an agent paging through results
// would see them shuffle.
func TestScanIsOrderStable(t *testing.T) {
	root := enrichWorkspace(t, 64)

	first, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Repos) != 64 {
		t.Fatalf("found %d repositories, want 64", len(first.Repos))
	}
	for i, e := range first.Repos {
		if e.RemoteURL == "" {
			t.Errorf("entry %d was not enriched: no remote URL", i)
		}
		if i > 0 && first.Repos[i-1].RelPath >= e.RelPath {
			t.Fatalf("entries are not sorted by RelPath at %d: %q then %q",
				i, first.Repos[i-1].RelPath, e.RelPath)
		}
	}

	for run := 0; run < 8; run++ {
		again, err := Scan(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Repos) != len(first.Repos) {
			t.Fatalf("run %d found %d, first found %d", run, len(again.Repos), len(first.Repos))
		}
		for i := range again.Repos {
			if again.Repos[i].Path != first.Repos[i].Path ||
				again.Repos[i].RemoteURL != first.Repos[i].RemoteURL {
				t.Fatalf("run %d differs at %d: %+v vs %+v", run, i, again.Repos[i], first.Repos[i])
			}
		}
	}
}

// TestScanSingleWorkerAgreesWithPool pins that the pool size is an
// optimisation and not a behaviour switch.
func TestScanSingleWorkerAgreesWithPool(t *testing.T) {
	root := enrichWorkspace(t, 40)

	pooled, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}

	old := scanWorkers
	scanWorkers = func() int { return 1 }
	t.Cleanup(func() { scanWorkers = old })

	serial, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(pooled.Repos) != len(serial.Repos) {
		t.Fatalf("pooled found %d, serial found %d", len(pooled.Repos), len(serial.Repos))
	}
	for i := range serial.Repos {
		if pooled.Repos[i] != serial.Repos[i] {
			t.Errorf("entry %d differs:\n pooled %+v\n serial %+v", i, pooled.Repos[i], serial.Repos[i])
		}
	}
}

// TestScanMoreWorkersThanRepos covers the case where the pool outnumbers
// the work: every worker must still exit cleanly when the queue closes.
func TestScanMoreWorkersThanRepos(t *testing.T) {
	old := scanWorkers
	scanWorkers = func() int { return maxScanWorkers }
	t.Cleanup(func() { scanWorkers = old })

	root := enrichWorkspace(t, 1)
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Repos) != 1 {
		t.Fatalf("found %d repositories, want 1", len(idx.Repos))
	}
}

// TestScanEmptyWorkspace: the pool must shut down cleanly when the walk
// hands it nothing at all.
func TestScanEmptyWorkspace(t *testing.T) {
	idx, err := Scan(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Repos) != 0 {
		t.Errorf("empty workspace yielded %d repositories", len(idx.Repos))
	}
	if idx.Truncated {
		t.Error("empty workspace should not report truncation")
	}
}

// TestScanTruncatesAtTheCap covers the bound, and that the queue still
// drains cleanly after the walk aborts early.
func TestScanTruncatesAtTheCap(t *testing.T) {
	old := maxIndexRepos
	maxIndexRepos = 5
	t.Cleanup(func() { maxIndexRepos = old })

	root := enrichWorkspace(t, 20)
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if !idx.Truncated {
		t.Error("hitting the repository cap must be reported")
	}
	if len(idx.Repos) > 5 {
		t.Errorf("kept %d repositories, want at most the cap of 5", len(idx.Repos))
	}
}

func TestScanWorkersIsBounded(t *testing.T) {
	n := scanWorkers()
	if n < 1 || n > maxScanWorkers {
		t.Errorf("scanWorkers = %d, want within [1, %d]", n, maxScanWorkers)
	}
}

func TestClampWorkers(t *testing.T) {
	// The bounds depend on the host's core count, so they are asserted
	// against the pure function rather than left to whatever this machine
	// happens to have.
	for _, tc := range []struct{ in, want int }{
		{-8, 1}, {0, 1}, {1, 1}, {24, 24},
		{maxScanWorkers, maxScanWorkers},
		{maxScanWorkers + 1, maxScanWorkers},
		{4096, maxScanWorkers},
	} {
		if got := clampWorkers(tc.in); got != tc.want {
			t.Errorf("clampWorkers(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestFindUsesPrecomputedKeysAndHandWrittenEntries covers both sides of the
// lookup fast path: entries built by buildEntry carry lowercase keys, and
// entries constructed by hand (as tests and embedders do) do not, so the
// fallback must still find them.
func TestFindUsesPrecomputedKeysAndHandWrittenEntries(t *testing.T) {
	root := enrichWorkspace(t, 2)
	idx, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Find("repo-0000"); err != nil {
		t.Errorf("built entry not found by name: %v", err)
	}
	if _, err := idx.Find("REPO-0001"); err != nil {
		t.Errorf("lookup should be case-insensitive: %v", err)
	}
	if _, err := idx.Find("go/repo-0000"); err != nil {
		t.Errorf("built entry not found by path suffix: %v", err)
	}

	// A hand-written entry has no precomputed keys at all.
	manual := &Index{Root: root, Repos: []RepoEntry{
		{Name: "Handmade", Path: "/x/Public/Go/Handmade", RelPath: "Public/Go/Handmade"},
	}}
	if _, err := manual.Find("handmade"); err != nil {
		t.Errorf("hand-written entry not found by name: %v", err)
	}
	if _, err := manual.Find("Public/Go/Handmade"); err != nil {
		t.Errorf("hand-written entry not found by rel path: %v", err)
	}
	if _, err := manual.Find("go/handmade"); err != nil {
		t.Errorf("hand-written entry not found by suffix: %v", err)
	}
	if _, err := manual.Find("nothing"); err == nil {
		t.Error("expected a miss for an unknown query")
	}
}
