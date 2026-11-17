package cli

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/keytest"
)

// An index list declared "tree": true draws its noun under the declared
// parent, and its parent rows are things of the parent noun: the detail
// (and every key) follows the row's own noun and key.
func TestMountedIndexTreeRowsAreTheirOwnNouns(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	store := func(things ...ochazuke.Thing) ochazuke.Store {
		return ochazuke.Store{
			Get: func(_ ochazuke.Ctx, key string) (ochazuke.Thing, error) {
				for _, th := range things {
					if th["id"] == key {
						return th, nil
					}
				}
				return nil, &ochazuke.NotFoundError{Noun: "thing", ID: key}
			},
			List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return things, nil },
		}
	}
	r.DefineOwn("group").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "name", Role: ochazuke.RoleLabel}).
		Store(store(ochazuke.Thing{"id": "g1", "name": "Group one"}))
	r.DefineOwn("item").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "group", Parameter: ochazuke.Parameter{Ref: []string{"group"}}}).Parent("group").
		Store(store(ochazuke.Thing{"id": "i1", "title": "Item one", "group": "g1"}))
	m := ochazuke.NewModel(r)
	m.Width, m.Height = 100, 30
	page := CatalogPage{ID: "items", Title: "Items", Layout: LayoutSplit, Lists: []CatalogList{{ID: "items/list", Noun: "item", Kind: ListKindIndex, Tree: true,
		Columns: []ochazuke.Column{{Title: "title"}, {Title: "group"}}}}, Detail: &CatalogDetail{}}
	v, err := MountPage(m, CatalogDoc{}, page, PageConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("items"); err != nil {
		t.Fatal(err)
	}
	row, ok := v.Row("items/list/group:g1")
	if !ok || row.Noun != "group" || row.ID != "g1" {
		t.Fatalf("parent row: %+v %v", row, ok)
	}
	if err := v.OpenRow("item", "i1"); err != nil {
		t.Fatal(err)
	}
	row, ok = v.Row("items/list/i1")
	if !ok || row.Noun != "item" || row.ID != "i1" {
		t.Fatalf("child row: %+v %v", row, ok)
	}
	// The page's columns, not the noun's: the group column shows the
	// group's label.
	if n, ok := r.Tree("items/list").Selected(); !ok || strings.Join(n.Cells, "|") != "Item one|Group one" {
		t.Fatalf("cells: %+v", n.Cells)
	}
	f := frameOf(m)
	if !strings.Contains(f, "Group one") || !strings.Contains(f, "  Item one") {
		t.Fatalf("frame:\n%s", f)
	}
}

// Leaving the / filter with esc on a mounted index tree selects the row
// the filter line opened on, not the first row, whatever the match the
// typing moved to (the Panes page's behaviour; run plan 5.6).
func TestMountedIndexTreeFilterEscReturnsToItsRow(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	store := func(things ...ochazuke.Thing) ochazuke.Store {
		return ochazuke.Store{
			Get: func(_ ochazuke.Ctx, key string) (ochazuke.Thing, error) {
				for _, th := range things {
					if th["id"] == key {
						return th, nil
					}
				}
				return nil, &ochazuke.NotFoundError{Noun: "thing", ID: key}
			},
			List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return things, nil },
		}
	}
	r.DefineOwn("group").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "name", Role: ochazuke.RoleLabel}).
		Store(store(ochazuke.Thing{"id": "g1", "name": "Group one"}, ochazuke.Thing{"id": "g2", "name": "Group two"}))
	r.DefineOwn("item").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "group", Parameter: ochazuke.Parameter{Ref: []string{"group"}}}).Parent("group").
		Store(store(ochazuke.Thing{"id": "i1", "title": "Item one", "group": "g1"}, ochazuke.Thing{"id": "i2", "title": "Item two", "group": "g1"}, ochazuke.Thing{"id": "i3", "title": "Item three", "group": "g2"}))
	m := ochazuke.NewModel(r)
	m.Width, m.Height = 100, 30
	page := CatalogPage{ID: "items", Title: "Items", Layout: LayoutSplit, Lists: []CatalogList{{ID: "items/list", Noun: "item", Kind: ListKindIndex, Tree: true}}, Detail: &CatalogDetail{Kind: "record-form"}}
	if _, err := MountPage(m, CatalogDoc{}, page, PageConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("items"); err != nil {
		t.Fatal(err)
	}
	m.View()
	sel := func() string {
		n, _ := r.Tree("items/list").Selected()
		return n.ID
	}
	for _, k := range []string{"j", "j", "j"} {
		keytest.Press(m, k)
		m.View()
	}
	if got := sel(); got != "items/list/group:g2" {
		t.Fatalf("before /: %s", got)
	}
	for _, k := range []string{"/", "o", "n", "e"} {
		keytest.Press(m, k)
		m.View()
	}
	if got := sel(); got == "items/list/group:g2" {
		t.Fatalf("the filter kept the row it hides: %s", got)
	}
	keytest.Press(m, "esc")
	m.View()
	if got := sel(); got != "items/list/group:g2" || r.Mode.Name != "normal" {
		t.Fatalf("after esc: %s in %s", got, r.Mode.Name)
	}
}
