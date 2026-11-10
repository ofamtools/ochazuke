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

// ownedField is one field with a single writer: the functions allowed to
// assign it and the exact legacy sites the guard still accepts. The field
// sits on root (Runtime when empty, or Instance for the per-modal
// bookkeeping); with member set only that member of the field (and a write
// of the whole field, which replaces it) counts, so Instance.Payload's
// other members stay free. Reads stay open everywhere.
type ownedField struct {
	name       string
	root       string
	member     string
	owners     map[string]bool
	exceptions []stateWriteException
	advice     string
}

// label is the field as the guard names it: Runtime.Mode,
// Instance.Payload.Return.
func (f ownedField) label() string {
	root := f.root
	if root == "" {
		root = "Runtime"
	}
	if f.member != "" {
		return root + "." + f.name + "." + f.member
	}
	return root + "." + f.name
}

// modalStackOwners are the modal stack's writers (modal_stack.go), each
// named exactly: no other function in the file is an owner.
func modalStackOwners(fns ...string) map[string]bool {
	out := map[string]bool{}
	for _, fn := range fns {
		out["modal_stack.go Runtime."+fn] = true
	}
	return out
}

// ownedFields are the Runtime fields the guard owns.
var ownedFields = []ownedField{
	{
		name:   "Mode",
		owners: map[string]bool{"mode_change.go Runtime.changeMode": true},
		// No exceptions. Nothing borrows the mode: the queries
		// (binding.stops, the typing check, help at its origin) read a
		// resolutionContext, a replayed remote verb reads the mode its
		// request started in from its RequestContext (Ctx.mode), and a
		// frame draws each surface in its presentation
		// (Runtime.PresentationAt, DrawnFocus, Model.StylesAt).
		advice: "request the mode (SetMode, mode.switch, Input.SetMode) or add the transition to changeMode's callers",
	},
	{
		name:   "Focus",
		owners: map[string]bool{"focus_change.go Runtime.changeFocus": true},
		// No exceptions: the page beneath the modals is drawn at the focus
		// the outermost modal saved by reading it (DrawnFocus), never by
		// assigning it for the frame.
		advice: "move the focus (SetFocus, setFocus, RetargetFocus, OpenPage, Push/Pop) or add the landing to changeFocus's callers",
	},
	// The modal stack (modal_stack.go): the open modal IDs, the instances
	// it created, each open modal's saved return and its Payload.Return
	// snapshot, and a borrowed focus's adoption bookkeeping. No exceptions.
	{
		name:   "modals",
		owners: modalStackOwners("initModalStack", "pushModalFrame", "popModalFrame"),
		advice: "open and close modals with Push, OpenModal, Pop or CloseModal; the stack's own writes are in modal_stack.go",
	},
	{
		name:   "modalInstances",
		owners: modalStackOwners("initModalStack", "markModalInstance", "releaseModalInstance"),
		advice: "record a created modal instance with markModalInstance or releaseModalInstance (modal_stack.go)",
	},
	{
		name:   "ModalReturns",
		owners: modalStackOwners("initModalStack", "setModalReturn", "popModalFrame"),
		advice: "save a return with pushModalFrame, setModalReturn or repairModalReturns (modal_stack.go); focus and mode change together",
	},
	{
		name:   "Payload",
		root:   "Instance",
		member: "Return",
		owners: modalStackOwners("setModalReturn", "popModalFrame"),
		// The regression that proves the snapshot is not authoritative has
		// to write it the way outside code could, through Modals().
		exceptions: []stateWriteException{
			{"modal_stack_test.go", "TestModalReturnRepairDuringSelection", 1, "proves a write to the Payload.Return snapshot through a Modals() pointer does not move the saved return"},
		},
		advice: "an open modal's Payload.Return is a snapshot of its saved return: change the return with setModalReturn or repairModalReturns (modal_stack.go)",
	},
	{
		name:   "borrowed",
		root:   "Instance",
		owners: modalStackOwners("adoptModalFocus", "popModalFrame"),
		advice: "adopt a focus under a modal with adoptModalFocus; popModalFrame gives it back (modal_stack.go)",
	},
	// Noun.Builtin says the framework registered the noun (inside
	// Runtime.Builtin); the runtime sets it when it first registers the noun
	// and applications never write it.
	{
		name:   "Builtin",
		root:   "Noun",
		owners: map[string]bool{"noun.go Runtime.ensureNoun": true},
		advice: "Noun.Builtin is set by the runtime when it registers a noun inside Runtime.Builtin (ensureNoun); never write it",
	},
	{
		name:   "lentParent",
		root:   "Instance",
		owners: modalStackOwners("adoptModalFocus", "popModalFrame"),
		advice: "adopt a focus under a modal with adoptModalFocus; popModalFrame gives it back (modal_stack.go)",
	},
}

// TestStateWriteOwnership is the state-ownership guard: no Go file in the
// repository (the root module, its packages, tests, build-tagged files and
// the nested modules cmd/o-story and examples) assigns an owned field
// (Runtime.Mode, Runtime.Focus, the modal stack: Runtime.modals,
// modalInstances, ModalReturns, an Instance's Payload.Return, borrowed and
// lentParent), a member of it (.Name, .Payload.Count), an element of it
// (an index write, delete, clear, copy, a sorting or deleting slices/sort
// call) or takes its address, writes it through a copied Runtime or a
// Runtime composite literal, outside its owner (changeMode, changeFocus,
// the modal stack methods in modal_stack.go), except the exact reviewed
// writes listed in ownedFields (one test-only snapshot write). Reads stay
// open everywhere. Mode and Focus have no exception at all: no render or
// query borrows them (MO-10 removed the last one, Model.beneath).
func TestStateWriteOwnership(t *testing.T) {
	writes := repositoryStateWrites(t)
	for _, field := range ownedFields {
		if (field.name == "Mode" || field.name == "Focus") && len(field.exceptions) > 0 {
			t.Errorf("%s has exceptions again (%v): a render or a query reads a presentation or resolution context, it never assigns the live state", field.label(), field.exceptions)
		}
		t.Run(field.label(), func(t *testing.T) {
			checkFieldOwnership(t, field, writes[field.label()])
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
			t.Errorf("%s: %s writes %s (%s) outside its owner; %s", s.pos, s.fn, field.label(), s.what, field.advice)
		}
	}
	if owned == 0 {
		t.Errorf("no owner assigns %s any more: the guard's owner list is stale", field.label())
	}
	for key, want := range allowed {
		if got[key] != want {
			t.Errorf("exception %s allows %d writes to %s, found %d: update the list (%s)", key, want, field.label(), got[key], reasons[key])
		}
	}
}

// TestStateWriteGuardFixtures proves the guard on a small package of its
// own, once per owned field: each bad* (Mode), focusBad* (Focus),
// stackBad* (modals), createdBad* (modalInstances), returnsBad*
// (ModalReturns), snapBad* (Instance.Payload.Return) or borrowBad*
// (Instance.borrowed) function writes that field one way; each matching
// *Ok/ok* function writes a same-named field of another type, another
// member, a copy, reads, or writes through a pointer the field holds. A
// field's check looks only at its own prefixes.
func TestStateWriteGuardFixtures(t *testing.T) {
	const src = `package fx

import (
	"slices"
	"sort"
)

type Request struct{ Args map[string]any }
type Payload struct {
	Return  string
	Count   int
	Request *Request
	Shadow  *ModeState
}
type ModeState struct {
	Name    string
	Payload Payload
}
type Runtime struct {
	Mode           ModeState
	Focus          string
	modals         []string
	modalInstances map[string]bool
	ModalReturns   map[string]ModalReturnState
}
type ModalReturnState struct {
	Focus string
	Mode  ModeState
}
type Instance struct {
	Payload    Payload
	borrowed   string
	lentParent string
}
type Noun struct {
	Name    string
	Builtin bool
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

func stackBadAppend(r *Runtime)   { r.modals = append(r.modals, "x") }
func stackBadIndex(r *Runtime)    { r.modals[0] = "x" }
func stackBadTruncate(r *Runtime) { r.modals = r.modals[:0] }
func stackBadReverse(r *Runtime)  { slices.Reverse(r.modals) }
func stackBadDelete(r *Runtime)   { _ = slices.Delete(r.modals, 0, 1) }
func stackBadSort(r *Runtime)     { sort.Strings(r.modals) }
func stackBadCopy(r *Runtime)     { copy(r.modals, []string{"x"}) }
func stackBadClear(r *Runtime)    { clear(r.modals) }
func stackBadAddress(r *Runtime)  { p := &r.modals[0]; *p = "x" }
func stackBadLiteral() *Runtime   { return &Runtime{modals: []string{"x"}} }
func stackBadRuntimeCopy(r *Runtime) Runtime { at := *r; return at }

func stackOkRead(r *Runtime) int       { return len(r.modals) }
func stackOkClone(r *Runtime)          { m := slices.Clone(r.modals); m[0] = "x" }
func stackOkReslice(r *Runtime) []string { m := r.modals; m = m[:0]; return m }
func stackOkContains(r *Runtime) bool  { return slices.Contains(r.modals, "x") }
func stackOkLocalSort(ids []string)    { sort.Strings(ids) }

func createdBadSet(r *Runtime)    { r.modalInstances["x"] = true }
func createdBadDelete(r *Runtime) { delete(r.modalInstances, "x") }
func createdBadClear(r *Runtime)  { clear(r.modalInstances) }
func createdBadWhole(r *Runtime)  { r.modalInstances = nil }

func createdOkRead(r *Runtime) bool { return r.modalInstances["x"] }
func createdOkRange(r *Runtime) (n int) {
	for range r.modalInstances {
		n++
	}
	return n
}

func returnsBadSet(r *Runtime)    { r.ModalReturns["x"] = ModalReturnState{Focus: "f"} }
func returnsBadDelete(r *Runtime) { delete(r.ModalReturns, "x") }
func returnsBadWhole(r *Runtime)  { r.ModalReturns = nil }

func returnsOkRead(r *Runtime) string       { return r.ModalReturns["x"].Focus }
func returnsOkCopyOut(r *Runtime) ModalReturnState { ret := r.ModalReturns["x"]; ret.Focus = "y"; return ret }

func snapBadReturn(in *Instance)        { in.Payload.Return = "x" }
func snapBadParen(in *Instance)         { (in.Payload).Return = "x" }
func snapBadWhole(in *Instance)         { in.Payload = Payload{} }
func snapBadAddress(in *Instance) *string { return &in.Payload.Return }
func snapBadAddressWhole(in *Instance)  { p := &in.Payload; p.Return = "x" }
func snapBadPointerMethod(in *Instance) { in.Payload.bump() }
func snapBadValue(in Instance)          { in.Payload.Return = "x" }

func snapOkCount(in *Instance)          { in.Payload.Count = 1 }
func snapOkRead(in *Instance) string    { return in.Payload.Return }
func snapOkLocal() Payload              { var p Payload; p.Return = "x"; return p }
func snapOkMode(r *Runtime)             { r.Mode.Payload.Count = 2 }
func snapOkLiteral() *Instance          { return &Instance{Payload: Payload{Return: "x"}} }

func borrowBadSet(in *Instance)         { in.borrowed = "x" }
func borrowBadAddress(in *Instance) *string { return &in.borrowed }
func borrowOkRead(in *Instance) string  { return in.borrowed }
func borrowOkOther(in *Instance)        { in.lentParent = "x" }

func builtinBadSet(n *Noun)             { n.Builtin = true }
func builtinBadAddress(n *Noun) *bool   { return &n.Builtin }
func builtinOkRead(n *Noun) bool        { return n.Builtin }
func builtinOkOther(n *Noun)            { n.Name = "x" }
func builtinOkLiteral() *Noun           { return &Noun{Name: "x"} }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fx.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	pkg, info := checkFiles(fset, "fx", []*ast.File{file}, fakeImporter(nil))
	runtime := pkg.Scope().Lookup("Runtime").Type()
	for _, c := range []struct {
		field   ownedField
		bad, ok string
	}{
		{ownedField{name: "Mode"}, "bad", "ok"},
		{ownedField{name: "Focus"}, "focusBad", "focusOk"},
		{ownedField{name: "modals"}, "stackBad", "stackOk"},
		{ownedField{name: "modalInstances"}, "createdBad", "createdOk"},
		{ownedField{name: "ModalReturns"}, "returnsBad", "returnsOk"},
		{ownedField{name: "Payload", root: "Instance", member: "Return"}, "snapBad", "snapOk"},
		{ownedField{name: "borrowed", root: "Instance"}, "borrowBad", "borrowOk"},
		{ownedField{name: "Builtin", root: "Noun"}, "builtinBad", "builtinOk"},
	} {
		byFn := map[string]int{}
		for _, s := range fieldWritesIn(fset, file, "fx.go", info, ownedTarget(t, pkg, c.field, runtime)) {
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
					t.Errorf("%s writes %s and the guard missed it", name, c.field.label())
				}
			case strings.HasPrefix(name, c.ok):
				checked++
				if byFn[name] != 0 {
					t.Errorf("%s does not write %s and the guard flagged it", name, c.field.label())
				}
			}
		}
		if checked == 0 {
			t.Errorf("no fixtures for %s", c.field.label())
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
			target := ownedTarget(t, rootPkg, owned, runtime)
			for i, f := range g.files {
				out[owned.label()] = append(out[owned.label()], fieldWritesIn(fset, f, g.names[i], info, target)...)
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
	return structField(t, pkg, "Runtime", name)
}

// structField is the named field of the package's named struct type.
func structField(t *testing.T, pkg *types.Package, typ, name string) *types.Var {
	t.Helper()
	obj := pkg.Scope().Lookup(typ)
	if obj == nil {
		t.Fatalf("no %s type", typ)
	}
	return fieldOf(t, obj.Type(), typ, name)
}

func fieldOf(t *testing.T, typ types.Type, label, name string) *types.Var {
	t.Helper()
	st, ok := typ.Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("%s is not a struct", label)
	}
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == name {
			return st.Field(i)
		}
	}
	t.Fatalf("%s has no %s field", label, name)
	return nil
}

// writeTarget is what fieldWritesIn looks for: the field, the member of
// it that counts when only one does, and the Runtime type whose copies and
// composite literals count (nil for a field on another type: a new
// Instance literal is not a write into an open modal).
type writeTarget struct {
	field, member *types.Var
	runtime       types.Type
}

// ownedTarget resolves an ownedField in pkg.
func ownedTarget(t *testing.T, pkg *types.Package, owned ownedField, runtime types.Type) writeTarget {
	t.Helper()
	root := owned.root
	if root == "" {
		root = "Runtime"
	}
	target := writeTarget{field: structField(t, pkg, root, owned.name)}
	if root == "Runtime" {
		target.runtime = runtime
	}
	if owned.member != "" {
		target.member = fieldOf(t, target.field.Type(), owned.label(), owned.member)
	}
	return target
}

// fieldWritesIn returns the writes to one owned field in one file: an
// assignment, increment or range assignment whose target is the field, a
// member of it or an element of it (an index of an array, slice or map
// field), an address taken of any of those (also implicitly, calling a
// pointer method on a member), a builtin or library call that changes an
// element in place (delete, clear, copy into it, a slices or sort call that
// reorders or deletes), a composite literal of runtime naming the field,
// and a copy of a runtime value (*r), which would let a write to the copy
// pose as the runtime's own. With a member, only writes reaching that
// member or replacing the whole field count.
func fieldWritesIn(fset *token.FileSet, f *ast.File, name string, info *types.Info, target writeTarget) []stateWriteSite {
	field, runtime := target.field, target.runtime
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
			if tv, ok := info.Types[x.X]; ok && tv.Type != nil {
				switch tv.Type.Underlying().(type) {
				case *types.Array, *types.Slice, *types.Map:
					return owned(x.X)
				}
			}
		}
		return false
	}
	// written narrows owned to the member when one is named: e writes it
	// (or a part of it), or replaces the whole field.
	written := owned
	if member := target.member; member != nil {
		var reaches func(e ast.Expr) bool
		reaches = func(e ast.Expr) bool {
			switch x := e.(type) {
			case *ast.ParenExpr:
				return reaches(x.X)
			case *ast.SelectorExpr:
				sel := info.Selections[x]
				if sel == nil || sel.Kind() != types.FieldVal || sel.Indirect() {
					return false
				}
				if sel.Obj() == member {
					return owned(x.X)
				}
				return reaches(x.X)
			}
			return false
		}
		written = func(e ast.Expr) bool {
			for p, ok := e.(*ast.ParenExpr); ok; p, ok = e.(*ast.ParenExpr) {
				e = p.X
			}
			if x, ok := e.(*ast.SelectorExpr); ok {
				if sel := info.Selections[x]; sel != nil && sel.Obj() == field {
					return true
				}
			}
			return reaches(e)
		}
	}
	// mutators are library calls that change their first argument in place.
	mutators := map[string]map[string]bool{
		"slices": {"Reverse": true, "Sort": true, "SortFunc": true, "SortStableFunc": true, "Delete": true, "DeleteFunc": true, "Insert": true, "Replace": true, "Compact": true, "CompactFunc": true},
		"sort":   {"Strings": true, "Ints": true, "Slice": true, "SliceStable": true, "Sort": true, "Stable": true},
		"maps":   {"Copy": true, "DeleteFunc": true},
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
					if written(lhs) {
						add(lhs, "assignment")
					}
				}
			}
		case *ast.IncDecStmt:
			if written(x.X) {
				add(x.X, "increment")
			}
		case *ast.RangeStmt:
			if x.Tok == token.ASSIGN {
				for _, e := range []ast.Expr{x.Key, x.Value} {
					if e != nil && written(e) {
						add(e, "range assignment")
					}
				}
			}
		case *ast.UnaryExpr:
			if x.Op == token.AND && written(x.X) {
				add(x, "address")
			}
		case *ast.SelectorExpr:
			if sel := info.Selections[x]; sel != nil && sel.Kind() == types.MethodVal {
				sig := sel.Obj().Type().(*types.Signature)
				if _, ptr := sig.Recv().Type().(*types.Pointer); ptr && written(x.X) {
					add(x, "pointer method")
				}
			}
		case *ast.CallExpr:
			if len(x.Args) == 0 {
				break
			}
			switch fun := x.Fun.(type) {
			case *ast.Ident:
				if _, builtin := info.Uses[fun].(*types.Builtin); builtin && (fun.Name == "delete" || fun.Name == "clear" || fun.Name == "copy") && written(x.Args[0]) {
					add(x, fun.Name)
				}
			case *ast.SelectorExpr:
				if pkg, ok := fun.X.(*ast.Ident); ok {
					if pn, ok := info.Uses[pkg].(*types.PkgName); ok && mutators[pn.Imported().Path()][fun.Sel.Name] && written(x.Args[0]) {
						add(x, pkg.Name+"."+fun.Sel.Name)
					}
				}
			}
		case *ast.CompositeLit:
			if tv, ok := info.Types[x]; ok && runtime != nil && tv.Type != nil && types.Identical(tv.Type, runtime) {
				for _, elt := range x.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						if id, ok := kv.Key.(*ast.Ident); ok && id.Name == field.Name() {
							add(kv, "composite literal")
						}
					}
				}
			}
		case *ast.StarExpr:
			if runtime != nil && isRuntime(x) {
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
