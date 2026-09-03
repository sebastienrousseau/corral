// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// enrichWorkspace lays out n repositories and returns the root plus their
// discovered paths in walk order.
func enrichWorkspace(t *testing.T, n int) (string, []string) {
	t.Helper()
	root := t.TempDir()
	paths := make([]string, 0, n)
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
		paths = append(paths, dir)
	}
	return root, paths
}

// TestEnrichEntriesIsOrderStable is the property the concurrent enrichment
// must not break: the output must follow discovery order, not completion
// order, or the index would shuffle between identical scans.
func TestEnrichEntriesIsOrderStable(t *testing.T) {
	// Above minParallelScan, so this exercises the concurrent path.
	root, paths := enrichWorkspace(t, 64)

	first := enrichEntries(root, paths)
	if len(first) != len(paths) {
		t.Fatalf("got %d entries, want %d", len(first), len(paths))
	}
	for i, e := range first {
		if e.Path != paths[i] {
			t.Fatalf("entry %d is %s, want %s: output order does not follow input", i, e.Path, paths[i])
		}
		if e.RemoteURL == "" {
			t.Errorf("entry %d was not enriched: no remote URL", i)
		}
	}

	// Repeated runs must agree exactly.
	for run := 0; run < 5; run++ {
		again := enrichEntries(root, paths)
		for i := range again {
			if again[i].Path != first[i].Path || again[i].RemoteURL != first[i].RemoteURL {
				t.Fatalf("run %d differs at %d: %+v vs %+v", run, i, again[i], first[i])
			}
		}
	}
}

// TestEnrichEntriesSerialAndConcurrentAgree pins that the threshold is an
// optimisation and not a behaviour switch.
func TestEnrichEntriesSerialAndConcurrentAgree(t *testing.T) {
	root, paths := enrichWorkspace(t, minParallelScan+8)

	concurrent := enrichEntries(root, paths)

	// Force the serial path by shrinking the worker count to 1 and using a
	// slice below the threshold, then compare entry by entry.
	oldWorkers := scanWorkers
	scanWorkers = func() int { return 1 }
	t.Cleanup(func() { scanWorkers = oldWorkers })

	serial := make([]RepoEntry, 0, len(paths))
	for _, p := range paths {
		serial = append(serial, buildEntry(root, p))
	}

	if len(concurrent) != len(serial) {
		t.Fatalf("length differs: %d vs %d", len(concurrent), len(serial))
	}
	for i := range serial {
		if concurrent[i].Path != serial[i].Path ||
			concurrent[i].RelPath != serial[i].RelPath ||
			concurrent[i].RemoteURL != serial[i].RemoteURL ||
			concurrent[i].Name != serial[i].Name {
			t.Errorf("entry %d differs:\n concurrent %+v\n serial     %+v", i, concurrent[i], serial[i])
		}
	}
}

func TestEnrichEntriesEdgeCases(t *testing.T) {
	root, paths := enrichWorkspace(t, 1)

	if got := enrichEntries(root, nil); got != nil {
		t.Errorf("enrichEntries(nil) = %v, want nil", got)
	}
	if got := enrichEntries(root, []string{}); got != nil {
		t.Errorf("enrichEntries(empty) = %v, want nil", got)
	}
	single := enrichEntries(root, paths)
	if len(single) != 1 || single[0].Path != paths[0] {
		t.Errorf("single-entry enrichment = %+v", single)
	}
}

// TestEnrichEntriesMoreWorkersThanRepos covers the clamp: the pool must not
// spawn more goroutines than there is work for them.
func TestEnrichEntriesMoreWorkersThanRepos(t *testing.T) {
	oldWorkers := scanWorkers
	scanWorkers = func() int { return 1000 }
	t.Cleanup(func() { scanWorkers = oldWorkers })

	root, paths := enrichWorkspace(t, minParallelScan+1)
	got := enrichEntries(root, paths)
	if len(got) != len(paths) {
		t.Fatalf("got %d entries, want %d", len(got), len(paths))
	}
	for i := range got {
		if got[i].Path != paths[i] {
			t.Errorf("entry %d out of order", i)
		}
	}
}

func TestScanWorkersIsBounded(t *testing.T) {
	n := scanWorkers()
	if n < 1 {
		t.Errorf("scanWorkers = %d, must be at least 1", n)
	}
	if n > 32 {
		t.Errorf("scanWorkers = %d, must be capped at 32", n)
	}
}

// TestFindUsesPrecomputedKeysAndHandWrittenEntries covers both sides of the
// PERF-2 fast path: entries built by buildEntry carry lowercase keys, and
// entries constructed by hand (as tests and embedders do) do not, so the
// fallback must still find them.
func TestFindUsesPrecomputedKeysAndHandWrittenEntries(t *testing.T) {
	root, paths := enrichWorkspace(t, 2)
	built := enrichEntries(root, paths)

	idx := &Index{Root: root, Repos: built}
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
