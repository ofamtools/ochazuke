package ochazuke

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func propFilterFixture(t *testing.T) (*Runtime, *Model, *List) {
	t.Helper()
	r, m, l := filterFixture(t)
	r.Define("a").Props(PropSpec{Name: "by"}, PropSpec{Name: "score", Parameter: Parameter{Type: "integer"}}, PropSpec{Name: "secret", Secret: true})
	items := l.Items()
	for i := range items {
		by := "owner"
		if i%2 == 1 {
			by = "agent"
		}
		items[i].Data = Thing{"by": by, "score": i + 1, "secret": "hidden"}
	}
	if err := l.SetItems(items); err != nil {
		t.Fatal(err)
	}
	r.Define("a").Filters(SavedFilter{Name: "mine", Where: map[string]any{"by": "owner"}}, SavedFilter{Name: "agents", Where: map[string]any{"by": "agent"}}, SavedFilter{Name: "all"})
	return r, m, l
}

func TestPropFilterMatcher(t *testing.T) {
	r, _ := newTaskRuntime(t)
	n := r.Noun("task")
	thing := Thing{"estimate": 2.0, "status": "todo", "title": "Write notes"}
	for _, tc := range []struct {
		f    PropFilter
		want bool
	}{
		{PropFilter{"estimate", "=", "2"}, true},
		{PropFilter{"estimate", "!=", 3}, true},
		{PropFilter{"title", "contains", "notes"}, true},
		{PropFilter{"title", "contains", "Notes"}, false},
		{PropFilter{"estimate", "in", []any{"1", "2"}}, true},
		{PropFilter{"status", "in", `["doing","done"]`}, false},
	} {
		f, err := typedFilter(n, tc.f)
		if err != nil {
			t.Fatal(err)
		}
		if got := propsMatch(n, thing, []PropFilter{f}); got != tc.want {
			t.Fatalf("%+v: got %v", tc.f, got)
		}
	}
	if !thingMatches(n, thing, map[string]any{"estimate": "2"}) {
		t.Fatal("where must share typed equality")
	}
	for _, f := range []PropFilter{{"bad", "=", "x"}, {"estimate", "contains", "2"}, {"status", "bogus", "todo"}, {"estimate", "in", `["bad"]`}, {"status", "in", nil}} {
		if _, err := typedFilter(n, f); err == nil {
			t.Fatalf("accepted invalid %+v", f)
		}
	}
}

func TestPropFilterListTextRefreshMarks(t *testing.T) {
	r, m, l := propFilterFixture(t)
	_ = l.ToggleMark("r1")
	if err := l.SetPropFilters([]PropFilter{{"by", "=", "owner"}, {"score", "!=", 5}}); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !reflect.DeepEqual(got, []string{"r1", "r3"}) {
		t.Fatal(got)
	}
	pressKeys(m, "/", "g", "enter")
	if got := shownIDs(l); !reflect.DeepEqual(got, []string{"r3"}) {
		t.Fatal(got)
	}
	if err := l.SetPropFilters([]PropFilter{{"bad", "=", "x"}}); err == nil {
		t.Fatal("invalid prop accepted")
	}
	if l.Shown() != 1 {
		t.Fatal("invalid update changed state")
	}
	items := l.Items()
	items[2].Data = Thing{"by": "agent", "score": 3}
	if err := l.SetItems(items); err != nil {
		t.Fatal(err)
	}
	if l.Shown() != 0 || r.Focus != l.id {
		t.Fatalf("shown %d focus %s", l.Shown(), r.Focus)
	}
	if err := l.SetPropFilters(nil); err != nil {
		t.Fatal(err)
	}
	if l.Shown() != 1 || r.Focus != "r3" || !l.marks["r1"] {
		t.Fatalf("shown %d focus %s marks %v", l.Shown(), r.Focus, l.marks)
	}
	pressKeys(m, "esc")
	if l.Shown() != 5 {
		t.Fatal("text clear")
	}
	if _, err := r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
}

func TestPropFilterPickerCycleAndStrip(t *testing.T) {
	r, m, l := propFilterFixture(t)
	pressKeys(m, "z", "/", "enter") // by is the first prop; observed values agent,owner
	chooser := r.Chooser("list-filter-value")
	if chooser == nil || len(chooser.Matches()) != 2 {
		t.Fatal("missing value picker")
	}
	pressKeys(m, "enter")
	if l.Shown() != 2 || len(l.propFilters) != 1 {
		t.Fatalf("shown %d filters %v", l.Shown(), l.propFilters)
	}
	if text := ansi.Strip(l.View(90, 10)); !strings.Contains(text, "[by = agent]") {
		t.Fatal(text)
	}
	pressKeys(m, "z", "C", "z", "]")
	if l.savedFilter != "mine" || l.Shown() != 3 {
		t.Fatalf("saved %s shown %d", l.savedFilter, l.Shown())
	}
	pressKeys(m, "z", "]")
	if l.savedFilter != "agents" || l.Shown() != 2 {
		t.Fatal("cycle agents")
	}
	pressKeys(m, "z", "]")
	if l.savedFilter != "all" || l.Shown() != 5 {
		t.Fatal("cycle all")
	}
	pressKeys(m, "z", "[")
	if l.savedFilter != "agents" {
		t.Fatal("reverse")
	}
	if err := r.Settings().Set("list.filter-strip", false); err != nil {
		t.Fatal(err)
	}
	if text := ansi.Strip(l.View(90, 10)); strings.Contains(text, "[agents]") || strings.Contains(text, "[by") {
		t.Fatal(text)
	}
	if err := r.Settings().Set("list.filter-cycle", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-cycle"}); err == nil {
		t.Fatal("cycle opt out")
	}
}

func TestPropFilterSavedSettingsAndCLI(t *testing.T) {
	r, s := newTaskRuntime(t)
	r.Define("task").Filters(SavedFilter{Name: "todo", Where: map[string]any{"status": "todo"}}, SavedFilter{Name: "all"})
	// A store which limits eagerly must see no limit until saved conditions
	// have run. The first thing doesn't match the named filter.
	s.things[0]["status"] = "doing"
	r.Define("task").Store(Store{List: func(_ Ctx, q ListQuery) ([]Thing, error) {
		if q.Limit > 0 {
			return s.things[:q.Limit], nil
		}
		return s.things, nil
	}})
	result, err := r.Execute(Request{Noun: "task", Verb: "list", Args: Arguments{"filter": "todo", "where": "estimate=1", "limit": 1}})
	if err != nil {
		t.Fatal(err)
	}
	rows := result.([]map[string]any)
	if len(rows) != 1 || rows[0]["id"] != "t3" {
		t.Fatal(rows)
	}
	if _, err := r.Execute(Request{Noun: "task", Verb: "list", Args: Arguments{"filter": "missing"}}); err == nil {
		t.Fatal("missing filter accepted")
	}
	setting := `[{"name":"todo","noun":"task","where":{"status":"doing"}},{"name":"todo","list":"one","where":{"status":"done"}}]`
	if err := r.Settings().Set("list.filters", setting); err != nil {
		t.Fatal(err)
	}
	filters, err := r.SavedFilters("one", "task")
	if err != nil {
		t.Fatal(err)
	}
	if filters[0].Where["status"] != "done" {
		t.Fatal(filters)
	}
	filters, err = r.SavedFilters("two", "task")
	if err != nil {
		t.Fatal(err)
	}
	if filters[0].Where["status"] != "doing" {
		t.Fatal(filters)
	}
	filters, err = r.SavedFilters("", "task")
	if err != nil {
		t.Fatal(err)
	}
	if filters[0].Where["status"] != "doing" {
		t.Fatal(filters)
	}
	_ = r.Settings().Set("list.filters", "broken")
	if _, err := r.SavedFilters("", "task"); err == nil {
		t.Fatal("invalid settings ignored")
	}
}

func TestPropFilterTreeKeepsAncestorsAndCollapse(t *testing.T) {
	r := NewRuntime()
	r.Define("node").Props(PropSpec{Name: "by"})
	tree, err := r.AddTree("tree", []TreeNode{
		{ID: "root", Label: "Root", Noun: "node", Data: Thing{"by": "agent"}},
		{ID: "middle", Parent: "root", Label: "Middle", Noun: "node", Data: Thing{"by": "agent"}},
		{ID: "mine", Parent: "middle", Label: "My note", Noun: "node", Data: Thing{"by": "owner"}},
		{ID: "other", Parent: "root", Label: "Agent note", Noun: "node", Data: Thing{"by": "agent"}},
		{ID: "unrelated", Label: "Unrelated", Noun: "node", Data: Thing{"by": "agent"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = tree.SetCollapsed("root", true)
	if err := tree.list.SetPropFilters([]PropFilter{{"by", "=", "owner"}}); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(tree.list); !reflect.DeepEqual(got, []string{"root", "middle", "mine"}) {
		t.Fatal(got)
	}
	if !tree.Collapsed("root") {
		t.Fatal("filter mutated collapse state")
	}
	tree.list.SetFilter("My")
	if got := shownIDs(tree.list); !reflect.DeepEqual(got, []string{"root", "middle", "mine"}) {
		t.Fatal(got)
	}
	tree.list.SetFilter("nothing")
	if tree.list.Shown() != 0 {
		t.Fatal("text and props must combine")
	}
	tree.list.SetFilter("")
	if err := tree.list.SetPropFilters(nil); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(tree.list); !reflect.DeepEqual(got, []string{"root", "unrelated"}) {
		t.Fatal(got)
	}
}

func TestPropFilterSavedPersistence(t *testing.T) {
	r, _, _ := propFilterFixture(t)
	r.Config().Dir = t.TempDir()
	input := `[{"name":"only","noun":"a","conditions":[{"prop":"by","op":"in","value":["owner"]}]}]`
	if err := r.Settings().Set("list.filters", input); err != nil {
		t.Fatal(err)
	}
	fresh := NewRuntime()
	fresh.Config().Dir = r.Config().Dir
	filters, err := fresh.SavedFilters("", "a")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(filters)
	if !strings.Contains(string(raw), `"name":"only"`) {
		t.Fatal(string(raw))
	}
}

func TestPropFilterTargetAndClearOne(t *testing.T) {
	r, _, l := propFilterFixture(t)
	// An API/CLI request can name an unfocused list (and can do so when its
	// active conditions leave no selected row).
	r.Instance("elsewhere", Parent("p"))
	if err := r.SetFocus("elsewhere"); err != nil {
		t.Fatal(err)
	}
	for _, by := range []string{"owner", "agent"} {
		if _, err := r.Execute(Request{Noun: "list", Verb: "filter-prop", Args: Arguments{"id": l.id, "prop": "by", "value": by}}); err != nil {
			t.Fatal(err)
		}
	}
	if l.Shown() != 0 {
		t.Fatal("two conditions on one prop must AND")
	}
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-prop-clear", Args: Arguments{"id": l.id, "index": 2}}); err != nil {
		t.Fatal(err)
	}
	if l.Shown() != 3 || len(l.propFilters) != 1 {
		t.Fatal("clear removed more than one condition")
	}
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-props-clear", Args: Arguments{"id": l.id}}); err != nil {
		t.Fatal(err)
	}
	if l.Shown() != 5 {
		t.Fatal("clear all")
	}
}

func TestPropFilterPickerVisibleValuesOnly(t *testing.T) {
	r, m, l := propFilterFixture(t)
	l.SetFilter("beta")
	pressKeys(m, "z", "/", "enter")
	c := r.Chooser("list-filter-value")
	if c == nil || len(c.Matches()) != 1 || c.Matches()[0].Label != "agent" {
		t.Fatal("picker used hidden rows")
	}
	pressKeys(m, "esc")
	if len(l.propFilters) != 0 || l.Filter() != "beta" {
		t.Fatal("cancel changed filters")
	}
}

func TestPropFilterSavedOperatorsAndMissingProps(t *testing.T) {
	r, _ := newTaskRuntime(t)
	r.Define("task").Filters(SavedFilter{Name: "notes", Conditions: []PropFilter{{"title", "contains", "notes"}, {"estimate", "in", []any{2, 3}}}})
	answer, err := r.Execute(Request{Noun: "task", Verb: "list", Args: Arguments{"filter": "notes"}})
	if err != nil {
		t.Fatal(err)
	}
	rows := answer.([]map[string]any)
	if len(rows) != 1 || rows[0]["id"] != "t2" {
		t.Fatal(rows)
	}
	f, err := typedFilter(r.Noun("task"), PropFilter{"status", "!=", "done"})
	if err != nil {
		t.Fatal(err)
	}
	if propsMatch(r.Noun("task"), Thing{}, []PropFilter{f}) {
		t.Fatal("missing props should not match")
	}
}

func TestPropFilterDurationAndCopies(t *testing.T) {
	r := NewRuntime()
	r.Define("job").Props(PropSpec{Name: "delay", Parameter: Parameter{Type: "duration"}}).Filters(SavedFilter{Name: "short", Where: map[string]any{"delay": 400 * time.Millisecond}})
	l, err := r.AddList("jobs", []ListItem{{ID: "j1", Noun: "job", Data: Thing{"delay": 400 * time.Millisecond}}, {ID: "j2", Noun: "job", Data: Thing{"delay": time.Second}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.ApplyFilter("short"); err != nil {
		t.Fatal(err)
	}
	if l.Shown() != 1 {
		t.Fatal("duration saved filter lost its type")
	}
	if err := l.SetPropFilters([]PropFilter{{"delay", "in", []any{400 * time.Millisecond}}}); err != nil {
		t.Fatal(err)
	}
	copied := l.PropFilters()
	copied[0].Value.([]any)[0] = "2s"
	if l.Shown() != 1 || l.PropFilters()[0].Value.([]any)[0] != "400ms" {
		t.Fatal("filter snapshot shares an array")
	}
	if err := l.SetPropFilters(l.PropFilters()); err != nil {
		t.Fatal(err)
	}
	if l.Shown() != 1 {
		t.Fatal("typed duration array copy changed matching")
	}
}

func TestPropFilterStandardPositionalOrder(t *testing.T) {
	r, _ := newTaskRuntime(t)
	op, _ := r.Operation("task.list")
	if want := []string{"limit", "props", "where", "filter"}; !reflect.DeepEqual(op.ParameterOrder(), want) {
		t.Fatalf("order %v want %v", op.ParameterOrder(), want)
	}
	r.Define("task").Store(Store{List: func(Ctx, ListQuery) ([]Thing, error) { return nil, nil }, ListParams: map[string]Parameter{"project": {Type: "string", Required: true}}})
	op, _ = r.Operation("task.list")
	if want := []string{"project", "limit", "props", "where", "filter"}; !reflect.DeepEqual(op.ParameterOrder(), want) {
		t.Fatalf("order %v want %v", op.ParameterOrder(), want)
	}
}
