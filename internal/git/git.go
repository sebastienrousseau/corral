package git

import (
	"fmt"
	"os/exec"
	"strings"
)

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

func CurrentBranch(targetDir string) (string, error) {
	cmd := exec.Command("git", "-C", targetDir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func RemoteOrigin(targetDir string) (string, error) {
	cmd := exec.Command("git", "-C", targetDir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
