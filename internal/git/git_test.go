package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// cleanup removes a temporary directory and reports a failure if removal
// errors, satisfying errcheck for deferred cleanup.
func cleanup(t *testing.T, dir string) {
	t.Helper()
	if err := os.RemoveAll(dir); err != nil {
		t.Errorf("failed to remove %s: %v", dir, err)
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v (%s)", name, args, err, string(out))
	}
}

func setupTestRepo(t *testing.T) (bareDir string, workDir string) {
	t.Helper()

	bareDir, err := os.MkdirTemp("", "git_test_upstream_bare")
	if err != nil {
		t.Fatal(err)
	}
	run(t, "git", "-C", bareDir, "init", "--bare")

	workDir, err = os.MkdirTemp("", "git_test_upstream_work")
	if err != nil {
		t.Fatal(err)
	}
	run(t, "git", "-C", workDir, "init")
	run(t, "git", "-C", workDir, "config", "user.name", "Test")
	run(t, "git", "-C", workDir, "config", "user.email", "test@test.com")

	file := filepath.Join(workDir, "test.txt")
	if err := os.WriteFile(file, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, "git", "-C", workDir, "add", "test.txt")
	run(t, "git", "-C", workDir, "commit", "-m", "init")
	run(t, "git", "-C", workDir, "branch", "-M", "main")
	run(t, "git", "-C", workDir, "remote", "add", "origin", bareDir)
	run(t, "git", "-C", workDir, "push", "-u", "origin", "main")
	run(t, "git", "-C", bareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	return bareDir, workDir
}

func TestGitCommands(t *testing.T) {
	upstream, workDir := setupTestRepo(t)
	defer cleanup(t, upstream)
	defer cleanup(t, workDir)

	targetDir, err := os.MkdirTemp("", "git_test_target")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup(t, targetDir)
	if err := os.RemoveAll(targetDir); err != nil {
		t.Fatal(err)
	}

	err = Clone(context.Background(), upstream, targetDir, CloneOptions{RecurseSubmodules: true})
	if err != nil {
		t.Errorf("Failed to clone: %v", err)
	}

	branch, err := CurrentBranch(targetDir)
	if err != nil || branch == "" {
		t.Errorf("Expected non-empty branch, got %q (err: %v)", branch, err)
	}

	remote, err := RemoteOrigin(targetDir)
	if err != nil || remote != upstream {
		t.Errorf("Expected remote %s, got %s (err: %v)", upstream, remote, err)
	}
	run(t, "git", "-C", targetDir, "config", "merge.verifySignatures", "false")

	file := filepath.Join(workDir, "test2.txt")
	if err := os.WriteFile(file, []byte("test2"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, "git", "-C", workDir, "add", "test2.txt")
	run(t, "git", "-C", workDir, "commit", "-m", "add test2")
	run(t, "git", "-C", workDir, "push", "origin", "main")

	err = Pull(context.Background(), targetDir, true)
	if err != nil {
		t.Errorf("Failed to pull: %v", err)
	}

	err = Clone(context.Background(), "invalid_url_that_does_not_exist", "/invalid/target/dir", CloneOptions{})
	if err == nil {
		t.Errorf("Expected clone to fail")
	}

	err = Pull(context.Background(), "/invalid/target/dir", false)
	if err == nil {
		t.Errorf("Expected pull to fail")
	}

	_, err = CurrentBranch("/invalid/target/dir")
	if err == nil {
		t.Errorf("Expected current branch to fail")
	}

	_, err = RemoteOrigin("/invalid/target/dir")
	if err == nil {
		t.Errorf("Expected remote origin to fail")
	}
}

// TestCloneOptions exercises each optional clone flag branch independently by
// cloning from a local upstream repository.
func TestCloneOptions(t *testing.T) {
	upstream, workDir := setupTestRepo(t)
	defer cleanup(t, upstream)
	defer cleanup(t, workDir)

	cases := []struct {
		name string
		opts CloneOptions
	}{
		{"SingleBranch", CloneOptions{SingleBranch: true}},
		{"Blobless", CloneOptions{Blobless: true}},
		{"Depth", CloneOptions{Depth: 1}},
		{"RecurseSubmodules", CloneOptions{RecurseSubmodules: true}},
		{"All", CloneOptions{RecurseSubmodules: true, SingleBranch: true, Blobless: true, Depth: 1}},
		{"None", CloneOptions{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			targetDir, err := os.MkdirTemp("", "git_test_opts_target")
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup(t, targetDir)
			// git clone requires the target to not exist (or be empty).
			if err := os.RemoveAll(targetDir); err != nil {
				t.Fatal(err)
			}

			if err := Clone(context.Background(), upstream, targetDir, tc.opts); err != nil {
				t.Errorf("Clone with %s failed: %v", tc.name, err)
			}
		})
	}
}

func TestPullIgnoresSignatureVerification(t *testing.T) {
	upstream, workDir := setupTestRepo(t)
	defer cleanup(t, upstream)
	defer cleanup(t, workDir)

	targetDir, err := os.MkdirTemp("", "git_test_verify")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup(t, targetDir)
	if err := os.RemoveAll(targetDir); err != nil {
		t.Fatal(err)
	}
	if err := Clone(context.Background(), upstream, targetDir, CloneOptions{}); err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	// Enable signature verification locally. The commits are unsigned, so a
	// plain "git pull --rebase" would abort with a fatal error.
	run(t, "git", "-C", targetDir, "config", "rebase.verifySignatures", "true")
	run(t, "git", "-C", targetDir, "config", "merge.verifySignatures", "true")

	// Create a new unsigned upstream commit to pull.
	file := filepath.Join(workDir, "more.txt")
	if err := os.WriteFile(file, []byte("more"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, "git", "-C", workDir, "add", "more.txt")
	run(t, "git", "-C", workDir, "commit", "-m", "more")
	run(t, "git", "-C", workDir, "push", "origin", "main")

	// Pull must succeed despite verifySignatures=true, because it overrides it.
	if err := Pull(context.Background(), targetDir, false); err != nil {
		t.Fatalf("Pull should ignore signature verification, got: %v", err)
	}
}

func TestAuthEnv(t *testing.T) {
	defer func() { TokenProvider = nil }()

	TokenProvider = nil
	if env := authEnv(); env != nil {
		t.Errorf("expected nil env when TokenProvider is unset, got %v", env)
	}

	TokenProvider = func() string { return "" }
	if env := authEnv(); env != nil {
		t.Errorf("expected nil env for an empty token, got %v", env)
	}

	TokenProvider = func() string { return "secret" }
	env := authEnv()
	if len(env) != 3 || env[0] != "GIT_CONFIG_COUNT=1" {
		t.Fatalf("expected three auth env vars, got %v", env)
	}
}

func envContains(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

func TestWithGitEnvAlwaysSetsNonInteractive(t *testing.T) {
	defer func() { TokenProvider = nil }()

	// No token: still must set the non-interactive guards so cron clones of
	// public repos don't hang on an SSH passphrase or askpass helper.
	TokenProvider = nil
	cmd := exec.Command("git", "version")
	withGitEnv(cmd)
	for _, want := range []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=/bin/true",
		"SSH_ASKPASS=/bin/true",
		"GCM_INTERACTIVE=Never",
	} {
		if !envContains(cmd.Env, want) {
			t.Errorf("expected withGitEnv to set %q, got %v", want, cmd.Env)
		}
	}
	// Inherited environment is preserved (PATH must always be present so
	// children can find their dependencies).
	if !envContains(cmd.Env, "PATH="+os.Getenv("PATH")) {
		t.Errorf("expected PATH to be inherited into cmd.Env")
	}
}

func TestWithGitEnvIncludesAuthHeader(t *testing.T) {
	defer func() { TokenProvider = nil }()

	TokenProvider = func() string { return "secret" }
	cmd := exec.Command("git", "version")
	withGitEnv(cmd)
	if !envContains(cmd.Env, "GIT_CONFIG_COUNT=1") {
		t.Errorf("expected GIT_CONFIG_COUNT=1 when a token is available, got %v", cmd.Env)
	}
	// Non-interactive guards still present alongside the auth header.
	if !envContains(cmd.Env, "GIT_TERMINAL_PROMPT=0") {
		t.Errorf("expected non-interactive guards to coexist with auth env")
	}
}

func TestResolveGitBinarySuccess(t *testing.T) {
	old := lookPath
	oldBinary := gitBinary
	defer func() {
		lookPath = old
		gitBinary = oldBinary
	}()
	lookPath = func(name string) (string, error) {
		if name != "git" {
			t.Errorf("expected lookup for %q, got %q", "git", name)
		}
		return "/usr/local/bin/git", nil
	}
	if err := ResolveGitBinary(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gitBinary != "/usr/local/bin/git" {
		t.Errorf("expected gitBinary to be cached, got %q", gitBinary)
	}
}

func TestResolveGitBinaryMissing(t *testing.T) {
	old := lookPath
	oldBinary := gitBinary
	defer func() {
		lookPath = old
		gitBinary = oldBinary
	}()
	lookPath = func(name string) (string, error) {
		return "", exec.ErrNotFound
	}
	err := ResolveGitBinary()
	if err == nil {
		t.Fatal("expected error when git is missing from PATH")
	}
	if !strings.Contains(err.Error(), "git not found on PATH") {
		t.Errorf("error should explain missing git, got %v", err)
	}
}

func TestRemoteOriginFromConfigHTTPS(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = https://github.com/seb/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`)
	got, err := RemoteOriginFromConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://github.com/seb/repo.git" {
		t.Errorf("got %q", got)
	}
}

func TestRemoteOriginFromConfigSSH(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "[remote \"origin\"]\r\n\turl = git@github.com:seb/repo.git\r\n")
	got, err := RemoteOriginFromConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "git@github.com:seb/repo.git" {
		t.Errorf("expected ssh url, got %q", got)
	}
}

func TestRemoteOriginFromConfigIgnoresOtherSections(t *testing.T) {
	dir := t.TempDir()
	// A different remote section appearing before origin must not leak its
	// url into the result.
	writeConfig(t, dir, `[remote "fork"]
	url = git@github.com:other/fork.git
[remote "origin"]
	url = https://github.com/seb/repo.git
[remote "upstream"]
	url = https://github.com/up/repo.git
`)
	got, err := RemoteOriginFromConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://github.com/seb/repo.git" {
		t.Errorf("got wrong url: %q", got)
	}
}

func TestRemoteOriginFromConfigSkipsCommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `
# top comment
; semicolon comment

[remote "origin"]
	# nested comment
	url = https://github.com/seb/repo.git
`)
	got, err := RemoteOriginFromConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://github.com/seb/repo.git" {
		t.Errorf("got %q", got)
	}
}

func TestRemoteOriginFromConfigMissingFile(t *testing.T) {
	dir := t.TempDir() // no .git/config inside
	_, err := RemoteOriginFromConfig(dir)
	if err == nil {
		t.Fatal("expected error when config is absent")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected wrapped os.ErrNotExist, got %v", err)
	}
}

func TestRemoteOriginFromConfigMissingOriginSection(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "[core]\n\trepositoryformatversion = 0\n")
	_, err := RemoteOriginFromConfig(dir)
	if err == nil {
		t.Fatal("expected error when origin section is missing")
	}
	if !strings.Contains(err.Error(), "origin url not found") {
		t.Errorf("error should mention missing origin url, got %v", err)
	}
}

func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPullArgsContainCommitSigningOverride(t *testing.T) {
	// Pull doesn't expose its args directly, so this verifies the override at
	// runtime against a real clone: configure global-style commit signing on a
	// local repo with no signing key available, then assert Pull still
	// succeeds (it would fail with "gpg failed to sign the data" otherwise).
	upstream, workDir := setupTestRepo(t)
	defer cleanup(t, upstream)
	defer cleanup(t, workDir)

	targetDir, err := os.MkdirTemp("", "git_test_sign")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup(t, targetDir)
	if err := os.RemoveAll(targetDir); err != nil {
		t.Fatal(err)
	}
	if err := Clone(context.Background(), upstream, targetDir, CloneOptions{}); err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	// Force a signing configuration that would normally hang or fail: ssh
	// signing with a nonexistent key. The -c overrides in Pull must disable
	// this so rebase succeeds.
	run(t, "git", "-C", targetDir, "config", "commit.gpgsign", "true")
	run(t, "git", "-C", targetDir, "config", "gpg.format", "ssh")
	run(t, "git", "-C", targetDir, "config", "user.signingkey", "/nonexistent/key")
	run(t, "git", "-C", targetDir, "config", "user.name", "Test")
	run(t, "git", "-C", targetDir, "config", "user.email", "test@test.com")

	// Make an upstream change so rebase actually replays a local commit on
	// pull. First make a local divergent commit so rebase has something to
	// re-apply through the signing path.
	if err := os.WriteFile(filepath.Join(targetDir, "local.txt"), []byte("l"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, "git", "-C", targetDir, "add", "local.txt")
	// The local commit must NOT be signed (we'd hit the bad key); use git
	// directly with explicit overrides to make the test setup deterministic.
	setup := exec.Command("git", "-C", targetDir,
		"-c", "commit.gpgsign=false",
		"-c", "user.name=Test",
		"-c", "user.email=test@test.com",
		"commit", "-m", "local")
	if out, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("setup commit failed: %v (%s)", err, out)
	}

	upstreamFile := filepath.Join(workDir, "upstream.txt")
	if err := os.WriteFile(upstreamFile, []byte("u"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, "git", "-C", workDir, "add", "upstream.txt")
	run(t, "git", "-C", workDir, "commit", "-m", "upstream")
	run(t, "git", "-C", workDir, "push", "origin", "main")

	if err := Pull(context.Background(), targetDir, false); err != nil {
		t.Fatalf("Pull must disable commit.gpgsign during rebase replay, got: %v", err)
	}
}
