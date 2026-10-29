package ochazuke

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// taskStore is a three-task store; reads names every key Get was asked for.
type taskStore struct {
	things []Thing
	reads  []string
}

func newTaskStore() *taskStore {
	return &taskStore{things: []Thing{
		{"id": "t1", "title": "Water the plants", "status": "todo", "estimate": 2, "token": "s3cret"},
		{"id": "t2", "title": "Write notes", "status": "doing", "estimate": 3},
		{"id": "t3", "title": "", "status": "todo", "estimate": 1, "summary": "Call the plumber"},
	}}
}

func (s *taskStore) store() Store {
	return Store{
		Get: func(_ Ctx, id string) (Thing, error) {
			s.reads = append(s.reads, id)
			for _, t := range s.things {
				if t["id"] == id {
					return t, nil
				}
			}
			return nil, fmt.Errorf("no task %q", id)
		},
		List: func(Ctx, ListQuery) ([]Thing, error) { return s.things, nil },
	}
}

func taskTestProps() []PropSpec {
	return []PropSpec{
		{Name: "id", Key: true},
		{Name: "title", Role: RoleLabel, Fallback: "summary"},
		{Name: "status", Parameter: Parameter{Enum: []string{"todo", "doing", "done"}}},
		{Name: "estimate", Parameter: Parameter{Type: SettingInteger}},
		{Name: "summary", ReadOnly: true},
		{Name: "token", Secret: true},
	}
}

func newTaskRuntime(t *testing.T) (*Runtime, *taskStore) {
	t.Helper()
	r := NewRuntime()
	s := newTaskStore()
	r.Define("task").Props(taskTestProps()...).Store(s.store())
	return r, s
}

func mustPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		t.Helper()
		got := recover()
		if got == nil {
			t.Fatalf("no panic, want %q", want)
		}
		if !strings.Contains(fmt.Sprint(got), want) {
			t.Fatalf("panic %q, want %q", got, want)
		}
	}()
	fn()
}

// TestStandardVerbsNeeds: a row is registered exactly when its kind fits
// and the store has its handler.
func TestStandardVerbsNeeds(t *testing.T) {
	cases := []struct {
		name  string
		props []PropSpec
		store Store
		want  []string
	}{
		{"get only", []PropSpec{{Name: "id", Key: true}}, Store{Get: func(Ctx, string) (Thing, error) { return Thing{}, nil }}, []string{"get"}},
		{"list only", []PropSpec{{Name: "id", Key: true}}, Store{List: func(Ctx, ListQuery) ([]Thing, error) { return nil, nil }}, []string{"list", "watch"}},
		{"both", []PropSpec{{Name: "id", Key: true}}, newTaskStore().store(), []string{"get", "list", "watch"}},
		{"singleton has no list", []PropSpec{{Name: "theme"}}, Store{Kind: Singleton, Get: func(Ctx, string) (Thing, error) { return Thing{}, nil }, List: func(Ctx, ListQuery) ([]Thing, error) { return nil, nil }}, []string{"get", "watch"}},
		{"nothing", []PropSpec{{Name: "id", Key: true}}, Store{}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := NewRuntime()
			r.Define("thing").Props(c.props...).Store(c.store)
			var got []string
			for _, verb := range StandardVerbs() {
				if op, ok := r.Operations["thing."+verb]; ok {
					if op.Standard != verb || op.Builtin {
						t.Fatalf("%s: standard %q builtin %v", verb, op.Standard, op.Builtin)
					}
					got = append(got, verb)
				}
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("verbs %v, want %v", got, c.want)
			}
		})
	}
	r := NewRuntime()
	r.Define("theme").Props(PropSpec{Name: "name"}).Store(Store{Kind: Singleton, Get: func(Ctx, string) (Thing, error) { return Thing{"name": "kombu"}, nil }})
	op := r.Operations["theme.get"]
	if _, ok := op.Parameters["name"]; ok || len(op.Positional) > 0 {
		t.Fatalf("a singleton's get takes no subject: %v %v", op.Parameters, op.Positional)
	}
	if got, err := r.Execute(Request{Noun: "theme", Verb: "get", Args: Arguments{"prop": "name"}}); err != nil || got != "kombu" {
		t.Fatalf("singleton get = %v, %v", got, err)
	}
	r2, _ := newTaskRuntime(t)
	get := r2.Operations["task.get"]
	if p := get.Parameters["id"]; !p.Focus || p.Noun != "task" || !slices.Equal(get.Positional, []string{"id"}) {
		t.Fatalf("task.get subject = %+v positional %v", p, get.Positional)
	}
	if list := r2.Operations["task.list"]; list.Rows != "task" || list.HistoryKindOf() != HistoryFetch || list.Kind != KindApplication {
		t.Fatalf("task.list = rows %q history %q kind %q", list.Rows, list.HistoryKindOf(), list.Kind)
	}
	if r2.ChoicesSource(get.Parameters["id"]) != "task.list" {
		t.Fatalf("the subject's choices are not task.list")
	}
}

// TestPropClashRules: after a noun a word is a verb or a prop, never both.
func TestPropClashRules(t *testing.T) {
	ok := Store{Get: func(Ctx, string) (Thing, error) { return Thing{}, nil }}
	mustPanic(t, "standard verb's name", func() { NewRuntime().Define("x").Props(PropSpec{Name: "delete"}) })
	mustPanic(t, "one word", func() { NewRuntime().Define("x").Props(PropSpec{Name: "a.b"}) })
	mustPanic(t, "already declared", func() { NewRuntime().Define("x").Props(PropSpec{Name: "a"}, PropSpec{Name: "a"}) })
	mustPanic(t, "clashes with the verb", func() {
		r := NewRuntime()
		r.Define("x").Verb("open", func(Ctx) Result { return Done(nil) }).Props(PropSpec{Name: "open"})
	})
	mustPanic(t, "clashes with its prop", func() {
		r := NewRuntime()
		r.Define("x").Props(PropSpec{Name: "status"}).Verb("status", func(Ctx) Result { return Done(nil) })
	})
	mustPanic(t, "nested under", func() {
		r := NewRuntime()
		r.Define("x.sub")
		r.Define("x").Props(PropSpec{Name: "sub"})
	})
	mustPanic(t, "unknown type", func() { NewRuntime().Define("x").Props(PropSpec{Name: "a", Parameter: Parameter{Type: "date"}}) })
	mustPanic(t, "only a string prop takes an enum", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "a", Parameter: Parameter{Type: SettingInteger, Enum: []string{"1"}}})
	})
	mustPanic(t, "both take the role label", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "a", Role: RoleLabel}, PropSpec{Name: "b", Role: RoleLabel}).Store(ok)
	})
	mustPanic(t, "owner role needs a reference", func() { NewRuntime().Define("x").Props(PropSpec{Name: "p", Role: RoleOwner}) })
	mustPanic(t, "not another prop", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "a", Role: RoleLabel, Fallback: "nope"}).Store(ok)
	})
	mustPanic(t, "exactly one Key prop", func() { NewRuntime().Define("x").Props(PropSpec{Name: "a"}).Store(ok) })
	mustPanic(t, "a singleton has no Key prop", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "a", Key: true}).Store(Store{Kind: Singleton, Get: ok.Get})
	})
	mustPanic(t, "a prop named id is the key", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "slug", Key: true}, PropSpec{Name: "id"}).Store(ok)
	})
	mustPanic(t, "declare Props before Store", func() { NewRuntime().Define("x").Store(ok) })
	mustPanic(t, "unknown store kind", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "id", Key: true}).Store(Store{Kind: "table", Get: ok.Get})
	})

	// A user action (actions.json) may not take a prop's name either.
	r, _ := newTaskRuntime(t)
	if err := (Action{Noun: "task", Verb: "status", Command: []string{"true"}}).check(r); err == nil || !strings.Contains(err.Error(), "is a prop of") {
		t.Fatalf("action named like a prop: %v", err)
	}
}

// TestKeyNameReserved: the key parameter's name is the key prop's, so no
// other parameter of the standard verbs may take it (owner decisions 4, 18).
func TestKeyNameReserved(t *testing.T) {
	ok := Store{List: func(Ctx, ListQuery) ([]Thing, error) { return nil, nil }}
	for _, name := range standardParamNames {
		mustPanic(t, "a standard verb's parameter", func() { NewRuntime().Define("x").Props(PropSpec{Name: name, Key: true}) })
	}
	mustPanic(t, "takes the key's name", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "name", Key: true}).Store(Store{List: ok.List, ListParams: map[string]Parameter{"name": {Type: "string"}}})
	})
	mustPanic(t, "standard parameter's name", func() {
		NewRuntime().Define("x").Props(PropSpec{Name: "name", Key: true}).Store(Store{List: ok.List, ListParams: map[string]Parameter{"where": {Type: "string"}}})
	})
	r := NewRuntime()
	r.Define("schedule").Props(PropSpec{Name: "name", Key: true}).Store(Store{Get: func(_ Ctx, id string) (Thing, error) { return Thing{"name": id}, nil }})
	if p, ok := r.Props("schedule")[0], true; !ok || !p.ReadOnly {
		t.Fatalf("a Key prop is read-only: %+v", p)
	}
	got, err := r.Execute(Request{Noun: "schedule", Verb: "get", Args: Arguments{"name": "nightly"}})
	if err != nil || !reflect.DeepEqual(got, map[string]any{"name": "nightly"}) {
		t.Fatalf("schedule get name=nightly = %v, %v", got, err)
	}
}

// TestSubjectOrder: a key argument wins over a target, a target over the
// focus, and the focus's thing is the key prop in its Data, else its id.
func TestSubjectOrder(t *testing.T) {
	r, s := newTaskRuntime(t)
	r.Instance("tasks")
	if _, err := r.AddList("tasks/list", []ListItem{
		{ID: "row-1", Label: "one", Noun: "task", Data: Thing{"id": "t1"}},
		{ID: "t2", Label: "two", Noun: "task"},
		{ID: "row-3", Label: "three", Noun: "task", Data: Thing{"id": "t3"}},
	}, Parent("tasks")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("row-1"); err != nil {
		t.Fatal(err)
	}
	get := func(q Request) {
		t.Helper()
		q.Noun, q.Verb = "task", "get"
		if _, err := r.Execute(q); err != nil {
			t.Fatal(err)
		}
	}
	get(Request{})                                             // focus: row-1's Data
	get(Request{Target: "row-3"})                              // target over focus
	get(Request{Target: "row-3", Args: Arguments{"id": "t2"}}) // key over target
	get(Request{Target: "t2"})                                 // no Data: the instance id
	if want := []string{"t1", "t3", "t2", "t2"}; !reflect.DeepEqual(s.reads, want) {
		t.Fatalf("reads %v, want %v", s.reads, want)
	}
	// Focus off any task: the CLI's case. The key is required then.
	if err := r.SetFocus("tasks"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "task", Verb: "get"}); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("get off a task: %v", err)
	}
	// The palette's word reaches it as the positional key.
	if v, err := r.Execute(Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t2", "prop": "status"}}); err != nil || v != "doing" {
		t.Fatalf("get t2 status = %v, %v", v, err)
	}
}

// TestAppVerbWins: an application verb named like a standard verb replaces
// it, registered before or after the store, and keeps its aliases (R5).
func TestAppVerbWins(t *testing.T) {
	mine := func(Ctx) Result { return Done("mine") }
	check := func(t *testing.T, r *Runtime) {
		t.Helper()
		op := r.Operations["task.get"]
		if op.Standard != "" {
			t.Fatalf("task.get is still the standard one")
		}
		if v, err := r.Execute(Request{Noun: "task", Verb: "get", Args: Arguments{}}); err != nil || v != "mine" {
			t.Fatalf("task.get = %v, %v", v, err)
		}
		if n := slices.Index(r.Nouns["task"].Verbs, "get"); n < 0 || slices.Index(r.Nouns["task"].Verbs[n+1:], "get") >= 0 {
			t.Fatalf("verbs %v: get once", r.Nouns["task"].Verbs)
		}
		if r.Operations["task.list"].Standard != "list" {
			t.Fatalf("task.list should stay standard")
		}
	}
	t.Run("after", func(t *testing.T) {
		r, _ := newTaskRuntime(t)
		if err := r.AddActionAlias(ActionAlias{Alias: "task.show", Action: "task.get"}); err != nil {
			t.Fatal(err)
		}
		r.Define("task").Verb("get", mine)
		check(t, r)
		if aliases := r.AliasesOf("task.get"); len(aliases) != 1 || aliases[0].Alias != "task.show" {
			t.Fatalf("aliases %v", aliases)
		}
		if v, err := r.Execute(Request{Noun: "task", Verb: "show"}); err != nil || v != "mine" {
			t.Fatalf("task.show = %v, %v", v, err)
		}
	})
	t.Run("after by Register", func(t *testing.T) {
		r, _ := newTaskRuntime(t)
		r.Register(Operation{Noun: "task", Verb: "get", App: mine})
		check(t, r)
	})
	t.Run("before", func(t *testing.T) {
		r := NewRuntime()
		r.Define("task").Verb("get", mine)
		if err := r.AddActionAlias(ActionAlias{Alias: "task.show", Action: "task.get"}); err != nil {
			t.Fatal(err)
		}
		r.Define("task").Props(taskTestProps()...).Store(newTaskStore().store())
		check(t, r)
		if len(r.AliasesOf("task.get")) != 1 {
			t.Fatalf("aliases lost")
		}
		// Props declared again after the store keep the app's verb too.
		r.Define("task").Props(PropSpec{Name: "owner"})
		check(t, r)
	})
	t.Run("duplicate non-standard still panics", func(t *testing.T) {
		r, _ := newTaskRuntime(t)
		r.Define("task").Verb("open", mine)
		mustPanic(t, "duplicate operation: task.open", func() { r.Register(Operation{Noun: "task", Verb: "open", App: mine}) })
		r.Register(Operation{Noun: "task", Verb: "get", App: mine})
		mustPanic(t, "duplicate operation: task.get", func() { r.Register(Operation{Noun: "task", Verb: "get", App: mine}) })
	})
}

// TestListRows: rows carry id and the label (the label prop, else its
// Fallback); where, props and limit narrow them; secrets stay out unless
// named.
func TestListRows(t *testing.T) {
	r, _ := newTaskRuntime(t)
	list := func(args Arguments) []map[string]any {
		t.Helper()
		v, err := r.Execute(Request{Noun: "task", Verb: "list", Args: args})
		if err != nil {
			t.Fatal(err)
		}
		return v.([]map[string]any)
	}
	rows := list(nil)
	var labels []string
	for _, row := range rows {
		labels = append(labels, row["label"].(string))
		if _, ok := row["token"]; ok {
			t.Fatalf("a secret in a row: %v", row)
		}
	}
	if want := []string{"Water the plants", "Write notes", "Call the plumber"}; !reflect.DeepEqual(labels, want) {
		t.Fatalf("labels %v, want %v", labels, want)
	}
	if rows[2]["title"] != "" || rows[2]["id"] != "t3" {
		t.Fatalf("the fallback is display only: %v", rows[2])
	}
	if got := list(Arguments{"where": "status=todo estimate=1"}); len(got) != 1 || got[0]["id"] != "t3" {
		t.Fatalf("where = %v", got)
	}
	if got := list(Arguments{"where": "status=todo", "limit": 1}); len(got) != 1 || got[0]["id"] != "t1" {
		t.Fatalf("limit = %v", got)
	}
	if got := list(Arguments{"props": "status,token", "limit": 1}); !reflect.DeepEqual(got[0], map[string]any{"id": "t1", "label": "Water the plants", "status": "todo", "token": "s3cret"}) {
		t.Fatalf("props = %v", got[0])
	}
	for _, bad := range []Arguments{{"where": "nope=1"}, {"where": "status"}, {"where": "status=late"}, {"where": "estimate=x"}, {"props": "nope"}} {
		if _, err := r.Execute(Request{Noun: "task", Verb: "list", Args: bad}); err == nil {
			t.Fatalf("list %v: no error", bad)
		}
	}
	got, err := r.Execute(Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1"}})
	if err != nil || !reflect.DeepEqual(got, map[string]any{"id": "t1", "title": "Water the plants", "status": "todo", "estimate": 2}) {
		t.Fatalf("get t1 = %v, %v", got, err)
	}
	if v, _ := r.Execute(Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1", "prop": "token"}}); v != "s3cret" {
		t.Fatalf("a secret asked by name = %v", v)
	}
	if _, err := r.Execute(Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t1", "prop": "status", "props": "title"}}); err == nil {
		t.Fatalf("prop and props: no error")
	}
}

// TestLabelFallbackInConfirm: {{label}} reads the label prop from a row's
// Data, else its Fallback.
func TestLabelFallbackInConfirm(t *testing.T) {
	r, _ := newTaskRuntime(t)
	r.Define("task").Verb("archive", func(Ctx) Result { return Done(nil) }).Confirm("Archive {{label}}?")
	r.Instance("tasks")
	if _, err := r.AddList("tasks/list", []ListItem{{ID: "row-3", Label: "row three", Noun: "task", Data: Thing{"id": "t3", "title": "", "summary": "Call the plumber"}}}, Parent("tasks")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("row-3"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Execute(Request{Noun: "task", Verb: "archive"})
	if err != nil {
		t.Fatal(err)
	}
	if prompt := got.(map[string]any)["prompt"]; prompt != "Archive Call the plumber?" {
		t.Fatalf("prompt %q", prompt)
	}
}
