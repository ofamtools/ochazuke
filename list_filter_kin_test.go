package ochazuke

import (
	"reflect"
	"testing"
)

func kinFixture(t *testing.T) (*Runtime, *Collection) {
	t.Helper()
	r := NewRuntime()
	r.Define("a")
	r.Instance("p")
	items := []ListItem{
		{ID: "p1", Label: "task props", Noun: "a"},
		{ID: "c1", Label: "status", Noun: "a", Under: "p1"},
		{ID: "c2", Label: "archived", Noun: "a", Under: "p1"},
		{ID: "p2", Label: "other", Noun: "a"},
		{ID: "d1", Label: "stray", Noun: "a", Under: "p2"},
	}
	list, err := r.AddList("l", items, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	return r, list
}

func TestListFilterKeepsChildren(t *testing.T) {
	r, l := kinFixture(t)
	for _, c := range []struct {
		query string
		want  []string
	}{
		{"task", []string{"p1", "c1", "c2"}},
		{"stray", []string{"p2", "d1"}},
		{"zzz", nil},
	} {
		l.SetFilter(c.query)
		if got := shownIDs(l); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%q: shown %v, want %v", c.query, got, c.want)
		}
		if l.Shown() != len(c.want) {
			t.Fatalf("%q: Shown %d", c.query, l.Shown())
		}
	}
	if err := r.Settings().Set(ListFilterKeepsChildrenSetting, false); err != nil {
		t.Fatal(err)
	}
	l.SetFilter("task")
	if got := shownIDs(l); !reflect.DeepEqual(got, []string{"p1"}) {
		t.Fatalf("off: shown %v", got)
	}
	l.SetFilter("stray")
	if got := shownIDs(l); !reflect.DeepEqual(got, []string{"d1"}) {
		t.Fatalf("off, child only: shown %v", got)
	}
}
