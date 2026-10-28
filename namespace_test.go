package ochazuke

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSplitKey(t *testing.T) {
	for _, tc := range []struct{ key, noun, verb string }{
		{"list.move", "list", "move"},
		{"ochazuke.settings.set", "ochazuke.settings", "set"},
		{"ai.skills.list", "ai.skills", "list"},
		{"app.quit", "app", "quit"},
		{"list", "list", ""},
		{"", "", ""},
	} {
		if noun, verb := SplitKey(tc.key); noun != tc.noun || verb != tc.verb {
			t.Errorf("SplitKey(%q) = %q, %q; want %q, %q", tc.key, noun, verb, tc.noun, tc.verb)
		}
		q := Do(tc.key).Request
		if q.Noun != tc.noun || q.Verb != tc.verb {
			t.Errorf("Do(%q) = %s / %s", tc.key, q.Noun, q.Verb)
		}
	}
	if _, _, ok := CutKey("list"); ok {
		t.Error("CutKey without a dot is ok")
	}
	if got := Namespaced("ai", "skills"); got != "ai.skills" {
		t.Errorf("Namespaced = %q", got)
	}
}

// Every framework noun is ochazuke.<noun> with its short name as an alias: the
// short name reaches it from Execute, Do, Noun and the palette, and what
// the runtime prints (history, bindings.list, operations.list) is the
// canonical key.
func TestFrameworkNounsAreNamespacedWithAliases(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	for noun := range r.Nouns {
		if noun != "app" && !strings.HasPrefix(noun, "ochazuke.") {
			t.Errorf("framework noun %q is not in the ochazuke namespace", noun)
		}
	}
	if r.Nouns["settings"] != nil || r.Nouns["ochazuke.settings"] == nil || r.Noun("settings") != r.Nouns["ochazuke.settings"] {
		t.Fatal("settings is not an alias of ochazuke.settings")
	}
	if got := r.CanonicalKey("list.move"); got != "ochazuke.list.move" {
		t.Fatalf("CanonicalKey(list.move) = %q", got)
	}
	if got := r.DisplayKey("ochazuke.list.move"); got != "list.move" {
		t.Fatalf("DisplayKey = %q", got)
	}
	for _, q := range []Request{{Noun: "settings", Verb: "names"}, {Noun: "ochazuke.settings", Verb: "names"}} {
		if _, err := r.Execute(q); err != nil {
			t.Fatalf("%s: %v", q.Key(), err)
		}
	}
	entries := r.History(0)
	if len(entries) < 2 || entries[len(entries)-1].Request.Key() != "ochazuke.settings.names" || entries[len(entries)-2].Request.Key() != "ochazuke.settings.names" {
		t.Fatalf("history keys: %v", historyKeys(entries))
	}
	ops, err := r.Execute(Request{Noun: "operations", Verb: "list"})
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, op := range ops.([]Operation) {
		keys[op.Key()] = true
	}
	if !keys["ochazuke.settings.set"] || keys["settings.set"] || !keys["app.quit"] {
		t.Fatalf("operations.list is not canonical")
	}
	for _, row := range r.ListBindings("app") {
		if row.Request.Noun != "app" && !strings.HasPrefix(row.Request.Noun, "ochazuke.") {
			t.Fatalf("bindings.list row %s runs %s", row.Keys, row.Request.Key())
		}
	}
	// The footer's hints keep the short names.
	if hints := BindingHints(r, "app"); !strings.Contains(hints, "palette.open") || strings.Contains(hints, "ochazuke.palette.open") {
		t.Fatalf("hints: %q", hints)
	}
	// A row built with the short name is stored canonical.
	r.Define("app").Mode("normal").Bind("z z", Do("list.move", Arguments{"delta": 1}))
	if !slices.ContainsFunc(r.Nouns["app"].Bindings["normal"], func(b Binding) bool { return b.Keys == "z z" && b.Request.Key() == "ochazuke.list.move" }) {
		t.Fatal("the z z row is not stored canonical")
	}
}

// Define of a short name extends the framework's noun; DefineOwn claims the
// short name for the application's own noun, and the framework's stays
// reachable by its canonical name, its verbs intact.
func TestDefineOwnShadowsTheFrameworkAlias(t *testing.T) {
	r := NewRuntime()
	done := func(v string) func(Ctx) Result { return func(Ctx) Result { return Done(v) } }
	r.Define("history").Verb("capture", done("captured"))
	if _, ok := r.Operations["ochazuke.history.capture"]; !ok {
		t.Fatal("Define(history) did not extend ochazuke.history")
	}
	r.DefineOwn("settings").Verb("list", done("own"))
	if r.Nouns["settings"] == nil || r.CanonicalNoun("settings") != "settings" {
		t.Fatal("DefineOwn did not claim settings")
	}
	got, err := r.Execute(Request{Noun: "settings", Verb: "list"})
	if err != nil || got != "own" {
		t.Fatalf("settings.list = %v, %v", got, err)
	}
	framework, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "list"})
	if err != nil || framework == "own" {
		t.Fatalf("ochazuke.settings.list = %v, %v", framework, err)
	}
	if _, err := r.Execute(Request{Noun: "settings", Verb: "names"}); err == nil {
		t.Fatal("the owned short name still reaches the framework's verbs")
	}
	if r.DisplayNoun("ochazuke.settings") != "ochazuke.settings" || r.DisplayNoun("ochazuke.list") != "list" {
		t.Fatalf("display names: %s %s", r.DisplayNoun("ochazuke.settings"), r.DisplayNoun("ochazuke.list"))
	}
	shadowed := false
	for _, a := range r.Aliases() {
		if a.Alias == "settings" {
			shadowed = a.Shadowed && a.Noun == "ochazuke.settings"
		}
	}
	if !shadowed {
		t.Fatalf("Aliases: %+v", r.Aliases())
	}
	// Define of the owned name now returns the application's noun.
	if r.Define("settings").Noun() != r.Nouns["settings"] {
		t.Fatal("Define(settings) after DefineOwn left the application's noun")
	}
	// A row naming the short name runs the application's verb.
	r.Define("app").Mode("normal").Bind("z s", Do("settings.list"))
	if !slices.ContainsFunc(r.Nouns["app"].Bindings["normal"], func(b Binding) bool { return b.Keys == "z s" && b.Request.Key() == "settings.list" }) {
		t.Fatal("the z s row does not run the application's settings.list")
	}
}

// A noun x and a noun x.y coexist, but a verb y on x beside a noun x.y
// (or a noun x.y beside a verb y on x) is refused, and a verb is one word.
func TestOverlappingVerbsAndNounsAreRefused(t *testing.T) {
	refused := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s was accepted", name)
			}
		}()
		fn()
	}
	r := NewRuntime()
	done := func(Ctx) Result { return Done(nil) }
	r.Define("tool").Verb("run", done)
	refused("noun tool.run beside the verb run on tool", func() { r.Define("tool.run") })
	refused("an empty noun part", func() { r.Define("tool..x") })
	r.Define("tool.part").Verb("run", done)
	if _, ok := r.Operations["tool.part.run"]; !ok {
		t.Fatal("a nested noun beside its parent was refused")
	}
	// A verb may hold a dot (the table is flat; a name is looked up whole),
	// but one name never means two actions.
	r.Define("tool").Verb("base.add", done)
	if op, ok := r.Operation("tool.base.add"); !ok || op.Noun != "tool" || op.Verb != "base.add" {
		t.Fatalf("dotted verb: %+v %v", op, ok)
	}
	refused("a dotted verb naming a nested noun's verb", func() { r.Define("tool").Verb("part.run", done) })
}

// A binding file and actions.json name framework nouns by their short
// names; the rows and the verbs land on the canonical nouns. The old
// two-word verb.noun spelling is still rewritten; a longer key is never
// swapped.
func TestShortNamesInFilesAndLegacySpelling(t *testing.T) {
	dir := t.TempDir()
	r := NewRuntime()
	r.Config().Dir = dir
	if err := os.WriteFile(filepath.Join(dir, BindingsFile), []byte(`[{"noun":"list","mode":"normal","keys":"z z","request":{"noun":"list","verb":"move","args":{"delta":1}}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ActionsFile), []byte(`[{"noun":"list","verb":"echo","command":["echo","{{noun}}"]},{"noun":"list","verb":"remote","base":["o-x"],"call":"ochazuke settings list"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if problems := r.EnsureUserActions(); len(problems) > 0 {
		t.Fatal(problems)
	}
	if problems := r.EnsureUserBindings(); len(problems) > 0 {
		t.Fatal(problems)
	}
	if _, ok := r.Operations["ochazuke.list.echo"]; !ok {
		t.Fatal("actions.json list.echo did not land on ochazuke.list")
	}
	if !slices.ContainsFunc(r.Nouns["ochazuke.list"].Bindings["normal"], func(b Binding) bool {
		return b.Keys == "z z" && b.Request.Key() == "ochazuke.list.move"
	}) {
		t.Fatal("bindings.json row on list did not land on ochazuke.list")
	}
	if a := r.Config().actions[0]; a.spelled() != "list" || a.Noun != "ochazuke.list" {
		t.Fatalf("action keeps its spelling: %q %q", a.spelled(), a.Noun)
	}
	legacy := r.modernRequest(Request{Noun: "undo", Verb: "input"})
	if legacy.Key() != "ochazuke.input.undo" {
		t.Fatalf("verb.noun rewrite: %s", legacy.Key())
	}
	if long := r.modernRequest(Request{Noun: "x.undo", Verb: "input"}); long.Key() != "x.undo.input" {
		t.Fatalf("a three-word key was swapped: %s", long.Key())
	}
}

// The palette reads a dotted noun as one word, and the short name too;
// completions offer the short name only.
func TestPaletteDottedNounWord(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	p := m.Palette
	for _, line := range []string{"ochazuke.settings names", "settings names", "names ochazuke.settings", "names settings"} {
		if c := p.parse(line); !c.Complete() || c.Op.Key() != "ochazuke.settings.names" {
			t.Errorf("%q = %q err %v missing %q", line, c.Op.Key(), c.Err, c.Missing)
		}
	}
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	if err := p.setText("sett"); err != nil {
		t.Fatal(err)
	}
	var words []string
	for _, e := range p.completions() {
		words = append(words, e.Value)
	}
	if !slices.Contains(words, "settings") || slices.Contains(words, "ochazuke.settings") {
		t.Fatalf("completions %v", words)
	}
	// An application noun of its own takes the short name; the framework's
	// is the dotted word.
	r.DefineOwn("settings").Verb("list", func(Ctx) Result { return Done("own") })
	if c := p.parse("settings list"); c.Op.Key() != "settings.list" {
		t.Fatalf("owned settings list = %q", c.Op.Key())
	}
	if c := p.parse("ochazuke.settings names"); c.Op.Key() != "ochazuke.settings.names" {
		t.Fatalf("ochazuke.settings names = %q (%v)", c.Op.Key(), c.Err)
	}
}

// The app noun's verbs and the framework's nouns never collide: an
// application verb state on app stands beside the framework noun
// ochazuke.state, each reachable by its own name (o-qbt's app.state).
func TestAppVerbBesideFrameworkNounOfThatName(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	if r.Nouns["ochazuke.state"] == nil {
		t.Fatal("no framework state noun")
	}
	r.Define("app").Verb("state", func(Ctx) Result { return Done("own") })
	got, err := r.Execute(Request{Noun: "app", Verb: "state"})
	if err != nil || got != "own" {
		t.Fatalf("app.state = %v, %v", got, err)
	}
	for _, noun := range []string{"state", "ochazuke.state"} {
		got, err := r.Execute(Request{Noun: noun, Verb: "get"})
		if err != nil || got == "own" {
			t.Fatalf("%s.get = %v, %v", noun, got, err)
		}
	}
	// The v0.16.0 spelling app.state.get is shadowed by the app verb.
	if got := r.CanonicalKey("app.state.get"); got != "app.state.get" {
		t.Fatalf("CanonicalKey(app.state.get) = %q with an app.state verb", got)
	}
	if noun, rest, ok := r.NounWords([]string{"app", "state"}); !ok || noun != "app" || !slices.Equal(rest, []string{"state"}) {
		t.Fatalf("NounWords(app state) = %q %v %v", noun, rest, ok)
	}
}

// The v0.16.0 spelling app.<noun> of a framework noun still reaches it, for
// one release: keys, binding files and restored history read it as
// ochazuke.<noun>; history prints the canonical key.
func TestLegacyAppNamespaceIsAnAlias(t *testing.T) {
	dir := t.TempDir()
	r := NewRuntime()
	r.Config().Dir = dir
	NewModel(r)
	if got := r.CanonicalKey("app.settings.names"); got != "ochazuke.settings.names" {
		t.Fatalf("CanonicalKey(app.settings.names) = %q", got)
	}
	if got := r.CanonicalKey("app.quit"); got != "app.quit" {
		t.Fatalf("CanonicalKey(app.quit) = %q", got)
	}
	if got := r.CanonicalNoun("app.nothing"); got != "app.nothing" {
		t.Fatalf("CanonicalNoun(app.nothing) = %q", got)
	}
	if _, err := r.Execute(Request{Noun: "app.settings", Verb: "names"}); err != nil {
		t.Fatal(err)
	}
	if entries := r.History(0); len(entries) == 0 || entries[len(entries)-1].Request.Key() != "ochazuke.settings.names" {
		t.Fatalf("history keys: %v", historyKeys(entries))
	}
	if noun, rest, ok := r.NounWords([]string{"app", "settings", "names"}); !ok || noun != "ochazuke.settings" || !slices.Equal(rest, []string{"names"}) {
		t.Fatalf("NounWords(app settings names) = %q %v %v", noun, rest, ok)
	}
	if err := os.WriteFile(filepath.Join(dir, BindingsFile), []byte(`[{"noun":"app.list","mode":"normal","keys":"z z","request":{"noun":"app.list","verb":"move","args":{"delta":1}}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if problems := r.EnsureUserBindings(); len(problems) > 0 {
		t.Fatal(problems)
	}
	if !slices.ContainsFunc(r.Nouns["ochazuke.list"].Bindings["normal"], func(b Binding) bool {
		return b.Keys == "z z" && b.Request.Key() == "ochazuke.list.move"
	}) {
		t.Fatal("a v0.16.0 bindings.json row on app.list did not land on ochazuke.list")
	}
}
