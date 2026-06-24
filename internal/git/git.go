// Package git provides helper functions to execute common Git commands
// by wrapping the system's git binary using os/exec.
package git

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// TokenProvider, when set, returns a GitHub token used to authenticate HTTPS
// operations against github.com so private repositories can be cloned and
// pulled non-interactively. The token is supplied to git via GIT_CONFIG_*
// environment variables (an http extraheader scoped to https://github.com/), so
// it is never written to a repository's .git/config or exposed in the process
// argument list.
var TokenProvider func() string

// authEnv returns the environment variables that inject an Authorization header
// for github.com HTTPS requests, or nil when no token is available. The header
// is scoped to https://github.com/, so it is harmless for SSH remotes.
func authEnv() []string {
	if TokenProvider == nil {
		return nil
	}
	tok := TokenProvider()
	if tok == "" {
		return nil
	}
	cred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + tok))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic " + cred,
	}
}

// withAuth attaches the github.com Authorization header environment to cmd when
// a token is available, preserving the inherited environment otherwise.
func withAuth(cmd *exec.Cmd) {
	if env := authEnv(); env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
}

// CloneOptions configures optional clone-time performance and layout flags.
type CloneOptions struct {
	// RecurseSubmodules, when true, clones submodules recursively by adding
	// the --recurse-submodules flag.
	RecurseSubmodules bool
	// SingleBranch, when true, clones only the history of the default branch
	// by adding the --single-branch flag.
	SingleBranch bool
	// Blobless, when true, performs a blobless partial clone by adding the
	// --filter=blob:none flag, deferring blob downloads until needed.
	Blobless bool
	// Depth, when greater than zero, creates a shallow clone truncated to the
	// given number of commits by adding the --depth flag.
	Depth int
}

// Clone executes a git clone command for the given URL into the target directory.
func Clone(ctx context.Context, url, targetDir string, opts CloneOptions) error {
	args := []string{"clone"}
	if opts.RecurseSubmodules {
		args = append(args, "--recurse-submodules")
	}
	if opts.SingleBranch {
		args = append(args, "--single-branch")
	}
	if opts.Blobless {
		args = append(args, "--filter=blob:none")
	}
	if opts.Depth > 0 {
		args = append(args, "--depth", strconv.Itoa(opts.Depth))
	}
	// The "--" terminator prevents a URL or path beginning with "-" from being
	// interpreted as a git option.
	args = append(args, "--", url, targetDir)
	// #nosec G204 -- the executable is the fixed "git" binary and all arguments
	// are constructed internally from controlled options, not shell input.
	cmd := exec.CommandContext(ctx, "git", args...)
	withAuth(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Pull executes a git pull --rebase --autostash command in the target directory.
// If recurseSubmodules is true, it appends the --recurse-submodules flag.
//
// Signature verification is explicitly disabled for the merge/rebase so an
// unattended sync never aborts on unsigned or untrusted commits when the user
// has merge.verifySignatures / rebase.verifySignatures enabled globally. This
// overrides those settings for these invocations only and does not change the
// user's configuration.
func Pull(ctx context.Context, targetDir string, recurseSubmodules bool) error {
	args := []string{
		"-c", "merge.verifySignatures=false",
		"-c", "rebase.verifySignatures=false",
		"-C", targetDir, "pull", "--rebase", "--autostash",
	}
	if recurseSubmodules {
		args = append(args, "--recurse-submodules")
	}
	// #nosec G204 -- the executable is the fixed "git" binary and all arguments
	// are constructed internally from controlled options, not shell input.
	cmd := exec.CommandContext(ctx, "git", args...)
	withAuth(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CurrentBranch retrieves the name of the currently checked-out branch.
func CurrentBranch(targetDir string) (string, error) {
	// #nosec G204 -- fixed "git" binary; targetDir is a local path, not shell input.
	cmd := exec.Command("git", "-C", targetDir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// RemoteOrigin retrieves the remote origin URL of the target directory.
func RemoteOrigin(targetDir string) (string, error) {
	// #nosec G204 -- fixed "git" binary; targetDir is a local path, not shell input.
	cmd := exec.Command("git", "-C", targetDir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
