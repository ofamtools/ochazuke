package ochazuke

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// newUndoRuntime is the write runtime's task noun on a store that declares
// Undo, and whose Create keeps a key it is given (AcceptsKey), unless
// undo or keyed say otherwise.
func newUndoRuntime(t *testing.T, undo, keyed bool) (*Runtime, *writeStore) {
	t.Helper()
	r, s := newWriteRuntime(t)
	st := s.store()
	create := st.Create
	st.Create = func(c Ctx, values Thing) (Thing, error) {
		if id, ok := values["id"].(string); ok && keyed {
			t := Thing{}
			for k, v := range values {
				t[k] = v
			}
			s.things = append(s.things, t)
			s.log = append(s.log, "create "+id)
			return t, nil
		}
		return create(c, values)
	}
	st.Undo, st.AcceptsKey = undo, keyed
	r.Define("task").Store(st)
	return r, s
}

// lastChanges are the changes of the newest entry that holds some.
func lastChanges(r *Runtime) []ThingChange {
	entries := r.History(0)
	for i := len(entries) - 1; i >= 0; i-- {
		if len(entries[i].Changes) > 0 {
			return entries[i].Changes
		}
	}
	return nil
}

func taskStatus(s *writeStore, id string) any {
	if t := s.find(id); t != nil {
		return t["status"]
	}
	return nil
}

// TestUndoRecordsBeforeAndAfter: every standard change entry records, per
// thing, the changed props before and after; a create has no before, a
// delete no after. Fetches record nothing.
func TestUndoRecordsBeforeAndAfter(t *testing.T) {
	r, _ := newUndoRuntime(t, true, true)
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "status", "value": "done"}})
	if got, want := lastChanges(r), []ThingChange{{Noun: "task", Key: "t1", Before: map[string]any{"status": "todo"}, After: map[string]any{"status": "done"}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("set changes %+v", got)
	}
	mustRun(t, r, Request{Noun: "task", Prop: "estimate", Value: 5, Args: Arguments{"id": "t2"}})
	if got, want := lastChanges(r), []ThingChange{{Noun: "task", Key: "t2", Before: map[string]any{"estimate": 3}, After: map[string]any{"estimate": 5}}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("prop row changes %+v", got)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"title": "Buy milk"}})
	if got := lastChanges(r); len(got) != 1 || got[0].Key != "t4" || got[0].Before != nil || got[0].After["title"] != "Buy milk" {
		t.Fatalf("create changes %+v", got)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "delete", Confirmed: true, Args: Arguments{"id": "t2"}})
	if got := lastChanges(r); len(got) != 1 || got[0].Key != "t2" || got[0].After != nil || got[0].Before["title"] != "Write notes" || got[0].Before["estimate"] != 5 {
		t.Fatalf("delete changes %+v", got)
	}
	before := len(r.History(0))
	mustRun(t, r, Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1"}})
	if e := r.History(0)[before]; len(e.Changes) != 0 {
		t.Fatalf("a fetch recorded changes %+v", e.Changes)
	}
}

// TestUndoChangesPersistOnlyWhenSimple: before and after reach the history
// file by the simple-value test (owner decision 12); a line kept for its
// own sake (a simple create, a delete) leaves out values that are not.
func TestUndoChangesPersistOnlyWhenSimple(t *testing.T) {
	r, _ := newUndoRuntime(t, true, true)
	entry := func() HistoryEntry {
		h := r.History(0)
		return h[len(h)-1]
	}
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "changes": map[string]any{"status": "done", "estimate": 4}}})
	if e, keep := r.persistable(entry()); !keep || len(e.Changes) != 1 || e.Changes[0].Before["estimate"] != 2 {
		t.Fatalf("simple set: keep %v changes %+v", keep, e.Changes)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "title", "value": "x"}})
	if len(entry().Changes) != 1 {
		t.Fatal("a text write records its change in memory")
	}
	if _, keep := r.persistable(entry()); keep {
		t.Fatal("a text write reached the file")
	}
	mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"status": "todo"}})
	if e, keep := r.persistable(entry()); !keep || len(e.Changes) != 1 {
		t.Fatalf("simple create: keep %v changes %+v (its new thing holds simple values only)", keep, e.Changes)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "delete", Confirmed: true, Args: Arguments{"id": "t2"}})
	if e, keep := r.persistable(entry()); !keep || e.Changes != nil {
		t.Fatalf("delete: keep %v changes %+v", keep, e.Changes)
	}
}

// TestUndoInverses: history.undo writes a set's before back, deletes what
// a create made, creates again under its old key what a delete removed,
// and walks back one change per run.
func TestUndoInverses(t *testing.T) {
	r, s := newUndoRuntime(t, true, true)
	undo := Request{Noun: "ochazuke.history", Verb: "undo"}
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "status", "value": "doing"}})
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "status", "value": "done"}})
	mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"title": "Buy milk"}})
	mustRun(t, r, Request{Noun: "task", Verb: "delete", Confirmed: true, Args: Arguments{"id": "t3"}})
	s.log = nil

	mustRun(t, r, undo)
	if s.find("t3") == nil || s.find("t3")["estimate"] != 1 || s.find("t3")["summary"] != "Call the plumber" {
		t.Fatalf("delete undone: %v", s.find("t3"))
	}
	mustRun(t, r, undo)
	if s.find("t4") != nil {
		t.Fatal("create undone: t4 is still there")
	}
	mustRun(t, r, undo)
	if got := taskStatus(s, "t1"); got != "doing" {
		t.Fatalf("second set undone: status %v", got)
	}
	mustRun(t, r, undo)
	if got := taskStatus(s, "t1"); got != "todo" {
		t.Fatalf("first set undone: status %v", got)
	}
	if want := []string{"create t3", "delete t4", "set t1 status=doing", "set t1 status=todo"}; !slices.Equal(s.log, want) {
		t.Fatalf("log %q", s.log)
	}
	// Nothing left: a plain answer, never an error; and undo's own
	// entries are not changes to undo.
	if v, err := r.Execute(undo); err != nil || v != nil {
		t.Fatalf("nothing to undo: %v %v", v, err)
	}
	if !r.Available(undo, "app", r.Mode) {
		t.Fatal("undo with nothing left is unavailable")
	}
	// A toggle records the value it flipped from.
	mustRun(t, r, Request{Noun: "task", Prop: "starred", Toggle: true, Args: Arguments{"id": "t1"}})
	mustRun(t, r, undo)
	if v, ok := s.find("t1")["starred"]; ok && v != nil && v != false {
		t.Fatalf("toggle undone: starred %v", v)
	}
}

// TestUndoVisualWriteIsOneGroup: a set over a visual selection is one
// entry whose changes undo together, in one run.
func TestUndoVisualWriteIsOneGroup(t *testing.T) {
	r, s := newUndoRuntime(t, true, true)
	if err := r.SetFocus("row-t1"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "set", Subjects: []string{"row-t1", "row-t2", "row-t3"}, Args: Arguments{"prop": "status", "value": "done"}})
	if got := lastChanges(r); len(got) != 3 {
		t.Fatalf("visual set changes %+v", got)
	}
	s.log = nil
	stop := r.UI().capture()
	v := mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo"})
	msgs := stop()
	if want := []string{"set t3 status=todo", "set t2 status=doing", "set t1 status=todo"}; !slices.Equal(s.log, want) {
		t.Fatalf("log %q", s.log)
	}
	if things := v.(map[string]any)["things"]; !reflect.DeepEqual(things, []string{"t1", "t2", "t3"}) {
		t.Fatalf("undo answered %v", v)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "t1, t2, t3") {
		t.Fatalf("notices %+v", msgs)
	}
}

// TestUndoUnavailableWithoutStoreUndo: history.undo is unavailable when
// the last standard change is on a store without Undo, and when a delete
// cannot be created again under its key (no AcceptsKey); it never skips
// that change to undo an older one.
func TestUndoUnavailableWithoutStoreUndo(t *testing.T) {
	undo := Request{Noun: "ochazuke.history", Verb: "undo"}
	r, s := newUndoRuntime(t, false, true)
	reads := len(s.reads)
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "status", "value": "done"}})
	// Not read around the write (set's own read back only), and marked
	// by noun and key alone.
	if got := len(s.reads) - reads; got != 1 {
		t.Fatalf("a store without Undo was read %d times for one set", got)
	}
	if got := lastChanges(r); !reflect.DeepEqual(got, []ThingChange{{Noun: "task", Key: "t1"}}) {
		t.Fatalf("mark %+v", got)
	}
	if e, _ := r.persistable(r.History(0)[len(r.History(0))-1]); e.Changes != nil {
		t.Fatalf("a mark reached the file: %+v", e.Changes)
	}
	if r.Available(undo, "app", r.Mode) {
		t.Fatal("undo available on a store without Undo")
	}
	if _, err := r.Execute(undo); err == nil || !strings.Contains(err.Error(), "not applicable") {
		t.Fatalf("undo ran: %v", err)
	}
	if got := taskStatus(s, "t1"); got != "done" {
		t.Fatalf("status %v", got)
	}

	r, s = newUndoRuntime(t, true, false)
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "status", "value": "done"}})
	mustRun(t, r, Request{Noun: "task", Verb: "delete", Confirmed: true, Args: Arguments{"id": "t2"}})
	if r.Available(undo, "app", r.Mode) {
		t.Fatal("undo of a delete available without AcceptsKey")
	}
	if got := taskStatus(s, "t1"); got != "done" {
		t.Fatalf("an older change was undone: status %v", got)
	}
}

// TestUndoSkipsRestoredEntries: an entry an earlier session ran (read from
// the history file) is never undone, as . never repeats it.
func TestUndoSkipsRestoredEntries(t *testing.T) {
	r, s := newUndoRuntime(t, true, true)
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "status", "value": "done"}})
	for i := range r.history.entries {
		r.history.entries[i].Restored = true
	}
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo"})
	if got := taskStatus(s, "t1"); got != "done" {
		t.Fatalf("a restored change was undone: status %v", got)
	}
}

// TestUndoUnavailableWithoutAnyUndoStore: in an application where no store
// declares Undo, history.undo is never offered.
func TestUndoUnavailableWithoutAnyUndoStore(t *testing.T) {
	r, _ := newWriteRuntime(t)
	if r.Available(Request{Noun: "ochazuke.history", Verb: "undo"}, "app", r.Mode) {
		t.Fatal("undo offered with no store that undoes")
	}
}

// A write whose store changes more props than the request wrote (mtm's
// move: the project written, the parent the move cleared) records every
// writable prop that changed, so undo restores them all; a read-only prop
// that changed (a summary the store derives) is not written back.
func TestUndoRestoresPropsTheStoreChangedToo(t *testing.T) {
	r, s := newUndoRuntime(t, true, true)
	st := *r.Nouns["task"].Store
	patch := st.Patch
	st.Patch = func(c Ctx, id string, changes Thing) (Thing, error) {
		if _, ok := changes["status"]; ok {
			if t := s.find(id); t != nil {
				t["estimate"] = 0 // a side effect: the status write clears the estimate
				t["summary"] = "derived"
			}
		}
		return patch(c, id, changes)
	}
	r.Define("task").Store(st)
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "status", "value": "done"}})
	want := []ThingChange{{Noun: "task", Key: "t1", Before: map[string]any{"status": "todo", "estimate": 2}, After: map[string]any{"status": "done", "estimate": 0}}}
	if got := lastChanges(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("changes %+v, want %+v", got, want)
	}
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo"})
	if th := s.find("t1"); th["status"] != "todo" || th["estimate"] != 2 {
		t.Fatalf("undo left %v", th)
	}
}
