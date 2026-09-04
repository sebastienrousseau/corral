//go:build ignore

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// pkg_check asserts that pkg/ documents every distribution format the
// project actually produces.
//
// pkg/ exists so a packager can find the recipe without reading the
// release pipeline. That only works while it is complete, and a directory
// of prose has no way of noticing that a new format shipped last month —
// which is exactly how this kind of directory rots. So the pipeline is the
// source of truth and this compares against it.
//
// Deliberately one-directional in the strict sense and two-directional in
// the loose one: a format the pipeline builds MUST have a page, and a page
// that names no known format is reported too, because a directory nobody
// can trace back to an artefact is the same rot from the other end.
//
//	go run scripts/pkg_check.go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// format is one distribution format: the directory expected under pkg/,
// and the evidence that the project actually produces it.
type format struct {
	dir string
	// file is where the recipe lives.
	file string
	// marker is a string that must appear in it. Chosen to be the section
	// key rather than a passing mention, so deleting the section fails
	// this check rather than merely renaming a comment.
	marker string
}

var formats = []format{
	{dir: "deb", file: ".goreleaser.yaml", marker: "nfpms:"},
	{dir: "rpm", file: ".goreleaser.yaml", marker: "nfpms:"},
	{dir: "aur", file: ".goreleaser.yaml", marker: "aurs:"},
	{dir: "brew", file: ".goreleaser.yaml", marker: "homebrew_casks:"},
	{dir: "nix", file: "flake.nix", marker: "buildGoModule"},
	{dir: "docker", file: "Dockerfile", marker: "FROM"},
}

func main() {
	var problems []string

	for _, f := range formats {
		b, err := os.ReadFile(f.file)
		if err != nil {
			problems = append(problems, fmt.Sprintf(
				"%s: recipe file %s is missing", f.dir, f.file))
			continue
		}
		if !strings.Contains(string(b), f.marker) {
			// The format is no longer produced. Either the page should go,
			// or this table is stale — both are worth a human deciding.
			problems = append(problems, fmt.Sprintf(
				"%s: %s no longer contains %q, so either the format was dropped "+
					"(remove pkg/%s) or this check is stale",
				f.dir, f.file, f.marker, f.dir))
			continue
		}
		page := filepath.Join("pkg", f.dir, "README.md")
		if _, err := os.Stat(page); err != nil {
			problems = append(problems, fmt.Sprintf(
				"%s is produced by %s but %s does not exist", f.dir, f.file, page))
			continue
		}
		// A page that does not point at its recipe sends the packager back
		// to reading the pipeline, which is what pkg/ exists to avoid.
		content, err := os.ReadFile(page) // #nosec G304 -- path built from the table above
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", page, err))
			continue
		}
		if !strings.Contains(string(content), f.file) {
			problems = append(problems, fmt.Sprintf(
				"%s does not name its recipe (%s)", page, f.file))
		}
	}

	// The other direction: a directory under pkg/ that no format claims.
	known := map[string]bool{}
	for _, f := range formats {
		known[f.dir] = true
	}
	entries, err := os.ReadDir("pkg")
	if err != nil {
		fail([]string{fmt.Sprintf("pkg/ is missing: %v", err)})
	}
	for _, e := range entries {
		if !e.IsDir() || known[e.Name()] {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"pkg/%s is not a format this project produces; add it to scripts/pkg_check.go or remove it",
			e.Name()))
	}

	for _, required := range []string{"pkg/README.md", "pkg/VERIFY.md"} {
		if _, err := os.Stat(required); err != nil {
			problems = append(problems, required+" is missing")
		}
	}

	if len(problems) > 0 {
		fail(problems)
	}
	fmt.Printf("pkg check: %d distribution format(s) documented\n", len(formats))
}

func fail(problems []string) {
	sort.Strings(problems)
	fmt.Fprintln(os.Stderr, "pkg check failed:")
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "  - "+p)
	}
	os.Exit(1)
}
