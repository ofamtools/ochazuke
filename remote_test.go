package ochazuke

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// remoteBox is a remote store over a map: every call is pending work the
// test's pump runs, as a mounted extension's CLI would be.
type remoteBox struct {
	mu    sync.Mutex
	items map[string]Thing
	calls []string
	hold  func(op string) // runs off the loop before a call answers
}

func (b *remoteBox) call(c Ctx, op, key string, write bool, run func() (any, error)) (any, error) {
	call := StoreCall{Store: "box", Op: op, Key: key, Write: write}
	return c.Await(call, func() Result {
		return Pending(func(context.Context) (any, error) {
			if b.hold != nil {
				b.hold(op)
			}
			b.mu.Lock()
			defer b.mu.Unlock()
			b.calls = append(b.calls, op+" "+key)
			return run()
		})
	}, time.Second)
}

func (b *remoteBox) store() Store {
	thing := func(v any, err error) (Thing, error) {
		t, _ := v.(Thing)
		return t, err
	}
	return Store{Kind: Collection, Remote: true, Stamps: true, Undo: true,
		Get: func(c Ctx, id string) (Thing, error) {
			return thing(b.call(c, "get", id, false, func() (any, error) {
				if t, ok := b.items[id]; ok {
					return maps.Clone(t), nil
				}
				return nil, fmt.Errorf("no box %q", id)
			}))
		},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) {
			v, err := b.call(c, "list", "", false, func() (any, error) {
				var out []Thing
				for _, t := range b.items {
					out = append(out, maps.Clone(t))
				}
				return out, nil
			})
			things, _ := v.([]Thing)
			return things, err
		},
		Patch: func(c Ctx, id string, ch Thing) (Thing, error) {
			return thing(b.call(c, "set", id, true, func() (any, error) {
				maps.Copy(b.items[id], ch)
				return maps.Clone(b.items[id]), nil
			}))
		},
		Delete: func(c Ctx, id string) error {
			_, err := b.call(c, "delete", id, true, func() (any, error) { delete(b.items, id); return nil, nil })
			return err
		},
	}
}

func remoteFixture(t *testing.T) (*Runtime, *remoteBox, *RecordingSink) {
	t.Helper()
	b := &remoteBox{items: map[string]Thing{
		"a": {"id": "a", "title": "A", "on": false},
		"b": {"id": "b", "title": "B", "on": false},
	}}
	r := NewRuntime()
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	r.Define("box").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "on", Parameter: Parameter{Type: "boolean"}},
	).Store(b.store())
	return r, b, sink
}

// pumpRemote delivers the runtime's commands until none is left.
func pumpRemote(t *testing.T, r *Runtime) {
	t.Helper()
	for range 200 {
		before := len(r.commands)
		settleAll(t, r)
		if before == 0 && len(r.commands) == 0 && !hasRunning(r) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("remote jobs never ended")
}

func hasRunning(r *Runtime) bool {
	for _, j := range r.Jobs() {
		if j.State == JobRunning {
			return true
		}
	}
	return false
}

func set(t *testing.T, r *Runtime, id string, changes map[string]any) {
	t.Helper()
	if _, err := r.Execute(Request{Noun: "box", Verb: "set", Args: Arguments{"id": id, "props": changes}}); err != nil {
		t.Fatal(err)
	}
}

// Toggles started before any ends both apply, in order: two quick toggles
// of one flag leave it as it was.
func TestRemoteTogglesQueuePerThing(t *testing.T) {
	r, b, _ := remoteFixture(t)
	for range 2 {
		if _, err := r.Execute(Request{Noun: "box", Verb: "set", Args: Arguments{"id": "a", "prop": "on", "toggle": true}}); err != nil {
			t.Fatal(err)
		}
	}
	pumpRemote(t, r)
	if b.items["a"]["on"] != false {
		t.Fatalf("two toggles: on = %v", b.items["a"]["on"])
	}
	sets := 0
	for _, c := range b.calls {
		if strings.HasPrefix(c, "set") {
			sets++
		}
	}
	if sets != 2 {
		t.Fatalf("both toggles write: %v", b.calls)
	}
}

// A change a remote write makes is undone by a job too, and the undo is one
// step: the write, then its inverse.
func TestRemoteUndoRunsAsAJob(t *testing.T) {
	r, b, _ := remoteFixture(t)
	set(t, r, "a", map[string]any{"title": "Changed"})
	pumpRemote(t, r)
	if b.items["a"]["title"] != "Changed" {
		t.Fatalf("set: %v", b.items["a"])
	}
	v, err := r.Execute(Request{Noun: "ochazuke.history", Verb: "undo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(Ticket); !ok {
		t.Fatalf("undo over a remote store answers its job, not %v", v)
	}
	pumpRemote(t, r)
	if b.items["a"]["title"] != "A" {
		t.Fatalf("undo: %v", b.items["a"])
	}
}

// A run over several subjects makes the calls for each in turn, one job, and
// posts one notice.
func TestRemoteSeveralSubjects(t *testing.T) {
	r, b, sink := remoteFixture(t)
	r.Instance("ra", Kind("box")).Data = map[string]any{"id": "a"}
	r.Instance("rb", Kind("box")).Data = map[string]any{"id": "b"}
	if _, err := r.Execute(Request{Noun: "box", Verb: "set", Target: "ra", Subjects: []string{"ra", "rb"}, Args: Arguments{"props": map[string]any{"title": "X"}}}); err != nil {
		t.Fatal(err)
	}
	pumpRemote(t, r)
	if b.items["a"]["title"] != "X" || b.items["b"]["title"] != "X" {
		t.Fatalf("both written: %v %v", b.items["a"], b.items["b"])
	}
	infos := 0
	for _, m := range sink.Messages() {
		if strings.Contains(m.Text, "done on 2 rows") {
			infos++
		}
	}
	if infos != 1 {
		t.Fatalf("one notice for the run, got %d: %+v", infos, sink.Messages())
	}
}

// Cancelling a verb between its calls writes nothing more and frees the
// thing for the next write.
func TestRemoteCancelMidSequence(t *testing.T) {
	r, b, _ := remoteFixture(t)
	set(t, r, "a", map[string]any{"title": "Lost"})
	// Run only the first call (the read before the write), then cancel.
	settleOnce(t, r)
	var job uint64
	for _, j := range r.Jobs() {
		if j.State == JobRunning {
			job = j.Job
		}
	}
	if job == 0 {
		t.Fatal("the set ended after one call")
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.jobs", Verb: "cancel", Args: Arguments{"job": int(job)}}); err != nil {
		t.Fatal(err)
	}
	pumpRemote(t, r)
	if b.items["a"]["title"] != "A" {
		t.Fatalf("cancelled write landed: %v", b.items["a"])
	}
	set(t, r, "a", map[string]any{"title": "Next"})
	pumpRemote(t, r)
	if b.items["a"]["title"] != "Next" {
		t.Fatalf("the next write is queued behind a cancelled one: %v", b.items["a"])
	}
}

// settleOnce delivers the messages of one round of commands.
func settleOnce(t *testing.T, r *Runtime) {
	t.Helper()
	var run func(cmd tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				run(c)
			}
			return
		}
		if _, err := r.Deliver(msg); err != nil {
			t.Fatal(err)
		}
	}
	run(r.PendingCommands())
}

// A run that makes a different call than it made before fails the job and
// says so.
func TestRemoteReplayMismatch(t *testing.T) {
	r, _, _ := remoteFixture(t)
	runs := 0
	body := func(c Ctx) (any, error) {
		runs++
		op := "get"
		if runs > 1 {
			op = "list" // the second run asks for something else
		}
		if _, err := c.Await(StoreCall{Store: "box", Op: op}, func() Result {
			return Pending(func(context.Context) (any, error) { return nil, nil })
		}, time.Second); err != nil {
			return nil, err
		}
		_, err := c.Await(StoreCall{Store: "box", Op: "again"}, func() Result {
			return Pending(func(context.Context) (any, error) { return nil, nil })
		}, time.Second)
		return nil, err
	}
	c := Ctx{actor: r.Actor(), Runtime: r, Context: r.Context(), Instance: r.Instances["app"]}
	res := r.runSteppedBody(c, "box.test", body, func(v any, err error) (any, error) { return v, err })
	if res.work == nil {
		t.Fatalf("a call is needed: %+v", res)
	}
	more, value, err := res.cont(nil, nil)
	if more != nil || err == nil || !strings.Contains(err.Error(), "changed between runs") {
		t.Fatalf("replay mismatch: %v %v %v", more != nil, value, err)
	}
}

// A notice a run posts waits for the run that finishes: it is posted once
// however many runs the calls took.
func TestRemoteNoticeOnce(t *testing.T) {
	r, _, sink := remoteFixture(t)
	body := func(c Ctx) (any, error) {
		c.Notify("planning")
		for _, op := range []string{"one", "two", "three"} {
			if _, err := c.Await(StoreCall{Store: "box", Op: op}, func() Result {
				return Pending(func(context.Context) (any, error) { return nil, nil })
			}, time.Second); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	c := Ctx{actor: r.Actor(), Runtime: r, Context: r.Context(), Instance: r.Instances["app"]}
	res := r.runSteppedBody(c, "box.test", body, nil)
	if _, err := r.Launch("app", "box.test", res, c); err != nil {
		t.Fatal(err)
	}
	pumpRemote(t, r)
	n := 0
	for _, m := range sink.Messages() {
		if m.Text == "planning" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("notice posted %d times: %+v", n, sink.Messages())
	}
}

// The loop never calls a remote store outside a plan: a read answers the
// last known answer or ErrNotLoaded, a write fails.
func TestRemoteReadsOutsideAPlanNeverCall(t *testing.T) {
	r, b, _ := remoteFixture(t)
	n := r.Nouns["box"]
	c := Ctx{actor: r.Actor(), Runtime: r, Context: r.Context()}
	if _, err := n.Store.Get(c, "a"); err == nil || !strings.Contains(err.Error(), "not loaded") {
		t.Fatalf("unknown read: %v", err)
	}
	if err := n.Store.Delete(c, "a"); err == nil {
		t.Fatal("a write outside a plan ran")
	}
	if _, err := r.Execute(Request{Noun: "box", Verb: "get", Args: Arguments{"id": "a"}}); err != nil {
		t.Fatal(err)
	}
	pumpRemote(t, r)
	if got, err := n.Store.Get(c, "a"); err != nil || got["title"] != "A" {
		t.Fatalf("known read: %v %v", got, err)
	}
	if len(b.calls) == 0 {
		t.Fatal("the get never called the source")
	}
}

// A list fed by a remote store fills as a job and fills again as one after
// a change: the loop never waits for the source.
func TestRemoteFromListFillsAsAJob(t *testing.T) {
	r, b, _ := remoteFixture(t)
	l, err := r.AddList("boxes", nil, From("box"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 0 || len(l.Items()) != 0 {
		t.Fatalf("filled on the loop: calls %v rows %d", b.calls, len(l.Items()))
	}
	pumpRemote(t, r)
	if len(l.Items()) != 2 {
		t.Fatalf("filled by a job: %d rows (calls %v)", len(l.Items()), b.calls)
	}
	set(t, r, "a", map[string]any{"title": "Renamed"})
	pumpRemote(t, r)
	found := false
	for _, it := range l.Items() {
		if it.Label == "Renamed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the list did not fill again after the change: %+v", l.Items())
	}
}
