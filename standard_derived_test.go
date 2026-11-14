package ochazuke

import (
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
		Stamps: true, // the log shows the writes the test makes, not the framework's times
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

// archive is the role-derived row; the purged verbs (rename, move, set's
// alias patch) are gone (duplicate and restore came back with the noun
// scaffold, scaffold_verbs.go), and a request that
// addresses a role the noun lacks is unavailable, as the verb was.
func TestDerivedVerbsFollowTheRoles(t *testing.T) {
	r, _ := newDerivedRuntime(t)
	if op := r.Operations.At(Key("task.archive")); op.Standard != "archive" {
		t.Fatalf("task.archive standard %q", op.Standard)
	}
	for _, verb := range []string{"rename", "move", "patch"} {
		if _, ok := r.Operation("task." + verb); ok || slices.Contains(StandardVerbs(), verb) || slices.Contains(reservedVerbNames(), verb) {
			t.Fatalf("task.%s is still a verb", verb)
		}
	}
	role := func(q Request) bool { return r.Available(q, "", r.Mode) }
	label := Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t1", "props": map[string]any{"@label": "x"}}}
	owner := Request{Noun: "task", Verb: "edit", Args: Arguments{"id": "t1", "field": "@owner"}}
	if !role(label) || !role(owner) {
		t.Fatal("role requests unavailable on a noun with the roles")
	}
	for _, c := range []struct {
		name   string
		props  []PropSpec
		store  func(Store) Store
		absent []string
		roles  []Request
	}{
		{"no roles", []PropSpec{{Name: "id", Key: true}, {Name: "status"}}, nil, []string{"archive"}, []Request{label, owner}},
		{"read-only label", []PropSpec{{Name: "id", Key: true}, {Name: "title", Role: RoleLabel, ReadOnly: true}, {Name: "status"}}, nil, nil, []Request{label}},
		{"no patch", derivedProps(), func(s Store) Store { s.Patch = nil; return s }, []string{"archive", "set", "edit"}, nil},
		{"no create", derivedProps(), func(s Store) Store { s.Create = nil; return s }, []string{"create"}, nil},
	} {
		r := NewRuntime()
		st := (&derivedStore{key: "id", things: []Thing{{"id": "t1", "status": "todo"}}}).store()
		if c.store != nil {
			st = c.store(st)
		}
		r.Define("project").Props(PropSpec{Name: "id", Key: true})
		r.Define("task").Props(c.props...).Store(st)
		for _, verb := range c.absent {
			if r.offers("task." + verb) {
				t.Fatalf("%s: task.%s is offered", c.name, verb)
			}
		}
		for _, q := range c.roles {
			if r.Available(q, "", r.Mode) {
				t.Fatalf("%s: %s %v is available", c.name, q.Verb, q.Args)
			}
			// A key row may still name it: the row is unavailable, not refused.
			if err := r.Bind(Binding{Noun: "task", Mode: "normal", Keys: "r", Request: q}); err != nil {
				t.Fatalf("%s: binding %s %v: %v", c.name, q.Verb, q.Args, err)
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

// What the purged verbs did, through the core verbs: set writes a prop
// addressed by its role (@label: rename, @owner: move, @archive false:
// restore), archive sets the archive prop, and create {from} copies a
// thing with overrides (duplicate).
func TestRoleWritesReplaceThePurgedVerbs(t *testing.T) {
	r, s := newDerivedRuntime(t)
	set := func(id string, props map[string]any) Request {
		return Request{Noun: "task", Verb: "set", Args: Arguments{"id": id, "props": props}}
	}
	mustRun(t, r, set("t1", map[string]any{"@label": "Feed the cat"}))
	mustRun(t, r, set("t2", map[string]any{"@label": ""})) // an empty label is a value
	mustRun(t, r, set("t1", map[string]any{"@owner": "work"}))
	mustRun(t, r, Request{Noun: "task", Verb: "archive", Args: Arguments{"id": "t1"}})
	mustRun(t, r, set("t3", map[string]any{"@archive": false}))
	got := mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"from": "t1", "pairs": `title="A copy" status=done`}}).(map[string]any)
	if got["id"] != "t4" || got["title"] != "A copy" || got["status"] != "done" || got["project"] != "work" || got["archived"] != true {
		t.Fatalf("create from = %v", got)
	}
	// from names the thing by its human key too, or is a row's Data.
	byLabel := mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"from": "Feed the cat"}}).(map[string]any)
	byRow := mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"from": map[string]any{"id": "t2"}, "status": "done"}}).(map[string]any)
	if byLabel["title"] != "Feed the cat" || byLabel["project"] != "work" || byRow["status"] != "done" || byRow["project"] != "work" {
		t.Fatalf("by label %v, by row %v", byLabel, byRow)
	}
	want := []string{
		"set t1 title=Feed the cat",
		"set t2 title=",
		"set t1 project=work",
		"set t1 archived=true",
		"set t3 archived=false",
		"create t4 archived=true project=work status=done title=A copy",
		"create t5 archived=true project=work status=todo title=Feed the cat",
		"create t6 archived=false project=work status=done title=",
	}
	if !reflect.DeepEqual(s.log, want) {
		t.Fatalf("log\n%q\nwant\n%q", s.log, want)
	}
	// The subject comes from the focus.
	if err := r.SetFocus("row-t2"); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"props": map[string]any{"@label": "Focused"}}})
	if s.find("t2")["title"] != "Focused" {
		t.Fatalf("t2 %v", s.find("t2"))
	}
	for _, q := range []Request{
		set("t1", map[string]any{"@nope": "x"}),
		set("t1", map[string]any{"summary": "x"}),
		{Noun: "task", Verb: "create", Args: Arguments{"from": "t1", "pairs": "summary=x"}},
		{Noun: "task", Verb: "create", Args: Arguments{"from": "t1", "pairs": "nope=x"}},
		{Noun: "task", Verb: "create", Args: Arguments{"from": "nonesuch"}},
		{Noun: "task", Verb: "create", Args: Arguments{"from": map[string]any{"title": "no key"}}},
	} {
		if _, err := r.Execute(q); err == nil {
			t.Fatalf("%s %v: no error", q.Verb, q.Args)
		}
	}
	// History and repeat: the archive write is simple, a label or an owner
	// never is (text, a reference); a create is never repeated.
	for _, c := range []struct {
		q                  Request
		simple, repeatable bool
	}{
		{Request{Noun: "task", Verb: "archive", Args: Arguments{"id": "t1"}}, true, true},
		{set("", map[string]any{"@archive": false}), true, true},
		{set("", map[string]any{"@label": "x"}), false, false},
		{set("", map[string]any{"@owner": "home"}), false, false},
		{Request{Noun: "task", Verb: "create", Args: Arguments{"from": "t1", "pairs": "status=done"}}, true, false},
	} {
		if got := r.simpleWrite(c.q); got != c.simple {
			t.Fatalf("simpleWrite(%s %v) = %v", c.q.Verb, c.q.Args, got)
		}
		if got := r.repeatableStandard(c.q); got != c.repeatable {
			t.Fatalf("repeatable(%s %v) = %v", c.q.Verb, c.q.Args, got)
		}
	}
}

// A copy is undone by deleting it, as any create is.
func TestCreateFromIsUndoneByDelete(t *testing.T) {
	r, s := newUndoRuntime(t, true, false)
	before := len(s.things)
	got := mustRun(t, r, Request{Noun: "task", Verb: "create", Args: Arguments{"from": "t1"}}).(map[string]any)
	if len(s.things) != before+1 || got["title"] != s.find("t1")["title"] {
		t.Fatalf("copy %v of %v", got, s.find("t1"))
	}
	mustRun(t, r, Request{Noun: "ochazuke.history-entry", Verb: "undo"})
	if len(s.things) != before || s.find(fmt.Sprint(got["id"])) != nil {
		t.Fatalf("the copy %v is still there: %q", got["id"], s.log)
	}
}

// The key prop's name is reserved, and so are the core verbs' own
// parameters (props, field, from): no key takes one.
func TestStandardParamsAvoidTheKey(t *testing.T) {
	r := NewRuntime()
	s := &derivedStore{key: "name", things: []Thing{{"name": "nightly", "title": "Nightly build"}}}
	r.Define("schedule").Props(PropSpec{Name: "name", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(s.store())
	mustRun(t, r, Request{Noun: "schedule", Verb: "set", Args: Arguments{"name": "nightly", "props": map[string]any{"@label": "Every night"}}})
	if s.find("nightly")["title"] != "Every night" {
		t.Fatalf("%v", s.things)
	}
	if p := r.Operations.At(Key("schedule.edit")).Parameters["field"]; !slices.Contains(p.Enum, "title") || !slices.Contains(p.Enum, "@label") {
		t.Fatalf("schedule.edit field %+v", p)
	}
	for _, name := range []string{"props", "field", "from"} {
		mustPanic(t, "standard verb's parameter", func() {
			NewRuntime().Define("x").Props(PropSpec{Name: name, Key: true})
		})
	}
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
	create := r.Operations.At(Key("task.create"))
	if !create.Parameters["project"].Focus || !slices.Equal(create.Positional, []string{"title", "project", "pairs"}) {
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
// archived one, asking first both times; a noun without the archive
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

// The confirm says what delete will do: archiving a live thing asks
// DefaultArchiveConfirm, deleting an archived one DefaultDeleteConfirm, a
// selection holding one that will be deleted the delete prompt; with
// delete.archive-confirm off the archive asks nothing and the delete
// still asks; with delete.archive off, the delete prompt.
func TestDeleteArchiveConfirm(t *testing.T) {
	r, s := newDerivedRuntime(t)
	prompt := func(q Request) string {
		t.Helper()
		got, err := r.Execute(q)
		if err != nil {
			t.Fatalf("%s: %v", q.Key(), err)
		}
		if r.TopModal() == "" {
			return ""
		}
		p := r.Instances[r.TopModal()].Payload.Prompt
		mustRun(t, r, Request{Noun: "ochazuke.modal", Verb: "pop"})
		if m, ok := got.(map[string]any); !ok || m["prompt"] != p {
			t.Fatalf("answer %v, modal %q", got, p)
		}
		return p
	}
	if err := r.SetFocus("row-t1"); err != nil {
		t.Fatal(err)
	}
	del := Request{Noun: "task", Verb: "delete"}
	if got := prompt(del); got != "Delete task Water the plants?" {
		t.Fatalf("delete.archive off: %q", got)
	}
	if _, err := r.Settings().SetSession(DeleteArchiveSetting, true); err != nil {
		t.Fatal(err)
	}
	if got, want := prompt(del), "Archive task Water the plants? (delete it again to delete it for good)"; got != want {
		t.Fatalf("archive: %q, want %q", got, want)
	}
	if got := prompt(Request{Noun: "task", Verb: "delete", Args: Arguments{"id": "t3"}}); got != "Delete task Call the plumber?" {
		t.Fatalf("archived: %q", got)
	}
	both := Request{Noun: "task", Verb: "delete", Subjects: []string{"row-t1", "row-t2"}}
	if got := prompt(both); !strings.HasPrefix(got, "Archive task ") {
		t.Fatalf("two live: %q", got)
	}
	apply := Request{Noun: "ochazuke.collection", Verb: "apply", Args: Arguments{"id": "tasks", "verb": "delete"}}
	if got := prompt(apply); !strings.HasPrefix(got, "Archive task ") { // {{label}} reads the row (collection.apply)
		t.Fatalf("collection.apply on one row: %q", got)
	}
	mustRun(t, r, Request{Noun: "task", Verb: "archive", Args: Arguments{"id": "t2"}})
	if got := prompt(both); !strings.HasPrefix(got, "Delete task ") {
		t.Fatalf("one archived: %q", got)
	}
	if len(s.log) != 1 {
		t.Fatalf("asking wrote: %q", s.log)
	}
	if _, err := r.Settings().SetSession(DeleteArchiveConfirmSetting, false); err != nil {
		t.Fatal(err)
	}
	if got := prompt(del); got != "" {
		t.Fatalf("archive-confirm off asks %q", got)
	}
	if s.log[len(s.log)-1] != "set t1 archived=true" {
		t.Fatalf("archive-confirm off: %q", s.log)
	}
	if got := prompt(del); got != "Delete task Water the plants?" {
		t.Fatalf("real delete with archive-confirm off: %q", got)
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
	r.Define("task").Verb("archive", func(Ctx) Result { return Done("mine") }, "The app's archive")
	s := &derivedStore{key: "id"}
	r.Define("task").Props(derivedProps()...).Store(s.store())
	if op := r.Operations.At(Key("task.archive")); op.Standard != "" {
		t.Fatalf("the app's archive lost: %+v", op)
	}
	want := "info: task.archive is a named verb: it shadows the standard archive of task"
	if got := r.StandardShadows(); !slices.Equal(got, []string{want}) {
		t.Fatalf("shadows %q", got)
	}
	for _, noun := range []string{"ochazuke.binding", "ochazuke.user-operation"} {
		v := mustRun(t, r, Request{Noun: noun, Verb: "check"})
		if !slices.Contains(v.([]string), want) {
			t.Fatalf("%s check %q", noun, v)
		}
	}
}

// A verb declared to shadow the standard one on purpose
// (NounBuilder.ShadowsStandard, Operation.ShadowsStandard) is not reported;
// one beside it that is not declared keeps its info line.
func TestChecksQuietADeclaredShadow(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Define("task").Verb("list", func(Ctx) Result { return Done(nil) }, "The app's own list").ShadowsStandard().
		Verb("archive", func(Ctx) Result { return Done("mine") }, "The app's archive")
	s := &derivedStore{key: "id"}
	r.Define("task").Props(derivedProps()...).Store(s.store())
	if op := r.Operations.At(Key("task.list")); op.Standard != "" || !op.ShadowsStandard {
		t.Fatalf("the app's list: %+v", op)
	}
	want := "info: task.archive is a named verb: it shadows the standard archive of task"
	if got := r.StandardShadows(); !slices.Equal(got, []string{want}) {
		t.Fatalf("shadows %q", got)
	}
}

// A required prop other than the label is a word of create before the
// pairs rest (a rest parameter takes the line's last words), so a store
// declaring one registers and create fills it by position:
// note create "Shopping" list kind=x.
func TestCreateRequiredPropComesBeforePairs(t *testing.T) {
	r := NewRuntime()
	s := &derivedStore{key: "id"}
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("Store with a required prop: %v", p)
			}
		}()
		r.Define("note").Props(
			PropSpec{Name: "id", Key: true},
			PropSpec{Name: "title", Role: RoleLabel},
			PropSpec{Name: "type", Parameter: Parameter{Required: true}},
			PropSpec{Name: "kind"},
		).Store(s.store())
	}()
	op, ok := r.Operation("note.create")
	if !ok {
		t.Fatal("no note.create")
	}
	if err := checkRestParameter(op); err != nil {
		t.Fatal(err)
	}
	order := op.ParameterOrder()
	if i, j := slices.Index(order, "type"), slices.Index(order, "pairs"); i < 0 || i > j {
		t.Fatalf("order %v: type after pairs", order)
	}
}

// An exact key wins over a prefix of other keys in the palette, whether or
// not its thing is listed: an archived t1 beside live t10, t11 and t12 is
// t1 (it was "ambiguous: t10, t11, t12"), and a live t1 is never read as
// another key it prefixes.
func TestPaletteExactKeyBeatsPrefix(t *testing.T) {
	r, s := newDerivedRuntime(t)
	s.find("t1")["archived"] = true
	for _, id := range []string{"t10", "t11", "t12"} {
		s.things = append(s.things, Thing{"id": id, "title": "Task " + id, "status": "todo", "project": "work", "archived": false})
	}
	NewPalette(r)
	for _, line := range []string{"task set --id t1 status=done", "task set t1 status=done"} {
		s.find("t1")["status"] = "todo"
		paletteRequest(t, r, "open", "palette", nil)
		paletteLine(t, r, line)
		if got := s.find("t1")["status"]; got != "done" {
			t.Fatalf("%s: t1 status %v, log %v", line, got, s.log)
		}
		for _, id := range []string{"t10", "t11", "t12"} {
			if s.find(id)["status"] != "todo" {
				t.Fatalf("%s: wrote %s", line, id)
			}
		}
	}
	// A word that is no key still reads as the prefix of one.
	s.find("t11")["archived"], s.find("t12")["archived"] = true, true
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "task set t10 status=done")
	if s.find("t10")["status"] != "done" {
		t.Fatalf("t10 not written: %v", s.log)
	}
}

// edit over several subjects writes every field to each, but the human key
// (the label here) to the subject alone: one name typed is not every row's.
// Given field @label, the others are left as they were.
func TestEditOverSubjectsKeepsTheHumanKeyToOne(t *testing.T) {
	r, s := newDerivedRuntime(t)
	subjects := []string{"row-t1", "row-t2"}
	mustRun(t, r, Request{Noun: "task", Verb: "edit", Target: "row-t1", Subjects: subjects, Args: Arguments{"title": "Renamed", "status": "done"}})
	t1, t2 := s.find("t1"), s.find("t2")
	if t1["title"] != "Renamed" || t1["status"] != "done" || t2["title"] != "Write notes" || t2["status"] != "done" {
		t.Fatalf("fan-out: t1 %v t2 %v", t1, t2)
	}
	t2["status"] = "todo"
	mustRun(t, r, Request{Noun: "task", Verb: "edit", Target: "row-t1", Subjects: subjects, Args: Arguments{"field": "@label", "title": "Again", "status": "done"}})
	if t1["title"] != "Again" || t2["title"] != "Write notes" || t2["status"] != "todo" {
		t.Fatalf("field @label: t1 %v t2 %v", t1, t2)
	}
}

// history-entry.undo from the palette posts its one plain line (its Summary),
// naming the thing by its label, and never its answer's JSON.
func TestPaletteUndoSaysOneLine(t *testing.T) {
	r, s := newDerivedRuntime(t)
	n := r.Noun("task")
	st := *n.Store
	st.Undo = true
	n.Store = &st
	mustRun(t, r, Request{Noun: "task", Verb: "set", Args: Arguments{"id": "t2", "props": map[string]any{"@label": "Renamed"}}})
	NewPalette(r)
	stop := r.UI().capture()
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "history undo")
	got := stop()
	if s.find("t2")["title"] != "Write notes" {
		t.Fatalf("not undone: %v", s.log)
	}
	var lines []string
	for _, m := range got {
		if strings.Contains(m.Text, "{") || m.Kind == UINotice {
			t.Fatalf("the answer was dumped: %+v", got)
		}
		if strings.HasPrefix(m.Text, "undid") {
			lines = append(lines, m.Text)
		}
	}
	if len(lines) == 0 || lines[len(lines)-1] != "undid task.set on Write notes" {
		t.Fatalf("undo lines %q", lines)
	}
}

// A request shape the operation does not declare is refused, naming the
// argument, on every core verb and on the framework's register (o-git's
// old {name, text}), never run as if it were not there.
func TestUndeclaredArgumentsAreRefused(t *testing.T) {
	r, s := newDerivedRuntime(t)
	before := len(s.log)
	for _, verb := range CoreVerbs() {
		args := Arguments{"bogus": "x"}
		if _, takes := r.Operations.At(Key("task." + verb)).Parameters["id"]; takes {
			args["id"] = "t1"
		}
		_, err := r.Execute(Request{Noun: "task", Verb: verb, Args: args})
		if err == nil || !strings.Contains(err.Error(), `"bogus"`) {
			t.Errorf("task.%s {bogus}: %v", verb, err)
		}
	}
	if len(s.log) != before {
		t.Fatalf("a refused request wrote: %v", s.log[before:])
	}
	_, err := r.Execute(Request{Noun: "ochazuke.register", Verb: "set", Args: Arguments{"name": "\"", "text": "hello"}})
	if err == nil || !strings.Contains(err.Error(), `"text"`) {
		t.Fatalf("register set {name, text}: %v", err)
	}
	if got, _ := r.Execute(Request{Noun: "ochazuke.register", Verb: "get", Args: Arguments{"name": "\"", "prop": "text"}}); got != "" {
		t.Fatalf("register written: %v", got)
	}
}

// The prop's own set may share its name whichever form of set it presets:
// prop (`{prop: status}`) or props (`{props: {status: done}}`); a props
// object that writes another prop, or more than that one, still clashes.
func TestAliasPropClashPropsForm(t *testing.T) {
	t.Parallel()
	r, s := newDerivedRuntime(t)
	if err := r.AddActionAlias(ActionAlias{Alias: "task.status", Action: "task.set", Args: Arguments{"props": map[string]any{"status": "done"}}}); err != nil {
		t.Fatalf("the prop's own set by props: %v", err)
	}
	if err := r.AddActionAlias(ActionAlias{Alias: "task.title", Action: "task.set", Args: Arguments{"props": map[string]any{"status": "done"}}}); err == nil || !strings.Contains(err.Error(), "clashes with the prop title") {
		t.Fatalf("another prop's set by props: %v", err)
	}
	if err := r.AddActionAlias(ActionAlias{Alias: "task.archived", Action: "task.set", Args: Arguments{"props": map[string]any{"archived": true, "status": "done"}}}); err == nil || !strings.Contains(err.Error(), "clashes with the prop archived") {
		t.Fatalf("a set of two props named like one: %v", err)
	}
	if _, err := r.Execute(Request{Noun: "task", Verb: "status", Args: Arguments{"id": "t1"}}); err != nil {
		t.Fatal(err)
	}
	if got := s.things[0]["status"]; got != "done" {
		t.Fatalf("task.status by props wrote %v", got)
	}
}
