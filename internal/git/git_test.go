package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
	if err := os.WriteFile(file, []byte("test"), 0o644); err != nil {
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
	defer os.RemoveAll(upstream)
	defer os.RemoveAll(workDir)

	targetDir, err := os.MkdirTemp("", "git_test_target")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(targetDir)
	_ = os.RemoveAll(targetDir)

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
	if err := os.WriteFile(file, []byte("test2"), 0o644); err != nil {
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
