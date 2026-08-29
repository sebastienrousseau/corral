// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// buildWorkspace lays out n repositories under a Corral-shaped tree and
// returns the root. Used by the scan and lookup benchmarks below.
func buildWorkspace(b *testing.B, n int) string {
	b.Helper()
	root := b.TempDir()
	for i := 0; i < n; i++ {
		repo := filepath.Join(root, "Public", "go", fmt.Sprintf("repo-%04d", i))
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o750); err != nil {
			b.Fatal(err)
		}
		cfg := fmt.Sprintf("[remote \"origin\"]\n\turl = https://github.com/acme/repo-%04d.git\n", i)
		if err := os.WriteFile(filepath.Join(repo, ".git", "config"), []byte(cfg), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	return root
}

// BenchmarkScan measures the workspace walk that every MCP tool call sits
// behind. It is the one operation whose cost grows with the user's
// repository count, so a regression here is felt on every request.
func BenchmarkScan(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("repos=%d", size), func(b *testing.B) {
			root := buildWorkspace(b, size)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Scan(root); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkIndexFind measures the query path an agent hits on every tool
// call once the index is warm.
func BenchmarkIndexFind(b *testing.B) {
	idx, err := Scan(buildWorkspace(b, 1000))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := idx.Find("repo-0999"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSafePath measures the sandbox check. It runs on every resource
// read and every mutation, so it is on the hot path for the security
// boundary as well as for throughput.
func BenchmarkSafePath(b *testing.B) {
	root := buildWorkspace(b, 1)
	idx := &Index{Root: root}
	target := filepath.Join(root, "Public", "go", "repo-0000")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := idx.SafePath(target); err != nil {
			b.Fatal(err)
		}
	}
}
