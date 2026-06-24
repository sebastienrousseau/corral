package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

	cmd := exec.Command("git", "version")
	withAuth(cmd)
	found := false
	for _, e := range cmd.Env {
		if e == "GIT_CONFIG_COUNT=1" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected withAuth to inject GIT_CONFIG env, got %v", cmd.Env)
	}

	TokenProvider = nil
	cmd2 := exec.Command("git", "version")
	withAuth(cmd2)
	if cmd2.Env != nil {
		t.Errorf("expected withAuth to leave Env unset when no token is available")
	}
}
