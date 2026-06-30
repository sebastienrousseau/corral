// Package mcp implements the corral Model Context Protocol server: a
// stdio-based JSON-RPC server that exposes the local Corral-organised
// workspace (cloned repositories under ~/Code) to AI coding agents via
// the read-only tools and resources defined in this package.
//
// The server is a wedge for the "local index for AI" positioning
// described in the v0.0.8 design doc: GitHub's own MCP server already
// covers the remote API surface with 50+ tools, so corral-mcp focuses
// on the dimension only it can serve — a developer's already-cloned
// local mirror, organised by visibility and language, queryable
// without a network round-trip.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sebastienrousseau/corral/internal/git"
)

// RepoEntry is one row in the workspace index. It captures the information
// agents most often want about a local clone without needing to spawn a
// `git` subprocess per repo.
type RepoEntry struct {
	// Name is the repository's basename (e.g. "corral").
	Name string `json:"name"`
	// Visibility is the visibility-directory the clone sits under
	// (typically "Public" or "Private"); empty when the layout does not
	// include a visibility segment.
	Visibility string `json:"visibility,omitempty"`
	// Language is the language-directory segment (lowercase, normalised
	// by corral on clone). Empty when not present in the layout.
	Language string `json:"language,omitempty"`
	// Path is the absolute on-disk path to the repository root.
	Path string `json:"path"`
	// RelPath is the path relative to the index root, joinable across
	// hosts (forward-slash separators).
	RelPath string `json:"rel_path"`
	// RemoteURL is the URL of the `origin` remote parsed from
	// .git/config; empty when unreadable.
	RemoteURL string `json:"remote_url,omitempty"`
	// State is the parsed contents of .corral-state.json when present.
	// nil when the sidecar is absent or unreadable.
	State *StateRecord `json:"state,omitempty"`
}

// StateRecord mirrors the on-disk .corral-state.json sidecar without
// importing internal/engine (which would create a dependency cycle —
// internal/engine already imports internal/git, and corral-mcp will need
// to import internal/engine in later phases for sync operations).
type StateRecord struct {
	// LastSyncedPushedAt is the upstream pushed_at timestamp the engine
	// observed on the previous successful sync, formatted per RFC 3339.
	LastSyncedPushedAt string `json:"last_synced_pushed_at,omitempty"`
	// LastSyncedAt is when the engine last touched this clone, RFC 3339.
	LastSyncedAt string `json:"last_synced_at,omitempty"`
}

// Index is an in-memory snapshot of the workspace beneath a root
// directory. It is intentionally cheap to rebuild — every tool call
// triggers a fresh Scan — so the server stays correct as the user
// clones, syncs, and removes repos out-of-band without us having to
// implement filesystem watching.
type Index struct {
	// Root is the absolute path the index was built against.
	Root string
	// Repos is the discovered set of clones, sorted deterministically
	// by RelPath for stable agent output.
	Repos []RepoEntry
}

// stateFileName mirrors engine.StateFileName without importing the
// engine package; kept in sync by hand because the value is part of
// corral's public on-disk contract.
const stateFileName = ".corral-state.json"

// maxIndexDepth bounds the walk so a misconfigured root (e.g. $HOME)
// cannot blow up scan time or memory on a deeply nested filesystem.
// Three levels covers the documented Visibility/Language/Repo layout
// plus a generous one-level slack for custom Layouts.
const maxIndexDepth = 4

// Scan walks root looking for directories that contain a .git child
// and returns an Index. The walk stops descending into a directory
// once it has been identified as a repository root (no recursing into
// nested submodules or vendor trees) and respects maxIndexDepth.
//
// Per-entry errors are tolerated: a single unreadable directory does
// not abort the whole scan. The function only returns an error when
// the root itself is unreadable, so a caller can distinguish a
// misconfigured root from an empty workspace.
func Scan(root string) (*Index, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root %q is not a directory", absRoot)
	}

	idx := &Index{Root: absRoot}

	// Per-entry errors inside the walk are swallowed (logged or ignored)
	// because the walk is best-effort discovery, not a transactional
	// scan: surfacing every unreadable directory would force the agent
	// into noise.
	_ = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Unreadable: skip its subtree without aborting the scan.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return fs.SkipDir
		}
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(filepath.Separator)) + 1
		}
		if depth > maxIndexDepth {
			return fs.SkipDir
		}
		// A directory containing .git is a repository root. Capture it
		// and don't descend further into the working tree.
		gitDir := filepath.Join(path, ".git")
		if st, err := os.Stat(gitDir); err == nil && st.IsDir() {
			idx.Repos = append(idx.Repos, buildEntry(absRoot, path))
			return fs.SkipDir
		}
		return nil
	})

	sort.Slice(idx.Repos, func(i, j int) bool {
		return idx.Repos[i].RelPath < idx.Repos[j].RelPath
	})
	return idx, nil
}

// buildEntry constructs a RepoEntry from a discovered repository path,
// extracting Visibility/Language from the leading path segments under
// the index root and best-effort enriching with remote URL + sidecar
// state.
func buildEntry(root, repoPath string) RepoEntry {
	rel, _ := filepath.Rel(root, repoPath)
	rel = filepath.ToSlash(rel)

	entry := RepoEntry{
		Name:    filepath.Base(repoPath),
		Path:    repoPath,
		RelPath: rel,
	}

	// Map leading segments to Visibility / Language. Layouts that don't
	// follow the default Visibility/Language/Repo schema simply leave
	// the corresponding fields empty.
	parts := strings.Split(rel, "/")
	if len(parts) >= 3 {
		entry.Visibility = parts[0]
		entry.Language = parts[1]
	} else if len(parts) == 2 {
		entry.Language = parts[0]
	}

	if url, err := git.RemoteOriginFromConfig(repoPath); err == nil {
		entry.RemoteURL = url
	}
	if state, ok := readState(repoPath); ok {
		entry.State = state
	}
	return entry
}

// readState parses the .corral-state.json sidecar. A missing file is
// not an error — it is the expected state for any clone made before
// the smart-sync feature shipped or for clones managed outside corral.
func readState(repoPath string) (*StateRecord, bool) {
	b, err := os.ReadFile(filepath.Join(repoPath, stateFileName))
	if err != nil {
		return nil, false
	}
	var s StateRecord
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, false
	}
	return &s, true
}

// Find returns the entry whose Name, RelPath, or RemoteURL repo segment
// equals or has the supplied query as a suffix. It is the primitive
// behind the corral_find_repo tool. Returns ErrRepoNotFound when no
// candidate matches and ErrAmbiguous when multiple do — the caller
// should surface both for the agent to disambiguate.
func (i *Index) Find(query string) (*RepoEntry, error) {
	if query == "" {
		return nil, ErrRepoNotFound
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var matches []*RepoEntry
	for idx := range i.Repos {
		r := &i.Repos[idx]
		if strings.EqualFold(r.Name, q) ||
			strings.EqualFold(r.RelPath, q) ||
			strings.HasSuffix(strings.ToLower(r.RelPath), "/"+q) {
			matches = append(matches, r)
		}
	}
	switch len(matches) {
	case 0:
		return nil, ErrRepoNotFound
	case 1:
		return matches[0], nil
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.RelPath)
		}
		return nil, fmt.Errorf("%w: %s", ErrAmbiguous, strings.Join(names, ", "))
	}
}

// ErrRepoNotFound is returned by Index.Find when no entry matches.
var ErrRepoNotFound = errors.New("no repository matches the query")

// ErrAmbiguous is returned by Index.Find when more than one entry
// matches and the caller must disambiguate.
var ErrAmbiguous = errors.New("multiple repositories match the query")

// SafePath validates that path resolves to a file or directory beneath
// the index root, blocking directory-traversal attempts via the
// corral_get_file tool and the corral://repo/{org}/{name}/file/{path}
// resource. Returns the cleaned absolute path on success.
//
// Both the root and the candidate's existing ancestors are
// canonicalised via EvalSymlinks. This matters on macOS where /tmp is
// a symlink to /private/tmp: without canonicalising both sides of the
// rel-prefix check, every lookup spuriously "escapes" the root. When
// the candidate itself doesn't exist, the canonicalisation walks up
// to the deepest existing ancestor and reconstructs the path, so
// would-be lookups (e.g. for a file the caller is about to create)
// still get the same security checks as existing-file lookups.
func (i *Index) SafePath(path string) (string, error) {
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(i.Root, path)
	}
	rawAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolving path: %w", err)
	}

	rootCanon := i.Root
	if r, err := filepath.EvalSymlinks(i.Root); err == nil {
		rootCanon = r
	}

	absCanon := canonicalizeExistingPrefix(rawAbs)

	rel, err := filepath.Rel(rootCanon, absCanon)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return "", fmt.Errorf("path %q escapes root %q", path, i.Root)
	}
	return absCanon, nil
}

// canonicalizeExistingPrefix returns abs with its longest existing
// prefix canonicalised via EvalSymlinks and the non-existing tail
// re-appended. If abs itself exists, EvalSymlinks handles it directly;
// otherwise we walk up looking for an existing ancestor whose
// canonical form we can use. Falls back to the raw path when no
// ancestor resolves (shouldn't happen on a normal POSIX root).
func canonicalizeExistingPrefix(abs string) string {
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	dir := abs
	var suffixParts []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			out := resolved
			// Re-append the un-resolved children in original order.
			suffixParts = append([]string{filepath.Base(dir)}, suffixParts...)
			for _, p := range suffixParts {
				out = filepath.Join(out, p)
			}
			return out
		}
		suffixParts = append([]string{filepath.Base(dir)}, suffixParts...)
		dir = parent
	}
}
