package ochazuke

import (
	"reflect"
	"slices"
	"testing"
)

func TestArchiveListVisibilityAndLimit(t *testing.T) {
	r := NewRuntime()
	things := []Thing{{"id": "old", "retired": true}, {"id": "legacy"}, {"id": "live", "retired": false}, {"id": "text", "retired": "true"}}
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "retired", Aliases: []string{"hidden"}, Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}}).Store(Store{List: func(Ctx, ListQuery) ([]Thing, error) { return things, nil }})
	ids := func(args Arguments) []string {
		t.Helper()
		v, err := r.Execute(Request{Noun: "parcel", Verb: "list", Args: args})
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, row := range AnswerRows(v).([]map[string]any) {
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
	if err := r.Settings().Set("collection.hide-archived", false); err != nil {
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
	if err := r.Settings().Set("collection.filters", `[{"name":"archived","noun":"parcel","where":{"retired":true}}]`); err != nil {
		t.Fatal(err)
	}
	fs, err = r.SavedFilters("", "parcel")
	if err != nil || len(fs) != 1 || fs[0].Where["retired"] != true {
		t.Fatal(fs, err)
	}
}

func TestAgentArchiveUnarchiveAndUndo(t *testing.T) {
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
	rows := AnswerRows(run("list", nil)).([]map[string]any)
	if len(rows) != 1 || rows[0]["id"] != "t2" {
		t.Fatal(rows)
	}
	run("set", Arguments{"id": "t1", "props": map[string]any{"@archive": false}}) // un-archive
	rows = AnswerRows(run("list", nil)).([]map[string]any)
	if len(rows) != 2 || rows[0]["id"] != "t1" {
		t.Fatal(rows)
	}
	if _, err := r.Execute(Request{Noun: "history-entry", Verb: "undo"}); err != nil {
		t.Fatal(err)
	}
	if s.find("t1")["archived"] != true {
		t.Fatal("un-archive not undone")
	}
	if _, err := r.Execute(Request{Noun: "history-entry", Verb: "undo"}); err != nil {
		t.Fatal(err)
	}
	if s.find("t1")["archived"] != false {
		t.Fatal("archive not undone")
	}
}

// archive's subject choices are the live things: the default list hides
// the archived ones, so an archived id never competes with its prefix
// siblings.
func TestArchiveSubjectChoices(t *testing.T) {
	r := NewRuntime()
	things := []Thing{{"id": "t1", "retired": true}, {"id": "t10"}, {"id": "t11", "retired": false}}
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "retired", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}}).Store(Store{
		List:  func(Ctx, ListQuery) ([]Thing, error) { return things, nil },
		Patch: func(Ctx, string, Thing) (Thing, error) { return nil, nil },
	})
	choices, err := r.ChoicesFor(r.Context(), r.Operations.At(Key("parcel.archive")).Parameters["id"], "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range choices {
		got = append(got, c.ID)
	}
	if !slices.Equal(got, []string{"t10", "t11"}) {
		t.Fatalf("archive %v", got)
	}
}

// A list whose rows are all hidden keeps its row noun, declared (RowNoun) or
// from its last rows, so the noun's saved filters still apply; the app's
// OnFilter refills it from the archived filter. Flat lists and trees alike.
func TestEmptyListKeepsRowNounForSavedFilters(t *testing.T) {
	r := NewRuntime()
	r.Define("parcel").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "retired", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}})
	archived := Thing{"id": "old", "retired": true}
	t.Run("flat declared", func(t *testing.T) {
		l, err := r.AddList("flat-declared", nil, RowNoun("parcel"))
		if err != nil {
			t.Fatal(err)
		}
		l.OnFilter(func(l *Collection) {
			if len(l.PropFilters()) > 0 {
				_ = l.SetItems([]ListItem{{ID: "old", Noun: "parcel", Data: archived}})
			}
		})
		if err := l.ApplyFilter("archived"); err != nil {
			t.Fatal(err)
		}
		if got := shownIDs(l); !slices.Equal(got, []string{"old"}) {
			t.Fatal(got)
		}
	})
	t.Run("flat last rows", func(t *testing.T) {
		l, err := r.AddList("flat-last", []ListItem{{ID: "live", Noun: "parcel", Data: Thing{"id": "live"}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := l.SetItems(nil); err != nil {
			t.Fatal(err)
		}
		l.OnFilter(func(l *Collection) {
			if len(l.PropFilters()) > 0 {
				_ = l.SetItems([]ListItem{{ID: "old2", Noun: "parcel", Data: archived}})
			}
		})
		if err := l.ApplyFilter("archived"); err != nil {
			t.Fatal(err)
		}
		if got := shownIDs(l); !slices.Equal(got, []string{"old2"}) {
			t.Fatal(got)
		}
	})
	t.Run("tree", func(t *testing.T) {
		tree, err := r.AddTree("tree", []TreeNode{{ID: "live", Label: "live", Noun: "parcel", Data: Thing{"id": "live"}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := tree.SetNodes(nil); err != nil {
			t.Fatal(err)
		}
		tree.list.OnFilter(func(l *Collection) {
			if len(l.PropFilters()) > 0 {
				_ = tree.SetNodes([]TreeNode{{ID: "old3", Label: "old", Noun: "parcel", Data: archived}})
			}
		})
		if err := tree.list.ApplyFilter("archived"); err != nil {
			t.Fatal(err)
		}
		if got := shownIDs(tree.list); !slices.Equal(got, []string{"old3"}) {
			t.Fatal(got)
		}
	})
}
