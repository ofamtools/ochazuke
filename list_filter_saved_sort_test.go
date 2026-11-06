package ochazuke

import (
	"encoding/json"
	"github.com/charmbracelet/x/ansi"
	"slices"
	"strings"
	"testing"
)

func savedSortFixture(t *testing.T) (*Runtime, *Model, *List) {
	r, m, l := compactFixture(t)
	r.Define("task").Filters(SavedFilter{Name: "open", Where: map[string]any{"status": "todo"}}, SavedFilter{Name: "all"})
	return r, m, l
}

func TestSavedFilterSortRoundTripAndLegacy(t *testing.T) {
	r, _, l := savedSortFixture(t)
	l.SetFilter("i")
	if err := l.SetSort([]SortKey{{Field: "score", Desc: true}}); err != nil {
		t.Fatal(err)
	}
	if err := l.SaveFilter("ranked"); err != nil {
		t.Fatal(err)
	}
	saved, err := l.SavedFilters()
	if err != nil {
		t.Fatal(err)
	}
	f := saved[slices.IndexFunc(saved, func(f SavedFilter) bool { return f.Name == "ranked" })]
	if len(f.Sort) != 1 || !f.Sort[0].Desc {
		t.Fatal(f)
	}
	f.Sort[0].Field = "bad"
	if err := l.SetSort(nil); err != nil {
		t.Fatal(err)
	}
	if err := l.ApplyFilter("ranked"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(shownIDs(l), []string{"t6", "t5", "t3", "t2", "t1"}) || l.Filter() != "i" {
		t.Fatal(shownIDs(l), l.Filter())
	}
	if err := l.ApplyFilter("all"); err != nil {
		t.Fatal(err)
	}
	if len(l.Sort()) != 0 || !slices.Equal(shownIDs(l), []string{"t1", "t2", "t3", "t5", "t6"}) {
		t.Fatal(l.Sort(), shownIDs(l))
	}
	var old SavedFilter
	if err := json.Unmarshal([]byte(`{"name":"legacy","list":"p/l","conditions":[{"prop":"score","op":"=","value":2}]}`), &old); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]SavedFilter{old})
	if err := r.Settings().Set("list.filters", string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := l.ApplyFilter("legacy"); err != nil || len(l.Sort()) != 0 {
		t.Fatal(err, l.Sort())
	}
}

func TestSavedFilterInvalidSortIsAtomicAndCycleSkips(t *testing.T) {
	r, _, l := savedSortFixture(t)
	_ = l.SetSort([]SortKey{{Field: "score", Desc: true}})
	_ = l.SetPropFilters([]PropFilter{{Prop: "score", Op: "=", Value: 3}})
	before := l.Snapshot()
	raw := `[{"name":"broken","list":"p/l","conditions":[],"sort":[{"field":"missing"}]}]`
	if err := r.Settings().Set("list.filters", raw); err != nil {
		t.Fatal(err)
	}
	if err := l.ApplyFilter("broken"); err == nil {
		t.Fatal("invalid sort accepted")
	}
	if !slices.Equal(l.Sort(), []SortKey{{Field: "score", Desc: true}}) || len(l.PropFilters()) != 1 {
		t.Fatal(before, l.Snapshot())
	}
	pressRun(t, r, "z", "]")
	if l.vs.Saved != "open" {
		t.Fatal(l.vs.Saved)
	}
	pressRun(t, r, "z", "[") // wraps past invalid last entry to all
	if l.vs.Saved != "all" || len(l.Sort()) != 0 {
		t.Fatal(l.vs.Saved, l.Sort())
	}
}

func TestCompactSavedFilterSaveCycleDelete(t *testing.T) {
	r, m, l := savedSortFixture(t)
	compactKeys(m, "z", "/")
	compactPart(t, m, "sort")
	compactKeys(m, "s", "c", "o", "r", "e", "enter")
	compactPart(t, m, "direction")
	compactKeys(m, "enter")
	compactPart(t, m, "save")
	compactKeys(m, "r", "a", "n", "k", "e", "d", "enter")
	if l.vs.Saved != "ranked" || len(l.Sort()) != 1 {
		t.Fatal(l.vs.Saved, l.Sort())
	}
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "Applied: ranked") || !strings.Contains(frame, "Replace") {
		t.Fatal(frame)
	}
	compactKeys(m, "esc", "esc", "z", "/")
	compactPart(t, m, "conditions")
	compactKeys(m, "z", "]")
	if l.vs.Saved != "open" || len(l.Sort()) != 0 {
		t.Fatal(l.vs.Saved, l.Sort())
	}
	compactKeys(m, "z", "[")
	if l.vs.Saved != "ranked" || !l.Sort()[0].Desc {
		t.Fatal(l.vs.Saved, l.Sort())
	}
	compactPart(t, m, "save")
	compactKeys(m, "C")
	if !strings.Contains(string(r.Input(r.Focus).Text), "C") || len(l.Sort()) != 1 {
		t.Fatal("C was not text")
	}
	compactPart(t, m, "conditions")
	compactKeys(m, "D", "r", "a", "n", "k", "e", "d", "enter")
	if r.TopModal() != "confirm" {
		t.Fatal("deletion did not ask", r.TopModal())
	}
	savedBefore, _ := l.SavedFilters()
	if !slices.ContainsFunc(savedBefore, func(f SavedFilter) bool { return f.Name == "ranked" }) {
		t.Fatal("deleted before confirmation")
	}
	compactKeys(m, "y")
	saved, _ := l.SavedFilters()
	if slices.ContainsFunc(saved, func(f SavedFilter) bool { return f.Name == "ranked" }) {
		t.Fatal("delete failed", r.Focus, r.TopModal(), m.Status, saved)
	}
}

func TestSavedFilterSortPersistenceOverwriteAndDelete(t *testing.T) {
	r, _, l := savedSortFixture(t)
	r.Config().Dir = t.TempDir()
	if err := l.SetSort([]SortKey{{Field: "status"}, {Field: "score", Desc: true}}); err != nil {
		t.Fatal(err)
	}
	if err := l.SaveFilter("open"); err != nil {
		t.Fatal(err)
	}
	fresh := NewRuntime()
	fresh.Config().Dir = r.Config().Dir
	filters, err := fresh.SavedFilters(l.id, "task")
	if err != nil {
		t.Fatal(err)
	}
	if len(filters) != 1 || len(filters[0].Sort) != 2 || !filters[0].Sort[1].Desc {
		t.Fatal(filters)
	}
	if err := l.SetSort(nil); err != nil {
		t.Fatal(err)
	}
	if err := l.SaveFilter("open"); err != nil {
		t.Fatal(err)
	}
	filters, _ = l.SavedFilters()
	if len(filters) != 2 || len(filters[0].Sort) != 0 {
		t.Fatal(filters)
	}
	if err := l.DeleteSavedFilter("open"); err != nil {
		t.Fatal(err)
	}
	if err := l.ApplyFilter("open"); err != nil || len(l.PropFilters()) != 1 {
		t.Fatal(err, l.PropFilters())
	}
	if err := l.DeleteSavedFilter("open"); err == nil {
		t.Fatal("deleted app declaration")
	}
}

func TestCompactSavedFilterOptOutAndMultiKeyIndicator(t *testing.T) {
	r, m, l := savedSortFixture(t)
	_ = l.SetSort([]SortKey{{Field: "status"}, {Field: "score", Desc: true}})
	compactKeys(m, "z", "/")
	if !strings.Contains(ansi.Strip(m.View().Content), "score ▼2") {
		t.Fatal(m.View().Content)
	}
	_ = r.Settings().Set(ListSortIndicatorSetting, false)
	if strings.Contains(ansi.Strip(m.View().Content), "▼2") {
		t.Fatal("indicator opt-out ignored")
	}
	compactKeys(m, "esc", "esc")
	_ = r.Settings().Set(ListFilterSaveSetting, false)
	compactKeys(m, "z", "/")
	if filterViewAt(r, r.Focus).saveInput != nil || strings.Contains(ansi.Strip(m.View().Content), "Save as") {
		t.Fatal("save opt-out ignored")
	}
}

func TestSavedSortInvalidConditionsLeaveSortAndName(t *testing.T) {
	r, _, l := savedSortFixture(t)
	_ = l.SetSort([]SortKey{{Field: "score", Desc: true}})
	_ = l.SaveFilter("ranked")
	raw := `[{"name":"bad","list":"p/l","conditions":[{"prop":"missing","value":"x"}],"sort":[{"field":"status"}]}]`
	if err := r.Settings().Set("list.filters", raw); err != nil {
		t.Fatal(err)
	}
	if err := l.ApplyFilter("bad"); err == nil || l.vs.Saved != "ranked" || !l.Sort()[0].Desc || l.Sort()[0].Field != "score" {
		t.Fatal(err, l.Snapshot())
	}
}

func TestCompactSavedFilterChooserUsesSameSaveAndCycle(t *testing.T) {
	r, m, c := chooserPropFixture(t)
	compactKeys(m, "ctrl+f")
	compactPart(t, m, "sort")
	compactKeys(m, "s", "c", "o", "r", "e", "enter")
	compactPart(t, m, "direction")
	compactKeys(m, "enter")
	compactPart(t, m, "save")
	compactKeys(m, "r", "a", "n", "k", "e", "d", "enter", "esc")
	if c.filters.vs.Saved != "ranked" || !slices.Equal(chooserIDs(c), []string{"c", "b", "a"}) {
		t.Fatal(c.state())
	}
	compactKeys(m, "z", "]")
	if c.filters.vs.Saved != "mine" || len(c.Sort()) != 0 || !slices.Equal(chooserIDs(c), []string{"a", "c"}) {
		t.Fatal(c.state())
	}
	compactKeys(m, "z", "[")
	if c.filters.vs.Saved != "ranked" || len(c.Sort()) != 1 || !c.Sort()[0].Desc {
		t.Fatal(c.state())
	}
	_ = r
}

func TestListSortPlainIndicatorIsSeparateFromConditions(t *testing.T) {
	r, _, l := savedSortFixture(t)
	if err := l.SetSort([]SortKey{{Field: "score"}, {Field: "status", Desc: true}}); err != nil {
		t.Fatal(err)
	}
	text := ansi.Strip(l.View(90, l.Height()))
	if !strings.Contains(text, "sort score ▲ · status ▼2") || l.Filtering() || len(l.PropFilters()) != 0 {
		t.Fatal(text, l.PropFilters())
	}
	_ = r.Settings().Set(ListSortIndicatorSetting, false)
	if strings.Contains(ansi.Strip(l.View(90, l.Height())), "sort score") {
		t.Fatal("plain sort indicator opt-out ignored")
	}
}
