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

// ownedField is one Runtime field with a single writer: the functions
// allowed to assign it and the exact legacy sites the guard still accepts.
// Reads stay open everywhere: the field is exported for inspection.
type ownedField struct {
	name       string
	owners     map[string]bool
	exceptions []stateWriteException
	advice     string
}

// ownedFields are the Runtime fields the guard owns.
var ownedFields = []ownedField{
	{
		name:   "Mode",
		owners: map[string]bool{"mode_change.go Runtime.changeMode": true},
		// The legacy borrow that still assigns Runtime.Mode for a render
		// and puts it back. It is not a real transition: no observer runs
		// and the generation does not move. The queries (bindings.stops,
		// the typing check, help at its origin) read a resolutionContext
		// instead, and a replayed remote verb reads the mode its request
		// started in from its RequestContext (Ctx.mode), so neither is here.
		exceptions: []stateWriteException{
			{"model.go", "Model.beneath", 2, "render borrow: draws the page in the mode the outer modal opened over and restores; MO-10 presentation"},
		},
		advice: "request the mode (SetMode, mode.switch, Input.SetMode) or add the transition to changeMode's callers",
	},
	{
		name:   "Focus",
		owners: map[string]bool{"focus_change.go Runtime.changeFocus": true},
		// The same render borrow, for the focus: the page beneath the modals
		// is drawn with the focus the outermost modal opened over, then the
		// live focus is put back. No row is selected, no mode rule runs.
		exceptions: []stateWriteException{
			{"model.go", "Model.beneath", 2, "render borrow: draws the page at the focus the outer modal opened over and restores; MO-10 presentation"},
		},
		advice: "move the focus (SetFocus, setFocus, RetargetFocus, OpenPage, Push/Pop) or add the landing to changeFocus's callers",
	},
}

// TestStateWriteOwnership is the state-ownership guard: no Go file in the
// repository (the root module, its packages, tests, build-tagged files and
// the nested modules cmd/o-story and examples) assigns an owned Runtime
// field (Runtime.Mode, Runtime.Focus), a member of it (.Name, .Payload,
// .Payload.Count) or takes its address, writes it through a copied Runtime
// or a composite literal, outside its owner (changeMode, changeFocus),
// except the exact legacy borrows listed in ownedFields. Reads stay open
// everywhere: the fields are exported for inspection.
func TestStateWriteOwnership(t *testing.T) {
	writes := repositoryStateWrites(t)
	for _, field := range ownedFields {
		t.Run(field.name, func(t *testing.T) {
			checkFieldOwnership(t, field, writes[field.name])
		})
	}
}

func checkFieldOwnership(t *testing.T, field ownedField, sites []stateWriteSite) {
	allowed := map[string]int{}
	reasons := map[string]string{}
	for _, e := range field.exceptions {
		if e.reason == "" {
			t.Errorf("exception %s %s has no reason", e.file, e.fn)
		}
		allowed[e.file+" "+e.fn] = e.count
		reasons[e.file+" "+e.fn] = e.reason
	}
	got := map[string]int{}
	owned := 0
	for _, s := range sites {
		if field.owners[s.key()] {
			owned++
			continue
		}
		got[s.key()]++
		if _, ok := allowed[s.key()]; !ok {
			t.Errorf("%s: %s writes Runtime.%s (%s) outside its owner; %s", s.pos, s.fn, field.name, s.what, field.advice)
		}
	}
	if owned == 0 {
		t.Errorf("no owner assigns Runtime.%s any more: the guard's owner list is stale", field.name)
	}
	for key, want := range allowed {
		if got[key] != want {
			t.Errorf("exception %s allows %d writes to Runtime.%s, found %d: update the list (%s)", key, want, field.name, got[key], reasons[key])
		}
	}
}

// TestStateWriteGuardFixtures proves the guard on a small package of its
// own, once per owned field: each bad* (Mode) or focusBad* (Focus)
// function writes that field one way; each ok* or focusOk* function writes
// a same-named field of another type, reads, or writes through a pointer
// the field holds. A field's check looks only at its own prefixes.
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
type Page struct{ Focus, Remembered string }
type KeyEvent struct{ Focus string }
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

func focusBadWhole(r *Runtime)             { r.Focus = "a" }
func focusBadMulti(r *Runtime)             { r.Focus, r.Mode = "a", ModeState{} }
func focusBadAddress(r *Runtime) *string   { return &r.Focus }
func focusBadEmbedded(m Model)             { m.Focus = "a" }
func focusBadField(h holder)               { h.r.Focus = "a" }
func focusBadValueRuntime(r Runtime)       { r.Focus = "a" }
func focusBadCopy(r *Runtime) Runtime      { at := *r; return at }
func focusBadDeref(r *Runtime)             { *r = Runtime{} }
func focusBadLiteral() *Runtime            { return &Runtime{Focus: "app"} }
func focusBadOpAssign(r *Runtime)          { r.Focus += "x" }
func focusBadParen(r *Runtime)             { (r.Focus) = "a" }
func focusBadRange(r *Runtime, ids []string) {
	for _, r.Focus = range ids {
	}
}

func focusOkRead(r *Runtime) string        { return r.Focus }
func focusOkCopyOut(r *Runtime) string     { f := r.Focus; f = "x"; return f }
func focusOkPage(p *Page)                  { p.Focus, p.Remembered = "a", "b" }
func focusOkEvent(ev *KeyEvent)            { ev.Focus = "a" }
func focusOkMode(r *Runtime)               { r.Mode.Name = "x" }
func focusOkLiteral() *Runtime             { return &Runtime{Mode: ModeState{Name: "n"}} }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fx.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	pkg, info := checkFiles(fset, "fx", []*ast.File{file}, fakeImporter(nil))
	runtime := pkg.Scope().Lookup("Runtime").Type()
	for _, c := range []struct{ field, bad, ok string }{
		{"Mode", "bad", "ok"},
		{"Focus", "focusBad", "focusOk"},
	} {
		byFn := map[string]int{}
		for _, s := range fieldWritesIn(fset, file, "fx.go", info, runtimeField(t, pkg, c.field), runtime) {
			byFn[s.fn]++
		}
		checked := 0
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			name := fn.Name.Name
			switch {
			case strings.HasPrefix(name, c.bad):
				checked++
				if byFn[name] == 0 {
					t.Errorf("%s writes Runtime.%s and the guard missed it", name, c.field)
				}
			case strings.HasPrefix(name, c.ok):
				checked++
				if byFn[name] != 0 {
					t.Errorf("%s does not write Runtime.%s and the guard flagged it", name, c.field)
				}
			}
		}
		if checked == 0 {
			t.Errorf("no fixtures for Runtime.%s", c.field)
		}
	}
}

// repositoryStateWrites type-checks every Go package in the repository with
// the root package as the only real import (other imports are empty
// stand-ins, so expressions that need them type as invalid and are not
// mistaken for Runtime) and returns every write to each owned field, by
// field name.
func repositoryStateWrites(t *testing.T) map[string][]stateWriteSite {
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
	runtime := rootPkg.Scope().Lookup("Runtime").Type()
	out := map[string][]stateWriteSite{}
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
		for _, owned := range ownedFields {
			field := runtimeField(t, rootPkg, owned.name)
			for i, f := range g.files {
				out[owned.name] = append(out[owned.name], fieldWritesIn(fset, f, g.names[i], info, field, runtime)...)
			}
		}
	}
	for _, sites := range out {
		sort.Slice(sites, func(i, j int) bool {
			if sites[i].file != sites[j].file {
				return sites[i].file < sites[j].file
			}
			return sites[i].pos.Offset < sites[j].pos.Offset
		})
	}
	return out
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

// runtimeField is the named field of the package's Runtime struct.
func runtimeField(t *testing.T, pkg *types.Package, name string) *types.Var {
	t.Helper()
	obj := pkg.Scope().Lookup("Runtime")
	if obj == nil {
		t.Fatal("no Runtime type")
	}
	st := obj.Type().Underlying().(*types.Struct)
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == name {
			return st.Field(i)
		}
	}
	t.Fatalf("Runtime has no %s field", name)
	return nil
}

// fieldWritesIn returns the writes to one owned field in one file: an
// assignment, increment or range assignment whose target is the field or a
// member of it, an address taken of either (also implicitly, calling a
// pointer method on a member), a composite literal of runtime naming the
// field, and a copy of a runtime value (*r), which would let a write to the
// copy pose as the runtime's own.
func fieldWritesIn(fset *token.FileSet, f *ast.File, name string, info *types.Info, field *types.Var, runtime types.Type) []stateWriteSite {
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
			if sel.Obj() == field {
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
						if id, ok := kv.Key.(*ast.Ident); ok && id.Name == field.Name() {
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
