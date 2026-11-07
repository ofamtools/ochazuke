package ochazuke_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// stateWriteSite is one write to an owned Runtime field: the file (slash
// path from the repository root), the function it sits in ("Recv.Name" for
// a method, "<init>" outside any function) and its position.
type stateWriteSite struct {
	file, fn string
	pos      token.Position
	what     string
}

func (s stateWriteSite) key() string { return s.file + " " + s.fn }

// stateWriteException is a write site the guard still accepts for this
// stage: the exact file, function and number of writes, and why. Each later
// mode-ownership lane removes its entries; the final list is empty.
type stateWriteException struct {
	file, fn string
	count    int
	reason   string
}

// modeWriteOwners are the functions that own Runtime.Mode: the one writer.
var modeWriteOwners = map[string]bool{
	"mode_change.go Runtime.changeMode": true,
}

// modeWriteExceptions are the legacy borrows that still assign
// Runtime.Mode for a render and put it back. None is a real transition:
// no observer runs and the generation does not move. The queries
// (bindings.stops, the typing check, help at its origin) read a
// resolutionContext instead, and a replayed remote verb reads the mode its
// request started in from its RequestContext (Ctx.mode), so neither is here.
var modeWriteExceptions = []stateWriteException{
	{"model.go", "Model.beneath", 2, "render borrow: draws the page in the mode the outer modal opened over and restores; MO-10 presentation"},
}

// TestStateWriteOwnership is the mode-ownership guard: no Go file in the
// repository (the root module, its packages, tests, build-tagged files and
// the nested modules cmd/o-story and examples) assigns Runtime.Mode, a
// member of it (.Name, .Payload, .Payload.Count) or takes its address,
// writes it through a copied Runtime or a composite literal, outside
// changeMode, except the exact legacy borrows listed above. Reads stay
// open everywhere: the field is exported for inspection.
func TestStateWriteOwnership(t *testing.T) {
	sites := repositoryModeWrites(t)
	allowed := map[string]int{}
	reasons := map[string]string{}
	for _, e := range modeWriteExceptions {
		if e.reason == "" {
			t.Errorf("exception %s %s has no reason", e.file, e.fn)
		}
		allowed[e.file+" "+e.fn] = e.count
		reasons[e.file+" "+e.fn] = e.reason
	}
	got := map[string]int{}
	owned := 0
	for _, s := range sites {
		if modeWriteOwners[s.key()] {
			owned++
			continue
		}
		got[s.key()]++
		if _, ok := allowed[s.key()]; !ok {
			t.Errorf("%s: %s writes Runtime.Mode (%s) outside changeMode; request the mode (SetMode, mode.switch, Input.SetMode) or add the transition to changeMode's callers", s.pos, s.fn, s.what)
		}
	}
	if owned == 0 {
		t.Errorf("changeMode no longer assigns Runtime.Mode: the guard's owner list is stale")
	}
	for key, want := range allowed {
		if got[key] != want {
			t.Errorf("exception %s allows %d writes, found %d: update the list (%s)", key, want, got[key], reasons[key])
		}
	}
}

// TestStateWriteGuardFixtures proves the guard on a small package of its
// own: each bad* function writes the owned field one way; each ok* function
// writes a same-named field of another type, reads, or writes through a
// pointer the field holds.
func TestStateWriteGuardFixtures(t *testing.T) {
	const src = `package fx

type Request struct{ Args map[string]any }
type Payload struct {
	Count   int
	Request *Request
	Shadow  *ModeState
}
type ModeState struct {
	Name    string
	Payload Payload
}
type Runtime struct {
	Mode  ModeState
	Focus string
}
type Input struct{ Mode string }
type Binding struct{ Mode string }
type Model struct{ *Runtime }
type holder struct{ r *Runtime }

func (p *Payload) bump()     { p.Count++ }
func (p Payload) empty() bool { return p.Count == 0 }

func badWhole(r *Runtime)            { r.Mode = ModeState{} }
func badName(r *Runtime)             { r.Mode.Name = "x" }
func badPayload(r *Runtime)          { r.Mode.Payload.Count = 1 }
func badIncrement(r *Runtime)        { r.Mode.Payload.Count++ }
func badOpAssign(r *Runtime)         { r.Mode.Payload.Count += 2 }
func badMulti(r *Runtime)            { r.Focus, r.Mode = "a", ModeState{} }
func badAddress(r *Runtime) *ModeState { return &r.Mode }
func badAddressMember(r *Runtime)    { p := &r.Mode.Payload; p.Count = 1 }
func badPointerMethod(r *Runtime)    { r.Mode.Payload.bump() }
func badEmbedded(m Model)            { m.Mode = ModeState{} }
func badField(h holder)              { h.r.Mode.Name = "x" }
func badValueRuntime(r Runtime)      { r.Mode.Name = "x" }
func badCopy(r *Runtime) Runtime     { at := *r; return at }
func badDeref(r *Runtime)            { *r = Runtime{} }
func badLiteral() *Runtime           { return &Runtime{Mode: ModeState{Name: "n"}} }
func badRange(r *Runtime, ms []ModeState) {
	for _, r.Mode = range ms {
	}
}
func badParen(r *Runtime) { (r.Mode).Name = "x" }

func okRead(r *Runtime) string       { return r.Mode.Name }
func okCopyOut(r *Runtime) ModeState { m := r.Mode; m.Name = "x"; return m }
func okInput(in *Input)              { in.Mode = "insert" }
func okBinding(b *Binding)           { b.Mode = "normal" }
func okFocus(r *Runtime)             { r.Focus = "a" }
func okValueMethod(r *Runtime) bool  { return r.Mode.Payload.empty() }
func okThroughPointer(r *Runtime)    { r.Mode.Payload.Request.Args["k"] = 1 }
func okLiteral() *Runtime            { return &Runtime{Focus: "app"} }
func okLocalState() ModeState        { var m ModeState; m.Payload.Count = 3; return m }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fx.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	pkg, info := checkFiles(fset, "fx", []*ast.File{file}, fakeImporter(nil))
	mode := runtimeModeField(t, pkg)
	byFn := map[string]int{}
	for _, s := range modeWritesIn(fset, file, "fx.go", info, mode, pkg.Scope().Lookup("Runtime").Type()) {
		byFn[s.fn]++
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		name := fn.Name.Name
		switch {
		case strings.HasPrefix(name, "bad") && byFn[name] == 0:
			t.Errorf("%s writes the owned field and the guard missed it", name)
		case strings.HasPrefix(name, "ok") && byFn[name] != 0:
			t.Errorf("%s does not write the owned field and the guard flagged it", name)
		}
	}
}

// repositoryModeWrites type-checks every Go package in the repository with
// the root package as the only real import (other imports are empty
// stand-ins, so expressions that need them type as invalid and are not
// mistaken for Runtime) and returns every write to Runtime.Mode.
func repositoryModeWrites(t *testing.T) []stateWriteSite {
	t.Helper()
	type group struct {
		dir, name string
		files     []*ast.File
		names     []string
	}
	fset := token.NewFileSet()
	groups := map[string]*group{}
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "vendor" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(p))
		key := dir + " " + f.Name.Name
		g := groups[key]
		if g == nil {
			g = &group{dir: dir, name: f.Name.Name}
			groups[key] = g
		}
		g.files = append(g.files, f)
		g.names = append(g.names, filepath.ToSlash(p))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	root := groups[". ochazuke"]
	if root == nil {
		t.Fatal("the root package ochazuke was not found")
	}
	rootPkg, rootInfo := checkFiles(fset, "ofam.dev/ochazuke", root.files, fakeImporter(nil))
	mode := runtimeModeField(t, rootPkg)
	runtime := rootPkg.Scope().Lookup("Runtime").Type()
	var sites []stateWriteSite
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range groups {
			if !yield(k) {
				return
			}
		}
	})
	for _, key := range keys {
		g := groups[key]
		info := rootInfo
		if g != root {
			_, info = checkFiles(fset, path.Join("repo", g.dir, g.name), g.files, fakeImporter(rootPkg))
		}
		for i, f := range g.files {
			sites = append(sites, modeWritesIn(fset, f, g.names[i], info, mode, runtime)...)
		}
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].file != sites[j].file {
			return sites[i].file < sites[j].file
		}
		return sites[i].pos.Offset < sites[j].pos.Offset
	})
	return sites
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// fakeImporter answers ofam.dev/ochazuke with root (when set) and every
// other path with an empty, complete package.
func fakeImporter(root *types.Package) types.Importer {
	fakes := map[string]*types.Package{}
	return importerFunc(func(p string) (*types.Package, error) {
		if root != nil && p == root.Path() {
			return root, nil
		}
		if pkg := fakes[p]; pkg != nil {
			return pkg, nil
		}
		name := path.Base(p)
		if strings.HasPrefix(name, "v") && strings.Trim(name[1:], "0123456789") == "" {
			name = path.Base(path.Dir(p))
		}
		pkg := types.NewPackage(p, strings.NewReplacer("-", "", ".", "").Replace(name))
		pkg.MarkComplete()
		fakes[p] = pkg
		return pkg, nil
	})
}

// checkFiles type-checks one package, ignoring errors (imports are stand-ins).
func checkFiles(fset *token.FileSet, path string, files []*ast.File, imp types.Importer) (*types.Package, *types.Info) {
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
	}
	conf := types.Config{Importer: imp, Error: func(error) {}, FakeImportC: true}
	pkg, _ := conf.Check(path, fset, files, info)
	return pkg, info
}

// runtimeModeField is the Mode field of the package's Runtime struct.
func runtimeModeField(t *testing.T, pkg *types.Package) *types.Var {
	t.Helper()
	obj := pkg.Scope().Lookup("Runtime")
	if obj == nil {
		t.Fatal("no Runtime type")
	}
	st := obj.Type().Underlying().(*types.Struct)
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == "Mode" {
			return st.Field(i)
		}
	}
	t.Fatal("Runtime has no Mode field")
	return nil
}

// modeWritesIn returns the writes to the mode field in one file: an
// assignment, increment or range assignment whose target is the field or a
// member of it, an address taken of either (also implicitly, calling a
// pointer method on a member), a composite literal of runtime naming the
// field, and a copy of a runtime value (*r), which would let a write to the
// copy pose as the runtime's own.
func modeWritesIn(fset *token.FileSet, f *ast.File, name string, info *types.Info, mode *types.Var, runtime types.Type) []stateWriteSite {
	var out []stateWriteSite
	fn := "<init>"
	add := func(n ast.Node, what string) {
		out = append(out, stateWriteSite{file: name, fn: fn, pos: fset.Position(n.Pos()), what: what})
	}
	// owned reports whether writing e writes into the field's own storage:
	// the field itself, or a member reached without following a pointer.
	var owned func(e ast.Expr) bool
	owned = func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.ParenExpr:
			return owned(x.X)
		case *ast.SelectorExpr:
			sel := info.Selections[x]
			if sel == nil || sel.Kind() != types.FieldVal {
				return false
			}
			if sel.Obj() == mode {
				return true
			}
			if sel.Indirect() {
				return false // a pointer the field holds: not the field's storage
			}
			return owned(x.X)
		case *ast.IndexExpr:
			if tv, ok := info.Types[x.X]; ok {
				if _, isArray := tv.Type.Underlying().(*types.Array); isArray {
					return owned(x.X)
				}
			}
		}
		return false
	}
	isRuntime := func(e ast.Expr) bool {
		tv, ok := info.Types[e]
		return ok && tv.IsValue() && tv.Type != nil && types.Identical(tv.Type, runtime)
	}
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok != token.DEFINE {
				for _, lhs := range x.Lhs {
					if owned(lhs) {
						add(lhs, "assignment")
					}
				}
			}
		case *ast.IncDecStmt:
			if owned(x.X) {
				add(x.X, "increment")
			}
		case *ast.RangeStmt:
			if x.Tok == token.ASSIGN {
				for _, e := range []ast.Expr{x.Key, x.Value} {
					if e != nil && owned(e) {
						add(e, "range assignment")
					}
				}
			}
		case *ast.UnaryExpr:
			if x.Op == token.AND && owned(x.X) {
				add(x, "address")
			}
		case *ast.SelectorExpr:
			if sel := info.Selections[x]; sel != nil && sel.Kind() == types.MethodVal {
				sig := sel.Obj().Type().(*types.Signature)
				if _, ptr := sig.Recv().Type().(*types.Pointer); ptr && owned(x.X) {
					add(x, "pointer method")
				}
			}
		case *ast.CompositeLit:
			if tv, ok := info.Types[x]; ok && tv.Type != nil && types.Identical(tv.Type, runtime) {
				for _, elt := range x.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						if id, ok := kv.Key.(*ast.Ident); ok && id.Name == mode.Name() {
							add(kv, "composite literal")
						}
					}
				}
			}
		case *ast.StarExpr:
			if isRuntime(x) {
				add(x, "runtime copy")
			}
		}
		return true
	}
	for _, decl := range f.Decls {
		fn = "<init>"
		if d, ok := decl.(*ast.FuncDecl); ok {
			fn = d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				fn = recvName(d.Recv.List[0].Type) + "." + fn
			}
		}
		ast.Inspect(decl, visit)
	}
	return out
}

// recvName is a method receiver's type name without pointer or type
// parameters.
func recvName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return recvName(x.X)
	case *ast.IndexExpr:
		return recvName(x.X)
	case *ast.IndexListExpr:
		return recvName(x.X)
	case *ast.Ident:
		return x.Name
	}
	return "?"
}
