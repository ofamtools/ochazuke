package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// countingStore is a store of n things that counts its reads: List calls
// (each a full read), Query calls and the rows they answered, and token
// calls. With paged, it answers Query; with token, ChangeToken.
type countingStore struct {
	things                 []Thing
	lists, queries, tokens int
	rows                   int
	version                int
}

func newCountingStore(n, archived int) *countingStore {
	s := &countingStore{}
	for i := range n {
		s.things = append(s.things, Thing{"id": fmt.Sprintf("t%05d", i), "title": fmt.Sprintf("thing %d", i), "done": i%3 == 0, "archived": i < archived})
	}
	return s
}

func (s *countingStore) store(paged, token bool) Store {
	st := Store{
		Stamps: true,
		Get: func(_ Ctx, id string) (Thing, error) {
			for _, t := range s.things {
				if t["id"] == id {
					return t, nil
				}
			}
			return nil, fmt.Errorf("no %s", id)
		},
		List: func(Ctx, ListQuery) ([]Thing, error) {
			s.lists++
			s.rows += len(s.things)
			return s.things, nil
		},
		Patch: func(_ Ctx, id string, changes Thing) (Thing, error) {
			for _, t := range s.things {
				if t["id"] == id {
					for k, v := range changes {
						t[k] = v
					}
				}
			}
			s.version++
			return nil, nil
		},
	}
	if paged {
		st.Query = func(c Ctx, q ListQuery) (ListPage, error) {
			s.queries++
			// The fake pages over its rows in memory, as a database would
			// with LIMIT/OFFSET: only the page's rows count as read.
			n := c.Runtime.Noun(c.Noun)
			if n == nil {
				n = c.Runtime.Noun("big")
			}
			page := c.Runtime.queryAll(n, s.things, q)
			s.rows += len(page.Items)
			return page, nil
		}
	}
	if token {
		st.ChangeToken = func(Ctx) (string, error) {
			s.tokens++
			return fmt.Sprint(s.version), nil
		}
	}
	return st
}

// nounsFixture is an application with a paged record noun (with an
// archive prop), a record noun that only lists, a command noun and a part.
func nounsFixture(t *testing.T) (*Runtime, *Model, *countingStore, *countingStore) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	big := newCountingStore(30, 4)
	whole := newCountingStore(5, 0)
	props := []PropSpec{{Name: "id", Key: true}, {Name: "title", Role: RoleLabel}, {Name: "done", Parameter: Parameter{Type: SettingBoolean}}, {Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}}}
	r.Define("big").Props(props...).Store(big.store(true, true))
	r.Define("whole").Props(props[:3]...).Store(whole.store(false, false))
	r.Define("setup").Verb("run", func(Ctx) Result { return Done(nil) }, "Run the setup")
	r.Define("turn-line").Verb("fold", func(*Runtime, Context, Arguments) (any, error) { return nil, nil }, "Fold the line")
	if _, err := r.AddPage("work", "Work", func(*Runtime, int, int) Block { return Text("work") }); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Init()
	m.Width, m.Height = 120, 40
	return r, m, big, whole
}

func nounRow(t *testing.T, r *Runtime, noun string) ListItem {
	t.Helper()
	for _, it := range r.List(NounsListID).Items() {
		if it.ID == nounsRowPrefix+noun {
			return it
		}
	}
	t.Fatalf("no row for %s", noun)
	return ListItem{}
}

// The Nouns page is the last tab, and nouns.page off keeps it out.
func TestNounsPageIsTheLastTab(t *testing.T) {
	r, _, _, _ := nounsFixture(t)
	tabs := r.TabPages()
	if len(tabs) != 2 || tabs[len(tabs)-1].ID != NounsPageID || tabs[0].ID != "work" {
		t.Fatalf("tabs %v", nounTabIDs(tabs))
	}
	if _, err := r.Settings().SetSession(NounsPageSetting, false); err != nil {
		t.Fatal(err)
	}
	if ids := nounTabIDs(r.TabPages()); slices.Contains(ids, NounsPageID) {
		t.Fatalf("nouns.page off: %v", ids)
	}
	mustRun(t, r, Request{Noun: nounsPageNoun, Verb: "open"})
	if r.Page != NounsPageID {
		t.Fatalf("noun-browser.open opens it while out of the tabs: %s", r.Page)
	}
	if ids := nounTabIDs(r.TabPages()); slices.Contains(ids, NounsPageID) {
		t.Fatalf("out of the tabs even while current: %v", ids)
	}
}

func nounTabIDs(pages []*Page) []string {
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = p.ID
	}
	return out
}

// Opening the page lists every noun of a shown kind, grouped by kind,
// counts a paged store with one-row pages and never reads a store that can
// only list everything; parts are left out; command nouns list their
// actions. Drawing reads no store.
func TestNounsPageListsKindsAndCounts(t *testing.T) {
	r, m, big, whole := nounsFixture(t)
	if big.lists+big.queries+whole.lists != 0 {
		t.Fatal("startup read a store")
	}
	if err := r.OpenPage(NounsPageID); err != nil {
		t.Fatal(err)
	}
	row := nounRow(t, r, "big")
	if row.Section != "records" || row.Cells[1] != "30" || row.Cells[2] != "4" || row.Cells[3] != "local · pages · token" {
		t.Fatalf("big row %q %v", row.Section, row.Cells)
	}
	if big.lists != 0 || big.rows > 2 {
		t.Fatalf("a paged store was counted by a full read: lists %d rows %d", big.lists, big.rows)
	}
	if row := nounRow(t, r, "whole"); row.Cells[1] != "–" || whole.lists != 0 {
		t.Fatalf("a list-only store is not read: %v lists %d", row.Cells, whole.lists)
	}
	if row := nounRow(t, r, "setup"); row.Section != "commands" || row.Cells[4] != "run" {
		t.Fatalf("setup row %q %v", row.Section, row.Cells)
	}
	for _, it := range r.List(NounsListID).Items() {
		if it.ID == nounsRowPrefix+"turn-line" {
			t.Fatal("parts are left out")
		}
	}
	// Facts are counted where they are.
	if row := nounRow(t, r, "ochazuke.noun"); row.Section != "facts" || row.Cells[1] == "–" {
		t.Fatalf("facts row %q %v", row.Section, row.Cells)
	}
	before := big.lists + big.queries + big.tokens + whole.lists
	for range 3 {
		_ = m.View()
	}
	if after := big.lists + big.queries + big.tokens + whole.lists; after != before {
		t.Fatalf("drawing read a store: %d -> %d", before, after)
	}
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"Nouns", "records", "facts", "big", "30"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame lacks %q:\n%s", want, frame)
		}
	}
	// The ochazuke.noun facts carry the kind and the counts read.
	res := mustRun(t, r, Request{Noun: "ochazuke.noun", Verb: "get", Args: Arguments{"noun": "big"}})
	thing, _ := ThingOf(res)
	if thing["kind"] != NounKindRecord || fmt.Sprint(thing["count"]) != "30" || fmt.Sprint(thing["archived"]) != "4" {
		t.Fatalf("noun get %v", thing)
	}
	// nouns.count-lists counts a list-only store by listing it.
	if _, err := r.Settings().SetSession(NounsCountListsSetting, true); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: nounsPageNoun, Verb: "refresh"})
	if row := nounRow(t, r, "whole"); row.Cells[1] != "5" || whole.lists != 1 {
		t.Fatalf("count-lists: %v lists %d", row.Cells, whole.lists)
	}
}

// A store whose change token is unchanged is not read again; a write
// moves the token and the next refresh reads the count again.
func TestNounsPageCountsGatedByChangeToken(t *testing.T) {
	r, _, big, _ := nounsFixture(t)
	if err := r.OpenPage(NounsPageID); err != nil {
		t.Fatal(err)
	}
	queries := big.queries
	mustRun(t, r, Request{Noun: nounsPageNoun, Verb: "refresh"})
	if big.queries != queries {
		t.Fatalf("an unchanged token read the store again: %d -> %d", queries, big.queries)
	}
	mustRun(t, r, Request{Noun: "big", Verb: "set", Args: Arguments{"id": "t00010", "props": map[string]any{"archived": true}}})
	mustRun(t, r, Request{Noun: nounsPageNoun, Verb: "refresh"})
	if big.queries == queries {
		t.Fatal("a moved token must read the count again")
	}
	if row := nounRow(t, r, "big"); row.Cells[2] != "5" {
		t.Fatalf("archived after the write %v", row.Cells)
	}
}

// While the Nouns page is open, a change recounts the stores whose token
// moved, with no refresh key.
func TestNounsPageRecountsOnAChangeWhileOpen(t *testing.T) {
	r, _, big, _ := nounsFixture(t)
	if err := r.OpenPage(NounsPageID); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "big", Verb: "set", Args: Arguments{"id": "t00011", "props": map[string]any{"archived": true}}})
	if row := nounRow(t, r, "big"); row.Cells[2] != "5" {
		t.Fatalf("archived after a write while open: %v (queries %d)", row.Cells, big.queries)
	}
}
