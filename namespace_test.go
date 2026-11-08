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
		{"collection.move", "collection", "move"},
		{"ochazuke.setting.set", "ochazuke.setting", "set"},
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
// the runtime prints (history, binding.list, operation.list) is the
// canonical key.
func TestFrameworkNounsAreNamespacedWithAliases(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	for noun := range r.Nouns {
		if noun != "app" && !strings.HasPrefix(noun, "ochazuke.") {
			t.Errorf("framework noun %q is not in the ochazuke namespace", noun)
		}
	}
	if r.Nouns["setting"] != nil || r.Nouns["ochazuke.setting"] == nil || r.Noun("setting") != r.Nouns["ochazuke.setting"] {
		t.Fatal("setting is not an alias of ochazuke.setting")
	}
	if got := r.CanonicalKey("collection.move"); got != "ochazuke.collection.move" {
		t.Fatalf("CanonicalKey(collection.move) = %q", got)
	}
	if got := r.DisplayKey("ochazuke.collection.move"); got != "collection.move" {
		t.Fatalf("DisplayKey = %q", got)
	}
	for _, q := range []Request{{Noun: "setting", Prop: "names"}, {Noun: "ochazuke.setting", Prop: "names"}} {
		if _, err := r.Execute(q); err != nil {
			t.Fatalf("%s: %v", q.Key(), err)
		}
	}
	entries := r.History(0)
	if len(entries) < 2 || entries[len(entries)-1].Request.Key() != "ochazuke.setting.names" || entries[len(entries)-2].Request.Key() != "ochazuke.setting.names" {
		t.Fatalf("history keys: %v", historyKeys(entries))
	}
	ops, err := r.Execute(Request{Noun: "operation", Verb: "list"})
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, op := range AnswerRows(ops).([]map[string]any) {
		keys[op["key"].(string)] = true
	}
	if !keys["ochazuke.setting.set"] || keys["setting.set"] || !keys["app.quit"] {
		t.Fatalf("operation.list is not canonical")
	}
	for _, row := range r.ListBindings("app") {
		if row.Request.Noun != "app" && !strings.HasPrefix(row.Request.Noun, "ochazuke.") {
			t.Fatalf("binding.list row %s runs %s", row.Keys, row.Request.Key())
		}
	}
	// The footer's hints keep the short names.
	if hints := BindingHints(r, "app"); !strings.Contains(hints, "palette.open") || strings.Contains(hints, "ochazuke.palette.open") {
		t.Fatalf("hints: %q", hints)
	}
	// A row built with the short name is stored canonical.
	r.Define("app").Mode("normal").Bind("z z", Do("collection.move", Arguments{"delta": 1}))
	if !slices.ContainsFunc(r.Nouns["app"].Bindings["normal"], func(b Binding) bool { return b.Keys == "z z" && b.Request.Key() == "ochazuke.collection.move" }) {
		t.Fatal("the z z row is not stored canonical")
	}
}

// Define of a short name extends the framework's noun; DefineOwn claims the
// short name for the application's own noun, and the framework's stays
// reachable by its canonical name, its verbs intact.
func TestDefineOwnShadowsTheFrameworkAlias(t *testing.T) {
	r := NewRuntime()
	done := func(v string) func(Ctx) Result { return func(Ctx) Result { return Done(v) } }
	r.Define("history-entry").Verb("capture", done("captured"))
	if _, ok := r.Operations.Find(Key("ochazuke.history-entry.capture")); !ok {
		t.Fatal("Define(history) did not extend ochazuke.history-entry")
	}
	r.DefineOwn("setting").Verb("list", done("own"))
	if r.Nouns["setting"] == nil || r.CanonicalNoun("setting") != "setting" {
		t.Fatal("DefineOwn did not claim setting")
	}
	got, err := r.Execute(Request{Noun: "setting", Verb: "list"})
	if err != nil || got != "own" {
		t.Fatalf("setting.list = %v, %v", got, err)
	}
	framework, err := r.Execute(Request{Noun: "ochazuke.setting", Verb: "list"})
	if err != nil || framework == "own" {
		t.Fatalf("ochazuke.setting.list = %v, %v", framework, err)
	}
	if _, err := r.Execute(Request{Noun: "setting", Prop: "names"}); err == nil {
		t.Fatal("the owned short name still reaches the framework's verbs")
	}
	if r.DisplayNoun("ochazuke.setting") != "ochazuke.setting" || r.DisplayNoun("ochazuke.collection") != "collection" {
		t.Fatalf("display names: %s %s", r.DisplayNoun("ochazuke.setting"), r.DisplayNoun("ochazuke.collection"))
	}
	shadowed := false
	for _, a := range r.Aliases() {
		if a.Alias == "setting" {
			shadowed = a.Shadowed && a.Noun == "ochazuke.setting"
		}
	}
	if !shadowed {
		t.Fatalf("Aliases: %+v", r.Aliases())
	}
	// Define of the owned name now returns the application's noun.
	if r.Define("setting").Noun() != r.Nouns["setting"] {
		t.Fatal("Define(setting) after DefineOwn left the application's noun")
	}
	// A row naming the short name runs the application's verb.
	r.Define("app").Mode("normal").Bind("z s", Do("setting.list"))
	if !slices.ContainsFunc(r.Nouns["app"].Bindings["normal"], func(b Binding) bool { return b.Keys == "z s" && b.Request.Key() == "setting.list" }) {
		t.Fatal("the z s row does not run the application's setting.list")
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
	if _, ok := r.Operations.Find(Key("tool.part.run")); !ok {
		t.Fatal("a nested noun beside its parent was refused")
	}
	// A name holds no dot: a key is namespace.noun.name.
	refused("a dotted verb", func() { r.Define("tool").Verb("base.add", done) })
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
	if err := os.WriteFile(filepath.Join(dir, BindingsFile), []byte(`[{"noun":"collection","mode":"normal","keys":"z z","request":{"noun":"collection","verb":"move","args":{"delta":1}}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ActionsFile), []byte(`[{"noun":"collection","verb":"echo","command":["echo","{{noun}}"]},{"noun":"collection","verb":"remote","base":["o-x"],"call":"ochazuke settings list"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if problems := r.EnsureUserActions(); len(problems) > 0 {
		t.Fatal(problems)
	}
	if problems := r.EnsureUserBindings(); len(problems) > 0 {
		t.Fatal(problems)
	}
	if _, ok := r.Operations.Find(Key("ochazuke.collection.echo")); !ok {
		t.Fatal("actions.json list.echo did not land on ochazuke.collection")
	}
	if !slices.ContainsFunc(r.Nouns["ochazuke.collection"].Bindings["normal"], func(b Binding) bool {
		return b.Keys == "z z" && b.Request.Key() == "ochazuke.collection.move"
	}) {
		t.Fatal("bindings.json row on list did not land on ochazuke.collection")
	}
	if a := r.Config().actions[0]; a.spelled() != "collection" || a.Noun != "ochazuke.collection" {
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
	for _, line := range []string{"ochazuke.setting names", "setting names", } {
		if c := p.parse(line); !c.Complete() || c.Op.Key.Short() != "ochazuke.setting.get" {
			t.Errorf("%q = %q err %v missing %q", line, c.Op.Key.Short(), c.Err, c.Missing)
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
	if !slices.Contains(words, "setting") || slices.Contains(words, "ochazuke.setting") {
		t.Fatalf("completions %v", words)
	}
	// An application noun of its own takes the short name; the framework's
	// is the dotted word.
	r.DefineOwn("setting").Verb("list", func(Ctx) Result { return Done("own") })
	if c := p.parse("setting list"); c.Op.Key.Short() != "setting.list" {
		t.Fatalf("owned setting list = %q", c.Op.Key.Short())
	}
	if c := p.parse("ochazuke.setting names"); c.Op.Key.Short() != "ochazuke.setting.get" {
		t.Fatalf("ochazuke.setting names = %q (%v)", c.Op.Key.Short(), c.Err)
	}
}

// The app noun's verbs and the framework's nouns never collide: an
// application verb state on app stands beside the framework noun
// ochazuke.snapshot, each reachable by its own name (o-qbt's app.state).
func TestAppVerbBesideFrameworkNounOfThatName(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	if r.Nouns["ochazuke.snapshot"] == nil {
		t.Fatal("no framework state noun")
	}
	r.Define("app").Verb("state", func(Ctx) Result { return Done("own") })
	got, err := r.Execute(Request{Noun: "app", Verb: "state"})
	if err != nil || got != "own" {
		t.Fatalf("app.state = %v, %v", got, err)
	}
	for _, noun := range []string{"snapshot", "ochazuke.snapshot"} {
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
	if got := r.CanonicalKey("app.setting.names"); got != "ochazuke.setting.names" {
		t.Fatalf("CanonicalKey(app.setting.names) = %q", got)
	}
	if got := r.CanonicalKey("app.quit"); got != "app.quit" {
		t.Fatalf("CanonicalKey(app.quit) = %q", got)
	}
	if got := r.CanonicalNoun("app.nothing"); got != "app.nothing" {
		t.Fatalf("CanonicalNoun(app.nothing) = %q", got)
	}
	if _, err := r.Execute(Request{Noun: "app.setting", Prop: "names"}); err != nil {
		t.Fatal(err)
	}
	if entries := r.History(0); len(entries) == 0 || entries[len(entries)-1].Request.Key() != "ochazuke.setting.names" {
		t.Fatalf("history keys: %v", historyKeys(entries))
	}
	if noun, rest, ok := r.NounWords([]string{"app", "setting", "names"}); !ok || noun != "ochazuke.setting" || !slices.Equal(rest, []string{"names"}) {
		t.Fatalf("NounWords(app setting names) = %q %v %v", noun, rest, ok)
	}
	if err := os.WriteFile(filepath.Join(dir, BindingsFile), []byte(`[{"noun":"app.collection","mode":"normal","keys":"z z","request":{"noun":"app.collection","verb":"move","args":{"delta":1}}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if problems := r.EnsureUserBindings(); len(problems) > 0 {
		t.Fatal(problems)
	}
	if !slices.ContainsFunc(r.Nouns["ochazuke.collection"].Bindings["normal"], func(b Binding) bool {
		return b.Keys == "z z" && b.Request.Key() == "ochazuke.collection.move"
	}) {
		t.Fatal("a v0.16.0 bindings.json row on app.collection did not land on ochazuke.collection")
	}
}
