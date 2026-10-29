package ochazuke

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// watchRecorder collects a subscription's events.
type watchRecorder struct{ events []WatchEvent }

func (w *watchRecorder) add(e WatchEvent) { w.events = append(w.events, e) }

// take answers the events so far as "type id" words and forgets them.
func (w *watchRecorder) take() []string {
	out := make([]string, len(w.events))
	for i, e := range w.events {
		out[i] = e.Type + " " + e.ID
	}
	w.events = nil
	return out
}

func wantEvents(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(want) == 0 {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events %q, want %q", got, want)
	}
}

// countLists wraps the store's List to count its calls.
func countLists(r *Runtime, noun string) *int {
	n := 0
	s := r.Nouns[noun].Store
	list := s.List
	s.List = func(c Ctx, q ListQuery) ([]Thing, error) { n++; return list(c, q) }
	return &n
}

// TestWatchFallbackEmitsTheThreeEventTypes: a store with no Watch handler
// is watched through its List: every thing first as added, then, each time
// the runtime is told, what was modified, deleted or added, each event
// with the thing as get answers it (no secrets) and the list hash after.
func TestWatchFallbackEmitsTheThreeEventTypes(t *testing.T) {
	r, s := newTaskRuntime(t)
	var rec watchRecorder
	w, err := r.Watch("task", nil, rec.add)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	first := append([]WatchEvent(nil), rec.events...)
	wantEvents(t, rec.take(), "added t1", "added t2", "added t3")
	if _, secret := first[0].Thing["token"]; secret || first[0].Thing["title"] != "Water the plants" {
		t.Fatalf("thing %v: want get's record, no secret", first[0].Thing)
	}
	if first[0].Hash == "" || first[0].Hash != w.Hash() {
		t.Fatalf("hash %q, watch at %q", first[0].Hash, w.Hash())
	}

	s.things[1] = Thing{"id": "t2", "title": "Write notes", "status": "done", "estimate": 3}
	r.Changed("task")
	got := append([]WatchEvent(nil), rec.events...)
	wantEvents(t, rec.take(), "modified t2")
	if got[0].Thing["status"] != "done" {
		t.Fatalf("modified thing %v", got[0].Thing)
	}

	s.things = s.things[:2]
	r.Changed("task")
	got = append([]WatchEvent(nil), rec.events...)
	wantEvents(t, rec.take(), "deleted t3")
	if got[0].Thing["summary"] != "Call the plumber" {
		t.Fatalf("a deleted thing carries its last record, got %v", got[0].Thing)
	}

	s.things = append(s.things, Thing{"id": "t4", "title": "New", "status": "todo"})
	r.Changed("task")
	wantEvents(t, rec.take(), "added t4")

	r.Changed("task") // nothing changed: nothing emitted
	r.Changed("other")
	wantEvents(t, rec.take())

	w.Stop()
	s.things = s.things[:1]
	r.Changed()
	wantEvents(t, rec.take())
}

// TestWatchSince: since resumes from an earlier event's hash, emitting what
// changed since; the current hash emits nothing; an unknown one is refused.
func TestWatchSince(t *testing.T) {
	r, s := newTaskRuntime(t)
	var rec watchRecorder
	w, err := r.Watch("task", nil, rec.add)
	if err != nil {
		t.Fatal(err)
	}
	since := rec.events[len(rec.events)-1].Hash
	w.Stop()

	again, err := r.Watch("task", Arguments{"since": since}, rec.add)
	if err != nil {
		t.Fatal(err)
	}
	again.Stop()
	wantEvents(t, rec.take()[3:])

	s.things[0] = Thing{"id": "t1", "title": "Water the cactus", "status": "todo", "estimate": 2}
	s.things = append(s.things[:1], s.things[2:]...)
	s.things = append(s.things, Thing{"id": "t9", "title": "Later", "status": "todo"})
	resumed, err := r.Watch("task", Arguments{"since": since}, rec.add)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Stop()
	got := append([]WatchEvent(nil), rec.events...)
	wantEvents(t, rec.take(), "modified t1", "added t9", "deleted t2")
	if got[2].Thing != nil {
		t.Fatalf("a thing deleted before the watch began has no record, got %v", got[2].Thing)
	}

	_, err = r.Watch("task", Arguments{"since": strings.Repeat("0", 32)}, rec.add)
	var bad *ArgumentError
	if !errors.As(err, &bad) || !strings.Contains(err.Error(), "watch without since") {
		t.Fatalf("unknown since: %v", err)
	}
}

// TestWatchKeyAndWhere: the key watches one thing; where watches the
// things whose props match, and a thing that stops matching is deleted.
func TestWatchKeyAndWhere(t *testing.T) {
	r, s := newTaskRuntime(t)
	var one, todo watchRecorder
	a, err := r.Watch("task", Arguments{"id": "t2"}, one.add)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Stop()
	b, err := r.Watch("task", Arguments{"where": "status=todo"}, todo.add)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Stop()
	wantEvents(t, one.take(), "added t2")
	wantEvents(t, todo.take(), "added t1", "added t3")

	s.things[0] = Thing{"id": "t1", "title": "Water the plants", "status": "done", "estimate": 2}
	s.things[1] = Thing{"id": "t2", "title": "Write notes", "status": "todo", "estimate": 3}
	r.Changed("task")
	wantEvents(t, one.take(), "modified t2")
	wantEvents(t, todo.take(), "added t2", "deleted t1")

	if _, err := r.Watch("task", Arguments{"where": "colour=red"}, nil); err == nil {
		t.Fatal("where on an unknown prop is refused")
	}
}

// TestWatchFollowsChangeRequestsAndJobs: the runtime tells the watches
// itself after a top-level request of the change kind and after a change
// verb's job completes; a fetch (task list) tells nobody, and a watch
// schedules nothing and lists nothing until told: no polling.
func TestWatchFollowsChangeRequestsAndJobs(t *testing.T) {
	r, s := newTaskRuntime(t)
	lists := countLists(r, "task")
	r.Define("task").Verb("finish", func(c Ctx) Result {
		s.things[0] = Thing{"id": "t1", "title": "Water the plants", "status": "done", "estimate": 2}
		return Done(nil)
	}, "Finish t1").Verb("drop", func(c Ctx) Result {
		return Pending(func(context.Context) (any, error) { return nil, nil }).Then(func(Ctx, any, error) {
			s.things = s.things[1:]
		})
	}, "Drop t1 off the loop")
	var rec watchRecorder
	w, err := r.Watch("task", nil, rec.add)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	rec.take()
	if *lists != 1 || r.PendingCommands() != nil {
		t.Fatalf("a fallback watch lists once and schedules nothing: %d lists", *lists)
	}

	if _, err := r.Execute(Request{Noun: "task", Verb: "list"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "focus", Verb: "next"}); err != nil {
		t.Fatal(err)
	}
	if *lists != 2 { // task list's own call; no refresh
		t.Fatalf("a fetch or an action refreshed the watch: %d lists", *lists)
	}

	if _, err := r.Execute(Request{Noun: "task", Verb: "finish"}); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, rec.take(), "modified t1")

	if _, err := r.Execute(Request{Noun: "task", Verb: "drop"}); err != nil {
		t.Fatal(err)
	}
	wantEvents(t, rec.take()) // the change is the job's
	cmd := r.PendingCommands()
	if cmd == nil {
		t.Fatal("drop scheduled no work")
	}
	deliverAll(t, r, cmd)
	wantEvents(t, rec.take(), "deleted t1")
}

// deliverAll runs a command (a batch's each) and delivers its messages,
// with a deadline.
func deliverAll(t *testing.T, r *Runtime, cmd tea.Cmd) {
	t.Helper()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					deliverAll(t, r, c)
				}
			}
			return
		}
		if _, err := r.Deliver(msg); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the command did not finish")
	}
}

// TestWatchStoreHandler: a store's Watch answers the events itself,
// pushed from another goroutine; the runtime brings them to the loop, each
// thing read as get answers it and its id filled from the key prop, and
// Stop stops the store's watch and the waiting command.
func TestWatchStoreHandler(t *testing.T) {
	r := NewRuntime()
	var got WatchQuery
	var emit func(WatchEvent)
	stopped := 0
	r.Define("task").Props(taskTestProps()...).Store(Store{
		Watch: func(c Ctx, q WatchQuery, e func(WatchEvent)) (func(), error) {
			got, emit = q, e
			return func() { stopped++ }, nil
		},
	})
	if _, ok := r.Operations["task.watch"]; !ok {
		t.Fatal("a store with only Watch has task.watch")
	}
	var rec watchRecorder
	w, err := r.Watch("task", Arguments{"where": "status=todo", "since": "rev-7"}, rec.add)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, WatchQuery{Where: map[string]any{"status": "todo"}, Since: "rev-7"}) {
		t.Fatalf("query %+v", got)
	}
	go func() {
		emit(WatchEvent{Type: WatchAdded, Thing: Thing{"id": "t5", "title": "Pushed", "status": "todo", "token": "x"}, Hash: "rev-8"})
		emit(WatchEvent{Type: WatchDeleted, ID: "t1"})
	}()
	for len(rec.events) < 2 {
		cmd := r.PendingCommands()
		if cmd == nil {
			t.Fatal("a store watch waits through a command")
		}
		deliverAll(t, r, cmd)
	}
	if e := rec.events[0]; e.ID != "t5" || e.Thing["token"] != nil || e.Thing["title"] != "Pushed" || e.Hash != "rev-8" {
		t.Fatalf("pushed event %+v", e)
	}
	wantEvents(t, rec.take(), "added t5", "deleted t1")

	cmd := r.PendingCommands()
	w.Stop()
	if stopped != 1 {
		t.Fatalf("store stop ran %d times", stopped)
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if msg != nil {
			t.Fatalf("a stopped watch's command answers %v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a stopped watch's command still waits")
	}
	emit(WatchEvent{Type: WatchAdded, ID: "late"})
	w.Stop()
	if stopped != 1 || len(rec.events) != 0 {
		t.Fatal("after Stop nothing more arrives")
	}
}

// TestWatchVerb: watch is a standard verb of every kind whose store can be
// watched; its subject is optional and never from the focus; the verb
// answers the watch, which a caller encoding it gets as the events so far.
func TestWatchVerb(t *testing.T) {
	r, _ := newTaskRuntime(t)
	op := r.Operations["task.watch"]
	if op.Standard != "watch" || op.HistoryKindOf() != HistoryFetch || !reflect.DeepEqual(op.Positional, []string{"id"}) {
		t.Fatalf("task.watch = %+v", op)
	}
	if p := op.Parameters["id"]; p.Focus || p.Noun != "task" {
		t.Fatalf("watch's key %+v: optional, never from the focus", p)
	}
	for _, name := range []string{"where", "since"} {
		if _, ok := op.Parameters[name]; !ok {
			t.Fatalf("task.watch has no %s", name)
		}
	}
	value, err := r.Execute(Request{Noun: "task", Verb: "watch", Args: Arguments{"id": "t3"}})
	if err != nil {
		t.Fatal(err)
	}
	w, ok := value.(*Watch)
	if !ok {
		t.Fatalf("task watch answered %T", value)
	}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	var events []WatchEvent
	if err := json.Unmarshal(data, &events); err != nil || len(events) != 1 || events[0].ID != "t3" || events[0].Type != WatchAdded {
		t.Fatalf("encoded watch %s (%v)", data, err)
	}
	if !w.Stopped() {
		t.Fatal("an encoded watch ends")
	}

	// A collection with only Get has no watch; a singleton with Get does.
	r.Define("note").Props(PropSpec{Name: "id", Key: true}).Store(Store{Get: func(Ctx, string) (Thing, error) { return nil, nil }})
	if _, ok := r.Operations["note.watch"]; ok {
		t.Fatal("a collection with only Get has note.watch")
	}
	look := Thing{"theme": "dark"}
	r.Define("look").Props(PropSpec{Name: "theme"}).Store(Store{Kind: Singleton, Get: func(Ctx, string) (Thing, error) { return look, nil }})
	sop, ok := r.Operations["look.watch"]
	if !ok || len(sop.Positional) != 0 {
		t.Fatalf("look.watch = %+v, %v", sop, ok)
	}
	if _, ok := sop.Parameters["where"]; ok {
		t.Fatal("a singleton's watch takes no where")
	}
	var rec watchRecorder
	sw, err := r.Watch("look", nil, rec.add)
	if err != nil {
		t.Fatal(err)
	}
	defer sw.Stop()
	look = Thing{"theme": "light"}
	r.Changed("look")
	wantEvents(t, rec.take(), "added ", "modified ")
}

// TestWatchRefreshFailureEndsIt: a list that fails ends the watch with its
// error.
func TestWatchRefreshFailureEndsIt(t *testing.T) {
	r, _ := newTaskRuntime(t)
	w, err := r.Watch("task", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Nouns["task"].Store.List = func(Ctx, ListQuery) ([]Thing, error) { return nil, errors.New("disk gone") }
	r.Changed()
	if !w.Stopped() || w.Err() == nil || !strings.Contains(w.Err().Error(), "disk gone") {
		t.Fatalf("stopped %v, err %v", w.Stopped(), w.Err())
	}
}

// TestMemoryVersionsKeepTheNewest: the in-memory keeper keeps the newest
// versions per scope and the newest scopes.
func TestMemoryVersionsKeepTheNewest(t *testing.T) {
	m := newMemoryVersions()
	for i := range memoryVersionsKeep + 2 {
		_ = m.WriteVersion("s", string(rune('a'+i)), []string{"x"}, []string{"h"})
	}
	if _, _, ok := m.ReadVersion("s", "a"); ok {
		t.Fatal("the oldest version is kept")
	}
	if ids, _, ok := m.ReadVersion("s", string(rune('a'+memoryVersionsKeep+1))); !ok || ids[0] != "x" {
		t.Fatal("the newest version is gone")
	}
	for i := range memoryVersionsScopes + 1 {
		_ = m.WriteVersion(string(rune('A'+i)), "h", nil, nil)
	}
	if _, _, ok := m.ReadVersion("s", string(rune('a'+memoryVersionsKeep+1))); ok {
		t.Fatal("the oldest scope is kept")
	}
	if err := m.WriteVersion("s", "h", []string{"a"}, nil); err == nil {
		t.Fatal("a version needs a hash per id")
	}
}
