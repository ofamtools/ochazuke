package ochazuke

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// placeOf is the one place of noun on page (and modal), failing otherwise.
func placeOf(t *testing.T, r *Runtime, noun, page, modal string) NounPlace {
	t.Helper()
	var found []NounPlace
	for _, p := range r.NounPlaces(noun) {
		if p.Page == page && p.Modal == modal {
			found = append(found, p)
		}
	}
	if len(found) != 1 {
		t.Fatalf("places of %q on %q/%q: %+v (all %+v)", noun, page, modal, found, r.NounPlaces(noun))
	}
	return found[0]
}

func placesPage(t *testing.T) (*Runtime, *List) {
	t.Helper()
	r := NewRuntime()
	r.Define("item")
	r.Define("other-item")
	if _, err := r.AddPage("ctx", "Context", func(*Runtime, int, int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	r.Instance("panel", Parent("ctx"), Kind("panel"))
	list, err := r.AddList("rows", []ListItem{{ID: "a", Noun: "item"}, {ID: "b", Noun: "item"}, {ID: "c", Noun: "other-item"}}, Parent("panel"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("ctx"); err != nil {
		t.Fatal(err)
	}
	return r, list
}

// Every added instance is recorded with its ancestors' nouns (outermost
// first, app left out) and its page; list rows by their noun, once per
// SetItems, never by id.
func TestNounPlacesRecordTheAncestorChain(t *testing.T) {
	r, list := placesPage(t)
	item := placeOf(t, r, "item", "ctx", "")
	if want := []string{"page", "panel", "list"}; !reflect.DeepEqual(item.Ancestors, want) || item.PageTitle != "Context" || item.Count != 1 {
		t.Fatalf("item place: %+v", item)
	}
	if got := item.Chain(); !reflect.DeepEqual(got, []string{"page", "panel", "list", "item"}) {
		t.Fatalf("chain %v", got)
	}
	if other := placeOf(t, r, "other-item", "ctx", ""); other.Count != 1 {
		t.Fatalf("other-item: %+v", other)
	}
	if l := placeOf(t, r, "list", "ctx", ""); !reflect.DeepEqual(l.Ancestors, []string{"page", "panel"}) {
		t.Fatalf("list place: %+v", l)
	}
	if p := placeOf(t, r, "page", "ctx", ""); len(p.Ancestors) != 0 {
		t.Fatalf("the page itself: %+v", p)
	}
	for _, p := range r.NounPlaces("") {
		for _, n := range p.Chain() {
			if n == "a" || n == "rows" || n == "ctx" {
				t.Fatalf("an instance id in a place: %+v", p)
			}
		}
	}
	// Same place again: one entry, counted, last seen moves.
	before := placeOf(t, r, "item", "ctx", "")
	time.Sleep(2 * time.Millisecond)
	if err := list.SetItems([]ListItem{{ID: "x", Noun: "item"}, {ID: "y", Noun: "item"}}); err != nil {
		t.Fatal(err)
	}
	after := placeOf(t, r, "item", "ctx", "")
	if after.Count != 2 || !after.Last.After(before.Last) || !after.First.Equal(before.First) {
		t.Fatalf("dedup: before %+v after %+v", before, after)
	}
}

// A page registered after its children (the instance first, the page
// later) still places them on it: placements settle after the update.
func TestNounPlacesSettleLate(t *testing.T) {
	r, _, _, _ := listFixture(t)
	note := placeOf(t, r, "prompt", "page", "")
	if !reflect.DeepEqual(note.Ancestors, []string{"page", "editor"}) {
		t.Fatalf("note: %+v", note)
	}
}

// Inside a modal the chain starts at the modal instance (the walk stops
// there) and the place names the page beneath.
func TestNounPlacesInsideAModal(t *testing.T) {
	r, _ := placesPage(t)
	in := NewInput("answer", "", false)
	if _, err := r.OpenModal("ask", Payload{}, WithInput(in, Kind("prompt"))); err != nil {
		t.Fatal(err)
	}
	if err := r.Pop(); err != nil { // removed in the same update: still seen
		t.Fatal(err)
	}
	p := placeOf(t, r, "prompt", "ctx", "ask")
	if !reflect.DeepEqual(p.Ancestors, []string{"ask"}) || p.PageTitle != "Context" {
		t.Fatalf("modal input: %+v", p)
	}
	if m := placeOf(t, r, "ask", "ctx", "ask"); len(m.Ancestors) != 0 {
		t.Fatalf("modal body: %+v", m)
	}
	// An existing instance opened as a modal is seen there with its
	// subtree.
	r.Instance("box", Kind("box"))
	r.Instance("box/field", Parent("box"), Kind("field"))
	if p := placeOf(t, r, "field", "", ""); !reflect.DeepEqual(p.Ancestors, []string{"box"}) {
		t.Fatalf("closed box: %+v", p)
	}
	if err := r.Push("box", "box/field"); err != nil {
		t.Fatal(err)
	}
	if p := placeOf(t, r, "field", "ctx", "box"); !reflect.DeepEqual(p.Ancestors, []string{"box"}) {
		t.Fatalf("open box: %+v", p)
	}
}

// SetItems of a thousand rows records one place per noun, counted once.
func TestNounPlacesSetItemsOncePerNoun(t *testing.T) {
	r, list := placesPage(t)
	r.NounPlaces("")
	items := benchRows("z", 1000)
	for i := range items {
		items[i].Noun = "item"
	}
	items[500].Noun = "other-item"
	if err := list.SetItems(items); err != nil {
		t.Fatal(err)
	}
	if p := r.placementConfig().places; len(p.pending) != 1 || !reflect.DeepEqual(p.pending[0].rows, []string{"item", "other-item"}) {
		t.Fatalf("pending after SetItems: %+v", p.pending)
	}
	if n := placeOf(t, r, "item", "ctx", "").Count; n != 2 {
		t.Fatalf("item count %d", n)
	}
	if n := placeOf(t, r, "other-item", "ctx", "").Count; n != 2 {
		t.Fatalf("other-item count %d", n)
	}
	if got := len(r.NounPlaces("a")); got != 0 {
		t.Fatalf("a row id became a noun place: %d", got)
	}
}

// Past NounPlaceLimit the least recently seen place is dropped.
func TestNounPlacesCap(t *testing.T) {
	p := &placements{byKey: map[string]*NounPlace{}}
	start := time.Now()
	for i := range NounPlaceLimit + 10 {
		p.record(NounPlace{Noun: fmt.Sprint("n", i), Ancestors: []string{}}, start.Add(time.Duration(i)*time.Second))
	}
	if len(p.byKey) != NounPlaceLimit {
		t.Fatalf("kept %d", len(p.byKey))
	}
	for _, gone := range []string{"n0", "n9"} {
		if p.byKey[NounPlace{Noun: gone, Ancestors: []string{}}.key()] != nil {
			t.Fatalf("%s kept", gone)
		}
	}
	if p.byKey[NounPlace{Noun: "n10", Ancestors: []string{}}.key()] == nil {
		t.Fatal("n10 dropped")
	}
}

// nouns.places answers the places of a noun (every noun without one);
// nouns.list the nouns with their counts of places.
func TestNounPlacesVerb(t *testing.T) {
	r, _ := placesPage(t)
	for _, key := range []string{"nouns.places", "nouns.list", "nouns.places-path"} {
		op := r.Operations[key]
		if !op.Builtin || op.HistoryKindOf() != HistoryFetch {
			t.Fatalf("%s: builtin %v kind %s", key, op.Builtin, op.HistoryKindOf())
		}
	}
	got, err := r.Execute(Request{Verb: "places", Noun: "nouns", Args: Arguments{"noun": "item"}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(got)
	var places []map[string]any
	if err := json.Unmarshal(data, &places); err != nil || len(places) != 1 {
		t.Fatalf("places: %s (%v)", data, err)
	}
	for _, field := range []string{"noun", "ancestors", "page", "pageTitle", "count", "first", "last"} {
		if _, ok := places[0][field]; !ok {
			t.Fatalf("no %s in %s", field, data)
		}
	}
	all, _ := r.Execute(Request{Verb: "places", Noun: "nouns"})
	if len(all.([]NounPlace)) <= 1 {
		t.Fatalf("every noun: %v", all)
	}
	list, _ := r.Execute(Request{Verb: "list", Noun: "nouns"})
	for _, row := range list.([]map[string]any) {
		if row["noun"] == "item" && row["places"] != 1 {
			t.Fatalf("nouns.list item: %v", row)
		}
	}
}

// The record persists in the state directory: written on Flush (temp +
// rename), read and merged by the next run; a file that does not parse is
// one notice and is replaced.
func TestNounPlacesPersist(t *testing.T) {
	dir := t.TempDir()
	run := func() (*Runtime, []string) {
		r := NewRuntime()
		var notices []string
		c := r.Config()
		c.StateDir = dir
		c.Report = func(p string) { notices = append(notices, p) }
		r.EnsureNounPlaces()
		r.Define("item")
		if _, err := r.AddPage("ctx", "Context", func(*Runtime, int, int) Block { return Text("") }); err != nil {
			t.Fatal(err)
		}
		if _, err := r.AddList("rows", []ListItem{{ID: "a", Noun: "item"}}, Parent("ctx")); err != nil {
			t.Fatal(err)
		}
		r.settlePlacements()
		if err := c.Flush(); err != nil {
			t.Fatal(err)
		}
		return r, notices
	}
	run()
	path := filepath.Join(dir, NounPlacesFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved []NounPlace
	if err := json.Unmarshal(data, &saved); err != nil || len(saved) == 0 {
		t.Fatalf("file: %s (%v)", data, err)
	}
	r, notices := run()
	if len(notices) != 0 {
		t.Fatalf("notices: %v", notices)
	}
	if n := placeOf(t, r, "item", "ctx", "").Count; n != 2 {
		t.Fatalf("merged count %d", n)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(dir, ".noun-places-*")); len(leftovers) != 0 {
		t.Fatalf("temp files left: %v", leftovers)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, notices = run()
	if len(notices) != 1 || !strings.Contains(notices[0], NounPlacesFile) {
		t.Fatalf("corrupt file notices: %v", notices)
	}
	if n := placeOf(t, r, "item", "ctx", "").Count; n != 1 {
		t.Fatalf("count after a corrupt file %d", n)
	}
	data, _ = os.ReadFile(path)
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("corrupt file not replaced: %s", data)
	}
}

// Without a state directory (a test binary, Config.Disable) nothing is
// read or written; the record still answers in memory.
func TestNounPlacesNoFileWithoutStateDir(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	r, _ := placesPage(t)
	c := r.Config()
	if c.NounPlacesPath() != "" {
		t.Fatalf("a test binary has a places file: %s", c.NounPlacesPath())
	}
	r.EnsureNounPlaces()
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(state); len(entries) != 0 {
		t.Fatalf("state written: %v", entries)
	}
	if len(r.NounPlaces("item")) != 1 {
		t.Fatal("in-memory record empty")
	}
}

// BenchmarkSetItemsPlaces is SetItems of n rows with the settle that
// records their place, as one Update does.
func BenchmarkSetItemsPlaces(b *testing.B) {
	for _, n := range []int{1000, 16000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			r := benchPage(b)
			sets := [2][]ListItem{benchRows("x", n), benchRows("y", n)}
			list, err := r.AddList("l", sets[0], Parent("page"))
			if err != nil {
				b.Fatal(err)
			}
			i := 0
			for b.Loop() {
				i++
				if err := list.SetItems(sets[i%2]); err != nil {
					b.Fatal(err)
				}
				r.settlePlacements()
			}
		})
	}
}
