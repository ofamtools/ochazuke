package ochazuke

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// writeStore is a task store that writes: the taskStore's things, with
// every write logged ("set t1 status=done", "create t4", "delete t2").
type writeStore struct {
	*taskStore
	log []string
}

func (s *writeStore) find(id string) Thing {
	for _, t := range s.things {
		if t["id"] == id {
			return t
		}
	}
	return nil
}

func (s *writeStore) store() Store {
	st := s.taskStore.store()
	st.Create = func(_ Ctx, values Thing) (Thing, error) {
		t := Thing{"id": fmt.Sprintf("t%d", len(s.things)+1)}
		for k, v := range values {
			t[k] = v
		}
		s.things = append(s.things, t)
		s.log = append(s.log, "create "+t["id"].(string))
		return t, nil
	}
	st.Patch = func(_ Ctx, id string, changes Thing) (Thing, error) {
		t := s.find(id)
		if t == nil {
			return nil, fmt.Errorf("no task %q", id)
		}
		var parts []string
		for _, p := range taskWriteProps() {
			if v, ok := changes[p.Name]; ok {
				t[p.Name] = v
				parts = append(parts, fmt.Sprintf("%s=%v", p.Name, v))
			}
		}
		s.log = append(s.log, "set "+id+" "+strings.Join(parts, " "))
		return nil, nil // read back through Get
	}
	st.Delete = func(_ Ctx, id string) error {
		if s.find(id) == nil {
			return fmt.Errorf("no task %q", id)
		}
		s.things = slices.DeleteFunc(s.things, func(t Thing) bool { return t["id"] == id })
		s.log = append(s.log, "delete "+id)
		return nil
	}
	return st
}

// taskWriteProps are the test task's props with a boolean (starred), a
// multiline text (notes) and a guarded one (secretly: only where the
// "admin" guard holds).
func taskWriteProps() []PropSpec {
	return append(taskTestProps(),
		PropSpec{Name: "starred", Parameter: Parameter{Type: SettingBoolean}},
		PropSpec{Name: "notes", Multiline: true},
		PropSpec{Name: "secretly", When: "admin"},
	)
}

func newWriteRuntime(t *testing.T) (*Runtime, *writeStore) {
	t.Helper()
	r := NewRuntime()
	s := &writeStore{taskStore: newTaskStore()}
	r.Define("task").Props(taskWriteProps()...).Store(s.store())
	r.Instance("tasks")
	var rows []ListItem
	for _, id := range []string{"t1", "t2", "t3"} {
		rows = append(rows, ListItem{ID: "row-" + id, Label: id, Noun: "task", Data: Thing{"id": id}})
	}
	if _, err := r.AddList("tasks/list", rows, Parent("tasks")); err != nil {
		t.Fatal(err)
	}
	return r, s
}

func mustRun(t *testing.T, r *Runtime, q Request) any {
	t.Helper()
	v, err := r.Execute(q)
	if err != nil {
		t.Fatalf("%+v: %v", q, err)
	}
	return v
}

// TestStandardWriteVerbs: create, set and delete appear with their store
// handlers; patch is set's alias; create's first word is the label prop.
func TestStandardWriteVerbs(t *testing.T) {
	r, _ := newWriteRuntime(t)
	for _, verb := range []string{"create", "set", "delete"} {
		if op := r.Operations["task."+verb]; op.Standard != verb {
			t.Fatalf("task.%s standard %q", verb, op.Standard)
		}
	}
	if a, ok := r.ActionAlias("task.patch"); !ok || a.Action != "task.set" || a.Source != AliasStandard {
		t.Fatalf("task.patch alias = %+v %v", a, ok)
	}
	create := r.Operations["task.create"]
	if !slices.Equal(create.Positional, []string{"title"}) {
		t.Fatalf("create positional %v", create.Positional)
	}
	for _, name := range []string{"id", "summary"} {
		if _, ok := create.Parameters[name]; ok {
			t.Fatalf("create takes the read-only %s", name)
		}
	}
	if set := r.Operations["task.set"]; !set.Many || !slices.Equal(set.Positional, []string{"id", "prop", "value", "pairs"}) {
		t.Fatalf("set many %v positional %v", set.Many, set.Positional)
	}
	if del := r.Operations["task.delete"]; !del.Many || del.Confirm != DefaultDeleteConfirm {
		t.Fatalf("delete many %v confirm %q", del.Many, del.Confirm)
	}
	// A store without the handler, or with nothing writable, has no verb.
	r2 := NewRuntime()
	r2.Define("ro").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", ReadOnly: true}).Store(Store{Patch: func(Ctx, string, Thing) (Thing, error) { return nil, nil }})
	if _, ok := r2.Operations["ro.set"]; ok {
		t.Fatal("set on a noun with no writable prop")
	}
	if _, ok := r2.ActionAlias("ro.patch"); ok {
		t.Fatal("patch without set")
	}
	// The application's own patch wins over the generated alias, silently.
	r3 := NewRuntime()
	r3.Define("task").Verb("patch", func(Ctx) Result { return Done("mine") })
	s3 := &writeStore{taskStore: newTaskStore()}
	r3.Define("task").Props(taskWriteProps()...).Store(s3.store())
	if v, err := r3.Execute(Request{Noun: "task", Verb: "patch"}); err != nil || v != "mine" {
		t.Fatalf("app patch = %v %v", v, err)
	}
	if len(r3.ActionAliasProblems()) > 0 {
		t.Fatalf("problems %v", r3.ActionAliasProblems())
	}
}

// TestSetOneAndSeveralProps: set's one-prop form (prop and value, typed by
// the prop; toggle on a boolean) and its several-prop forms (changes, and
// prop=value words) share one handler; patch runs set.
func TestSetOneAndSeveralProps(t *testing.T) {
	r, s := newWriteRuntime(t)
	got := mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "estimate", "value": "5"}}).(map[string]any)
	if got["estimate"] != 5 || got["id"] != "t1" {
		t.Fatalf("set estimate = %v", got)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "starred", "toggle": true}})
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t2", "changes": map[string]any{"status": "done", "title": "Ship"}}})
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t3", "prop": "title=Buy milk", "value": "status=doing", "pairs": `notes="two words" estimate=4`}})
	mustRun(t, r, Request{Noun: "task", Verb: "patch", Args: Arguments{"id": "t3", "prop": "status", "value": "todo"}})
	want := []string{"set t1 estimate=5", "set t1 starred=true", "set t2 title=Ship status=done", "set t3 title=Buy milk status=doing estimate=4 notes=two words", "set t3 status=todo"}
	if !reflect.DeepEqual(s.log, want) {
		t.Fatalf("log\n%q\nwant\n%q", s.log, want)
	}
	for _, bad := range []struct {
		args Arguments
		want string
	}{
		{Arguments{"id": "t1", "prop": "status", "value": "nope"}, "must be one of"},
		{Arguments{"id": "t1", "prop": "summary", "value": "x"}, "read-only"},
		{Arguments{"id": "t1", "prop": "id", "value": "t9"}, "read-only"},
		{Arguments{"id": "t1", "prop": "status"}, "value is required"},
		{Arguments{"id": "t1", "prop": "title", "toggle": true}, "only a boolean prop toggles"},
		{Arguments{"id": "t1", "prop": "title=a", "value": "status"}, "do not mix"},
		{Arguments{"id": "t1", "prop": "status", "value": "done", "changes": map[string]any{"title": "x"}}, "not both"},
		{Arguments{"id": "t1"}, "set needs a prop"},
	} {
		if _, err := r.Execute(Request{Noun: "task", Verb: "set", Args: bad.args}); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Fatalf("set %v: %v, want %q", bad.args, err, bad.want)
		}
	}
}

// TestCreateAndDelete: create writes the props given and each omitted
// one's Default; delete asks first (the default prompt names the thing by
// its label, else its Fallback) and runs once confirmed.
func TestCreateAndDelete(t *testing.T) {
	r := NewRuntime()
	s := &writeStore{taskStore: newTaskStore()}
	props := taskWriteProps()
	props[2].Default = "todo"
	r.Define("task").Props(props...).Store(s.store())
	got := mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"title": "Buy milk", "estimate": 1}}).(map[string]any)
	if got["id"] != "t4" || got["status"] != "todo" || got["title"] != "Buy milk" {
		t.Fatalf("create = %v", got)
	}
	if _, err := r.Execute(Request{Noun: "task", Verb: "create", Args: Arguments{"summary": "x"}}); err == nil {
		t.Fatal("create wrote a read-only prop")
	}
	asked := mustRun(t, r, Request{Noun: "task", Verb: "delete", Args: Arguments{"id": "t3"}}).(map[string]any)
	if asked["prompt"] != "Delete task Call the plumber?" {
		t.Fatalf("delete asked %v", asked)
	}
	if len(s.log) != 1 {
		t.Fatalf("delete ran before the answer: %v", s.log)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.confirm", Verb: "accept"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(s.log, []string{"create t4", "delete t3"}) {
		t.Fatalf("log %v", s.log)
	}
	// A store's own prompt wins; "" asks nothing.
	r2 := NewRuntime()
	s2 := &writeStore{taskStore: newTaskStore()}
	st := s2.store()
	st.Confirm = map[string]string{"delete": ""}
	r2.Define("task").Props(taskWriteProps()...).Store(st)
	if v := mustRun(t, r2, Request{Noun: "task", Verb: "delete", Args: Arguments{"id": "t1"}}).(map[string]any); v["deleted"] != "t1" {
		t.Fatalf("delete with no prompt = %v", v)
	}
	mustPanic(t, "not a standard verb", func() {
		NewRuntime().Define("task").Props(taskWriteProps()...).Store(Store{Confirm: map[string]string{"archive": "?"}})
	})
}

// TestPropRequests: prop-shaped requests read, write and toggle a prop of
// the thing the request acts for, through get and set; history keeps them
// as they were sent, a write a change and a read a fetch.
func TestPropRequests(t *testing.T) {
	r, s := newWriteRuntime(t)
	if err := r.SetFocus("row-t2"); err != nil {
		t.Fatal(err)
	}
	if v := mustRun(t, r, Request{Noun: "task", Prop: "status"}); v != "doing" {
		t.Fatalf("read status = %v", v)
	}
	mustRun(t, r, Request{Noun: "task", Prop: "status", Value: "done"})
	mustRun(t, r, Request{Noun: "task", Prop: "starred", Toggle: true})
	mustRun(t, r, Request{Noun: "task", Prop: "starred", Toggle: true})
	mustRun(t, r, Request{Noun: "task", Prop: "estimate", Value: 7, Args: Arguments{"id": "t1"}})
	want := []string{"set t2 status=done", "set t2 starred=true", "set t2 starred=false", "set t1 estimate=7"}
	if !reflect.DeepEqual(s.log, want) {
		t.Fatalf("log %q", s.log)
	}
	entries := r.HistoryOf(0)
	var kinds []string
	for _, e := range entries {
		if e.Request.Noun == "task" {
			if !e.Request.IsProp() {
				t.Fatalf("history rewrote the request: %+v", e.Request)
			}
			kinds = append(kinds, e.Kind)
		}
	}
	if !slices.Equal(kinds, []string{HistoryFetch, HistoryChange, HistoryChange, HistoryChange, HistoryChange}) {
		t.Fatalf("kinds %v", kinds)
	}
	for _, bad := range []struct {
		q    Request
		want string
	}{
		{Request{Noun: "task", Prop: "summary", Value: "x"}, "read-only"},
		{Request{Noun: "task", Prop: "title", Toggle: true}, "only a boolean"},
		{Request{Noun: "task", Prop: "nope"}, "no prop"},
		{Request{Noun: "task", Prop: "status", Verb: "get"}, "a verb or a prop"},
		{Request{Noun: "task", Prop: "status", Value: "done", Toggle: true}, "value or toggle"},
		{Request{Noun: "task", Prop: "status", Value: "done", Args: Arguments{"where": "x"}}, "not the argument"},
		{Request{Noun: "task", Value: "done"}, "go with a prop"},
	} {
		if _, err := r.Execute(bad.q); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Fatalf("%+v: %v, want %q", bad.q, err, bad.want)
		}
	}
}

// TestPropRowsInBindings: a binding row may name a prop request (SetProp,
// ToggleProp, or the file's "request"); it validates against the prop,
// runs on the row it matched, From fills its value, its hint names the
// prop, and the bindings file writes it back as it reads.
func TestPropRowsInBindings(t *testing.T) {
	r, s := newWriteRuntime(t)
	done := SetProp("task", "status", "done")
	done.Noun, done.Mode, done.Keys = "task", "normal", "x"
	star := ToggleProp("task", "starred")
	star.Noun, star.Mode, star.Keys = "task", "normal", "*"
	est := Binding{Noun: "task", Mode: "normal", Keys: "digit", Request: Request{Noun: "task", Prop: "estimate"}, From: map[string]string{"value": SourceDigit}}
	est.Pattern, est.Keys = "digit", ""
	if err := r.Bind(done, star, est); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("row-t3"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"x", "*", "4"} {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if want := []string{"set t3 status=done", "set t3 starred=true", "set t3 estimate=4"}; !reflect.DeepEqual(s.log, want) {
		t.Fatalf("log %q", s.log)
	}
	var shown []string
	for _, b := range HintBindings(r, "row-t3") {
		shown = append(shown, rowHint(r, b))
	}
	hints := strings.Join(shown, " · ")
	if !strings.Contains(hints, "x task.status") {
		t.Fatalf("hints %q", hints)
	}
	for _, bad := range []struct {
		row  Binding
		want string
	}{
		{SetProp("task", "summary", "x"), "read-only"},
		{SetProp("task", "status", "nope"), "must be one of"},
		{ToggleProp("task", "title"), "only a boolean"},
		{SetProp("task", "nope", 1), "no prop"},
		{Binding{Request: Request{Noun: "task", Prop: "status", Value: "done"}, From: map[string]string{"id": SourceKey}}, "only its value"},
	} {
		bad.row.Noun, bad.row.Mode, bad.row.Keys = "task", "normal", "q"
		if err := r.Bind(bad.row); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Fatalf("%+v: %v, want %q", bad.row.Request, err, bad.want)
		}
	}
	// The grouped file's "request" field reads back to the same row.
	line, err := formatBindingLine(done)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"key": "x", "request": {"noun": "task", "prop": "status", "value": "done"}}`; string(line) != want {
		t.Fatalf("line %s\nwant %s", line, want)
	}
	back, key, err := parseBindingLine(line)
	if err != nil || back.Request.Prop != "status" || back.Request.Value != "done" || key != "x" {
		t.Fatalf("read back %+v %v", back, err)
	}
	if _, _, err := parseBindingLine([]byte(`{"key": "x", "action": "task.set", "request": {"noun": "task", "prop": "status"}}`)); err == nil {
		t.Fatal("action and request both")
	}
}

// TestPropGuardsAndStoreWhen: a prop's When hides it where its guard is
// false (set and a prop request are unavailable there, get leaves it out),
// and a store's When is the generated verb's availability.
func TestPropGuardsAndStoreWhen(t *testing.T) {
	admin := false
	r := NewRuntime()
	r.DefineGuard("admin", func(*Runtime, KeyEvent) bool { return admin })
	r.DefineGuard("not-t1", func(r *Runtime, ev KeyEvent) bool { return StringArg(ev.Args, "id", "") != "t1" })
	s := &writeStore{taskStore: newTaskStore()}
	st := s.store()
	st.When = map[string]string{"delete": "not-t1"}
	r.Define("task").Props(taskWriteProps()...).Store(st)
	focus := r.Focus
	set := Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "secretly", "value": "x"}}
	if r.Available(set, focus, r.Mode) || r.Available(Request{Noun: "task", Prop: "secretly", Value: "x"}, focus, r.Mode) {
		t.Fatal("a guarded prop is writable where its guard is false")
	}
	if !r.Available(Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "prop": "status", "value": "done"}}, focus, r.Mode) {
		t.Fatal("an unguarded prop is not writable")
	}
	if _, err := r.Execute(set); err == nil || !strings.Contains(err.Error(), "not applicable") {
		t.Fatalf("set secretly: %v", err)
	}
	s.things[0]["secretly"] = "shh"
	if got := mustRun(t, r, Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1"}}).(map[string]any); got["secretly"] != nil {
		t.Fatalf("get shows a hidden prop: %v", got)
	}
	admin = true
	mustRun(t, r, set)
	if got := mustRun(t, r, Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1"}}).(map[string]any); got["secretly"] != "x" {
		t.Fatalf("get with the guard on: %v", got)
	}
	if r.VerbGuard("task.delete") != "not-t1" || r.Available(Request{Noun: "task", Verb: "delete", Args: Arguments{"id": "t1"}}, focus, r.Mode) {
		t.Fatal("store When is not delete's availability")
	}
	if !r.Available(Request{Noun: "task", Verb: "delete", Args: Arguments{"id": "t2"}}, focus, r.Mode) {
		t.Fatal("delete t2 unavailable")
	}
	// No writable prop offered: set itself is unavailable.
	r2 := NewRuntime()
	r2.DefineGuard("never", func(*Runtime, KeyEvent) bool { return false })
	r2.Define("box").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "lid", When: "never"}).Store(Store{Patch: func(Ctx, string, Thing) (Thing, error) { return nil, nil }})
	if r2.Available(Request{Noun: "box", Verb: "set"}, r2.Focus, r2.Mode) {
		t.Fatal("set offered with no prop available")
	}
}

// TestConfirmUsesTheFallbackLabel: delete's prompt names the thing by its
// label prop, else its Fallback, from a key argument (read from the store)
// or from the row it acts for; over several rows, the first and a count.
func TestConfirmUsesTheFallbackLabel(t *testing.T) {
	r, _ := newWriteRuntime(t)
	prompt := func(q Request) string {
		t.Helper()
		v := mustRun(t, r, q).(map[string]any)
		if err := r.Pop(); err != nil {
			t.Fatal(err)
		}
		return v["prompt"].(string)
	}
	if got := prompt(Request{Noun: "task", Verb: "delete", Args: Arguments{"id": "t3"}}); got != "Delete task Call the plumber?" {
		t.Fatalf("by key: %q", got)
	}
	if err := r.SetFocus("row-t3"); err != nil {
		t.Fatal(err)
	}
	if got := prompt(Request{Noun: "task", Verb: "delete"}); got != "Delete task Call the plumber?" {
		t.Fatalf("by focus: %q", got)
	}
	if got := prompt(Request{Noun: "task", Verb: "delete", Target: "row-t1"}); got != "Delete task Water the plants?" {
		t.Fatalf("by target: %q", got)
	}
}

// TestVisualSetIsOneUndoGroup: set over a visual selection runs once with
// every row (Many), writes each row once, and makes one undo group.
func TestVisualSetIsOneUndoGroup(t *testing.T) {
	r, s := newWriteRuntime(t)
	groups, sealed := 0, 0
	r.List("tasks/list").undoGroup = func() func() {
		groups++
		return func() { sealed++ }
	}
	if err := r.SetFocus("row-t1"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "set", Subjects: []string{"row-t1", "row-t2", "row-t3"}, Args: Arguments{"prop": "status", "value": "done"}})
	if want := []string{"set t1 status=done", "set t2 status=done", "set t3 status=done"}; !reflect.DeepEqual(s.log, want) {
		t.Fatalf("log %q", s.log)
	}
	if groups != 1 || sealed != 1 {
		t.Fatalf("undo groups %d sealed %d", groups, sealed)
	}
	// A key argument does not pull the rows onto one thing.
	s.log = nil
	mustRun(t, r, Request{Noun: "task", Verb: "set", Subjects: []string{"row-t1", "row-t2"}, Args: Arguments{"id": "t3", "prop": "status", "value": "todo"}})
	if want := []string{"set t1 status=todo", "set t2 status=todo"}; !reflect.DeepEqual(s.log, want) {
		t.Fatalf("log %q", s.log)
	}
}

// TestSimpleValueTest: one test (enum, boolean, number, duration; never
// text, multiline, references or secrets) decides both what . repeats and
// what the history file keeps (owner decision 12).
func TestSimpleValueTest(t *testing.T) {
	r, _ := newWriteRuntime(t)
	set := func(args Arguments) Request {
		args["id"] = "t1"
		return Request{Noun: "task", Verb: "set", Args: args}
	}
	for _, c := range []struct {
		q      Request
		simple bool
	}{
		{set(Arguments{"prop": "status", "value": "done"}), true},
		{set(Arguments{"prop": "estimate", "value": 3}), true},
		{set(Arguments{"prop": "starred", "toggle": true}), true},
		{set(Arguments{"changes": map[string]any{"status": "done", "estimate": 2}}), true},
		{set(Arguments{"prop": "title", "value": "x"}), false},
		{set(Arguments{"prop": "notes", "value": "x"}), false},
		{set(Arguments{"prop": "token", "value": "x"}), false},
		{set(Arguments{"changes": map[string]any{"status": "done", "title": "x"}}), false},
		{Request{Noun: "task", Prop: "status", Value: "done"}, true},
		{Request{Noun: "task", Prop: "title", Value: "x"}, false},
		{Request{Noun: "task", Verb: "create", Args: Arguments{"status": "todo"}}, true},
		{Request{Noun: "task", Verb: "create", Args: Arguments{"title": "x"}}, false},
	} {
		if got := r.simpleWrite(c.q); got != c.simple {
			t.Fatalf("simpleWrite(%+v) = %v", c.q, got)
		}
		e, keep := r.persistable(HistoryEntry{Kind: HistoryChange, Request: c.q, Result: HistoryResult{OK: true}})
		if keep != c.simple {
			t.Fatalf("persisted(%+v) = %v", c.q, keep)
		}
		if keep && !reflect.DeepEqual(e.Request.Args, c.q.Args) {
			t.Fatalf("a simple write's line lost its values: %v", e.Request.Args)
		}
		if repeat := r.repeatableStandard(c.q); repeat != (c.simple && c.q.Verb != "create") {
			t.Fatalf("repeatable(%+v) = %v", c.q, repeat)
		}
	}
	// delete writes no values: its line is kept as any change's.
	if _, keep := r.persistable(HistoryEntry{Kind: HistoryChange, Request: Request{Noun: "task", Verb: "delete", Args: Arguments{"id": "t1"}}, Result: HistoryResult{OK: true}}); !keep {
		t.Fatal("delete not persisted")
	}
}

// TestDotRepeatsSimpleWrites: . writes the last simple value to the new
// focus; after a text edit it repeats the simple write before it, never
// the text.
func TestDotRepeatsSimpleWrites(t *testing.T) {
	r, s := newWriteRuntime(t)
	if err := r.SetFocus("row-t1"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"prop": "status", "value": "done"}})
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"prop": "title", "value": "Renamed"}})
	if err := r.SetFocus("row-t2"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "repeat"})
	mustRun(t, r, Request{Noun: "task", Prop: "estimate", Value: 9})
	if err := r.SetFocus("row-t3"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "ochazuke.history", Verb: "repeat"})
	want := []string{"set t1 status=done", "set t1 title=Renamed", "set t2 status=done", "set t2 estimate=9", "set t3 estimate=9"}
	if !reflect.DeepEqual(s.log, want) {
		t.Fatalf("log\n%q\nwant\n%q", s.log, want)
	}
}

// PropField is a prop as a form field.
func TestPropField(t *testing.T) {
	p := PropSpec{Name: "notes", Multiline: true, Parameter: Parameter{Type: SettingString, Description: "d"}}
	if f := PropField(p); f.Name != "notes" || !f.Multiline || f.Description != "d" {
		t.Fatalf("field %+v", f)
	}
}

// bindings check passes on prop rows in a user file, in the grouped
// format and the rows format, and reports a bad one.
func TestBindingsCheckOnPropRows(t *testing.T) {
	for _, tc := range []struct{ text, problem string }{
		{`{"bindings": [{"context": "task/normal", "bindings": [
			{"key": "x", "request": {"noun": "task", "prop": "status", "value": "done"}},
			{"key": "*", "request": {"noun": "task", "prop": "starred", "toggle": true}},
			{"key": "digit", "request": {"noun": "task", "prop": "estimate"}, "from": {"value": "digit"}}]}]}`, ""},
		{`[{"noun": "task", "mode": "normal", "keys": "x", "request": {"noun": "task", "prop": "status", "value": "done"}}]`, ""},
		{`{"bindings": [{"context": "task/normal", "bindings": [{"key": "x", "request": {"noun": "task", "prop": "summary", "value": "x"}}]}]}`, "read-only"},
	} {
		r, _ := newWriteRuntime(t)
		dir := t.TempDir()
		r.Config().Dir = dir
		writeConfig(t, dir, BindingsFile, tc.text)
		got, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "check"})
		if err != nil {
			t.Fatal(err)
		}
		problems := strings.Join(got.([]string), "\n")
		if tc.problem == "" && problems != "" || tc.problem != "" && !strings.Contains(problems, tc.problem) {
			t.Fatalf("check %q, want %q\n%s", problems, tc.problem, tc.text)
		}
	}
}

// A prop-shaped request reads from JSON as the API's socket sends it.
func TestPropRequestDecodes(t *testing.T) {
	var q Request
	if err := decodeRequest([]byte(`{"noun": "task", "prop": "starred", "toggle": true}`), &q); err != nil || !q.Writes() || q.Prop != "starred" {
		t.Fatalf("decoded %+v %v", q, err)
	}
	if err := decodeRequest([]byte(`{"noun": "task"}`), new(Request)); err == nil {
		t.Fatal("a request naming neither verb nor prop decoded")
	}
}
