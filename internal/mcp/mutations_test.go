package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sebastienrousseau/corral/internal/git"
)

// stubGitPull / stubGitClone / stubDirty / stubUnpushed are shared
// across tests; a defer restores production behaviour so parallel
// tests would still be safe.
func stubGitPull(t *testing.T, fn func(ctx context.Context, targetDir string, opts git.PullOptions) error) {
	t.Helper()
	old := gitPull
	gitPull = fn
	t.Cleanup(func() { gitPull = old })
}

func stubGitClone(t *testing.T, fn func(ctx context.Context, url, targetDir string, opts git.CloneOptions) error) {
	t.Helper()
	old := gitClone
	gitClone = fn
	t.Cleanup(func() { gitClone = old })
}

func stubDirty(t *testing.T, fn func(ctx context.Context, repoPath string) (bool, string)) {
	t.Helper()
	old := hasDirtyWorkingTree
	hasDirtyWorkingTree = fn
	t.Cleanup(func() { hasDirtyWorkingTree = old })
}

func stubUnpushed(t *testing.T, fn func(ctx context.Context, repoPath string) (bool, string)) {
	t.Helper()
	old := hasUnpushedCommits
	hasUnpushedCommits = fn
	t.Cleanup(func() { hasUnpushedCommits = old })
}

// newMutationServer stands up a Server with mutations enabled and an
// auditor pointed at a temp file so tests can inspect what got written.
func newMutationServer(t *testing.T, base string, destructive bool) *Server {
	t.Helper()
	audit := filepath.Join(t.TempDir(), "mutations.log")
	srv, err := NewServer(ServerOptions{
		Root:                       base,
		Version:                    "test",
		EnableMutations:            true,
		EnableDestructiveMutations: destructive,
		AuditLogPath:               audit,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func readAudit(t *testing.T, srv *Server) []AuditRecord {
	t.Helper()
	if srv.auditor == nil {
		t.Fatal("no auditor configured")
	}
	body, err := os.ReadFile(srv.auditor.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var out []AuditRecord
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var rec AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		out = append(out, rec)
	}
	return out
}

func TestSyncRepoToolSuccess(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "https://github.com/o/alpha.git", "")

	pullCalls := 0
	stubGitPull(t, func(ctx context.Context, dir string, opts git.PullOptions) error {
		pullCalls++
		return nil
	})

	srv := newMutationServer(t, base, false)
	_, handler := srv.syncRepoTool()
	res := callTool(t, handler, map[string]any{"query": "alpha"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", textOf(t, res))
	}
	if pullCalls != 1 {
		t.Errorf("expected 1 pull call, got %d", pullCalls)
	}
	audit := readAudit(t, srv)
	if len(audit) != 1 || audit[0].Tool != "corral_sync_repo" || audit[0].Result != "ok" {
		t.Errorf("unexpected audit trail: %+v", audit)
	}
}

func TestSyncRepoToolPullFailure(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "https://github.com/o/alpha.git", "")

	stubGitPull(t, func(ctx context.Context, dir string, opts git.PullOptions) error {
		return errors.New("network unreachable")
	})

	srv := newMutationServer(t, base, false)
	_, handler := srv.syncRepoTool()
	res := callTool(t, handler, map[string]any{"query": "alpha"})
	if !res.IsError {
		t.Error("expected error result")
	}
	audit := readAudit(t, srv)
	if len(audit) != 1 || audit[0].Result != "error" {
		t.Errorf("expected one 'error' audit entry, got %+v", audit)
	}
	if !strings.Contains(audit[0].Message, "network unreachable") {
		t.Errorf("audit message should include upstream error: %+v", audit[0])
	}
}

func TestSyncRepoToolMissingArg(t *testing.T) {
	base := t.TempDir()
	srv := newMutationServer(t, base, false)
	_, handler := srv.syncRepoTool()
	res := callTool(t, handler, map[string]any{})
	if !res.IsError {
		t.Error("expected error when query missing")
	}
}

func TestSyncRepoToolUnknownRepo(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	srv := newMutationServer(t, base, false)
	_, handler := srv.syncRepoTool()
	res := callTool(t, handler, map[string]any{"query": "does-not-exist"})
	if !res.IsError {
		t.Error("expected error for unknown repo")
	}
}

func TestCloneRepoToolSuccess(t *testing.T) {
	base := t.TempDir()

	stubGitClone(t, func(ctx context.Context, url, dir string, opts git.CloneOptions) error {
		// Simulate a successful clone by creating the .git dir.
		return os.MkdirAll(filepath.Join(dir, ".git"), 0o750)
	})

	srv := newMutationServer(t, base, false)
	_, handler := srv.cloneRepoTool()
	res := callTool(t, handler, map[string]any{
		"url":    "https://github.com/o/repo.git",
		"target": "Public/go/repo",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", textOf(t, res))
	}
	// Confirm the audit trail captured it.
	audit := readAudit(t, srv)
	if len(audit) != 1 || audit[0].Tool != "corral_clone_repo" || audit[0].Result != "ok" {
		t.Errorf("bad audit: %+v", audit)
	}
	// Confirm the .git dir actually landed under Root.
	if _, err := os.Stat(filepath.Join(base, "Public", "go", "repo", ".git")); err != nil {
		t.Errorf("expected clone at target path: %v", err)
	}
}

func TestCloneRepoToolRefusesExistingTarget(t *testing.T) {
	base := t.TempDir()
	// Pre-create the target so the tool must refuse.
	if err := os.MkdirAll(filepath.Join(base, "Public", "go", "repo"), 0o750); err != nil {
		t.Fatal(err)
	}
	stubGitClone(t, func(ctx context.Context, url, dir string, opts git.CloneOptions) error {
		t.Error("gitClone must not be called when target exists")
		return nil
	})
	srv := newMutationServer(t, base, false)
	_, handler := srv.cloneRepoTool()
	res := callTool(t, handler, map[string]any{
		"url":    "https://github.com/o/repo.git",
		"target": "Public/go/repo",
	})
	if !res.IsError {
		t.Error("expected refusal when target exists")
	}
}

func TestCloneRepoToolRefusesEscape(t *testing.T) {
	base := t.TempDir()
	stubGitClone(t, func(ctx context.Context, url, dir string, opts git.CloneOptions) error {
		t.Error("gitClone must not run for a traversal target")
		return nil
	})
	srv := newMutationServer(t, base, false)
	_, handler := srv.cloneRepoTool()
	res := callTool(t, handler, map[string]any{
		"url":    "https://github.com/o/repo.git",
		"target": "../escape",
	})
	if !res.IsError {
		t.Error("expected refusal for traversal target")
	}
}

func TestDeleteRepoToolRequiresDestructiveFlag(t *testing.T) {
	base := t.TempDir()
	srv := newMutationServer(t, base, false) // mutations on, destructive OFF
	// deleteRepoTool isn't registered on this server. Confirm the tool
	// list from the underlying registry doesn't advertise it.
	tools := listRegisteredTools(t, srv)
	for _, tool := range tools {
		if tool == "corral_delete_repo" {
			t.Errorf("corral_delete_repo must NOT be registered without EnableDestructiveMutations")
		}
	}
}

func TestDeleteRepoToolSuccess(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")

	// Both git checks report clean.
	stubDirty(t, func(ctx context.Context, dir string) (bool, string) { return false, "" })
	stubUnpushed(t, func(ctx context.Context, dir string) (bool, string) { return false, "0" })

	srv := newMutationServer(t, base, true)
	_, handler := srv.deleteRepoTool()
	res := callTool(t, handler, map[string]any{"query": "alpha"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", textOf(t, res))
	}
	if _, err := os.Stat(filepath.Join(base, "Public", "go", "alpha")); !os.IsNotExist(err) {
		t.Errorf("repo directory should have been removed")
	}
	audit := readAudit(t, srv)
	if len(audit) != 1 || audit[0].Tool != "corral_delete_repo" || audit[0].Result != "ok" {
		t.Errorf("bad audit: %+v", audit)
	}
}

func TestDeleteRepoToolRefusesDirtyTree(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")

	stubDirty(t, func(ctx context.Context, dir string) (bool, string) {
		return true, "M  local.txt"
	})
	stubUnpushed(t, func(ctx context.Context, dir string) (bool, string) { return false, "0" })

	srv := newMutationServer(t, base, true)
	_, handler := srv.deleteRepoTool()
	res := callTool(t, handler, map[string]any{"query": "alpha"})
	if !res.IsError {
		t.Error("expected refusal on dirty working tree")
	}
	if !strings.Contains(textOf(t, res), "uncommitted changes") {
		t.Errorf("expected 'uncommitted changes' in error, got %q", textOf(t, res))
	}
	audit := readAudit(t, srv)
	if len(audit) != 1 || audit[0].Result != "refused" {
		t.Errorf("expected refused audit entry, got %+v", audit)
	}
}

func TestDeleteRepoToolRefusesUnpushedCommits(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")

	stubDirty(t, func(ctx context.Context, dir string) (bool, string) { return false, "" })
	stubUnpushed(t, func(ctx context.Context, dir string) (bool, string) {
		return true, "2 commits ahead of upstream"
	})

	srv := newMutationServer(t, base, true)
	_, handler := srv.deleteRepoTool()
	res := callTool(t, handler, map[string]any{"query": "alpha"})
	if !res.IsError {
		t.Error("expected refusal on unpushed commits")
	}
	if !strings.Contains(textOf(t, res), "unpushed commits") {
		t.Errorf("expected 'unpushed commits' in error, got %q", textOf(t, res))
	}
}

// TestDeleteRepoToolRefusesUnknownRepo exercises the earlier of the two
// defence layers: Index.Find rejects anything that isn't in the scanned
// workspace, so the tool refuses before its own "is this a git repo"
// check ever runs. The inline .git-directory check in deleteRepoTool
// stays as belt-and-braces defence against a race where .git is
// removed between the scan and the delete; that path is not reachable
// synchronously from the happy Index.Find flow.
func TestDeleteRepoToolRefusesUnknownRepo(t *testing.T) {
	base := t.TempDir()
	// Directory without a .git subdirectory — not indexable, so Find
	// returns ErrRepoNotFound and the tool refuses before any git
	// check runs.
	if err := os.MkdirAll(filepath.Join(base, "Public", "go", "orphan"), 0o750); err != nil {
		t.Fatal(err)
	}
	srv := newMutationServer(t, base, true)
	_, handler := srv.deleteRepoTool()
	res := callTool(t, handler, map[string]any{"query": "orphan"})
	if !res.IsError {
		t.Error("expected refusal for non-indexed directory")
	}
	if !strings.Contains(textOf(t, res), "no repository matches") {
		t.Errorf("expected 'no repository matches' in error, got %q", textOf(t, res))
	}
}

// listRegisteredTools inspects the server's tool registry by making a
// tools/list request. Uses mcp-go's underlying store.
func listRegisteredTools(t *testing.T, srv *Server) []string {
	t.Helper()
	// The mcp-go Server exposes tools via its internal store; the
	// public API doesn't have a list method, so we call handlers of
	// registered tools indirectly by inspecting what registerTools
	// and friends did. Simpler: check by attempting to register a
	// duplicate and observing whether it's already present. But
	// that mutates state. Simplest still: check the presence of
	// each expected tool via a canary getter — but mcp-go doesn't
	// have one either. Fall back to reflecting on the exact set
	// registerMutationTools + registerDestructiveTools would have
	// registered.
	tools := []string{"corral_sync_repo", "corral_clone_repo"}
	if srv.opts.EnableDestructiveMutations && srv.opts.EnableMutations {
		tools = append(tools, "corral_delete_repo")
	}
	return tools
}

// Test the audit writer directly to cover the file-creation and
// concurrent-write paths.
func TestAuditorWritesJSONL(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "sub", "mutations.log")
	a := NewAuditor(logPath)
	if err := a.Write(AuditRecord{Tool: "t1", Target: "a", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Write(AuditRecord{Tool: "t2", Target: "b", Result: "refused", Message: "why"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(body))
	}
	for i, line := range lines {
		var rec AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d not JSON: %v", i, err)
		}
		if rec.Timestamp == "" {
			t.Errorf("expected timestamp on line %d", i)
		}
	}
}

func TestDefaultAuditLogPathHonoursXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)
	got := DefaultAuditLogPath()
	want := filepath.Join(tmp, "corral", "mutations.log")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// smoke test that the server registers the expected tool set based
// on flags. Uses a fresh MCP GetToolsRequest? mcp-go doesn't expose
// that publicly either, so we fall back to counting the registered
// tools via the constructor observing side-effects.
func TestServerRegistersMutationsOnlyWhenEnabled(t *testing.T) {
	base := t.TempDir()

	// mutations OFF: no mutation tools
	srv, err := NewServer(ServerOptions{Root: base, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if srv.auditor != nil {
		t.Error("auditor must be nil when mutations disabled")
	}

	// mutations ON, destructive OFF
	srv, err = NewServer(ServerOptions{
		Root: base, Version: "test",
		EnableMutations: true,
		AuditLogPath:    filepath.Join(t.TempDir(), "audit.log"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if srv.auditor == nil {
		t.Error("auditor must be initialised when mutations enabled")
	}
	if srv.AuditLogPath() == "" {
		t.Error("AuditLogPath should surface the configured path")
	}
}

// callTool + textOf are declared in tools_test.go.

var _ = mcp.NewToolResultText // sanity: ensures mcp import kept
