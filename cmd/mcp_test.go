package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sebastienrousseau/corral/internal/mcp"
)

// stubMCPServer is the mcpServer implementation the tests inject via
// mcpNewServer. It records the constructor options and controls the
// ServeStdio outcome so runMCP's branches are all reachable without a
// real stdio loop.
type stubMCPServer struct {
	root            string
	mutations       bool
	serveErr        error
	serveCallCount  int
}

func (s *stubMCPServer) Root() string           { return s.root }
func (s *stubMCPServer) MutationsEnabled() bool { return s.mutations }
func (s *stubMCPServer) AuditLogPath() string   { return "" }
func (s *stubMCPServer) ServeStdio() error {
	s.serveCallCount++
	return s.serveErr
}

// withStubServer swaps in the given stubMCPServer and restores the
// production constructor at test teardown. Returns a pointer to the
// recorded ServerOptions so the test can assert what runMCP handed to
// the constructor.
func withStubServer(t *testing.T, stub *stubMCPServer, ctorErr error) *mcp.ServerOptions {
	t.Helper()
	captured := &mcp.ServerOptions{}
	old := mcpNewServer
	mcpNewServer = func(opts mcp.ServerOptions) (mcpServer, error) {
		*captured = opts
		if ctorErr != nil {
			return nil, ctorErr
		}
		return stub, nil
	}
	t.Cleanup(func() { mcpNewServer = old })
	return captured
}

// resetMCPFlags restores the package-level flag vars to their defaults
// so tests don't leak state into each other via cobra's global flag
// registry.
func resetMCPFlags(t *testing.T) {
	t.Helper()
	oldRoot, oldMut := mcpRoot, mcpEnableMutations
	mcpRoot = ""
	mcpEnableMutations = false
	t.Cleanup(func() { mcpRoot = oldRoot; mcpEnableMutations = oldMut })
}

func TestRunMCPHappyPath(t *testing.T) {
	resetMCPFlags(t)
	dir := t.TempDir()
	mcpRoot = dir

	stub := &stubMCPServer{root: dir}
	captured := withStubServer(t, stub, nil)

	if err := runMCP(nil, nil); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if stub.serveCallCount != 1 {
		t.Errorf("expected exactly one ServeStdio call, got %d", stub.serveCallCount)
	}

	// Absolute path arrived at the constructor.
	if abs, _ := filepath.Abs(dir); captured.Root != abs {
		t.Errorf("constructor root = %q, want %q", captured.Root, abs)
	}
}

func TestRunMCPDefaultsToBaseDir(t *testing.T) {
	resetMCPFlags(t)
	dir := t.TempDir()

	// mcpRoot unset → runMCP must fall back to the shared baseDir var.
	oldBase := baseDir
	baseDir = dir
	t.Cleanup(func() { baseDir = oldBase })

	stub := &stubMCPServer{root: dir}
	captured := withStubServer(t, stub, nil)

	if err := runMCP(nil, nil); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if abs, _ := filepath.Abs(dir); captured.Root != abs {
		t.Errorf("expected fallback root %q, got %q", abs, captured.Root)
	}
}

func TestRunMCPMutationsFlagPropagates(t *testing.T) {
	resetMCPFlags(t)
	dir := t.TempDir()
	mcpRoot = dir
	mcpEnableMutations = true

	stub := &stubMCPServer{root: dir, mutations: true}
	captured := withStubServer(t, stub, nil)

	if err := runMCP(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !captured.EnableMutations {
		t.Error("expected --enable-mutations to reach ServerOptions")
	}
}

func TestRunMCPRejectsMissingRoot(t *testing.T) {
	resetMCPFlags(t)
	mcpRoot = "/definitely/not/a/directory/at/all"

	stub := &stubMCPServer{}
	withStubServer(t, stub, nil)

	err := runMCP(nil, nil)
	if err == nil {
		t.Fatal("expected error for missing root")
	}
	if !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("expected 'not accessible', got %v", err)
	}
	if stub.serveCallCount != 0 {
		t.Errorf("ServeStdio should not run when root is invalid; called %d times", stub.serveCallCount)
	}
}

func TestRunMCPRejectsFileRoot(t *testing.T) {
	resetMCPFlags(t)
	f, err := os.CreateTemp("", "mcp_root_file_*")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	mcpRoot = f.Name()

	stub := &stubMCPServer{}
	withStubServer(t, stub, nil)

	err = runMCP(nil, nil)
	if err == nil {
		t.Fatal("expected error for non-directory root")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected 'not a directory', got %v", err)
	}
}

func TestRunMCPPropagatesConstructorError(t *testing.T) {
	resetMCPFlags(t)
	dir := t.TempDir()
	mcpRoot = dir

	ctorErr := errors.New("boom-from-ctor")
	withStubServer(t, nil, ctorErr)

	err := runMCP(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "constructing mcp server") {
		t.Errorf("expected wrapped constructor error, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "boom-from-ctor") {
		t.Errorf("expected inner error preserved in wrap, got %v", err)
	}
}

func TestRunMCPPropagatesServeError(t *testing.T) {
	resetMCPFlags(t)
	dir := t.TempDir()
	mcpRoot = dir

	serveErr := errors.New("stdio-blew-up")
	stub := &stubMCPServer{root: dir, serveErr: serveErr}
	withStubServer(t, stub, nil)

	err := runMCP(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "mcp server") {
		t.Errorf("expected wrapped serve error, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "stdio-blew-up") {
		t.Errorf("expected inner error preserved, got %v", err)
	}
}
