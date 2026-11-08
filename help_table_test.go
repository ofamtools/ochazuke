package ochazuke

import (
	"slices"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

func tableOpen(t *testing.T) (*Runtime, *Model, *[]string) {
	t.Helper()
	r, m, ran := unboundFixture(t)
	r.EnsureConfigSettings()
	for _, k := range []string{"?", "S", "S"} {
		keytest.Press(m, k)
	}
	if h := r.helpOpen(); h == nil || h.view != HelpTable {
		t.Fatalf("not in the table view: %+v", h)
	}
	return r, m, ran
}

func tableIDs(r *Runtime) []string {
	var out []string
	for _, item := range r.List(helpListID).items {
		out = append(out, item.Label)
	}
	return out
}

// The table lists the noun's unbound verbs as rows below the keys, in their
// own section, the key cell blank; help.table-unbound off leaves the keys.
func TestHelpTableListsUnboundVerbs(t *testing.T) {
	r, _, _ := tableOpen(t)
	var unbound, keys int
	for _, item := range r.List(helpListID).Items() {
		if row, ok := item.Data.(HelpRow); ok {
			unbound++
			if item.Section != helpTableUnboundSection || !row.Unbound || item.Cells[0] != "" {
				t.Fatalf("unbound row %+v", item)
			}
		} else if unbound > 0 {
			t.Fatalf("a key row after the unbound ones: %+v", item)
		} else {
			keys++
		}
	}
	if unbound != 2 || keys == 0 {
		t.Fatalf("unbound %d keys %d", unbound, keys)
	}
	if err := r.Settings().Set(HelpTableUnboundSetting, false); err != nil {
		t.Fatal(err)
	}
	if err := r.helpOpen().refreshBindings(); err != nil {
		t.Fatal(err)
	}
	for _, item := range r.List(helpListID).Items() {
		if _, ok := item.Data.(HelpRow); ok {
			t.Fatalf("setting off still lists %+v", item)
		}
	}
}

// A verb bound in some mode of the table is not unbound.
func TestHelpTableUnboundExcludesBoundVerbs(t *testing.T) {
	r, _, _ := tableOpen(t)
	for _, op := range r.TableUnboundVerbs(NounPlace{Noun: "widget"}, r.helpOpen().modes) {
		if op.Key.Short() == "widget.open" {
			t.Fatalf("bound verb listed: %v", op.Key)
		}
	}
}

// enter runs an unbound row as it does in the other views, and b binds it.
func TestHelpTableUnboundRowsRun(t *testing.T) {
	r, m, ran := tableOpen(t)
	list := r.List(helpListID)
	for i, item := range list.Items() {
		if row, ok := item.Data.(HelpRow); ok && row.Operation == "widget.ping" {
			if err := list.Select(item.ID); err != nil {
				t.Fatalf("select %d: %v", i, err)
			}
		}
	}
	keytest.Press(m, "enter")
	if !slices.Equal(*ran, []string{"ping"}) {
		t.Fatalf("ran %v", *ran)
	}
}

// s sorts by key, descending, then restores the default order; u keeps the
// unbound rows only (the standard prop filter) and U brings the keys back.
func TestHelpTableSortAndUnboundOnly(t *testing.T) {
	r, m, _ := tableOpen(t)
	list := r.List(helpListID)
	def := tableIDs(r)
	keytest.Press(m, "s")
	if got := list.Sort(); len(got) != 1 || got[0].Field != "key" || got[0].Desc {
		t.Fatalf("sort %v", got)
	}
	keyAt := func(i int) string { return list.items[i].Cells[0] }
	asc := keyAt(0)
	keytest.Press(m, "s")
	if got := list.Sort(); len(got) != 1 || !got[0].Desc {
		t.Fatalf("sort %v", got)
	}
	if keyAt(0) == asc {
		t.Fatalf("descending starts where ascending did: %q", asc)
	}
	keytest.Press(m, "s")
	if len(list.Sort()) != 0 || !slices.Equal(tableIDs(r), def) {
		t.Fatalf("not restored: %v", list.Sort())
	}
	keytest.Press(m, "u")
	for _, item := range list.items {
		if _, ok := item.Data.(HelpRow); !ok {
			t.Fatalf("a key row under unbound only: %+v", item)
		}
	}
	keytest.Press(m, "U")
	if len(tableIDs(r)) != len(def) {
		t.Fatalf("keys not back: %d vs %d", len(tableIDs(r)), len(def))
	}
}
