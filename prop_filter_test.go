package ochazuke

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	// z / is hidden from the footer (every list with data offers it; ? lists it).
	if hints := BindingHints(r, r.Focus); strings.Contains(hints, "z / prop filter") || strings.Contains(hints, "z ! clear prop") {
		t.Fatal(hints)
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
	if err := r.Settings().Set("list.filters", "broken"); err == nil || !strings.Contains(err.Error(), "JSON array") {
		t.Fatalf("invalid setting write: %v", err)
	}
	if got := r.Settings().String("list.filters"); got != setting {
		t.Fatal("invalid write replaced setting")
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

func TestPropFilterFocusRevealsHiddenRow(t *testing.T) {
	r, _, l := propFilterFixture(t)
	for _, via := range []string{"focus.set", "job focus"} {
		t.Run(via, func(t *testing.T) {
			if err := l.ApplyFilter("mine"); err != nil {
				t.Fatal(err)
			}
			if via == "focus.set" {
				if _, err := r.Execute(Request{Noun: "focus", Verb: "set", Args: Arguments{"id": "r2"}}); err != nil {
					t.Fatal(err)
				}
			} else if err := r.SetFocus("r2"); err != nil {
				t.Fatal(err)
			}
			selected, _ := l.Selected()
			if selected.ID != "r2" || r.Focus != "r2" || l.Shown() != 5 || len(l.PropFilters()) != 0 {
				t.Fatalf("focus %s selected %s shown %d", r.Focus, selected.ID, l.Shown())
			}
		})
	}
}

func propTreeFixture(t *testing.T) (*Runtime, *Tree) {
	t.Helper()
	r := NewRuntime()
	r.Define("node").Props(PropSpec{Name: "by"})
	tree, err := r.AddTree("tree", []TreeNode{
		{ID: "root", Label: "Root", Noun: "node", Data: Thing{"by": "agent"}},
		{ID: "mid", Parent: "root", Label: "Middle", Noun: "node", Data: Thing{"by": "agent"}},
		{ID: "deep", Parent: "mid", Label: "Deep note", Noun: "node", Data: Thing{"by": "owner"}},
		{ID: "other", Label: "Other", Noun: "node", Data: Thing{"by": "agent"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, tree
}

func TestPropFilterTreeSelectionMarksAndInstances(t *testing.T) {
	r, tree := propTreeFixture(t)
	l := tree.List()
	_ = tree.SetCollapsed("root", true)
	if err := l.SetPropFilters([]PropFilter{{"by", "=", "owner"}}); err != nil {
		t.Fatal(err)
	}
	_ = l.Select("deep")
	_ = r.SetFocus("deep")
	_ = l.ToggleMark("deep")
	inst := r.Instances["deep"]
	if err := l.SetPropFilters(nil); err != nil {
		t.Fatal(err)
	}
	selected, _ := l.Selected()
	if selected.ID != "root" || r.Focus != "root" {
		t.Fatalf("selected %s focus %s", selected.ID, r.Focus)
	}
	if !l.marks["deep"] || r.Instances["deep"] != inst {
		t.Fatal("hidden node lost its mark or instance")
	}
	_ = tree.SetCollapsed("root", false)
	_ = l.Select("deep")
	l.SetFilter("Deep")
	l.SetFilter("")
	selected, _ = l.Selected()
	if selected.ID != "deep" {
		t.Fatal("clearing text jumped from the deep row")
	}
	l.back = "deep"
	l.SetFilter("nothing")
	l.SetFilter("")
	selected, _ = l.Selected()
	if selected.ID != "deep" {
		t.Fatal("widening lost the starting row")
	}
	nodes := tree.Nodes()[:2]
	if err := tree.SetNodes(nodes); err != nil {
		t.Fatal(err)
	}
	if l.marks["deep"] || r.Instances["deep"] != nil {
		t.Fatal("deleted node retained mark or instance")
	}
	selected, _ = l.Selected()
	if selected.ID != "mid" {
		t.Fatal("deleting a node lost the nearest-index fallback")
	}
}

func TestPropFilterTreeFoldsAndTextCompatibility(t *testing.T) {
	_, tree := propTreeFixture(t)
	l := tree.List()
	_ = tree.SetCollapsed("root", true)
	l.SetFilter("Deep")
	if l.Shown() != 0 {
		t.Fatal("text filter expanded a collapsed node")
	}
	l.SetFilter("")
	_ = tree.SetCollapsed("root", false)
	l.SetFilter("Deep")
	if got := shownIDs(l); !reflect.DeepEqual(got, []string{"deep"}) {
		t.Fatal(got)
	}
	l.SetFilter("")
	_ = tree.SetCollapsed("root", true)
	if err := l.SetPropFilters([]PropFilter{{"by", "=", "owner"}}); err != nil {
		t.Fatal(err)
	}
	_ = tree.SetCollapsed("mid", true)
	if got := shownIDs(l); !reflect.DeepEqual(got, []string{"root", "mid"}) {
		t.Fatal(got)
	}
	_ = tree.SetCollapsed("mid", false)
	if l.Shown() != 3 {
		t.Fatal("expanding filtered context had no visible effect")
	}
	_ = l.SetPropFilters(nil)
	if !tree.Collapsed("root") || tree.Collapsed("mid") {
		t.Fatal("filter folds changed original collapse state")
	}
}

func TestPropFilterCycleInvalidAndEmpty(t *testing.T) {
	r, m, l := propFilterFixture(t)
	if err := r.Settings().Set("list.filters", `[{"name":"broken","noun":"a","where":{"missing":"x"}}]`); err != nil {
		t.Fatal(err)
	}
	_ = l.ApplyFilter("all")
	pressKeys(m, "z", "]")
	if l.savedFilter != "mine" || !strings.Contains(m.Info, "Skipped saved filters: broken") {
		t.Fatalf("saved %s info %s", l.savedFilter, m.Info)
	}
	_ = l.ApplyFilter("mine")
	pressKeys(m, "z", "[")
	if l.savedFilter != "all" || !strings.Contains(m.Info, "broken") {
		t.Fatalf("reverse %s info %s", l.savedFilter, m.Info)
	}
	_ = r.Settings().Set("list.filter-strip", false)
	_ = l.SetPropFilters([]PropFilter{{"by", "=", "none"}})
	if text := ansi.Strip(l.View(60, max(1, l.Height()))); !strings.Contains(text, "0 of 5 (filtered)") {
		t.Fatal(text)
	}
}

func TestPropFilterMalformedSettingsLoad(t *testing.T) {
	r := NewRuntime()
	r.Config().Dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(r.Config().Dir, SettingsFile), []byte(`{"list.filters":"broken"}`), 0600); err != nil {
		t.Fatal(err)
	}
	problems := r.Settings().Problems()
	if len(problems) != 1 || !strings.Contains(problems[0], "list.filters") || !strings.Contains(problems[0], "JSON array") {
		t.Fatal(problems)
	}
	if r.Settings().String("list.filters") != "[]" {
		t.Fatal("malformed setting not defaulted")
	}
}

func TestPropFilterSavedManagement(t *testing.T) {
	r, _, l := propFilterFixture(t)
	r.Config().Dir = t.TempDir()
	_ = l.ApplyFilter("agents")
	for _, verb := range []string{"filter-save", "filters", "filter-saved-pick"} {
		args := Arguments{"id": l.id}
		if verb == "filter-save" {
			args["name"] = "local"
		}
		if _, err := r.Execute(Request{Noun: "list", Verb: verb, Args: args}); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
	}
	if c := r.Chooser("list-filter-saved"); c == nil || len(c.Matches()) != 4 {
		t.Fatal("saved filter chooser missing definitions")
	}
	_ = r.Pop()
	_ = l.SetPropFilters(nil)
	if err := l.ApplyFilter("local"); err != nil || l.Shown() != 2 {
		t.Fatalf("apply: %v shown %d", err, l.Shown())
	}
	fresh := NewRuntime()
	fresh.Config().Dir = r.Config().Dir
	filters, err := fresh.SavedFilters(l.id, "a")
	if err != nil || len(filters) != 1 || filters[0].Name != "local" {
		t.Fatalf("persisted %+v %v", filters, err)
	}
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-saved-delete", Args: Arguments{"id": l.id, "name": "local"}}); err != nil {
		t.Fatal(err)
	}
	filters, err = l.SavedFilters()
	if err != nil || len(filters) != 3 {
		t.Fatalf("delete %+v %v", filters, err)
	}
}

type countingFilterData struct {
	calls *int
	By    string
}

func (d countingFilterData) MarshalJSON() ([]byte, error) {
	*d.calls++
	return json.Marshal(Thing{"by": d.By})
}

func TestPropFilterCachesAndVisibility(t *testing.T) {
	r, _, l := propFilterFixture(t)
	calls := 0
	items := l.Items()
	for i := range items {
		items[i].Data = countingFilterData{&calls, "owner"}
	}
	_ = l.SetItems(items)
	r.DefineGuard("at-list", func(_ *Runtime, ev KeyEvent) bool { return ev.Focus == l.id })
	r.Noun("a").prop("by").When = "at-list"
	_ = l.SetPropFilters([]PropFilter{{"by", "=", "owner"}})
	if l.Shown() != 5 {
		t.Fatal("matcher used a different visibility focus")
	}
	decoded := calls
	for _, q := range []string{"a", "al", "", "z", ""} {
		l.SetFilter(q)
		BindingHints(r, r.Focus)
	}
	if calls != decoded || decoded != 5 {
		t.Fatalf("decoded %d times, want 5", calls)
	}
	if err := l.pickPropFilter("by", false); err != nil {
		t.Fatal(err)
	}
	_ = r.Pop()
	items[0].Data = countingFilterData{&calls, "agent"}
	_ = l.SetItems(items)
	if l.Shown() != 4 {
		t.Fatal("SetItems did not invalidate cached props")
	}
	if l.matchApplies() {
		t.Fatal("prop-only filter enables text match-mode keys")
	}
	l.SetFilter("a")
	if !l.matchApplies() {
		t.Fatal("text filter lost match-mode keys")
	}
}

func TestPropFilterTreeCacheInvalidation(t *testing.T) {
	_, tree := propTreeFixture(t)
	calls := 0
	nodes := tree.Nodes()
	for i := range nodes {
		nodes[i].Data = countingFilterData{&calls, "owner"}
	}
	_ = tree.SetNodes(nodes)
	_ = tree.List().SetPropFilters([]PropFilter{{"by", "=", "owner"}})
	decoded := calls
	for _, q := range []string{"D", "De", "Deep", ""} {
		tree.List().SetFilter(q)
	}
	if calls != decoded || decoded != 4 {
		t.Fatalf("tree refilter decoded %d times", calls)
	}
	nodes[2].Data = countingFilterData{&calls, "agent"}
	_ = tree.SetNodes(nodes)
	if tree.List().shows("deep") {
		t.Fatal("node change kept stale cached props")
	}
}

func TestPropFilterTreeExpandKeepsFilter(t *testing.T) {
	r, tree := propTreeFixture(t)
	nodes := tree.Nodes()
	nodes = append(nodes[:1], append([]TreeNode{{ID: "hidden-first", Parent: "root", Label: "Hidden", Noun: "node", Data: Thing{"by": "agent"}}}, nodes[1:]...)...)
	if err := tree.SetNodes(nodes); err != nil {
		t.Fatal(err)
	}
	_ = tree.List().SetPropFilters([]PropFilter{{"by", "=", "owner"}})
	_ = r.SetFocus("root")
	if _, err := r.Execute(Request{Noun: "outline", Verb: "expand"}); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "mid" || len(tree.List().PropFilters()) != 1 {
		t.Fatalf("focus %s conditions %v", r.Focus, tree.List().PropFilters())
	}
}
