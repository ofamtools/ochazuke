package cli

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke"
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
	page := CatalogPage{ID: "items", Title: "Items", Layout: LayoutSplit, Lists: []CatalogList{{ID: "items/list", Noun: "item", Kind: ListKindIndex, Tree: true}}, Detail: &CatalogDetail{}}
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
	f := frameOf(m)
	if !strings.Contains(f, "Group one") || !strings.Contains(f, "  Item one") {
		t.Fatalf("frame:\n%s", f)
	}
}
