// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package symbols

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Benchmarks for the extraction path.
//
// This package carries the claim that a cross-repository symbol lookup
// went from 6.9 seconds to 1.3 on a real workspace, and until now nothing
// guarded it: a change that made extraction three times slower would have
// passed every gate. These are the two halves that claim rests on — the
// walk, which turned out to dominate, and the parsing, which did not.

// benchRepo lays out n source files across a few directories.
func benchRepo(b *testing.B, n int) string {
	b.Helper()
	root := b.TempDir()
	body := "package a\n\n" +
		"// Alpha does something.\nfunc Alpha() {}\n\n" +
		"type Config struct {\n\tName string\n}\n\n" +
		"func (c *Config) Load() error { return nil }\n\n" +
		"const Limit = 10\n"
	for i := 0; i < n; i++ {
		dir := filepath.Join(root, fmt.Sprintf("pkg%02d", i%10))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			b.Fatal(err)
		}
		f := filepath.Join(dir, fmt.Sprintf("file%04d.go", i))
		if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	return root
}

// BenchmarkExtractRepo is the whole operation, walk and parse together.
func BenchmarkExtractRepo(b *testing.B) {
	for _, n := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("files=%d", n), func(b *testing.B) {
			root := benchRepo(b, n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := ExtractRepo(context.Background(), root); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDiscover isolates the walk, which measurement showed to be the
// dominant cost — roughly four times the parsing on a real workspace. A
// regression here matters more than one in the extractors.
func BenchmarkDiscover(b *testing.B) {
	root := benchRepo(b, 500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := discover(context.Background(), root); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExtractRepoCached is what a warm cache buys. The gap between
// this and BenchmarkExtractRepo is the parsing the cache skips; the time
// that remains is the walk, which a cache hit still pays because that is
// what keeps a stale entry from being served.
func BenchmarkExtractRepoCached(b *testing.B) {
	root := benchRepo(b, 500)
	cache := NewDiskCache(b.TempDir())
	if _, err := ExtractRepoCached(context.Background(), root, cache); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ExtractRepoCached(context.Background(), root, cache); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGoExtractor and BenchmarkScannedExtractor separate the parser
// from the walk, so a change to one language's scanner is attributable.
func BenchmarkGoExtractor(b *testing.B) {
	src := []byte("package a\n\nfunc Alpha() {}\ntype T struct{}\nfunc (t T) M() {}\nconst C = 1\n")
	e, _ := ExtractorFor("a.go")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Extract("a.go", src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScannedExtractor(b *testing.B) {
	for path, src := range map[string]string{
		"a.py": "MAX = 1\n\n\nclass C:\n    def m(self):\n        pass\n\n\ndef f():\n    pass\n",
		"a.ts": "export const MAX = 1;\nexport interface I { x: number }\nexport function f() {}\nclass C { m() {} }\n",
		"a.rs": "pub const MAX: u8 = 1;\npub struct S;\nimpl S {\n    pub fn m(&self) {}\n}\npub trait T {}\n",
	} {
		b.Run(path, func(b *testing.B) {
			e, ok := ExtractorFor(path)
			if !ok {
				b.Fatalf("no extractor for %s", path)
			}
			body := []byte(src)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := e.Extract(path, body); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
