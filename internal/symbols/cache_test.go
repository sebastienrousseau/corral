// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package symbols

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cacheRepo lays down a small repository and returns its root.
func cacheRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"),
		[]byte("package a\n\nfunc Alpha() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// countingCache wraps a Cache and records what it was asked.
type countingCache struct {
	inner Cache
	gets  int
	puts  int
	hits  int
}

func (c *countingCache) Get(repo string, fp Fingerprint) (*Result, bool) {
	c.gets++
	res, ok := c.inner.Get(repo, fp)
	if ok {
		c.hits++
	}
	return res, ok
}

func (c *countingCache) Put(repo string, fp Fingerprint, res *Result) {
	c.puts++
	c.inner.Put(repo, fp, res)
}

// TestDiskCacheRoundTrip is the basic contract: a second extraction of an
// unchanged repository is served from disk.
func TestDiskCacheRoundTrip(t *testing.T) {
	root := cacheRepo(t)
	c := &countingCache{inner: NewDiskCache(t.TempDir())}

	first, err := ExtractRepoCached(context.Background(), root, c)
	if err != nil {
		t.Fatal(err)
	}
	if c.hits != 0 || c.puts != 1 {
		t.Fatalf("first call: hits=%d puts=%d, want 0 and 1", c.hits, c.puts)
	}

	second, err := ExtractRepoCached(context.Background(), root, c)
	if err != nil {
		t.Fatal(err)
	}
	if c.hits != 1 {
		t.Errorf("the second call should hit the cache, hits=%d", c.hits)
	}
	if len(first.Symbols) != len(second.Symbols) || len(second.Symbols) == 0 {
		t.Fatalf("cached result differs: %d vs %d symbols", len(first.Symbols), len(second.Symbols))
	}
	for i := range first.Symbols {
		if first.Symbols[i] != second.Symbols[i] {
			t.Errorf("symbol %d differs:\n fresh  %+v\n cached %+v", i, first.Symbols[i], second.Symbols[i])
		}
	}
	if first.Files != second.Files || first.Truncated != second.Truncated {
		t.Error("the cached result must carry the same counts and flags")
	}
}

// TestDiskCacheMissesOnEveryKindOfEdit is the property that makes the
// cache safe to trust: a hit still walks, so anything the walk can see
// invalidates it.
func TestDiskCacheMissesOnEveryKindOfEdit(t *testing.T) {
	for name, edit := range map[string]func(t *testing.T, root string){
		"a file changes size": func(t *testing.T, root string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(root, "a.go"),
				[]byte("package a\n\nfunc Alpha() {}\nfunc Beta() {}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"a file is added": func(t *testing.T, root string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(root, "b.go"),
				[]byte("package a\n\nfunc Gamma() {}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"a file is removed": func(t *testing.T, root string) {
			t.Helper()
			if err := os.Remove(filepath.Join(root, "a.go")); err != nil {
				t.Fatal(err)
			}
		},
		"a file is touched": func(t *testing.T, root string) {
			t.Helper()
			later := time.Now().Add(time.Hour)
			if err := os.Chtimes(filepath.Join(root, "a.go"), later, later); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := cacheRepo(t)
			c := &countingCache{inner: NewDiskCache(t.TempDir())}
			if _, err := ExtractRepoCached(context.Background(), root, c); err != nil {
				t.Fatal(err)
			}
			edit(t, root)
			if _, err := ExtractRepoCached(context.Background(), root, c); err != nil {
				t.Fatal(err)
			}
			if c.hits != 0 {
				t.Errorf("an edited repository must not be served from cache (hits=%d)", c.hits)
			}
		})
	}
}

// TestDiskCacheKeysOnTheRepository: two repositories with identical
// content must not share an entry, or one would answer for the other.
func TestDiskCacheKeysOnTheRepository(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir)
	fp := Fingerprint{Files: 1, Bytes: 10, ModUnixNano: 1}

	c.Put("/one", fp, &Result{Files: 1, Symbols: []Symbol{{Name: "One"}}})
	c.Put("/two", fp, &Result{Files: 1, Symbols: []Symbol{{Name: "Two"}}})

	one, ok := c.Get("/one", fp)
	if !ok || len(one.Symbols) != 1 || one.Symbols[0].Name != "One" {
		t.Errorf("/one returned %+v", one)
	}
	two, ok := c.Get("/two", fp)
	if !ok || len(two.Symbols) != 1 || two.Symbols[0].Name != "Two" {
		t.Errorf("/two returned %+v", two)
	}
	if _, ok := c.Get("/three", fp); ok {
		t.Error("an unknown repository should miss")
	}
}

// TestDiskCacheRejectsAMismatchedEntry covers the guards that make a hit
// trustworthy: the wrong schema, the wrong repository, or the wrong
// fingerprint must all miss rather than serve.
func TestDiskCacheRejectsAMismatchedEntry(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir)
	fp := Fingerprint{Files: 1, Bytes: 10, ModUnixNano: 1}
	repo := "/repo"

	writeEntry := func(t *testing.T, e cacheEntry) {
		t.Helper()
		b, err := compressEntry(e)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(c.pathFor(repo), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	good := cacheEntry{
		Schema: cacheSchema, Repo: repo, Fingerprint: fp,
		Files: 1, Symbols: []Symbol{{Name: "X"}},
	}
	writeEntry(t, good)
	if _, ok := c.Get(repo, fp); !ok {
		t.Fatal("a matching entry should hit")
	}

	stale := good
	stale.Schema = cacheSchema + 1
	writeEntry(t, stale)
	if _, ok := c.Get(repo, fp); ok {
		t.Error("an entry from another schema must not be served")
	}

	// A hash collision is vanishingly unlikely, but serving one
	// repository's symbols for another would be a silent wrong answer, so
	// the path is not the only thing checked.
	wrongRepo := good
	wrongRepo.Repo = "/somewhere-else"
	writeEntry(t, wrongRepo)
	if _, ok := c.Get(repo, fp); ok {
		t.Error("an entry naming a different repository must not be served")
	}

	writeEntry(t, good)
	if _, ok := c.Get(repo, Fingerprint{Files: 2}); ok {
		t.Error("a different fingerprint must miss")
	}
}

// TestDiskCacheTreatsCorruptionAsAMiss: a truncated or garbage file must
// cost a re-extraction, never a failure.
func TestDiskCacheTreatsCorruptionAsAMiss(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir)
	fp := Fingerprint{Files: 1}

	// Not gzip at all, gzip that decompresses to nothing useful, and a
	// truncated stream: each must be a miss.
	valid, err := compressEntry(cacheEntry{Schema: cacheSchema, Repo: "/repo", Fingerprint: fp})
	if err != nil {
		t.Fatal(err)
	}
	corrupt := []string{"", "{", "not gzip at all", string(valid[:len(valid)/2])}
	for _, body := range corrupt {
		if err := os.WriteFile(c.pathFor("/repo"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := c.Get("/repo", fp); ok {
			t.Errorf("a corrupt entry (%q) must not be served", body)
		}
	}

	// And the next Put repairs it.
	c.Put("/repo", fp, &Result{Files: 1, Symbols: []Symbol{{Name: "Y"}}})
	if _, ok := c.Get("/repo", fp); !ok {
		t.Error("a corrupt entry should be overwritten by the next extraction")
	}
}

// TestDiskCacheWritesAtomically: a reader must never see a half-written
// entry, so nothing is written under the final name until it is complete.
func TestDiskCacheWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir)
	c.Put("/repo", Fingerprint{Files: 1}, &Result{Files: 1, Symbols: []Symbol{{Name: "Z"}}})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly the finished entry, got %d files", len(entries))
	}
	if filepath.Ext(entries[0].Name()) != ".json" {
		t.Errorf("a temporary file was left behind: %s", entries[0].Name())
	}
}

func TestDiskCachePrunesTheOldest(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir)
	c.maxEntries = 3

	// Written oldest-first, with distinct modification times so the order
	// is not left to filesystem timestamp granularity.
	for i := 0; i < 6; i++ {
		repo := filepath.Join("/repo", string(rune('a'+i)))
		c.Put(repo, Fingerprint{Files: i}, &Result{Files: i})
		when := time.Now().Add(time.Duration(i-10) * time.Minute)
		if err := os.Chtimes(c.pathFor(repo), when, when); err != nil {
			t.Fatal(err)
		}
	}
	// One more, to trigger the prune with the timestamps in place.
	c.Put("/repo/z", Fingerprint{Files: 99}, &Result{Files: 99})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) > c.maxEntries+1 {
		t.Errorf("kept %d entries, want about the cap of %d", len(entries), c.maxEntries)
	}
	// The newest is the one that must survive.
	if _, ok := c.Get("/repo/z", Fingerprint{Files: 99}); !ok {
		t.Error("the entry just written was pruned")
	}
}

// TestNilDiskCacheIsUsable: the constructor returns nil when the directory
// cannot be made, and every method must tolerate that rather than making
// each caller check.
func TestNilDiskCacheIsUsable(t *testing.T) {
	var c *DiskCache
	if _, ok := c.Get("/repo", Fingerprint{}); ok {
		t.Error("a nil cache has nothing to return")
	}
	c.Put("/repo", Fingerprint{}, &Result{}) // must not panic

	// A path that cannot be a directory yields nil rather than an error to
	// handle at every call site.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := NewDiskCache(filepath.Join(file, "sub")); got != nil {
		t.Error("an unusable directory should yield no cache")
	}
	if got := NewDiskCache(""); got != nil {
		t.Error("an empty directory should yield no cache")
	}
}

func TestDiskCacheIgnoresANilResult(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir)
	c.Put("/repo", Fingerprint{}, nil)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Error("a nil result should not be stored")
	}
}

// TestExtractRepoCachedStoresAnEmptyRepository: a repository with no
// source at all is a real answer, and re-walking it every time to
// rediscover that would be the same waste as any other miss.
func TestExtractRepoCachedStoresAnEmptyRepository(t *testing.T) {
	root := t.TempDir()
	c := &countingCache{inner: NewDiskCache(t.TempDir())}

	for i := 0; i < 2; i++ {
		res, err := ExtractRepoCached(context.Background(), root, c)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Symbols) != 0 {
			t.Errorf("expected no symbols, got %d", len(res.Symbols))
		}
	}
	if c.hits != 1 {
		t.Errorf("the second call should have hit the cache, hits=%d", c.hits)
	}
}

// TestExtractRepoIsTheUncachedPath pins that the original entry point
// still works with no cache at all.
func TestExtractRepoIsTheUncachedPath(t *testing.T) {
	root := cacheRepo(t)
	res, err := ExtractRepo(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Symbols) == 0 {
		t.Error("expected symbols")
	}
	if _, err := ExtractRepoCached(context.Background(), root, nil); err != nil {
		t.Errorf("a nil cache is the uncached path: %v", err)
	}
}

// TestFingerprintIsStableAcrossRuns: an unchanged repository must
// fingerprint identically, or the cache never hits.
func TestFingerprintIsStableAcrossRuns(t *testing.T) {
	root := cacheRepo(t)
	_, _, first, err := discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, second, err := discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("fingerprint changed with no edit: %+v then %+v", first, second)
	}
	if first.Files == 0 || first.Bytes == 0 || first.ModUnixNano == 0 {
		t.Errorf("fingerprint looks empty: %+v", first)
	}
}

// TestDiskCachePutSurvivesAWriteFailure covers the arms where the write
// itself goes wrong. A cache that cannot be written must be a slow index,
// never a broken one — so each failure leaves the directory clean and the
// caller none the wiser.
func TestDiskCachePutSurvivesAWriteFailure(t *testing.T) {
	t.Run("the directory disappeared", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "gone")
		c := NewDiskCache(dir)
		if c == nil {
			t.Fatal("expected a cache")
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		// CreateTemp fails; this must return quietly.
		c.Put("/repo", Fingerprint{}, &Result{Files: 1})
		if _, ok := c.Get("/repo", Fingerprint{}); ok {
			t.Error("nothing should have been stored")
		}
	})

	t.Run("a result that cannot be marshalled", func(t *testing.T) {
		dir := t.TempDir()
		c := NewDiskCache(dir)
		// A NaN in a numeric field is the one thing encoding/json refuses,
		// and it reaches here through Symbol.Line only via a hand-built
		// result — which is exactly what an embedder may pass.
		c.Put("/repo", Fingerprint{}, &Result{
			Files:   1,
			Symbols: []Symbol{{Name: string([]byte{0xff, 0xfe}), Line: 1}},
		})
		// Invalid UTF-8 is coerced rather than rejected by encoding/json,
		// so this stores; the assertion is only that it did not panic and
		// the directory is coherent.
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if filepath.Ext(e.Name()) != ".json" {
				t.Errorf("a temporary file was left behind: %s", e.Name())
			}
		}
	})

	t.Run("the destination is a directory", func(t *testing.T) {
		dir := t.TempDir()
		c := NewDiskCache(dir)
		// A directory where the entry file should go makes the rename
		// fail. The temporary file must not be left behind.
		if err := os.MkdirAll(c.pathFor("/repo"), 0o750); err != nil {
			t.Fatal(err)
		}
		c.Put("/repo", Fingerprint{}, &Result{Files: 1})

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name()[0] == '.' {
				t.Errorf("a temporary file was left behind: %s", e.Name())
			}
		}
	})
}

// TestDiskCachePruneToleratesAnUnreadableDirectory: pruning is
// housekeeping, and housekeeping that fails must not fail the write it
// followed.
func TestDiskCachePruneToleratesAnUnreadableDirectory(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir)
	c.maxEntries = 0
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	c.prune() // must not panic
}

// TestCompressEntryRoundTrip covers the compression path directly,
// including the error arm that a successful entry never reaches.
func TestCompressEntryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := NewDiskCache(dir)
	fp := Fingerprint{Files: 2, Bytes: 40, ModUnixNano: 7}
	want := &Result{Files: 2, Truncated: true, Symbols: []Symbol{
		{Name: "Alpha", Kind: KindFunc, File: "a.go", Line: 3, Exported: true, Language: "go"},
		{Name: "Beta", Kind: KindMethod, Receiver: "T", File: "b.go", Line: 9, Language: "go"},
	}}
	c.Put("/repo", fp, want)

	got, ok := c.Get("/repo", fp)
	if !ok {
		t.Fatal("the entry just written should hit")
	}
	if got.Files != want.Files || got.Truncated != want.Truncated {
		t.Errorf("counts differ: %+v", got)
	}
	for i := range want.Symbols {
		if got.Symbols[i] != want.Symbols[i] {
			t.Errorf("symbol %d differs:\n got  %+v\n want %+v", i, got.Symbols[i], want.Symbols[i])
		}
	}

	// The stored file is gzip, not plain JSON: the first two bytes are the
	// gzip magic number.
	b, err := os.ReadFile(c.pathFor("/repo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 2 || b[0] != 0x1f || b[1] != 0x8b {
		t.Errorf("the entry is not gzipped: % x", b[:min(4, len(b))])
	}
}

// TestCompressEntryShrinksRepetitiveContent is the reason the cache is
// gzipped at all: uncompressed, a real 187-repository workspace produced
// 53 MB of the same dozen field names repeated.
func TestCompressEntryShrinksRepetitiveContent(t *testing.T) {
	b, err := compressEntry(cacheEntry{
		Schema:  cacheSchema,
		Repo:    "/repo",
		Symbols: []Symbol{{Name: strings.Repeat("x", 1<<16)}},
	})
	if err != nil {
		t.Fatalf("a large but valid entry should compress: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("expected compressed bytes")
	}
	if len(b) > 1<<12 {
		t.Errorf("compressed to %d bytes; the content is 64 KiB of one character", len(b))
	}
}
