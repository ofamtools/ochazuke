package bindings

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

func keyboardBindings() []core.Binding {
	entries := []core.Binding{
		bind("app", "normal", "g p", "page.prev"),
		bind("app", "normal", "ctrl+a g p", "pane.prev"),
		bind("app", "normal", "ctrl+a x y z", "pane.zoom"),
		bind("app", "normal", "g", "page.go"),
		bind("app", "normal", "G", "page.bottom"),
		bind("app", "normal", "K", "page.up"),
		bind("app", "normal", "?", "help.open"),
		bind("app", "normal", "{", "group.prev"),
		bind("app", "normal", "}", "group.next"),
		bind("app", "normal", "g P", "page.last"),
		bind("app", "normal", "left", "focus.left"),
		bind("page", "normal", "g p", "item.prev"),
	}
	entries[0].Hint = "Previous page"
	return entries
}

func TestExtractPrefixes(t *testing.T) {
	want := []PrefixLayer{
		{Label: "Base", SubPrefixCount: 2},
		{Prefix: "ctrl+a", Label: "ctrl+a", LastKey: "ctrl+a", Depth: 1, SubPrefixCount: 2},
		{Prefix: "g", Label: "g", LastKey: "g", Depth: 1, DirectCount: 1},
		{Prefix: "ctrl+a g", Label: "ctrl+a g", LastKey: "g", Depth: 2},
		{Prefix: "ctrl+a x", Label: "ctrl+a x", LastKey: "x", Depth: 2, SubPrefixCount: 1},
		{Prefix: "ctrl+a x y", Label: "ctrl+a x y", LastKey: "y", Depth: 3},
	}
	entries := keyboardBindings()
	if got := ExtractPrefixes(entries); !slices.Equal(got, want) {
		t.Fatalf("prefixes = %+v, want %+v", got, want)
	}

	entries = append(entries, bind("", "", "G a", "one"), bind("", "", "alt+a g n", "two"))
	var got []string
	for _, layer := range ExtractPrefixes(entries) {
		if strings.EqualFold(layer.LastKey, "g") {
			got = append(got, layer.Prefix)
		}
	}
	if !slices.Equal(got, []string{"G", "g", "alt+a g", "ctrl+a g"}) {
		t.Fatalf("g group = %q", got)
	}
}

func TestProjectKeyboard(t *testing.T) {
	entries := keyboardBindings()
	cases := []struct {
		prefix, physical string
		shifted          bool
		key, next, tone  string
		childCount       int
		shiftHint        bool
		commands         []string
	}{
		{"", "g", false, "g", "g", "warning", 2, true, []string{"page.go"}},
		{"", "g", true, "G", "", "accent", 0, false, []string{"page.bottom"}},
		{"", "k", false, "k", "", "muted", 0, true, nil},
		{"", "k", true, "K", "", "accent", 0, false, []string{"page.up"}},
		{"", "/", true, "?", "", "accent", 0, false, []string{"help.open"}},
		{"", "[", true, "{", "", "accent", 0, false, []string{"group.prev"}},
		{"", "]", true, "}", "", "accent", 0, false, []string{"group.next"}},
		{"", "left", true, "left", "", "accent", 0, false, []string{"focus.left"}},
		{"", "ctrl+a", false, "ctrl+a", "ctrl+a", "warning", 2, false, nil},
		{"g", "p", false, "p", "", "accent", 0, true, []string{"page.prev", "item.prev"}},
		{"g", "p", true, "P", "", "accent", 0, false, []string{"page.last"}},
		{"ctrl+a", "g", false, "g", "ctrl+a g", "warning", 1, false, nil},
		{"ctrl+a", "x", false, "x", "ctrl+a x", "warning", 1, false, nil},
		{"ctrl+a g", "p", false, "p", "", "accent", 0, false, []string{"pane.prev"}},
		{"ctrl+a x y", "z", false, "z", "", "accent", 0, false, []string{"pane.zoom"}},
	}
	for _, tc := range cases {
		spec := ProjectKeyboard(entries, tc.prefix, tc.shifted)
		cap, ok := spec.keycap(tc.physical)
		var commands []string
		for _, action := range cap.Actions {
			commands = append(commands, action.Command)
		}
		if !ok || cap.Key != tc.key || cap.Tone != tc.tone || cap.NextPrefix != tc.next ||
			cap.ChildCount != tc.childCount || cap.HasShift != tc.shiftHint || !slices.Equal(commands, tc.commands) {
			t.Errorf("prefix %q key %q (Shift=%t): %+v", tc.prefix, tc.physical, tc.shifted, cap)
		}
		if spec.Empty || spec.ActivePrefix.Prefix != tc.prefix || spec.Shifted != tc.shifted {
			t.Errorf("layer summary: %+v", spec.ActivePrefix)
		}
	}
	if spec := ProjectKeyboard(entries, "ctrl+a g", true); !spec.Empty {
		t.Fatal("an unshifted letter appeared in the shifted projection")
	}
	if spec := ProjectKeyboard(nil, "unknown", false); !spec.Empty || spec.ActivePrefix.Label != "Base" || len(spec.Prefixes) != 1 {
		t.Fatalf("empty projection: %+v", spec.ActivePrefix)
	}
}

func TestKeyboardCommandsAndViewNode(t *testing.T) {
	m := New(nil, keyboardBindings())
	command := func(id string, args map[string]any) bool {
		return m.HandleCommand(core.Command{ID: id, Args: args})
	}
	assertLayer := func(prefix string, shifted, empty bool) ui.ViewNode {
		t.Helper()
		node := m.ViewNode("keys")
		want := map[string]any{"viewMode": ViewKeyboard, "activePrefix": prefix, "prefixCount": 6, "shifted": shifted, "empty": empty}
		if !maps.Equal(node.Facts, want) || node.ByID("keys.table") != nil {
			t.Fatalf("view facts = %+v, want %+v", node.Facts, want)
		}
		return node
	}
	assertMove := func(id, prefix string) {
		t.Helper()
		if !command(id, nil) {
			t.Fatalf("%s was not handled", id)
		}
		assertLayer(prefix, false, false)
	}
	if got := m.View("keys"); got.Facts["viewMode"] != ViewTable || got.ByID("keys.table") == nil {
		t.Fatal("expected table by default")
	}
	assertMove(CmdViewToggle, "")
	if command(CmdPrefixPrev, nil) || command(CmdPrefixSelect, nil) || command(CmdPrefixSelect, map[string]any{"prefix": "g p"}) {
		t.Fatal("moved above Base or selected an invalid prefix")
	}
	assertMove(CmdPrefixNext, "ctrl+a")
	assertMove(CmdPrefixNext, "g")
	assertMove(CmdPrefixNext, "ctrl+a g")
	assertMove(CmdPrefixPrev, "g")
	assertMove(CmdPrefixClear, "")

	root := assertLayer("", false, false)
	cap := root.ByID("keys.key.g")
	if cap == nil || !cap.Focusable || cap.Tone != "warning" || len(cap.Actions) != 1 {
		t.Fatalf("g should be a selectable warning keycap: %+v", cap)
	}
	if a := cap.Actions[0]; !command(a.Command, a.Args) {
		t.Fatal("keycap action did not select its prefix")
	}
	root = assertLayer("g", false, false)
	if layer := root.ByID("keys.prefix.g"); layer == nil || layer.Facts["active"] != true {
		t.Fatalf("missing active prefix row: %+v", layer)
	}
	if a := root.ByID("keys.key.p").Actions[0]; !command(a.Command, a.Args) {
		t.Fatal("could not inspect p")
	}
	frame, _ := ui.Render(m.View("keys"), ui.DefaultDarkTheme(), "keys.key.p", 100, 25)
	for _, s := range []string{"Layer · g", "Prefixes", "Shift off", "Previous page"} {
		if !strings.Contains(frame.ANSI, s) {
			t.Errorf("keyboard render missing %q\n%s", s, frame.ANSI)
		}
	}
	if a := root.ByID("keys.prefix.ctrl+a g").Actions[0]; !command(a.Command, a.Args) {
		t.Fatal("prefix row action did not select its prefix")
	}
	assertLayer("ctrl+a g", false, false)
	if !command(CmdShiftToggle, nil) {
		t.Fatal("shift command not handled")
	}
	assertLayer("ctrl+a g", true, true)
	if !command(CmdViewToggle, nil) {
		t.Fatal("could not toggle away from keyboard")
	}
	if table := m.View("keys"); table.ByID("keys.table") == nil {
		t.Fatal("could not return to table")
	}
	if !command(CmdViewToggle, nil) {
		t.Fatal("could not return to keyboard")
	}
	assertLayer("ctrl+a g", true, true)

	m.SetQuery("verb:page.prev")
	if facts := m.View("keys").Facts; facts["activePrefix"] != "" || facts["prefixCount"] != 2 {
		t.Fatalf("stale prefix facts after filtering: %v", facts)
	}
}

func TestKeyboardProjectionDoesNotMutateInputs(t *testing.T) {
	entries := keyboardBindings()
	before := copyBindings(entries)
	spec := ProjectKeyboard(entries, "", false)
	first := spec.ViewNode("keys", "g")
	if _, err := json.Marshal(first); err != nil {
		t.Fatal(err)
	}
	second := spec.ViewNode("keys", "g")
	if !reflect.DeepEqual(first, second) || !reflect.DeepEqual(entries, before) {
		t.Fatal("projection changed inputs or depended on prior rendering")
	}
	second.Facts["viewMode"] = "changed"
	second.ByID("keys.key.g").Actions[0].Args["prefix"] = "changed"
	if !reflect.DeepEqual(first, spec.ViewNode("keys", "g")) {
		t.Fatal("a caller could mutate subsequent projections")
	}
}
