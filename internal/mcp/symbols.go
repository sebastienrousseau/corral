// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sebastienrousseau/corral/internal/sanitize"
	"github.com/sebastienrousseau/corral/internal/symbols"
)

// Cross-repository symbol lookup.
//
// This is the tool the rest of the index exists to make possible. Every
// competing code-context server answers "where is this symbol" for one
// open repository; corral is the only one that can answer it across every
// clone on the machine, because it is the only one that knows they are all
// there.

// symbolCacheTTL is how long an extracted repository stays fresh.
//
// Longer than the workspace scan's five seconds, because the inputs move at
// different speeds: the set of repositories changes when someone clones,
// while a repository's declarations change when someone edits. Parsing is
// also far more expensive than stat-ing, so a short TTL would spend most of
// an agent's session re-parsing files that had not changed.
const symbolCacheTTL = 2 * time.Minute

// maxCachedRepos bounds the symbol cache. An agent sweeping a large
// workspace would otherwise hold every repository's symbols at once.
const maxCachedRepos = 24

// symbolEntry is one repository's cached extraction.
type symbolEntry struct {
	result  *symbols.Result
	expires time.Time
}

// symbolCache is a small TTL cache keyed by absolute repository path.
//
// Deliberately not an LRU: the eviction policy is "drop everything expired,
// then drop the oldest", which is a few lines and behaves identically for
// the access pattern that actually occurs — an agent working through a
// handful of repositories in one session.
type symbolCache struct {
	mu      sync.Mutex
	entries map[string]symbolEntry
}

func newSymbolCache() *symbolCache {
	return &symbolCache{entries: map[string]symbolEntry{}}
}

// get returns a cached extraction if it is still fresh.
func (c *symbolCache) get(path string) (*symbols.Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[path]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.result, true
}

// put stores an extraction, evicting to stay within maxCachedRepos.
func (c *symbolCache) put(path string, res *symbols.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if len(c.entries) >= maxCachedRepos {
		for k, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, k)
			}
		}
	}
	for len(c.entries) >= maxCachedRepos {
		oldest, at := "", time.Time{}
		for k, e := range c.entries {
			if at.IsZero() || e.expires.Before(at) {
				oldest, at = k, e.expires
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[path] = symbolEntry{result: res, expires: now.Add(symbolCacheTTL)}
}

// symbolsFor extracts one repository's symbols, using the cache.
func (s *Server) symbolsFor(ctx context.Context, repo *RepoEntry) (*symbols.Result, error) {
	if cached, ok := s.symbolCache.get(repo.Path); ok {
		return cached, nil
	}
	res, err := extractSymbols(ctx, repo.Path)
	if err != nil {
		return nil, err
	}
	s.symbolCache.put(repo.Path, res)
	return res, nil
}

// extractSymbols is indirected so tests can stub the expensive parse.
var extractSymbols = symbols.ExtractRepo

// findSymbolInput is the argument set for corral_find_symbol.
type findSymbolInput struct {
	Name         string `json:"name" jsonschema:"Symbol to find. Matched exactly and case-insensitively; a method also matches its 'Receiver.Name' form."`
	Kind         string `json:"kind,omitempty" jsonschema:"Restrict to one kind: func, method, type, interface, const or var."`
	Repo         string `json:"repo,omitempty" jsonschema:"Restrict to one repository, by the same identifier corral_find_repo accepts. Omit to search every clone in the workspace."`
	Substring    bool   `json:"substring,omitempty" jsonschema:"Match any symbol whose name contains the query, instead of matching it exactly."`
	ExportedOnly bool   `json:"exported_only,omitempty" jsonschema:"Return only symbols visible outside their package."`
	IncludeTests bool   `json:"include_tests,omitempty" jsonschema:"Include declarations from test files. Excluded by default because they usually outnumber everything else."`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum symbols to return. Default 50, maximum 200."`
}

// repoOverviewInput is the argument set for corral_repo_overview.
type repoOverviewInput struct {
	Query string `json:"query" jsonschema:"Repository identifier: bare name, relative path, or any unique path suffix."`
}

// registerSymbolTools attaches the symbol tools. They are read-only and
// local, like the rest of the read set.
func (s *Server) registerSymbolTools() {
	langs := strings.Join(symbols.Languages(), ", ")

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "corral_find_symbol",
		Title:       "Find where a symbol is defined",
		Annotations: readOnlyAnnotations(),
		Description: "Find where a function, method, type, interface, constant or variable is defined, across every repository in the Corral workspace — not just one. This is the tool to reach for when you know a name but not which repository it lives in. Returns file and line, not source: read the file at the location it gives you. Indexed languages: " + langs + ". Test declarations are excluded unless include_tests is set. Results are paginated and the response reports whether any repository's index was truncated.",
	}, s.handleFindSymbol)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "corral_repo_overview",
		Title:       "Summarise one repository",
		Annotations: readOnlyAnnotations(),
		Description: "Orient in a single repository in one call: its location and origin, how many source files it has, its declaration counts by kind, and its most significant exported types and functions. Cheaper and far smaller than listing the tree and reading files. Use it before corral_find_symbol when you do not yet know what a repository contains.",
	}, s.handleRepoOverview)
}

// handleFindSymbol resolves a symbol across one repository or all of them.
func (s *Server) handleFindSymbol(ctx context.Context, _ *mcp.CallToolRequest, in findSymbolInput) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" {
		return toolError("name must not be empty"), nil, nil
	}

	query := symbols.Query{
		Name:         in.Name,
		Substring:    in.Substring,
		ExportedOnly: in.ExportedOnly,
		IncludeTests: in.IncludeTests,
	}
	if in.Kind != "" {
		kind, err := symbols.ParseKind(in.Kind)
		if err != nil {
			// A filter that cannot be honoured is an error, never an empty
			// result: the two are indistinguishable to the caller.
			return toolError("%v", err), nil, nil
		}
		query.Kind = kind
	}

	idx, err := s.scan()
	if err != nil {
		return toolError("scan workspace: %v", err), nil, nil
	}

	targets := idx.Repos
	if in.Repo != "" {
		match, err := idx.Find(in.Repo)
		if err != nil {
			return toolError("%v", err), nil, nil
		}
		targets = []RepoEntry{*match}
	}

	type hit struct {
		Repo     string       `json:"repo"`
		Symbol   string       `json:"symbol"`
		Kind     symbols.Kind `json:"kind"`
		File     string       `json:"file"`
		Line     int          `json:"line"`
		Exported bool         `json:"exported"`
		Language string       `json:"language"`
		Test     bool         `json:"test,omitempty"`
	}

	var (
		hits      []hit
		truncated []string
		scanned   int
	)
	for i := range targets {
		if err := ctx.Err(); err != nil {
			return toolError("cancelled after %d repositories", scanned), nil, nil
		}
		repo := &targets[i]
		res, err := s.symbolsFor(ctx, repo)
		if err != nil {
			// One unreadable repository must not fail the whole lookup.
			continue
		}
		scanned++
		if res.Truncated {
			truncated = append(truncated, repo.Redacted().RelPath)
		}
		for _, sym := range res.Symbols {
			if !query.Match(sym) {
				continue
			}
			red := repo.Redacted()
			hits = append(hits, hit{
				Repo:     red.RelPath,
				Symbol:   sanitize.Untrusted(sym.Qualified(), maxEntryName),
				Kind:     sym.Kind,
				File:     sanitize.Untrusted(sym.File, maxEntryPath),
				Line:     sym.Line,
				Exported: sym.Exported,
				Language: sym.Language,
				Test:     sym.Test,
			})
		}
	}

	// Exported before unexported, then by repository and location, so the
	// first page holds the answers most likely to be wanted.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Exported != hits[j].Exported {
			return hits[i].Exported
		}
		if hits[i].Repo != hits[j].Repo {
			return hits[i].Repo < hits[j].Repo
		}
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		return hits[i].Line < hits[j].Line
	})

	limit := in.Limit
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	total := len(hits)
	if len(hits) > limit {
		hits = hits[:limit]
	}

	body := map[string]any{
		"query":               in.Name,
		"repositories_search": scanned,
		"total_matched":       total,
		"returned":            len(hits),
		"symbols":             hits,
	}
	if total == 0 {
		body["note"] = "No match. Symbols come from " + strings.Join(symbols.Languages(), ", ") +
			" sources only; try substring:true, or include_tests:true if it is declared in a test."
	}
	if total > len(hits) {
		body["note"] = "More matches than the limit; narrow with kind, repo or exported_only."
	}
	if len(truncated) > 0 {
		// A silently partial index is worse than a slow one: the caller
		// cannot tell a missing symbol from an absent one.
		body["indexes_truncated"] = truncated
		body["indexes_truncated_note"] = "These repositories exceeded the per-repository file or symbol cap; their results are incomplete."
	}
	return jsonResult(body), nil, nil
}

// handleRepoOverview summarises one repository in a single call.
func (s *Server) handleRepoOverview(ctx context.Context, _ *mcp.CallToolRequest, in repoOverviewInput) (*mcp.CallToolResult, any, error) {
	idx, err := s.scan()
	if err != nil {
		return toolError("scan workspace: %v", err), nil, nil
	}
	match, err := idx.Find(in.Query)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	res, err := s.symbolsFor(ctx, match)
	if err != nil {
		return toolError("index %s: %v", match.Redacted().RelPath, err), nil, nil
	}

	byKind := map[symbols.Kind]int{}
	var exportedTypes, exportedFuncs []string
	for _, sym := range res.Symbols {
		if sym.Test {
			continue
		}
		byKind[sym.Kind]++
		if !sym.Exported {
			continue
		}
		name := sanitize.Untrusted(sym.Qualified(), maxEntryName)
		switch sym.Kind {
		case symbols.KindType, symbols.KindInterface:
			exportedTypes = append(exportedTypes, name)
		case symbols.KindFunc:
			exportedFuncs = append(exportedFuncs, name)
		}
	}
	sort.Strings(exportedTypes)
	sort.Strings(exportedFuncs)

	const maxListed = 25
	red := match.Redacted()
	body := map[string]any{
		"repo":       red.RelPath,
		"path":       red.Path,
		"remote_url": red.RemoteURL,
		"language":   red.Language,
		"visibility": red.Visibility,
		"files":      res.Files,
		"declarations": map[string]int{
			"func": byKind[symbols.KindFunc], "method": byKind[symbols.KindMethod],
			"type": byKind[symbols.KindType], "interface": byKind[symbols.KindInterface],
			"const": byKind[symbols.KindConst], "var": byKind[symbols.KindVar],
		},
		"exported_types":     capList(exportedTypes, maxListed),
		"exported_functions": capList(exportedFuncs, maxListed),
	}
	if res.Truncated {
		body["truncated"] = true
		body["truncated_note"] = "This repository exceeded the per-repository file or symbol cap; the counts are a lower bound."
	}
	if len(exportedTypes) > maxListed || len(exportedFuncs) > maxListed {
		body["note"] = fmt.Sprintf("Lists are capped at %d; use corral_find_symbol for the rest.", maxListed)
	}
	return jsonResult(body), nil, nil
}

// capList truncates a list to at most n entries.
func capList(xs []string, n int) []string {
	if len(xs) > n {
		return xs[:n]
	}
	return xs
}
