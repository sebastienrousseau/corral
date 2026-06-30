package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

var (
	osMkdirAll  = os.MkdirAll
	osWriteFile = os.WriteFile
)

// readResource invokes a resource handler with a hand-rolled
// ReadResourceRequest whose URI param matches the resource's template.
func readResource(t *testing.T, handler func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error), uri string) ([]mcp.ResourceContents, error) {
	t.Helper()
	req := mcp.ReadResourceRequest{}
	req.Params.URI = uri
	return handler(context.Background(), req)
}

func TestWorkspaceIndexResource(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")

	srv := newTestServer(t, base)
	_, handler := srv.workspaceIndexResource()
	contents, err := readResource(t, handler, "corral://workspace/index")
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(contents))
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatalf("expected TextResourceContents, got %T", contents[0])
	}
	var idx Index
	if err := json.Unmarshal([]byte(text.Text), &idx); err != nil {
		t.Fatal(err)
	}
	if len(idx.Repos) != 1 {
		t.Errorf("expected 1 repo in resource output, got %d", len(idx.Repos))
	}
}

func TestRepoStateResourceReturnsSidecar(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", `{"last_synced_at":"2026-06-30T00:00:00Z","last_synced_pushed_at":"2026-06-29T00:00:00Z"}`)

	srv := newTestServer(t, base)
	_, handler := srv.repoStateResource()
	contents, err := readResource(t, handler, "corral://repo/Public/alpha/state")
	if err != nil {
		t.Fatal(err)
	}
	text := contents[0].(mcp.TextResourceContents).Text
	if !strings.Contains(text, "2026-06-30T00:00:00Z") {
		t.Errorf("expected sidecar timestamps in output: %s", text)
	}
}

func TestRepoStateResourceMissingSidecar(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")

	srv := newTestServer(t, base)
	_, handler := srv.repoStateResource()
	_, err := readResource(t, handler, "corral://repo/Public/alpha/state")
	if err == nil {
		t.Error("expected error when sidecar absent")
	}
}

func TestRepoStateResourceUnknownRepo(t *testing.T) {
	base := t.TempDir()
	srv := newTestServer(t, base)
	_, handler := srv.repoStateResource()
	_, err := readResource(t, handler, "corral://repo/Public/missing/state")
	if err == nil {
		t.Error("expected error when repo missing")
	}
}

func TestRepoTreeResourceLists(t *testing.T) {
	base := t.TempDir()
	repo := makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	// Add a file at the root + a nested dir to ensure listing works.
	if err := writeFile(filepath.Join(repo, "main.go"), "package main\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(repo, "internal", "x.go"), "package internal\n"); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, base)
	_, handler := srv.repoTreeResource()
	contents, err := readResource(t, handler, "corral://repo/Public/alpha/tree")
	if err != nil {
		t.Fatal(err)
	}
	text := contents[0].(mcp.TextResourceContents).Text
	for _, want := range []string{"main.go", "internal/"} {
		if !strings.Contains(text, want) {
			t.Errorf("tree listing missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, ".git/") {
		t.Error(".git should be hidden from tree listing")
	}
}

func TestRepoFileResourceReadsBoundedFile(t *testing.T) {
	base := t.TempDir()
	repo := makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	if err := writeFile(filepath.Join(repo, "README.md"), "# hello\n"); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, base)
	_, handler := srv.repoFileResource()
	contents, err := readResource(t, handler, "corral://repo/Public/alpha/file/README.md")
	if err != nil {
		t.Fatal(err)
	}
	text := contents[0].(mcp.TextResourceContents)
	if !strings.Contains(text.Text, "# hello") {
		t.Errorf("missing file body, got: %s", text.Text)
	}
	if text.MIMEType != mimeMarkdown {
		t.Errorf("expected markdown MIME, got %s", text.MIMEType)
	}
}

func TestRepoFileResourceRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")

	srv := newTestServer(t, base)
	_, handler := srv.repoFileResource()
	// Attempt to read /etc/passwd-equivalent via .. traversal. The
	// server must refuse regardless of whether such a path exists.
	_, err := readResource(t, handler, "corral://repo/Public/alpha/file/../../../../../etc/passwd")
	if err == nil {
		t.Error("expected traversal to be rejected")
	}
}

func TestRepoFileResourceMissingPath(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	srv := newTestServer(t, base)
	_, handler := srv.repoFileResource()
	// No file segment after /file/
	_, err := readResource(t, handler, "corral://repo/Public/alpha/file/")
	if err == nil {
		t.Error("expected error on empty path")
	}
}

func TestRepoFileResourceTruncates(t *testing.T) {
	base := t.TempDir()
	repo := makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	// Write a file just over the cap so we exercise the truncation
	// path. 1 MiB + 100 bytes is comfortably > maxFileBytes.
	big := strings.Repeat("A", maxFileBytes+100)
	if err := writeFile(filepath.Join(repo, "big.txt"), big); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, base)
	_, handler := srv.repoFileResource()
	contents, err := readResource(t, handler, "corral://repo/Public/alpha/file/big.txt")
	if err != nil {
		t.Fatal(err)
	}
	text := contents[0].(mcp.TextResourceContents).Text
	if !strings.Contains(text, "truncated at 1 MiB") {
		t.Error("expected truncation notice")
	}
}

func TestResolveURIRepoErrors(t *testing.T) {
	base := t.TempDir()
	srv := newTestServer(t, base)
	cases := []struct {
		name, uri, want string
	}{
		{"unsupported scheme", "http://repo/a/b/state", "unsupported scheme"},
		{"missing segments", "corral://repo/onlyowner", "missing owner/name"},
		{"unknown repo", "corral://repo/Public/ghost/state", "no repository"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := srv.resolveURIRepo(tc.uri)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("uri %q: expected error %q, got %v", tc.uri, tc.want, err)
			}
		})
	}
}

func TestExtractFilePath(t *testing.T) {
	cases := map[string]struct {
		want    string
		wantErr bool
	}{
		"corral://repo/o/n/file/main.go":        {want: "main.go"},
		"corral://repo/o/n/file/sub%2Fmain.go":  {want: "sub/main.go"},
		"corral://repo/o/n/state":               {wantErr: true},
		"corral://repo/o/n/file/":               {wantErr: true},
	}
	for uri, tc := range cases {
		got, err := extractFilePath(uri)
		if tc.wantErr {
			if err == nil {
				t.Errorf("extractFilePath(%q) expected error, got %q", uri, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("extractFilePath(%q) unexpected error %v", uri, err)
		}
		if got != tc.want {
			t.Errorf("extractFilePath(%q) = %q, want %q", uri, got, tc.want)
		}
	}
}

func TestGuessMIME(t *testing.T) {
	cases := map[string]string{
		"foo.md":       mimeMarkdown,
		"foo.markdown": mimeMarkdown,
		"foo.json":     mimeJSON,
		"foo.go":       mimePlain,
		"NOEXT":        mimePlain,
	}
	for in, want := range cases {
		if got := guessMIME(in); got != want {
			t.Errorf("guessMIME(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFirstSegment(t *testing.T) {
	if firstSegment("a/b/c") != "a" {
		t.Error("expected first segment 'a'")
	}
	if firstSegment("solo") != "solo" {
		t.Error("expected solo to return itself")
	}
}

// writeFile mkdir-p's the parent and writes body. Test-only helper.
func writeFile(path, body string) error {
	if err := osMkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return osWriteFile(path, []byte(body), 0o600)
}
