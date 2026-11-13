package ochazuke

import "testing"

// hiddenRuntime has two pages, each with a From list of notes; page a is
// current.
func hiddenRuntime(t *testing.T, mode string) (*Runtime, *fromStore, *Collection, *Collection) {
	t.Helper()
	r, notes, _ := fromRuntime(t)
	if err := r.Settings().Set(RefreshHiddenSetting, mode); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := r.AddPage(id, id, func(*Runtime, int, int) Block { return Text("") }); err != nil {
			t.Fatal(err)
		}
	}
	la, err := r.AddList("a/l", nil, From("note"), Parent("a"))
	if err != nil {
		t.Fatal(err)
	}
	lb, err := r.AddList("b/l", nil, From("note"), Parent("b"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("a"); err != nil {
		t.Fatal(err)
	}
	return r, notes, la, lb
}

func TestRefreshHiddenLazy(t *testing.T) {
	r, notes, la, lb := hiddenRuntime(t, RefreshHiddenLazy)
	reads, before := notes.lists, lb.refills
	notes.things = append(notes.things, Thing{"id": "n4", "title": "Fourth"})
	r.Changed("note")
	if len(la.all) != 4 {
		t.Fatalf("the shown list has %d rows, want 4", len(la.all))
	}
	if lb.refills != before || !lb.Stale() || len(lb.all) != 3 {
		t.Fatalf("the hidden list was read (%d reads, stale %v, %d rows)", lb.refills-before, lb.Stale(), len(lb.all))
	}
	if notes.lists-reads != 2 { // the watch's snapshot and the shown list
		t.Fatalf("%d store reads for one change, want 2", notes.lists-reads)
	}

	// Drawn by a model: opening the page keeps the last rows for the frame
	// and reads after it.
	r.watchHub().frames = 1
	if err := r.OpenPage("b"); err != nil {
		t.Fatal(err)
	}
	if !lb.Stale() || len(lb.all) != 3 || !(JobSlot{Scope: "b/l", Slot: listReadSlot}).Running(r) {
		t.Fatalf("shown again: stale %v, %d rows, read queued %v", lb.Stale(), len(lb.all), (JobSlot{Scope: "b/l", Slot: listReadSlot}).Running(r))
	}
	settleAll(t, r)
	if lb.Stale() || len(lb.all) != 4 {
		t.Fatalf("after the frame: stale %v, %d rows", lb.Stale(), len(lb.all))
	}

	// Without a model the list reads when its page opens; Items reads it too.
	r.watchHub().frames = 0
	notes.things = append(notes.things, Thing{"id": "n5", "title": "Fifth"})
	r.Changed("note")
	if !la.Stale() {
		t.Fatal("page a's list, now hidden, was read")
	}
	if got := len(la.Items()); got != 5 || la.Stale() {
		t.Fatalf("Items read %d rows, stale %v", got, la.Stale())
	}
}

func TestRefreshHiddenEager(t *testing.T) {
	r, notes, _, lb := hiddenRuntime(t, RefreshHiddenEager)
	before := lb.refills
	notes.things = append(notes.things, Thing{"id": "n4", "title": "Fourth"})
	r.Changed("note")
	if lb.refills == before || lb.Stale() || len(lb.all) != 4 {
		t.Fatalf("eager: the hidden list was not read (stale %v, %d rows)", lb.Stale(), len(lb.all))
	}
}
