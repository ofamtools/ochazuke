package ochazuke

import (
	"reflect"
	"slices"
	"testing"
)

func TestArchiveListVisibilityAndLimit(t *testing.T) {
	r := NewRuntime()
	things := []Thing{{"id": "old", "retired": true}, {"id": "legacy"}, {"id": "live", "retired": false}, {"id": "text", "retired": "true"}}
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "retired", Aliases: []string{"hidden"}, Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}}).Store(Store{List: func(_ Ctx, q ListQuery) ([]Thing, error) {
		if q.Limit > 0 {
			return things[:min(len(things), q.Limit)], nil
		}
		return things, nil
	}})
	ids := func(args Arguments) []string {
		t.Helper()
		v, err := r.Execute(Request{Noun: "parcel", Verb: "list", Args: args})
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, row := range v.([]map[string]any) {
			out = append(out, row["id"].(string))
		}
		return out
	}
	for _, tc := range []struct {
		args Arguments
		want []string
	}{
		{nil, []string{"legacy", "live"}},
		{Arguments{"limit": 1}, []string{"legacy"}},
		{Arguments{"where": "hidden=true"}, []string{"old", "text"}},
		{Arguments{"where": "retired=false"}, []string{"live"}},
		{Arguments{"filter": "archived"}, []string{"old", "text"}},
		{Arguments{"filter": "archived", "limit": 1}, []string{"old"}},
	} {
		if got := ids(tc.args); !slices.Equal(got, tc.want) {
			t.Fatalf("%v: %v want %v", tc.args, got, tc.want)
		}
	}
	r.Define("parcel").Filters(SavedFilter{Name: "either", Conditions: []PropFilter{{Prop: "hidden", Op: "in", Value: []any{true, false}}}}, SavedFilter{Name: "unrelated", Where: map[string]any{"id": "old"}})
	if got := ids(Arguments{"filter": "either"}); !slices.Equal(got, []string{"old", "live", "text"}) {
		t.Fatal(got)
	}
	if got := ids(Arguments{"filter": "unrelated"}); len(got) != 0 {
		t.Fatal(got)
	}
	if err := r.Settings().Set("list.hide-archived", false); err != nil {
		t.Fatal(err)
	}
	if got := ids(nil); !slices.Equal(got, []string{"old", "legacy", "live", "text"}) {
		t.Fatal(got)
	}
}

func TestArchiveSavedFilterOverrides(t *testing.T) {
	r := NewRuntime()
	b := r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "retired", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}})
	fs, err := r.SavedFilters("", "parcel")
	if err != nil || len(fs) != 1 || !reflect.DeepEqual(fs[0].Where, map[string]any{"retired": true}) {
		t.Fatal(fs, err)
	}
	b.Filters(SavedFilter{Name: "archived", Where: map[string]any{"retired": false}})
	fs, err = r.SavedFilters("", "parcel")
	if err != nil || len(fs) != 1 || fs[0].Where["retired"] != false {
		t.Fatal(fs, err)
	}
	if err := r.Settings().Set("list.filters", `[{"name":"archived","noun":"parcel","where":{"retired":true}}]`); err != nil {
		t.Fatal(err)
	}
	fs, err = r.SavedFilters("", "parcel")
	if err != nil || len(fs) != 1 || fs[0].Where["retired"] != true {
		t.Fatal(fs, err)
	}
}

func TestAgentArchiveRestoreAndUndo(t *testing.T) {
	r, s := newDerivedRuntime(t)
	store := s.store()
	store.Undo = true
	r.Define("task").Store(store)
	restore := r.BindActor(testAgent("p"), false)
	defer restore()
	run := func(verb string, args Arguments) any {
		t.Helper()
		got, err := r.Execute(Request{Noun: "task", Verb: verb, Args: args})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	run("archive", Arguments{"id": "t1"})
	rows := run("list", nil).([]map[string]any)
	if len(rows) != 1 || rows[0]["id"] != "t2" {
		t.Fatal(rows)
	}
	run("restore", Arguments{"id": "t1"})
	rows = run("list", nil).([]map[string]any)
	if len(rows) != 2 || rows[0]["id"] != "t1" {
		t.Fatal(rows)
	}
	if _, err := r.Execute(Request{Noun: "history", Verb: "undo"}); err != nil {
		t.Fatal(err)
	}
	if s.find("t1")["archived"] != true {
		t.Fatal("restore not undone")
	}
	if _, err := r.Execute(Request{Noun: "history", Verb: "undo"}); err != nil {
		t.Fatal(err)
	}
	if s.find("t1")["archived"] != false {
		t.Fatal("archive not undone")
	}
}

// The derived restore offers archived rows as its subject choices, archive
// the live ones, through the subject parameter's data (SourceFixed), so a
// hidden archived row is a choice and its id no longer competes with
// prefix siblings.
func TestArchiveRestoreSubjectChoices(t *testing.T) {
	r := NewRuntime()
	things := []Thing{{"id": "t1", "retired": true}, {"id": "t10"}, {"id": "t11", "retired": false}}
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "retired", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}}).Store(Store{
		List:  func(Ctx, ListQuery) ([]Thing, error) { return things, nil },
		Patch: func(Ctx, string, Thing) (Thing, error) { return nil, nil },
	})
	ids := func(verb string) []string {
		t.Helper()
		op, ok := r.Operations["parcel."+verb]
		if !ok {
			t.Fatal("no", verb)
		}
		choices, err := r.ChoicesFor(r.Context(), op.Parameters["id"], "")
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, c := range choices {
			out = append(out, c.ID)
		}
		return out
	}
	if got := ids("restore"); !slices.Equal(got, []string{"t1"}) {
		t.Fatalf("restore %v", got)
	}
	if got := ids("archive"); !slices.Equal(got, []string{"t10", "t11"}) {
		t.Fatalf("archive %v", got)
	}
	if err := r.CheckSourceArgs(r.Operations["parcel.restore"].Parameters); err != nil {
		t.Fatal(err)
	}
}
