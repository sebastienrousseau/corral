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
	"strings"
)

type DocData struct {
	Packages []PkgDoc
}

type PkgDoc struct {
	Name  string
	Path  string
	Doc   string
	Funcs []FuncDoc
	Types []TypeDoc
}

type FuncDoc struct {
	Name string
	Decl string
	Doc  string
}

type TypeDoc struct {
	Name    string
	Doc     string
	Decl    string
	Methods []FuncDoc
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Corral API Documentation</title>
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
            <h1>Corral API Documentation</h1>
            <div class="subtitle">Reference manual covering all modules & packages</div>
        </header>
        
        {{range .Packages}}
        <div class="pkg-card">
            <h2>Package {{.Name}}</h2>
            <p><code>import "github.com/sebastienrousseau/corral/{{.Path}}"</code></p>
            <p>{{.Doc}}</p>

            {{if .Funcs}}
            <h3>Functions</h3>
            {{range .Funcs}}
            <div>
                <h4>func {{.Name}}</h4>
                <pre>{{.Decl}}</pre>
                <p>{{.Doc}}</p>
            </div>
            {{end}}
            {{end}}

            {{if .Types}}
            <h3>Types</h3>
            {{range .Types}}
            <div>
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

func main() {
	fset := token.NewFileSet()
	var docData DocData

	paths := []string{"internal/github", "internal/git", "internal/engine", "internal/tui", "internal/mcp"}
	for _, p := range paths {
		pkgs, err := parser.ParseDir(fset, p, func(info os.FileInfo) bool {
			return !strings.HasSuffix(info.Name(), "_test.go")
		}, parser.ParseComments)

		if err != nil {
			fmt.Printf("Error parsing dir %s: %v\n", p, err)
			continue
		}

		for name, pkg := range pkgs {
			d := doc.New(pkg, p, doc.AllDecls)
			var pkgDoc PkgDoc
			pkgDoc.Name = name
			pkgDoc.Path = p
			pkgDoc.Doc = d.Doc

			// Functions
			for _, f := range d.Funcs {
				pkgDoc.Funcs = append(pkgDoc.Funcs, FuncDoc{
					Name: f.Name,
					Decl: formatFuncDecl(fset, f.Decl),
					Doc:  f.Doc,
				})
			}

			// Types
			for _, t := range d.Types {
				typeDoc := TypeDoc{
					Name: t.Name,
					Doc:  t.Doc,
					Decl: formatDecl(fset, t.Decl),
				}
				for _, m := range t.Methods {
					typeDoc.Methods = append(typeDoc.Methods, FuncDoc{
						Name: m.Name,
						Decl: formatFuncDecl(fset, m.Decl),
						Doc:  m.Doc,
					})
				}
				pkgDoc.Types = append(pkgDoc.Types, typeDoc)
			}

			docData.Packages = append(docData.Packages, pkgDoc)
		}
	}

	err := os.MkdirAll("public", 0755)
	if err != nil {
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

	fmt.Println("Successfully generated Corral API documentation inside public/index.html")
}
