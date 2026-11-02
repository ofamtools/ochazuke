package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

func compactFixture(t *testing.T) (*Runtime, *Model, *List) {
	t.Helper()
	r, m, l := filterFormFixture(t)
	if err := r.Settings().Set(ListFilterLayoutSetting, "compact"); err != nil {
		t.Fatal(err)
	}
	_ = r.Settings().Set(ListFilterSortSetting, true)
	return r, m, l
}
func compactKeys(m *Model, keys ...string) {
	for _, key := range keys {
		keytest.Press(m, key)
		m.View()
	}
}

func compactField(t *testing.T, m *Model, name string) {
	t.Helper()
	compactKeys(m, "ctrl+alt+f")
	for _, c := range name {
		compactKeys(m, string(c))
	}
	compactKeys(m, "enter")
}
func TestCompactFilterValuesRoundTripAndClear(t *testing.T) {
	r, m, l := compactFixture(t)
	compactKeys(m, "z", "/")
	if r.Focus != listFilterFormID+"/field/query" {
		t.Fatal(r.Focus)
	}
	compactField(t, m, "status")
	compactKeys(m, "space", "ctrl+n", "ctrl+n", "space", "enter")
	if !slices.Equal(shownIDs(l), []string{"t3", "t5", "t6"}) || l.PropFilters()[0].Op != "in" {
		t.Fatal(l.PropFilters(), shownIDs(l))
	}
	if r.Focus != listFilterFormID+"/field/query" {
		t.Fatal("Enter did not return to fields", r.Focus)
	}
	compactKeys(m, "esc", "z", "/")
	if !strings.Contains(ansi.Strip(m.View().Content), "status in") {
		t.Fatal(m.View().Content)
	}
	compactKeys(m, "C") // An input keeps capital C, without clearing conditions.
	if len(l.PropFilters()) != 1 || r.Input(r.Focus).Text[0] != 'C' {
		t.Fatal("C cleared from input")
	}
	compactKeys(m, "ctrl+alt+c")
	if len(l.PropFilters()) != 0 || len(l.Sort()) != 0 {
		t.Fatal("clear did not empty all view state")
	}
}
func TestCompactFilterSortAndKeptConditions(t *testing.T) {
	r, m, l := compactFixture(t)
	if err := l.SetPropFilters([]PropFilter{{Prop: "title", Op: "in", Value: []any{"Buy milk"}}}); err != nil {
		t.Fatal(err)
	}
	compactKeys(m, "z", "/", "ctrl+alt+s", "s", "c", "o", "r", "e", "enter", "ctrl+alt+d")
	if len(l.Sort()) != 1 || !l.Sort()[0].Desc || len(l.PropFilters()) != 1 {
		t.Fatal(l.Snapshot())
	}
	compactField(t, m, "status")
	compactKeys(m, "d", "o", "i", "n", "g", "enter")
	if len(l.PropFilters()) != 2 || l.PropFilters()[0].Prop != "title" {
		t.Fatal("unrepresented condition lost", l.PropFilters())
	}
	compactKeys(m, "ctrl+alt+c")
	if len(l.PropFilters()) != 0 || len(l.Sort()) != 0 {
		t.Fatal("clear retained unsupported conditions")
	}
	if r.TopModal() != listFilterFormID {
		t.Fatal("clear closed modal")
	}
}
func TestCompactFilterFreeTextWritesAndUndo(t *testing.T) {
	r, m, l := compactFixture(t)
	compactKeys(m, "z", "/")
	compactField(t, m, "title")
	compactKeys(m, "B", "u", "y")
	if !slices.Equal(shownIDs(l), []string{"t2"}) {
		t.Fatal("text did not apply live", shownIDs(l))
	}
	compactKeys(m, "enter", "ctrl+alt+a", "!")
	if l.PropFilters()[0].Op != "not-contains" {
		t.Fatal(l.PropFilters())
	}
	compactKeys(m, "d", "d")
	if len(l.PropFilters()) != 0 {
		t.Fatal("remove failed")
	}
	if _, err := r.Execute(Request{Noun: "form", Verb: "undo"}); err != nil {
		t.Fatal(err)
	}
	if len(l.PropFilters()) != 1 {
		t.Fatal("undo did not restore condition")
	}
}
func TestCompactFilterTreeAndChooser(t *testing.T) {
	r, tree := propTreeFixture(t)
	m := NewModel(r)
	m.Width, m.Height = 100, 28
	_ = tree.List().Focus()
	compactKeys(m, "z", "/")
	compactField(t, m, "by")
	compactKeys(m, "o", "w", "n", "e", "r", "enter")
	if !tree.List().shows("deep") || tree.List().shows("other") {
		t.Fatal(tree.List().Snapshot())
	}
	r, m, c := chooserPropFixture(t)
	compactKeys(m, "ctrl+f")
	compactField(t, m, "by")
	compactKeys(m, "a", "g", "e", "n", "t", "enter", "esc")
	if !slices.Equal(chooserIDs(c), []string{"b"}) {
		t.Fatal(c.state())
	}
}
func TestCompactFilterSettingsAndBindings(t *testing.T) {
	r, m, l := compactFixture(t)
	if r.Settings().String(ListFilterLayoutSetting) != "compact" || !r.Settings().Bool(ListFilterSortSetting) {
		t.Fatal("bad defaults")
	}
	_ = r.Settings().Set(ListFilterSortSetting, false)
	compactKeys(m, "z", "/")
	if strings.Contains(ansi.Strip(m.View().Content), "Sort") {
		t.Fatal("sort row opt-out ignored")
	}
	data, _ := frameworkBindings.ReadFile("bindings/filter-form.json")
	if problems := r.CheckDefaultBindings("bindings/filter-form.json", data); len(problems) > 0 {
		t.Fatal(problems)
	}
	if _, err := r.Execute(Request{Noun: "bindings", Verb: "check"}); err != nil {
		t.Fatal(err)
	}
	_ = l
}

func TestCompactFilterClearResetsPickerQueries(t *testing.T) {
	r, m, l := compactFixture(t)
	compactKeys(m, "z", "/", "ctrl+alt+s", "s", "c", "o", "r", "e", "enter")
	compactField(t, m, "status")
	compactKeys(m, "d", "o", "n", "e", "enter", "ctrl+alt+c")
	v := filterViewAt(r, r.Focus)
	if v.current != nil || v.sortPicker.Query() != "" || v.fieldPicker.Query() != "" || len(l.PropFilters()) > 0 {
		t.Fatal("clear retained control state")
	}
}

func TestEmbeddedChooserStaysAndUsesDeclaredRowView(t *testing.T) {
	r, _, l := compactFixture(t)
	e, err := r.OpenRecordEditor("composed", RecordSpec{Fields: []RecordField{{Name: "backing", Hidden: true}, {Name: "shown"}}, RowView: l})
	if err != nil {
		t.Fatal(err)
	}
	picked := ""
	c, err := r.AddChooser(ChooserSpec{Kind: "embedded", Stay: true, Rows: []ChooserRow{{ID: "one", Label: "one"}}, Choose: func(row ChooserRow) Result { picked = row.ID; return Done(nil) }}, Parent(e.id))
	if err != nil {
		t.Fatal(err)
	}
	_ = r.SetFocus(c.input.ID)
	if r.rowFilterList(r.Focus) != l {
		t.Fatal("composed row context lost")
	}
	c.pick()
	if picked != "one" || r.Chooser(c.Kind()) != c || r.TopModal() != e.id {
		t.Fatal("placed pick closed editor")
	}
	if err := e.Leave(); err != nil {
		t.Fatal(err)
	}
	if r.Chooser(c.Kind()) != nil {
		t.Fatal("placed chooser leaked")
	}
}
