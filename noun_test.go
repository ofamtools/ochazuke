package ochazuke

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"ochazuke/internal/keytest"
)

// focusOf is the leaf FocusID reports for a context: its input, else its last
// path element.
func focusOf(c Context) string {
	if c.InputID != "" {
		return c.InputID
	}
	return c.Path[len(c.Path)-1]
}

// layerActionFor is the ExecuteAt loop: innermost layer on the path that
// overrides the operation.
func layerActionFor(r *Runtime, c Context, key string) Handler {
	for i := len(c.Path) - 1; i >= 0; i-- {
		if fn := r.Layers[c.Path[i]].Actions[key]; fn != nil {
			return fn
		}
	}
	return nil
}

func handlerPointer(fn Handler) uintptr {
	if fn == nil {
		return 0
	}
	return reflect.ValueOf(fn).Pointer()
}

// assertNounModelAgrees checks that the instance graph rebuilds the active
// context and that the noun walk resolves bindings and actions exactly as the
// layer path does, for every frame the runtime holds.
func assertNounModelAgrees(t *testing.T, r *Runtime, step string) {
	t.Helper()
	if got, want := r.ContextFromFocus(), r.Context(); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: ContextFromFocus %+v; Context %+v", step, got, want)
	}
	if ambiguous := r.AmbiguousInstances(); len(ambiguous) != 0 {
		t.Fatalf("%s: ambiguous parents: %v", step, ambiguous)
	}
	var contexts []Context
	for _, f := range r.Frames {
		contexts = append(contexts, f.Context)
	}
	for _, page := range r.Pages {
		contexts = append(contexts, page.Frame.Context)
	}
	for _, c := range contexts {
		focus := focusOf(c)
		if got, want := r.EffectiveBindingsFor(focus, r.Mode(c)), r.EffectiveBindings(c); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: bindings for %+v differ\n new %v\n old %v", step, c, got, want)
		}
		for key := range r.Operations {
			if got, want := handlerPointer(r.ActionFor(focus, key)), handlerPointer(layerActionFor(r, c, key)); got != want {
				t.Fatalf("%s: action %s for %+v resolves differently", step, key, c)
			}
		}
	}
}

func TestLayerIsAViewOverItsNoun(t *testing.T) {
	r := NewRuntime()
	r.Layers["input"] = &Layer{ID: "input"}
	r.Layers["page"] = &Layer{ID: "page", Actions: map[string]Handler{}}
	r.Layers["page"].Bindings = []Binding{{Keys: "x", Request: Request{Verb: "quit", Noun: "app"}}, {Mode: "normal", Keys: "y", Request: Request{Verb: "quit", Noun: "app"}}}
	r.Layers["page"].Actions["quit.app"] = func(*Runtime, Context, Arguments) (any, error) { return "page", nil }
	noun := r.Noun("page")
	if noun == nil || len(noun.Bindings[""]) != 1 || len(noun.Bindings["normal"]) != 1 || noun.Bindings["normal"][0].Keys != "y" {
		t.Fatalf("noun table not derived from layer rows: %+v", noun)
	}
	if reflect.ValueOf(noun.Actions).Pointer() != reflect.ValueOf(r.Layers["page"].Actions).Pointer() {
		t.Fatal("noun and layer must share one action map")
	}
	if r.Noun("input") == nil || r.Noun("input").Actions != nil {
		t.Fatal("a layer without actions is a noun without actions")
	}
	if _, err := r.Execute(Request{Verb: "set", Noun: "binding", Args: Arguments{"layer": "page", "mode": "insert", "keys": "z", "request": `{"verb":"quit","noun":"app"}`}}); err != nil {
		t.Fatal(err)
	}
	if rows := r.Noun("page").Bindings["insert"]; len(rows) != 1 || rows[0].Keys != "z" {
		t.Fatalf("set.binding did not reach the noun table: %+v", rows)
	}
	if err := r.RegisterNoun(&Noun{Name: "panel", Verbs: []string{"open"}, Bindings: map[string][]Binding{"normal": {{Mode: "normal", Keys: "o", Request: Request{Verb: "quit", Noun: "app"}}}}}); err != nil {
		t.Fatal(err)
	}
	if layer := r.Layers["panel"]; layer == nil || len(layer.Bindings) != 1 || layer.Actions == nil {
		t.Fatalf("RegisterNoun must create the layer view: %+v", layer)
	}
	if err := r.RegisterNoun(&Noun{Name: "page"}); err == nil {
		t.Fatal("a noun cannot shadow an existing layer")
	}
	if r.Noun("panel").Verbs[0] != "open" {
		t.Fatal("declared verbs must survive a sync")
	}
	delete(r.Layers, "panel")
	if r.Noun("panel") != nil || r.Instances["panel"] != nil {
		t.Fatal("a removed layer removes its noun and instance")
	}
}

func TestResolveWalksAncestryWithLayerPrecedence(t *testing.T) {
	r := NewRuntime()
	for _, id := range []string{"page", "list", "row"} {
		r.Layers[id] = &Layer{ID: id, Actions: map[string]Handler{}}
	}
	quit := func(id string) Request { return Request{Verb: "quit", Noun: "app", Args: Arguments{"from": id}} }
	r.Layers["app"].Bindings = []Binding{{Mode: "normal", Keys: "x", Request: quit("app-normal")}, {Keys: "y", Request: quit("app-any")}}
	r.Layers["page"].Bindings = []Binding{{Keys: "x", Request: quit("page-any")}, {Mode: "normal", Keys: "y", Request: quit("page-normal")}, {Mode: "normal", Keys: "y", Request: quit("page-normal-last")}}
	r.Layers["row"].Bindings = []Binding{{Mode: "insert", Keys: "x", Request: quit("row-insert")}}
	if err := r.SelectContext(Context{Path: []string{"app", "page", "list", "row"}}); err != nil {
		t.Fatal(err)
	}
	assertNounModelAgrees(t, r, "select")
	got := map[string]string{}
	for _, b := range r.EffectiveBindingsFor("row", "normal") {
		got[b.Keys] = StringArg(b.Request.Args, "from", "")
	}
	if want := map[string]string{"x": "page-any", "y": "page-normal-last"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("precedence: got %v; want %v", got, want)
	}
	if b := r.EffectiveBindingsFor("row", "insert"); len(b) != 2 || StringArg(b[0].Request.Args, "from", "") != "row-insert" {
		t.Fatalf("mode rows of the innermost noun must win: %+v", b)
	}
	page := func(*Runtime, Context, Arguments) (any, error) { return "page", nil }
	r.Layers["page"].Actions["quit.app"] = page
	if handlerPointer(r.ActionFor("row", "quit.app")) != handlerPointer(page) || r.ActionFor("row", "get.state") != nil || r.ActionFor("nowhere", "quit.app") != nil {
		t.Fatal("ActionFor must return the innermost override or nil")
	}
	if got := r.Ancestry("row"); !reflect.DeepEqual(got, []string{"row", "list", "page", "app"}) {
		t.Fatalf("ancestry %v", got)
	}
	if got := r.InstancePath("row"); !reflect.DeepEqual(got, []string{"app", "page", "list", "row"}) {
		t.Fatalf("instance path %v", got)
	}
	if r.Ancestry("missing") != nil {
		t.Fatal("unknown instance has no ancestry")
	}
}

func TestInstancesForInputsAndSharedIDs(t *testing.T) {
	r := NewRuntime()
	r.Layers["editor"] = &Layer{ID: "editor"}
	r.Layers["form"] = &Layer{ID: "form"}
	if err := r.AddInput(NewInput("editor", "", true), []string{"app", "editor"}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddInput(NewInput("title", "", false), []string{"app", "form"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"editor", "title"} {
		if err := r.Focus(id); err != nil {
			t.Fatal(err)
		}
		assertNounModelAgrees(t, r, "focus "+id)
		if r.FocusID() != id {
			t.Fatalf("FocusID %q; want %q", r.FocusID(), id)
		}
	}
	if inst := r.Instances["title"]; inst.Parent != "form" || inst.Noun != "" {
		t.Fatalf("input instance: %+v", inst)
	}
	if inst := r.Instances["editor"]; inst.Parent != "app" || inst.Noun != "editor" {
		t.Fatalf("shared id instance: %+v", inst)
	}
	// A second parent for one id is recorded, not guessed at.
	if err := r.SelectContext(Context{Path: []string{"app", "editor", "form"}}); err != nil {
		t.Fatal(err)
	}
	if got := r.AmbiguousInstances(); len(got) != 1 || !strings.HasPrefix(got[0], "form: parent app") {
		t.Fatalf("ambiguity not recorded: %v", got)
	}
	if got := r.ContextFromFocus(); !reflect.DeepEqual(got.Path, []string{"app", "form"}) {
		t.Fatalf("first parent wins: %+v", got)
	}
}

func TestSnapshotExposesNounsAndInstances(t *testing.T) {
	r := NewDemo().Runtime
	nouns, err := JSONPointer(r.Snapshot(), "/nouns/playground/bindings/normal")
	if err != nil {
		t.Fatal(err)
	}
	if rows, ok := nouns.([]any); !ok || len(rows) == 0 {
		t.Fatalf("playground normal rows: %#v", nouns)
	}
	parent, err := JSONPointer(r.Snapshot(), "/instances/directory/parent")
	if err != nil || parent != "agent" {
		t.Fatalf("instance parent %v, %v", parent, err)
	}
}

// Rule for phase 5: the new walk must agree with the layer path on every
// context a real session produces. This drives the demo through pages, fields,
// nested modals, the yank history, the palette, the picker and sequences.
func TestNounModelMatchesLayerModelInDemo(t *testing.T) {
	m := NewDemo()
	r := m.Runtime
	r.BindingTimeout = time.Hour
	assertNounModelAgrees(t, r, "start")
	step := func(label string, run func()) {
		t.Helper()
		run()
		if m.Status != "" {
			t.Fatalf("%s: %s", label, m.Status)
		}
		assertNounModelAgrees(t, r, label)
	}
	press := func(keys ...string) {
		t.Helper()
		for _, key := range keys {
			step("key "+key, func() { keytest.Press(m, key) })
		}
	}
	execute := func(verb, noun string, args Arguments) {
		t.Helper()
		step(verb+"."+noun, func() {
			if _, err := r.Execute(Request{Verb: verb, Noun: noun, Args: args}); err != nil {
				t.Fatalf("%s.%s: %v", verb, noun, err)
			}
		})
	}
	press("ctrl+j", "i", "h", "i", "esc", "y", "y", "tab", "tab", "shift+tab", "ctrl+x")
	press("ctrl+n", "i", "x", "esc", "ctrl+n", "ctrl+g", "ctrl+g")
	press("ctrl+y", "esc")
	execute("open", "palette", nil)
	press("p", "a")
	execute("close", "palette", nil)
	press("?", "ctrl+g")
	execute("focus", "input", Arguments{"id": "directory"})
	press("ctrl+o")
	execute("close", "picker", nil)
	press("g")
	execute("select", "context", Arguments{"path": `["app","playground","agent"]`})
	press("ctrl+j", "j", "k", "ctrl+j", "t", "d", "ctrl+j", "ctrl+k", "ctrl+k")
	execute("open", "page", Arguments{"id": "playground"})
	execute("push", "modal", Arguments{"id": "draft", "input": "dialog"})
	execute("pop", "modal", nil)
}
