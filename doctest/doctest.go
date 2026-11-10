// Package doctest is the contract a DocTable keeps (ochazuke.DocStore): an
// application runs Run against its own table layer (an sqlite table, a
// file) and the reference MemDocTable passes it too. Each check makes a
// fresh table with newTable for the noun "doc":
//
//	func TestMyTableKeepsTheContract(t *testing.T) {
//		doctest.Run(t, func(t *testing.T, r *ochazuke.Runtime, noun string) ochazuke.DocTable {
//			return myTable(t.TempDir(), noun)
//		})
//	}
//
// The noun's props are Props: id (key), title (label), status (todo, doing,
// done), size (integer), archived (archive role).
package doctest

import (
	"fmt"
	"testing"

	"ofam.dev/ochazuke"
)

// Noun is the noun the contract's documents belong to.
const Noun = "doc"

// Props are the contract noun's props.
var Props = []ochazuke.PropSpec{
	{Name: "id", Key: true, CreateOnly: true},
	{Name: "title", Role: ochazuke.RoleLabel},
	{Name: "status", Parameter: ochazuke.Parameter{Enum: []string{"todo", "doing", "done"}}},
	{Name: "size", Parameter: ochazuke.Parameter{Type: ochazuke.SettingInteger}},
	{Name: "archived", Role: ochazuke.RoleArchive, Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean}},
}

// NewTable makes an empty table for noun's documents on r.
type NewTable func(t *testing.T, r *ochazuke.Runtime, noun string) ochazuke.DocTable

// fixture is a runtime with the contract noun over a DocStore on a fresh
// table, and the table.
func fixture(t *testing.T, newTable NewTable) (*ochazuke.Runtime, ochazuke.DocTable) {
	t.Helper()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	table := newTable(t, r, Noun)
	r.Define(Noun).Props(Props...).Store(ochazuke.DocStore(table, Noun))
	return r, table
}

func run(t *testing.T, r *ochazuke.Runtime, verb string, args ochazuke.Arguments) any {
	t.Helper()
	v, err := r.Execute(ochazuke.Request{Noun: Noun, Verb: verb, Args: args})
	if err != nil {
		t.Fatalf("%s %s %v: %v", Noun, verb, args, err)
	}
	return v
}

// seed creates n documents d1..dn: title "doc N", status by N mod 3, size
// N, every fifth archived.
func seed(t *testing.T, r *ochazuke.Runtime, n int) {
	t.Helper()
	statuses := []string{"todo", "doing", "done"}
	for i := 1; i <= n; i++ {
		run(t, r, "create", ochazuke.Arguments{"id": fmt.Sprintf("d%d", i), "title": fmt.Sprintf("doc %d", i), "status": statuses[i%3], "size": i, "archived": i%5 == 0})
	}
}

// Run runs every check of the contract against tables newTable makes.
func Run(t *testing.T, newTable NewTable) {
	t.Run("crud", func(t *testing.T) { checkCRUD(t, newTable) })
	t.Run("version", func(t *testing.T) { checkVersion(t, newTable) })
	t.Run("query", func(t *testing.T) { checkQuery(t, newTable) })
	t.Run("archive", func(t *testing.T) { checkArchive(t, newTable) })
}

func checkCRUD(t *testing.T, newTable NewTable) {
	r, table := fixture(t, newTable)
	made, _ := ochazuke.ThingOf(run(t, r, "create", ochazuke.Arguments{"title": "first"}))
	if made["id"] == nil || made["id"] == "" {
		t.Fatalf("create made no key: %v", made)
	}
	run(t, r, "create", ochazuke.Arguments{"id": "x9", "title": "given"})
	if _, err := r.Execute(ochazuke.Request{Noun: Noun, Verb: "create", Args: ochazuke.Arguments{"id": "x9", "title": "again"}}); err == nil {
		t.Fatal("a second document under one key must be refused")
	}
	run(t, r, "set", ochazuke.Arguments{"id": "x9", "props": map[string]any{"status": "doing"}})
	got, ok, err := table.Get("x9")
	if err != nil || !ok || got["status"] != "doing" || got["title"] != "given" {
		t.Fatalf("after set: %v %v %v", got, ok, err)
	}
	if _, err := r.Execute(ochazuke.Request{Noun: Noun, Verb: "delete", Args: ochazuke.Arguments{"id": "x9"}, Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := table.Get("x9"); ok {
		t.Fatal("delete left the document")
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.history-entry", Verb: "undo"}); err != nil {
		t.Fatalf("undo the delete: %v", err)
	}
	if _, ok, _ := table.Get("x9"); !ok {
		t.Fatal("undoing the delete puts the document back under its key")
	}
}

func checkVersion(t *testing.T, newTable NewTable) {
	r, table := fixture(t, newTable)
	v0, err := table.Version()
	if err != nil {
		t.Fatal(err)
	}
	seed(t, r, 3)
	v1, _ := table.Version()
	if v1 == v0 {
		t.Fatal("a write must move the version")
	}
	if _, err := table.Query(ochazuke.DocQuery{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := table.Get("d1"); err != nil {
		t.Fatal(err)
	}
	if v2, _ := table.Version(); v2 != v1 {
		t.Fatalf("a read moved the version: %s -> %s", v1, v2)
	}
}

func ids(page ochazuke.ListPage) []string {
	out := make([]string, len(page.Items))
	for i, t := range page.Items {
		out[i] = fmt.Sprint(t["id"])
	}
	return out
}

func checkQuery(t *testing.T, newTable NewTable) {
	r, table := fixture(t, newTable)
	seed(t, r, 12)
	q := func(dq ochazuke.DocQuery) ochazuke.ListPage {
		t.Helper()
		page, err := table.Query(dq)
		if err != nil {
			t.Fatalf("query %+v: %v", dq, err)
		}
		return page
	}
	if page := q(ochazuke.DocQuery{}); page.Total != 12 || len(page.Items) != 12 {
		t.Fatalf("everything: total %d items %d", page.Total, len(page.Items))
	}
	if page := q(ochazuke.DocQuery{Filters: []ochazuke.PropFilter{{Prop: "status", Value: "done"}}}); page.Total != 4 {
		t.Fatalf("status = done: %v", ids(page))
	}
	if page := q(ochazuke.DocQuery{Filters: []ochazuke.PropFilter{{Prop: "status", Op: "!=", Value: "done"}}}); page.Total != 8 {
		t.Fatalf("status != done: %v", ids(page))
	}
	if page := q(ochazuke.DocQuery{Filters: []ochazuke.PropFilter{{Prop: "status", Op: "in", Value: []any{"todo", "doing"}}}}); page.Total != 8 {
		t.Fatalf("status in: %v", ids(page))
	}
	page := q(ochazuke.DocQuery{Sort: []ochazuke.SortKey{{Field: "size", Desc: true}}, Page: 2, PerPage: 5})
	if page.Total != 12 || fmt.Sprint(ids(page)) != "[d7 d6 d5 d4 d3]" {
		t.Fatalf("sorted page 2: total %d %v", page.Total, ids(page))
	}
	if page := q(ochazuke.DocQuery{Text: "DOC 1", TextFields: []string{"id", "title"}}); fmt.Sprint(ids(page)) != "[d1 d10 d11 d12]" {
		t.Fatalf("text: %v", ids(page))
	}
	// Grouping happens before the row page is cut; a group may continue
	// across that cut. Use the public verb so this also checks DocStore's
	// translation of ListQuery into the table's DocQuery.
	grouped := run(t, r, "list", ochazuke.Arguments{"archived": true, "group-by": "status", "group-sort": "status:desc", "sort": "size", "page": 2, "per-page": 3}).(ochazuke.ListAnswer)
	if grouped.Total != 12 || len(grouped.Items) != 3 {
		t.Fatalf("grouped page: %#v", grouped)
	}
	for i, want := range []struct{ id, group string }{{"d11", "done"}, {"d1", "doing"}, {"d4", "doing"}} {
		if row := grouped.Items[i]; row["id"] != want.id || row["group"] != want.group {
			t.Fatalf("grouped row %d: %#v, want %s in %s", i, row, want.id, want.group)
		}
	}
	// The standard list runs the same query through the store.
	ans, err := r.Execute(ochazuke.Request{Noun: Noun, Verb: "list", Args: ochazuke.Arguments{"search": "doc 1", "per-page": 2, "page": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := ans.(ochazuke.ListAnswer); !ok || a.Total != 3 || len(a.Items) != 2 {
		t.Fatalf("list search (d10 archived): %#v", ans)
	}
}

func checkArchive(t *testing.T, newTable NewTable) {
	r, _ := fixture(t, newTable)
	seed(t, r, 10)
	ans, err := r.Execute(ochazuke.Request{Noun: Noun, Verb: "list"})
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := ans.(ochazuke.ListAnswer); !ok || a.Total != 8 {
		t.Fatalf("archived documents hidden: %#v", ans)
	}
	ans, _ = r.Execute(ochazuke.Request{Noun: Noun, Verb: "list", Args: ochazuke.Arguments{"archived": true}})
	if a, ok := ans.(ochazuke.ListAnswer); !ok || a.Total != 10 {
		t.Fatalf("archived documents shown: %#v", ans)
	}
	run(t, r, "archive", ochazuke.Arguments{"id": "d1"})
	ans, _ = r.Execute(ochazuke.Request{Noun: Noun, Verb: "list"})
	if a, ok := ans.(ochazuke.ListAnswer); !ok || a.Total != 7 {
		t.Fatalf("after archive: %#v", ans)
	}
}
