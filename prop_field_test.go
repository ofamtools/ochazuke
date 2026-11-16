package ochazuke

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

func listIDs(t *testing.T, r *Runtime, noun string, args Arguments) ListAnswer {
	t.Helper()
	v, err := r.Execute(Request{Noun: noun, Verb: "list", Args: args})
	if err != nil {
		t.Fatalf("list %v: %v", args, err)
	}
	return v.(ListAnswer)
}

// A prop stored under another field name (PropSpec.Field) round-trips
// through DocStore: the document holds the field, the thing the prop, and
// conditions, sort, text search and the archive hiding reach the table in
// field names.
func TestDocStoreFieldRenamesEveryPath(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	table := NewMemDocTable(r, "chore")
	r.Define("chore").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "project", Field: "project_id"},
		PropSpec{Name: "archived", Field: "is_archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
	).Store(DocStore(table, "chore"))
	for _, v := range []Arguments{{"title": "sweep", "project": "p2"}, {"title": "dust", "project": "p1"}, {"title": "mop", "project": "p3"}} {
		mustRun(t, r, Request{Noun: "chore", Verb: "create", Args: v})
	}
	doc, _, _ := table.Get("c1")
	if doc["project_id"] != "p2" || doc["project"] != nil {
		t.Fatalf("the document holds the field: %v", doc)
	}
	c := Ctx{Runtime: r}
	if got, err := r.Nouns["chore"].Store.Get(c, "c1"); err != nil || got["project"] != "p2" || got["project_id"] != nil {
		t.Fatalf("get reads the prop: %v %v", got, err)
	}
	if a := listIDs(t, r, "chore", Arguments{"where": "project=p1"}); !slices.Equal(answerIDs(a), []string{"c2"}) {
		t.Fatalf("filter on the renamed prop: %v", answerIDs(a))
	}
	if a := listIDs(t, r, "chore", Arguments{"sort": "project:desc"}); !slices.Equal(answerIDs(a), []string{"c3", "c1", "c2"}) {
		t.Fatalf("sort on the renamed prop: %v", answerIDs(a))
	}
	mustRun(t, r, Request{Noun: "chore", Verb: "set", Target: "c1", Args: Arguments{"props": map[string]any{"project": "p9"}}})
	if doc, _, _ := table.Get("c1"); doc["project_id"] != "p9" || doc["project"] != nil {
		t.Fatalf("set writes the field: %v", doc)
	}
	if page, err := r.Nouns["chore"].Store.Query(c, ListQuery{Text: "dust"}); err != nil || len(page.Items) != 1 || page.Items[0]["project"] != "p1" {
		t.Fatalf("text search: %v %v", page.Items, err)
	}
	mustRun(t, r, Request{Noun: "chore", Verb: "archive", Target: "c2"})
	if doc, _, _ := table.Get("c2"); doc["is_archived"] != true {
		t.Fatalf("archive writes the field: %v", doc)
	}
	if a := listIDs(t, r, "chore", nil); !slices.Equal(answerIDs(a), []string{"c1", "c3"}) {
		t.Fatalf("the archive hiding reads the field: %v", answerIDs(a))
	}
}

// A derived prop (PropSpec.Derivation) is computed on every read and never
// stored; a list filters and sorts on it (reading the table unpaged), set
// answers the read-only outcome, and a change in the noun it reads moves
// the store's change token and marks its lists changed.
func TestDerivedPropsComputedFilteredAndTracked(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	boxes, items := NewMemDocTable(r, "box"), NewMemDocTable(r, "item")
	r.Define("box").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "name", Role: RoleLabel},
		PropSpec{Name: "opened", Parameter: Parameter{Type: PropTime}},
		PropSpec{Name: "body", Multiline: true},
		PropSpec{Name: "items", Derivation: &Derivation{Of: "count", Reads: []string{"item.box"}}},
		PropSpec{Name: "age", Derivation: &Derivation{Of: "since", Reads: []string{"opened"}}},
		PropSpec{Name: "head", Derivation: &Derivation{Of: "first-line", Reads: []string{"body"}}},
	).Store(DocStore(boxes, "box"))
	r.Define("item").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "box", Parameter: Parameter{Noun: "box"}},
		PropSpec{Name: "place", Derivation: &Derivation{Of: "label", Reads: []string{"box"}}},
	).Store(DocStore(items, "item"))
	mustRun(t, r, Request{Noun: "box", Verb: "create", Args: Arguments{"name": "attic", "opened": "2026-01-01T00:00:00Z", "body": "\n  first line\nsecond"}})
	mustRun(t, r, Request{Noun: "box", Verb: "create", Args: Arguments{"name": "cellar"}})
	for _, box := range []string{"b1", "b1", "b2"} {
		mustRun(t, r, Request{Noun: "item", Verb: "create", Args: Arguments{"title": "thing", "box": box}})
	}
	c := Ctx{Runtime: r}
	b1, err := r.Nouns["box"].Store.Get(c, "b1")
	if err != nil || b1["items"] != 2 || b1["head"] != "first line" {
		t.Fatalf("derived on get: %v %v", b1, err)
	}
	if d, err := time.ParseDuration(b1["age"].(string)); err != nil || d <= 0 {
		t.Fatalf("since: %v %v", b1["age"], err)
	}
	if doc, _, _ := boxes.Get("b1"); doc["items"] != nil || doc["head"] != nil || doc["age"] != nil {
		t.Fatalf("a derived prop is never stored: %v", doc)
	}
	if i1, _ := r.Nouns["item"].Store.Get(c, "i1"); i1["place"] != "attic" {
		t.Fatalf("label of a reference: %v", i1)
	}
	if a := listIDs(t, r, "box", Arguments{"where": "items=1"}); !slices.Equal(answerIDs(a), []string{"b2"}) || fmt.Sprint(a.Items[0]["items"]) != "1" {
		t.Fatalf("filter on a derived prop: %+v", a)
	}
	if a := listIDs(t, r, "box", Arguments{"sort": "items", "per-page": 1}); !slices.Equal(answerIDs(a), []string{"b2"}) || a.Total != 2 {
		t.Fatalf("sort and page on a derived prop: %+v", a)
	}
	_, err = r.Execute(Request{Noun: "box", Verb: "set", Target: "b1", Args: Arguments{"props": map[string]any{"items": 9}}})
	if out := OutcomeOf(nil, err); out.Kind != OutcomeNothing {
		t.Fatalf("a write of a derived prop answers the read-only outcome: %+v %v", out, err)
	}
	before := storeToken(r.Nouns["box"].Store, c)
	mustRun(t, r, Request{Noun: "item", Verb: "create", Args: Arguments{"title": "more", "box": "b2"}})
	if after := storeToken(r.Nouns["box"].Store, c); before == "" || after == before {
		t.Fatalf("an item's change moves the box token: %q %q", before, after)
	}
	if !slices.Contains(r.withDependents([]string{"item"}), "box") {
		t.Fatal("a change to item marks box changed")
	}
}
