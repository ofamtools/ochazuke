package ochazuke

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func names[T any](list []T, name func(T) string) []string {
	out := []string{}
	for _, x := range list {
		out = append(out, name(x))
	}
	return out
}

func nounName(n *Noun) string            { return n.Name }
func actionName(a *Action) string        { return a.Name }
func namespaceName(ns *Namespace) string { return ns.Name }

func TestKeyParts(t *testing.T) {
	for _, c := range []struct {
		key                         Key
		ns, noun, name, path, short string
	}{
		{"ochazuke.input.undo", "ochazuke", "input", "undo", "ochazuke.input", "ochazuke.input.undo"},
		{"lane.lane.define", "lane", "lane", "define", "lane.lane", "lane.lane.define"},
		{"pane.kill", DefaultNamespace, "pane", "kill", "pane", "pane.kill"},
		{Key(DefaultNamespace + ".pane.kill"), DefaultNamespace, "pane", "kill", "pane", "pane.kill"},
	} {
		ns, noun, name := c.key.Parts()
		if ns != c.ns || noun != c.noun || name != c.name || c.key.NounPath() != c.path || c.key.Short() != c.short {
			t.Errorf("%s: %s %s %s %s %s", c.key, ns, noun, name, c.key.NounPath(), c.key.Short())
		}
		if c.key.Full().Namespace() != c.ns || strings.Count(string(c.key.Full()), ".") != 2 {
			t.Errorf("%s: full %s", c.key, c.key.Full())
		}
	}
	if Key("a.b.c.d").Check() == nil || Key("a..b").Check() == nil || Key("pane.kill").Check() != nil {
		t.Fatal("Check must refuse more than three parts and an empty part")
	}
}

func TestProgramNamespace(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/bin/o-lane": "lane", "mtm": "mtm", "o-questions.exe": "questions",
		"ochazuke.test": FallbackNamespace, "ochazuke": FallbackNamespace, "app": FallbackNamespace,
	} {
		if got := ProgramNamespace([]string{in}); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

// A two-part key gets the default namespace: stored full, read by either
// spelling.
func TestPushFillsTheDefaultNamespace(t *testing.T) {
	ops := NewOperations(map[string]*Noun{})
	op := ops.Register("task.done", "", "")
	if op.Key != Key(DefaultNamespace+".task.done") || op.App != App(DefaultNamespace) || op.Handler != HandlerID(op.Key) {
		t.Fatalf("pushed %+v", op)
	}
	for _, k := range []Key{"task.done", op.Key} {
		if got, ok := ops.Get(k); !ok || got != op {
			t.Fatalf("Get(%s) = %v %v", k, got, ok)
		}
	}
	if op.Namespace() != DefaultNamespace || op.Noun() != "task" || op.Name() != "done" || op.Action().Name != "done" {
		t.Fatalf("methods: %s %s %s", op.Namespace(), op.Noun(), op.Name())
	}
	r := NewRuntime()
	r.Define("task").Verb("done", func(Ctx) Result { return Done(nil) })
	if got, ok := r.Operations.Get("task.done"); !ok || got.Key.Namespace() != DefaultNamespace {
		t.Fatalf("Runtime.Register stores the full key: %v %v", got, ok)
	}
	if got, ok := r.Operations.Get("ochazuke.input.undo"); !ok || got.Namespace() != FrameworkNamespace {
		t.Fatalf("framework key: %v %v", got, ok)
	}
}

func TestOperationsChangingAndCopies(t *testing.T) {
	ops := NewOperations(map[string]*Noun{})
	ops.Set(Operation{Key: "task.done", Description: "one"})
	ops.Set(Operation{Key: "task.undo"})
	if ops.Len() != 2 || ops.At("task.done").Description != "one" || ops.At("task.none").Key != "" {
		t.Fatalf("Set/At: %d %+v", ops.Len(), ops.At("task.done"))
	}
	cp, ok := ops.Find("task.done")
	cp.Description = "changed"
	if !ok || ops.At("task.done").Description != "one" {
		t.Fatal("Find must answer a copy")
	}
	if got := names(ops.List(), func(o *Operation) string { return o.Key.Short() }); !slices.Equal(got, []string{"task.done", "task.undo"}) {
		t.Fatalf("List keeps registration order: %v", got)
	}
	short := map[string]bool{}
	for k, op := range ops.Entries() {
		short[k] = op.Key.Short() == k
	}
	if !short["task.done"] || !short["task.undo"] {
		t.Fatalf("Entries yields short keys: %v", short)
	}
	ops.Delete("task.done")
	if _, ok := ops.Get("task.done"); ok || len(ops.Order) != 1 {
		t.Fatal("Delete")
	}
}

// Every lookup reads the memoized index, which Invalidate drops and the
// next lookup rebuilds.
func TestInvalidateThenLookupRebuilds(t *testing.T) {
	ops := NewOperations(map[string]*Noun{})
	ops.Register("task.done", "", "")
	if ops.Action("done") == nil || ops.Builds != 1 {
		t.Fatalf("first lookup builds: %d", ops.Builds)
	}
	ops.Noun("task")
	ops.Actions()
	if ops.Builds != 1 {
		t.Fatalf("lookups reuse the index: %d builds", ops.Builds)
	}
	ops.Register("task.undo", "", "")
	if ops.Index != nil || ops.Action("undo") == nil || ops.Builds != 2 {
		t.Fatalf("Register invalidates; the next lookup rebuilds: %d", ops.Builds)
	}
	ops.Push("task.redo", "", "")
	if ops.Action("redo") != nil {
		t.Fatal("Push alone leaves the index as it was")
	}
	ops.Invalidate()
	if ops.Action("redo") == nil {
		t.Fatal("Invalidate then lookup sees the pushed operation")
	}
}

// A mount pushes many operations and invalidates once.
func TestHoldInvalidatesOnce(t *testing.T) {
	r := NewRuntime()
	before := r.Operations.Invalidations
	end := r.Operations.Hold()
	for _, v := range []string{"a", "b", "c", "d", "e", "f"} {
		r.Define("ext.thing").Verb(v, func(Ctx) Result { return Done(nil) })
	}
	if r.Operations.Invalidations != before+1 { // the noun's creation, before any verb
		t.Fatalf("held: %d invalidations", r.Operations.Invalidations-before)
	}
	end()
	if r.Operations.Invalidations != before+2 || len(r.Operations.ActionsOnNoun(r.Operations.Noun("ext.thing"))) != 6 {
		t.Fatalf("end invalidates once: %d", r.Operations.Invalidations-before)
	}
}

func TestOperationsRelations(t *testing.T) {
	ops := NewOperations(map[string]*Noun{"pane": {Name: "pane"}, "empty": {Name: "empty"}})
	for _, k := range []Key{"pane.kill", "pane.show", "ochazuke.input.show", "lane.lane.show", "lane.lane.define"} {
		ops.Register(k, "", "")
	}
	show, pane, lane := ops.Action("show"), ops.Noun("pane"), ops.Namespace("lane")
	if got := names(ops.Namespaces(), namespaceName); !slices.Equal(got, []string{DefaultNamespace, "lane", "ochazuke"}) && !slices.Equal(got, []string{"lane", DefaultNamespace, "ochazuke"}) {
		t.Fatalf("Namespaces: %v", got)
	}
	if got := names(ops.Nouns(), nounName); !slices.Equal(got, []string{"empty", "lane.lane", "ochazuke.input", "pane"}) {
		t.Fatalf("Nouns: %v", got)
	}
	if got := names(ops.Actions(), actionName); !slices.Equal(got, []string{"define", "kill", "show"}) {
		t.Fatalf("Actions: %v", got)
	}
	if got := names(ops.NounsWithAction(show), nounName); !slices.Equal(got, []string{"pane", "ochazuke.input", "lane.lane"}) {
		t.Fatalf("NounsWithAction: %v", got)
	}
	if got := names(ops.NamespacesWithAction(show), namespaceName); !slices.Equal(got, []string{DefaultNamespace, "ochazuke", "lane"}) {
		t.Fatalf("NamespacesWithAction: %v", got)
	}
	if got := names(ops.ActionsOnNoun(pane), actionName); !slices.Equal(got, []string{"kill", "show"}) {
		t.Fatalf("ActionsOnNoun: %v", got)
	}
	if got := names(ops.NounsInNamespace(lane), nounName); !slices.Equal(got, []string{"lane.lane"}) {
		t.Fatalf("NounsInNamespace: %v", got)
	}
	if got := names(ops.ActionsInNamespace(lane), actionName); !slices.Equal(got, []string{"show", "define"}) {
		t.Fatalf("ActionsInNamespace: %v", got)
	}
	if op := ops.OperationFor(pane, show); op == nil || op.Key.Short() != "pane.show" || ops.OperationFor(ops.Noun("empty"), show) != nil {
		t.Fatalf("OperationFor: %v", op)
	}
	if got := names(ops.OperationsOfAction(show), func(o *Operation) string { return o.Key.Short() }); len(got) != 3 {
		t.Fatalf("OperationsOfAction: %v", got)
	}
	if ops.Noun(DefaultNamespace+".pane") != pane || ops.Noun("nothing") != nil || ops.Action("nothing") != nil || ops.Namespace("nothing") != nil {
		t.Fatal("exact lookups")
	}
	if ops.NounsWithAction(nil) != nil || ops.ActionsOnNoun(nil) != nil || ops.OperationFor(nil, show) != nil {
		t.Fatal("nil objects answer nothing")
	}
}

func TestOperationsMatching(t *testing.T) {
	ops := NewOperations(map[string]*Noun{})
	for _, k := range []Key{"pane.kill", "pane.keep", "ochazuke.input.show", "lane.lane.show"} {
		ops.Register(k, "", "")
	}
	if got := ops.ActionsMatching("k"); len(got) != 2 || got[0].Action.Name != "keep" || got[0].PartOfSpeech != ActionPart || got[0].OriginalValue != "k" || got[0].Exact {
		t.Fatalf("ActionsMatching: %+v", got)
	}
	if got := ops.ActionsMatching("kill"); len(got) != 1 || !got[0].Exact {
		t.Fatalf("an exact action: %+v", got)
	}
	in := ops.NounsMatching("in")
	if len(in) != 1 || in[0].Noun.Name != "ochazuke.input" || in[0].PartOfSpeech != NounPart || in[0].Exact {
		t.Fatalf("NounsMatching by the noun's own word: %+v", in)
	}
	if got := ops.NounsMatching("input"); len(got) != 1 || !got[0].Exact {
		t.Fatalf("the own word is exact: %+v", got)
	}
	if got := ops.NamespacesMatching("la"); len(got) != 1 || got[0].Namespace.Name != "lane" || got[0].PartOfSpeech != NamespacePart {
		t.Fatalf("NamespacesMatching: %+v", got)
	}
	if len(ops.NounsMatching("")) != 3 || len(ops.ActionsMatching("")) != 3 {
		t.Fatal("an empty prefix matches everything")
	}
}

func TestOperationJSONKeepsNounAndVerb(t *testing.T) {
	data, err := json.Marshal(Operation{Key: "ochazuke.input.undo", Description: "Undo"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m["noun"] != "ochazuke.input" || m["verb"] != "undo" || m["name"] != "undo" || m["namespace"] != "ochazuke" || m["key"] != "ochazuke.input.undo" || m["description"] != "Undo" {
		t.Fatalf("json: %s", data)
	}
	var back Operation
	if err := json.Unmarshal(data, &back); err != nil || back.Key.Short() != "ochazuke.input.undo" || back.Description != "Undo" {
		t.Fatalf("round trip: %+v %v", back, err)
	}
	if err := json.Unmarshal([]byte(`{"noun":"pane","verb":"kill"}`), &back); err != nil || back.Key.Short() != "pane.kill" {
		t.Fatalf("noun and verb read: %+v", back)
	}
}

// ochazuke.action list {noun} and ochazuke.noun list {action} read one
// relation from both sides.
func TestActionAndNounListsMirror(t *testing.T) {
	r := NewRuntime()
	r.Define("task").Verb("finish", func(Ctx) Result { return Done(nil) })
	r.Define("chore").Verb("finish", func(Ctx) Result { return Done(nil) })
	acts, err := r.Execute(Request{Noun: "ochazuke.action", Verb: "list", Args: Arguments{"noun": "task"}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(acts)
	if !strings.Contains(string(data), `"name":"finish"`) || !strings.Contains(string(data), `"chore"`) {
		t.Fatalf("action list {noun}: %s", data)
	}
	nouns, err := r.Execute(Request{Noun: "ochazuke.noun", Verb: "list", Args: Arguments{"action": "finish"}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(nouns)
	if !strings.Contains(string(data), `"noun":"task"`) || !strings.Contains(string(data), `"noun":"chore"`) || strings.Contains(string(data), `"noun":"ochazuke.input"`) {
		t.Fatalf("nouns list {action}: %s", data)
	}
}

// operationsWriters are the only functions that may write the registry's
// All, Order or Index.
var operationsWriters = map[string]bool{"Push": true, "Delete": true, "Invalidate": true, "Lookups": true, "NewOperations": true}

// registryWrites lists every write to an Operations' All, Order or Index
// in a Go file outside the writers: an assignment, an IncDec, a delete
// or clear call on one, read off a selector chain ending in Operations or
// an *Operations receiver.
func registryWrites(fset *token.FileSet, f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		receiver := ""
		if fn.Recv != nil && len(fn.Recv.List) == 1 && len(fn.Recv.List[0].Names) == 1 {
			if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
				if id, ok := star.X.(*ast.Ident); ok && id.Name == "Operations" {
					receiver = fn.Recv.List[0].Names[0].Name
					if operationsWriters[fn.Name.Name] {
						continue
					}
				}
			}
		}
		if fn.Recv == nil && operationsWriters[fn.Name.Name] {
			continue
		}
		isField := func(e ast.Expr) bool {
			for {
				switch x := e.(type) {
				case *ast.IndexExpr:
					e = x.X
					continue
				case *ast.SelectorExpr:
					if x.Sel.Name != "All" && x.Sel.Name != "Order" && x.Sel.Name != "Index" {
						return false
					}
					switch owner := x.X.(type) {
					case *ast.Ident:
						return receiver != "" && owner.Name == receiver
					case *ast.SelectorExpr:
						return owner.Sel.Name == "Operations"
					}
				}
				return false
			}
		}
		report := func(n ast.Node) { out = append(out, fset.Position(n.Pos()).String()+" in "+fn.Name.Name) }
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for _, l := range n.Lhs {
					if isField(l) {
						report(n)
					}
				}
			case *ast.IncDecStmt:
				if isField(n.X) {
					report(n)
				}
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && (id.Name == "delete" || id.Name == "clear") && len(n.Args) > 0 && isField(n.Args[0]) {
					report(n)
				}
			}
			return true
		})
	}
	return out
}

// Only Push, Delete and Invalidate (and the index build) write the
// registry; everything else registers through Register, Set or Push.
func TestOperationsWriters(t *testing.T) {
	var found []string
	err := filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") && p != "." || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		found = append(found, registryWrites(fset, f)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("the registry is written outside Push, Delete and Invalidate:\n%s", strings.Join(found, "\n"))
	}
	planted := `package x
func (ops *Operations) Sneak(k Key) { ops.All[k] = nil; ops.Index = nil }
func f(r *Runtime) { delete(r.Operations.All, "a.b.c"); r.Operations.Order = nil }
func (ops *Operations) Push(k Key) { ops.All[k] = nil }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "planted.go", planted, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := registryWrites(fset, f); len(got) != 4 {
		t.Fatalf("the guard must catch each planted write and allow Push: %v", got)
	}
}

// A declared singular namespace is used over the binary's name.
func TestDeclareNamespace(t *testing.T) {
	was, declared := DefaultNamespace, DeclaredNamespace
	t.Cleanup(func() { DefaultNamespace, DeclaredNamespace = was, declared })
	for _, bad := range []string{"", "Question", "a.b", FrameworkNamespace, AppNoun, "9x"} {
		if DeclareNamespace(bad) == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if err := DeclareNamespace("question"); err != nil || DefaultNamespace != "question" || DeclaredNamespace != "question" {
		t.Fatalf("declare: %v %s", err, DefaultNamespace)
	}
	ops := NewOperations(map[string]*Noun{})
	if op := ops.Register("question.ask", "", ""); op.Key != "question.question.ask" || op.Noun() != "question" {
		t.Fatalf("the declared namespace fills the key: %s", op.Key)
	}
}
