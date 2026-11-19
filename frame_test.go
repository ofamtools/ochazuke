package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// frameFixture is a project noun and a task noun whose list is scoped to
// the pinned project unless all (Store.ListScope, as an application pins
// one): projects p1 and p2, tasks t1 of p1 and t2 of p2, on a Work page,
// with pages.frame listing both. *pinned is the pin ("" is all).
func frameFixture(t *testing.T) (*Runtime, *Model, *string) {
	t.Helper()
	r := NewRuntime()
	r.Config().Disable()
	pinned := "p1"
	r.Define("project").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
	).Store(DocStore(NewMemDocTable(r, "project"), "project"))
	tasks := DocStore(NewMemDocTable(r, "task"), "task")
	tasks.ListScope = []ListScope{{Prop: "project", Value: "pinned"}}
	tasks.ScopeValue = func(_ Ctx, name string) any { return pinned }
	r.Define("task").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "project", Parameter: Parameter{Noun: "project"}},
	).Store(tasks)
	if _, err := r.AddPage("work", "Work", func(*Runtime, int, int) Block { return Text("work") }); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Init()
	m.Width, m.Height = 140, 40
	for _, p := range []Arguments{{"id": "p1", "title": "Alpha"}, {"id": "p2", "title": "Beta"}} {
		mustRun(t, r, Request{Noun: "project", Verb: "create", Args: p})
	}
	for _, tk := range []Arguments{{"id": "t1", "title": "Write", "project": "p1"}, {"id": "t2", "title": "Ship", "project": "p2"}} {
		mustRun(t, r, Request{Noun: "task", Verb: "create", Args: tk})
	}
	if err := r.Settings().Set(FrameSetting, []string{"project", "task"}); err != nil {
		t.Fatal(err)
	}
	return r, m, &pinned
}

// railTasks are the task keys the task frame's rail lists.
func railTasks(r *Runtime) []string {
	var out []string
	for _, it := range r.List(IndexPageID("task") + "/list").Items() {
		if it.Noun == "task" {
			out = append(out, strings.TrimPrefix(it.ID, IndexPageID("task")+"/list/"))
		}
	}
	return out
}

// sectionRow is the row of key in the show body's section of noun.
func sectionRow(t *testing.T, r *Runtime, page, noun, key string) string {
	t.Helper()
	for _, s := range r.ShowFormOf(page + "/show").Related().sections {
		if s.ref.Noun != noun {
			continue
		}
		for _, it := range s.list.Items() {
			if it.Noun == noun && strings.HasSuffix(it.ID, "/"+key) {
				return it.ID
			}
		}
	}
	t.Fatalf("no %s row %s in %s's sections", noun, key, page)
	return ""
}

// pages.frame listing the noun: enter on an index row opens the show body
// (the rail, the props, the related boxes); e edits in place, esc shows it
// again, - goes back to the index body, then to the page it came from.
func TestFrameListed(t *testing.T) {
	r, m, _ := frameFixture(t)
	if err := r.OpenPage("work"); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenIndex("project", "p1"); err != nil {
		t.Fatal(err)
	}
	forceFocus(r, IndexPageID("project")+"/list/p1")
	press(t, r, "enter")
	_, fs := r.frameOf(r.Page)
	if r.Page != IndexPageID("project") || fs.Body != FrameShow {
		t.Fatalf("enter opened %s %+v", r.Page, fs)
	}
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"Alpha", "title", "Tasks (project) (1)", "Write", "History"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("show body lacks %q:\n%s", want, frame)
		}
	}
	press(t, r, "e")
	if fs.Body != FrameEdit || !slices.Contains(r.InstancePath(r.Focus), IndexPageID("project")+"/form") {
		t.Fatalf("e: body %s focus %s", fs.Body, r.Focus)
	}
	press(t, r, "esc")
	if fs.Body != FrameShow || r.Focus != IndexPageID("project")+"/list/p1" {
		t.Fatalf("esc: body %s focus %s", fs.Body, r.Focus)
	}
	press(t, r, "-")
	if fs.Body != FrameIndex {
		t.Fatalf("- left body %s", fs.Body)
	}
	press(t, r, "-")
	if r.Page != "work" {
		t.Fatalf("- again went to %s", r.Page)
	}
}

// pages.frame empty: enter keeps opening today's index, the focus in the
// selected thing's form.
func TestFrameEmpty(t *testing.T) {
	r, _, _ := frameFixture(t)
	if err := r.Settings().Set(FrameSetting, []string{}); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenIndex("project", "p1"); err != nil {
		t.Fatal(err)
	}
	forceFocus(r, IndexPageID("project")+"/list/p1")
	press(t, r, "enter")
	if _, fs := r.frameOf(r.Page); r.Page != IndexPageID("project") || fs.Body != "" || !slices.Contains(r.InstancePath(r.Focus), IndexPageID("project")+"/form") {
		t.Fatalf("enter: page %s body %q focus %s", r.Page, fs.Body, r.Focus)
	}
}

// show.section-columns: 3 draws the boxes side by side, 1 one under the
// other.
func TestFrameSectionColumns(t *testing.T) {
	for _, tc := range []struct{ cols, perLine int }{{3, 2}, {1, 1}} {
		r, m, _ := frameFixture(t)
		if err := r.Settings().Set(ShowSectionColumnsSetting, tc.cols); err != nil {
			t.Fatal(err)
		}
		if err := r.OpenFrame("project", "p1", FrameShow, nil); err != nil {
			t.Fatal(err)
		}
		most := 0
		for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			most = max(most, strings.Count(line, "╭"))
		}
		if most != tc.perLine {
			t.Fatalf("show.section-columns %d: %d boxes on a line:\n%s", tc.cols, most, ansi.Strip(m.View().Content))
		}
	}
}

// Opened directly, the rail lists the pinned project's tasks.
func TestFrameRailPinned(t *testing.T) {
	r, _, _ := frameFixture(t)
	if err := r.OpenFrame("task", "t1", FrameShow, nil); err != nil {
		t.Fatal(err)
	}
	if got := railTasks(r); !slices.Equal(got, []string{"t1"}) {
		t.Fatalf("pinned rail %v", got)
	}
}

// With all on (nothing pinned), the rail lists every project's tasks.
func TestFrameRailAll(t *testing.T) {
	r, _, pinned := frameFixture(t)
	*pinned = ""
	if err := r.OpenFrame("task", "t1", FrameShow, nil); err != nil {
		t.Fatal(err)
	}
	if got := railTasks(r); !slices.Equal(got, []string{"t1", "t2"}) {
		t.Fatalf("all rail %v", got)
	}
}

// enter on a task in a project's tasks box moves the frame to the task:
// its show body, the rail filtered to that project's tasks (an ordinary
// prop filter, replacing the pin's scope on the same prop); - comes back
// to the project's show body on the row it left.
func TestFrameFollowsALink(t *testing.T) {
	r, _, _ := frameFixture(t)
	if err := r.OpenFrame("project", "p2", FrameShow, nil); err != nil {
		t.Fatal(err)
	}
	row := sectionRow(t, r, IndexPageID("project"), "task", "t2")
	forceFocus(r, row)
	press(t, r, "enter")
	if r.Page != IndexPageID("task") {
		t.Fatalf("enter went to %s", r.Page)
	}
	if got := railTasks(r); !slices.Equal(got, []string{"t2"}) {
		t.Fatalf("linked rail %v", got)
	}
	if f := r.List(IndexPageID("task") + "/list").PropFilters(); len(f) != 1 || f[0].Prop != "project" || f[0].Value != "p2" {
		t.Fatalf("rail filters %+v", f)
	}
	press(t, r, "-")
	if r.Page != IndexPageID("project") || r.Focus != row {
		t.Fatalf("- went to %s focus %s", r.Page, r.Focus)
	}
}

// On a row in a related box the row's thing is the top of the focus stack:
// a key its noun binds runs its verb on it, the generated keys (v show) are
// not shadowed by the section's, and the footer lists its keys first.
func TestFrameRowThingKeysFirst(t *testing.T) {
	r, _, _ := frameFixture(t)
	var pinged string
	r.Define("task").Verb("ping", func(c Ctx) Result { pinged = c.Instance.ID; return Done(nil) }, "Ping a task")
	if err := r.Bind(Binding{Mode: "normal", Noun: "task", Keys: "x", Request: Request{Noun: "task", Verb: "ping"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenFrame("project", "p1", FrameShow, nil); err != nil {
		t.Fatal(err)
	}
	row := sectionRow(t, r, IndexPageID("project"), "task", "t1")
	forceFocus(r, row)
	hints := BindingHints(r, row)
	if at, other := strings.Index(hints, "task.ping"), strings.Index(hints, "related-section"); at < 0 || other >= 0 && other < at {
		t.Fatalf("footer does not lead with the task's keys: %s", hints)
	}
	press(t, r, "x")
	if pinged != row {
		t.Fatalf("x pinged %q, want %s", pinged, row)
	}
	press(t, r, "v")
	if r.Page != IndexPageID("task") {
		t.Fatalf("v (task.show) went to %s", r.Page)
	}
}
