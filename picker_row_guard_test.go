package ochazuke

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// Picker rows come from data: a source returns []Choice and Choice.Row (via
// ChoiceRows) is the one builder of a PickerRow. This guard fails when a
// non-test file builds a PickerRow or ChooserRow literal (with fields) by
// hand, outside params.go and the allow list.

// rowLiteralAllowed are the files (a name, or with Prefix the start of names) that
// may build rows by hand, each with its reason. The owner (2026-11-09): only
// anything that references a file or directory, and the command palette.
var rowLiteralAllowed = []struct {
	File, Reason string
	Prefix       bool
}{
	{"params.go", "Choice.Row is the one builder of a picker row", false},
	{"path_", "path, file and directory pickers build folder, file, typed and walk rows from the filesystem (path_chooser.go, path_find.go, path_walk.go)", true},
	{"palette", "the command palette parses a command line and builds its own entries; it has no PickerRow literal today", true},
}

func rowLiteralFileAllowed(file string) bool {
	base := filepath.Base(file)
	for _, a := range rowLiteralAllowed {
		if base == a.File || a.Prefix && strings.HasPrefix(base, a.File) {
			return true
		}
	}
	return false
}

// handBuiltRows are the positions of non-empty PickerRow/ChooserRow
// composite literals in the source, including the elided elements of a
// []PickerRow{{...}}.
func handBuiltRows(fset *token.FileSet, f *ast.File) []token.Position {
	isRow := func(e ast.Expr) bool {
		switch t := e.(type) {
		case *ast.Ident:
			return t.Name == "PickerRow" || t.Name == "ChooserRow"
		case *ast.SelectorExpr:
			return t.Sel.Name == "PickerRow" || t.Sel.Name == "ChooserRow"
		}
		return false
	}
	var out []token.Position
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if lit.Type != nil && isRow(lit.Type) && len(lit.Elts) > 0 {
			out = append(out, fset.Position(lit.Pos()))
		}
		if arr, ok := lit.Type.(*ast.ArrayType); ok && isRow(arr.Elt) {
			for _, e := range lit.Elts {
				if el, ok := e.(*ast.CompositeLit); ok && el.Type == nil && len(el.Elts) > 0 {
					out = append(out, fset.Position(el.Pos()))
				}
			}
		}
		return true
	})
	return out
}

func TestPickerRowsAreNotHandBuilt(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != "." && (strings.HasPrefix(name, ".") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || rowLiteralFileAllowed(path) {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, pos := range handBuiltRows(fset, f) {
			t.Errorf("%s: a PickerRow built by hand; return []Choice from a source and use ChoiceRows/Choice.Row (rows come from data), or add the file to rowLiteralAllowed with a reason", pos)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPickerRowGuardSeesLiterals(t *testing.T) {
	t.Parallel()
	src := `package x
var a = PickerRow{ID: "a"}
var b = []ChooserRow{{ID: "b"}}
var c = ochazuke.PickerRow{Label: "c"}
var ok = PickerRow{}
var fine = []PickerRow{row}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := handBuiltRows(fset, f); len(got) != 3 {
		t.Fatalf("found %d literals, want 3: %v", len(got), got)
	}
	for _, a := range rowLiteralAllowed {
		if a.Reason == "" {
			t.Errorf("allow entry %q has no reason", a.File)
		}
	}
	if rowLiteralFileAllowed("list_sort.go") || !rowLiteralFileAllowed("path_find.go") || !rowLiteralFileAllowed("palette_choices.go") {
		t.Error("allow list matches the wrong files")
	}
}
