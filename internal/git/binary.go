// Package git: binary resolution.
//
// gitBinary holds the absolute path to the git executable used by every
// helper in this package. It is initialized to "git" so commands still work
// when the consumer chooses not to call ResolveGitBinary (e.g. tests), but
// production callers should invoke ResolveGitBinary once at startup so the
// CLI fails fast with a clear message when git is missing rather than
// surfacing a confusing error mid-run.

package git

import (
	"fmt"
	"os/exec"
)

// lookPath is indirected through a variable so tests can stub the resolver.
var lookPath = exec.LookPath

// gitBinary is the path used by every exec.Command in this package.
var gitBinary = "git"

// ResolveGitBinary looks up the absolute path to the git executable on PATH
// and caches it for the rest of the process. Returns a clear error when git
// is not installed.
func ResolveGitBinary() error {
	p, err := lookPath("git")
	if err != nil {
		return fmt.Errorf("git not found on PATH: %w", err)
	}
	gitBinary = p
	return nil
}
