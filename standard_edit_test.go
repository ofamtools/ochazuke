package ochazuke

import (
	"context"
	"errors"
	"maps"
	"ofam.dev/ochazuke/keytest"
	"reflect"
	"testing"
)

func editFixture(t *testing.T) (*Runtime, *derivedStore) {
	t.Helper()
	r := NewRuntime()
	s := &derivedStore{key: "id", things: []Thing{{"id": "t1", "title": "Original", "status": "todo", "active": false, "stamp": "fixed"}}}
	st := s.store()
	st.Undo = true
	r.Define("parcel").Props(
		PropSpec{Name: "id", Key: true},
		PropSpec{Name: "title", Parameter: Parameter{Required: true}},
		PropSpec{Name: "status", Parameter: Parameter{Enum: []string{"todo", "done"}}},
		PropSpec{Name: "active", Parameter: Parameter{Type: SettingBoolean}},
		PropSpec{Name: "stamp", ReadOnly: true},
	).Store(st)
	r.Instance("row", Kind("parcel")).Data = Thing{"id": "t1"}
	if err := r.SetFocus("row"); err != nil {
		t.Fatal(err)
	}
	return r, s
}

func TestStandardEditConfirmCancelUndo(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	if _, err := r.Settings().SetSession(EditApplySetting, "confirm"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	original := maps.Clone(s.find("t1"))
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit"})
	e := r.RecordEditor("edit-form")
	if e == nil {
		t.Fatal("edit did not open the form")
	}
	if got := e.Values(); !reflect.DeepEqual(got, Record{"title": "Original", "status": "todo", "active": "false"}) {
		t.Fatalf("values %v", got)
	}
	for _, key := range []string{"i", "ctrl+u", "Cancelled", "esc", "esc"} {
		keytest.Press(m, key)
		if !reflect.DeepEqual(s.find("t1"), original) {
			t.Fatalf("after cancel key %q: %v", key, s.find("t1"))
		}
	}
	if !reflect.DeepEqual(s.find("t1"), original) {
		t.Fatalf("cancel changed store: %v", s.find("t1"))
	}
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit"})
	for _, key := range []string{"i", "ctrl+u", "Changed", "esc", "j", "space", "j", "enter", "j", "space", "k", "enter"} {
		keytest.Press(m, key)
		if !reflect.DeepEqual(s.find("t1"), original) {
			t.Fatalf("before confirm after %q: %v", key, s.find("t1"))
		}
	}
	keytest.Press(m, "enter")
	want := Thing{"id": "t1", "title": "Changed", "status": "done", "active": true, "stamp": "fixed"}
	if !reflect.DeepEqual(s.find("t1"), want) {
		t.Fatalf("confirmed %v", s.find("t1"))
	}
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "undo"})
	if !reflect.DeepEqual(s.find("t1"), original) {
		t.Fatalf("undo %v", s.find("t1"))
	}
}

func TestStandardEditDirectAndOverride(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit", Args: Arguments{"id": "t1", "title": "CLI", "active": true}})
	if s.find("t1")["title"] != "CLI" || s.find("t1")["active"] != true || r.TopModal() != "" {
		t.Fatalf("direct edit: %v modal %s", s.find("t1"), r.TopModal())
	}
	if _, err := r.Execute(Request{Noun: "parcel", Verb: "edit", Args: Arguments{"stamp": "bad"}}); err == nil {
		t.Fatal("read-only prop accepted")
	}
	r.Define("parcel").Verb("edit", func(Ctx) Result { return Done("app editor") })
	if got := mustRun(t, r, Request{Noun: "parcel", Verb: "edit"}); got != "app editor" {
		t.Fatalf("own edit %v", got)
	}
}

func TestStandardEditOptOutAndReadOnly(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	if _, err := r.Settings().SetSession(EditFormsSetting, false); err != nil {
		t.Fatal(err)
	}
	s := (&derivedStore{key: "id"}).store()
	r.Define("disabled").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title"}).Store(s)
	if _, ok := r.Operation("disabled.edit"); ok {
		t.Fatal("disabled noun unexpectedly offers edit")
	}
	r.Define("disabled").Verb("edit", func(Ctx) Result { return Done("own editor") })
	if got := mustRun(t, r, Request{Noun: "disabled", Verb: "edit"}); got != "own editor" {
		t.Fatalf("own editor: %v", got)
	}
	on := NewRuntime()
	on.Define("readonly").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", ReadOnly: true}).Store(s)
	if _, ok := on.Operation("readonly.edit"); ok {
		t.Fatal("read-only noun unexpectedly offers edit")
	}
	on.Define("own").Verb("edit", func(Ctx) Result { return Done("existing editor") }).Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title"}).Store(s)
	if got := mustRun(t, on, Request{Noun: "own", Verb: "edit"}); got != "existing editor" {
		t.Fatalf("predeclared editor: %v", got)
	}
}

func TestStandardEditPendingChoicesKeepTyping(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	m := NewModel(r)
	workRan := false
	r.Define("slow").Verb("list", func(Ctx) Result {
		return Pending(func(context.Context) (any, error) { workRan = true; return []Choice{{ID: "x", Label: "X"}}, nil })
	})
	r.Define("parcel").Props(PropSpec{Name: "owner", Parameter: Parameter{Source: "slow.list"}})
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit"})
	e := r.RecordEditor(editFormID)
	if workRan {
		t.Fatal("opening the form ran pending choice work inline")
	}
	keytest.Press(m, "A")
	keytest.Press(m, "x")
	if e.Values()["title"] != "Originalx" || s.find("t1")["title"] != "Original" {
		t.Fatalf("typing %v store %v", e.Values(), s.find("t1"))
	}
	if !e.field("owner").choiceLoading {
		t.Fatal("slow choices were not queued")
	}
	keytest.Press(m, "esc")
	keytest.Press(m, "esc")
	if r.TopModal() != "" || s.find("t1")["title"] != "Originalx" {
		t.Fatalf("cancel modal %s store %v", r.TopModal(), s.find("t1"))
	}
}

func TestStandardEditAgentRights(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	restore := r.BindActor(testAgent("edit"), false)
	defer restore()
	mustRun(t, r, Request{Noun: "parcel", Verb: "edit", Args: Arguments{"id": "t1", "title": "Agent"}})
	if s.find("t1")["title"] != "Agent" {
		t.Fatalf("agent write %v", s.find("t1"))
	}
	restore()
	if err := r.Settings().Set(AgentRightsSetting, `[{"match":"parcel.set","right":"never"}]`); err != nil {
		t.Fatal(err)
	}
	restore = r.BindActor(testAgent("edit"), false)
	defer restore()
	_, err := r.Execute(Request{Noun: "parcel", Verb: "edit", Args: Arguments{"id": "t1", "title": "Forbidden"}})
	var refused *RefusedError
	if !errors.As(err, &refused) || s.find("t1")["title"] != "Agent" {
		t.Fatalf("refused %v store %v", err, s.find("t1"))
	}
}

func TestStandardEditKeyPrecedence(t *testing.T) {
	t.Parallel()
	r, s := editFixture(t)
	r.Define("parcel").Verb("custom-edit", func(Ctx) Result {
		s.find("t1")["title"] = "Custom"
		return Done(nil)
	}).Mode("normal").Bind("i", Do("parcel.custom-edit"))
	m := NewModel(r)
	keytest.Press(m, "i")
	if s.find("t1")["title"] != "Custom" || r.TopModal() != "" {
		t.Fatalf("custom edit: %v modal %s", s.find("t1"), r.TopModal())
	}
	other, store := editFixture(t)
	input := NewInput("text", "Original", false)
	if err := other.AddInput(input, Parent("row")); err != nil {
		t.Fatal(err)
	}
	m = NewModel(other)
	if err := other.SetFocus("text"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "i")
	if other.Mode.Name != "insert" || other.TopModal() != "" || store.find("t1")["title"] != "Original" {
		t.Fatalf("input mode %s modal %s store %v", other.Mode.Name, other.TopModal(), store.find("t1"))
	}
}

func TestStandardEditIntegerKey(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	thing := Thing{"id": 7, "title": "Seven"}
	r.Define("item").Props(PropSpec{Name: "id", Key: true, Parameter: Parameter{Type: SettingInteger}}, PropSpec{Name: "title"}).Store(Store{
		Get: func(_ Ctx, key string) (Thing, error) {
			if key != "7" {
				t.Fatalf("key %q", key)
			}
			return maps.Clone(thing), nil
		},
		Patch: func(_ Ctx, key string, changes Thing) (Thing, error) {
			if key != "7" {
				t.Fatalf("key %q", key)
			}
			maps.Copy(thing, changes)
			return maps.Clone(thing), nil
		},
	})
	mustRun(t, r, Request{Noun: "item", Verb: "edit", Args: Arguments{"id": 7, "title": "Changed"}})
	mustRun(t, r, Request{Noun: "item", Verb: "edit", Args: Arguments{"id": 7}})
	if thing["title"] != "Changed" || r.RecordEditor(editFormID).Values()["title"] != "Changed" {
		t.Fatalf("integer key: %v", thing)
	}
}
