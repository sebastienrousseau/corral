//go:build ignore

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// spdx_sweep adds, or verifies the presence of, per-file SPDX copyright and
// licence headers.
//
//	go run scripts/spdx_sweep.go            # add missing headers in place
//	go run scripts/spdx_sweep.go -check     # report and exit 1; change nothing
//
// The -check mode is what CI runs. A licence that is machine-readable only
// on the files someone remembered is not machine-readable: REUSE-style
// compliance is a property of the whole tree, so it needs a gate rather
// than a convention. Twenty-one workflow and config files had drifted
// uncovered before this grew a check mode.
//
// Comment syntax is chosen per extension, and the tool is deliberately
// conservative about what must stay on the first line:
//   - Go: `//`, after any `//go:build` / `// +build` constraint block.
//   - YAML, shell, Python, Dockerfile, Makefile: `#`, after any `#!` shebang.
//   - Files already containing "SPDX-License-Identifier" are left alone.
//   - vendor/, .git/, dist/, build/, node_modules/, docs-site/ and public/
//     are skipped — the first four hold generated output, which carries the
//     licence of whatever generated it rather than its own header.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	copyrightText = "SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>"
	licenceText   = "SPDX-License-Identifier: GPL-3.0-only"
)

// commentPrefix returns the line-comment marker for path, and whether the
// file type is covered at all.
func commentPrefix(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "//", true
	case ".yml", ".yaml", ".sh", ".bash", ".py", ".toml", ".cfg":
		return "#", true
	}
	switch strings.ToLower(filepath.Base(path)) {
	case "makefile", "gnumakefile", "dockerfile":
		return "#", true
	}
	return "", false
}

func skipDir(name string) bool {
	switch name {
	case ".git", "vendor", "dist", "build", "node_modules", "docs-site", "public":
		return true
	}
	return false
}

func main() {
	check := flag.Bool("check", false, "report files missing SPDX headers and exit non-zero; change nothing")
	flag.Parse()

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	var modified, covered int
	var missing []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		prefix, ok := commentPrefix(path)
		if !ok {
			return nil
		}
		src, err := os.ReadFile(path) // #nosec G304 -- walking the repository the tool was pointed at
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if bytes.Contains(src, []byte("SPDX-License-Identifier")) {
			covered++
			return nil
		}
		if *check {
			missing = append(missing, path)
			return nil
		}
		if err := addHeader(path, prefix, src); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		modified++
		fmt.Printf("added: %s\n", path)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	if *check {
		if len(missing) > 0 {
			fmt.Fprintf(os.Stderr, "SPDX check: %d file(s) missing a licence header:\n", len(missing))
			for _, m := range missing {
				fmt.Fprintf(os.Stderr, "  %s\n", m)
			}
			fmt.Fprintln(os.Stderr, "\nRun: go run scripts/spdx_sweep.go")
			os.Exit(1)
		}
		fmt.Printf("SPDX check: all %d covered file(s) carry a licence header\n", covered)
		return
	}
	fmt.Printf("\nDone. modified=%d already-covered=%d\n", modified, covered)
}

// addHeader writes path back with an SPDX header inserted after whatever
// must remain on the first lines: a shebang, or a Go build-constraint block.
func addHeader(path, prefix string, src []byte) error {
	header := prefix + " " + copyrightText + "\n" + prefix + " " + licenceText + "\n\n"

	lines := strings.Split(string(src), "\n")
	keep := 0

	// A shebang must stay on line 1 or the file stops being executable.
	if len(lines) > 0 && strings.HasPrefix(lines[0], "#!") {
		keep = 1
	}
	// A Go build constraint must precede the package clause and be followed
	// by a blank line, so the header goes after it.
	if prefix == "//" {
		for i := keep; i < len(lines); i++ {
			trim := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trim, "//go:build") || strings.HasPrefix(trim, "// +build") {
				keep = i + 1
				continue
			}
			break
		}
	}

	var out strings.Builder
	if keep > 0 {
		out.WriteString(strings.Join(lines[:keep], "\n"))
		out.WriteString("\n\n")
		// Skip an existing blank separator so blanks do not accumulate.
		if keep < len(lines) && strings.TrimSpace(lines[keep]) == "" {
			keep++
		}
		out.WriteString(header)
		out.WriteString(strings.Join(lines[keep:], "\n"))
	} else {
		out.WriteString(header)
		out.WriteString(string(src))
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(out.String()), info.Mode().Perm())
}
