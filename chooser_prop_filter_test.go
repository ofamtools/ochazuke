package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

func chooserPropFixture(t *testing.T) (*Runtime, *Model, *Chooser) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	m := NewModel(r)
	m.Width, m.Height = 100, 24
	r.Define("entry").Props(PropSpec{Name: "by"}, PropSpec{Name: "score", Parameter: Parameter{Type: "integer"}}).
		Filters(SavedFilter{Name: "mine", Where: map[string]any{"by": "owner"}}, SavedFilter{Name: "agents", Where: map[string]any{"by": "agent"}}, SavedFilter{Name: "all"})
	c, err := r.OpenChooser(ChooserSpec{Kind: "entries", Noun: "entry", Rows: []ChooserRow{
		{ID: "a", Label: "alpha", Props: Thing{"by": "owner", "score": 1}},
		{ID: "b", Label: "beta", Props: Thing{"by": "agent", "score": 2}},
		{ID: "c", Label: "gamma", Props: Thing{"by": "owner", "score": 3}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return r, m, c
}

func TestChooserPropFiltersSharedVerbsRefreshAndHighlight(t *testing.T) {
	r, m, c := chooserPropFixture(t)
	keytest.Press(m, "ctrl+n") // moved highlight b
	if err := c.SetPropFilters([]PropFilter{{Prop: "score", Op: "in", Value: []any{"2", "3"}}}); err != nil {
		t.Fatal(err)
	}
	if got := chooserIDs(c); !slices.Equal(got, []string{"b", "c"}) || highlighted(c) != "b" || !c.Moved() {
		t.Fatalf("rows %v highlight %s moved %v", got, highlighted(c), c.Moved())
	}
	c.SetQuery("beta")
	if err := c.SetPropFilters([]PropFilter{{Prop: "bad", Value: "x"}}); err == nil {
		t.Fatal("accepted unknown prop")
	}
	if len(c.PropFilters()) != 1 {
		t.Fatal("invalid update changed conditions")
	}
	run := func(verb string, args Arguments) {
		t.Helper()
		if _, err := r.Execute(Request{Noun: "list", Verb: verb, Args: args}); err != nil {
			t.Fatal(verb, err)
		}
	}
	run("filter-save", Arguments{"name": "scores"})
	filters, err := c.SavedFilters()
	if err != nil || len(filters) != 4 || filters[3].List != c.Kind() {
		t.Fatalf("filters %+v err %v", filters, err)
	}
	run("filter-props-clear", nil)
	run("filter-saved", Arguments{"name": "scores"})
	if c.Query() != "beta" || !slices.Equal(chooserIDs(c), []string{"b"}) {
		t.Fatal(c.state())
	}
	run("filter-saved-delete", Arguments{"name": "scores"})
	// SetRows invalidates decoded values and keeps conditions and text.
	c.SetRows([]ChooserRow{{ID: "b", Label: "beta", Props: Thing{"by": "agent", "score": 9}}})
	if len(c.Matches()) != 0 {
		t.Fatal("stale row props")
	}
	run("filter-props-clear", nil)
	if len(c.Matches()) != 1 || c.Query() != "beta" {
		t.Fatal(c.state())
	}
}

func TestChooserPropFilterTypingAndOptOut(t *testing.T) {
	r, m, c := chooserPropFixture(t)
	keytest.Type(m, "zebra")
	if c.Query() != "zebra" {
		t.Fatalf("fallback dropped z: %q", c.Query())
	}
	c.SetQuery("")
	keytest.Type(m, "z]z[z/zC")
	if c.Query() != "z]z[z/zC" {
		t.Fatalf("letters were commands: %q", c.Query())
	}
	c.SetQuery("")
	if err := c.ApplyFilter("mine"); err != nil {
		t.Fatal(err)
	}
	if err := r.Settings().Set(ChooserPropFiltersSetting, "false"); err != nil {
		t.Fatal(err)
	}
	if len(c.Matches()) != 3 || strings.Contains(c.View(80, 10).Text, "[mine]") {
		t.Fatal("runtime opt-out kept filtering")
	}
	keytest.Type(m, "z]")
	if c.Query() != "z]" {
		t.Fatalf("opt-out query %q", c.Query())
	}
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-saved", Args: Arguments{"name": "mine"}}); err == nil {
		t.Fatal("disabled chooser resolved")
	}
	c.spec.NoPropFilters = true
	if err := r.Settings().Set(ChooserPropFiltersSetting, "true"); err != nil {
		t.Fatal(err)
	}
	c.SetQuery("")
	if len(c.Matches()) != 3 {
		t.Fatal("per-chooser opt-out ignored")
	}
}

func TestChooserPropFilterNestedPickerAndSavedPicker(t *testing.T) {
	r, m, c := chooserPropFixture(t)
	chooserChain(t, r) // the prop chooser then the value chooser (list.filter-form off)
	keytest.Press(m, "ctrl+f")
	keytest.Press(m, "enter")
	if r.TopModal() != "list-filter-value" {
		t.Fatalf("second stage %s", r.TopModal())
	}
	keytest.Type(m, "owner")
	keytest.Press(m, "enter")
	if r.TopModal() != c.Kind() || !slices.Equal(chooserIDs(c), []string{"a", "c"}) {
		t.Fatal(c.state())
	}
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-saved-pick"}); err != nil {
		t.Fatal(err)
	}
	keytest.Type(m, "agents")
	keytest.Press(m, "enter")
	if r.TopModal() != c.Kind() || !slices.Equal(chooserIDs(c), []string{"b"}) {
		t.Fatal(c.state())
	}
}

func TestChooserPropFilterRepeatCtrlFAndClearFallback(t *testing.T) {
	r, m, c := chooserPropFixture(t)
	chooserChain(t, r) // the prop chooser then the value chooser (list.filter-form off)
	keytest.Press(m, "ctrl+f")
	if r.TopModal() != "list-filter-prop" {
		t.Fatalf("ctrl+f: %s", r.TopModal())
	}
	keytest.Press(m, "esc")
	keytest.Press(m, "alt+]")
	keytest.Press(m, "ctrl+f")
	if c.filters.savedFilter != "agents" || r.TopModal() != c.Kind() {
		t.Fatal(c.state())
	}
	if err := r.Settings().Set("list.filter-cycle", "false"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "ctrl+f")
	if r.TopModal() != "list-filter-prop" {
		t.Fatal("no clear picker")
	}
	keytest.Type(m, "clear")
	keytest.Press(m, "enter")
	if len(c.PropFilters()) != 0 || len(c.Matches()) != 3 {
		t.Fatal(c.state())
	}
	// The chooser rows deliberately override the editor's ctrl+f cursor move
	// and alt+backspace word erase only while the relevant guards hold.
	data, err := frameworkBindings.ReadFile("bindings/chooser.json")
	if err != nil {
		t.Fatal(err)
	}
	if problems := r.CheckDefaultBindings("bindings/chooser.json", data); len(problems) > 0 {
		t.Fatal(problems)
	}
	if _, err := r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
}

func TestChooserPropFiltersEmptyAndSourceReload(t *testing.T) {
	_, m, c := chooserPropFixture(t)
	c.SetRows(nil)
	keytest.Press(m, "alt+]")
	if c.filters.savedFilter != "mine" || len(c.Matches()) != 0 {
		t.Fatal(c.state())
	}
	c.spec.Sources = []ChooserSource{
		{Name: "first", Load: func(string) Result {
			return Done([]ChooserRow{{ID: "owner", Label: "owner", Data: Thing{"by": "owner", "score": 1}}})
		}},
		{Name: "second", Load: func(string) Result {
			return Done([]ChooserRow{{ID: "agent", Label: "agent", Data: Thing{"by": "agent", "score": 2}}})
		}},
	}
	if err := c.cycle(1); err != nil {
		t.Fatal(err)
	}
	if len(c.Matches()) != 0 {
		t.Fatal("new source bypassed active props")
	}
	keytest.Press(m, "ctrl+f")
	if !slices.Equal(chooserIDs(c), []string{"agent"}) {
		t.Fatal(c.state())
	}
}
