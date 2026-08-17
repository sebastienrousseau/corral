// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
)

// preflightSummary renders the always-printed banner that surfaces the
// two most important arguments — the GitHub owner corral will fetch
// from, and the on-disk directory it will write clones into. Making
// these visible catches the "positional arg typo" class of mistake,
// e.g. `corral i sebastienrousseau` where the user meant a single
// owner but ended up with owner=i and base_dir=sebastienrousseau.
func preflightSummary(owner, baseDir string) string {
	// Resolve the base_dir to an absolute path so a mistyped relative
	// name jumps out. If Abs fails (rare), fall back to the raw input.
	abs := baseDir
	if a, err := filepath.Abs(baseDir); err == nil {
		abs = a
	}
	return fmt.Sprintf("Owner:    %s\nTarget:   %s\n", owner, abs)
}

// preflightConfirm prints the summary banner and, when the target
// directory does not yet exist and stdout is a TTY and neither --yes
// nor --dry-run is set, prompts for confirmation.
//
// It returns (true, nil) to proceed, (false, nil) when the user declined at
// the prompt, and (false, err) when corral refused because it could not ask.
// The distinction drives the exit status: declining is a choice and exits 0,
// while a refusal is a failure and exits non-zero so scripts notice.
//
// The prompt fires only for a genuinely-new target so re-running
// corral against an existing workspace stays frictionless. That's the
// common case (users cron corral daily) and adding a prompt there
// would train them to hit y without reading, defeating the point.
//
// isTerminal / stdin are indirected so tests can drive the prompt
// without a real TTY.
func preflightConfirm(w io.Writer, in io.Reader, isTTY bool, owner, baseDir string, yes, dryRun bool) (bool, error) {
	_, _ = fmt.Fprintln(w, preflightSummary(owner, baseDir))

	if yes || dryRun {
		return true, nil
	}
	if _, err := os.Stat(baseDir); err == nil {
		return true, nil // target already exists; nothing surprising to confirm
	}

	// Creating a new base directory is the one irreversible-feeling step in a
	// run, and it is also what a mistyped positional argument produces. When
	// there is no TTY there is nobody to ask, so refuse rather than proceed:
	// previously this branch returned true, which meant the banner printed the
	// wrong target and the run continued anyway in scripts, pipes, cron and CI
	// — exactly the contexts where nobody is watching.
	//
	// This is reported as refused rather than declined so the exit status is
	// non-zero: a script must be able to tell "I stopped and did nothing"
	// from "I finished".
	if !isTTY {
		return false, fmt.Errorf(
			"refusing to create a new target directory without confirmation\n"+
				"  Target: %s\n"+
				"Stdin is not a terminal, so corral cannot ask. Pass --yes to create it,\n"+
				"or --dry-run to preview without touching disk",
			baseDir)
	}

	_, _ = fmt.Fprintf(w, "The target directory does not exist yet — corral will create it.\n"+
		"Continue? [y/N] ")

	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

// runPreflight is the default entry point cmd/root.go uses. It wraps
// preflightConfirm with the concrete stdout/stdin/isatty implementations
// and applies the assumeYes / dryRun flags from the flag registry.
//
// Split out so the cmd-layer call site is a one-liner and the exit
// path when the user declines is uniform.
var runPreflight = func(owner, baseDir string) (bool, error) {
	return preflightConfirm(
		os.Stderr,
		os.Stdin,
		isatty.IsTerminal(os.Stdin.Fd()),
		owner, baseDir,
		assumeYes, dryRun,
	)
}
