// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package symbols

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func init() { register(goExtractor{}) }

// goExtractor reads declarations out of Go source with go/ast.
//
// This is the same parser the compiler uses, so it agrees with the language
// by construction rather than by a grammar someone maintains separately.
type goExtractor struct{}

// Language implements Extractor.
func (goExtractor) Language() string { return "go" }

// Extensions implements Extractor.
func (goExtractor) Extensions() []string { return []string{".go"} }

// Extract implements Extractor.
//
// Parsing is deliberately tolerant. A workspace is full of source that is
// mid-edit, generated, or written against a newer toolchain, and a parse
// error is not a reason to report nothing: go/parser returns whatever AST
// it managed to build alongside the error, so the declarations before the
// broken one are still worth having. Returning them with a nil error is
// what lets one unparseable file avoid blanking an otherwise good index.
func (g goExtractor) Extract(path string, src []byte) ([]Symbol, error) {
	fset := token.NewFileSet()
	// SkipObjectResolution: nothing here needs the identifier graph, and
	// building it is the expensive half of a parse.
	// The error is deliberately discarded. go/parser returns whatever AST
	// it managed to build alongside it, and with a non-nil src it always
	// returns a non-nil file — verified against binary input, empty input,
	// a missing package clause, and enough syntax errors to trigger its
	// bailout. So there is no nil case to guard, and a syntax error is not
	// a reason to discard the declarations that parsed cleanly before it.
	file, _ := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)

	isTest := strings.HasSuffix(path, "_test.go")

	var out []Symbol
	add := func(name string, kind Kind, receiver string, pos token.Pos) {
		// The blank identifier declares nothing anyone can look up.
		if name == "" || name == "_" {
			return
		}
		out = append(out, Symbol{
			Name:     name,
			Kind:     kind,
			File:     path,
			Line:     fset.Position(pos).Line,
			Receiver: receiver,
			Exported: ast.IsExported(name),
			Language: g.Language(),
			Test:     isTest,
		})
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if recv := receiverName(d.Recv); recv != "" {
				add(d.Name.Name, KindMethod, recv, d.Pos())
				continue
			}
			add(d.Name.Name, KindFunc, "", d.Pos())

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					// An interface is reported as its own kind: "find the
					// interface called Reader" is a question people ask, and
					// collapsing it into "type" makes it unanswerable.
					kind := KindType
					if _, isIface := s.Type.(*ast.InterfaceType); isIface {
						kind = KindInterface
					}
					add(s.Name.Name, kind, "", s.Pos())

				case *ast.ValueSpec:
					kind := KindVar
					if d.Tok == token.CONST {
						kind = KindConst
					}
					for _, name := range s.Names {
						// Each name in `const a, b = 1, 2` is its own symbol,
						// and each sits on the line of the name rather than
						// of the spec, so a multi-line grouping points at the
						// right line for every member.
						add(name.Name, kind, "", name.Pos())
					}
				}
			}
		}
	}
	return out, nil
}

// receiverName renders a method receiver's type without its pointer star
// or type parameters: `func (c *Client[T]) Do()` yields "Client".
//
// The star and the parameters are noise for a lookup — nobody searches for
// "*Client" — and stripping them means a method is found by the type name
// a person would actually type.
func receiverName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	return typeName(recv.List[0].Type)
}

// typeName reduces a receiver expression to its bare identifier.
func typeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeName(t.X)
	case *ast.IndexExpr:
		// Generic receiver with one type parameter: Client[T].
		return typeName(t.X)
	case *ast.IndexListExpr:
		// Generic receiver with several: Pair[K, V].
		return typeName(t.X)
	case *ast.SelectorExpr:
		// Qualified, which a receiver cannot legally be, but costs nothing
		// to handle and keeps this total.
		return t.Sel.Name
	}
	return ""
}
