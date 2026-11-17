package rulecheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The static checks that read the source through the syntax tree
// (declarations, gokeys, stringids, storeliterals, storeui, hookrefusal,
// nounlines) share one parse of Config.Dirs under Config.Root per Check:
// the files, their package's string constants and functions by name. No
// types are loaded; each check says what it matches by syntax alone.

// Source is the parsed non-test Go of the scanned directories.
type Source struct {
	Fset  *token.FileSet
	Files []*SourceFile
}

// SourceFile is one parsed file: its path relative to the root (slash
// separated), its directory (the package) and its syntax tree.
type SourceFile struct {
	Path string
	Dir  string
	AST  *ast.File
	pkg  *sourcePkg
}

// sourcePkg is what a check looks up across a package's files: its string
// constants (name to value) and its functions and methods by name.
type sourcePkg struct {
	consts map[string]string
	funcs  map[string][]*ast.FuncDecl
	files  map[*ast.FuncDecl]*SourceFile
}

// LoadSource parses the non-test Go files of dirs (relative to root;
// "dir/..." walks dir and below, skipping hidden directories, testdata,
// vendor and nested modules), leaving generated files out.
func LoadSource(root string, dirs []string) (*Source, error) {
	if root == "" {
		root = "."
	}
	if len(dirs) == 0 {
		dirs = []string{"./..."}
	}
	names, err := goFiles(scanDirs(root, dirs))
	if err != nil {
		return nil, err
	}
	src := &Source{Fset: token.NewFileSet()}
	pkgs := map[string]*sourcePkg{}
	cleanRoot := filepath.ToSlash(filepath.Clean(root))
	for _, name := range names {
		f, err := parser.ParseFile(src.Fset, name, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		if ast.IsGenerated(f) {
			continue
		}
		rel := name
		if r, ok := strings.CutPrefix(name, cleanRoot+"/"); ok && cleanRoot != "." {
			rel = r
		}
		dir := path.Dir(rel)
		p := pkgs[dir]
		if p == nil {
			p = &sourcePkg{consts: map[string]string{}, funcs: map[string][]*ast.FuncDecl{}, files: map[*ast.FuncDecl]*SourceFile{}}
			pkgs[dir] = p
		}
		sf := &SourceFile{Path: rel, Dir: dir, AST: f, pkg: p}
		src.Files = append(src.Files, sf)
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				p.funcs[d.Name.Name] = append(p.funcs[d.Name.Name], d)
				p.files[d] = sf
			case *ast.GenDecl:
				if d.Tok != token.CONST {
					continue
				}
				for _, sp := range d.Specs {
					vs := sp.(*ast.ValueSpec)
					for i, n := range vs.Names {
						if i < len(vs.Values) {
							if s, ok := stringLit(vs.Values[i]); ok {
								p.consts[n.Name] = s
							}
						}
					}
				}
			}
		}
	}
	return src, nil
}

// Line is the line of pos.
func (s *Source) Line(pos token.Pos) int { return s.Fset.Position(pos).Line }

// Decls calls fn with each function declaration with a body (its key:
// "Recv.Name" or "Name") and each package-level var initializer ("var
// NAME"), with the file it is in.
func (s *Source) Decls(fn func(f *SourceFile, key string, n ast.Node)) {
	for _, f := range s.Files {
		for _, d := range f.AST.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body != nil {
					fn(f, funcKey(d), d)
				}
			case *ast.GenDecl:
				if d.Tok == token.VAR {
					fn(f, genName(d), d)
				}
			}
		}
	}
}

// Const is the string value of e when it is a string, rune or byte
// literal or a constant of f's package holding one.
func (f *SourceFile) Const(e ast.Expr) (string, bool) {
	if s, ok := stringLit(e); ok {
		return s, true
	}
	if id, ok := ast.Unparen(e).(*ast.Ident); ok {
		s, ok := f.pkg.consts[id.Name]
		return s, ok
	}
	return "", false
}

// importName says whether name is a package f imports (its last path
// element or its alias).
func (f *SourceFile) importName(name string) bool {
	for _, im := range f.AST.Imports {
		if im.Name != nil {
			if im.Name.Name == name {
				return true
			}
			continue
		}
		p, _ := strconv.Unquote(im.Path.Value)
		if path.Base(p) == name {
			return true
		}
	}
	return false
}

// ConstNamed is the value of the package constant name.
func (f *SourceFile) ConstNamed(name string) (string, bool) {
	s, ok := f.pkg.consts[name]
	return s, ok
}

// Funcs are the bodies an expression stands for in f's package: a
// function literal, an identifier or selector naming a function or method
// of the package (a method value a.m resolves by its name m, whatever
// the receiver), and for a call, the called function of the package (a
// builder returning the function) and any function literal among its
// arguments (recordFunc(func ...)). Each comes with the key it is
// reported under.
func (f *SourceFile) Funcs(e ast.Expr, at string) []FuncBody {
	var out []FuncBody
	switch x := ast.Unparen(e).(type) {
	case *ast.FuncLit:
		out = append(out, FuncBody{Key: at, File: f, Body: x.Body})
	case *ast.Ident:
		out = append(out, f.named(x.Name)...)
	case *ast.SelectorExpr:
		out = append(out, f.named(x.Sel.Name)...)
	case *ast.CallExpr:
		// A call that builds the function (a.deletionConfirm("task")):
		// the builder's body, which holds the literal it returns, and
		// any literal passed to it.
		switch fn := ast.Unparen(x.Fun).(type) {
		case *ast.Ident:
			out = append(out, f.named(fn.Name)...)
		case *ast.SelectorExpr:
			if _, ok := fn.X.(*ast.Ident); ok && !f.importName(fn.X.(*ast.Ident).Name) {
				out = append(out, f.named(fn.Sel.Name)...)
			}
		}
		for _, a := range x.Args {
			if lit, ok := ast.Unparen(a).(*ast.FuncLit); ok {
				out = append(out, FuncBody{Key: at, File: f, Body: lit.Body})
			}
		}
	}
	return out
}

func (f *SourceFile) named(name string) []FuncBody {
	var out []FuncBody
	for _, d := range f.pkg.funcs[name] {
		if d.Body != nil {
			fb := FuncBody{Key: funcKey(d), File: f.pkg.files[d], Body: d.Body}
			if d.Recv != nil && len(d.Recv.List) > 0 && len(d.Recv.List[0].Names) > 0 {
				fb.Recv = d.Recv.List[0].Names[0].Name
			}
			out = append(out, fb)
		}
	}
	return out
}

// FuncBody is a function's body with the key it is reported under and its
// file.
type FuncBody struct {
	Key  string
	File *SourceFile
	Body *ast.BlockStmt
	// Recv is a method's receiver name ("" for a function or literal).
	Recv string
}

// FuncNamed is the package's function or method bodies of that name.
func (f *SourceFile) FuncNamed(name string) []FuncBody { return f.named(name) }

// sourceCache parses once per Check, for every static check that asks.
type sourceCache struct {
	once sync.Once
	src  *Source
	err  error
}

// Source is the shared parse of Config.Dirs under Config.Root; it fails
// t when the source does not parse.
func (c *Context) Source(t testing.TB) *Source {
	t.Helper()
	if c.src == nil {
		c.src = &sourceCache{}
	}
	c.src.once.Do(func() { c.src.src, c.src.err = LoadSource(c.Root, c.Dirs) })
	if c.src.err != nil {
		t.Fatalf("rulecheck: reading the source: %v", c.src.err)
	}
	return c.src.src
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := ast.Unparen(e).(*ast.BasicLit)
	if !ok || (lit.Kind != token.STRING && lit.Kind != token.CHAR) {
		return "", false
	}
	if lit.Kind == token.CHAR {
		r, _, _, err := strconv.UnquoteChar(lit.Value[1:len(lit.Value)-1], '\'')
		if err != nil {
			return "", false
		}
		return string(r), true
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// callName is "pkg.Fn" for a package function call, ".Method" for a
// method call on any other receiver, and "Fn" for a plain call.
func callName(c *ast.CallExpr) string {
	switch fn := ast.Unparen(c.Fun).(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if id, ok := fn.X.(*ast.Ident); ok {
			return id.Name + "." + fn.Sel.Name
		}
		return "." + fn.Sel.Name
	case *ast.IndexExpr:
		if id, ok := fn.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// methodName is the selected name of a method or package call (Bind of
// x.Bind(...), Define of a.r.Define(...)), "" for a plain call.
func methodName(c *ast.CallExpr) string {
	if sel, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return ""
}

// exprText is a short source form of an expression for a baseline key:
// a constant's value in quotes, else the expression's shape (a.r.x,
// f(...), <expr>).
func exprText(f *SourceFile, e ast.Expr) string {
	if s, ok := f.Const(e); ok {
		return strconv.Quote(s)
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprText(f, x.X) + "." + x.Sel.Name
	case *ast.CallExpr:
		if n := callName(x); n != "" {
			return strings.TrimPrefix(n, ".") + "(...)"
		}
	case *ast.IndexExpr:
		return exprText(f, x.X) + "[...]"
	}
	return "<expr>"
}

// ochazukeName is the name a file imports ofam.dev/ochazuke under ("" when
// it does not; "." for the package itself, whose names are unqualified).
func ochazukeName(f *SourceFile) string {
	if f.AST.Name.Name == "ochazuke" {
		return "."
	}
	for _, im := range f.AST.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		if p != "ofam.dev/ochazuke" {
			continue
		}
		if im.Name != nil {
			return im.Name.Name
		}
		return "ochazuke"
	}
	return ""
}

// isOchazukeType says whether e names ochazuke's type name in f (Store in
// package ochazuke, ochazuke.Store elsewhere), through a pointer.
func isOchazukeType(f *SourceFile, e ast.Expr, name string) bool {
	if st, ok := e.(*ast.StarExpr); ok {
		e = st.X
	}
	switch x := e.(type) {
	case *ast.Ident:
		return ochazukeName(f) == "." && x.Name == name
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		q := ochazukeName(f)
		return ok && q != "" && q != "." && id.Name == q && x.Sel.Name == name
	}
	return false
}

func funcKey(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	t := d.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch ix := t.(type) {
	case *ast.IndexExpr:
		t = ix.X
	case *ast.IndexListExpr:
		t = ix.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + d.Name.Name
	}
	return d.Name.Name
}

func genName(g *ast.GenDecl) string {
	for _, sp := range g.Specs {
		if vs, ok := sp.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
			return g.Tok.String() + " " + vs.Names[0].Name
		}
	}
	return g.Tok.String() + " _"
}

// goFiles is the non-test Go files of dirs, as sorted slash paths.
func goFiles(dirs []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(dir string) error {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			p := path.Clean(filepath.ToSlash(filepath.Join(dir, n)))
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
		return nil
	}
	for _, d := range dirs {
		root, recursive := strings.CutSuffix(d, "/...")
		if !recursive {
			if err := add(d); err != nil {
				return nil, err
			}
			continue
		}
		err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil || !e.IsDir() {
				return err
			}
			if p != root {
				n := e.Name()
				if strings.HasPrefix(n, ".") || n == "testdata" || n == "vendor" {
					return filepath.SkipDir
				}
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return add(p)
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}
