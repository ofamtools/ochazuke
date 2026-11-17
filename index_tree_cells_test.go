package ochazuke

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// countedStore is a store over things (by id) that counts its reads.
type countedStore struct {
	things []Thing
	calls  map[string]int
}

func (s *countedStore) store() Store {
	return Store{
		Get: func(_ Ctx, key string) (Thing, error) {
			s.calls["get"]++
			for _, t := range s.things {
				if t["id"] == key {
					return t, nil
				}
			}
			return nil, &NotFoundError{Noun: "thing", ID: key}
		},
		List: func(Ctx, ListQuery) ([]Thing, error) {
			s.calls["list"]++
			return s.things, nil
		},
	}
}

// cellsFixture is an index tree of items under groups, its columns the
// label, the group (a reference) and the star mark of a starred item.
func cellsFixture(t *testing.T) (*Runtime, *Tree, *countedStore, *countedStore) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	groups := &countedStore{calls: map[string]int{}, things: []Thing{{"id": "g1", "name": "Group one"}}}
	items := &countedStore{calls: map[string]int{}, things: []Thing{
		{"id": "i1", "title": "one", "group": "g1", "starred": true},
		{"id": "i2", "title": "two", "group": "g1"},
	}}
	r.Define("group").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel}).Store(groups.store())
	r.Define("item").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "group", Parameter: Parameter{Ref: []string{"group"}}},
		PropSpec{Name: "starred", Parameter: Parameter{Type: SettingBoolean}},
		PropSpec{Name: "star", Aliases: []string{"★"}, ReadOnly: true, Derivation: &Derivation{Of: "mark", Reads: []string{"starred", "★"}}},
	).Parent("group").Columns(Column{Title: "title"}, Column{Title: "★"}, Column{Title: "group"}).Store(items.store())
	if _, err := r.AddPage("p", "P", func(*Runtime, int, int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	tree, err := r.AddIndexTree("p/items", "item", Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	return r, tree, groups, items
}

func treeCells(tree *Tree) string {
	var out []string
	for _, n := range tree.Nodes() {
		out = append(out, strings.Join(n.Cells, "|"))
	}
	return strings.Join(out, " ; ")
}

// A fill reads each store once: the parents first, kept for the cells
// that show a reference's label, so the group column reads the group's
// label without another read of the group store; a starred item shows
// its mark (the mark derivation).
func TestIndexTreeFillReadsEachStoreOnceAndShowsLabels(t *testing.T) {
	r, tree, groups, items := cellsFixture(t)
	clear(groups.calls)
	clear(items.calls)
	if err := r.IndexTreeRefill("p/items"); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(groups.calls, items.calls); got != "map[list:1] map[list:1]" {
		t.Fatalf("store reads per fill: %s", got)
	}
	if got, want := treeCells(tree), "Group one|| ; one|★|Group one ; two||Group one"; got != want {
		t.Fatalf("cells\n got %s\nwant %s", got, want)
	}
}

// index.ref-labels off shows a reference's stored value.
func TestIndexRefLabelsOffShowsTheKey(t *testing.T) {
	r, tree, _, _ := cellsFixture(t)
	if err := r.Settings().Set(IndexRefLabelsSetting, false); err != nil {
		t.Fatal(err)
	}
	if err := r.IndexTreeRefill("p/items"); err != nil {
		t.Fatal(err)
	}
	if got, want := treeCells(tree), "Group one|| ; one|★|g1 ; two||g1"; got != want {
		t.Fatalf("cells\n got %s\nwant %s", got, want)
	}
}

// A fetch verb that declares the stores it writes (Operation.Writes) tells
// their watches once its job is done: a shown index tree reads again, as
// after a change verb (a daemon's listing loaded into the app's cache),
// reading each store once though both of its nouns' watches were told.
func TestFetchJobWithWritesRefillsTheIndexTree(t *testing.T) {
	r, tree, _, items := cellsFixture(t)
	r.Define("cache").Kind(NounKindPart).Verb("load", func(Ctx) Result {
		return Pending(func(context.Context) (any, error) { return nil, nil }).Then(func(Ctx, any, error) {
			items.things = append(items.things, Thing{"id": "i3", "title": "three", "group": "g1"})
		})
	}, "Load the items").Fetch().Writes("item", "group")
	mustRun(t, r, Request{Noun: "cache", Verb: "load"})
	clear(items.calls)
	settleJob(t, r)
	if got := treeCells(tree); !strings.Contains(got, "three") {
		t.Fatalf("the tree did not read the loaded item: %s", got)
	}
	if items.calls["list"] != 1 {
		t.Fatalf("item reads after the job: %v", items.calls)
	}
}
