package ochazuke

import (
	"reflect"
	"strings"
	"testing"
)

// rowPropsFixture is a list of lane rows whose noun declares only title
// (and a secret token): state, count, tags, id, done and mixed are fields
// of the rows' data alone.
func rowPropsFixture(t *testing.T) (*Runtime, *Model, *List) {
	t.Helper()
	r, m, l := filterFixture(t)
	r.Define("a").Props(PropSpec{Name: "title", Parameter: Parameter{Enum: []string{"one", "two"}}}, PropSpec{Name: "token", Secret: true}, PropSpec{Name: "note", NoFilter: true})
	items := l.Items()
	states := []string{"running", "done", "running", "queued", "done"}
	for i := range items {
		var mixed any = "x"
		if i%2 == 1 {
			mixed = 3
		}
		items[i].Data = map[string]any{
			"id": items[i].ID, "title": "one", "token": "s3cret", "note": "n",
			"state": states[i], "count": float64(i), "done": i%2 == 0,
			"tags": []any{"a", states[i]}, "mixed": mixed, "nested": map[string]any{"x": 1}, "empty": nil,
		}
	}
	if err := l.SetItems(items); err != nil {
		t.Fatal(err)
	}
	return r, m, l
}

func TestRowPropsOfferUndeclaredFields(t *testing.T) {
	_, _, l := rowPropsFixture(t)
	got := l.filterProps()
	want := []string{"count", "done", "empty", "id", "mixed", "state", "tags", "title"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("props %v, want %v (no secret, no NoFilter, no object)", got, want)
	}
	specs := l.filterSpecs()
	for name, typ := range map[string]string{"count": SettingNumber, "done": SettingBoolean, "tags": SettingList, "state": SettingString, "mixed": SettingString} {
		p := specs[name]
		if p.Type != typ || !p.Derived || !p.ReadOnly {
			t.Fatalf("%s: %+v", name, p)
		}
	}
	if p := specs["title"]; p.Derived || len(p.Enum) != 2 {
		t.Fatalf("a declared prop must win: %+v", p)
	}
	for _, tc := range []struct {
		f    PropFilter
		want []string
	}{
		{PropFilter{"state", "=", "running"}, []string{"r1", "r3"}},
		{PropFilter{"count", "=", "2"}, []string{"r3"}},
		{PropFilter{"done", "=", true}, []string{"r1", "r3", "r5"}},
		{PropFilter{"tags", "contains", "queued"}, []string{"r4"}},
		{PropFilter{"mixed", "=", "3"}, []string{"r2", "r4"}},
		{PropFilter{"id", "in", []any{"r2", "r5"}}, []string{"r2", "r5"}},
	} {
		if err := l.SetPropFilters([]PropFilter{tc.f}); err != nil {
			t.Fatalf("%+v: %v", tc.f, err)
		}
		if got := shownIDs(l); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%+v: shown %v, want %v", tc.f, got, tc.want)
		}
	}
	for _, f := range []PropFilter{{"token", "=", "s3cret"}, {"note", "=", "n"}, {"nested", "=", "x"}, {"absent", "=", "x"}} {
		if err := l.SetPropFilters([]PropFilter{f}); err == nil {
			t.Fatalf("filter on %s must be refused", f.Prop)
		}
	}
}

func TestRowPropsPickerAndCacheByRows(t *testing.T) {
	r, m, l := rowPropsFixture(t)
	chooserChain(t, r) // the prop chooser then the value chooser (list.filter-form off)
	if err := r.Settings().Set(PropsOffSetting, `["*.count","*.done","*.empty","*.id","*.mixed","*.tags","*.title"]`); err != nil {
		t.Fatal(err)
	}
	pressKeys(m, "z", "/")
	c := r.Chooser("list-filter-prop")
	if c == nil {
		t.Fatal("prop picker did not open")
	}
	if ms := c.Matches(); len(ms) != 1 || ms[0].ID != "state" {
		t.Fatalf("prop picker rows %+v", ms)
	}
	pressKeys(m, "enter")
	v := r.Chooser("list-filter-value")
	if v == nil {
		t.Fatal("value picker did not open")
	}
	var labels []string
	for _, row := range v.Matches() {
		labels = append(labels, row.Label)
	}
	if !reflect.DeepEqual(labels, []string{"done", "queued", "running"}) {
		t.Fatal(labels)
	}
	pressKeys(m, "r", "u", "n", "enter")
	if got := shownIDs(l); !reflect.DeepEqual(got, []string{"r1", "r3"}) {
		t.Fatalf("state=running shown %v", got)
	}
	// New rows are read again: a field no row had before is offered.
	items := l.Items()
	items[0].Data.(map[string]any)["owner"] = "me"
	if err := l.SetPropFilters(nil); err != nil {
		t.Fatal(err)
	}
	if err := l.SetItems(items); err != nil {
		t.Fatal(err)
	}
	if got := l.filterProps(); !reflect.DeepEqual(got, []string{"owner", "state"}) {
		t.Fatalf("after new rows %v", got)
	}
}

func TestRowPropsOffByNounAndSetting(t *testing.T) {
	r, _, l := rowPropsFixture(t)
	r.Define("a").PropsOff("count", "ta*")
	if err := r.Settings().Set(PropsOffSetting, `["a.state", "*.mixed"]`); err != nil {
		t.Fatal(err)
	}
	if got := l.filterProps(); !reflect.DeepEqual(got, []string{"done", "empty", "id", "title"}) {
		t.Fatal(got)
	}
	if err := r.Settings().Set(PropsOffSetting, `["*"]`); err != nil {
		t.Fatal(err)
	}
	if got := l.filterProps(); len(got) != 0 {
		t.Fatalf("* turns every prop off: %v", got)
	}
}

func TestRowPropsSavedFilterRoundTrip(t *testing.T) {
	r, _, l := rowPropsFixture(t)
	r.Config().Dir = t.TempDir()
	r.Define("a").Filters(SavedFilter{Name: "live", Where: map[string]any{"state": "running"}})
	if err := l.ApplyFilter("live"); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !reflect.DeepEqual(got, []string{"r1", "r3"}) {
		t.Fatal(got)
	}
	if err := l.SetPropFilters([]PropFilter{{"tags", "contains", "done"}, {"count", "!=", 1}}); err != nil {
		t.Fatal(err)
	}
	if err := l.SaveFilter("done-late"); err != nil {
		t.Fatal(err)
	}
	if err := l.SetPropFilters(nil); err != nil {
		t.Fatal(err)
	}
	if err := l.ApplyFilter("done-late"); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !reflect.DeepEqual(got, []string{"r5"}) {
		t.Fatal(got)
	}
	if text := r.Settings().String("list.filters"); !strings.Contains(text, `"prop":"tags"`) {
		t.Fatal(text)
	}
}

func TestRowPropsNeverWritable(t *testing.T) {
	r, _ := newWriteRuntime(t)
	l, err := r.AddList("lanes-test", []ListItem{{ID: "t1", Label: "one", Noun: "task", Data: Thing{"id": "t1", "lane": "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	if p := l.filterSpecs()["lane"]; !p.Derived || !p.ReadOnly {
		t.Fatalf("lane is filterable and read-only: %+v", p)
	}
	_, err = r.Execute(Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "lane", "value": "x"}})
	if err == nil || !strings.Contains(err.Error(), "only declared props are writable") {
		t.Fatalf("a field that is not declared must refuse a write: %v", err)
	}
}

func TestRowPropsTableCells(t *testing.T) {
	r := NewRuntime()
	r.Define("a")
	l, err := r.AddList("t", []ListItem{
		{ID: "x", Label: "x", Noun: "a", Cells: []string{"x", "running"}},
		{ID: "y", Label: "y", Noun: "a", Cells: []string{"y", "idle"}},
	}, Columns(Column{Title: "Name"}, Column{Title: "Run State"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := l.filterProps(); !reflect.DeepEqual(got, []string{"name", "run-state"}) {
		t.Fatal(got)
	}
	if err := l.SetPropFilters([]PropFilter{{"run-state", "=", "idle"}}); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !reflect.DeepEqual(got, []string{"y"}) {
		t.Fatal(got)
	}
}
