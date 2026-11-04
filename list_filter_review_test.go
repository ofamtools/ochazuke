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
