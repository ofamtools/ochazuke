package ochazuke

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

// nounPageFixture is an application with one noun of each kind beside the
// framework's own: records (project, task naming its project, with a prop
// it does not declare), a remote record (box, naming a project), a
// singleton (prefs), facts (planet), a command noun (deploy), a part with
// a live instance (widget) and one with none (gadget).
func nounPageFixture(t *testing.T) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	projects := NewMemDocTable(r, "project")
	r.Define("project").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(DocStore(projects, "project"))
	tasks := NewMemDocTable(r, "task")
	r.Define("task").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "project", Parameter: Parameter{Noun: "project"}}).Store(DocStore(tasks, "task"))
	for key, t0 := range map[string]Thing{"p1": {"id": "p1", "title": "Alpha"}} {
		if err := projects.Put(key, t0); err != nil {
			t.Fatal(err)
		}
	}
	for key, t0 := range map[string]Thing{"t1": {"id": "t1", "title": "One", "project": "p1", "extra": "kept"}, "t2": {"id": "t2", "title": "Two", "project": "p1"}} {
		if err := tasks.Put(key, t0); err != nil {
			t.Fatal(err)
		}
	}
	box := &remoteBox{items: map[string]Thing{"b1": {"id": "b1", "title": "Crate", "project": "p1"}}}
	r.Define("box").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "project", Parameter: Parameter{Noun: "project"}}).Store(box.store())
	r.Define("prefs").Props(PropSpec{Name: "theme"}).Store(Store{Kind: Singleton, Get: func(Ctx, string) (Thing, error) { return Thing{"theme": "dark"}, nil }})
	r.defineFacts(factsSpec{Noun: "planet", StableKeys: true, Key: "name", Extra: []PropSpec{{Name: "name"}},
		List: func(Ctx, ListQuery) ([]Thing, error) { return []Thing{{"name": "earth"}}, nil }})
	r.Define("deploy").Verb("run", func(Ctx) Result { return Done(nil) }, "Deploy")
	r.Define("widget").Kind(NounKindPart)
	r.Define("gadget").Kind(NounKindPart)
	r.Instance("w1", Parent("app"), Kind("widget"))
	if _, err := r.AddPage("work", "Work", func(*Runtime, int, int) Block { return Text("work") }); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Init()
	m.Width, m.Height = 120, 40
	return r, m
}

// branchIDs are the ids of a related stack's sections but the history's.
func branchIDs(rel *Related) []string {
	var out []string
	for _, s := range rel.sections {
		if !s.ref.History {
			out = append(out, s.ref.ID())
		}
	}
	slices.Sort(out)
	return out
}

// wantBranches are the relations a thing of noun has one step away: every
// referrer (a relation kind's only while relations.sections is on) and
// every own reference, of a noun the registry has.
func wantBranches(r *Runtime, noun string) []string {
	var out []string
	refs := append(r.Referrers(noun), r.OwnReferences(noun)...)
	for _, ref := range refs {
		if r.Nouns[ref.Noun] == nil || ref.Relation != "" && !r.Settings().Bool(RelationSectionsSetting) {
			continue
		}
		out = append(out, ref.ID())
	}
	slices.Sort(out)
	return out
}

// Every noun of the registry, the framework's and the application's of
// every kind, opens the same page: a list with a row (or the empty line of
// a store with no things), the first row's props, and one branch per
// relation one step away.
func TestEveryNounOpensItsPage(t *testing.T) {
	r, m := nounPageFixture(t)
	seen := map[string]int{}
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		if err := r.OpenIndex(name, ""); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		landLoads(m, r.PendingCommands())
		id := IndexPageID(name)
		list := r.List(id + "/list")
		a := r.NounAnswers(name)
		seen[a.Rows]++
		if list == nil {
			t.Errorf("%s: no list", name)
			continue
		}
		if list.Len() == 0 {
			if view := ansi.Strip(list.View(80, 4)); a.Rows != NounRowsStore || !strings.Contains(view, "no ") {
				t.Errorf("%s (%s): no rows and no empty line: %q", name, a.Rows, view)
			}
			continue
		}
		item := list.Items()[0]
		thing, _ := item.Data.(Thing)
		if len(r.ThingProps(item.Noun, thing)) == 0 {
			t.Errorf("%s: row %s shows no props", name, item.ID)
		}
		rel := r.ShowFormOf(id + "/show").Related()
		if noun := rel.Noun(); noun != "" {
			if got, want := branchIDs(rel), wantBranches(r, noun); !slices.Equal(got, want) {
				t.Errorf("%s: branches %v, want %v", name, got, want)
			}
		}
	}
	for _, rows := range []string{NounRowsStore, NounRowsUnkeyed, NounRowsInstances, NounRowsDeclaration} {
		if seen[rows] == 0 {
			t.Errorf("no noun opened with %s rows: %v", rows, seen)
		}
	}
	t.Logf("rows by source: %v", seen)
}

// A thing's page shows its props, undeclared ones too, and its relations
// in both directions with their counts: the tasks and the remote boxes
// that name a project (read as a job), the project a task names.
func TestNounPageRelationsBothWays(t *testing.T) {
	r, m := nounPageFixture(t)
	mustRun(t, r, Request{Noun: "ochazuke.noun", Verb: "show", Args: Arguments{"noun": "project", "key": "p1"}})
	landLoads(m, r.PendingCommands())
	rel := r.ShowFormOf(IndexPageID("project") + "/show").Related()
	for id, want := range map[string]int{"task.project": 2, "box.project": 1} {
		if s := rel.byID[id]; s == nil || s.count != want || !s.collapsed() {
			t.Fatalf("section %s: %+v (want %d, folded)", id, s, want)
		}
	}
	mustRun(t, r, Request{Noun: "ochazuke.noun", Verb: "show", Args: Arguments{"noun": "task", "key": "t1"}})
	rel = r.ShowFormOf(IndexPageID("task") + "/show").Related()
	if s := rel.byID["task.project>"]; s == nil || s.count != 1 || s.shown[0]["title"] != "Alpha" {
		t.Fatalf("the task's own project: %+v", s)
	}
	props := r.ThingProps("task", Thing{"id": "t1", "title": "One", "project": "p1", "extra": "kept"})
	if last := props[len(props)-1]; last.Name != "extra" || last.Declared || last.Value != "kept" {
		t.Fatalf("undeclared props come last: %+v", props)
	}
	if _, err := r.Settings().SetSession(IndexRelatedOutwardSetting, false); err != nil {
		t.Fatal(err)
	}
	if rel.Noun(); rel.byID["task.project>"] != nil {
		t.Fatal("index.related-outward off drops the own references")
	}
	r2, _ := nounPageFixture(t)
	if _, err := r2.Settings().SetSession(RelatedRemoteSetting, false); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r2, Request{Noun: "ochazuke.noun", Verb: "show", Args: Arguments{"noun": "project", "key": "p1"}})
	if s := r2.ShowFormOf(IndexPageID("project") + "/show").Related().byID["box.project"]; s == nil || s.count != 0 || r2.PendingCommands() != nil {
		t.Fatalf("detail.related-remote off reads the remote section as empty: %+v", s)
	}
}

// The Modes page is the noun page of ochazuke.mode: enter on its row of
// the Nouns page shows every mode; a branch unfolds to the bindings of the
// mode, g f jumps to one on the bindings' page, and - comes back to it.
func TestModesPageWalk(t *testing.T) {
	r, m := nounPageFixture(t)
	if err := r.OpenPage(NounsPageID); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus(nounsRowPrefix + "ochazuke.mode"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "enter")
	page := IndexPageID("ochazuke.mode")
	if r.Page != page {
		t.Fatalf("enter on the mode noun: page %s", r.Page)
	}
	if got := r.List(page + "/list").Len(); got != len(r.allModeNames()) || got < 2 {
		t.Fatalf("every mode: %d rows, %d modes", got, len(r.allModeNames()))
	}
	if err := r.List(page + "/list").Select(page + "/list/normal"); err != nil {
		t.Fatal(err)
	}
	r.syncIndexForm()
	rel := r.ShowFormOf(page + "/show").Related()
	rel.Noun() // in step with the selection, as a frame would be
	s := rel.byID["ochazuke.binding.mode"]
	if s == nil || s.count == 0 || !s.collapsed() {
		t.Fatalf("the bindings branch, folded with a count: %+v", s)
	}
	if err := r.SetFocus(s.headID()); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "enter")
	if s.collapsed() || s.list.Len() != s.count+1 {
		t.Fatalf("enter unfolds the branch: %d rows for %d", s.list.Len(), s.count)
	}
	keytest.Press(m, "j")
	row := r.Focus
	keytest.Press(m, "g")
	keytest.Press(m, "f")
	if r.Page != IndexPageID("ochazuke.binding") {
		t.Fatalf("g f on a binding jumps to the bindings' page: %s (focus %s)", r.Page, r.Focus)
	}
	keytest.Press(m, "-")
	if r.Page != page || r.Focus != row {
		t.Fatalf("- comes back: page %s focus %s, want %s", r.Page, r.Focus, row)
	}
	keytest.Press(m, "-")
	if r.Page != NounsPageID {
		t.Fatalf("- again goes back to the Nouns page: %s", r.Page)
	}
}
