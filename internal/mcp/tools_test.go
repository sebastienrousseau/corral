package mcp

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// callTool is a small helper that invokes a tool handler with a
// hand-rolled mcp.CallToolRequest. The mcp-go API doesn't ship a
// public test helper for this, so we wire the arguments directly.
func callTool(t *testing.T, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned go error: %v", err)
	}
	if res == nil {
		t.Fatal("handler returned nil result")
	}
	return res
}

// textOf extracts the text content from the first TextContent block
// in a CallToolResult, which is where jsonResult/NewToolResultText
// stash their payload.
func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatalf("no TextContent in result: %+v", res)
	return ""
}

func newTestServer(t *testing.T, base string) *Server {
	t.Helper()
	srv, err := NewServer(ServerOptions{Root: base, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestListReposToolFiltersByLanguage(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	makeFakeRepo(t, base, "Public", "rust", "beta", "", "")
	makeFakeRepo(t, base, "Private", "go", "gamma", "", "")

	srv := newTestServer(t, base)
	_, handler := srv.listReposTool()

	res := callTool(t, handler, map[string]any{"language": "go"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", textOf(t, res))
	}
	var payload struct {
		Count int         `json:"count"`
		Repos []RepoEntry `json:"repos"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 2 {
		t.Errorf("expected 2 Go repos, got %d (%+v)", payload.Count, payload.Repos)
	}
}

func TestListReposToolFiltersByVisibility(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	makeFakeRepo(t, base, "Private", "go", "beta", "", "")

	srv := newTestServer(t, base)
	_, handler := srv.listReposTool()
	res := callTool(t, handler, map[string]any{"visibility": "private"})
	var payload struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Count != 1 {
		t.Errorf("expected 1 private repo, got %d", payload.Count)
	}
}

func TestListReposToolSyncedOnly(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "synced", "", `{"last_synced_at":"2026-06-30T00:00:00Z"}`)
	makeFakeRepo(t, base, "Public", "go", "unsynced", "", "")

	srv := newTestServer(t, base)
	_, handler := srv.listReposTool()
	res := callTool(t, handler, map[string]any{"synced_only": true})
	var payload struct {
		Repos []RepoEntry `json:"repos"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Repos) != 1 || payload.Repos[0].Name != "synced" {
		t.Errorf("expected only 'synced' repo, got %+v", payload.Repos)
	}
}

func TestFindRepoToolErrorOnMissingArg(t *testing.T) {
	base := t.TempDir()
	srv := newTestServer(t, base)
	_, handler := srv.findRepoTool()
	res := callTool(t, handler, map[string]any{})
	if !res.IsError {
		t.Error("expected error result when query missing")
	}
}

func TestFindRepoToolReturnsMatch(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "needle", "", "")

	srv := newTestServer(t, base)
	_, handler := srv.findRepoTool()
	res := callTool(t, handler, map[string]any{"query": "needle"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", textOf(t, res))
	}
	if !strings.Contains(textOf(t, res), `"name": "needle"`) {
		t.Errorf("expected name in output, got: %s", textOf(t, res))
	}
}

func TestFindRepoToolReportsAmbiguity(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "x", "", "")
	makeFakeRepo(t, base, "Private", "go", "x", "", "")

	srv := newTestServer(t, base)
	_, handler := srv.findRepoTool()
	res := callTool(t, handler, map[string]any{"query": "x"})
	if !res.IsError {
		t.Error("expected error on ambiguity")
	}
	if !strings.Contains(textOf(t, res), "multiple repositories") {
		t.Errorf("expected ambiguity message, got: %s", textOf(t, res))
	}
}

// TestCurrentBranchLogsOnFailure guards the diagnostic upgrade in v0.0.10:
// git rev-parse failures used to be swallowed silently, which made
// detached-HEAD, corrupted-refs, and permission-denied cases invisible
// to operators debugging client issues. The tool contract is unchanged
// (returns "" on error), but the failure is now surfaced via the log
// package (routed to stderr in production so stdout stays clean).
func TestCurrentBranchLogsOnFailure(t *testing.T) {
	var buf strings.Builder
	oldOut := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(oldOut)

	// Point at a definitely-not-a-git-repo path so rev-parse exits nonzero.
	got := currentBranch(context.Background(), "/dev/null")
	if got != "" {
		t.Errorf("expected empty branch on failure, got %q", got)
	}
	if !strings.Contains(buf.String(), "git rev-parse") {
		t.Errorf("expected log line to mention 'git rev-parse', got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "/dev/null") {
		t.Errorf("expected log line to include repoPath, got: %q", buf.String())
	}
}

func TestRepoMetadataToolStubsBranch(t *testing.T) {
	old := currentBranch
	defer func() { currentBranch = old }()
	currentBranch = func(ctx context.Context, path string) string { return "main" }

	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "https://github.com/o/alpha.git", `{"last_synced_at":"2026-06-30T00:00:00Z"}`)

	srv := newTestServer(t, base)
	_, handler := srv.repoMetadataTool()
	res := callTool(t, handler, map[string]any{"query": "alpha"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", textOf(t, res))
	}
	for _, want := range []string{`"current_branch": "main"`, `"name": "alpha"`, "github.com/o/alpha.git"} {
		if !strings.Contains(textOf(t, res), want) {
			t.Errorf("missing %q in output: %s", want, textOf(t, res))
		}
	}
}

func TestStatusSummaryTool(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "a", "", `{"last_synced_at":"2026-06-30T00:00:00Z"}`)
	makeFakeRepo(t, base, "Public", "go", "b", "", "")
	makeFakeRepo(t, base, "Public", "rust", "c", "", "")
	makeFakeRepo(t, base, "Private", "go", "d", "", "")

	srv := newTestServer(t, base)
	_, handler := srv.statusSummaryTool()
	res := callTool(t, handler, map[string]any{})
	if res.IsError {
		t.Fatalf("unexpected error: %s", textOf(t, res))
	}
	out := textOf(t, res)
	for _, want := range []string{`"total": 4`, `"synced": 1`, `"Public": 3`, `"Private": 1`, `"language": "go"`, `"count": 3`} {
		if !strings.Contains(out, want) {
			t.Errorf("status_summary missing %q: %s", want, out)
		}
	}
}

func TestWorkspaceIndexTool(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "only", "", "")

	srv := newTestServer(t, base)
	_, handler := srv.workspaceIndexTool()
	res := callTool(t, handler, map[string]any{})
	var idx Index
	if err := json.Unmarshal([]byte(textOf(t, res)), &idx); err != nil {
		t.Fatal(err)
	}
	if len(idx.Repos) != 1 || idx.Repos[0].Name != "only" {
		t.Errorf("expected one repo, got %+v", idx.Repos)
	}
}

func TestSortedLangCountsOrdering(t *testing.T) {
	m := map[string]int{"go": 3, "rust": 3, "python": 2}
	out := sortedLangCounts(m)
	// Sorted by count desc, then alpha. go and rust both have 3 → go first.
	if out[0]["language"] != "go" || out[1]["language"] != "rust" || out[2]["language"] != "python" {
		t.Errorf("unexpected ordering: %+v", out)
	}
}
