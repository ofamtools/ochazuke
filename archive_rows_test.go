package ochazuke

import (
	"slices"
	"testing"
)

func archiveRowsRuntime() *Runtime {
	r := NewRuntime()
	r.Config().Disable()
	r.Define("lane").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "state"}, PropSpec{Name: "archived", Aliases: []string{"gone"}, Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}})
	r.Define("plain").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "archived"})
	r.Instance("p")
	return r
}

func lane(id string, archived bool, parent string) ListItem {
	return ListItem{ID: id, Label: id, Noun: "lane", Props: Thing{"state": "x", "archived": archived}}
}

func TestArchiveRowsListHideRevealAndClear(t *testing.T) {
	t.Parallel()
	r := archiveRowsRuntime()
	l, err := r.AddList("p/l", []ListItem{lane("a", false, ""), lane("b", true, ""), lane("c", false, "")}, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !slices.Equal(got, []string{"a", "c"}) || l.Len() != 2 || l.Shown() != 2 {
		t.Fatalf("default %v len %d", got, l.Len())
	}
	if err := l.ApplyFilter("archived"); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !slices.Equal(got, []string{"b"}) || l.Len() != 3 {
		t.Fatalf("saved filter %v len %d", got, l.Len())
	}
	// an alias of the archive prop names it too
	if err := l.SetPropFilters([]PropFilter{{Prop: "gone", Value: true}}); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("alias filter %v", got)
	}
	// a filter on another prop keeps hiding
	if err := l.SetPropFilters([]PropFilter{{Prop: "state", Value: "x"}}); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("other prop filter %v", got)
	}
	if err := l.SetPropFilters(nil); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("cleared %v", got)
	}
}

func TestArchiveRowsNounWithoutRoleAndSettingOff(t *testing.T) {
	t.Parallel()
	r := archiveRowsRuntime()
	rows := []ListItem{{ID: "x", Label: "x", Noun: "plain", Props: Thing{"archived": true}}, lane("a", true, "")}
	l, err := r.AddList("p/l", rows, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !slices.Equal(got, []string{"x"}) {
		t.Fatalf("a prop without the role hides nothing: %v", got)
	}
	if err := r.Settings().Set(HideArchivedSetting, false); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !slices.Equal(got, []string{"x", "a"}) || l.Len() != 2 {
		t.Fatalf("off: %v", got)
	}
}

func TestArchiveRowsCursorMarksAndCounts(t *testing.T) {
	t.Parallel()
	r := archiveRowsRuntime()
	l, err := r.AddList("p/l", []ListItem{lane("a", false, ""), lane("b", false, ""), lane("c", false, "")}, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Select("b"); err != nil {
		t.Fatal(err)
	}
	if err := l.ToggleMark("b"); err != nil {
		t.Fatal(err)
	}
	// b is archived under the cursor and its mark.
	if err := l.Patch(ListPatch{Upsert: []ListItem{lane("b", true, "")}}); err != nil {
		t.Fatal(err)
	}
	sel, ok := l.Selected()
	if !ok || (sel.ID != "a" && sel.ID != "c") || l.selected < 0 || l.selected >= len(l.items) {
		t.Fatalf("cursor must land on a shown row: %+v %v", sel, ok)
	}
	if got := l.markedShown(); len(got) != 0 {
		t.Fatalf("a mark on a hidden row must not act: %v", got)
	}
	if l.Len() != 2 || l.Shown() != 2 {
		t.Fatalf("counts len %d shown %d", l.Len(), l.Shown())
	}
	// visual selection ranges over shown rows only
	l.Move(-100)
	if err := l.StartVisual(); err != nil {
		t.Fatal(err)
	}
	l.Move(100)
	if m := l.markedShown(); len(m) != 2 {
		t.Fatalf("visual covers shown rows: %v", m)
	}
	l.EndVisual()
	// every row archived: empty state, valid cursor, no error
	if err := l.Patch(ListPatch{Upsert: []ListItem{lane("a", true, ""), lane("c", true, "")}}); err != nil {
		t.Fatal(err)
	}
	if l.Len() != 0 || l.Shown() != 0 {
		t.Fatalf("len %d shown %d", l.Len(), l.Shown())
	}
	if _, ok := l.Selected(); ok {
		t.Fatal("nothing to select")
	}
	if want := l.placeholder.Text(r); l.emptyLine() != want {
		t.Fatalf("empty state %q, want the list's Empty %q", l.emptyLine(), want)
	}
}

func treeNode(id, parent string, archived bool) TreeNode {
	return TreeNode{ID: id, Parent: parent, Label: id, Noun: "lane", Props: Thing{"state": "x", "archived": archived}}
}

func TestArchiveRowsTreeParentRule(t *testing.T) {
	t.Parallel()
	r := archiveRowsRuntime()
	tr, err := r.AddTree("p/t", []TreeNode{
		treeNode("a", "", false), treeNode("a1", "a", false),
		treeNode("b", "", true), treeNode("b1", "b", false), treeNode("b11", "b1", false),
		treeNode("c", "", false), treeNode("c1", "c", true),
	}, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	l := tr.List()
	if got := shownIDs(l); !slices.Equal(got, []string{"a", "a1", "c"}) {
		t.Fatalf("an archived node hides its whole subtree: %v", got)
	}
	if err := l.ApplyFilter("archived"); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !slices.Equal(got, []string{"b", "c", "c1"}) {
		t.Fatalf("archived filter keeps ancestors as context: %v", got)
	}
	if err := l.SetPropFilters(nil); err != nil {
		t.Fatal(err)
	}
	if got := shownIDs(l); !slices.Equal(got, []string{"a", "a1", "c"}) {
		t.Fatalf("cleared: %v", got)
	}
	// a text query on a tree stays flat (no ancestors) and still hides
	l.SetFilter("b1")
	if l.Shown() != 0 {
		t.Fatalf("text filter must not reveal: %v", shownIDs(l))
	}
}
