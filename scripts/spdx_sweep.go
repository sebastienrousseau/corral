//go:build ignore

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

// spdx_sweep prepends per-file SPDX copyright + licence headers to every
// Go source file that doesn't already have them. Run with `go run scripts/spdx_sweep.go`.
//
// This is a one-shot sweep, not a CI check. If a future file lands without
// headers, contributors add them by hand or re-run this tool.
//
// The tool is intentionally conservative:
//   - Skips files that already contain "SPDX-License-Identifier".
//   - Preserves any leading `//go:build` / `// +build` constraint block; headers
//     land after that block (with the required blank line) so the constraint
//     stays recognisable to the Go toolchain.
//   - Skips vendor/ and anything under .git/.
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	copyrightLine = "// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>"
	licenceLine   = "// SPDX-License-Identifier: GPL-3.0-only"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	var modified, skipped int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == "vendor" || base == ".git" || base == "dist" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		changed, err := addHeader(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if changed {
			modified++
			fmt.Printf("added: %s\n", path)
		} else {
			skipped++
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nDone. modified=%d skipped=%d\n", modified, skipped)
}

func addHeader(path string) (bool, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if bytes.Contains(src, []byte("SPDX-License-Identifier")) {
		return false, nil
	}

	// Split off any leading build-constraint block. A build-constraint block
	// is one or more `//go:build …` / `// +build …` lines at the very top,
	// optionally followed by a blank line separator.
	lines := strings.Split(string(src), "\n")
	buildEnd := 0
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "//go:build") || strings.HasPrefix(trim, "// +build") {
			buildEnd = i + 1
			continue
		}
		break
	}

	header := copyrightLine + "\n" + licenceLine + "\n\n"

	var out strings.Builder
	if buildEnd > 0 {
		// Preserve the constraint block + its mandatory blank line separator.
		out.WriteString(strings.Join(lines[:buildEnd], "\n"))
		out.WriteString("\n\n")
		// Skip an existing blank separator so we don't accumulate blanks.
		start := buildEnd
		if start < len(lines) && strings.TrimSpace(lines[start]) == "" {
			start++
		}
		out.WriteString(header)
		out.WriteString(strings.Join(lines[start:], "\n"))
	} else {
		out.WriteString(header)
		out.WriteString(string(src))
	}

	return true, os.WriteFile(path, []byte(out.String()), 0o644)
}
