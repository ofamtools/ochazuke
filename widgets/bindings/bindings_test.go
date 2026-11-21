package bindings

import (
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/table"
)

func bind(scope, mode, keys, command string) core.Binding {
	return core.Binding{Scope: scope, Mode: mode, Keys: strings.Fields(keys), Command: command}
}

func TestFindFreeBinding(t *testing.T) {
	modes := []core.Mode{{Name: "normal"}, {Name: "visual", Base: "normal"}, {Name: "insert", Passthrough: true}}
	bindings := []core.Binding{
		bind("app", "normal", "a", "app.a"),
		bind("card", "visual", "b x", "card.b"),
		bind("", "", "c", "global.c"),
		bind("other", "normal", "d", "other.d"),
		bind("card", "insert", "d", "insert.d"),
		bind("page", "normal", "d", "page.d"),
	}
	got, ok := FindFreeBinding(modes, bindings, "card", "visual", false, nil)
	if !ok || !slices.Equal(got, []string{"d"}) {
		t.Fatalf("focused: %v", got)
	}
	chain := []core.ContextFrame{{Scope: "card"}, {Scope: "page"}, {Scope: "app"}}
	got, ok = FindFreeBindingInChain(modes, bindings, chain, "visual", false, nil)
	if !ok || !slices.Equal(got, []string{"e"}) {
		t.Fatalf("with parent: %v", got)
	}

	keys := []core.Binding{bind("", "insert", "ctrl+a", "a"), bind("", "insert", "ctrl+b", "b")}
	keys[0].Phase = core.PhaseCapture // A declared key stays occupied in passthrough.
	got, ok = FindFreeBinding(modes, keys, "card", "insert", true, nil)
	if !ok || !slices.Equal(got, []string{"ctrl+d"}) {
		t.Fatalf("insert skipped reservations: %v", got)
	}
	for _, r := range letters {
		for _, mod := range []string{"ctrl+", "alt+"} {
			keys = append(keys, bind("", "insert", mod+string(r), "mod"))
		}
	}
	got, ok = FindFreeBinding(modes, keys, "", "insert", true, nil)
	if !ok || !slices.Equal(got, []string{"ctrl+a", "a"}) {
		t.Fatalf("insert chord: %v", got)
	}
	if got, ok = FindFreeBinding(nil, []core.Binding{{Class: core.ClassAny, Command: "any"}}, "", "normal", false, nil); ok || got != nil {
		t.Fatalf("exhausted: %v", got)
	}
}

func TestFindFreeBindingAfterSinglesAndResolve(t *testing.T) {
	var list []core.Binding
	for _, r := range letters + strings.ToUpper(letters) + symbols {
		list = append(list, bind("", "normal", string(r), "single"))
	}
	list = append(list, bind("", "normal", "g a", "in.use"), bind("", "normal", "g b z", "longer"))
	keys, ok := FindFreeBinding(nil, list, "", "normal", false, nil)
	if !ok || !slices.Equal(keys, []string{"g", "c"}) {
		t.Fatalf("suggestion = %v", keys)
	}
	km, err := core.NewKeymap(nil, append(list, core.Binding{Mode: "normal", Keys: keys, Command: "suggested"}))
	if err != nil {
		t.Fatal(err)
	}
	first := km.Resolve(core.KeyQuery{Mode: "normal", Key: "g"})
	if first.Outcome != core.Pending || first.Input.Fallback.ID != "single" {
		t.Fatalf("first stroke %+v", first)
	}
	last := km.Resolve(core.KeyQuery{Mode: "normal", Input: first.Input, Key: "c"})
	if last.Outcome != core.Matched || last.Command.ID != "suggested" {
		t.Fatalf("second stroke %+v", last)
	}
}

func TestAnalyzeBindings(t *testing.T) {
	modes := []core.Mode{{Name: "normal"}, {Name: "visual", Base: "normal"}}
	bs := []core.Binding{
		bind("card", "normal", "x", "ok"),
		bind("card", "normal", "x", "ok"),
		bind("card", "normal", "x y", "ok"),
		bind("card", "normal", "x", "ok"),
		bind("other", "normal", "x", "ok"),
		bind("", "normal", "F12", "missing"),
		bind("app", "normal", "z", "ok"),
		bind("card", "visual", "z", "ok"),
	}
	bs[1].Phase = core.PhaseBubble
	bs[3].When = "("
	bs[6].Phase = core.PhaseCapture
	rows := AnalyzeBindings(modes, bs, nil, core.CommandSpec{ID: "ok"})
	check := func(i int, want ...string) {
		t.Helper()
		var got []string
		for _, c := range rows[i].Conflicts {
			if !slices.Contains(got, c.Kind) {
				got = append(got, c.Kind)
			}
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("row %d conflicts %v want %v", i, got, want)
		}
	}
	check(0, "duplicate", "prefix-collision", "same-key-diff-when")
	check(1, "duplicate", "shadowed", "prefix-collision", "same-key-diff-when")
	check(2, "prefix-collision")
	check(3, "bad-when", "same-key-diff-when", "prefix-collision")
	check(4)
	check(5, "reserved", "unknown-command")
	check(6)
	check(7, "shadowed")
}

func TestLeaderOutcomes(t *testing.T) {
	modes := []core.Mode{{Name: "normal"}, {Name: "terminal", Passthrough: true}, {Name: "pane-commands"}}
	bs := []core.Binding{
		{Scope: "terminal", Mode: "terminal", Keys: []string{"ctrl+a"}, Layer: &core.Layer{Name: "leader", Modes: []string{"pane-commands", "normal"}, ExcludeTags: []string{"editing"}}},
		bind("", "normal", "a", "open"),
		bind("", "normal", "b", "edit"),
		bind("", "normal", "c", "open"),
		bind("", "pane-commands", "b", "open"),
		bind("", "pane-commands", "c", "open"),
		bind("", "pane-commands", "d", "open"),
		bind("", "normal", "e", "edit"),
		{Mode: "pane-commands", Class: core.ClassDigit, Command: "open"},
	}
	bs[2].Args = map[string]any{"id": "b"}
	bs[5].Args = map[string]any{"id": "c"}
	bs[6].When = "facts.allow" // Static leader analysis requires no runtime facts.
	rows := AnalyzeBindings(modes, bs, nil, core.CommandSpec{ID: "open"}, core.CommandSpec{ID: "edit", Tags: []string{"editing"}})
	for i, want := range []string{"", "mirror", "differs", "differs", "differs", "differs", "leader-only", "", "leader-only"} {
		if strings.Join(rows[i].Leader, ",") != want {
			t.Errorf("row %d: %+v want %q", i, rows[i], want)
		}
	}
}

func TestFilteredEditingAndHistory(t *testing.T) {
	a, b := bind("app", "normal", "a", "file.open"), bind("editor", "insert", "b", "file.save")
	b.When, b.Hint = "facts.dirty", "Write file"
	m := New(nil, []core.Binding{a, b}, core.CommandSpec{ID: "file.open"})
	tests := []struct {
		query string
		mode  table.MatchMode
		want  []int
	}{
		{"scope:app mode:normal key:a", table.MatchSubstring, []int{0}},
		{"verb:save when:dirty conflict:unknown-command", table.MatchSubstring, []int{1}},
		{"Write file", table.MatchSubstring, []int{1}},
		{"fsv", table.MatchFuzzy, []int{1}},
		{"^file[.]op", table.MatchRegex, []int{0}},
	}
	for _, tc := range tests {
		m.SetMatchMode(tc.mode)
		m.SetQuery(tc.query)
		if got := m.Filtered(); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v", tc.query, got)
		}
	}
	m.SetQuery("[")
	if m.FilterError() == "" {
		t.Fatal("missing regex error")
	}
	m.SetMatchMode(table.MatchSubstring)
	m.SetQuery("")
	m.HandleCommand(core.Command{ID: "motion.down"})
	if i, ok := m.Focused(); !ok || i != 1 {
		t.Fatal(i)
	}
	if !m.HandleCommand(core.Command{ID: "bindings.unbind"}) || !m.Bindings()[1].Unbind {
		t.Fatal("unbind")
	}
	if !m.Rebind(1, []string{"y"}) {
		t.Fatal("rebind")
	}
	km, err := m.Keymap()
	if err != nil {
		t.Fatal(err)
	}
	q := core.KeyQuery{Mode: "insert", Chain: []core.ContextFrame{{Scope: "editor", Facts: map[string]any{"dirty": true}}}, Key: "y"}
	if res := km.Resolve(q); res.Command == nil || res.Command.ID != "file.save" {
		t.Fatal("stale edited keymap")
	}
	if !m.Reset(1) || m.Bindings()[1].Keys[0] != "b" {
		t.Fatal("reset")
	}
	if !m.Undo() || m.Bindings()[1].Keys[0] != "y" || !m.Redo() || m.Bindings()[1].Keys[0] != "b" {
		t.Fatal("history")
	}
	i := m.Add(bind("", "normal", "x", "new"))
	if !m.Reset(i) || len(m.Bindings()) != 2 || !m.Undo() || len(m.Bindings()) != 3 {
		t.Fatal("new binding reset")
	}
	m.Rebind(i, []string{"z"})
	if m.Redo() || m.Rebind(99, []string{"q"}) {
		t.Fatal("history branch")
	}
	state := m.Bindings()
	state[0].Keys[0] = "bad"
	a.Keys[0] = "bad"
	if m.Bindings()[0].Keys[0] != "a" {
		t.Fatal("model shares keys")
	}
}

func TestRenderedEditorAndContextualHelp(t *testing.T) {
	bs := []core.Binding{
		bind("", "normal", "x", "open"), bind("app", "normal", "x", "masked"),
		bind("app", "normal", "y", "guarded"), bind("app", "normal", "g g", "jump"),
		bind("app", "normal", "h", "hidden"), {Mode: "normal", Class: core.ClassDigit, Command: "count"},
	}
	bs[0].Phase = core.PhaseCapture
	bs[2].When = "facts.allow"
	bs[3].Hint = "top"
	bs[4].Hidden = true
	m := New([]core.Mode{{Name: "normal"}}, bs)
	m.SetQuery(`key:"g g" verb:jump`)
	root := m.View("keys")
	f, _ := ui.Render(root, ui.DefaultDarkTheme(), "keys.filter", 160, 10)
	for _, s := range []string{"verb:jump", "g g", "jump", "top", "unknown-command"} {
		if !strings.Contains(f.ANSI, s) {
			t.Errorf("editor missing %q\n%s", s, f.ANSI)
		}
	}
	q := core.KeyQuery{Mode: "normal", Chain: []core.ContextFrame{{Scope: "app"}}}
	render := func() string {
		v := m.HelpView("help", q)
		f, _ := ui.Render(v, ui.DefaultDarkTheme(), "help", 160, 12)
		return f.ANSI
	}
	v := render()
	for _, s := range []string{"open", "jump", "count"} {
		if !strings.Contains(v, s) {
			t.Errorf("help missing %s", s)
		}
	}
	for _, s := range []string{"masked", "guarded", "hidden"} {
		if strings.Contains(v, s) {
			t.Errorf("help includes %s", s)
		}
	}
	q.Chain[0].Facts = map[string]any{"allow": true}
	m.Rebind(2, []string{"v", "v"})
	q.Input.Prefix = []string{"v"}
	v = render()
	if !strings.Contains(v, "guarded") || strings.Contains(v, "jump") {
		t.Errorf("pending help\n%s", v)
	}
}
