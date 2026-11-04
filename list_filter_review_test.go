package ochazuke

import (
	"reflect"
	"testing"
)

func TestFilterFormAbsentConditionValue(t *testing.T) {
	for _, layout := range []string{"compact", "fields"} {
		t.Run(layout, func(t *testing.T) {
			r, m, l := compactFixture(t)
			_ = r.Settings().Set(ListFilterLayoutSetting, layout)
			if err := l.SetPropFilters([]PropFilter{{Prop: "status", Value: "archived"}}); err != nil {
				t.Fatal(err)
			}
			compactKeys(m, "z", "/")
			e := r.RecordEditor(listFilterFormID)
			f := e.field("status")
			if err := e.set(f, ChipsValue([]string{"archived", "todo"})); err != nil {
				t.Fatal(err)
			}
			if err := e.seal(f); err != nil {
				t.Fatal(err)
			}
			want := []PropFilter{{Prop: "status", Op: "in", Value: []any{"archived", "todo"}}}
			if !reflect.DeepEqual(l.PropFilters(), want) {
				t.Fatalf("conditions = %#v, want %#v; field problem: %s", l.PropFilters(), want, e.problem(f))
			}
		})
	}
}

func TestFilterFormRemoveNegatedConditionResetsSign(t *testing.T) {
	r, m, l := compactFixture(t)
	if err := l.SetPropFilters([]PropFilter{{Prop: "status", Op: "!=", Value: "todo"}}); err != nil {
		t.Fatal(err)
	}
	compactKeys(m, "z", "/")
	v := filterViewAt(r, r.Focus)
	if err := v.replaceConditions(nil); err != nil {
		t.Fatal(err)
	}
	if err := v.setField("status", ChipsValue([]string{"done"})); err != nil {
		t.Fatal(err)
	}
	want := []PropFilter{{Prop: "status", Op: "=", Value: "done"}}
	if !reflect.DeepEqual(l.PropFilters(), want) {
		t.Fatalf("replacement inherited removed negation: %#v", l.PropFilters())
	}
}

func TestFilterFormDirectionRetainsSecondarySort(t *testing.T) {
	r, m, l := compactFixture(t)
	want := []SortKey{{Field: "status"}, {Field: "score", Desc: true}}
	if err := l.SetSort(want); err != nil {
		t.Fatal(err)
	}
	compactKeys(m, "z", "/")
	v := filterViewAt(r, r.Focus)
	if err := v.flipDirection(); err != nil {
		t.Fatal(err)
	}
	desc := []SortKey{{Field: "status", Desc: true}, {Field: "score", Desc: true}}
	if !reflect.DeepEqual(l.Sort(), desc) {
		t.Fatalf("direction lost secondary sort: %v", l.Sort())
	}
	if err := v.e.history(false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(l.Sort(), want) {
		t.Fatalf("undo lost secondary sort: %v", l.Sort())
	}
}

func TestSavedFilterConditionsApplyWithoutSorting(t *testing.T) {
	r, _, l := compactFixture(t)
	r.Define("task").Filters(SavedFilter{Name: "ranked", Conditions: []PropFilter{{Prop: "status", Value: "todo"}}, Sort: []SortKey{{Field: "score", Desc: true}}})
	l.noSort = true
	if err := l.ApplyFilter("ranked"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(shownIDs(l), []string{"t1", "t4"}) || len(l.Sort()) != 0 {
		t.Fatal(l.Snapshot())
	}
}

func TestSaveFilterRefusesRemovedSortField(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	r.Define("row")
	l, err := r.AddList("rows", []ListItem{{ID: "one", Noun: "row", Cells: []string{"1"}}}, Columns(Column{Title: "score"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.SetSort([]SortKey{{Field: "score"}}); err != nil {
		t.Fatal(err)
	}
	l.SetColumns(Column{Title: "other"})
	if err := l.SaveFilter("stale"); err == nil {
		t.Fatal("saved unavailable sort field")
	}
}

func TestChooserAnonymousRowsSortIndependently(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	r.Define("choice").Props(PropSpec{Name: "rank", Parameter: Parameter{Type: SettingInteger}})
	c, err := r.OpenChooser(ChooserSpec{Kind: "anonymous", Noun: "choice", Rows: []ChooserRow{{Label: "three", Data: Thing{"rank": 3}}, {Label: "one", Data: Thing{"rank": 1}}, {Label: "two", Data: Thing{"rank": 2}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetSort([]SortKey{{Field: "rank"}}); err != nil {
		t.Fatal(err)
	}
	rows := c.Matches()
	if rows[0].Label != "one" || rows[1].Label != "two" || rows[2].Label != "three" {
		t.Fatal(rows)
	}
}

func TestFilterFormKeptIntegerPrecision(t *testing.T) {
	r, m, l := compactFixture(t)
	const large = 9007199254740993
	if err := l.SetPropFilters([]PropFilter{{Prop: "score", Op: "not-in", Value: []any{large}}}); err != nil {
		t.Fatal(err)
	}
	compactKeys(m, "z", "/")
	v := filterViewAt(r, r.Focus)
	if err := v.setField("status", ChipsValue([]string{"todo"})); err != nil {
		t.Fatal(err)
	}
	if got := l.PropFilters()[0].Label(); got != "score not in [9007199254740993]" {
		t.Fatal(got)
	}
	if err := l.SaveFilter("precise"); err != nil {
		t.Fatal(err)
	}
	if err := l.ApplyFilter("precise"); err != nil {
		t.Fatal(err)
	}
	if got := l.PropFilters()[0].Label(); got != "score not in [9007199254740993]" {
		t.Fatal(got)
	}
}
