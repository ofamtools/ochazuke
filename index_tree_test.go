package ochazuke

import (
	"fmt"
	"strings"
	"testing"
)

// An index tree nests a noun's things under their declared parent: the
// parent noun's things are parent rows of their own noun, a thing whose
// parent is empty or not listed sits at the top level, and a change of
// either noun fills the tree again.
func TestIndexTreeNestsByTheDeclaredParent(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	groups := NewMemDocTable(r, "group")
	items := NewMemDocTable(r, "item")
	r.Define("group").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "name", Role: RoleLabel},
	).Store(DocStore(groups, "group"))
	r.Define("item").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "group", Parameter: Parameter{Ref: []string{"group"}}},
	).Parent("group").Store(DocStore(items, "item"))
	for _, g := range []string{"g1", "g2"} {
		_ = groups.Put(g, Thing{"id": g, "name": "G" + g[1:]})
	}
	for _, it := range []Thing{
		{"id": "i1", "title": "one", "group": "g1"},
		{"id": "i2", "title": "two", "group": "g2"},
		{"id": "i3", "title": "three"},
		{"id": "i4", "title": "four", "group": "gone"},
	} {
		_ = items.Put(fmt.Sprint(it["id"]), it)
	}
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
	shape := func() string {
		var out []string
		for _, n := range tree.Nodes() {
			out = append(out, fmt.Sprintf("%s<%s>%s", strings.TrimPrefix(n.ID, "p/items/"), strings.TrimPrefix(n.Parent, "p/items/"), n.Noun))
		}
		return strings.Join(out, " ")
	}
	want := "group:g1<>group i1<group:g1>item group:g2<>group i2<group:g2>item i3<>item i4<>item"
	if got := shape(); got != want {
		t.Fatalf("nodes\n got %s\nwant %s", got, want)
	}
	mustRun(t, r, Request{Noun: "item", Verb: "set", Args: Arguments{"id": "i3", "props": map[string]any{"group": "g2"}}})
	want = "group:g1<>group i1<group:g1>item group:g2<>group i2<group:g2>item i3<group:g2>item i4<>item"
	if got := shape(); got != want {
		t.Fatalf("after a change\n got %s\nwant %s", got, want)
	}
}
