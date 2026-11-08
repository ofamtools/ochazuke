package ochazuke_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// renderInputFields are the public table fields the binding render cache
// trusts its RenderStamp for: a write to one must sit in a function that also
// bumps the stamp (InvalidateBindings, or the generation itself).
var renderInputFields = map[string]bool{"Bindings": true, "Guards": true, "Availability": true, "Extends": true}

// renderInputOtherTypes are files whose Bindings/Extends belong to other
// types (a bindings file document, the reference doc).
var renderInputOtherTypes = map[string]bool{"docs_reference.go": true, "bindings_file.go": true}

// renderInputOtherFuncs write an Extends that is not a Noun table: ModeExtends
// fills a ModeSpec that DefineMode (which bumps) applies, and the two
// default-bindings functions read and write BindingsDoc groups and ModeSpecs.
var renderInputOtherFuncs = map[string]bool{"ModeExtends": true, "defaultBindingsDoc": true, "loadDefaultDoc": true}

func writesRenderInput(e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.IndexExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.SelectorExpr:
			if renderInputFields[x.Sel.Name] {
				return true
			}
			e = x.X
		default:
			return false
		}
	}
}

func bumpsRender(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "InvalidateBindings" {
				found = true
			}
		case *ast.IncDecStmt:
			if sel, ok := x.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "generation" {
				found = true
			}
		}
		return !found
	})
	return found
}

// TestRenderInputWritersBump holds every function that writes a binding,
// guard, availability or extends table to bump the render stamp, so the
// caches that trust the stamp never serve stale hints or help.
func TestRenderInputWritersBump(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	var missing []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || renderInputOtherTypes[file] {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || renderInputOtherFuncs[fn.Name.Name] {
				continue
			}
			var sites []string
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.AssignStmt:
					for _, lhs := range x.Lhs {
						if writesRenderInput(lhs) {
							sites = append(sites, fset.Position(x.Pos()).String())
						}
					}
				case *ast.CallExpr:
					if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "delete" || id.Name == "clear") && len(x.Args) > 0 && writesRenderInput(x.Args[0]) {
						sites = append(sites, fset.Position(x.Pos()).String())
					}
				}
				return true
			})
			if len(sites) > 0 && !bumpsRender(fn.Body) {
				missing = append(missing, fn.Name.Name+" "+sites[0])
			}
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("writes a render input without InvalidateBindings: %s", m)
	}
}
