package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "git_test_upstream")
	if err != nil {
		t.Fatal(err)
	}

	exec.Command("git", "-C", dir, "init", "--initial-branch=main").Run()
	exec.Command("git", "-C", dir, "config", "user.name", "Test").Run()
	exec.Command("git", "-C", dir, "config", "user.email", "test@test.com").Run()

	file := filepath.Join(dir, "test.txt")
	os.WriteFile(file, []byte("test"), 0644)
	exec.Command("git", "-C", dir, "add", "test.txt").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "init").Run()

	return dir
}

func TestGitCommands(t *testing.T) {
	upstream := setupTestRepo(t)
	defer os.RemoveAll(upstream)

	targetDir, err := os.MkdirTemp("", "git_test_target")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(targetDir)
	os.RemoveAll(targetDir) // Clone needs it to not exist

	// Test successful Clone
	err = Clone(upstream, targetDir, true)
	if err != nil {
		t.Errorf("Failed to clone: %v", err)
	}

	// Test successful CurrentBranch
	branch, err := CurrentBranch(targetDir)
	if err != nil || branch != "main" {
		t.Errorf("Expected branch main, got %s (err: %v)", branch, err)
	}

	// Test successful RemoteOrigin
	remote, err := RemoteOrigin(targetDir)
	if err != nil || remote != upstream {
		t.Errorf("Expected remote %s, got %s (err: %v)", upstream, remote, err)
	}

	// Modify upstream to test Pull
	file := filepath.Join(upstream, "test2.txt")
	os.WriteFile(file, []byte("test2"), 0644)
	exec.Command("git", "-C", upstream, "add", "test2.txt").Run()
	exec.Command("git", "-C", upstream, "commit", "-m", "add test2").Run()

	// Test successful Pull
	err = Pull(targetDir, true)
	if err != nil {
		t.Errorf("Failed to pull: %v", err)
	}

	// Test failed paths
	err = Clone("invalid_url_that_does_not_exist", "/invalid/target/dir", false)
	if err == nil {
		t.Errorf("Expected clone to fail")
	}

	err = Pull("/invalid/target/dir", false)
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
