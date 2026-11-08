package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// fromStore is a small mutable store of nouns "note" and "other".
type fromStore struct {
	things []Thing
	lists  int
}

func (s *fromStore) store() Store {
	return Store{
		Get: func(_ Ctx, id string) (Thing, error) {
			for _, t := range s.things {
				if t["id"] == id {
					return t, nil
				}
			}
			return nil, fmt.Errorf("no thing %q", id)
		},
		List: func(Ctx, ListQuery) ([]Thing, error) { s.lists++; return slices.Clone(s.things), nil },
		Create: func(_ Ctx, v Thing) (Thing, error) {
			t := Thing{"id": fmt.Sprintf("n%d", len(s.things)+1)}
			for k, x := range v {
				t[k] = x
			}
			s.things = append(s.things, t)
			return t, nil
		},
		Patch: func(_ Ctx, id string, ch Thing) (Thing, error) {
			for _, t := range s.things {
				if t["id"] == id {
					for k, x := range ch {
						t[k] = x
					}
					return t, nil
				}
			}
			return nil, fmt.Errorf("no thing %q", id)
		},
		Delete: func(_ Ctx, id string) error {
			s.things = slices.DeleteFunc(s.things, func(t Thing) bool { return t["id"] == id })
			return nil
		},
	}
}

func fromRuntime(t *testing.T) (*Runtime, *fromStore, *fromStore) {
	t.Helper()
	r := NewRuntime()
	notes := &fromStore{things: []Thing{{"id": "n1", "title": "First", "tags": []string{"a", "b"}}, {"id": "n2", "title": "Second"}, {"id": "n3", "title": "Third"}}}
	others := &fromStore{things: []Thing{{"id": "o1", "title": "Other"}}}
	props := []PropSpec{{Name: "id", Key: true}, {Name: "title", Role: RoleLabel, Parameter: Parameter{Type: SettingString}}, {Name: "tags", Parameter: Parameter{Type: SettingList}}, {Name: "gone", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}}}
	r.Define("note").Props(props...).Columns(Column{Title: "Title"}, Column{Title: "Tags"}).Store(notes.store())
	r.Define("other").Props(props...).Store(others.store())
	return r, notes, others
}

func fromIDs(l *Collection) []string {
	var out []string
	for _, it := range l.Snapshot()["items"].([]ListItem) {
		out = append(out, it.ID)
	}
	return out
}

func TestListFromStoreRefillsOnlyItsNoun(t *testing.T) {
	r, _, _ := fromRuntime(t)
	a, err := r.AddList("a", nil, From("note"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.AddList("b", nil, From("note"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := r.AddList("o", nil, From("other"))
	if err != nil {
		t.Fatal(err)
	}
	fed, err := r.AddList("fed", []ListItem{{ID: "x", Label: "x", Noun: "note"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := fromIDs(a); !slices.Equal(got, []string{"a/n1", "a/n2", "a/n3"}) {
		t.Fatalf("a shows %v", got)
	}
	if cells := a.Items()[0].Cells; !slices.Equal(cells, []string{"First", "a, b"}) {
		t.Fatalf("cells come from the noun's columns: %v", cells)
	}
	if err := a.Select("a/n2"); err != nil {
		t.Fatal(err)
	}
	base := []int{a.refills, b.refills, o.refills}

	if _, err := r.Execute(Request{Noun: "note", Verb: "create", Args: Arguments{"title": "Fourth"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "note", Verb: "set", Args: Arguments{"id": "n1", "prop": "title", "value": "Renamed"}}); err != nil {
		t.Fatal(err)
	}
	if got := fromIDs(b); !slices.Equal(got, []string{"b/n1", "b/n2", "b/n3", "b/n4"}) {
		t.Fatalf("b shows %v", got)
	}
	if a.Items()[0].Label != "Renamed" || a.Items()[0].Cells[0] != "Renamed" {
		t.Fatalf("set did not reach the row: %+v", a.Items()[0])
	}
	if sel, _ := a.Selected(); sel.ID != "a/n2" {
		t.Fatalf("selection moved to %q", sel.ID)
	}
	if _, err := r.Execute(Request{Noun: "note", Verb: "delete", Args: Arguments{"id": "n3"}, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if got := fromIDs(a); !slices.Equal(got, []string{"a/n1", "a/n2", "a/n4"}) {
		t.Fatalf("a after delete %v", got)
	}
	if a.refills == base[0] || b.refills == base[1] {
		t.Fatal("the lists of the changed noun did not fill again")
	}
	if o.refills != base[2] {
		t.Fatalf("a list of another noun filled again (%d)", o.refills-base[2])
	}
	if got := fed.Items(); len(got) != 1 || got[0].ID != "x" {
		t.Fatalf("a row-fed list changed: %v", got)
	}
}

func TestListFromStoreChangedAndArchive(t *testing.T) {
	r, notes, others := fromRuntime(t)
	a, _ := r.AddList("a", nil, From("note"))
	o, _ := r.AddList("o", nil, From("other"))
	notes.things = append(notes.things, Thing{"id": "n9", "title": "Outside"})
	r.Changed("other")
	if len(a.Items()) != 3 {
		t.Fatal("a change of another noun reached the list")
	}
	r.Changed("note")
	if len(a.Items()) != 4 || len(o.Items()) != 1 {
		t.Fatalf("Changed(note): a has %d rows, o %d", len(a.Items()), len(o.Items()))
	}
	_ = others
	if _, err := r.Execute(Request{Noun: "note", Verb: "archive", Args: Arguments{"id": "n2"}}); err != nil {
		t.Fatal(err)
	}
	if got := fromIDs(a); slices.Contains(got, "a/n2") || len(a.Items()) != 4 {
		t.Fatalf("an archived thing shows (or left the rows): %v, %d rows", got, len(a.Items()))
	}
	if err := r.Settings().Set("collection.hide-archived", false); err != nil {
		t.Fatal(err)
	}
	if got := fromIDs(a); !slices.Contains(got, "a/n2") {
		t.Fatalf("collection.hide-archived off still hides: %v", got)
	}
}

func TestListFromStoreRefuses(t *testing.T) {
	r, _, _ := fromRuntime(t)
	r.Define("bare")
	for _, tc := range []struct {
		noun  string
		items []ListItem
	}{{"nothing", nil}, {"bare", nil}, {"note", []ListItem{{ID: "x", Noun: "note"}}}} {
		if _, err := r.AddList("l"+tc.noun, tc.items, From(tc.noun)); err == nil {
			t.Fatalf("From(%q) with %d rows was accepted", tc.noun, len(tc.items))
		}
		if r.Instances["l"+tc.noun] != nil {
			t.Fatalf("a refused list stays registered")
		}
	}
}

func TestListFromStoreStopsWatchWithItsLists(t *testing.T) {
	r, notes, _ := fromRuntime(t)
	if _, err := r.AddList("a", nil, From("note")); err != nil {
		t.Fatal(err)
	}
	hub := r.watchHub().from["note"]
	if err := r.RemoveInstance("a"); err != nil {
		t.Fatal(err)
	}
	notes.things = append(notes.things, Thing{"id": "n9", "title": "Late"})
	r.Changed("note")
	// nothing alive: the hub ended its watch on the next change
	if !hub.watch.Stopped() || r.watchHub().from["note"] != nil {
		t.Fatal("the watch outlives its lists")
	}
}

func TestListFromPropCellHook(t *testing.T) {
	r := NewRuntime()
	s := &fromStore{things: []Thing{{"id": "n1", "title": "First", "argv": []string{"go", "test"}}, {"id": "n2", "title": "Second"}}}
	cell := func(_ Ctx, th Thing) string {
		if a, ok := th["argv"].([]string); ok {
			return strings.Join(a, " ")
		}
		return "daemon shell"
	}
	r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel, Parameter: Parameter{Type: SettingString}},
		PropSpec{Name: "argv", Parameter: Parameter{Type: SettingList}, Cell: cell}).
		Columns(Column{Title: "Title"}, Column{Title: "Argv"}).Store(s.store())
	l, err := r.AddList("a", nil, From("note"))
	if err != nil {
		t.Fatal(err)
	}
	items := l.Snapshot()["items"].([]ListItem)
	if got := items[0].Cells; got[0] != "First" || got[1] != "go test" {
		t.Fatalf("cells %q", got)
	}
	if got := items[1].Cells[1]; got != "daemon shell" {
		t.Fatalf("fallback cell %q", got)
	}
	got, err := s.store().Get(Ctx{}, "n1")
	if err != nil || fmt.Sprint(got["argv"]) != "[go test]" {
		t.Fatalf("store value changed: %v %v", got, err)
	}
	res, err := r.Execute(Request{Noun: "note", Verb: "get", Args: Arguments{"id": "n1"}})
	if err != nil || !strings.Contains(fmt.Sprint(res), "go test") || strings.Contains(fmt.Sprint(res), "daemon shell") {
		t.Fatalf("get: %v %v", res, err)
	}
}
