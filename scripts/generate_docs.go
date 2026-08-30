//go:build ignore

// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/printer"
	"go/token"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DocData struct {
	Packages []PkgDoc
}

type PkgDoc struct {
	Name string
	Path string
	Doc  string
	// Anchor is the slug used for in-page links.
	Anchor string
	// Importable is true when code outside this module may import the
	// package. Everything under internal/ cannot be, and neither can a
	// main package, so printing an import statement for those would be
	// telling the reader to write something that does not compile.
	Importable bool
	// Kind labels why a package is not importable: "internal" or
	// "program". Empty when it is importable.
	Kind string
	// ImportPath is the full module path, set only when Importable.
	ImportPath string
	// SourceURL points at the package directory on GitHub.
	SourceURL string
	Funcs     []FuncDoc
	Types     []TypeDoc
}

type FuncDoc struct {
	Name   string
	Decl   string
	Doc    string
	Anchor string
}

type TypeDoc struct {
	Name    string
	Doc     string
	Decl    string
	Anchor  string
	Methods []FuncDoc
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Corral Package Reference</title>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg: #0f172a;
            --text: #f1f5f9;
            --primary: #f56b5e;
            --border: #1e293b;
            --card-bg: #1e293b;
            --code-bg: #0b0f19;
            --muted: #94a3b8;
        }
        body {
            font-family: 'Inter', sans-serif;
            background: var(--bg);
            color: var(--text);
            margin: 0;
            padding: 0;
            line-height: 1.6;
        }
        .container {
            max-width: 1000px;
            margin: 0 auto;
            padding: 60px 20px;
        }
        header {
            border-bottom: 1px solid var(--border);
            padding-bottom: 30px;
            margin-bottom: 50px;
            text-align: center;
        }
        h1 { color: var(--primary); font-size: 2.8rem; margin: 0 0 10px; font-weight: 700; }
        .subtitle { font-size: 1.2rem; color: var(--muted); }
        .pkg-card {
            background: #111827;
            border: 1px solid var(--border);
            border-radius: 12px;
            padding: 40px;
            margin-bottom: 40px;
            box-shadow: 0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1);
        }
        h2 { font-size: 2rem; border-bottom: 1px solid var(--border); padding-bottom: 10px; margin-top: 0; font-weight: 600; }
        h3 { font-size: 1.4rem; margin-top: 40px; color: var(--primary); border-bottom: 1px solid var(--border); padding-bottom: 5px; }
        h4 { font-size: 1.1rem; margin-top: 25px; margin-bottom: 10px; font-family: 'JetBrains Mono', monospace; color: #f8fafc; }
        h5 { font-size: 1rem; margin-top: 20px; margin-bottom: 5px; color: var(--muted); }
        pre {
            background: var(--code-bg);
            border: 1px solid var(--border);
            border-radius: 8px;
            padding: 18px;
            overflow-x: auto;
            font-family: 'JetBrains Mono', monospace;
            font-size: 0.85rem;
        }
        code {
            font-family: 'JetBrains Mono', monospace;
            background: var(--code-bg);
            padding: 2px 6px;
            border-radius: 4px;
            font-size: 0.85rem;
            color: #38bdf8;
        }
        .method-block {
            margin-left: 20px;
            border-left: 2px solid var(--border);
            padding-left: 15px;
            margin-bottom: 15px;
        }
        .note {
            color: var(--muted);
            font-size: 0.95rem;
            line-height: 1.6;
        }
        .toc {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 10px;
            padding: 20px 28px;
            margin-bottom: 40px;
        }
        .toc ul { list-style: none; padding: 0; margin: 0; }
        .toc li { padding: 4px 0; }
        .toc a { color: var(--primary); text-decoration: none; }
        .toc a:hover { text-decoration: underline; }
        .tag {
            font-size: 0.75rem;
            color: var(--muted);
            border: 1px solid var(--border);
            border-radius: 10px;
            padding: 1px 7px;
            margin-left: 6px;
        }
        .footer {
            text-align: center;
            margin-top: 80px;
            color: var(--muted);
            font-size: 0.95rem;
            border-top: 1px solid var(--border);
            padding-top: 30px;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>Corral Package Reference</h1>
            <div class="subtitle">Exported Go declarations, generated from the source of every package in the module</div>
            <p class="note">Most of Corral lives under <code>internal/</code>, which Go forbids other modules from importing. Those packages are documented here for people working on Corral, not as an API to build against. The interfaces Corral does offer to the outside world are its command line and its MCP server, both documented in the <a href="https://github.com/sebastienrousseau/corral#readme">README</a>.</p>
        </header>

        <nav class="toc">
            <h2>Packages</h2>
            <ul>
            {{range .Packages}}
                <li><a href="#{{.Anchor}}">{{.Path}}</a>{{if .Kind}} <span class="tag">{{.Kind}}</span>{{end}}</li>
            {{end}}
            </ul>
        </nav>
        
        {{range .Packages}}
        <div class="pkg-card" id="{{.Anchor}}">
            <h2>Package {{.Name}}</h2>
            {{if .Importable}}
            <p><code>import "{{.ImportPath}}"</code></p>
            {{else}}
            <p class="note">{{if eq .Kind "program"}}A command, not a library — there is nothing here to import.{{else}}Not importable from outside this module: Go refuses an <code>internal/</code> path across module boundaries.{{end}}
            <a href="{{.SourceURL}}">Read the source</a>.</p>
            {{end}}
            <p>{{.Doc}}</p>

            {{if .Funcs}}
            <h3>Functions</h3>
            {{range .Funcs}}
            <div id="{{.Anchor}}">
                <h4>func {{.Name}}</h4>
                <pre>{{.Decl}}</pre>
                <p>{{.Doc}}</p>
            </div>
            {{end}}
            {{end}}

            {{if .Types}}
            <h3>Types</h3>
            {{range .Types}}
            <div id="{{.Anchor}}">
                <h4>type {{.Name}}</h4>
                <pre>{{.Decl}}</pre>
                <p>{{.Doc}}</p>

                {{if .Methods}}
                <h5>Methods</h5>
                {{range .Methods}}
                <div class="method-block">
                    <h4>func {{.Name}}</h4>
                    <pre>{{.Decl}}</pre>
                    <p>{{.Doc}}</p>
                </div>
                {{end}}
                {{end}}
            </div>
            {{end}}
            {{end}}
        </div>
        {{end}}

        <div class="footer">
            Generated from source by <code>scripts/generate_docs.go</code>.
            Made with ❤️ in London, UK
        </div>
    </div>
</body>
</html>`

func formatFuncDecl(fset *token.FileSet, decl *ast.FuncDecl) string {
	tmp := *decl
	tmp.Body = nil
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, fset, &tmp)
	return buf.String()
}

func formatDecl(fset *token.FileSet, decl ast.Decl) string {
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, fset, decl)
	return buf.String()
}

const (
	modulePath = "github.com/sebastienrousseau/corral"
	sourceRoot = "https://github.com/sebastienrousseau/corral/tree/main"
)

// skipDirs are directories that hold Go files which are not part of the
// module's package set: build-ignored example and tooling programs, plus
// generated output and version control metadata.
var skipDirs = map[string]bool{
	".git": true, "scripts": true, "examples": true,
	"public": true, "testdata": true, "vendor": true, "docs": true,
}

// discoverPackages walks the module for directories holding non-test Go
// files, in sorted order.
//
// This used to be a hardcoded list of five paths, and it went stale the
// moment a package was added: internal/diag shipped in v0.0.26 and never
// appeared on the site, while the documentation-coverage gate counted it.
// The gate checked eight packages and the published site showed five.
// Discovering them removes the possibility rather than the current
// instance.
func discoverPackages() ([]string, error) {
	var paths []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if path != "." && (skipDirs[name] || strings.HasPrefix(name, ".")) {
			return filepath.SkipDir
		}
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return readErr
		}
		for _, e := range entries {
			n := e.Name()
			if !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
				if path != "." {
					paths = append(paths, filepath.ToSlash(path))
				}
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("found no packages to document")
	}
	sort.Strings(paths)
	return paths, nil
}

// importable reports whether code outside this module may import the
// package. Go refuses an internal/ path across module boundaries, and a
// main package is a program rather than a library, so in both cases an
// import statement would not compile for a reader who copied it.
func importable(path, pkgName string) bool {
	if pkgName == "main" {
		return false
	}
	return path != "internal" &&
		!strings.HasPrefix(path, "internal/") &&
		!strings.Contains(path, "/internal/")
}

// anchor builds a stable, URL-safe in-page identifier.
func anchor(scope, name string) string {
	s := strings.ToLower(scope + "-" + name)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

func main() {
	fset := token.NewFileSet()
	var docData DocData

	paths, err := discoverPackages()
	if err != nil {
		fmt.Fprintf(os.Stderr, "discovering packages: %v\n", err)
		os.Exit(1)
	}
	for _, p := range paths {
		pkgs, err := parser.ParseDir(fset, p, func(info os.FileInfo) bool {
			return !strings.HasSuffix(info.Name(), "_test.go")
		}, parser.ParseComments)

		if err != nil {
			fmt.Printf("Error parsing dir %s: %v\n", p, err)
			continue
		}

		for name, pkg := range pkgs {
			// Mode 0, not doc.AllDecls: this is documentation, and an
			// unexported helper is not part of any surface a reader can
			// use. The previous setting published 173 private
			// declarations — three quarters of the page — including
			// several with no doc comment at all, which rendered as
			// empty paragraphs.
			d := doc.New(pkg, p, 0)
			var pkgDoc PkgDoc
			pkgDoc.Name = name
			pkgDoc.Path = p
			pkgDoc.Doc = d.Doc
			pkgDoc.Anchor = anchor("pkg", p)
			pkgDoc.SourceURL = sourceRoot + "/" + p
			pkgDoc.Importable = importable(p, name)
			switch {
			case pkgDoc.Importable:
				pkgDoc.ImportPath = modulePath + "/" + p
			case name == "main":
				pkgDoc.Kind = "program"
			default:
				pkgDoc.Kind = "internal"
			}

			// Functions
			for _, f := range d.Funcs {
				pkgDoc.Funcs = append(pkgDoc.Funcs, FuncDoc{
					Name:   f.Name,
					Decl:   formatFuncDecl(fset, f.Decl),
					Doc:    f.Doc,
					Anchor: anchor(p, f.Name),
				})
			}

			// Types
			for _, t := range d.Types {
				typeDoc := TypeDoc{
					Name:   t.Name,
					Doc:    t.Doc,
					Decl:   formatDecl(fset, t.Decl),
					Anchor: anchor(p, t.Name),
				}
				for _, m := range t.Methods {
					typeDoc.Methods = append(typeDoc.Methods, FuncDoc{
						Name:   m.Name,
						Decl:   formatFuncDecl(fset, m.Decl),
						Doc:    m.Doc,
						Anchor: anchor(p, t.Name+"."+m.Name),
					})
				}
				pkgDoc.Types = append(pkgDoc.Types, typeDoc)
			}

			docData.Packages = append(docData.Packages, pkgDoc)
		}
	}

	if err := os.MkdirAll("public", 0o750); err != nil {
		fmt.Printf("Error creating public dir: %v\n", err)
		os.Exit(1)
	}

	f, err := os.Create(filepath.Join("public", "index.html"))
	if err != nil {
		fmt.Printf("Error creating index.html: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	tmpl, err := template.New("docs").Parse(htmlTemplate)
	if err != nil {
		fmt.Printf("Error parsing template: %v\n", err)
		os.Exit(1)
	}

	err = tmpl.Execute(f, docData)
	if err != nil {
		fmt.Printf("Error executing template: %v\n", err)
		os.Exit(1)
	}

	// A count, not a cheer. The previous version reported success while
	// publishing five of eight packages and 173 unexported helpers, so the
	// message says what was produced and lets the reader judge it.
	exported := 0
	for _, pkg := range docData.Packages {
		exported += len(pkg.Funcs)
		for _, t := range pkg.Types {
			exported += 1 + len(t.Methods)
		}
	}
	fmt.Printf("Generated public/index.html: %d packages, %d exported declarations\n",
		len(docData.Packages), exported)
}
