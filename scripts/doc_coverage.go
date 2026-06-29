//go:build ignore

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	fset := token.NewFileSet()
	total := 0
	documented := 0
	var undocumented []string

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "vendor" || info.Name() == "examples" || info.Name() == ".git" || info.Name() == "scratch" || info.Name() == ".github" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}

		ast.Inspect(node, func(n ast.Node) bool {
			switch decl := n.(type) {
			case *ast.GenDecl:
				if decl.Tok == token.IMPORT {
					return true
				}
				for _, spec := range decl.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(s.Name.Name) {
							total++
							hasDoc := s.Doc != nil || decl.Doc != nil
							if hasDoc {
								documented++
							} else {
								undocumented = append(undocumented, fmt.Sprintf("%s: type %s", fset.Position(s.Pos()), s.Name.Name))
							}
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if ast.IsExported(name.Name) {
								total++
								hasDoc := s.Doc != nil || decl.Doc != nil
								if hasDoc {
									documented++
								} else {
									undocumented = append(undocumented, fmt.Sprintf("%s: value %s", fset.Position(name.Pos()), name.Name))
								}
							}
						}
					}
				}
			case *ast.FuncDecl:
				if ast.IsExported(decl.Name.Name) {
					if decl.Recv != nil && len(decl.Recv.List) > 0 {
						recvType := decl.Recv.List[0].Type
						var recvName string
						switch t := recvType.(type) {
						case *ast.Ident:
							recvName = t.Name
						case *ast.StarExpr:
							if ident, ok := t.X.(*ast.Ident); ok {
								recvName = ident.Name
							}
						}
						if recvName != "" && !ast.IsExported(recvName) {
							return true
						}
					}
					total++
					if decl.Doc != nil {
						documented++
					} else {
						undocumented = append(undocumented, fmt.Sprintf("%s: func/method %s", fset.Position(decl.Name.Pos()), decl.Name.Name))
					}
				}
			}
			return true
		})
		return nil
	})

	if err != nil {
		fmt.Printf("Error walking path: %v\n", err)
		os.Exit(1)
	}

	coverage := 0.0
	if total > 0 {
		coverage = float64(documented) / float64(total) * 100
	}

	fmt.Printf("Documentation Coverage: %d/%d (%.2f%%)\n", documented, total, coverage)
	if len(undocumented) > 0 {
		fmt.Println("\nUndocumented exported symbols:")
		for _, sym := range undocumented {
			fmt.Println(" -", sym)
		}
	}

	if coverage < 100.0 {
		fmt.Println("\nError: Documentation coverage is below 100%. Please document all exported symbols.")
		os.Exit(1)
	}
}
