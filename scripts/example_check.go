// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

//go:build ignore

// Command example_check compiles every program under examples/.
//
// The examples carry `//go:build ignore` so that `go build ./...` does not
// try to link four separate main packages into the module build. The side
// effect is that nothing compiled them at all: they could reference a
// function that had been renamed, or a signature that had changed, and no
// gate in this repository would notice. Documentation that does not build is
// worse than no documentation, because a reader assumes it was checked.
//
// This copies each example into a scratch directory with the build
// constraint swapped for one that is satisfied, compiles it against the real
// module, and reports every failure rather than stopping at the first.
//
//	go run scripts/example_check.go
//
// Compiling is deliberately as far as this goes. Running the examples would
// mean cloning repositories and calling the GitHub API, which is not
// something a pull request check should do.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// buildTag is substituted for `ignore` so the copied file builds.
const buildTag = "examplecheck"

func main() {
	examples, err := filepath.Glob(filepath.Join("examples", "*.go"))
	if err != nil {
		fail("listing examples: %v", err)
	}
	if len(examples) == 0 {
		fail("no examples found under examples/ — has the directory moved?")
	}

	// The scratch tree has to live inside the module: the examples import
	// internal/ packages, which the compiler only permits from within the
	// module that declares them.
	scratch, err := os.MkdirTemp(".", ".example-check-")
	if err != nil {
		fail("creating scratch directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(scratch); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove %s: %v\n", scratch, err)
		}
	}()

	var broken []string
	for _, example := range examples {
		name := strings.TrimSuffix(filepath.Base(example), ".go")
		if err := compileExample(scratch, name, example); err != nil {
			broken = append(broken, fmt.Sprintf("%s: %v", example, err))
			fmt.Printf("FAIL  %s\n", example)
			continue
		}
		fmt.Printf("ok    %s\n", example)
	}

	if len(broken) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d example(s) do not compile:\n", len(broken))
		for _, b := range broken {
			fmt.Fprintf(os.Stderr, "\n%s\n", b)
		}
		os.Exit(1)
	}
	fmt.Printf("\nExample check: %d/%d compile\n", len(examples), len(examples))
}

// compileExample copies one example into the scratch tree with a satisfiable
// build constraint and compiles it, discarding the binary.
func compileExample(scratch, name, path string) error {
	source, err := os.ReadFile(path) // #nosec G304 -- path comes from a glob of examples/
	if err != nil {
		return fmt.Errorf("reading: %w", err)
	}
	patched := strings.Replace(string(source), "//go:build ignore", "//go:build "+buildTag, 1)
	if patched == string(source) {
		return fmt.Errorf("no `//go:build ignore` constraint found; either add one or move the file out of examples/")
	}

	dir := filepath.Join(scratch, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(patched), 0o600); err != nil {
		return fmt.Errorf("writing copy: %w", err)
	}

	cmd := exec.Command("go", "build", "-tags", buildTag, "-o", os.DevNull, "./"+filepath.ToSlash(dir)) // #nosec G204 -- fixed executable, path derived from a glob
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// fail reports a fatal setup error.
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
