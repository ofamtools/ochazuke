// Command opname moves a module's Go code from ochazuke's string-field
// operation registry (v0.99.0 and earlier) to the Operations registry and
// the three-part Key (the lexicon plan's LX1, ~/hub/o-plans/plans/lexicon.md).
//
// It type-checks the packages against the ochazuke they build with today
// (run it before bumping ochazuke past v0.99.0) and rewrites, by type, never
// by name alone:
//
//	Operation{Noun: n, Verb: v}      -> Operation{Key: "n.v"} or Key: ochazuke.KeyOf(n, v)
//	Operation{App: h}                -> Operation{AppHandler: h}
//	op.Noun, op.Verb (reads)         -> op.Noun(), op.Name()
//	op.Noun = x / op.Verb = x        -> op.Key = ochazuke.KeyOf(x, op.Name()) / KeyOf(op.Noun(), x)
//	op.App                           -> op.AppHandler
//	op.Key()                         -> op.Key.Short()
//	r.Operations[k] (a read)         -> r.Operations.At(k)
//	v, ok := r.Operations[k]         -> v, ok := r.Operations.Find(k)
//	r.Operations[k] = op             -> r.Operations.Set(op)
//	delete(r.Operations, k)          -> r.Operations.Delete(k)
//	range r.Operations               -> range r.Operations.Entries()
//	len(r.Operations)                -> r.Operations.Len()
//	the type Action (actions.json)   -> UserOperation
//
// A key argument that is not an untyped string constant is converted
// (ochazuke.Key(k)); op.Key() as the key is op.Key itself. Every other use
// of Runtime.Operations (the map passed on, assigned, or built) is listed as
// left for a person, with its position. A package already on the new API
// (Operation has no Noun field) is left alone, so a second run changes
// nothing.
//
//	go run ofam.dev/ochazuke/cmd/opname -n ./...   # list the edits, write nothing
//	go run ofam.dev/ochazuke/cmd/opname ./...      # rewrite the files in place
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// Path is the import path of the library whose API moved.
const Path = "ofam.dev/ochazuke"

func main() {
	dry := flag.Bool("n", false, "report the edits without writing files")
	tags := flag.String("tags", "", "build tags to load the packages with")
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	var flags []string
	if *tags != "" {
		flags = []string{"-tags=" + *tags}
	}
	run, err := Rewrite(packages.Config{BuildFlags: flags}, patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, "opname:", err)
		os.Exit(1)
	}
	changed, err := run.Write(*dry)
	if err != nil {
		fmt.Fprintln(os.Stderr, "opname:", err)
		os.Exit(1)
	}
	for _, m := range run.Manual {
		fmt.Println("manual:", m)
	}
	fmt.Printf("opname: %d edits in %d files, %d left for a person\n", run.Count, changed, len(run.Manual))
}

// Rewrite loads the packages (with their tests) under cfg's Dir, Env and
// BuildFlags and plans every edit; Write applies them.
func Rewrite(cfg packages.Config, patterns []string) (*Run, error) {
	cfg.Mode = packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps
	cfg.Tests = true
	pkgs, err := packages.Load(&cfg, patterns...)
	if err != nil {
		return nil, err
	}
	var problems []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			problems = append(problems, e.Error())
		}
	})
	if len(problems) > 0 {
		return nil, fmt.Errorf("%d load errors (the packages must build against the old ochazuke), first: %s", len(problems), problems[0])
	}
	run := NewRun()
	for _, p := range pkgs {
		run.Package(p)
	}
	return run, nil
}

// Edit replaces Src[Start:End] of one file with Text.
type Edit struct {
	Start, End int
	Text       string
}

// File is one source file's text and its pending edits, kept outermost
// only: an edit made over a range takes in every edit inside it.
type File struct {
	Path  string
	Src   []byte
	Edits []Edit
	Seen  map[[2]int]bool // node ranges already rewritten (a file shared by package variants)
	// Import is set when an edit names the library in a file that does
	// not import it (a test reaching the registry through another package).
	Import bool
}

// Render is Src[start:end] with the edits inside it applied.
func (f *File) Render(start, end int) string {
	var b strings.Builder
	at := start
	for _, e := range f.Edits {
		if e.Start >= start && e.End <= end {
			b.Write(f.Src[at:e.Start])
			b.WriteString(e.Text)
			at = e.End
		}
	}
	b.Write(f.Src[at:end])
	return b.String()
}

// Add records an edit, dropping the ones it contains.
func (f *File) Add(start, end int, text string) {
	kept := f.Edits[:0]
	for _, e := range f.Edits {
		if e.Start >= start && e.End <= end {
			continue
		}
		kept = append(kept, e)
	}
	f.Edits = append(kept, Edit{start, end, text})
	sort.Slice(f.Edits, func(i, j int) bool { return f.Edits[i].Start < f.Edits[j].Start })
}

// Run holds every file touched and the uses left for a person.
type Run struct {
	Files  map[string]*File
	Manual []string
	Count  int
	manual map[string]bool
}

// NewRun is an empty run.
func NewRun() *Run { return &Run{Files: map[string]*File{}, manual: map[string]bool{}} }

// Write applies the edits, gofmt-ing each file, and reports how many changed.
func (run *Run) Write(dry bool) (int, error) {
	names := make([]string, 0, len(run.Files))
	for name := range run.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	changed := 0
	for _, name := range names {
		f := run.Files[name]
		if len(f.Edits) == 0 {
			continue
		}
		out := []byte(f.Render(0, len(f.Src)))
		if f.Import {
			out = addImport(name, out)
		}
		if pretty, err := format.Source(out); err == nil {
			out = pretty
		} else {
			run.note(name + ": gofmt after the rewrite failed: " + err.Error())
		}
		if bytes.Equal(out, f.Src) {
			continue
		}
		changed++
		if dry {
			fmt.Printf("%s: %d edits\n", name, len(f.Edits))
			continue
		}
		if err := os.WriteFile(name, out, 0o644); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func (run *Run) note(s string) {
	if !run.manual[s] {
		run.manual[s] = true
		run.Manual = append(run.Manual, s)
	}
}

// pass is one package's rewrite.
type pass struct {
	run  *Run
	pkg  *packages.Package
	info *types.Info
	fset *token.FileSet
	file *File
	ast  *ast.File
	qual string // "ochazuke." outside the package, "" inside it
	own  bool   // the package is ochazuke itself
	// missing is set when the file does not import the library yet.
	missing bool
}

// Package rewrites one loaded package unless it already uses the new API.
func (run *Run) Package(p *packages.Package) {
	lib := findLib(p)
	if lib == nil || !oldAPI(lib) {
		return
	}
	for _, f := range p.Syntax {
		name := p.Fset.File(f.Pos()).Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		file := run.Files[name]
		if file == nil {
			src, err := os.ReadFile(name)
			if err != nil {
				run.note(name + ": " + err.Error())
				continue
			}
			file = &File{Path: name, Src: src, Seen: map[[2]int]bool{}}
			run.Files[name] = file
		}
		ps := &pass{run: run, pkg: p, info: p.TypesInfo, fset: p.Fset, file: file, ast: f, own: p.PkgPath == Path}
		if !ps.own {
			name, ok := importName(f)
			ps.qual = name + "."
			if !ok {
				ps.missing = true
			}
		}
		ps.walk()
	}
}

// findLib is the ochazuke package p builds against (p itself, for ochazuke).
func findLib(p *packages.Package) *types.Package {
	if p.PkgPath == Path {
		return p.Types
	}
	var found *types.Package
	seen := map[*packages.Package]bool{}
	var visit func(q *packages.Package)
	visit = func(q *packages.Package) {
		if found != nil || seen[q] {
			return
		}
		seen[q] = true
		if q.PkgPath == Path {
			found = q.Types
			return
		}
		for _, imp := range q.Imports {
			visit(imp)
		}
	}
	visit(p)
	return found
}

// oldAPI reports whether lib's Operation still has its Noun field.
func oldAPI(lib *types.Package) bool {
	obj := lib.Scope().Lookup("Operation")
	if obj == nil {
		return false
	}
	st, ok := obj.Type().Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := range st.NumFields() {
		if st.Field(i).Name() == "Noun" {
			return true
		}
	}
	return false
}

// importName is the name f imports the library under, and whether it does.
func importName(f *ast.File) (string, bool) {
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == Path {
			if imp.Name != nil {
				return imp.Name.Name, true
			}
			return "ochazuke", true
		}
	}
	return "ochazuke", false
}

// addImport adds the library's import to a rewritten file.
func addImport(name string, src []byte) []byte {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return src
	}
	astutil.AddImport(fset, f, Path)
	var b bytes.Buffer
	if err := format.Node(&b, fset, f); err != nil {
		return src
	}
	return b.Bytes()
}

func (ps *pass) off(p token.Pos) int { return ps.fset.Position(p).Offset }

func (ps *pass) text(n ast.Node) string { return ps.file.Render(ps.off(n.Pos()), ps.off(n.End())) }

func (ps *pass) pos(n ast.Node) string { return ps.fset.Position(n.Pos()).String() }

// replace rewrites node n's range once (a file in several package variants
// is walked more than once).
func (ps *pass) replace(n ast.Node, text string) {
	ps.replaceRange(ps.off(n.Pos()), ps.off(n.End()), text)
}

func (ps *pass) replaceRange(start, end int, text string) {
	k := [2]int{start, end}
	if ps.file.Seen[k] {
		return
	}
	ps.file.Seen[k] = true
	ps.file.Add(start, end, text)
	ps.run.Count++
	if ps.missing && strings.Contains(text, ps.qual) {
		ps.file.Import = true
	}
}

// named is t's named type when it is lib's type name (through a pointer).
func isLib(t types.Type, name string) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == Path && obj.Name() == name
}

// fieldOwner is the struct type (named) the selected field belongs to.
func fieldOwner(sel *types.Selection) types.Type {
	t := sel.Recv()
	idx := sel.Index()
	for _, i := range idx[:len(idx)-1] {
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		st, ok := t.Underlying().(*types.Struct)
		if !ok {
			return nil
		}
		t = st.Field(i).Type()
	}
	return t
}

// opField is the field of Operation sel picks ("" when not one).
func (ps *pass) opField(se *ast.SelectorExpr) string {
	sel := ps.info.Selections[se]
	if sel == nil || sel.Kind() != types.FieldVal || !isLib(fieldOwner(sel), "Operation") {
		return ""
	}
	return se.Sel.Name
}

// isOpKeyCall reports whether e is op.Key() on an Operation.
func (ps *pass) isOpKeyCall(e ast.Expr) (*ast.SelectorExpr, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil, false
	}
	se, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || se.Sel.Name != "Key" {
		return nil, false
	}
	sel := ps.info.Selections[se]
	if sel == nil || sel.Kind() != types.MethodVal || !isLib(sel.Recv(), "Operation") {
		return nil, false
	}
	return se, true
}

// isRegistry reports whether e is a Runtime's Operations field.
func (ps *pass) isRegistry(e ast.Expr) bool {
	se, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok || se.Sel.Name != "Operations" {
		return false
	}
	sel := ps.info.Selections[se]
	return sel != nil && sel.Kind() == types.FieldVal && isLib(fieldOwner(sel), "Runtime")
}

// key is a key argument as a Key: an untyped constant as it is, op.Key()
// as op.Key, anything else converted.
func (ps *pass) key(e ast.Expr) string {
	if se, ok := ps.isOpKeyCall(e); ok {
		return ps.text(se.X) + ".Key"
	}
	if b, ok := ast.Unparen(e).(*ast.BasicLit); ok && b.Kind == token.STRING {
		return ps.text(e)
	}
	if tv, ok := ps.info.Types[e]; ok {
		if b, ok := tv.Type.(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
			return ps.text(e)
		}
	}
	return ps.qual + "Key(" + ps.text(e) + ")"
}

// walk visits the file children first, so an outer rewrite renders the
// inner ones it contains.
func (ps *pass) walk() {
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(ps.ast, func(n ast.Node) bool {
		if n == nil {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			ps.exit(top, parents)
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
}

func (ps *pass) exit(n ast.Node, parents map[ast.Node]ast.Node) {
	switch n := n.(type) {
	case *ast.Ident:
		ps.ident(n, parents)
	case *ast.SelectorExpr:
		ps.selector(n, parents)
	case *ast.CallExpr:
		ps.call(n)
	case *ast.CompositeLit:
		ps.literal(n)
	case *ast.IndexExpr:
		ps.index(n, parents)
	case *ast.AssignStmt:
		ps.assign(n)
	case *ast.RangeStmt:
		if ps.isRegistry(n.X) {
			ps.replace(n.X, ps.text(n.X)+".Entries()")
		}
	}
}

// ident renames the type Action (an actions.json entry) to UserOperation.
func (ps *pass) ident(id *ast.Ident, parents map[ast.Node]ast.Node) {
	if id.Name != "Action" {
		return
	}
	obj := ps.info.Uses[id]
	if obj == nil {
		obj = ps.info.Defs[id]
	}
	tn, ok := obj.(*types.TypeName)
	if !ok || tn.Pkg() == nil || tn.Pkg().Path() != Path {
		return
	}
	ps.replace(id, "UserOperation")
}

func (ps *pass) selector(se *ast.SelectorExpr, parents map[ast.Node]ast.Node) {
	if se.Sel.Name == "Operations" && ps.isRegistry(se) {
		ps.bareRegistry(se, parents)
		return
	}
	switch ps.opField(se) {
	case "App":
		ps.replace(se.Sel, "AppHandler")
	case "Noun", "Verb":
		if as, ok := parents[se].(*ast.AssignStmt); ok && slicesContains(as.Lhs, se) {
			return // the assignment rewrites the whole statement
		}
		if _, ok := parents[se].(*ast.UnaryExpr); ok {
			ps.run.note(ps.pos(se) + ": address of an Operation's " + se.Sel.Name)
			return
		}
		method := map[string]string{"Noun": ".Noun()", "Verb": ".Name()"}[se.Sel.Name]
		ps.replace(se, ps.text(se.X)+method)
	}
}

// bareRegistry lists a use of Runtime.Operations no rule covers.
func (ps *pass) bareRegistry(se *ast.SelectorExpr, parents map[ast.Node]ast.Node) {
	switch p := parents[se].(type) {
	case *ast.IndexExpr:
		if p.X == se {
			return
		}
	case *ast.RangeStmt:
		if p.X == se {
			return
		}
	case *ast.CallExpr:
		if id, ok := p.Fun.(*ast.Ident); ok && (id.Name == "delete" || id.Name == "len") && len(p.Args) > 0 && p.Args[0] == se {
			return
		}
	case *ast.SelectorExpr:
		return // already the new API's r.Operations.X
	case *ast.KeyValueExpr:
	}
	ps.run.note(ps.pos(se) + ": Runtime.Operations used as a map (" + ps.text(se) + "); move it by hand")
}

func slicesContains(list []ast.Expr, e ast.Expr) bool {
	for _, x := range list {
		if x == e {
			return true
		}
	}
	return false
}

func (ps *pass) call(c *ast.CallExpr) {
	if se, ok := ps.isOpKeyCall(c); ok {
		ps.replace(c, ps.text(se.X)+".Key.Short()")
		return
	}
	id, ok := c.Fun.(*ast.Ident)
	if !ok || len(c.Args) == 0 || !ps.isRegistry(c.Args[0]) {
		return
	}
	if b, ok := ps.info.Uses[id].(*types.Builtin); !ok || b == nil {
		return
	}
	reg := ps.text(c.Args[0])
	switch id.Name {
	case "delete":
		ps.replace(c, reg+".Delete("+ps.key(c.Args[1])+")")
	case "len":
		ps.replace(c, reg+".Len()")
	}
}

// literal rewrites an Operation literal's Noun, Verb and App keys.
func (ps *pass) literal(lit *ast.CompositeLit) {
	tv, ok := ps.info.Types[lit]
	if !ok || !isLib(tv.Type, "Operation") {
		return
	}
	var noun, verb *ast.KeyValueExpr
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		id, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch id.Name {
		case "Noun":
			noun = kv
		case "Verb":
			verb = kv
		case "App":
			ps.replace(id, "AppHandler")
		}
	}
	if noun == nil && verb == nil {
		return
	}
	value := func(kv *ast.KeyValueExpr) (string, string, bool) {
		if kv == nil {
			return `""`, "", true
		}
		if b, ok := kv.Value.(*ast.BasicLit); ok && b.Kind == token.STRING {
			s, err := strconv.Unquote(b.Value)
			return ps.text(kv.Value), s, err == nil
		}
		return ps.text(kv.Value), "", false
	}
	nText, nLit, nOK := value(noun)
	vText, vLit, vOK := value(verb)
	key := ps.qual + "KeyOf(" + nText + ", " + vText + ")"
	if nOK && vOK && noun != nil && verb != nil {
		key = strconv.Quote(nLit + "." + vLit)
	}
	first, second := noun, verb
	if first == nil || (second != nil && second.Pos() < first.Pos()) {
		first, second = second, first
	}
	ps.replace(first, "Key: "+key)
	if second != nil {
		start, end := ps.elementRange(second)
		ps.replaceRange(start, end, "")
	}
}

// elementRange is a literal element's range with its comma, and its whole
// line when it stands alone on one.
func (ps *pass) elementRange(kv *ast.KeyValueExpr) (int, int) {
	src := ps.file.Src
	start, end := ps.off(kv.Pos()), ps.off(kv.End())
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	if end < len(src) && src[end] == ',' {
		end++
	}
	for end < len(src) && (src[end] == ' ' || src[end] == '\t') {
		end++
	}
	lineStart := start
	for lineStart > 0 && (src[lineStart-1] == ' ' || src[lineStart-1] == '\t') {
		lineStart--
	}
	if lineStart > 0 && src[lineStart-1] == '\n' && end < len(src) && src[end] == '\n' {
		return lineStart, end + 1
	}
	return start, end
}

// index rewrites a read of the registry by key.
func (ps *pass) index(ix *ast.IndexExpr, parents map[ast.Node]ast.Node) {
	if !ps.isRegistry(ix.X) {
		return
	}
	reg := ps.text(ix.X)
	switch p := parents[ix].(type) {
	case *ast.AssignStmt:
		if slicesContains(p.Lhs, ix) {
			return // a write: the statement is rewritten
		}
		if len(p.Lhs) == 2 && len(p.Rhs) == 1 {
			ps.replace(ix, reg+".Find("+ps.key(ix.Index)+")")
			return
		}
	case *ast.ValueSpec:
		if len(p.Names) == 2 && len(p.Values) == 1 {
			ps.replace(ix, reg+".Find("+ps.key(ix.Index)+")")
			return
		}
	}
	ps.replace(ix, reg+".At("+ps.key(ix.Index)+")")
}

// assign rewrites writes: r.Operations[k] = op and op.Noun/op.Verb = x.
func (ps *pass) assign(as *ast.AssignStmt) {
	if len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		for _, l := range as.Lhs {
			if ix, ok := l.(*ast.IndexExpr); ok && ps.isRegistry(ix.X) {
				ps.run.note(ps.pos(as) + ": a multiple assignment writes the registry; move it by hand")
			}
			if se, ok := l.(*ast.SelectorExpr); ok && (ps.opField(se) == "Noun" || ps.opField(se) == "Verb") {
				ps.run.note(ps.pos(as) + ": a multiple assignment sets an Operation's " + se.Sel.Name + "; write op.Key = KeyOf(noun, verb) by hand")
			}
		}
		return
	}
	lhs := as.Lhs[0]
	if ix, ok := lhs.(*ast.IndexExpr); ok && ps.isRegistry(ix.X) {
		if as.Tok != token.ASSIGN {
			ps.run.note(ps.pos(as) + ": " + as.Tok.String() + " on the registry; move it by hand")
			return
		}
		ps.replace(as, ps.text(ix.X)+".Set("+ps.text(as.Rhs[0])+")")
		return
	}
	se, ok := lhs.(*ast.SelectorExpr)
	if !ok || as.Tok != token.ASSIGN {
		return
	}
	x := ps.text(se.X)
	switch ps.opField(se) {
	case "Noun":
		ps.replace(as, x+".Key = "+ps.qual+"KeyOf("+ps.text(as.Rhs[0])+", "+x+".Name())")
	case "Verb":
		ps.replace(as, x+".Key = "+ps.qual+"KeyOf("+x+".Noun(), "+ps.text(as.Rhs[0])+")")
	}
}
