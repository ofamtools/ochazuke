package ochazuke

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// A prop with a Read is computed when asked for: get's default record and
// list's rows do not hold it, get {prop} and the prop-shaped request read
// it from the whole thing, and the store's thing is never changed.
func TestComputedPropReadOnlyWhenAsked(t *testing.T) {
	r := NewRuntime()
	s := newTaskStore()
	reads := 0
	r.Define("task").Props(append(taskTestProps(), PropSpec{
		Name: "slug", Parameter: Parameter{Description: "A slug of the title"},
		Read: func(_ Ctx, t Thing) (any, error) {
			reads++
			return fmt.Sprint(t["status"], "-", t["id"]), nil
		},
	})...).Store(s.store())

	got, err := r.Execute(Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(map[string]any)["slug"]; ok || reads != 0 {
		t.Fatalf("the default record holds the computed prop (%d reads): %v", reads, got)
	}
	if v, err := r.Execute(Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1", "prop": "slug"}}); err != nil || v != "todo-t1" {
		t.Fatalf("get prop = %v, %v", v, err)
	}
	if v, err := r.Execute(Request{Noun: "task", Prop: "slug", Args: Arguments{"id": "t2"}}); err != nil || v != "doing-t2" {
		t.Fatalf("prop-shaped read = %v, %v", v, err)
	}
	rec, err := r.Execute(Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1", "props": "title,slug"}})
	if err != nil || rec.(map[string]any)["slug"] != "todo-t1" || rec.(map[string]any)["title"] != "Water the plants" {
		t.Fatalf("get props = %v, %v", rec, err)
	}
	if _, ok := s.things[0]["slug"]; ok {
		t.Fatal("the store's thing was written")
	}
	if _, err := r.Execute(Request{Noun: "task", Prop: "slug", Value: "x", Args: Arguments{"id": "t1"}}); err == nil {
		t.Fatal("a computed prop was written")
	}
	rows, err := r.Execute(Request{Noun: "task", Verb: "list"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range AnswerRows(rows).([]map[string]any) {
		if _, ok := row["slug"]; ok {
			t.Fatalf("a list row holds the computed prop: %v", row)
		}
	}
	if _, err := r.Execute(Request{Noun: "task", Verb: "list", Args: Arguments{"props": "slug"}}); err == nil {
		t.Fatal("list asked for a computed prop")
	}
	if p, _ := r.PropOf("task", "slug"); !p.Computed || !p.ReadOnly || !p.NoFilter {
		t.Fatalf("the declared prop = %+v", p)
	}
}

func TestComputedPropErrorNamesTheProp(t *testing.T) {
	r := NewRuntime()
	s := newTaskStore()
	r.Define("task").Props(append(taskTestProps(), PropSpec{Name: "slug", Read: func(Ctx, Thing) (any, error) { return nil, errors.New("no slug") }})...).Store(s.store())
	_, err := r.Execute(Request{Noun: "task", Prop: "slug", Args: Arguments{"id": "t1"}})
	if err == nil || err.Error() != "task.slug: no slug" {
		t.Fatalf("error = %v", err)
	}
}

// A collection prop describes all of a noun's things: read without a
// subject, from Read(nil thing), and kept out of every record.
func TestCollectionProps(t *testing.T) {
	r := NewRuntime()
	s := newTaskStore()
	r.Define("task").Props(append(taskTestProps(),
		CountProp(),
		NamesProp(),
		PropSpec{Name: "kinds", Subject: SubjectCollection, Read: func(c Ctx, t Thing) (any, error) {
			if t != nil {
				return nil, errors.New("a collection prop is read with no thing")
			}
			return []string{"todo", "doing"}, nil
		}, Parameter: Parameter{Type: SettingList}},
	)...).Store(s.store())

	if v, err := r.Execute(Request{Noun: "task", Prop: "count"}); err != nil || v != 3 {
		t.Fatalf("count = %v, %v", v, err)
	}
	names, err := r.Execute(Request{Noun: "task", Prop: "names"})
	if err != nil || !reflect.DeepEqual(names, []string{"Water the plants", "Write notes", "Call the plumber"}) {
		t.Fatalf("names = %v, %v", names, err)
	}
	if v, err := r.Execute(Request{Noun: "task", Verb: "get", Args: Arguments{"prop": "kinds.1"}}); err != nil || v != "doing" {
		t.Fatalf("path into a collection prop = %v, %v", v, err)
	}
	got, err := r.Execute(Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"count", "names", "kinds"} {
		if _, ok := got.(map[string]any)[name]; ok {
			t.Fatalf("a thing's record holds the collection prop %s", name)
		}
	}
	if _, err := r.Execute(Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1", "props": "count"}}); err == nil {
		t.Fatal("props named a collection prop")
	}
	if p, _ := r.PropOf("task", "count"); p.Subject != SubjectCollection || p.Computed != true {
		t.Fatalf("count = %+v", p)
	}
}

// A status prop's Enum is the collection's states: no states verb.
func TestEnumIsTheStates(t *testing.T) {
	r, _ := newTaskRuntime(t)
	p, ok := r.PropOf("task", "status")
	if !ok || !slices.Equal(p.Enum, []string{"todo", "doing", "done"}) {
		t.Fatalf("status = %+v", p)
	}
}

func TestComputedPropDeclarationChecks(t *testing.T) {
	r := NewRuntime()
	mustPanic(t, "a collection prop needs Read", func() {
		r.Define("a").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "total", Subject: SubjectCollection})
	})
	mustPanic(t, "unknown subject", func() {
		r.Define("b").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "total", Subject: "world", Read: func(Ctx, Thing) (any, error) { return 1, nil }})
	})
	mustPanic(t, "only read", func() {
		r.Define("c").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "total", Secret: true, Read: func(Ctx, Thing) (any, error) { return 1, nil }})
	})
}

// snapshot get {lists} answers the rows of every store noun's list in one
// call (the state source, in place of a tool's own "every list's rows"
// verb); without lists it is the standard get.
func TestSnapshotGetLists(t *testing.T) {
	r, _ := newTaskRuntime(t)
	v, err := r.Execute(Request{Noun: "ochazuke.snapshot", Verb: "get", Args: Arguments{"lists": true}})
	if err != nil {
		t.Fatal(err)
	}
	lines, ok := v.(Lines)
	if !ok || len(lines) != 3 {
		t.Fatalf("lists = %#v", v)
	}
	first := lines[0].(map[string]any)
	if first["noun"] != "task" || first["id"] != "t1" || first["label"] != "Water the plants" {
		t.Fatalf("first row = %v", first)
	}
	if _, secret := first["data"].(map[string]any)["token"]; secret {
		t.Fatalf("a secret in a row: %v", first)
	}
	if op, _ := r.Operation("ochazuke.snapshot.get"); op.HistoryKindOf() != HistoryFetch || op.Parameters["prop"].Type == "" || op.Parameters["lists"].Type != "boolean" {
		t.Fatalf("snapshot.get = %+v", op)
	}
}

func TestListsVerbIsTheStateSource(t *testing.T) {
	r := NewRuntime()
	r.Define("agent").Verb("state", func(Ctx) Result { return Done(Lines{}) }, "rows").Lists()
	op, _ := r.Operation("agent.state")
	if op.Result != ResultLists || op.HistoryKindOf() != HistoryFetch {
		t.Fatalf("op = %+v", op)
	}
}
