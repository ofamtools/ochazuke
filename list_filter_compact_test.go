package ochazuke

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

func compactFixture(t *testing.T) (*Runtime, *Model, *Collection) {
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

func compactPart(t *testing.T, m *Model, part string) {
	t.Helper()
	r := m.Runtime
	v := filterViewAt(r, r.Focus)
	if v == nil {
		t.Fatal("no compact form")
	}
	target := ""
	switch part {
	case "field":
		target = v.fieldPicker.input.ID
	case "sort":
		target = v.sortPicker.input.ID
	case "direction":
		target = v.e.field(filterDirectionField).id
	case "conditions":
		target = v.conditions.id
	case "save":
		target = v.saveInput.ID
	}
	for i := 0; i < 16; i++ {
		if r.Focus == target || part == "conditions" && v.conditions.Focused() {
			if r.Input(r.Focus) != nil && r.Mode.Name != "insert" {
				compactKeys(m, "i")
			}
			return
		}
		compactKeys(m, "tab")
	}
	t.Fatalf("Tab could not reach %s from %s", part, r.Focus)
}

func compactField(t *testing.T, m *Model, name string) {
	t.Helper()
	compactPart(t, m, "field")
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
	compactKeys(m, "esc", "space", "ctrl+n", "ctrl+n", "space", "enter")
	if !slices.Equal(shownIDs(l), []string{"t3", "t5", "t6"}) || l.PropFilters()[0].Op != "in" {
		t.Fatal(l.PropFilters(), shownIDs(l))
	}
	if r.Focus != listFilterFormID+"/field/query" {
		t.Fatal("Enter did not return to fields", r.Focus)
	}
	compactKeys(m, "esc", "esc", "z", "/")
	if !strings.Contains(ansi.Strip(m.View().Content), "status in") {
		t.Fatal(m.View().Content)
	}
	compactKeys(m, "C") // An input keeps capital C, without clearing conditions.
	if len(l.PropFilters()) != 1 || r.Input(r.Focus).Text[0] != 'C' {
		t.Fatal("C cleared from input")
	}
	compactKeys(m, "esc")
	compactPart(t, m, "conditions")
	compactKeys(m, "C")
	if len(l.PropFilters()) != 0 || len(l.Sort()) != 0 {
		t.Fatal("clear did not empty all view state")
	}
}
func TestCompactFilterSortAndKeptConditions(t *testing.T) {
	r, m, l := compactFixture(t)
	if err := l.SetPropFilters([]PropFilter{{Prop: "title", Op: "in", Value: []any{"Buy milk"}}}); err != nil {
		t.Fatal(err)
	}
	compactKeys(m, "z", "/")
	compactPart(t, m, "sort")
	compactKeys(m, "s", "c", "o", "r", "e", "enter")
	compactPart(t, m, "direction")
	compactKeys(m, "enter")
	if len(l.Sort()) != 1 || !l.Sort()[0].Desc || len(l.PropFilters()) != 1 {
		t.Fatal(l.Snapshot())
	}
	compactField(t, m, "status")
	compactKeys(m, "d", "o", "i", "n", "g", "enter")
	if len(l.PropFilters()) != 2 || l.PropFilters()[0].Prop != "title" {
		t.Fatal("unrepresented condition lost", l.PropFilters())
	}
	compactKeys(m, "esc")
	compactPart(t, m, "conditions")
	compactKeys(m, "C")
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
	compactKeys(m, "enter")
	compactPart(t, m, "conditions")
	compactKeys(m, "!")
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
	compactKeys(m, "a", "g", "e", "n", "t", "enter", "esc", "esc")
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
	if _, err := r.Execute(Request{Noun: "binding", Verb: "check"}); err != nil {
		t.Fatal(err)
	}
	_ = l
}

func TestCompactFilterClearResetsPickerQueries(t *testing.T) {
	r, m, l := compactFixture(t)
	compactKeys(m, "z", "/")
	compactPart(t, m, "sort")
	compactKeys(m, "s", "c", "o", "r", "e", "enter")
	compactField(t, m, "status")
	compactKeys(m, "d", "o", "n", "e", "enter")
	compactPart(t, m, "conditions")
	compactKeys(m, "C")
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

func TestCompactFilterLegacyPropChooserReachesSort(t *testing.T) {
	r, m, l := compactFixture(t)
	_ = r.Settings().Set(ListFilterFormSetting, false)
	_ = r.Settings().Set(ListFilterPickerSortSetting, true)
	compactKeys(m, "z", "/", "S", "o", "r", "t", "enter")
	if r.Chooser("list-sort-field") == nil {
		t.Fatal("Sort entry did not use shared sort picker", r.TopModal())
	}
	compactKeys(m, "s", "c", "o", "r", "e", "enter")
	if len(l.Sort()) != 1 || l.Sort()[0].Field != "score" {
		t.Fatal(l.Sort())
	}
}

func TestCompactFilterTreeSortChangesSiblingsAndClear(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	r.Define("node")
	tree, err := r.AddTree("tree", []TreeNode{{ID: "z", Noun: "node", Props: Thing{"rank": 2}}, {ID: "zb", Parent: "z", Noun: "node", Props: Thing{"rank": 2}}, {ID: "za", Parent: "z", Noun: "node", Props: Thing{"rank": 1}}, {ID: "a", Noun: "node", Props: Thing{"rank": 1}}})
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 100, 28
	_ = tree.List().Focus()
	compactKeys(m, "z", "/")
	compactPart(t, m, "sort")
	compactKeys(m, "r", "a", "n", "k", "enter")
	if !slices.Equal(shownIDs(tree.List()), []string{"a", "z", "za", "zb"}) {
		t.Fatal(shownIDs(tree.List()))
	}
	compactPart(t, m, "conditions")
	compactKeys(m, "C")
	if !slices.Equal(shownIDs(tree.List()), []string{"z", "zb", "za", "a"}) {
		t.Fatal("clear lost tree app order")
	}
}

func TestFilterFormClearTerminalEncoding(t *testing.T) {
	for _, layout := range []string{"compact", "fields"} {
		t.Run(layout, func(t *testing.T) {
			r, m, l := compactFixture(t)
			_ = r.Settings().Set(ListFilterLayoutSetting, layout)
			_ = r.Settings().Set(ListFilterSaveSetting, false)
			_ = l.SetPropFilters([]PropFilter{{Prop: "status", Value: "todo"}})
			_ = l.SetSort([]SortKey{{Field: "score", Desc: true}})
			compactKeys(m, "z", "/")
			compactPart(t, m, "direction")
			for _, raw := range []string{"C"} {
				var decoder uv.EventDecoder
				n, event := decoder.Decode([]byte(raw))
				key, ok := event.(uv.KeyPressEvent)
				if !ok || n != len(raw) {
					t.Fatalf("decode %x: %#v", raw, event)
				}
				m.Update(tea.KeyPressMsg(key))
				m.View()
			}
			if len(l.PropFilters()) != 0 || len(l.Sort()) != 0 || l.Shown() != 6 || r.TopModal() != listFilterFormID {
				t.Fatal(l.Snapshot(), r.TopModal())
			}
		})
	}
}

// terminalKeys sends legacy bytes through the same decoder Bubble Tea uses.
func terminalKeys(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	codes := map[string]string{"tab": "\t", "shift+tab": "\x1b[Z", "esc": "\x1b", "enter": "\r", "space": " ", "ctrl+r": "\x12", "up": "\x1b[A", "down": "\x1b[B"}
	for _, key := range keys {
		raw := key
		if code, ok := codes[key]; ok {
			raw = code
		}
		var d uv.EventDecoder
		n, event := d.Decode([]byte(raw))
		press, ok := event.(uv.KeyPressEvent)
		if !ok || n != len(raw) {
			t.Fatalf("decode %q: %d %#v", key, n, event)
		}
		t.Logf("%q (%x) -> %s", key, raw, press.String())
		m.Update(tea.KeyPressMsg(press))
		m.View()
	}
}

func TestCompactFilterPlainTabCycle(t *testing.T) {
	r, m, l := compactFixture(t)
	terminalKeys(t, m, "z", "/", "shift+tab", "shift+tab", "s", "c", "o", "r", "e", "enter", "shift+tab", "enter")
	if len(l.Sort()) != 1 || !l.Sort()[0].Desc {
		t.Fatal(l.Sort())
	}
	terminalKeys(t, m, "tab", "s", "t", "a", "t", "u", "s", "enter", "esc", "space", "enter", "tab")
	terminalKeys(t, m, "u")
	if len(l.PropFilters()) != 0 {
		t.Fatal("u on conditions did not undo", l.PropFilters())
	}
	terminalKeys(t, m, "ctrl+r")
	if len(l.PropFilters()) != 1 || l.PropFilters()[0].Value != "cancelled" {
		t.Fatal(l.PropFilters())
	}
	terminalKeys(t, m, "tab", "k", "e", "p", "t", "enter", "esc", "d", "d")
	saved, _ := l.SavedFilters()
	if !slices.ContainsFunc(saved, func(f SavedFilter) bool { return f.Name == "kept" }) {
		t.Fatal("text dd deleted a saved filter")
	}
	if got := string(r.Input(r.Focus).Text); got != "" {
		t.Fatal("dd did not edit the name", got)
	}
	terminalKeys(t, m, "u")
	if got := string(r.Input(r.Focus).Text); got != "kept" {
		t.Fatal("u did not undo text", got)
	}
	terminalKeys(t, m, "tab", "tab", "C")
	if len(l.PropFilters()) != 0 || len(l.Sort()) != 0 {
		t.Fatal(l.Snapshot())
	}
}

func TestFilterFormNoRedundantChords(t *testing.T) {
	r := frameworkRuntime(t)
	data, _ := frameworkBindings.ReadFile("bindings/filter-form.json")
	file, _, err := ParseBindings(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range file.Groups {
		for _, b := range g.Rows {
			if strings.Contains(b.Keys, "alt+") || b.Keys == "ctrl+g" {
				t.Fatalf("redundant key %s", b.Keys)
			}
		}
	}
	for _, verb := range []string{"focus", "direction", "write-saved", "delete-saved", "next", "toggle"} {
		if _, ok := r.Operation(filterFormNoun + "." + verb); !ok {
			t.Fatal("missing verb", verb)
		}
	}
}

func TestChooserNormalModeOptIn(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	m := NewModel(r)
	m.Width, m.Height = 90, 25
	c, err := r.OpenChooser(ChooserSpec{Kind: "normal-capable", NormalMode: true, Rows: []ChooserRow{{ID: "one", Label: "one"}}})
	if err != nil {
		t.Fatal(err)
	}
	compactKeys(m, "o", "esc")
	if r.Mode.Name != "normal" || c.Query() != "o" || r.TopModal() != c.Kind() {
		t.Fatal(r.Mode, c.Query(), r.TopModal())
	}
	compactKeys(m, "i", "n", "e")
	if c.Query() != "neo" { // Escape moves the cursor back; i resumes there, as in other inputs.
		t.Fatalf("normal input editing did not resume: %q", c.Query())
	}
	compactKeys(m, "esc", "esc")
	if r.TopModal() != "" {
		t.Fatal("normal Escape did not close")
	}
}

// Search keeps spaces in insert; selecting several uses the same plain key
// after Escape that works in terminals without an extended keyboard protocol.
func TestCompactFilterValueSpaceIsTextUntilNormal(t *testing.T) {
	r, m, l := compactFixture(t)
	compactKeys(m, "z", "/")
	compactField(t, m, "status")
	compactKeys(m, "space")
	if got := string(r.Input(r.Focus).Text); got != " " || len(l.PropFilters()) != 0 {
		t.Fatalf("insert Space: query %q, conditions %v", got, l.PropFilters())
	}
	if shadows := r.TypingShadows(); len(shadows) != 0 {
		t.Fatal(shadows)
	}
	compactKeys(m, "ctrl+u", "esc", "space")
	if len(l.PropFilters()) != 1 {
		t.Fatal("normal Space did not toggle", l.PropFilters())
	}
}
