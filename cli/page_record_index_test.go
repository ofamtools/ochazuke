package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func TestMountedIndexOpenKeepsRowsAndSelectedDetail(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.DefineOwn("document").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "name", Role: ochazuke.RoleLabel}, ochazuke.PropSpec{Name: "content", Multiline: true}).Store(ochazuke.Store{
		Get: func(_ ochazuke.Ctx, key string) (ochazuke.Thing, error) {
			return ochazuke.Thing{"id": key, "name": key}, nil
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			return []ochazuke.Thing{{"id": "one", "name": "One"}, {"id": "two", "name": "Two"}}, nil
		},
	})
	m := ochazuke.NewModel(r)
	m.Width, m.Height = 100, 30
	page := CatalogPage{ID: "documents", Title: "Documents", Layout: LayoutSplit, Lists: []CatalogList{{ID: "documents/list", Noun: "document", Kind: ListKindIndex}}, Detail: &CatalogDetail{Fields: []string{"content"}}}
	v, err := MountPage(m, CatalogDoc{}, page, PageConfig{FetchRecord: func(_ context.Context, noun, key string) ([]byte, error) {
		return []byte(fmt.Sprintf(`{"id":%q,"name":%q,"content":%q}`, key, key, "Actual record "+key)), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	list := v.List("documents/list")
	if err := list.SetItems([]ochazuke.ListItem{{ID: "documents/list/one", Noun: "document", Label: "One", Data: "one", Props: ochazuke.Thing{"id": "one", "name": "One"}}, {ID: "documents/list/two", Noun: "document", Label: "Two", Data: "two", Props: ochazuke.Thing{"id": "two", "name": "Two"}}}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"one", "two", "one"} {
		if err := v.OpenRow("document", key); err != nil {
			t.Fatal(err)
		}
		if err := Settle(r, nil); err != nil {
			t.Fatal(err)
		}
		frameOf(m)
		if err := Settle(r, nil); err != nil {
			t.Fatal(err)
		}
		if f := frameOf(m); !strings.Contains(f, "Actual record "+key) {
			t.Fatalf("index detail for %s: %s", key, f)
		}
		if list.Len() != 2 {
			t.Fatalf("repeated native open duplicated or dropped rows: %d", list.Len())
		}
	}
	if list.From() != "document" {
		t.Fatalf("native open replaced index source: %q", list.From())
	}
}
