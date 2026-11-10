package ochazuke

import (
	"fmt"
	"testing"
)

// A noun declared with props over a DocStore needs no store code: its
// index over 20,000 documents reads one page, and the index's text search
// reaches the whole table, not only the page on screen.
func TestDocStoreIndexReadsOnePageAndSearchesTheWholeTable(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	table := NewMemDocTable(r, "chore")
	r.Define("chore").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
	).Store(DocStore(table, "chore"))
	for i := 1; i <= 20000; i++ {
		_ = table.Put(fmt.Sprintf("c%05d", i), Thing{"id": fmt.Sprintf("c%05d", i), "title": fmt.Sprintf("chore %05d", i)})
	}
	_ = table.Put("zz", Thing{"id": "zz", "title": "needle in the haystack"})
	m := NewModel(r)
	m.Init()
	m.Width, m.Height = 100, 30
	table.Reads, table.Queries = 0, 0
	if err := r.OpenIndex("chore", ""); err != nil {
		t.Fatal(err)
	}
	list := r.List(IndexPageID("chore") + "/list")
	if !list.Windowed() || list.WindowTotal() != 20001 || table.Reads > 201 {
		t.Fatalf("windowed %v total %d reads %d", list.Windowed(), list.WindowTotal(), table.Reads)
	}
	list.SetFilter("needle")
	r.readStaleWindows()
	if items := list.Items(); len(items) != 1 || items[0].Label != "needle in the haystack" || list.WindowTotal() != 1 {
		t.Fatalf("search the whole table: %d rows, total %d", len(list.Items()), list.WindowTotal())
	}
	mustRun(t, r, Request{Noun: "chore", Verb: "create", Args: Arguments{"title": "new one"}})
	if _, ok, _ := table.Get("c20001"); !ok {
		t.Fatal("create makes the next key")
	}
}
