// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package search

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Bounds. Every one of these exists because a workspace contains at least
// one repository that will otherwise make a search unusable — a vendored
// monorepo, a checked-in dataset, a minified bundle on one 4 MB line.
var (
	// maxFilesPerRepo bounds the walk.
	maxFilesPerRepo = 20_000
	// maxFileBytes skips a file too large to be hand-written source. A
	// generated bundle or a committed binary is never the answer to
	// "where is this used", and reading it costs more than every real
	// file put together.
	maxFileBytes int64 = 2 << 20 // 2 MiB
	// maxLineBytes bounds one line. A minified file is a single enormous
	// line; matching in it is useless and buffering it is expensive.
	maxLineBytes = 4096
	// maxHitTextBytes bounds the reported text of a hit, before the MCP
	// layer sanitises it further.
	maxHitTextBytes = 400
)

// searchWorkers bounds the read pool.
//
// Higher than GOMAXPROCS because this is I/O bound: a worker waiting on a
// read is not using a core, and the page cache makes the second pass over
// a workspace far cheaper than the first.
var searchWorkers = func() int {
	return clampWorkers(runtime.GOMAXPROCS(0) * 4)
}

const maxSearchWorkers = 32

// clampWorkers holds a worker count inside [1, maxSearchWorkers].
func clampWorkers(n int) int {
	if n < 1 {
		return 1
	}
	if n > maxSearchWorkers {
		return maxSearchWorkers
	}
	return n
}

// FileFilter reports whether a repository-relative path may be searched.
//
// Supplied by the caller rather than decided here, because the policy that
// matters lives in the MCP layer: the same allowlist that decides what the
// file resource will serve. A search that could match inside a file the
// server refuses to hand over would be a way to read it one line at a
// time.
type FileFilter func(rel string) bool

// IsTestFile reports whether a path looks like test code, by the
// conventions the major ecosystems share.
//
// Deliberately generous. A false positive hides a hit behind
// include_tests, which the caller can set; a false negative buries real
// answers under a repository's test suite, which they cannot undo.
func IsTestFile(rel string) bool {
	lower := strings.ToLower(rel)
	base := lower
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	switch {
	case strings.HasSuffix(base, "_test.go"),
		strings.HasSuffix(base, "_test.py"),
		strings.HasPrefix(base, "test_"),
		strings.HasSuffix(base, "_test.rs"),
		strings.Contains(base, ".test."),
		strings.Contains(base, ".spec."):
		return true
	}
	for _, seg := range strings.Split(lower, "/") {
		switch seg {
		case "test", "tests", "__tests__", "spec", "benches", "testdata":
			return true
		}
	}
	return false
}

// skipDir reports directories never worth descending into: build output
// and dependency trees, which dwarf hand-written source and are never what
// somebody is looking for in their own workspace.
func skipDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "vendor",
		"dist", "build", "target", "bin", "obj", ".venv", "venv",
		"__pycache__", ".next", ".cache", ".terraform", "DerivedData", "Pods":
		return true
	}
	return false
}

// SearchRepo walks one repository and returns the lines matching m.
//
// The walk is serial and the reading is concurrent, for the same reason
// the workspace scan and the symbol walk split them: directory entries are
// cheap and already cached, per-file work is not.
//
// ctx cancellation is honoured between files, so an agent that gives up
// does not leave a pool reading a monorepo.
func SearchRepo(ctx context.Context, root string, m *Matcher, allowed FileFilter) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	paths, truncated, err := discover(ctx, root, m, allowed)
	if err != nil {
		return nil, err
	}

	res := &Result{Files: len(paths), Truncated: truncated}
	if len(paths) == 0 {
		return res, nil
	}

	var (
		mu      sync.Mutex
		next    atomic.Int64
		wg      sync.WaitGroup
		hitsCap atomic.Bool
	)
	workers := clampWorkers(min(searchWorkers(), len(paths)))

	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			// Each worker accumulates locally and merges once, so the
			// mutex is contended per worker rather than per file.
			var local []Hit
			for {
				i := int(next.Add(1)) - 1
				if i >= len(paths) || ctx.Err() != nil || hitsCap.Load() {
					break
				}
				fileHits, more := searchFile(root, paths[i], m)
				local = append(local, fileHits...)
				if more {
					// The file had matches this result will not carry, so
					// the answer is partial however few hits came back.
					hitsCap.Store(true)
					break
				}
				// A cheap global check so a query matching everything
				// stops the whole pool rather than every worker filling
				// its own cap.
				if len(local) >= m.MaxHits() {
					hitsCap.Store(true)
					break
				}
			}
			if len(local) == 0 {
				return
			}
			mu.Lock()
			res.Hits = append(res.Hits, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Workers append in whatever order they finish, so determinism comes
	// from this sort rather than from arrival order. Without it two
	// identical searches would disagree and an agent paging through
	// results would see them shuffle.
	// Only the first match on a line is reported, so (file, line) is
	// unique and orders the result completely.
	sort.Slice(res.Hits, func(i, j int) bool {
		a, b := res.Hits[i], res.Hits[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})

	if len(res.Hits) > m.MaxHits() {
		res.Hits = res.Hits[:m.MaxHits()]
		res.Truncated = true
	}
	if hitsCap.Load() {
		res.Truncated = true
	}
	return res, nil
}

// discover collects the searchable files, in walk order.
func discover(ctx context.Context, root string, m *Matcher, allowed FileFilter) ([]string, bool, error) {
	var (
		paths     []string
		truncated bool
	)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			// Unreadable subtree: skip it rather than abandon the walk.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if len(paths) >= maxFilesPerRepo {
			truncated = true
			return fs.SkipAll
		}
		// WalkDir yields paths under root, so the prefix is always there
		// and this is total — unlike filepath.Rel, whose error branch
		// could never be taken and so could never be tested.
		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))

		if !m.IncludeTests() && IsTestFile(rel) {
			return nil
		}
		if m.pathGlob != "" && !matchGlob(m.pathGlob, rel) {
			return nil
		}
		// The file policy is the caller's, and it is what stops a search
		// reading a credential file one line at a time.
		if allowed != nil && !allowed(rel) {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil && info.Size() > maxFileBytes {
			truncated = true
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil && ctx.Err() != nil {
		return nil, false, err
	}
	// Any other walk error is best-effort: a partial file list still
	// produces a useful answer, and an unreadable root shows up as zero
	// files rather than as a failure the agent has to interpret.
	return paths, truncated, nil
}

// matchGlob reports whether rel matches the pattern, against both the full
// relative path and the bare filename.
//
// Both, because "*.go" is what somebody types and it is a filename
// pattern, while "internal/*/*.go" is a path pattern. Trying one and then
// the other is what makes the obvious input work.
func matchGlob(pattern, rel string) bool {
	if ok, err := filepath.Match(pattern, rel); err == nil && ok {
		return true
	}
	base := rel
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	ok, err := filepath.Match(pattern, base)
	return err == nil && ok
}

// searchFile reads one file and returns its matching lines, and whether it
// stopped before the end of the file.
//
// The second return is what makes a truncated answer say so. Without it, a
// file whose matches exactly fill the cap is indistinguishable from one
// that happened to contain exactly that many — and the caller reports a
// partial answer as complete.
//
// Errors are swallowed on purpose: a file that vanished between the walk
// and the read, or that turned out to be unreadable, is not a reason to
// fail a search across thousands of others.
func searchFile(root, rel string, m *Matcher) (hits []Hit, more bool) {
	f, err := os.Open(filepath.Join(root, rel)) // #nosec G304 -- rel is walk-derived and policy-filtered
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()

	// A binary file has no lines worth reporting, and a match inside one
	// is noise at best. Sniffing the first block is what git itself does.
	head := make([]byte, 8000)
	n, _ := io.ReadFull(f, head)
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return nil, false
	}
	// The sniffed bytes are put back in front of the rest rather than
	// seeking to the start again: it saves re-reading the first block, and
	// a Seek on a regular file has an error branch that cannot be taken
	// and therefore cannot be tested.
	sc := bufio.NewScanner(io.MultiReader(bytes.NewReader(head[:n]), f))
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		col := m.MatchLine(text)
		if col < 0 {
			continue
		}
		hits = append(hits, Hit{
			File:   rel,
			Line:   line,
			Column: col + 1,
			Text:   trimHitText(text),
		})
		if len(hits) >= m.MaxHits() {
			// There may or may not be more below; saying so is the safe
			// direction, because claiming a complete answer that is not
			// one is the failure that misleads.
			return hits, true
		}
	}
	// A scanner error — almost always a line longer than the buffer, which
	// means a minified file — leaves the hits found so far, which is the
	// useful half, and marks the answer partial.
	return hits, sc.Err() != nil
}

// trimHitText prepares a matching line for a caller: leading and trailing
// whitespace removed, and bounded.
//
// The indentation is dropped because it carries no information once the
// line number is known, and it is often most of the line.
func trimHitText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxHitTextBytes {
		return s
	}
	// Cut on a rune boundary so the result is still valid UTF-8.
	cut := maxHitTextBytes
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// utf8Start reports whether b begins a UTF-8 sequence rather than
// continuing one.
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
