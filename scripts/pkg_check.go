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
	"regexp"
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
	problems = append(problems, checkVerifyDoc()...)

	if len(problems) > 0 {
		fail(problems)
	}
	fmt.Printf("pkg check: %d distribution format(s) documented\n", len(formats))
}

// checkVerifyDoc verifies that pkg/VERIFY.md names the signature artefact
// the release pipeline actually produces.
//
// v0.0.29 shipped a VERIFY.md telling packagers to pass
// `--certificate checksums.txt.pem --signature checksums.txt.sig` to
// cosign. goreleaser has never produced those: its `signs:` block writes
// a sigstore bundle. Anyone following the document got "no such file",
// and nothing in the pipeline noticed, because a document that is wrong
// still builds.
//
// The filename lives in one place — the `signature:` template in
// .goreleaser.yaml — so that is the source of truth and this compares
// against it.
func checkVerifyDoc() []string {
	cfg, err := os.ReadFile(".goreleaser.yaml")
	if err != nil {
		return []string{fmt.Sprintf("reading .goreleaser.yaml: %v", err)}
	}
	doc, err := os.ReadFile("pkg/VERIFY.md")
	if err != nil {
		return []string{fmt.Sprintf("reading pkg/VERIFY.md: %v", err)}
	}

	m := signatureTemplate.FindSubmatch(cfg)
	if m == nil {
		return []string{".goreleaser.yaml has no `signature:` template under `signs:`, " +
			"so pkg/VERIFY.md cannot be checked against it"}
	}
	// "${artifact}.sigstore.json" -> ".sigstore.json", the part a reader
	// has to type. The artifact name itself varies per release.
	suffix := strings.TrimPrefix(string(m[1]), "${artifact}")

	var problems []string
	if !strings.Contains(string(doc), suffix) {
		problems = append(problems, fmt.Sprintf(
			"pkg/VERIFY.md does not mention %q, which is the signature artefact "+
				".goreleaser.yaml produces — the verification steps cannot work as written",
			suffix))
	}
	// The shapes goreleaser does not produce, which a reader would spend
	// real time on before discovering they do not exist.
	for _, stale := range []string{"checksums.txt.pem", "checksums.txt.sig`", "checksums.txt.sig "} {
		if strings.Contains(string(doc), stale) {
			problems = append(problems, fmt.Sprintf(
				"pkg/VERIFY.md refers to %q, which the release does not publish", strings.TrimSpace(stale)))
		}
	}
	return problems
}

// signatureTemplate matches goreleaser's blob-signature filename template.
var signatureTemplate = regexp.MustCompile(`(?m)^\s*signature:\s*'?([^'\n]+)'?`)

func fail(problems []string) {
	sort.Strings(problems)
	fmt.Fprintln(os.Stderr, "pkg check failed:")
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "  - "+p)
	}
	os.Exit(1)
}
