package ochazuke

import (
	"testing"

	"ofam.dev/ochazuke/keytest"
)

// rowThingsFixture is an editable tree whose rows are parcels (a noun
// with a store): the rows' ids are the tree's, their keys in their Props.
// The tree reads its rows' keys last and records writes to their things.
func rowThingsFixture(t *testing.T) (*Runtime, *Model, *Tree, *derivedStore, *[]string) {
	t.Helper()
	r, m, tree, _ := treeFixture(t)
	s := &derivedStore{key: "id", things: []Thing{{"id": "t1", "title": "one", "status": "todo"}}}
	st := s.store()
	st.Undo = true
	var marked []string
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title"}, PropSpec{Name: "status"}).Store(st).
		Verb("mark", func(c Ctx) Result {
			marked = append(marked, c.Runtime.ThingKey(c.Instance))
			return Done(nil)
		}, "Mark the parcel").
		Mode("normal").
		Bind("alt+w", Do("parcel.mark")).
		Bind("d d", Do("parcel.delete"))
	// The outline's own key for its parcel rows, guarded per row.
	r.DefineGuard("parcel-row", func(r *Runtime, ev KeyEvent) bool {
		inst := r.Instances[ev.Focus]
		return inst != nil && inst.Noun == "parcel"
	})
	r.Define("ochazuke.outline").Mode("normal").
		Bind("alt+x", Do("parcel.set", Arguments{"props": map[string]any{"status": "done"}}).Guard("parcel-row"))
	nodes := func() []TreeNode {
		return []TreeNode{{ID: "row:t1", Label: "one", Noun: "parcel", Props: Thing(s.find("t1"))}}
	}
	tree.RowKeysLast, tree.RecordWrites = true, true
	tree.Reload = func() error { return tree.SetNodes(nodes()) }
	if err := tree.SetNodes(nodes()); err != nil {
		t.Fatal(err)
	}
	if err := tree.Focus(); err != nil {
		t.Fatal(err)
	}
	return r, m, tree, s, &marked
}

// On a tree that reads its rows' keys last, the tree's own row wins a key
// both bind (d d deletes the outline row, not the parcel), a key only the
// row's noun binds runs its verb on the row's thing (by its key, not the
// row's id), and the outline's noun-guarded row acts on the focused
// thing: a person's write there is one step of the outline's undo.
func TestTreeRowKeysLastAndRecordedWrites(t *testing.T) {
	_, m, tree, s, marked := rowThingsFixture(t)
	keytest.Press(m, "alt+w")
	if len(*marked) != 1 || (*marked)[0] != "t1" {
		t.Fatalf("alt+w marked %v, want [t1]", *marked)
	}
	keytest.Press(m, "alt+x")
	if s.find("t1")["status"] != "done" {
		t.Fatalf("alt+x: %v", s.find("t1"))
	}
	keytest.Press(m, "u")
	if s.find("t1")["status"] != "todo" {
		t.Fatalf("u: %v", s.find("t1"))
	}
	keytest.Press(m, "ctrl+r")
	if s.find("t1")["status"] != "done" {
		t.Fatalf("ctrl+r: %v", s.find("t1"))
	}
	keytest.Press(m, "d")
	keytest.Press(m, "d")
	if s.find("t1") == nil {
		t.Fatal("d d deleted the parcel, not the outline row")
	}
	if len(tree.Nodes()) != 0 {
		t.Fatalf("d d kept the row: %v", tree.Nodes())
	}
}

// tree.row-keys off: the row's noun answers no key on such a tree.
func TestTreeRowKeysOff(t *testing.T) {
	r, m, _, _, marked := rowThingsFixture(t)
	if err := r.Settings().Set(TreeRowKeysSetting, false); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "alt+w")
	if len(*marked) != 0 {
		t.Fatalf("alt+w with tree.row-keys off marked %v", *marked)
	}
}
