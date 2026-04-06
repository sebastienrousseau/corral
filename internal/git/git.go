// Package git provides helper functions to execute common Git commands
// by wrapping the system's git binary using os/exec.
package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// Clone executes a git clone command for the given URL into the target directory.
// If recurseSubmodules is true, it appends the --recurse-submodules flag.
func Clone(url, targetDir string, recurseSubmodules bool) error {
	args := []string{"clone"}
	if recurseSubmodules {
		args = append(args, "--recurse-submodules")
	}
	args = append(args, url, targetDir)
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", out)
	}
	return nil
}

// Pull executes a git pull --rebase --autostash command in the target directory.
// If recurseSubmodules is true, it appends the --recurse-submodules flag.
func Pull(targetDir string, recurseSubmodules bool) error {
	args := []string{"-C", targetDir, "pull", "--rebase", "--autostash"}
	if recurseSubmodules {
		args = append(args, "--recurse-submodules")
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", out)
	}
	return nil
}

// CurrentBranch retrieves the name of the currently checked-out branch.
func CurrentBranch(targetDir string) (string, error) {
	cmd := exec.Command("git", "-C", targetDir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// RemoteOrigin retrieves the remote origin URL of the target directory.
func RemoteOrigin(targetDir string) (string, error) {
	cmd := exec.Command("git", "-C", targetDir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
