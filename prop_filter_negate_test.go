package ochazuke

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPropFilterNegatedOps(t *testing.T) {
	r, _ := newTaskRuntime(t)
	n := r.Noun("task")
	thing := Thing{"estimate": 2.0, "status": "todo", "title": "Write notes"}
	for _, tc := range []struct {
		f    PropFilter
		want bool
	}{
		{PropFilter{"status", "not-in", []any{"done", "doing"}}, true},
		{PropFilter{"status", "not-in", `["todo","done"]`}, false},
		{PropFilter{"title", "not-contains", "notes"}, false},
		{PropFilter{"title", "not-contains", "milk"}, true},
		{PropFilter{"status", "!=", "todo"}, false},
	} {
		f, err := typedFilter(n, tc.f)
		if err != nil {
			t.Fatal(err)
		}
		if got := propsMatch(n, thing, []PropFilter{f}); got != tc.want {
			t.Fatalf("%+v: got %v", tc.f, got)
		}
		// A condition and its negation never agree on a row with the prop.
		neg, err := typedFilter(n, tc.f.Negate())
		if err != nil {
			t.Fatal(err)
		}
		if propsMatch(n, thing, []PropFilter{neg}) == tc.want {
			t.Fatalf("%+v negated agrees", tc.f)
		}
	}
	// A row without the prop matches no condition, a negated one included.
	f, _ := typedFilter(n, PropFilter{"status", "not-in", []any{"done"}})
	if propsMatch(n, Thing{}, []PropFilter{f}) {
		t.Fatal("missing prop matched not-in")
	}
	if _, err := typedFilter(n, PropFilter{"estimate", "not-contains", "2"}); err == nil {
		t.Fatal("not-contains on a number")
	}
	for op, want := range map[string]string{"": "!=", "=": "!=", "!=": "=", "in": "not-in", "not-in": "in", "contains": "not-contains", "not-contains": "contains", "bogus": "bogus"} {
		if got := NegateFilterOp(op); got != want {
			t.Fatalf("negate %q = %q, want %q", op, got, want)
		}
	}
}

func TestPropFilterLabels(t *testing.T) {
	for f, want := range map[PropFilter]string{
		{Prop: "status", Value: "done"}:                 "status = done",
		{Prop: "status", Op: "!=", Value: "done"}:       "status != done",
		{Prop: "title", Op: "not-contains", Value: "x"}: "title not contains x",
		{Prop: "status", Op: "in", Value: "x"}:          `status in "x"`,
		{Prop: "status", Op: "not-in", Value: "[1]"}:    `status not in "[1]"`,
		{Prop: "status", Op: "weird", Value: "x"}:       "status weird x",
	} {
		if got := f.Label(); got != want {
			t.Fatalf("%+v: %q, want %q", f, got, want)
		}
	}
	got := PropFilter{Prop: "status", Op: "not-in", Value: []any{"done", "cancelled"}}.Label()
	if got != `status not in ["done","cancelled"]` {
		t.Fatal(got)
	}
}

func TestPropFilterVerbNotAndSavedNotIn(t *testing.T) {
	r, _, l := propFilterFixture(t)
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-prop", Args: Arguments{"id": l.id, "prop": "by", "op": "not-in", "value": `["agent"]`}}); err != nil {
		t.Fatal(err)
	}
	if l.Shown() != 3 {
		t.Fatalf("not-in shown %d", l.Shown())
	}
	_ = l.SetPropFilters(nil)
	if _, err := r.Execute(Request{Noun: "list", Verb: "filter-prop", Args: Arguments{"id": l.id, "prop": "by", "value": "agent", "not": true}}); err != nil {
		t.Fatal(err)
	}
	if l.Shown() != 3 || l.propFilters[0].Op != "!=" {
		t.Fatalf("not: %v shown %d", l.propFilters, l.Shown())
	}
	_ = l.SetPropFilters([]PropFilter{{Prop: "by", Op: "not-in", Value: []any{"agent"}}})
	if err := l.SaveFilter("humans"); err != nil {
		t.Fatal(err)
	}
	raw := r.Settings().String("list.filters")
	var saved []SavedFilter
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].Conditions[0].Op != "not-in" {
		t.Fatal(raw)
	}
	_ = l.SetPropFilters(nil)
	if err := l.ApplyFilter("humans"); err != nil || l.Shown() != 3 {
		t.Fatalf("apply %v shown %d", err, l.Shown())
	}
	if !reflect.DeepEqual(l.PropFilters()[0].Value, []any{"agent"}) {
		t.Fatal(l.PropFilters())
	}
}
