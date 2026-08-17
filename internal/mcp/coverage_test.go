// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sebastienrousseau/corral/internal/git"
)

type fakeSyncFile struct {
	name     string
	writeErr error
	closeErr error
}

func (f *fakeSyncFile) Write([]byte) (int, error) { return 0, f.writeErr }
func (f *fakeSyncFile) Close() error              { return f.closeErr }
func (f *fakeSyncFile) Name() string              { return f.name }

type fakeAuditWriter struct{ writeErr error }

func (f *fakeAuditWriter) Write(p []byte) (int, error) { return len(p), f.writeErr }
func (f *fakeAuditWriter) Close() error                { return nil }

type fakeResourceEntry struct{ name string }

func (f fakeResourceEntry) Name() string               { return f.name }
func (f fakeResourceEntry) IsDir() bool                { return false }
func (f fakeResourceEntry) Type() fs.FileMode          { return 0 }
func (f fakeResourceEntry) Info() (fs.FileInfo, error) { return nil, nil }

func TestPromptHandlers(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	explain, explainHandler := srv.explainWorkspacePrompt()
	if explain.Name != "explain_workspace" {
		t.Fatalf("prompt name = %q", explain.Name)
	}
	result, err := explainHandler(context.Background(), mcp.GetPromptRequest{})
	if err != nil || len(result.Messages) != 1 {
		t.Fatalf("explain result = %+v, %v", result, err)
	}

	stale, staleHandler := srv.identifyStaleReposPrompt()
	if stale.Name != "identify_stale_repos" {
		t.Fatalf("prompt name = %q", stale.Name)
	}
	for _, tc := range []struct {
		args map[string]string
		want string
	}{{nil, "30 days"}, {map[string]string{}, "30 days"}, {map[string]string{"threshold_days": "7"}, "7 days"}} {
		req := mcp.GetPromptRequest{}
		req.Params.Arguments = tc.args
		got, handlerErr := staleHandler(context.Background(), req)
		if handlerErr != nil {
			t.Fatal(handlerErr)
		}
		content, ok := got.Messages[0].Content.(mcp.TextContent)
		if !ok || !strings.Contains(content.Text, tc.want) {
			t.Fatalf("stale prompt content = %#v, want %q", got.Messages[0].Content, tc.want)
		}
	}
}

func TestAuditDefaultsAndFailures(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	if got := DefaultAuditLogPath(); !strings.HasSuffix(got, filepath.Join("state", "corral", "mutations.log")) {
		t.Fatalf("XDG audit path = %q", got)
	}
	oldHome := auditUserHomeDir
	t.Cleanup(func() { auditUserHomeDir = oldHome })
	_ = os.Unsetenv("XDG_STATE_HOME")
	homeDir := filepath.Join(string(filepath.Separator), "home", "test")
	auditUserHomeDir = func() (string, error) { return homeDir, nil }
	if got := DefaultAuditLogPath(); got != filepath.Join(homeDir, ".local", "state", "corral", "mutations.log") {
		t.Fatalf("home audit path = %q", got)
	}
	auditUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	if got := DefaultAuditLogPath(); !strings.HasSuffix(got, filepath.Join("corral", "mutations.log")) {
		t.Fatalf("fallback audit path = %q", got)
	}

	auditor := NewAuditor(filepath.Join(t.TempDir(), "audit.log"))
	if err := auditor.Write(AuditRecord{Args: map[string]any{"bad": make(chan int)}}); err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal failure, got %v", err)
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewAuditor(filepath.Join(blocked, "audit.log")).Write(AuditRecord{Tool: "x"}); err == nil || !strings.Contains(err.Error(), "create audit dir") {
		t.Fatalf("expected mkdir failure, got %v", err)
	}
	dirPath := t.TempDir()
	if err := NewAuditor(dirPath).Write(AuditRecord{Tool: "x"}); err == nil || !strings.Contains(err.Error(), "open audit log") {
		t.Fatalf("expected open failure, got %v", err)
	}
}

func TestServeStdioDelegates(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	old := serveStdio
	t.Cleanup(func() { serveStdio = old })
	want := errors.New("stdio stopped")
	serveStdio = func(got *server.MCPServer, opts ...server.StdioOption) error {
		if got != srv.mcp {
			t.Fatal("ServeStdio passed the wrong server")
		}
		return want
	}
	if err := srv.ServeStdio(); !errors.Is(err, want) {
		t.Fatalf("ServeStdio error = %v", err)
	}
}

func TestRepoTreeCancellationAndTruncation(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	oldWalk := walkResource
	t.Cleanup(func() { walkResource = oldWalk })
	walkResource = func(root string, fn fs.WalkDirFunc) error {
		entry := fakeResourceEntry{name: "entry"}
		if err := fn(root, entry, nil); err != nil {
			return err
		}
		for i := 0; i <= maxTreeEntries; i++ {
			path := filepath.Join(root, "entry-"+strconv.Itoa(i))
			if err := fn(path, entry, nil); err != nil {
				if errors.Is(err, filepath.SkipAll) {
					return nil
				}
				return err
			}
		}
		return nil
	}
	srv := newTestServer(t, base)
	_, handler := srv.repoTreeResource()
	contents, err := readResource(t, handler, "corral://repo/Public/alpha/tree")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(contents[0].(mcp.TextResourceContents).Text, "tree truncated") {
		t.Fatal("expected tree truncation marker")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := mcp.ReadResourceRequest{}
	req.Params.URI = "corral://repo/Public/alpha/tree"
	if _, err := handler(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled tree error = %v", err)
	}
}

func TestIndexInjectedFailuresAndBounds(t *testing.T) {
	oldAbs, oldStat, oldWalk, oldRel, oldMax := absIndex, statIndex, walkIndex, relIndex, maxIndexRepos
	t.Cleanup(func() {
		absIndex, statIndex, walkIndex, relIndex, maxIndexRepos = oldAbs, oldStat, oldWalk, oldRel, oldMax
	})
	absIndex = func(string) (string, error) { return "", errors.New("abs failed") }
	if _, err := Scan("x"); err == nil || !strings.Contains(err.Error(), "resolving root") {
		t.Fatalf("absolute root error = %v", err)
	}
	absIndex = filepath.Abs
	statIndex = func(string) (os.FileInfo, error) { return nil, errors.New("stat failed") }
	if _, err := Scan("x"); err == nil || !strings.Contains(err.Error(), "stat root") {
		t.Fatalf("stat root error = %v", err)
	}
	statIndex = os.Stat

	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	file := filepath.Join(root, "file")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var dirEntry, fileEntry os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			dirEntry = entry
		} else {
			fileEntry = entry
		}
	}
	walkIndex = func(base string, fn fs.WalkDirFunc) error {
		_ = fn(file, fileEntry, nil)
		_ = fn(dir, dirEntry, errors.New("dir unreadable"))
		_ = fn(file, nil, errors.New("file unreadable"))
		return nil
	}
	if _, err := Scan(root); err != nil {
		t.Fatal(err)
	}

	walkIndex = func(base string, fn fs.WalkDirFunc) error { _ = fn(dir, dirEntry, nil); return nil }
	relIndex = func(string, string) (string, error) { return "", errors.New("rel failed") }
	if _, err := Scan(root); err != nil {
		t.Fatal(err)
	}
	relIndex = filepath.Rel
	deep := filepath.Join(root, "a", "b", "c", "d", "e")
	walkIndex = func(base string, fn fs.WalkDirFunc) error { _ = fn(deep, dirEntry, nil); return nil }
	if _, err := Scan(root); err != nil {
		t.Fatal(err)
	}

	maxIndexRepos = 0
	walkIndex = func(base string, fn fs.WalkDirFunc) error {
		if got := fn(root, dirEntry, nil); got != fs.SkipAll {
			t.Fatalf("cap callback = %v", got)
		}
		return nil
	}
	idx, err := Scan(root)
	if err != nil || !idx.Truncated {
		t.Fatalf("truncated index = %+v, %v", idx, err)
	}
}

func TestStateInjectedIOFailures(t *testing.T) {
	oldRead, oldCreate, oldRename := readStateFile, createSyncTemp, renameSyncFile
	t.Cleanup(func() { readStateFile, createSyncTemp, renameSyncFile = oldRead, oldCreate, oldRename })
	readStateFile = func(string) ([]byte, error) { return nil, errors.New("read failed") }
	if state, ok := readState(t.TempDir()); ok || state != nil {
		t.Fatalf("read failure = %+v %v", state, ok)
	}
	readStateFile = oldRead
	dir := t.TempDir()
	// markStateSynced resolves the git directory before creating a temp file.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o750); err != nil {
		t.Fatal(err)
	}
	createSyncTemp = func(string, string) (syncTempFile, error) { return nil, errors.New("create failed") }
	if err := markStateSynced(dir); err == nil || !strings.Contains(err.Error(), "create sync") {
		t.Fatalf("create error = %v", err)
	}
	createSyncTemp = func(string, string) (syncTempFile, error) {
		return &fakeSyncFile{name: filepath.Join(dir, "tmp"), writeErr: errors.New("write failed")}, nil
	}
	if err := markStateSynced(dir); err == nil || !strings.Contains(err.Error(), "write sync") {
		t.Fatalf("write error = %v", err)
	}
	createSyncTemp = func(string, string) (syncTempFile, error) {
		return &fakeSyncFile{name: filepath.Join(dir, "tmp"), closeErr: errors.New("close failed")}, nil
	}
	if err := markStateSynced(dir); err == nil || !strings.Contains(err.Error(), "close sync") {
		t.Fatalf("close error = %v", err)
	}
	createSyncTemp = oldCreate
	renameSyncFile = func(string, string) error { return errors.New("rename failed") }
	if err := markStateSynced(dir); err == nil || !strings.Contains(err.Error(), "replace sync") {
		t.Fatalf("rename error = %v", err)
	}
}

func TestSafePathInjectedErrorsAndCanonicalFallback(t *testing.T) {
	oldAbs, oldRel, oldEval := absSafePath, relSafePath, evalSafePath
	t.Cleanup(func() { absSafePath, relSafePath, evalSafePath = oldAbs, oldRel, oldEval })
	idx := &Index{Root: t.TempDir()}
	absSafePath = func(string) (string, error) { return "", errors.New("abs failed") }
	if _, err := idx.SafePath("x"); err == nil || !strings.Contains(err.Error(), "resolving path") {
		t.Fatalf("abs error = %v", err)
	}
	absSafePath = filepath.Abs
	relSafePath = func(string, string) (string, error) { return "", errors.New("rel failed") }
	if _, err := idx.SafePath("x"); err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("rel error = %v", err)
	}
	evalSafePath = func(string) (string, error) { return "", errors.New("eval failed") }
	if got := canonicalizeExistingPrefix("/"); got != "/" {
		t.Fatalf("canonical root fallback = %q", got)
	}
}

func TestAuditWriteFailure(t *testing.T) {
	oldOpen := openAuditFile
	t.Cleanup(func() { openAuditFile = oldOpen })
	openAuditFile = func(string, int, os.FileMode) (auditFile, error) {
		return &fakeAuditWriter{writeErr: errors.New("write failed")}, nil
	}
	if err := NewAuditor(filepath.Join(t.TempDir(), "audit.log")).Write(AuditRecord{Tool: "x"}); err == nil || !strings.Contains(err.Error(), "write audit") {
		t.Fatalf("audit write error = %v", err)
	}
}

func TestToolAndResourceScanErrors(t *testing.T) {
	oldScan := scanWorkspace
	t.Cleanup(func() { scanWorkspace = oldScan })
	scanWorkspace = func(string) (*Index, error) { return nil, errors.New("scan failed") }
	srv := newTestServer(t, t.TempDir())
	toolHandlers := []func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){}
	_, list := srv.listReposTool()
	toolHandlers = append(toolHandlers, list)
	_, find := srv.findRepoTool()
	toolHandlers = append(toolHandlers, find)
	_, metadata := srv.repoMetadataTool()
	toolHandlers = append(toolHandlers, metadata)
	_, status := srv.statusSummaryTool()
	toolHandlers = append(toolHandlers, status)
	_, workspace := srv.workspaceIndexTool()
	toolHandlers = append(toolHandlers, workspace)
	for i, handler := range toolHandlers {
		args := map[string]any{}
		if i == 1 || i == 2 {
			args["query"] = "repo"
		}
		result := callTool(t, handler, args)
		if !result.IsError || !strings.Contains(result.Content[0].(mcp.TextContent).Text, "scan") {
			t.Fatalf("tool %d result = %+v", i, result)
		}
	}
	_, workspaceResource := srv.workspaceIndexResource()
	req := mcp.ReadResourceRequest{}
	req.Params.URI = "corral://workspace/index"
	if _, err := workspaceResource(context.Background(), req); err == nil {
		t.Fatal("expected workspace resource scan error")
	}
}

func TestResourceInjectedFailures(t *testing.T) {
	base := t.TempDir()
	repo := makeFakeRepo(t, base, "Public", "go", "alpha", "", `{"last_synced_at":"now"}`)
	srv := newTestServer(t, base)
	oldMarshal, oldWalk, oldOpen, oldRead := marshalResource, walkResource, openResource, readResourceAll
	t.Cleanup(func() {
		marshalResource, walkResource, openResource, readResourceAll = oldMarshal, oldWalk, oldOpen, oldRead
	})
	marshalResource = func(any, string, string) ([]byte, error) { return nil, errors.New("marshal failed") }
	_, workspace := srv.workspaceIndexResource()
	if _, err := readResource(t, workspace, "corral://workspace/index"); err == nil {
		t.Fatal("expected index marshal error")
	}
	_, state := srv.repoStateResource()
	if _, err := readResource(t, state, "corral://repo/Public/alpha/state"); err == nil {
		t.Fatal("expected state marshal error")
	}
	marshalResource = json.MarshalIndent
	walkResource = func(string, fs.WalkDirFunc) error { return errors.New("walk failed") }
	_, tree := srv.repoTreeResource()
	if _, err := readResource(t, tree, "corral://repo/Public/alpha/tree"); err == nil {
		t.Fatal("expected tree walk error")
	}
	openResource = func(string) (*os.File, error) { return nil, errors.New("open failed") }
	_, file := srv.repoFileResource()
	if _, err := readResource(t, file, "corral://repo/Public/alpha/file/missing"); err == nil || !strings.Contains(err.Error(), "open file") {
		t.Fatalf("open resource error = %v", err)
	}
	openResource = os.Open
	readResourceAll = func(io.Reader) ([]byte, error) { return nil, errors.New("read failed") }
	if err := os.WriteFile(filepath.Join(repo, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readResource(t, file, "corral://repo/Public/alpha/file/file"); err == nil || !strings.Contains(err.Error(), "read file") {
		t.Fatalf("read resource error = %v", err)
	}
}

func TestURIParsingAdditionalErrors(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	if _, err := srv.resolveURIRepo("corral://repo/%zz"); err == nil || !strings.Contains(err.Error(), "invalid uri") {
		t.Fatalf("invalid URI error = %v", err)
	}
	if ownerMatchesURL("", "owner") || ownerMatchesURL("https://github.com/a/r", "") {
		t.Fatal("empty owner URL unexpectedly matched")
	}
	if _, err := extractFilePath("corral://repo/o/n/file/%zz"); err == nil || !strings.Contains(err.Error(), "decoding") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestBuildEntryTwoLevelAndMarshalStateFailure(t *testing.T) {
	root := t.TempDir()
	repo := makeFakeRepo(t, root, "", "go", "repo", "", "")
	entry := buildEntry(root, repo)
	if entry.Language != "go" || entry.Visibility != "" {
		t.Fatalf("two-level entry = %+v", entry)
	}
	oldMarshal := marshalSyncState
	t.Cleanup(func() { marshalSyncState = oldMarshal })
	marshalSyncState = func(any, string, string) ([]byte, error) { return nil, errors.New("marshal failed") }
	if err := markStateSynced(repo); err == nil || !strings.Contains(err.Error(), "marshal sync") {
		t.Fatalf("marshal state error = %v", err)
	}
}

func TestResourceResolutionAndTreeCallbackBranches(t *testing.T) {
	base := t.TempDir()
	repo := makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	srv := newTestServer(t, base)
	_, tree := srv.repoTreeResource()
	if _, err := readResource(t, tree, "corral://repo/Public/missing/tree"); err == nil {
		t.Fatal("expected tree repo resolution error")
	}
	_, file := srv.repoFileResource()
	if _, err := readResource(t, file, "corral://repo/Public/missing/file/x"); err == nil {
		t.Fatal("expected file repo resolution error")
	}

	deepDir := filepath.Join(repo, "a", "b", "c")
	if err := os.MkdirAll(deepDir, 0o750); err != nil {
		t.Fatal(err)
	}
	deepFile := filepath.Join(repo, "a", "b", "file")
	if err := os.WriteFile(deepFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	rootEntries, _ := os.ReadDir(filepath.Join(repo, "a", "b"))
	var dirEntry, fileEntry os.DirEntry
	for _, entry := range rootEntries {
		if entry.IsDir() {
			dirEntry = entry
		} else {
			fileEntry = entry
		}
	}
	oldWalk := walkResource
	t.Cleanup(func() { walkResource = oldWalk })
	walkResource = func(root string, fn fs.WalkDirFunc) error {
		_ = fn(root, dirEntry, nil)
		_ = fn(deepDir, dirEntry, nil)
		_ = fn(deepFile, fileEntry, nil)
		_ = fn(filepath.Join(root, "bad"), nil, errors.New("entry failed"))
		return nil
	}
	if _, err := readResource(t, tree, "corral://repo/Public/alpha/tree"); err != nil {
		t.Fatal(err)
	}
}

func TestResolveURIRepoScanFailureAndAuditDisabled(t *testing.T) {
	srv := newTestServer(t, t.TempDir())
	if srv.AuditLogPath() != "" {
		t.Fatal("read-only server returned audit path")
	}
	if err := srv.audit(AuditRecord{}); err == nil || !strings.Contains(err.Error(), "no auditor") {
		t.Fatalf("nil auditor error = %v", err)
	}
	oldScan := scanWorkspace
	t.Cleanup(func() { scanWorkspace = oldScan })
	scanWorkspace = func(string) (*Index, error) { return nil, errors.New("scan failed") }
	if _, err := srv.resolveURIRepo("corral://repo/Public/repo/state"); err == nil || !strings.Contains(err.Error(), "scan failed") {
		t.Fatalf("URI scan error = %v", err)
	}
}

func TestAdditionalToolFiltersAndMetadataErrors(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	srv := newTestServer(t, base)
	_, list := srv.listReposTool()
	res := callTool(t, list, map[string]any{"name_contains": "zzz"})
	if res.IsError {
		t.Fatalf("name filter result = %+v", res)
	}
	_, metadata := srv.repoMetadataTool()
	if res := callTool(t, metadata, map[string]any{}); !res.IsError {
		t.Fatal("metadata missing query must fail")
	}
	if res := callTool(t, metadata, map[string]any{"query": "missing"}); !res.IsError {
		t.Fatal("metadata missing repo must fail")
	}

	oldBranch := currentBranch
	t.Cleanup(func() { currentBranch = oldBranch })
	currentBranch = oldBranch
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	if branch := currentBranch(context.Background(), filepath.Join(base, "missing")); branch != "" {
		t.Fatalf("failed branch = %q", branch)
	}
	t.Setenv("PATH", originalPath)
	if branch := currentBranch(context.Background(), base); branch != "" {
		t.Fatalf("stderr branch = %q", branch)
	}
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-b", "feature/coverage", repo) // #nosec G204 -- fixed test command
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	cmd = exec.Command("git", "-C", repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init") // #nosec G204 -- fixed test command
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
	if branch := currentBranch(context.Background(), repo); branch != "feature/coverage" {
		t.Fatalf("successful branch = %q", branch)
	}
}

func setAuditFailure(t *testing.T, failAt int) {
	t.Helper()
	oldOpen := openAuditFile
	calls := 0
	openAuditFile = func(string, int, os.FileMode) (auditFile, error) {
		calls++
		writer := &fakeAuditWriter{}
		if calls == failAt {
			writer.writeErr = errors.New("audit write failed")
		}
		return writer, nil
	}
	t.Cleanup(func() { openAuditFile = oldOpen })
}

func TestMutationArgumentScanAndSandboxErrors(t *testing.T) {
	base := t.TempDir()
	srv := newMutationServer(t, base, true)
	_, syncHandler := srv.syncRepoTool()
	_, cloneHandler := srv.cloneRepoTool()
	_, deleteHandler := srv.deleteRepoTool()
	if !callTool(t, syncHandler, nil).IsError {
		t.Fatal("sync missing query must fail")
	}
	if !callTool(t, cloneHandler, nil).IsError {
		t.Fatal("clone missing URL must fail")
	}
	if !callTool(t, cloneHandler, map[string]any{"url": "https://example.com/r.git"}).IsError {
		t.Fatal("clone missing target must fail")
	}
	if !callTool(t, deleteHandler, nil).IsError {
		t.Fatal("delete missing query must fail")
	}

	oldScan := scanWorkspace
	t.Cleanup(func() { scanWorkspace = oldScan })
	scanWorkspace = func(string) (*Index, error) { return nil, errors.New("scan failed") }
	srv.invalidateScanCache()
	if !callTool(t, syncHandler, map[string]any{"query": "repo"}).IsError {
		t.Fatal("sync scan must fail")
	}
	if !callTool(t, deleteHandler, map[string]any{"query": "repo"}).IsError {
		t.Fatal("delete scan must fail")
	}
	scanWorkspace = oldScan

	outside := filepath.Join(t.TempDir(), "repo")
	for _, handler := range []func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){syncHandler, deleteHandler} {
		srv.scanIndex = &Index{Root: base, Repos: []RepoEntry{{Name: "repo", Path: outside, RelPath: "repo"}}}
		srv.scanExpires = time.Now().Add(time.Hour)
		if !callTool(t, handler, map[string]any{"query": "repo"}).IsError {
			t.Fatal("sandbox escape must fail")
		}
	}

	if got := redactCloneURL("git@example.com:owner/repo.git"); got != "REDACTED@example.com:owner/repo.git" {
		t.Fatalf("scp redaction = %q", got)
	}
	if got := redactCloneURL("local-path"); got != "local-path" {
		t.Fatalf("raw redaction = %q", got)
	}
}

func TestSyncMutationFailureMatrix(t *testing.T) {
	base := t.TempDir()
	repo := makeFakeRepo(t, base, "Public", "go", "repo", "", "")
	oldPull, oldMark := gitPull, markSynced
	t.Cleanup(func() { gitPull, markSynced = oldPull, oldMark })

	run := func(failAuditAt int, pullErr, stateErr error) *mcp.CallToolResult {
		srv := newMutationServer(t, base, false)
		_, handler := srv.syncRepoTool()
		gitPull = func(context.Context, string, git.PullOptions) error { return pullErr }
		markSynced = func(string) error { return stateErr }
		setAuditFailure(t, failAuditAt)
		return callTool(t, handler, map[string]any{"query": "repo"})
	}
	if res := run(2, errors.New("pull failed"), nil); !res.IsError {
		t.Fatal("pull plus completion audit must fail")
	}
	if res := run(1, nil, nil); !res.IsError {
		t.Fatal("sync intent audit must fail")
	}
	if res := run(0, nil, errors.New("state failed")); !res.IsError {
		t.Fatal("state update must fail")
	}
	if res := run(2, nil, errors.New("state failed")); !res.IsError {
		t.Fatal("state plus completion audit must fail")
	}
	if res := run(2, nil, nil); !res.IsError {
		t.Fatal("success completion audit must fail")
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", stateFileName)); err == nil {
		t.Fatal("state stub unexpectedly wrote sidecar")
	}
}

func TestCloneMutationFailureMatrix(t *testing.T) {
	base := t.TempDir()
	oldClone, oldMkdir, oldStat := gitClone, mkdirMutation, statMutation
	t.Cleanup(func() { gitClone, mkdirMutation, statMutation = oldClone, oldMkdir, oldStat })
	statMutation = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	run := func(failAuditAt int, mkdirErr, cloneErr error) *mcp.CallToolResult {
		srv := newMutationServer(t, base, false)
		_, handler := srv.cloneRepoTool()
		mkdirMutation = func(string, os.FileMode) error { return mkdirErr }
		gitClone = func(context.Context, string, string, git.CloneOptions) error { return cloneErr }
		setAuditFailure(t, failAuditAt)
		return callTool(t, handler, map[string]any{"url": "https://example.com/r.git", "target": "Public/go/r"})
	}
	if res := run(0, errors.New("mkdir failed"), nil); !res.IsError {
		t.Fatal("mkdir failure must fail")
	}
	if res := run(2, errors.New("mkdir failed"), nil); !res.IsError {
		t.Fatal("mkdir plus audit failure must fail")
	}
	if res := run(0, nil, errors.New("clone failed")); !res.IsError {
		t.Fatal("clone failure must fail")
	}
	if res := run(2, nil, errors.New("clone failed")); !res.IsError {
		t.Fatal("clone plus audit failure must fail")
	}
	if res := run(2, nil, nil); !res.IsError {
		t.Fatal("success completion audit must fail")
	}
}

func TestDeleteMutationFailureMatrix(t *testing.T) {
	base := t.TempDir()
	repo := makeFakeRepo(t, base, "Public", "go", "repo", "", "")
	oldDirty, oldAhead, oldRemove := hasDirtyWorkingTree, hasUnpushedCommits, removeMutation
	t.Cleanup(func() { hasDirtyWorkingTree, hasUnpushedCommits, removeMutation = oldDirty, oldAhead, oldRemove })
	hasDirtyWorkingTree = func(context.Context, string) (bool, string) { return false, "" }
	hasUnpushedCommits = func(context.Context, string) (bool, string) { return false, "" }
	run := func(failAuditAt int, removeErr error) *mcp.CallToolResult {
		srv := newMutationServer(t, base, true)
		_, handler := srv.deleteRepoTool()
		removeMutation = func(string) error { return removeErr }
		setAuditFailure(t, failAuditAt)
		return callTool(t, handler, map[string]any{"query": "repo"})
	}
	if res := run(0, errors.New("remove failed")); !res.IsError {
		t.Fatal("remove failure must fail")
	}
	if res := run(2, errors.New("remove failed")); !res.IsError {
		t.Fatal("remove plus audit failure must fail")
	}
	if res := run(2, nil); !res.IsError {
		t.Fatal("delete completion audit must fail")
	}
	if res := run(1, nil); !res.IsError {
		t.Fatal("delete intent audit must fail")
	}

	// Refusal audit failures for non-repository, dirty, and unpublished cases.
	plain := filepath.Join(base, "Public", "go", "plain")
	if err := os.MkdirAll(plain, 0o750); err != nil {
		t.Fatal(err)
	}
	plainServer := newMutationServer(t, base, true)
	_, plainHandler := plainServer.deleteRepoTool()
	plainServer.scanIndex = &Index{Root: base, Repos: []RepoEntry{{Name: "plain", Path: plain, RelPath: "Public/go/plain"}}}
	plainServer.scanExpires = time.Now().Add(time.Hour)
	setAuditFailure(t, 0)
	if res := callTool(t, plainHandler, map[string]any{"query": "plain"}); !res.IsError {
		t.Fatal("non-repository refusal must fail")
	}
	for mode := 0; mode < 3; mode++ {
		srv := newMutationServer(t, base, true)
		_, handler := srv.deleteRepoTool()
		switch mode {
		case 0:
			srv.scanIndex = &Index{Root: base, Repos: []RepoEntry{{Name: "plain", Path: plain, RelPath: "Public/go/plain"}}}
			srv.scanExpires = time.Now().Add(time.Hour)
		case 1:
			hasDirtyWorkingTree = func(context.Context, string) (bool, string) { return true, "dirty" }
		default:
			hasDirtyWorkingTree = func(context.Context, string) (bool, string) { return false, "" }
			hasUnpushedCommits = func(context.Context, string) (bool, string) { return true, "ahead" }
		}
		setAuditFailure(t, 1)
		query := "repo"
		if mode == 0 {
			query = "plain"
		}
		if res := callTool(t, handler, map[string]any{"query": query}); !res.IsError {
			t.Fatalf("refusal mode %d must fail", mode)
		}
	}
	_ = repo
}

func TestHasDirtyWorkingTreeDefault(t *testing.T) {
	if dirty, _ := hasDirtyWorkingTree(context.Background(), t.TempDir()); !dirty {
		t.Fatal("invalid repository must fail closed")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init", dir) // #nosec G204 -- fixed executable with a test-owned destination
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if dirty, detail := hasDirtyWorkingTree(context.Background(), dir); dirty || detail != "" {
		t.Fatalf("clean tree = %v %q", dirty, detail)
	}
	if err := os.WriteFile(filepath.Join(dir, "new"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if dirty, detail := hasDirtyWorkingTree(context.Background(), dir); !dirty || detail == "" {
		t.Fatalf("dirty tree = %v %q", dirty, detail)
	}
}
