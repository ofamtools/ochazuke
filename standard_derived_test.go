package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// derivedStore is an in-memory store of a noun's things that reads and
// writes, every write logged ("set t1 project=work", "create t4",
// "delete t2").
type derivedStore struct {
	key    string
	things []Thing
	log    []string
}

func (s *derivedStore) find(id string) Thing {
	for _, t := range s.things {
		if fmt.Sprint(t[s.key]) == id {
			return t
		}
	}
	return nil
}

func (s *derivedStore) store() Store {
	return Store{
		Get: func(_ Ctx, id string) (Thing, error) {
			if t := s.find(id); t != nil {
				return maps.Clone(t), nil
			}
			return nil, fmt.Errorf("no thing %q", id)
		},
		List: func(Ctx, ListQuery) ([]Thing, error) { return s.things, nil },
		Create: func(_ Ctx, values Thing) (Thing, error) {
			t := maps.Clone(values)
			t[s.key] = fmt.Sprintf("t%d", len(s.things)+1)
			s.things = append(s.things, t)
			s.log = append(s.log, "create "+fmt.Sprint(t[s.key])+logValues(values))
			return t, nil
		},
		Patch: func(_ Ctx, id string, changes Thing) (Thing, error) {
			t := s.find(id)
			if t == nil {
				return nil, fmt.Errorf("no thing %q", id)
			}
			maps.Copy(t, changes)
			s.log = append(s.log, "set "+id+logValues(changes))
			return nil, nil
		},
		Delete: func(_ Ctx, id string) error {
			if s.find(id) == nil {
				return fmt.Errorf("no thing %q", id)
			}
			s.things = slices.DeleteFunc(s.things, func(t Thing) bool { return fmt.Sprint(t[s.key]) == id })
			s.log = append(s.log, "delete "+id)
			return nil
		},
	}
}

func logValues(values Thing) string {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(values)) {
		fmt.Fprintf(&b, " %s=%v", name, values[name])
	}
	return b.String()
}

// derivedProps are a task with every role: a label with a fallback, an
// owner reference filled from the focus, an archive flag.
func derivedProps() []PropSpec {
	return []PropSpec{
		{Name: "id", Key: true},
		{Name: "title", Role: RoleLabel, Fallback: "summary"},
		{Name: "status", Parameter: Parameter{Enum: []string{"todo", "done"}}},
		{Name: "summary", ReadOnly: true},
		{Name: "project", Role: RoleOwner, FromFocus: true, Parameter: Parameter{Noun: "project"}},
		{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
	}
}

// newDerivedRuntime: a task store with three tasks, and a page of
// project rows (p-home, p-work) each holding task rows.
func newDerivedRuntime(t *testing.T) (*Runtime, *derivedStore) {
	t.Helper()
	r := NewRuntime()
	s := &derivedStore{key: "id", things: []Thing{
		{"id": "t1", "title": "Water the plants", "status": "todo", "project": "home", "archived": false},
		{"id": "t2", "title": "Write notes", "status": "todo", "project": "work", "archived": false},
		{"id": "t3", "title": "", "summary": "Call the plumber", "status": "todo", "project": "", "archived": true},
	}}
	r.Define("project").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel})
	r.Define("task").Props(derivedProps()...).Store(s.store())
	r.Instance("board")
	if _, err := r.AddList("projects", []ListItem{
		{ID: "p-home", Label: "Home", Noun: "project", Data: Thing{"id": "home"}},
		{ID: "p-work", Label: "Work", Noun: "project", Data: Thing{"id": "work"}},
	}, Parent("board")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddList("tasks", []ListItem{
		{ID: "row-t1", Label: "t1", Noun: "task", Data: Thing{"id": "t1"}},
		{ID: "row-t2", Label: "t2", Noun: "task", Data: Thing{"id": "t2"}},
	}, Parent("p-work")); err != nil {
		t.Fatal(err)
	}
	return r, s
}

// The derived rows appear exactly when the roles and handlers they read
// are there.
func TestDerivedVerbsFollowTheRoles(t *testing.T) {
	r, _ := newDerivedRuntime(t)
	for _, verb := range []string{"rename", "move", "duplicate", "archive", "restore"} {
		if op := r.Operations["task."+verb]; op.Standard != verb {
			t.Fatalf("task.%s standard %q", verb, op.Standard)
		}
	}
	if !slices.Contains(StandardVerbs(), "archive") || !slices.Contains(reservedStandardVerbs, "restore") {
		t.Fatalf("table %v reserved %v", StandardVerbs(), reservedStandardVerbs)
	}
	for _, c := range []struct {
		name   string
		props  []PropSpec
		store  func(Store) Store
		absent []string
	}{
		{"no roles", []PropSpec{{Name: "id", Key: true}, {Name: "status"}}, nil, []string{"rename", "move", "archive", "restore"}},
		{"read-only label", []PropSpec{{Name: "id", Key: true}, {Name: "title", Role: RoleLabel, ReadOnly: true}, {Name: "status"}}, nil, []string{"rename"}},
		{"no patch", derivedProps(), func(s Store) Store { s.Patch = nil; return s }, []string{"rename", "move", "archive", "restore", "set"}},
		{"no create", derivedProps(), func(s Store) Store { s.Create = nil; return s }, []string{"duplicate", "create"}},
		{"no get", derivedProps(), func(s Store) Store { s.Get = nil; return s }, []string{"duplicate"}},
	} {
		r := NewRuntime()
		st := (&derivedStore{key: "id"}).store()
		if c.store != nil {
			st = c.store(st)
		}
		r.Define("task").Props(c.props...).Store(st)
		for _, verb := range c.absent {
			if _, ok := r.Operations["task."+verb]; ok {
				t.Fatalf("%s: task.%s exists", c.name, verb)
			}
		}
	}
	mustPanic(t, "archive role is a boolean", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "gone", Role: RoleArchive})
	})
	mustPanic(t, "fromFocus is for a reference", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "where", FromFocus: true})
	})
	mustPanic(t, "standard verb's name", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "archive", Parameter: Parameter{Type: SettingBoolean}})
	})
}

// rename writes the label prop, move the owner, archive and restore the
// archive prop, each through set; duplicate creates a copy with overrides.
func TestDerivedVerbsWrite(t *testing.T) {
	r, s := newDerivedRuntime(t)
	mustRun(t, r, Request{Noun: "task", Verb: "rename", Args: Arguments{"id": "t1", "name": "Feed the cat"}})
	mustRun(t, r, Request{Noun: "task", Verb: "rename", Args: Arguments{"id": "t2", "name": ""}}) // an empty label is a value
	mustRun(t, r, Request{Noun: "task", Verb: "move", Args: Arguments{"id": "t1", "to": "work"}})
	mustRun(t, r, Request{Noun: "task", Verb: "archive", Args: Arguments{"id": "t1"}})
	mustRun(t, r, Request{Noun: "task", Verb: "restore", Args: Arguments{"id": "t3"}})
	got := mustRun(t, r, Request{Noun: "task", Verb: "duplicate", Args: Arguments{"id": "t1", "pairs": `title="A copy" status=done`}}).(map[string]any)
	if got["id"] != "t4" || got["title"] != "A copy" || got["status"] != "done" || got["project"] != "work" || got["archived"] != true {
		t.Fatalf("duplicate = %v", got)
	}
	want := []string{
		"set t1 title=Feed the cat",
		"set t2 title=",
		"set t1 project=work",
		"set t1 archived=true",
		"set t3 archived=false",
		"create t4 archived=true project=work status=done title=A copy",
	}
	if !reflect.DeepEqual(s.log, want) {
		t.Fatalf("log\n%q\nwant\n%q", s.log, want)
	}
	// The subject comes from the focus, as set's does.
	if err := r.SetFocus("row-t2"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "rename", Args: Arguments{"name": "Focused"}})
	if s.find("t2")["title"] != "Focused" {
		t.Fatalf("t2 %v", s.find("t2"))
	}
	for _, q := range []Request{
		{Noun: "task", Verb: "rename", Args: Arguments{"id": "t1"}},
		{Noun: "task", Verb: "move", Args: Arguments{"id": "t1"}},
	} {
		var argErr *ArgumentError
		if _, err := r.Execute(q); !errors.As(err, &argErr) || !strings.Contains(err.Error(), "required") {
			t.Fatalf("%s without its argument: %v", q.Verb, err)
		}
	}
	for _, q := range []Request{
		{Noun: "task", Verb: "duplicate", Args: Arguments{"id": "t1", "pairs": "summary=x"}},
		{Noun: "task", Verb: "duplicate", Args: Arguments{"id": "t1", "pairs": "nope=x"}},
		{Noun: "task", Verb: "duplicate", Args: Arguments{"id": "t1", "pairs": "status=x"}},
		{Noun: "task", Verb: "duplicate", Args: Arguments{"id": "t1", "pairs": "status=done", "changes": map[string]any{"title": "x"}}},
	} {
		if _, err := r.Execute(q); err == nil {
			t.Fatalf("duplicate %v: no error", q.Args)
		}
	}
	// History and repeat: the archive write is simple, a rename or a move
	// never is (text, a reference).
	for _, c := range []struct {
		q      Request
		simple bool
	}{
		{Request{Noun: "task", Verb: "archive", Args: Arguments{"id": "t1"}}, true},
		{Request{Noun: "task", Verb: "restore"}, true},
		{Request{Noun: "task", Verb: "rename", Args: Arguments{"name": "x"}}, false},
		{Request{Noun: "task", Verb: "move", Args: Arguments{"to": "home"}}, false},
		{Request{Noun: "task", Verb: "duplicate", Args: Arguments{"pairs": "status=done"}}, true},
	} {
		if got := r.simpleWrite(c.q); got != c.simple {
			t.Fatalf("simpleWrite(%s) = %v", c.q.Verb, got)
		}
		if got := r.repeatableStandard(c.q); got != (c.simple && c.q.Verb != "duplicate") {
			t.Fatalf("repeatable(%s) = %v", c.q.Verb, got)
		}
	}
}

// The key prop's name is reserved: rename's argument is to on a noun
// keyed by name, and no key may take to (move's argument).
func TestRenameArgumentAvoidsTheKey(t *testing.T) {
	r := NewRuntime()
	s := &derivedStore{key: "name", things: []Thing{{"name": "nightly", "title": "Nightly build"}}}
	r.Define("schedule").Props(PropSpec{Name: "name", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(s.store())
	op := r.Operations["schedule.rename"]
	if _, ok := op.Parameters["to"]; !ok || !slices.Equal(op.Positional, []string{"name", "to"}) {
		t.Fatalf("schedule.rename params %v positional %v", op.Parameters, op.Positional)
	}
	mustRun(t, r, Request{Noun: "schedule", Verb: "rename", Args: Arguments{"name": "nightly", "to": "Every night"}})
	if s.find("nightly")["title"] != "Every night" {
		t.Fatalf("%v", s.things)
	}
	if op := (newDerived(t)).Operations["task.rename"]; !slices.Equal(op.Positional, []string{"id", "name"}) || op.Parameters["name"].Edits != "title" {
		t.Fatalf("task.rename %+v", op)
	}
	mustPanic(t, "standard verb's parameter", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "to", Key: true})
	})
}

func newDerived(t *testing.T) *Runtime {
	r, _ := newDerivedRuntime(t)
	return r
}

// create fills a FromFocus reference from the focused thing of its noun
// and says so; a given value wins; with no such thing focused, or with
// create.from-focus off, the reference stays empty and create still runs.
func TestCreateFillsFromTheFocus(t *testing.T) {
	r, s := newDerivedRuntime(t)
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	create := r.Operations["task.create"]
	if !create.Parameters["project"].Focus || !slices.Equal(create.Positional, []string{"title", "project"}) {
		t.Fatalf("create %+v", create)
	}
	if err := r.SetFocus("row-t1"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"title": "A"}})
	mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"title": "B", "project": "home"}})
	if err := r.SetFocus("board"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"title": "C"}})
	if err := r.SetFocus("row-t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Settings().SetSession(CreateFromFocusSetting, false); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"title": "D"}})
	want := []string{"create t4 project=work title=A", "create t5 project=home title=B", "create t6 title=C", "create t7 title=D"}
	if !reflect.DeepEqual(s.log, want) {
		t.Fatalf("log\n%q\nwant\n%q", s.log, want)
	}
	var notices []string
	for _, m := range sink.Messages() {
		notices = append(notices, m.Text)
	}
	if len(notices) != 1 || notices[0] != "project Work (from the focus)" {
		t.Fatalf("notices %q", notices)
	}
	// The slot's reading: off, nothing; on, the focused project.
	if _, ok := r.focusValue(create, "project", "row-t1"); ok {
		t.Fatal("focusValue with the setting off")
	}
	if _, err := r.Settings().SetSession(CreateFromFocusSetting, true); err != nil {
		t.Fatal(err)
	}
	if v, ok := r.focusValue(create, "project", "row-t1"); !ok || v.Key != "work" || v.Label != "Work" {
		t.Fatalf("focusValue %+v %v", v, ok)
	}
}

// delete.archive: on, delete archives a live thing and deletes an
// archived one, with the confirm both times; a noun without the archive
// role deletes as before; off, delete deletes.
func TestDeleteArchive(t *testing.T) {
	r, s := newDerivedRuntime(t)
	if _, err := r.Settings().SetSession(DeleteArchiveSetting, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("row-t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "task", Verb: "delete"}); err != nil || r.TopModal() == "" {
		t.Fatalf("delete asks first: %v (top %q)", err, r.TopModal())
	}
	mustRun(t, r, Request{Noun: "ochazuke.confirm", Verb: "accept"})
	mustRun(t, r, Request{Noun: "task", Verb: "delete", Confirmed: true, Args: Arguments{"id": "t3"}})
	if want := []string{"set t1 archived=true", "delete t3"}; !reflect.DeepEqual(s.log, want) {
		t.Fatalf("log %q", s.log)
	}
	// Without the role: unchanged.
	r2 := NewRuntime()
	s2 := &derivedStore{key: "id", things: []Thing{{"id": "n1", "title": "x"}}}
	r2.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(s2.store())
	if _, err := r2.Settings().SetSession(DeleteArchiveSetting, true); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r2, Request{Noun: "note", Verb: "delete", Confirmed: true, Args: Arguments{"id": "n1"}})
	if !reflect.DeepEqual(s2.log, []string{"delete n1"}) {
		t.Fatalf("no role: %q", s2.log)
	}
	if _, err := r.Settings().SetSession(DeleteArchiveSetting, false); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "delete", Confirmed: true, Args: Arguments{"id": "t2"}})
	if s.log[len(s.log)-1] != "delete t2" {
		t.Fatalf("off: %q", s.log)
	}
}

// An alias named like a prop of its noun clashes, except the prop's own
// set (task.status as task.set {prop: status}), in either order.
func TestAliasPropClashException(t *testing.T) {
	r, _ := newDerivedRuntime(t)
	if err := r.AddActionAlias(ActionAlias{Alias: "task.status", Action: "task.set", Args: Arguments{"prop": "status"}}); err != nil {
		t.Fatalf("the prop's own set: %v", err)
	}
	if err := r.AddActionAlias(ActionAlias{Alias: "task.title", Action: "task.set", Args: Arguments{"prop": "status"}}); err == nil || !strings.Contains(err.Error(), "clashes with the prop title") {
		t.Fatalf("another prop's set: %v", err)
	}
	if err := r.AddActionAlias(ActionAlias{Alias: "task.summary", Action: "task.get"}); err == nil {
		t.Fatal("an alias of get named like a prop")
	}
	r2 := NewRuntime()
	r2.Define("task").Verb("show", func(Ctx) Result { return Done(nil) })
	if err := r2.AddActionAlias(ActionAlias{Alias: "task.title", Action: "task.show"}); err != nil {
		t.Fatal(err)
	}
	mustPanic(t, "clashes with the alias", func() { r2.Define("task").Props(PropSpec{Name: "title"}) })
}

// checks report a named verb that shadows a standard one, as info.
func TestChecksReportShadowedStandardVerbs(t *testing.T) {
	r := NewRuntime()
	r.Define("task").Verb("rename", func(Ctx) Result { return Done("mine") }, "The app's rename")
	s := &derivedStore{key: "id"}
	r.Define("task").Props(derivedProps()...).Store(s.store())
	if op := r.Operations["task.rename"]; op.Standard != "" {
		t.Fatalf("the app's rename lost: %+v", op)
	}
	want := "info: task.rename is a named verb: it shadows the standard rename of task"
	if got := r.StandardShadows(); !slices.Equal(got, []string{want}) {
		t.Fatalf("shadows %q", got)
	}
	for _, noun := range []string{"ochazuke.bindings", "ochazuke.actions"} {
		v := mustRun(t, r, Request{Noun: noun, Verb: "check"})
		if !slices.Contains(v.([]string), want) {
			t.Fatalf("%s check %q", noun, v)
		}
	}
}
