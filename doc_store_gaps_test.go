package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// A store's Order (Store.Order) is the order of a list that sorts on
// nothing and the tie-break after a request's keys, run in the table's
// paged query (only the page is read).
func TestStoreOrderSortsInThePagedQuery(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	table := NewMemDocTable(r, "chore")
	s := DocStore(table, "chore")
	s.Order = []SortKey{{Field: "rank"}}
	r.Define("chore").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "rank", Parameter: Parameter{Type: SettingInteger}},
	).Store(s)
	for i, rank := range []int{3, 1, 4, 2, 5, 0} {
		_ = table.Put(fmt.Sprintf("c%d", i+1), Thing{"id": fmt.Sprintf("c%d", i+1), "title": []string{"b", "a"}[i%2], "rank": rank})
	}
	ids := func(a ListAnswer) string {
		var out []string
		for _, row := range a.Items {
			out = append(out, fmt.Sprint(row["id"]))
		}
		return strings.Join(out, " ")
	}
	table.Reads = 0
	if a := listIDs(t, r, "chore", Arguments{"per-page": 3}); ids(a) != "c6 c2 c4" || table.Reads != 3 {
		t.Fatalf("no sort is the store's order, paged in the table: %q (%d reads)", ids(a), table.Reads)
	}
	if a := listIDs(t, r, "chore", Arguments{"sort": "title"}); ids(a) != "c6 c2 c4 c1 c3 c5" {
		t.Fatalf("ties of the request's key in the store's order: %q", ids(a))
	}
}

// A ReadDefault prop reads its Default, or its type's zero, where the
// stored document lacks the field; the document is not written.
func TestDocStoreReadsDefaultsOfMissingFields(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	table := NewMemDocTable(r, "memo")
	r.Define("memo").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel, ReadDefault: true},
		PropSpec{Name: "archived", Role: RoleArchive, ReadDefault: true, Parameter: Parameter{Type: SettingBoolean}},
		PropSpec{Name: "maker", Field: "made_by", ReadOnly: true, ReadDefault: true, Default: map[string]any{"kind": "unknown"}, Parameter: Parameter{Type: PropObject}},
		PropSpec{Name: "text"},
	).Store(DocStore(table, "memo"))
	_ = table.Put("m1", Thing{"id": "m1", "text": "hi"})
	got, err := r.Nouns["memo"].Store.Get(Ctx{Runtime: r}, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if got["title"] != "" || got["archived"] != false || fmt.Sprint(got["maker"]) != "map[kind:unknown]" || got["text"] != "hi" {
		t.Fatalf("missing fields read as defaults: %v", got)
	}
	if doc, _, _ := table.Get("m1"); len(doc) != 2 {
		t.Fatalf("nothing written back: %v", doc)
	}
}

// Derivations shape their values as data (first-line's FirstOnly,
// Untrimmed, Max, Tail; Else; first's Blank, a text entry, an object
// prop's field), and an application registers a kind of its own; each is
// filterable, sortable and follows the nouns it reads.
func TestDerivationOptionsAndRegisteredKinds(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	r.RegisterDerivation("shout", DerivationKind{About: "The text in capitals", Derive: func(_ Ctx, d Derivation, t Thing) (any, bool) {
		text, _ := t[d.Reads[0]].(string)
		return strings.ToUpper(text), text != ""
	}})
	r.Define("deck").Props(PropSpec{Name: "id", Key: true, CreateOnly: true}).Store(DocStore(NewMemDocTable(r, "deck"), "deck"))
	table := NewMemDocTable(r, "memo")
	r.Define("memo").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "text"},
		PropSpec{Name: "task"},
		PropSpec{Name: "deck", Parameter: Parameter{Noun: "deck"}},
		PropSpec{Name: "maker", Parameter: Parameter{Type: PropObject}},
		PropSpec{Name: "summary", Derivation: &Derivation{Of: "first-line", Reads: []string{"text"}, FirstOnly: true, Untrimmed: true, Max: 8, Tail: "…", Else: "untitled"}},
		PropSpec{Name: "shown", Derivation: &Derivation{Of: "first", Reads: []string{"title", "summary"}, Blank: true}},
		PropSpec{Name: "about", Derivation: &Derivation{Of: "first", Reads: []string{"about", "task:{task}"}}},
		PropSpec{Name: "made-by", Derivation: &Derivation{Of: "first", Reads: []string{"maker.kind"}, Else: "unknown"}},
		PropSpec{Name: "loud", Derivation: &Derivation{Of: "shout", Reads: []string{"deck"}}},
	).Store(DocStore(table, "memo"))
	_ = table.Put("m1", Thing{"id": "m1", "title": "  ", "text": "  a long first line\nsecond", "task": "t7", "maker": map[string]any{"kind": "agent"}, "deck": "d1"})
	_ = table.Put("m2", Thing{"id": "m2", "title": " Plan ", "text": "\nsecond", "about": "pane:W1"})
	get := func(key string) Thing {
		got, err := r.Nouns["memo"].Store.Get(Ctx{Runtime: r}, key)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	m1, m2 := get("m1"), get("m2")
	if m1["summary"] != "  a lon…" || m1["shown"] != "a lon…" || m1["about"] != "task:t7" || m1["made-by"] != "agent" || m1["loud"] != "D1" {
		t.Fatalf("m1: %v", m1)
	}
	if m2["summary"] != "untitled" || m2["shown"] != "Plan" || m2["about"] != "pane:W1" || m2["made-by"] != "unknown" || m2["loud"] != nil {
		t.Fatalf("m2: %v", m2)
	}
	if a := listIDs(t, r, "memo", Arguments{"where": "summary=untitled"}); len(a.Items) != 1 || a.Items[0]["id"] != "m2" {
		t.Fatalf("filter on a shaped derivation: %v", a.Items)
	}
	if a := listIDs(t, r, "memo", Arguments{"sort": "loud:desc"}); len(a.Items) != 2 || a.Items[0]["id"] != "m1" {
		t.Fatalf("sort on a registered kind: %v", a.Items)
	}
	if !slices.Contains(r.withDependents([]string{"deck"}), "memo") {
		t.Fatal("a registered kind reading a reference follows its noun")
	}
}

// A delete records the whole stored document, fields no prop declares
// included, and its undo puts it back whole.
func TestDocStoreUndoOfDeleteRestoresTheWholeDocument(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	table := NewMemDocTable(r, "memo")
	r.Define("memo").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "text", Role: RoleLabel},
	).Store(DocStore(table, "memo"))
	_ = table.Put("m1", Thing{"id": "m1", "text": "hi", "former_owner": "p9", "stamp": 42})
	mustRun(t, r, Request{Noun: "memo", Verb: "delete", Target: "m1", Confirmed: true})
	if _, ok, _ := table.Get("m1"); ok {
		t.Fatal("deleted")
	}
	mustRun(t, r, Request{Noun: "ochazuke.history-entry", Verb: "undo"})
	if doc, _, _ := table.Get("m1"); doc["former_owner"] != "p9" || doc["stamp"] != 42 || doc["text"] != "hi" {
		t.Fatalf("undo restores every field: %v", doc)
	}
}

// A BeforeWrite hook's messages are told each on its own by kind once the
// write is stored.
func TestDocWriteMessagesAreToldByKind(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	r.Define("memo").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "text", Role: RoleLabel},
	).Store(DocStore(NewMemDocTable(r, "memo"), "memo", DocHooks{BeforeWrite: func(_ Ctx, w *DocWrite) error {
		w.Messages = append(w.Messages, UIMessage{Kind: UINotice, Text: "task t9 not found: saved unassociated"}, UIMessage{Kind: UINotice, Text: "project p2 not found"})
		return nil
	}}))
	mustRun(t, r, Request{Noun: "memo", Verb: "create", Args: Arguments{"text": "hi"}})
	var notices []string
	for _, m := range sink.Messages() {
		if m.Kind == UINotice {
			notices = append(notices, m.Text)
		}
	}
	if !slices.Contains(notices, "task t9 not found: saved unassociated") || !slices.Contains(notices, "project p2 not found") {
		t.Fatalf("each message a notice of its own: %v", notices)
	}
}

// A scope's values are part of the store's change token, and a scope row
// that says Every holds for requests that ask for every thing (counts,
// pickers); one that does not leaves them the whole store.
func TestListScopeTokenAndEvery(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	table := NewMemDocTable(r, "chore")
	pinned := "p1"
	s := DocStore(table, "chore")
	s.ListScope = []ListScope{{Prop: "project", Value: "pinned"}, {Prop: "kind", Value: "kind", Every: true}}
	s.ScopeValue = func(_ Ctx, name string) any {
		if name == "pinned" {
			return pinned
		}
		return "chore"
	}
	r.Define("chore").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "project"},
		PropSpec{Name: "kind"},
	).Store(s)
	for i := 1; i <= 6; i++ {
		_ = table.Put(fmt.Sprintf("c%d", i), Thing{"id": fmt.Sprintf("c%d", i), "project": []string{"p1", "p2"}[i%2], "kind": []string{"chore", "task", "chore"}[i%3]})
	}
	c := Ctx{Runtime: r}
	store := r.Nouns["chore"].Store
	before, _ := store.ChangeToken(c)
	pinned = "p2"
	if after, _ := store.ChangeToken(c); after == before {
		t.Fatalf("the pin moves the token: %q", after)
	}
	every, err := store.Query(c, ListQuery{Every: true, Archived: true})
	if err != nil {
		t.Fatal(err)
	}
	if every.Total != 4 {
		t.Fatalf("every thing of the Every row's scope, any project: %d", every.Total)
	}
}

// A derivation over another noun reads the things it names in one Query
// of that noun's store when it has one, not a Get per thing: 26 notes on
// 26 tasks, a first and a label derivation.
func TestDerivationReadsReferencedThingsInOneQuery(t *testing.T) {
	for _, withQuery := range []bool{false, true} {
		r := NewRuntime()
		r.Config().Disable()
		var all []Thing
		for i := range 26 {
			all = append(all, Thing{"id": fmt.Sprintf("t%d", i), "title": fmt.Sprintf("task %d", i), "project": "p1"})
		}
		gets, queries := 0, 0
		ts := Store{
			Get: func(c Ctx, key string) (Thing, error) {
				gets++
				for _, t := range all {
					if t["id"] == key {
						return cloneThing(t), nil
					}
				}
				return nil, c.Runtime.ThingNotFound(c, "task", key)
			},
			List: func(Ctx, ListQuery) ([]Thing, error) { return all, nil },
		}
		if withQuery {
			ts.Query = func(c Ctx, q ListQuery) (ListPage, error) {
				queries++
				return c.Runtime.QueryAll("task", all, q), nil
			}
		}
		r.Define("task").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}, PropSpec{Name: "project"}).Store(ts)
		table := NewMemDocTable(r, "note")
		r.Define("note").Props(
			PropSpec{Name: "id", Key: true, CreateOnly: true},
			PropSpec{Name: "task", Parameter: Parameter{Noun: "task"}},
			PropSpec{Name: "task-title", Derivation: &Derivation{Of: "label", Reads: []string{"task"}}},
			PropSpec{Name: "project", Derivation: &Derivation{Of: "first", Reads: []string{"task.project", "project"}}},
		).Store(DocStore(table, "note"))
		for i := range 26 {
			_ = table.Put(fmt.Sprintf("n%d", i), Thing{"id": fmt.Sprintf("n%d", i), "task": fmt.Sprintf("t%d", i)})
		}
		gets, queries = 0, 0
		a := listIDs(t, r, "note", nil)
		if len(a.Items) != 26 || a.Items[3]["task-title"] != "task 3" || a.Items[3]["project"] != "p1" {
			t.Fatalf("derived rows: %v", a.Items[3])
		}
		t.Logf("with Query %v: %d gets, %d queries", withQuery, gets, queries)
		// The batch's two derived props share their reads: one per task.
		if withQuery && (gets != 0 || queries != 1) || !withQuery && gets != 26 {
			t.Fatalf("with Query %v: %d gets, %d queries", withQuery, gets, queries)
		}
	}
}
