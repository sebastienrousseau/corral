// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

//go:build ignore

// Command manifest_check verifies that the repository's hand-maintained
// manifests still describe the repository.
//
// Two things here have drifted before, in both cases silently and in both
// cases into something a reader would reasonably trust:
//
//   - SBOM.md listed `go-github/v74` for six releases after go.mod moved to
//     v90, and omitted three direct dependencies entirely — while SECURITY.md
//     linked to it as the *full* bill of materials.
//   - server.json's version sat at 0.0.13 through five releases, so the MCP
//     registry advertised a stale OCI image tag.
//
// Neither is the kind of mistake review catches, because neither file is
// where the change happens. So they are checked mechanically instead:
//
//	go run scripts/manifest_check.go
//
// Checks performed:
//
//  1. SBOM.md's dependency table matches go.mod's direct requirements
//     exactly — same module set, same versions, in both directions.
//  2. server.json's version matches the newest release in CHANGELOG.md, and
//     its OCI image tag matches that same version.
//  3. No prose file quotes a base image that disagrees with the Dockerfile.
//
// Exits non-zero, listing every mismatch, if any check fails.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// directRequire matches one non-indirect line inside go.mod's require block.
var directRequire = regexp.MustCompile(`^\s+(\S+)\s+(v\S+)\s*$`)

// sbomRow matches one dependency row of SBOM.md's table, whose first two
// cells are the backticked module path and its version.
var sbomRow = regexp.MustCompile("^\\|\\s*`([^`]+)`\\s*\\|\\s*(v\\S+)\\s*\\|")

// changelogHeading matches a released version heading in CHANGELOG.md.
var changelogHeading = regexp.MustCompile(`^## \[(\d+\.\d+\.\d+)\]`)

func main() {
	var problems []string
	problems = append(problems, checkSBOM()...)
	problems = append(problems, checkServerManifest()...)
	problems = append(problems, checkBaseImage()...)

	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "manifest check failed:")
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "  - %s\n", p)
		}
		os.Exit(1)
	}
	fmt.Println("Manifest check: SBOM.md, server.json and the prose docs agree with go.mod, CHANGELOG.md and the Dockerfile")
}

// checkSBOM compares SBOM.md's table with go.mod's direct requirements.
func checkSBOM() []string {
	goMod, err := parseDirectRequires("go.mod")
	if err != nil {
		return []string{err.Error()}
	}
	sbom, err := parseSBOMTable("SBOM.md")
	if err != nil {
		return []string{err.Error()}
	}

	var problems []string
	for _, mod := range sortedKeys(goMod) {
		version, listed := sbom[mod]
		switch {
		case !listed:
			problems = append(problems, fmt.Sprintf("SBOM.md is missing %s %s", mod, goMod[mod]))
		case version != goMod[mod]:
			problems = append(problems, fmt.Sprintf("SBOM.md lists %s %s, go.mod requires %s", mod, version, goMod[mod]))
		}
	}
	for _, mod := range sortedKeys(sbom) {
		if _, required := goMod[mod]; !required {
			problems = append(problems, fmt.Sprintf("SBOM.md lists %s, which is not a direct requirement", mod))
		}
	}
	return problems
}

// checkServerManifest compares server.json's version with the newest
// CHANGELOG.md release, and with its own OCI image tag.
func checkServerManifest() []string {
	raw, err := os.ReadFile("server.json")
	if err != nil {
		return []string{fmt.Sprintf("reading server.json: %v", err)}
	}
	var manifest struct {
		Version  string `json:"version"`
		Packages []struct {
			Identifier string `json:"identifier"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return []string{fmt.Sprintf("parsing server.json: %v", err)}
	}

	released, err := latestChangelogVersion("CHANGELOG.md")
	if err != nil {
		return []string{err.Error()}
	}

	var problems []string
	if manifest.Version != released {
		problems = append(problems, fmt.Sprintf(
			"server.json version is %s, newest CHANGELOG.md release is %s", manifest.Version, released))
	}
	for _, pkg := range manifest.Packages {
		_, tag, found := strings.Cut(pkg.Identifier, ":")
		if !found {
			problems = append(problems, fmt.Sprintf("server.json package %q has no image tag", pkg.Identifier))
			continue
		}
		if tag != manifest.Version {
			problems = append(problems, fmt.Sprintf(
				"server.json image tag is %s, its version field is %s", tag, manifest.Version))
		}
	}
	return problems
}

// dockerFrom captures the image reference on the Dockerfile's FROM line.
var dockerFrom = regexp.MustCompile(`(?m)^FROM\s+(\S+)`)

// quotedImage finds a name:tag image reference inside prose, with or without
// a trailing digest.
var quotedImage = regexp.MustCompile(`\b([a-z][a-z0-9._-]*):(\d+\.\d+(?:\.\d+)?)(@sha256:[0-9a-f]+)?`)

// proseFiles are the documents that make claims about the build, and so can
// contradict it.
var proseFiles = []string{"README.md", "SECURITY.md", "SBOM.md",
	"docs/security-model.md", "docs/osps-baseline-fillable.md"}

// checkBaseImage reports prose that names a base image version other than the
// one the Dockerfile actually uses.
//
// docs/osps-baseline-fillable.md quoted "alpine:3.20@sha256:d9e853…" as an
// OSPS attestation. When Dependabot bumped the base to 3.24 the attestation
// became false, silently, in a file nobody edits during a dependency bump —
// the same shape of drift that put go-github v74 in SBOM.md for six releases.
// The prose no longer names a version; this makes sure it stays that way, or
// that any version it does name is the right one.
func checkBaseImage() []string {
	body, err := os.ReadFile("Dockerfile")
	if err != nil {
		return []string{fmt.Sprintf("reading Dockerfile: %v", err)}
	}
	m := dockerFrom.FindSubmatch(body)
	if m == nil {
		return []string{"Dockerfile: no FROM line found"}
	}
	actual := string(m[1])
	name, tag, _ := strings.Cut(strings.SplitN(actual, "@", 2)[0], ":")

	var problems []string
	for _, file := range proseFiles {
		text, err := os.ReadFile(file) // #nosec G304 -- fixed list of repository documents
		if err != nil {
			problems = append(problems, fmt.Sprintf("reading %s: %v", file, err))
			continue
		}
		seen := map[string]bool{}
		for _, hit := range quotedImage.FindAllStringSubmatch(string(text), -1) {
			if hit[1] != name || hit[2] == tag || seen[hit[0]] {
				continue
			}
			seen[hit[0]] = true
			problems = append(problems, fmt.Sprintf(
				"%s names %s:%s, the Dockerfile builds on %s", file, hit[1], hit[2], actual))
		}
	}
	return problems
}

// parseDirectRequires returns the module-to-version map of every
// non-indirect requirement in a go.mod file.
func parseDirectRequires(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	requires := make(map[string]string)
	inBlock := false
	for _, line := range strings.Split(string(body), "\n") {
		switch {
		case strings.HasPrefix(line, "require ("):
			inBlock = true
		case inBlock && strings.HasPrefix(line, ")"):
			inBlock = false
		case inBlock && !strings.Contains(line, "// indirect"):
			if m := directRequire.FindStringSubmatch(line); m != nil {
				requires[m[1]] = m[2]
			}
		}
	}
	if len(requires) == 0 {
		return nil, fmt.Errorf("%s: found no direct requirements", path)
	}
	return requires, nil
}

// parseSBOMTable returns the module-to-version map of SBOM.md's dependency
// table. Rows whose version cell is absent are documentation, not entries.
func parseSBOMTable(path string) (map[string]string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	listed := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		if m := sbomRow.FindStringSubmatch(line); m != nil {
			listed[m[1]] = m[2]
		}
	}
	if len(listed) == 0 {
		return nil, fmt.Errorf("%s: found no dependency rows", path)
	}
	return listed, nil
}

// latestChangelogVersion returns the newest released version heading in a
// Keep a Changelog file, skipping the Unreleased section.
func latestChangelogVersion(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		if m := changelogHeading.FindStringSubmatch(line); m != nil {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("%s: found no released version heading", path)
}

// sortedKeys returns a map's keys in a stable order so output is
// deterministic across runs.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
