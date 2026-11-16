package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// A prop that keeps its own value and shows a referenced thing's when it
// has one (Derivation first: Reads deck.project, project): get shows the
// deck's project over the card's own field; a set on a card with no deck
// stores the card's own field; a set the deck's project would hide does
// nothing and says why; clearing the deck in the same set lets it apply;
// and a change to a deck moves the card's lists.
func TestFirstDerivationReadAndWritten(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	decks, cards := NewMemDocTable(r, "deck"), NewMemDocTable(r, "card")
	r.Define("project").Props(PropSpec{Name: "id", Key: true, CreateOnly: true}).Store(DocStore(NewMemDocTable(r, "project"), "project"))
	r.Define("deck").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "project"},
	).Store(DocStore(decks, "deck"))
	r.Define("card").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "deck", Parameter: Parameter{Noun: "deck"}},
		PropSpec{Name: "project", Field: "project_id", Role: RoleOwner, FromFocus: true, Parameter: Parameter{Noun: "project", Free: true}, Derivation: &Derivation{Of: "first", Reads: []string{"deck.project", "project"}}},
	).Store(DocStore(cards, "card"))
	mustRun(t, r, Request{Noun: "deck", Verb: "create", Args: Arguments{"title": "spades", "project": "p1"}})
	mustRun(t, r, Request{Noun: "card", Verb: "create", Args: Arguments{"title": "ace", "deck": "d1"}})
	mustRun(t, r, Request{Noun: "card", Verb: "create", Args: Arguments{"title": "loose", "project": "p2"}})
	c := Ctx{Runtime: r}
	get := func(key string) Thing {
		t.Helper()
		got, err := r.Nouns["card"].Store.Get(c, key)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := get("c1"); got["project"] != "p1" {
		t.Fatalf("the deck's project shows: %v", got)
	}
	if doc, _, _ := cards.Get("c2"); doc["project_id"] != "p2" || get("c2")["project"] != "p2" {
		t.Fatalf("a card with no deck stores its own field: %v", doc)
	}
	mustRun(t, r, Request{Noun: "card", Verb: "set", Target: "c2", Args: Arguments{"props": map[string]any{"project": "p3"}}})
	if doc, _, _ := cards.Get("c2"); doc["project_id"] != "p3" {
		t.Fatalf("set on a card with no deck writes its own field: %v", doc)
	}
	if out := r.ExecuteOutcome(Request{Noun: "card", Verb: "set", Target: "c1", Args: Arguments{"props": map[string]any{"project": "p9"}}}); out.Kind != OutcomeNothing || !strings.Contains(out.Reason, "deck d1's project (p1)") {
		t.Fatalf("a set the deck's project hides does nothing, with the reason: %+v", out)
	}
	if doc, _, _ := cards.Get("c1"); doc["project_id"] != nil {
		t.Fatalf("nothing was written: %v", doc)
	}
	mustRun(t, r, Request{Noun: "card", Verb: "set", Target: "c1", Args: Arguments{"props": map[string]any{"deck": "", "project": "p9"}}})
	if got := get("c1"); got["project"] != "p9" {
		t.Fatalf("with the deck cleared the card's own project shows: %v", got)
	}
	if !slices.Contains(r.withDependents([]string{"deck"}), "card") {
		t.Fatal("a change to a deck marks card changed")
	}
}

// A BeforeWrite hook sees each create and set with the thing as it is,
// changes what is stored (an owner it cannot find saved unassociated, a
// field that is no prop set beside it) and adds an info line.
func TestDocStoreBeforeWriteChangesWhatIsStored(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	table := NewMemDocTable(r, "memo")
	var olds []Thing
	r.Define("memo").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "text", Role: RoleLabel},
		PropSpec{Name: "owner"},
	).Store(DocStore(table, "memo", DocHooks{BeforeWrite: func(_ Ctx, w *DocWrite) error {
		olds = append(olds, w.Old)
		if owner, ok := w.Changes["owner"].(string); ok && owner != "" && owner != "known" {
			w.Changes["owner"] = nil
			w.Changes["former_owner"] = owner
			w.Info = append(w.Info, "no owner "+owner+": saved unassociated")
		}
		return nil
	}}))
	mustRun(t, r, Request{Noun: "memo", Verb: "create", Args: Arguments{"text": "hi", "owner": "known"}})
	mustRun(t, r, Request{Noun: "memo", Verb: "set", Target: "m1", Args: Arguments{"props": map[string]any{"owner": "gone"}}})
	doc, _, _ := table.Get("m1")
	if _, has := doc["owner"]; has || doc["former_owner"] != "gone" || doc["text"] != "hi" {
		t.Fatalf("the hook's changes are stored: %v", doc)
	}
	if len(olds) != 2 || olds[0] != nil || olds[1]["owner"] != "known" {
		t.Fatalf("the hook sees nothing on create and the thing on set: %v", olds)
	}
	if !slices.ContainsFunc(sink.Messages(), func(m UIMessage) bool { return m.Kind == UIInfo && m.Text == "no owner gone: saved unassociated" }) {
		t.Fatalf("the hook's line is told: %v", sink.Messages())
	}
}

// A list scope declared as data (Store.ListScope) narrows the list to the
// value the application supplies (the pinned project) unless all is
// given, and the table runs the condition in its paged query.
func TestListScopeRunsInThePagedQuery(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	table := NewMemDocTable(r, "chore")
	pinned := "p1"
	s := DocStore(table, "chore")
	s.ListParams = map[string]Parameter{"all": {Type: SettingBoolean, Description: "Every chore, not only the pinned project's"}}
	s.ListScope = []ListScope{{Prop: "project", Value: "pinned", Unless: "all"}}
	s.ScopeValue = func(_ Ctx, name string) any {
		if name == "pinned" {
			return pinned
		}
		return nil
	}
	r.Define("chore").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "project"},
	).Store(s)
	for i := 1; i <= 30; i++ {
		_ = table.Put(fmt.Sprintf("c%d", i), Thing{"id": fmt.Sprintf("c%d", i), "title": "chore", "project": []string{"p1", "p2", "p2"}[i%3]})
	}
	table.Reads, table.Queries = 0, 0
	if a := listIDs(t, r, "chore", Arguments{"per-page": 4}); a.Total != 10 || len(a.Items) != 4 || table.Reads != 4 || table.Queries != 1 {
		t.Fatalf("scoped page: total %d items %d reads %d queries %d", a.Total, len(a.Items), table.Reads, table.Queries)
	}
	if a := listIDs(t, r, "chore", Arguments{"all": true}); a.Total != 30 {
		t.Fatalf("all widens the scope: %d", a.Total)
	}
	pinned = ""
	if a := listIDs(t, r, "chore", nil); a.Total != 30 {
		t.Fatalf("nothing pinned is everything: %d", a.Total)
	}
}
