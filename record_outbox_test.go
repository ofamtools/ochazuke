package ochazuke

import (
	"slices"
	"testing"
)

func TestRecordOutboxQueueIsASet(t *testing.T) {
	o := newRecordOutbox(Record{}, "app")
	o.queue("a", "b", "a")
	o.queue("b", "c")
	if want := []string{"a", "b", "c"}; !slices.Equal(o.dirty, want) {
		t.Fatalf("dirty %v, want %v", o.dirty, want)
	}
	o.queueAllIfIdle([]string{"x"})
	if len(o.dirty) != 3 {
		t.Fatalf("queueAllIfIdle queued on a busy outbox: %v", o.dirty)
	}
}

func TestRecordOutboxNextWholeRecordAndPerField(t *testing.T) {
	o := newRecordOutbox(Record{}, "app")
	o.queue("c", "a", "b")
	order := []string{"a", "b", "c", "d"}
	if got := o.next(order, false, nil); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("whole-record next %v, want form order", got)
	}
	failed := map[string]bool{"a": true}
	retry := func(n string) bool { return !failed[n] }
	if got := o.next(order, true, retry); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("per-field next %v, want the first retryable (b)", got)
	}
	if len(o.dirty) != 3 {
		t.Fatalf("next changed the queue: %v", o.dirty)
	}
	failed["b"], failed["c"] = true, true
	if got := o.next(order, true, retry); len(got) != 0 {
		t.Fatalf("next %v with every field failed", got)
	}
}

func TestRecordOutboxTakeStageSettle(t *testing.T) {
	o := newRecordOutbox(Record{}, "app")
	o.queue("a", "b")
	o.take([]string{"a"})
	o.launch([]string{"a"})
	if !o.inflight || !o.owes("a") || !o.owes("b") || o.owes("z") || o.idle() {
		t.Fatalf("after launch: %+v", o)
	}
	o.queue("a") // edited again while in flight
	o.settle()
	if o.inflight || o.sending != nil || !slices.Equal(o.dirty, []string{"b", "a"}) {
		t.Fatalf("after settle: %+v", o)
	}
}

func TestRecordOutboxPending(t *testing.T) {
	o := newRecordOutbox(Record{}, "app")
	o.queue("a", "b")
	o.take([]string{"b"})
	o.stage([]string{"b"})
	if !o.pending("a", false) || o.pending("a", true) {
		t.Fatalf("queued field: pending(false)=%v pending(failed)=%v", o.pending("a", false), o.pending("a", true))
	}
	if !o.pending("b", true) || o.pending("c", false) {
		t.Fatalf("sending field must stay pending even when marked failed")
	}
}

func TestRecordOutboxAbandonRequeuesAndKeepsReason(t *testing.T) {
	o := newRecordOutbox(Record{}, "app")
	o.launch([]string{"a"})
	o.abandon("gone", []string{"a", "b"})
	if o.inflight || o.sending != nil || o.err != "gone" || !slices.Equal(o.dirty, []string{"a", "b"}) {
		t.Fatalf("after abandon: %+v", o)
	}
}

func TestRecordOutboxLoadFollowsOnlyWhatIsNotOwed(t *testing.T) {
	o := newRecordOutbox(Record{"a": "1", "b": "1", "c": "1"}, "app")
	for _, n := range []string{"a", "b", "c"} {
		o.set(n, "1")
	}
	o.queue("a")
	o.launch([]string{"b"})
	if o.load("a", "2") || o.load("b", "2") {
		t.Fatal("a load overwrote a field with a write queued or in flight")
	}
	if !o.load("c", "2") {
		t.Fatal("a load did not reach an untouched field")
	}
	if o.baseline["a"] != "2" || o.baseline["b"] != "2" {
		t.Fatalf("baseline not updated: %v", o.baseline)
	}
	o.set("c", "2")
	if o.load("c", "2") {
		t.Fatal("a load equal to the written value must not follow")
	}
}

func TestRecordOutboxChangedSince(t *testing.T) {
	o := newRecordOutbox(Record{"a": "1", "b": "2"}, "app")
	got := o.changedSince(Record{"a": "1", "b": "3", "c": "x"}, []string{"a", "b", "c"})
	if !slices.Equal(got, []string{"b", "c"}) {
		t.Fatalf("changedSince %v", got)
	}
}

func TestRecordOutboxRecordIsACopy(t *testing.T) {
	o := newRecordOutbox(Record{}, "app")
	o.set("a", "1")
	r := o.record()
	r["a"] = "2"
	if o.value("a") != "1" {
		t.Fatal("record() aliased the written map")
	}
}
