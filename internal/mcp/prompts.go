// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerPrompts attaches Corral's MCP prompt templates to the
// underlying server. Prompts are structured invocations an MCP client
// (Claude Code, Cursor, Cline) surfaces to the user as pre-canned
// options — the user picks one from a menu, the client fills it into
// the conversation, and the agent uses the resulting instructions.
//
// The prompts here don't call tools directly. They tell the agent
// which tools/resources to consult to answer the user's intent, which
// makes them useful even before Corral ships write tools: an agent can
// still explain the workspace or find stale clones using only the
// read-only surface.
//
// Prompt-capability advertising is enabled unconditionally at
// NewServer time; the prompts themselves are free to register whether
// or not mutations are enabled.
func (s *Server) registerPrompts() {
	s.mcp.AddPrompt(&mcp.Prompt{
		Name:        "explain_workspace",
		Title:       "Explain this workspace",
		Description: "Ask the agent to survey the local Corral-organised workspace and produce a human-readable summary: total repository count, breakdown by visibility and language, freshly-synced vs long-stale clones. Uses only read-only tools.",
	}, s.handleExplainWorkspace)
	s.mcp.AddPrompt(&mcp.Prompt{
		Name:        "identify_stale_repos",
		Title:       "Find stale clones",
		Description: "Ask the agent to find clones that have not been synced recently, using the workspace index's sync state. Uses only read-only tools.",
	}, s.handleIdentifyStaleRepos)
}

// explainWorkspacePrompt returns explain_workspace. Instructs the
// agent to survey the workspace via the corral_status_summary and
// corral_workspace_index tools and summarise the layout for the user
// — how many repos, what languages, which orgs, which are freshly
// synced vs stale.
func (s *Server) handleExplainWorkspace(ctx context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: "Survey the Corral workspace",
		Messages: []*mcp.PromptMessage{
			{
				Role: "user",
				Content: &mcp.TextContent{Text: "Please survey my Corral-organised local workspace and explain what's there.\n\n" +
					"Concretely:\n" +
					"1. Call `corral_status_summary` to get high-level counts by visibility and language.\n" +
					"2. Call `corral_workspace_index` if you need repository-level detail (path, remote URL, last sync).\n" +
					"3. Summarise for me:\n" +
					"   - Total repository count.\n" +
					"   - Breakdown by visibility (Public/Private) and top languages.\n" +
					"   - Any repos whose sync state (`last_synced_at`) looks stale.\n" +
					"   - Any repos with no origin URL — those might be local-only work.\n\n" +
					"Keep the summary short and structured; skip repos that are unremarkable.",
				},
			},
		},
	}, nil
}

// identifyStaleReposPrompt returns identify_stale_repos. Directs the
// agent to scan .corral-state.json state via the workspace_index tool
// and flag clones whose upstream has moved but whose local state has
// not. Intended as the "which of my forks/mirrors need attention"
// question every developer periodically wants answered.
func (s *Server) handleIdentifyStaleRepos(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	threshold := "30"
	if req != nil && req.Params != nil && req.Params.Arguments != nil {
		if t := req.Params.Arguments["threshold_days"]; t != "" {
			threshold = t
		}
	}
	return &mcp.GetPromptResult{
		Description: "Find stale clones in the Corral workspace",
		Messages: []*mcp.PromptMessage{
			{
				Role: "user",
				Content: &mcp.TextContent{Text: fmt.Sprintf(
					"Please identify Corral-managed clones that have gone stale (haven't been synced in more than %s days).\n\n"+
						"Concretely:\n"+
						"1. Call `corral_list_repos` with `synced_only=true` to get only clones with a state sidecar.\n"+
						"2. For each entry, look at `state.last_synced_at`. If that timestamp is older than %s days from today, it counts as stale.\n"+
						"3. Present a short table: repository, language, days since last sync. Sort oldest-first.\n"+
						"4. If nothing is stale, say so and stop — don't pad the answer.\n\n"+
						"Do NOT try to sync anything yet — this prompt is diagnostic only.",
					threshold, threshold,
				)},
			},
		},
	}, nil
}
