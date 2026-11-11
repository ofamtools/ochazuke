// Package namecheck fails an application's tests when its code branches
// on a name its own runtime declares, and when a function grows more
// tangled.
//
// An ochazuke application is data: nouns, verbs, modes, motions and
// parameters are declared once and reached through tables and handlers.
// The wrong way is an `if op.Verb == "set"`, a `case "insert":` or a
// package-level `map[string]X{"task": ...}` in Go, a branch the
// declarations cannot see. namecheck finds those. It takes the vocabulary
// from the application's runtime (Runtime.NameVocab) and reports three
// kinds of hit in non-test Go:
//
//   - compare: a == or != against a string literal, or a constant whose
//     value is one, that is in the vocabulary;
//     constants of local defined string value types are separate domains
//     (for example pane IDs), not interchangeable dispatch-name strings;
//   - case: a case label of a tagged switch that is such a literal;
//   - table: a package-level map literal keyed by such literals.
//
// Declarations do not count: a Register, Define, Verb or Bind call, a
// Binding, Parameter or Arguments literal names things, it does not branch
// on them. Which calls and types are declarative is the data in
// DeclCalls and DeclTypes (Config adds to them).
//
// A second rule ratchets cyclomatic complexity: no function may exceed
// its baseline number, and a function the baseline does not list may not
// exceed Ceiling (25).
//
// The baseline is a text file of tab-separated rows keyed by file,
// function, kind and literal, never by line, so moving code inside a file
// does not change it. The test fails on a hit the baseline does not
// cover, and on a baseline row that no longer matches, so a removed
// branch has to come out of the file and the counts only go down. A row
// with a reason is a legitimate use that stays; a row without one is debt.
// Adding a row is a reviewed line in the diff: NAMECHECK_UPDATE=1 only
// removes rows and lowers counts, and NAMECHECK_UPDATE=seed writes a
// baseline that does not exist yet.
//
// An application adopts it with one test:
//
//	func TestNames(t *testing.T) {
//		namecheck.Check(t, namecheck.Config{
//			Vocab:    rt.NameVocab(),
//			Baseline: "testdata/namecheck.txt",
//		})
//	}
package namecheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Kinds of hit. KindCyclo rows hold a function's complexity as the count.
const (
	KindCompare = "compare"
	KindCase    = "case"
	KindTable   = "table"
	KindCyclo   = "cyclo"
)

// DefaultCeiling is the complexity a function the baseline does not list
// may not exceed.
const DefaultCeiling = 25

// Vocab maps each name of the application (a noun, verb, mode, motion or
// parameter name) to the categories it belongs to.
type Vocab map[string][]string

// DeclCalls are the call names (the last identifier of the callee) whose
// arguments declare things; a map literal inside one is not a name table.
var DeclCalls = []string{
	"Register", "Define", "Verb", "Bind", "Do", "Noun", "Mode", "Param", "Parameters",
	"AddPage", "AddList", "AddInput", "AddTree", "AddTerminal", "AddTimeline", "AddRecordEditor",
	"SetDefaultBindings", "LoadDefaultBindings", "Columns", "Props", "Store",
}

// DeclTypes are the composite types (by name) that declare things.
var DeclTypes = []string{
	"Arguments", "Parameter", "Binding", "Request", "Noun", "Operation", "Example",
	"PropSpec", "Column", "ModeSpec", "BindingGroup", "NounAlias", "ActionAlias",
	"MotionTable",
}

// Dispatch fields give a named string type its role in the registry schema.
// Values of those types remain name branches even when explicitly typed.
var dispatchFields = map[string]bool{
	"Key": true, "Noun": true, "Verb": true, "Mode": true, "Motion": true, "Action": true,
}

// Config is what Check needs.
type Config struct {
	// Vocab is the application's names, from its runtime.
	Vocab Vocab
	// Baseline is the baseline file, relative to the test's directory.
	Baseline string
	// Dirs are the package directories to scan, relative to the test's
	// directory; "dir/..." walks dir and below. Default ".". A walk skips
	// hidden directories, testdata, vendor and nested modules.
	Dirs []string
	// Ceiling is the complexity a function not in the baseline may not
	// exceed; default DefaultCeiling.
	Ceiling int
	// DeclCalls and DeclTypes add to the package defaults.
	DeclCalls, DeclTypes []string
}

// Hit is one branch or table found in the code.
type Hit struct {
	File, Func, Kind, Literal string
	Line                      int
	Categories                []string
}

// Key is the baseline key: file, function, kind and literal.
func (h Hit) Key() Key { return Key{h.File, h.Func, h.Kind, h.Literal} }

// Key identifies a baseline row; it holds no line number.
type Key struct{ File, Func, Kind, Literal string }

func (k Key) String() string {
	return fmt.Sprintf("%s\t%s\t%s\t%s", k.File, k.Func, k.Kind, strconv.Quote(k.Literal))
}

// Report is what a scan found.
type Report struct {
	Hits  []Hit
	Cyclo map[Key]int // per function: file, function, KindCyclo, "-"
	Lines map[Key]int // first line of each function, for messages
}

// walker holds one scan's tables.
type walker struct {
	cfg        Config
	vocab      Vocab
	consts     map[string]string // constant name -> value; "" entries mark ambiguity
	valueTypes map[string]bool   // package directory + defined string type
	nameTypes  map[string]bool   // typed dispatch fields in declarative structs
	declCalls  map[string]bool
	declTypes  map[string]bool
	rep        *Report
	fset       *token.FileSet
	file       string
	pkgNames   map[string]bool // package names the current file imports
	funcName   string
}

// rule finds hits in one node. Code rules run on every node inside
// function bodies; the table rule runs on package-level declarations.
type rule struct {
	kind string
	find func(w *walker, n ast.Node) []Hit
}

var codeRules = []rule{
	{KindCompare, compareHits},
	{KindCase, caseHits},
}

// weights is the cyclomatic cost of each node type: a decision point.
var weights = map[reflect.Type]func(ast.Node) int{
	reflect.TypeOf((*ast.IfStmt)(nil)):    func(ast.Node) int { return 1 },
	reflect.TypeOf((*ast.ForStmt)(nil)):   func(ast.Node) int { return 1 },
	reflect.TypeOf((*ast.RangeStmt)(nil)): func(ast.Node) int { return 1 },
	reflect.TypeOf((*ast.CaseClause)(nil)): func(n ast.Node) int {
		return b2i(n.(*ast.CaseClause).List != nil)
	},
	reflect.TypeOf((*ast.CommClause)(nil)): func(n ast.Node) int {
		return b2i(n.(*ast.CommClause).Comm != nil)
	},
	reflect.TypeOf((*ast.BinaryExpr)(nil)): func(n ast.Node) int {
		op := n.(*ast.BinaryExpr).Op
		return b2i(op == token.LAND || op == token.LOR)
	},
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Scan parses the configured directories and reports every hit and every
// function's complexity.
func Scan(cfg Config) (*Report, error) {
	w := &walker{
		cfg: cfg, vocab: cfg.Vocab, consts: map[string]string{}, valueTypes: map[string]bool{}, nameTypes: map[string]bool{},
		declCalls: set(DeclCalls, cfg.DeclCalls), declTypes: set(DeclTypes, cfg.DeclTypes),
		rep:  &Report{Cyclo: map[Key]int{}, Lines: map[Key]int{}},
		fset: token.NewFileSet(),
	}
	dirs := cfg.Dirs
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	files, err := listFiles(dirs)
	if err != nil {
		return nil, err
	}
	type parsed struct {
		name string
		f    *ast.File
	}
	var all []parsed
	for _, name := range files {
		f, err := parser.ParseFile(w.fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		if generated(f) {
			continue
		}
		all = append(all, parsed{name, f})
		w.file = name
		w.collectValueTypes(f)
	}
	for _, p := range all {
		w.file = p.name
		w.collectConsts(p.f)
	}
	for _, p := range all {
		w.file = p.name
		w.pkgNames = importNames(p.f)
		for _, d := range p.f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body != nil {
					w.function(funcKey(d), d.Pos(), d.Body)
				}
			case *ast.GenDecl:
				w.packageLevel(d)
			}
		}
	}
	sort.Slice(w.rep.Hits, func(i, j int) bool {
		a, b := w.rep.Hits[i], w.rep.Hits[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return w.rep, nil
}

func set(lists ...[]string) map[string]bool {
	m := map[string]bool{}
	for _, l := range lists {
		for _, s := range l {
			m[s] = true
		}
	}
	return m
}

func generated(f *ast.File) bool {
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "// Code generated") && strings.HasSuffix(c.Text, "DO NOT EDIT.") {
				return true
			}
		}
	}
	return false
}

// listFiles is the non-test Go files of dirs, as slash paths.
func listFiles(dirs []string) ([]string, error) {
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

func importNames(f *ast.File) map[string]bool {
	m := map[string]bool{}
	for _, im := range f.Imports {
		if im.Name != nil {
			m[im.Name.Name] = true
			continue
		}
		p, _ := strconv.Unquote(im.Path.Value)
		m[path.Base(p)] = true
	}
	return m
}

// Defined value types enforce a compile-time boundary from dispatch strings.
// Aliases, primitive string constants and imported framework key types do not.
func (w *walker) collectValueTypes(f *ast.File) {
	for _, decl := range f.Decls {
		g, ok := decl.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}
		for _, spec := range g.Specs {
			t := spec.(*ast.TypeSpec)
			base, ok := t.Type.(*ast.Ident)
			if ok && base.Name == "string" && !t.Assign.IsValid() {
				w.valueTypes[filepath.Dir(w.file)+"\x00"+t.Name.Name] = true
			}
			if fields, ok := t.Type.(*ast.StructType); ok && w.declTypes[t.Name.Name] {
				for _, field := range fields.Fields.List {
					typ, named := field.Type.(*ast.Ident)
					if !named {
						continue
					}
					for _, name := range field.Names {
						if dispatchFields[name.Name] {
							w.nameTypes[filepath.Dir(w.file)+"\x00"+typ.Name] = true
						}
					}
				}
			}
		}
	}
}

// collectConsts records `const X = "lit"` anywhere; two different values
// for one name make it ambiguous, so it is never resolved.
func (w *walker) collectConsts(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		g, ok := n.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			return true
		}
		for _, s := range g.Specs {
			vs := s.(*ast.ValueSpec)
			if typ, ok := vs.Type.(*ast.Ident); ok && w.valueTypes[filepath.Dir(w.file)+"\x00"+typ.Name] && !w.nameTypes[filepath.Dir(w.file)+"\x00"+typ.Name] {
				for _, id := range vs.Names {
					w.consts[id.Name] = "\x00ambiguous"
				}
				continue
			}
			for i, id := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				bl, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || bl.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(bl.Value)
				if err != nil {
					continue
				}
				if old, dup := w.consts[id.Name]; dup && old != v {
					w.consts[id.Name] = "\x00ambiguous"
				} else {
					w.consts[id.Name] = v
				}
			}
		}
		return true
	})
}

// literal is the string value an expression is, when it is a string
// literal or a constant (a bare name, or pkg.Name of an imported
// package) with a known value.
func (w *walker) literal(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			v, err := strconv.Unquote(e.Value)
			return v, err == nil
		}
	case *ast.ParenExpr:
		return w.literal(e.X)
	case *ast.Ident:
		return w.constValue(e.Name)
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok && w.pkgNames[x.Name] {
			return w.constValue(e.Sel.Name)
		}
	}
	return "", false
}

func (w *walker) constValue(name string) (string, bool) {
	v, ok := w.consts[name]
	return v, ok && v != "\x00ambiguous"
}

func (w *walker) hit(kind, lit string, pos token.Pos) (Hit, bool) {
	cats, ok := w.vocab[lit]
	if !ok {
		return Hit{}, false
	}
	return Hit{File: w.file, Func: w.funcName, Kind: kind, Literal: lit, Line: w.fset.Position(pos).Line, Categories: cats}, true
}

func compareHits(w *walker, n ast.Node) []Hit {
	b, ok := n.(*ast.BinaryExpr)
	if !ok || (b.Op != token.EQL && b.Op != token.NEQ) {
		return nil
	}
	for _, side := range []ast.Expr{b.X, b.Y} {
		if lit, ok := w.literal(side); ok {
			if h, ok := w.hit(KindCompare, lit, b.Pos()); ok {
				return []Hit{h}
			}
		}
	}
	return nil
}

// caseHits are the labels of a tagged switch; a tagless switch's labels
// are conditions the compare rule reads. The switch itself holds the tag,
// so the rule runs on the switch node.
func caseHits(w *walker, n ast.Node) []Hit {
	sw, ok := n.(*ast.SwitchStmt)
	if !ok || sw.Tag == nil {
		return nil
	}
	var out []Hit
	for _, c := range sw.Body.List {
		for _, e := range c.(*ast.CaseClause).List {
			if lit, ok := w.literal(e); ok {
				if h, ok := w.hit(KindCase, lit, e.Pos()); ok {
					out = append(out, h)
				}
			}
		}
	}
	return out
}

// function scans one function body: the code rules over every node
// (closures included, counted into the function) and its complexity.
func (w *walker) function(name string, pos token.Pos, body ast.Node) {
	w.funcName = name
	cyc := 1
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		if wt, ok := weights[reflect.TypeOf(n)]; ok {
			cyc += wt(n)
		}
		for _, r := range codeRules {
			w.rep.Hits = append(w.rep.Hits, r.find(w, n)...)
		}
		return true
	})
	k := Key{w.file, name, KindCyclo, "-"}
	w.rep.Cyclo[k] += cyc // two package-level literals of one name add up
	if _, ok := w.rep.Lines[k]; !ok {
		w.rep.Lines[k] = w.fset.Position(pos).Line
	}
}

// packageLevel scans a top-level var or const declaration: function
// literals are functions (named "var NAME"), map literals keyed by
// vocabulary names are tables unless they sit inside a declaration.
func (w *walker) packageLevel(g *ast.GenDecl) {
	if g.Tok != token.VAR {
		return
	}
	for _, s := range g.Specs {
		vs := s.(*ast.ValueSpec)
		name := "var _"
		if len(vs.Names) > 0 {
			name = "var " + vs.Names[0].Name
		}
		for _, v := range vs.Values {
			w.funcName = name
			w.tables(v, 0)
			ast.Inspect(v, func(n ast.Node) bool {
				if fl, ok := n.(*ast.FuncLit); ok {
					w.function(name, fl.Pos(), fl.Body)
					return false
				}
				return true
			})
		}
	}
}

// tables walks an expression for map literals. decl counts the
// declarations around the node (a declarative call or type); a map inside
// one names things and is not a table. A function literal ends the walk:
// it is code, which the code rules read.
func (w *walker) tables(n ast.Node, decl int) {
	switch n := n.(type) {
	case *ast.CallExpr:
		if w.declCalls[calleeName(n.Fun)] {
			decl++
		}
		for _, a := range n.Args {
			w.tables(a, decl)
		}
	case *ast.CompositeLit:
		if n.Type != nil && w.declTypes[typeName(n.Type)] {
			decl++
		}
		if decl == 0 {
			w.mapTable(n)
		}
		for _, el := range n.Elts {
			w.tables(el, decl)
		}
	case *ast.KeyValueExpr:
		w.tables(n.Value, decl)
	case *ast.UnaryExpr:
		w.tables(n.X, decl)
	case *ast.ParenExpr:
		w.tables(n.X, decl)
	}
}

// mapTable reports a map literal keyed by vocabulary names. A literal of
// a named type may be a struct, whose keys are field names, so only a
// declared map type reads constants as keys; any literal reads string
// literal keys.
func (w *walker) mapTable(c *ast.CompositeLit) {
	_, isMap := c.Type.(*ast.MapType)
	for _, el := range c.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if _, isLit := kv.Key.(*ast.BasicLit); !isLit && !isMap {
			continue
		}
		if lit, ok := w.literal(kv.Key); ok {
			if h, ok := w.hit(KindTable, lit, kv.Pos()); ok {
				w.rep.Hits = append(w.rep.Hits, h)
			}
		}
	}
}

func calleeName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.IndexExpr:
		return calleeName(e.X)
	}
	return ""
}

// typeName is the name of a composite literal's type: the identifier, the
// selector's name, or the element of a slice or array.
func typeName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.ArrayType:
		return typeName(e.Elt)
	case *ast.MapType:
		return typeName(e.Value)
	}
	return ""
}

func funcKey(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	t := d.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if ix, ok := t.(*ast.IndexExpr); ok {
		t = ix.X
	}
	if ix, ok := t.(*ast.IndexListExpr); ok {
		t = ix.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + d.Name.Name
	}
	return d.Name.Name
}
