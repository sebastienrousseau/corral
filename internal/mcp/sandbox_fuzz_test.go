// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzSafePathNeverEscapesRoot is the fuzz target for the highest-risk
// function in the package.
//
// Index.SafePath is the boundary that keeps an MCP client — which is to say,
// an agent acting on text it was given — inside the configured workspace. A
// defect here is not a wrong answer, it is arbitrary file read outside the
// sandbox. The unit tests cover the traversal shapes we thought of; this
// covers the ones we did not.
//
// The invariant is absolute and easy to state: whatever SafePath returns
// must be inside the root. It may reject anything it likes, and rejection is
// always safe — only a wrong acceptance is a vulnerability.
func FuzzSafePathNeverEscapesRoot(f *testing.F) {
	seeds := []string{
		"",
		".",
		"..",
		"../",
		"../../etc/passwd",
		"..\\..\\windows\\system32",
		"a/../../b",
		"./a/./../../..",
		"..foo",              // a legitimate repository name that starts with dots
		"..foo/../../escape", // the same name used as a traversal springboard
		"/etc/passwd",
		"//etc/passwd",
		"C:\\Windows",
		"repo/%2e%2e/%2e%2e",
		"repo/\x00/etc",
		strings.Repeat("../", 64) + "etc/passwd",
		strings.Repeat("a/", 512),
		"\u202e/etc/passwd", // right-to-left override
		"a/\u0000/b",
		"日本語/../../escape",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, candidate string) {
		// A pathological length is a denial-of-service question, not a
		// containment one, and the OS rejects it long before we do.
		if len(candidate) > 4096 {
			t.Skip()
		}

		root := t.TempDir()
		// A real repository inside the root, so the accepted paths under
		// test are not all rejections of an empty directory.
		if err := os.MkdirAll(filepath.Join(root, "Public", "go", "alpha", ".git"), 0o750); err != nil {
			t.Fatal(err)
		}

		idx := &Index{Root: root}
		for _, input := range []string{candidate, filepath.Join(root, candidate)} {
			safe, err := idx.SafePath(input)
			if err != nil {
				// Refusing is always a correct answer.
				continue
			}
			assertInsideRoot(t, root, input, safe)

			// SafeMutationPath is strictly narrower: everything it accepts,
			// SafePath accepts, and it additionally never resolves to the
			// root itself.
			mutable, mutErr := idx.SafeMutationPath(input)
			if mutErr != nil {
				continue
			}
			assertInsideRoot(t, root, input, mutable)
			if sameResolvedPath(t, root, mutable) {
				t.Fatalf("SafeMutationPath(%q) resolved to the workspace root itself", input)
			}
		}
	})
}

// assertInsideRoot fails the test unless resolved is the root or lives
// beneath it, comparing whole path segments rather than string prefixes so a
// sibling directory named like the root cannot pass.
func assertInsideRoot(t *testing.T, root, input, resolved string) {
	t.Helper()
	if !filepath.IsAbs(resolved) {
		t.Fatalf("SafePath(%q) returned a relative path %q", input, resolved)
	}
	rel, err := filepath.Rel(canonical(t, root), canonical(t, resolved))
	if err != nil {
		t.Fatalf("SafePath(%q) returned %q, which cannot be relativised against the root: %v", input, resolved, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("SafePath(%q) escaped the sandbox: %q resolves outside %q (rel %q)", input, resolved, root, rel)
	}
}

// sameResolvedPath reports whether two paths name the same location once
// symlinks are resolved.
func sameResolvedPath(t *testing.T, a, b string) bool {
	t.Helper()
	return canonical(t, a) == canonical(t, b)
}

// canonical resolves symlinks where it can, falling back to the cleaned path
// for locations that do not exist.
func canonical(t *testing.T, path string) string {
	t.Helper()
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}
