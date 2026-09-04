// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Benchmarks for content search.
//
// Search reads every candidate file on every query — there is no index —
// so its cost is the thing most likely to become unacceptable as a
// workspace grows, and the thing most likely to be made worse by a
// well-meaning change to the matcher. Nothing guarded it before.

func benchTree(b *testing.B, files, linesPerFile int) string {
	b.Helper()
	root := b.TempDir()
	var body strings.Builder
	for i := 0; i < linesPerFile; i++ {
		if i == linesPerFile/2 {
			body.WriteString("const RetryLimit = 3 // the needle\n")
			continue
		}
		body.WriteString("// ordinary line of source that does not match anything\n")
	}
	content := body.String()
	for i := 0; i < files; i++ {
		dir := filepath.Join(root, fmt.Sprintf("pkg%02d", i%10))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			b.Fatal(err)
		}
		f := filepath.Join(dir, fmt.Sprintf("file%04d.go", i))
		if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	return root
}

// BenchmarkSearchRepo is the whole operation. The literal and regex forms
// are separated because the regex path cannot use the fast substring
// search and is the one a caller reaches for when a name is spelled
// differently per language.
func BenchmarkSearchRepo(b *testing.B) {
	root := benchTree(b, 200, 200)
	for name, q := range map[string]Query{
		"literal":      {Pattern: "RetryLimit", CaseSensitive: true, MaxHits: 1000},
		"literal fold": {Pattern: "retrylimit", MaxHits: 1000},
		"regex":        {Pattern: `retry_?limit`, Regex: true, MaxHits: 1000},
		"literal miss": {Pattern: "zzz-absent-zzz", MaxHits: 1000},
	} {
		b.Run(name, func(b *testing.B) {
			m, err := Compile(q)
			if err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := SearchRepo(context.Background(), root, m, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMatchLine isolates the per-line cost, which runs once for every
// line of every file and is where an accidental allocation hurts most.
//
// The case-insensitive form used to lowercase the whole line before
// searching it, so its allocation scaled with the line: 80 B/op here and
// proportionally more on a long one. It is now a quoted regex, which
// allocates 16 B/op regardless — see BenchmarkMatchLineLong.
func BenchmarkMatchLine(b *testing.B) {
	line := "\tconst RetryLimit = 3 // the needle in an otherwise ordinary line"
	for name, q := range map[string]Query{
		"literal":      {Pattern: "RetryLimit", CaseSensitive: true},
		"literal fold": {Pattern: "retrylimit"},
		"regex":        {Pattern: `retry_?limit`, Regex: true},
	} {
		b.Run(name, func(b *testing.B) {
			m, err := Compile(q)
			if err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.MatchLine(line)
			}
		})
	}
}

// BenchmarkSearchRepoSerial pins what the worker pool buys. A change that
// accidentally serialised the pool would otherwise look like ordinary
// drift.
func BenchmarkSearchRepoSerial(b *testing.B) {
	root := benchTree(b, 200, 200)
	old := searchWorkers
	searchWorkers = func() int { return 1 }
	b.Cleanup(func() { searchWorkers = old })

	m, err := Compile(Query{Pattern: "RetryLimit", CaseSensitive: true, MaxHits: 1000})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := SearchRepo(context.Background(), root, m, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMatchLineLong pins the property the allocation change bought:
// the per-line cost of a case-insensitive search no longer scales with
// the length of the line. Minified and generated files are exactly where
// that mattered.
func BenchmarkMatchLineLong(b *testing.B) {
	line := strings.Repeat("ordinary source text that does not match; ", 100) + "RetryLimit"
	for name, q := range map[string]Query{
		"literal fold": {Pattern: "retrylimit"},
		"literal":      {Pattern: "RetryLimit", CaseSensitive: true},
	} {
		b.Run(name, func(b *testing.B) {
			m, err := Compile(q)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.MatchLine(line)
			}
		})
	}
}
