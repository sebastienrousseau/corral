// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestRepoFileResourceRejectsSiblingRepositoryTraversal(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "", "")
	beta := makeFakeRepo(t, base, "Private", "go", "beta", "", "")
	if err := writeFile(filepath.Join(beta, "secret.txt"), "private"); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, base)
	_, handler := srv.repoFileResource()
	_, err := readResource(t, handler, "corral://repo/Public/alpha/file/../../../Private/go/beta/secret.txt")
	if err == nil {
		t.Fatal("expected traversal into a sibling repository to be rejected")
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
		"corral://repo/o/n/file/main.go":       {want: "main.go"},
		"corral://repo/o/n/file/sub%2Fmain.go": {want: "sub/main.go"},
		"corral://repo/o/n/state":              {wantErr: true},
		"corral://repo/o/n/file/":              {wantErr: true},
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

// TestResolveURIRepoNestedNamespace covers self-hosted GitLab/Gitea
// layouts where the origin URL has multiple namespace segments before
// the repository name. Any segment must be a valid {owner} in the
// corral:// URI, not just the direct parent — otherwise agents that
// know the top-level group can't resolve a repo whose direct parent is
// a nested subgroup.
func TestResolveURIRepoNestedNamespace(t *testing.T) {
	base := t.TempDir()
	// HTTPS with three levels of namespace before the repo.
	makeFakeRepo(t, base, "Public", "go", "widget", "https://git.example.com/parent/subgroup/team/widget.git", "")
	// SSH with two levels of namespace before the repo.
	makeFakeRepo(t, base, "Public", "rust", "gizmo", "git@gitea.corp:sso/team-b/gizmo.git", "")

	srv := newTestServer(t, base)

	// Every namespace segment must resolve. `parent`, `subgroup`, and
	// `team` are all valid owners for the HTTPS clone.
	for _, owner := range []string{"parent", "subgroup", "team"} {
		got, err := srv.resolveURIRepo("corral://repo/" + owner + "/widget/state")
		if err != nil {
			t.Errorf("owner=%q: unexpected error: %v", owner, err)
			continue
		}
		if got.Name != "widget" {
			t.Errorf("owner=%q: expected widget, got %s", owner, got.Name)
		}
	}
	for _, owner := range []string{"sso", "team-b"} {
		got, err := srv.resolveURIRepo("corral://repo/" + owner + "/gizmo/state")
		if err != nil {
			t.Errorf("owner=%q: unexpected error: %v", owner, err)
			continue
		}
		if got.Name != "gizmo" {
			t.Errorf("owner=%q: expected gizmo, got %s", owner, got.Name)
		}
	}

	// A wrong owner still misses cleanly.
	if _, err := srv.resolveURIRepo("corral://repo/ghost/widget/state"); err == nil {
		t.Error("expected miss for owner=ghost")
	}
}

// TestParseOwnerFromURLReturnsSegments locks in the []string API of
// parseOwnerFromURL. Returning every namespace segment is the fix that
// makes nested-group URLs resolve; regressing to a single-segment
// return would silently break TestResolveURIRepoNestedNamespace.
func TestParseOwnerFromURLReturnsSegments(t *testing.T) {
	cases := map[string][]string{
		"":                           nil,
		"https://github.com/o/r.git": {"o"},
		"https://gitlab.com/g/sub/r": {"g", "sub"},
		"https://git.example.com/parent/sub/team/r.git": {"parent", "sub", "team"},
		"git@github.com:o/r.git":                        {"o"},
		"git@gitea.corp:group/subgroup/r":               {"group", "subgroup"},
		"malformed":                                     nil,
	}
	for in, want := range cases {
		got := parseOwnerFromURL(in)
		if len(got) != len(want) {
			t.Errorf("parseOwnerFromURL(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("parseOwnerFromURL(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}

func TestResolveURIRepoWithOwner(t *testing.T) {
	base := t.TempDir()
	makeFakeRepo(t, base, "Public", "go", "alpha", "https://github.com/sebastienrousseau/alpha.git", "")
	makeFakeRepo(t, base, "Private", "rust", "beta", "git@github.com:openclaw/beta.git", "")

	srv := newTestServer(t, base)

	// Test HTTPS owner resolution
	repo, err := srv.resolveURIRepo("corral://repo/sebastienrousseau/alpha/state")
	if err != nil {
		t.Fatalf("expected to resolve alpha via HTTPS owner, got: %v", err)
	}
	if repo.Name != "alpha" {
		t.Errorf("expected alpha, got %s", repo.Name)
	}

	// Test SSH owner resolution
	repo, err = srv.resolveURIRepo("corral://repo/openclaw/beta/state")
	if err != nil {
		t.Fatalf("expected to resolve beta via SSH owner, got: %v", err)
	}
	if repo.Name != "beta" {
		t.Errorf("expected beta, got %s", repo.Name)
	}
}

// readResourceViaRouter issues a real resources/read JSON-RPC request through
// mcp-go's router, so URI-template matching is exercised.
//
// Every other resource test in this package calls the handler function
// directly with a hand-built ReadResourceRequest, which bypasses template
// matching entirely — that is precisely why the {path} routing bug below
// survived: the handler was always correct, the route never matched.
func readResourceViaRouter(t *testing.T, srv *Server, uri string) (string, error) {
	t.Helper()
	req := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":%q}}`, uri)
	resp := srv.mcp.HandleMessage(context.Background(), json.RawMessage(req))
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result *struct {
			Contents []struct {
				Text string `json:"text"`
			} `json:"contents"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal response: %v (%s)", err, raw)
	}
	if envelope.Error != nil {
		return "", fmt.Errorf("%s", envelope.Error.Message)
	}
	if envelope.Result == nil || len(envelope.Result.Contents) == 0 {
		return "", fmt.Errorf("empty result: %s", raw)
	}
	return envelope.Result.Contents[0].Text, nil
}

// TestRepoFileResourceReadsNestedPaths is the regression test for the routing
// bug: the template used RFC 6570 simple expansion ({path}), which does not
// match "/", so no file below a repository's top level was reachable at all.
func TestRepoFileResourceReadsNestedPaths(t *testing.T) {
	base := t.TempDir()
	repo := makeFakeRepo(t, base, "Public", "go", "alpha", "https://github.com/o/alpha.git", "")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("top level\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "src", "deep"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "deep", "x.txt"), []byte("deeper\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, base)

	for _, tc := range []struct{ path, want string }{
		{"README.md", "top level"},
		{"src/main.go", "package main"},
		{"src/deep/x.txt", "deeper"},
	} {
		got, err := readResourceViaRouter(t, srv, "corral://repo/Public/alpha/file/"+tc.path)
		if err != nil {
			t.Errorf("%s: %v", tc.path, err)
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want it to contain %q", tc.path, got, tc.want)
		}
	}
}

// TestRepoFileResourceRefusesCredentialFiles is the other half of the same
// change. Fixing the routing above without this would have converted an
// unreachable resource into a credential leak: .git/config is inside the
// repository, so the path sandbox permits it by design, and it contains the
// token of anyone who cloned over HTTPS with credentials in the URL.
func TestRepoFileResourceRefusesCredentialFiles(t *testing.T) {
	base := t.TempDir()
	repo := makeFakeRepo(t, base, "Public", "go", "alpha", "https://github.com/o/alpha.git", "")

	// A realistic leaked-token config, plus the other usual suspects.
	if err := os.WriteFile(filepath.Join(repo, ".git", "config"),
		[]byte("[remote \"origin\"]\n\turl = https://ghp_SECRETTOKEN@github.com/o/alpha.git\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seed := map[string]string{
		".env":                            "AWS_SECRET_ACCESS_KEY=hunter2",
		".npmrc":                          "//registry.npmjs.org/:_authToken=npm_secret",
		"deploy.pem":                      "-----BEGIN PRIVATE KEY-----",
		"id_ed25519":                      "-----BEGIN OPENSSH PRIVATE KEY-----",
		".env.production":                 "STRIPE_KEY=sk_live_x",
		filepath.Join("k", "secrets.yml"): "password: p",
	}
	for rel, body := range seed {
		full := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	srv := newTestServer(t, base)

	blocked := []string{
		".git/config",
		".git/../.git/config",
		".env",
		".env.production",
		".npmrc",
		"deploy.pem",
		"id_ed25519",
		"k/secrets.yml",
	}
	for _, rel := range blocked {
		got, err := readResourceViaRouter(t, srv, "corral://repo/Public/alpha/file/"+rel)
		if err == nil {
			t.Errorf("%s: expected refusal, got %d bytes: %q", rel, len(got), got)
			continue
		}
		// The refusal must not itself echo the secret.
		if strings.Contains(err.Error(), "ghp_SECRETTOKEN") ||
			strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%s: refusal leaked the secret: %v", rel, err)
		}
	}

	// An ordinary file in the same repository still reads, so the denylist is
	// not just blocking everything.
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readResourceViaRouter(t, srv, "corral://repo/Public/alpha/file/main.go"); err != nil {
		t.Errorf("ordinary file must remain readable: %v", err)
	} else if !strings.Contains(got, "package main") {
		t.Errorf("unexpected content: %q", got)
	}
}

func TestBlockedRepoFile(t *testing.T) {
	blocked := []string{
		".git/config", ".git/HEAD", "sub/.git/config", ".GIT/config",
		".env", ".env.local", ".ENV", ".envrc", ".netrc", "_netrc",
		".npmrc", ".pypirc", ".git-credentials", "credentials",
		"id_rsa", "id_ed25519", ".dockercfg", "terraform.tfstate",
		"secrets.yml", "secrets.yaml", "service-account.json",
		"a/b/tls.pem", "server.key", "store.p12", "store.jks", "vault.kdbx",
		".ssh/known_hosts", ".aws/credentials", ".gnupg/secring.gpg",
		"", ".",
	}
	for _, rel := range blocked {
		if reason, ok := blockedRepoFile(rel); !ok {
			t.Errorf("%q should be blocked", rel)
		} else if reason == "" {
			t.Errorf("%q blocked without a reason", rel)
		}
	}

	allowed := []string{
		"README.md", "main.go", "src/deep/x.txt", "Makefile",
		"docs/environment.md", // contains "environment" but is not .env
		"keyboard.go",         // ends in neither .key nor a blocked name
		"pemberton.txt",
		".github/workflows/ci.yml",
		"credentials.md", // documentation about credentials, not a store
	}
	for _, rel := range allowed {
		if _, ok := blockedRepoFile(rel); ok {
			t.Errorf("%q should be allowed", rel)
		}
	}
}
