// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerTools attaches the v0 read-only tool set to the underlying
// MCP server. Tool names follow the snake_case `corral_<verb>_<noun>`
// convention recommended by the 2025-11-25 spec and shipped by
// github/github-mcp-server, so an agent that has loaded both servers
// can rank tools by prefix without confusion.
func (s *Server) registerTools() {
	s.mcp.AddTool(s.listReposTool())
	s.mcp.AddTool(s.findRepoTool())
	s.mcp.AddTool(s.repoMetadataTool())
	s.mcp.AddTool(s.statusSummaryTool())
	s.mcp.AddTool(s.workspaceIndexTool())
}

// listReposTool returns the tool definition + handler for
// corral_list_repos. The tool answers the most common opening question
// an agent asks ("what's in this workspace?") without requiring a full
// index dump up front. All filters are optional and intersected; the
// result is the structured RepoEntry list serialised as JSON.
func (s *Server) listReposTool() (mcp.Tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("corral_list_repos",
		mcp.WithDescription("List local clones in the Corral-organised workspace, optionally filtered by visibility (Public/Private), language, repository-name substring, or whether a .corral-state.json sidecar is present. Returns the structured repository index as JSON."),
		mcp.WithString("visibility",
			mcp.Description("Filter by visibility directory: 'Public' or 'Private'. Case-insensitive."),
		),
		mcp.WithString("language",
			mcp.Description("Filter by language directory (e.g. 'go', 'rust'). Case-insensitive."),
		),
		mcp.WithString("name_contains",
			mcp.Description("Substring match against the repository name. Case-insensitive."),
		),
		mcp.WithBoolean("synced_only",
			mcp.Description("When true, return only repositories with a populated .corral-state.json sidecar (i.e. previously synced by corral). Default false."),
		),
	)

	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		idx, err := s.scan()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("scan workspace: %v", err)), nil
		}
		// Optional filters; missing arguments are zero values that
		// match-anything by design (no separate "filter is present"
		// flag needed).
		visibility := strings.ToLower(req.GetString("visibility", ""))
		language := strings.ToLower(req.GetString("language", ""))
		nameSubstr := strings.ToLower(req.GetString("name_contains", ""))
		syncedOnly := req.GetBool("synced_only", false)

		var out []RepoEntry
		for _, r := range idx.Repos {
			if visibility != "" && strings.ToLower(r.Visibility) != visibility {
				continue
			}
			if language != "" && strings.ToLower(r.Language) != language {
				continue
			}
			if nameSubstr != "" && !strings.Contains(strings.ToLower(r.Name), nameSubstr) {
				continue
			}
			if syncedOnly && (r.State == nil || r.State.LastSyncedAt == "") {
				continue
			}
			out = append(out, r)
		}
		return jsonResult(map[string]any{
			"root":  idx.Root,
			"count": len(out),
			"repos": out,
		}), nil
	}
	return tool, handler
}

// findRepoTool returns corral_find_repo: a single-string lookup that
// resolves a fuzzy name to a unique RepoEntry. Returns a structured
// error result (IsError=true) on no-match or ambiguous match, listing
// the candidate paths so the agent can re-call with a more specific
// query. This is the workhorse for "open repo X" style intents.
func (s *Server) findRepoTool() (mcp.Tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("corral_find_repo",
		mcp.WithDescription("Resolve a fuzzy repository name (bare name, relative path, or path suffix) to a single local clone in the Corral workspace. Returns the matched RepoEntry, or an error result listing all candidate paths when the query is ambiguous."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Repository identifier: bare name ('corral'), relative path ('Public/go/corral'), or any path suffix that uniquely identifies a repo."),
		),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		idx, err := s.scan()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("scan workspace: %v", err)), nil
		}
		match, err := idx.Find(query)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return jsonResult(match), nil
	}
	return tool, handler
}

// repoMetadataTool returns corral_get_repo_metadata: deep info about
// one repo, including current branch (resolved via git rev-parse),
// remote origin URL, and parsed sidecar state. Separate from
// corral_find_repo because the metadata fetch involves a subprocess
// call per request (CurrentBranch) and isn't free; list_repos and
// find_repo keep their per-call cost predictable.
func (s *Server) repoMetadataTool() (mcp.Tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("corral_get_repo_metadata",
		mcp.WithDescription("Return full metadata for a single local clone: repo entry, current branch, and parsed .corral-state.json. The branch lookup spawns one git subprocess per call; prefer corral_list_repos for bulk queries."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Repository identifier: bare name, relative path, or any path suffix that uniquely identifies a repo."),
		),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		idx, err := s.scan()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("scan workspace: %v", err)), nil
		}
		match, err := idx.Find(query)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		branch := currentBranch(ctx, match.Path)
		return jsonResult(map[string]any{
			"repo":           match,
			"current_branch": branch,
		}), nil
	}
	return tool, handler
}

// statusSummaryTool returns corral_status_summary: a workspace-wide
// summary intended as the agent's opening read on a large workspace —
// "how many repos, broken down how, and how many are stale?". Cheap to
// compute because it only touches the in-memory index, no subprocesses.
func (s *Server) statusSummaryTool() (mcp.Tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("corral_status_summary",
		mcp.WithDescription("High-level workspace summary: total repository count and breakdowns by visibility and language. Cheap to compute; suitable as an agent's opening discovery call."),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		idx, err := s.scan()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("scan workspace: %v", err)), nil
		}
		byVis := map[string]int{}
		byLang := map[string]int{}
		synced := 0
		for _, r := range idx.Repos {
			if r.Visibility != "" {
				byVis[r.Visibility]++
			}
			if r.Language != "" {
				byLang[r.Language]++
			}
			if r.State != nil && r.State.LastSyncedAt != "" {
				synced++
			}
		}
		return jsonResult(map[string]any{
			"root":          idx.Root,
			"total":         len(idx.Repos),
			"synced":        synced,
			"by_visibility": byVis,
			"by_language":   sortedLangCounts(byLang),
		}), nil
	}
	return tool, handler
}

// workspaceIndexTool returns corral_workspace_index: the raw index dump
// for clients that prefer one round-trip to many. Useful for an agent
// priming its context at session start, then querying the in-memory
// copy without further tool calls. Capped result size is the agent's
// problem (rounds-trip cost matters more than bytes-on-the-wire here).
func (s *Server) workspaceIndexTool() (mcp.Tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	tool := mcp.NewTool("corral_workspace_index",
		mcp.WithDescription("Return the full structured index of the Corral workspace in one call. Use this when an agent wants to prime its context with the complete repository list rather than make many filtered corral_list_repos calls. Mirrors the corral://workspace/index resource."),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		idx, err := s.scan()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("scan workspace: %v", err)), nil
		}
		return jsonResult(idx), nil
	}
	return tool, handler
}

// sortedLangCounts converts a language-count map into a stable
// descending-by-count, then alphabetical-by-name list so the JSON
// output is deterministic across calls. Agents that diff successive
// snapshots benefit from the stability.
func sortedLangCounts(m map[string]int) []map[string]any {
	out := make([]map[string]any, 0, len(m))
	for k, v := range m {
		out = append(out, map[string]any{"language": k, "count": v})
	}
	sort.Slice(out, func(i, j int) bool {
		ci, cj := out[i]["count"].(int), out[j]["count"].(int)
		if ci != cj {
			return ci > cj
		}
		return out[i]["language"].(string) < out[j]["language"].(string)
	})
	return out
}

// currentBranch shells out to git rev-parse to resolve HEAD's branch.
// Indirected through a package var so tests can stub without spawning
// a real subprocess. On error the caller gets an empty string (the
// tool result still succeeds with "current_branch": "") but the error
// is logged to stderr so operators can debug detached-HEAD,
// permission, and corrupt-git-tree cases that used to be silent.
// stderr is the only safe channel — stdout carries the JSON-RPC
// protocol stream and must not be polluted.
var currentBranch = func(ctx context.Context, repoPath string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		// Include stderr from the failed process so operators can tell
		// "not a git repo" apart from "detached HEAD" apart from
		// "permission denied" apart from "corrupted refs".
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr != "" {
			log.Printf("corral-mcp: git rev-parse in %s failed: %v (%s)", repoPath, err, stderr)
		} else {
			log.Printf("corral-mcp: git rev-parse in %s failed: %v", repoPath, err)
		}
		return ""
	}
	return strings.TrimSpace(string(out))
}
