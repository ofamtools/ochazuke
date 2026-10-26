package ochazuke

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// filterFixture is a page with a list of five rows over a second list,
// the focus on the first row, through the Model so keys run as in the TUI.
func filterFixture(t *testing.T) (*Runtime, *Model, *List) {
	t.Helper()
	r := NewRuntime()
	r.Define("a").Verb("touch", func(c Ctx) Result { return Done(c.Instance.ID) })
	r.Instance("p")
	items := []ListItem{
		{ID: "r1", Label: "alpha", Noun: "a"},
		{ID: "r2", Label: "beta", Noun: "a", Cells: []string{"beta", "idle"}},
		{ID: "r3", Label: "gamma", Noun: "a"},
		{ID: "r4", Label: "alphabet", Noun: "a"},
		{ID: "r5", Label: "delta", Noun: "a", Cells: []string{"delta", "IDLE"}},
	}
	list, err := r.AddList("p/l", items, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	below, err := r.AddList("p/b", []ListItem{{ID: "b1", Label: "b1", Noun: "a"}}, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	render := func(r *Runtime, w, h int) Block { return Text(list.View(w, list.Height()) + "\n" + below.View(w, 1)) }
	if _, err := r.AddPage("p", "P", render, Focus("r1")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 80, 20
	m.View()
	return r, m, list
}

func pressKeys(m *Model, keys ...string) {
	for _, key := range keys {
		m.handleKey(MacroKey{Key: key})
		m.View()
	}
}

func shownIDs(l *List) []string {
	var out []string
	for _, item := range l.items {
		out = append(out, item.ID)
	}
	return out
}

func TestListFilterKeys(t *testing.T) {
	r, m, list := filterFixture(t)
	pressKeys(m, "/", "a", "l")
	if r.Mode.Name != ListFilterMode || list.Filter() != "al" || !reflect.DeepEqual(shownIDs(list), []string{"r1", "r4"}) {
		t.Fatalf("typing: mode %s query %q shown %v", r.Mode.Name, list.Filter(), shownIDs(list))
	}
	if list.Len() != 5 || list.Shown() != 2 || len(list.Items()) != 5 || r.Instances["r3"] == nil {
		t.Fatal("the filter must hide rows, not remove them")
	}
	pressKeys(m, "down", "down") // clamps on the last match, never spills into p/b
	if r.Focus != "r4" || r.Mode.Name != ListFilterMode {
		t.Fatalf("down: focus %s mode %s", r.Focus, r.Mode.Name)
	}
	pressKeys(m, "enter", "k")
	if r.Mode.Name != "normal" || r.Focus != "r1" || list.Filter() != "al" {
		t.Fatalf("enter keeps: mode %s focus %s query %q", r.Mode.Name, r.Focus, list.Filter())
	}
	pressKeys(m, "j", "j") // in normal mode the motions spill past the last match
	if r.Focus != "b1" {
		t.Fatalf("j past the last match: focus %s", r.Focus)
	}
	pressKeys(m, "k", "/", "backspace", "backspace", "backspace")
	if r.Mode.Name != "normal" || list.Filter() != "" || list.Shown() != 5 {
		t.Fatalf("backspace on empty leaves: mode %s query %q shown %d", r.Mode.Name, list.Filter(), list.Shown())
	}
	pressKeys(m, "j", "/", "z", "esc") // k landed on r4, the last match
	if r.Mode.Name != "normal" || list.Filter() != "" || r.Focus != "r5" {
		t.Fatalf("esc clears and goes back: mode %s query %q focus %s", r.Mode.Name, list.Filter(), r.Focus)
	}
	// Cells match, ignoring case; every word must match.
	pressKeys(m, "/", "i", "d", "l", "e")
	if !reflect.DeepEqual(shownIDs(list), []string{"r2", "r5"}) {
		t.Fatalf("cells: shown %v", shownIDs(list))
	}
	pressKeys(m, "space", "b")
	if !reflect.DeepEqual(shownIDs(list), []string{"r2"}) {
		t.Fatalf("two words: shown %v", shownIDs(list))
	}
	pressKeys(m, "ctrl+u")
	if list.Filter() != "" || r.Mode.Name != ListFilterMode {
		t.Fatalf("ctrl+u: query %q mode %s", list.Filter(), r.Mode.Name)
	}
}

func TestListFilterSetItemsMarksAndApply(t *testing.T) {
	r, m, list := filterFixture(t)
	for _, id := range []string{"r2", "r4"} {
		if err := list.ToggleMark(id); err != nil {
			t.Fatal(err)
		}
	}
	list.SetFilter("al")
	if sel, _ := list.Selected(); sel.ID != "r1" || r.Focus != "r1" {
		t.Fatalf("selection %s focus %s", sel.ID, r.Focus)
	}
	pressKeys(m, "j")
	// SetItems while filtered: the new rows are filtered, the selection
	// stays on its row, the marks stay.
	items := list.Items()
	items = append(items, ListItem{ID: "r6", Label: "Alps", Noun: "a"})
	if err := list.SetItems(items); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(shownIDs(list), []string{"r1", "r4", "r6"}) || r.Focus != "r4" {
		t.Fatalf("SetItems: shown %v focus %s", shownIDs(list), r.Focus)
	}
	if marked := list.Marked(); len(marked) != 2 {
		t.Fatalf("marks: %v", marked)
	}
	// list.apply fans out to the marked rows on screen only.
	res, err := r.Execute(Request{Noun: "list", Verb: "apply", Target: "p/l", Args: Arguments{"verb": "touch"}})
	if err != nil {
		t.Fatal(err)
	}
	if applied := res.(map[string]any)["applied"]; !reflect.DeepEqual(applied, []string{"r4"}) {
		t.Fatalf("apply: %v", applied)
	}
	// The selected row dropped by SetItems falls to the nearest shown row.
	if err := list.SetItems(slicesWithout(list.Items(), "r4")); err != nil {
		t.Fatal(err)
	}
	if sel, _ := list.Selected(); sel.ID != "r6" || r.Focus != "r6" {
		t.Fatalf("removed selection: %s focus %s", sel.ID, r.Focus)
	}
	// A command landing on a hidden row shows it.
	if err := r.SetFocus("r3"); err != nil {
		t.Fatal(err)
	}
	if list.Filter() != "" || list.Shown() != 5 {
		t.Fatalf("focus on a hidden row: query %q shown %d", list.Filter(), list.Shown())
	}
	if sel, _ := list.Selected(); sel.ID != "r3" {
		t.Fatalf("selected %s", sel.ID)
	}
}

func slicesWithout(items []ListItem, id string) []ListItem {
	var out []ListItem
	for _, item := range items {
		if item.ID != id {
			out = append(out, item)
		}
	}
	return out
}

func TestListFilterVerbAndView(t *testing.T) {
	r, m, list := filterFixture(t)
	res, err := r.Execute(Request{Noun: "list", Verb: "filter", Target: "p/l", Args: Arguments{"query": "ALPHA"}})
	if err != nil {
		t.Fatal(err)
	}
	state := res.(map[string]any)
	if state["filter"] != "ALPHA" || state["total"] != 5 || len(state["items"].([]ListItem)) != 2 {
		t.Fatalf("state: %v", state)
	}
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "/ALPHA") || !strings.Contains(frame, "2 of 5") || strings.Contains(frame, "gamma") {
		t.Fatalf("frame:\n%s", frame)
	}
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter", Target: "p/l", Args: Arguments{"query": ""}}); err != nil {
		t.Fatal(err)
	}
	if list.Shown() != 5 || strings.Contains(ansi.Strip(m.View().Content), "/ALPHA") {
		t.Fatal("an empty query clears the filter")
	}
	// filter-clear is guarded off with nothing to clear, so esc stands
	// aside for the level outside the list.
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-clear", Target: "p/l"}); err == nil {
		t.Fatal("filter-clear with no filter should be refused")
	}
}

func TestListFilterHintsAndHelp(t *testing.T) {
	r, m, _ := filterFixture(t)
	m.Width = 200
	if hints := BindingHints(r, r.Focus); !strings.Contains(hints, "/ filter") || strings.Contains(hints, "list.filter-clear") {
		t.Fatalf("normal hints: %s", hints)
	}
	pressKeys(m, "/")
	hints := BindingHints(r, r.Focus)
	for _, want := range []string{"enter list.filter-keep", "esc list.filter-clear"} {
		if !strings.Contains(hints, want) {
			t.Fatalf("filter hints lack %q: %s", want, hints)
		}
	}
	if strings.Contains(hints, "list.filter-type") || strings.Contains(hints, "list.move") {
		t.Fatalf("typing and motions stay off the footer: %s", hints)
	}
	pressKeys(m, "esc")
	found := false
	for _, row := range BindingHelp(r, r.Focus) {
		found = found || row.Key == "/" && strings.Contains(row.Description, "filter line")
	}
	if !found {
		t.Fatal("? help lists / list.filter-open")
	}
}

// An application's own "/" answers before the framework's, wherever it is
// on the stack: here on the page instance, outside the list.
func TestListFilterAppSlashWins(t *testing.T) {
	r, m, list := filterFixture(t)
	r.Define("p").Verb("search", func(Ctx) Result { return Done("searched") })
	row := Do("p.search")
	row.Instance, row.Mode, row.Keys = "p", "normal", "/"
	if err := r.Bind(row); err != nil {
		t.Fatal(err)
	}
	pressKeys(m, "/")
	if r.Mode.Name != "normal" || list.Filtering() {
		t.Fatalf("the app's / should answer: mode %s", r.Mode.Name)
	}
}
