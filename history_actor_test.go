package ochazuke

import (
	"strings"
	"testing"
)

func actorSet(t *testing.T, r *Runtime, actor Actor, id, status string) uint64 {
	t.Helper()
	mustRun(t, r, Request{Noun: "task", Verb: "set", Actor: &actor, Args: Arguments{"id": id, "prop": "status", "value": status}})
	return lastEntry(t, r).Seq
}

func TestActorUndoEntryScope(t *testing.T) {
	r, s := newUndoRuntime(t, true, true)
	a, b := testAgent("a"), testAgent("b")
	own := actorSet(t, r, a, "t1", "done")
	owner := actorSet(t, r, Owner(), "t2", "done")
	other := actorSet(t, r, b, "t2", "todo")
	for _, seq := range []uint64{owner, other} {
		_, err := r.Execute(Request{Noun: "ochazuke.history", Verb: "undo", Actor: &a, Args: Arguments{"entry": int(seq)}})
		if err == nil || !strings.Contains(err.Error(), "only its own") {
			t.Fatalf("undo %d: %v", seq, err)
		}
	}
	changedSession := a
	changedSession.Session = "other"
	if _, err := r.Execute(Request{Noun: "ochazuke.history", Verb: "undo", Actor: &changedSession, Args: Arguments{"entry": int(own)}}); err == nil {
		t.Fatal("another session claimed entry")
	}
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo", Actor: &a, Args: Arguments{"entry": int(own)}})
	if taskStatus(s, "t1") != "todo" || taskStatus(s, "t2") != "todo" {
		t.Fatal("undo affected another entry")
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.history", Verb: "undo", Actor: &a, Args: Arguments{"entry": int(own)}}); err == nil {
		t.Fatal("undid entry twice")
	}
}

func TestActorUndoDefaultSkipsOtherActors(t *testing.T) {
	r, s := newUndoRuntime(t, true, true)
	a := testAgent("a")
	actorSet(t, r, a, "t1", "done")
	actorSet(t, r, Owner(), "t2", "done")
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo", Actor: &a})
	if taskStatus(s, "t1") != "todo" || taskStatus(s, "t2") != "done" {
		t.Fatal("default agent undo touched owner")
	}
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo"})
	if taskStatus(s, "t2") != "doing" {
		t.Fatal("owner default undo changed")
	}
}

func TestActorUndoRefusesLaterEdits(t *testing.T) {
	r, s := newUndoRuntime(t, true, true)
	a := testAgent("a")
	seq := actorSet(t, r, a, "t1", "done")
	actorSet(t, r, Owner(), "t1", "todo")
	for _, args := range []Arguments{nil, {"entry": int(seq)}} {
		if _, err := r.Execute(Request{Noun: "ochazuke.history", Verb: "undo", Actor: &a, Args: args}); err == nil || !strings.Contains(err.Error(), "later work") {
			t.Fatalf("stale undo: %v", err)
		}
	}
	if taskStatus(s, "t1") != "todo" {
		t.Fatal("overwrote later owner edit")
	}
}

func TestActorUndoTargetValidationAndOwner(t *testing.T) {
	r, s := newUndoRuntime(t, true, true)
	a := testAgent("a")
	seq := actorSet(t, r, a, "t1", "done")
	actorSet(t, r, Owner(), "t2", "done")
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo", Args: Arguments{"entry": int(seq)}})
	if taskStatus(s, "t1") != "todo" || taskStatus(s, "t2") != "done" {
		t.Fatal("owner target wrong entry")
	}
	for _, entry := range []int{0, -1, 10000} {
		if _, err := r.Execute(Request{Noun: "ochazuke.history", Verb: "undo", Args: Arguments{"entry": entry}}); err == nil {
			t.Fatalf("accepted entry %d", entry)
		}
	}
	r.history.entries[0].Restored = true
	if _, err := r.Execute(Request{Noun: "ochazuke.history", Verb: "undo", Args: Arguments{"entry": int(seq)}}); err == nil || !strings.Contains(err.Error(), "earlier session") {
		t.Fatalf("restored entry: %v", err)
	}
}

func TestActorUndoCreateDeleteAndGuard(t *testing.T) {
	r, s := newUndoRuntime(t, true, true)
	a := testAgent("a")
	created, err := r.Execute(Request{Noun: "task", Verb: "create", Actor: &a, Args: Arguments{"title": "agent task"}})
	if err != nil {
		t.Fatal(err)
	}
	key := answerKey(r.Noun("task"), created)
	seq := lastEntry(t, r).Seq
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo", Actor: &a, Args: Arguments{"entry": int(seq)}})
	if s.find(key) != nil {
		t.Fatal("create not undone")
	}
	mustRun(t, r, Request{Noun: "task", Verb: "delete", Actor: &a, Confirmed: true, Args: Arguments{"id": "t1"}})
	seq = lastEntry(t, r).Seq
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo", Actor: &a, Args: Arguments{"entry": int(seq)}})
	if s.find("t1") == nil {
		t.Fatal("delete not undone")
	}
	// A newer non-undoable entry must not hide an explicitly targeted older one.
	own := actorSet(t, r, a, "t1", "done")
	r.history.entries = append(r.history.entries, HistoryEntry{Seq: r.history.seq + 1, Result: HistoryResult{OK: true}, Context: HistoryContext{Actor: Owner()}, Changes: []ThingChange{{Noun: "missing"}}})
	r.history.seq++
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo", Actor: &a, Args: Arguments{"entry": int(own)}})
	if taskStatus(s, "t1") != "todo" {
		t.Fatal("newest guard hid explicit target")
	}
}

func TestActorRepeatRunsAsCaller(t *testing.T) {
	r, _ := newUndoRuntime(t, true, true)
	a := testAgent("a")
	actorSet(t, r, a, "t1", "done")
	// Explicit owner repeat must not inherit an actor stored on the original API call.
	q, err := r.repeatRequest("app", 0)
	if err != nil {
		t.Fatal(err)
	}
	if q.Actor != nil {
		t.Fatal("replay retained origin actor")
	}
	mustRun(t, r, q)
	if lastEntry(t, r).Context.Actor != Owner() {
		t.Fatal("repeat impersonated previous agent")
	}
}

func TestActorUndoCreatePreservesLaterProps(t *testing.T) {
	r, s := newUndoRuntime(t, true, true)
	a := testAgent("a")
	created, err := r.Execute(Request{Noun: "task", Verb: "create", Actor: &a, Args: Arguments{"title": "agent task"}})
	if err != nil {
		t.Fatal(err)
	}
	key := answerKey(r.Noun("task"), created)
	seq := lastEntry(t, r).Seq
	// A property absent at creation was added later by the owner.
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": key, "prop": "estimate", "value": 7}})
	if _, err := r.Execute(Request{Noun: "ochazuke.history", Verb: "undo", Actor: &a, Args: Arguments{"entry": int(seq)}}); err == nil || !strings.Contains(err.Error(), "later work") {
		t.Fatalf("undo would delete the owner's new prop: %v", err)
	}
	if s.find(key) == nil || s.find(key)["estimate"] != 7 {
		t.Fatal("deleted the owner's new prop")
	}
}
