package ochazuke

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

// relMem is an in-memory store of things in order, keyed by "id".
type relMem struct {
	rows []Thing
	next int
}

func (m *relMem) add(kv ...any) {
	t := Thing{}
	for i := 0; i < len(kv); i += 2 {
		t[kv[i].(string)] = kv[i+1]
	}
	m.rows = append(m.rows, t)
}

func (m *relMem) find(id string) int {
	for i, t := range m.rows {
		if fmt.Sprint(t["id"]) == id {
			return i
		}
	}
	return -1
}

func (m *relMem) store() Store {
	return Store{Kind: KindCollection, Undo: true, AcceptsKey: true,
		List: func(Ctx, ListQuery) ([]Thing, error) {
			out := make([]Thing, len(m.rows))
			for i, t := range m.rows {
				out[i] = maps.Clone(t)
			}
			return out, nil
		},
		Get: func(_ Ctx, id string) (Thing, error) {
			if i := m.find(id); i >= 0 {
				return maps.Clone(m.rows[i]), nil
			}
			return nil, fmt.Errorf("no thing %q", id)
		},
		Create: func(_ Ctx, v Thing) (Thing, error) {
			t := maps.Clone(v)
			if id, _ := t["id"].(string); id == "" || m.find(id) >= 0 {
				m.next++
				t["id"] = fmt.Sprintf("new%d", m.next)
			}
			m.rows = append(m.rows, t)
			return maps.Clone(t), nil
		},
		Patch: func(_ Ctx, id string, ch Thing) (Thing, error) {
			i := m.find(id)
			if i < 0 {
				return nil, fmt.Errorf("no thing %q", id)
			}
			maps.Copy(m.rows[i], ch)
			return maps.Clone(m.rows[i]), nil
		},
		Delete: func(_ Ctx, id string) error {
			if i := m.find(id); i >= 0 {
				m.rows = append(m.rows[:i], m.rows[i+1:]...)
			}
			return nil
		},
	}
}

// relFixture is a stack over a project noun with three referencing nouns:
// task (owner, a table), note (a plain reference, a list), link (no store;
// rows through the Rows hook), plus read-only "origin" on notes.
type relFixture struct {
	r      *Runtime
	m      *Model
	rel    *Related
	key    string
	tasks  *relMem
	notes  *relMem
	links  []Thing
	height int
}

func newRelFixture(t *testing.T, mod func(*RelatedSpec)) *relFixture {
	t.Helper()
	return newRelFixtureWith(t, nil, mod)
}

// newRelFixtureWith runs init once the settings are declared, before the
// stack is made.
func newRelFixtureWith(t *testing.T, init func(*Runtime), mod func(*RelatedSpec)) *relFixture {
	t.Helper()
	f := &relFixture{key: "p1", tasks: &relMem{}, notes: &relMem{}, height: 30}
	r := NewRuntime()
	f.r = r
	projects := &relMem{}
	projects.add("id", "p1", "name", "Alpha")
	projects.add("id", "p2", "name", "Beta")
	r.Define("project").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel}).Store(projects.store())
	f.tasks.add("id", "t1", "title", "write", "project", "p1")
	f.tasks.add("id", "t2", "title", "test", "project", "p1")
	f.tasks.add("id", "t3", "title", "ship", "project", "p2")
	r.Define("task").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "project", Role: RoleOwner, Parameter: Parameter{Noun: "project"}}).
		Columns(Column{Title: "id", Width: 4}, Column{Title: "title", Width: 10}).Store(f.tasks.store())
	f.notes.add("id", "n1", "title", "idea", "project", "p1", "origin", "p2")
	f.notes.add("id", "n2", "title", "memo", "project", "p2", "origin", "p2")
	r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "project", Parameter: Parameter{Noun: "project"}},
		PropSpec{Name: "origin", ReadOnly: true, Parameter: Parameter{Noun: "project"}}).Store(f.notes.store())
	f.links = []Thing{{"id": "l1", "name": "docs", "project": "p1"}, {"id": "l2", "name": "repo", "project": "p2"}}
	r.Define("link").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel},
		PropSpec{Name: "project", Role: RoleOwner, Parameter: Parameter{Noun: "project"}})
	for _, s := range []Setting{
		{Name: "rel.order", Type: SettingList, Default: []string{}},
		{Name: "rel.hidden", Type: SettingList, Default: []string{}},
		{Name: "rel.collapsed", Type: SettingList, Default: []string{}},
		{Name: "rel.counts", Type: SettingBoolean, Default: true},
		{Name: "rel.empty", Type: SettingString, Default: RelatedEmptyHeader},
	} {
		if err := r.DeclareSetting(s); err != nil {
			t.Fatal(err)
		}
	}
	if init != nil {
		init(r)
	}
	r.Instance("page")
	spec := RelatedSpec{Noun: "project", Key: func() string { return f.key },
		Order: "rel.order", Hidden: "rel.hidden", Collapsed: "rel.collapsed", Counts: "rel.counts", Empty: "rel.empty",
		Rows: func(_ Ctx, ref Referrer, key string) ([]Thing, error) {
			var out []Thing
			for _, l := range f.links {
				if l[ref.Prop] == key {
					out = append(out, maps.Clone(l))
				}
			}
			return out, nil
		}}
	if mod != nil {
		mod(&spec)
	}
	rel, err := r.AddRelated("page/details", spec, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	f.rel = rel
	if _, err := r.AddPage("page", "Page", func(r *Runtime, w, h int) Block { return Text(rel.View(w, h)) }, Focus(rel.FocusTarget())); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	f.m = NewModel(r)
	// An application binds u to the undo, as matomeru does.
	if err := r.Bind(Binding{Noun: "app", Mode: "normal", Keys: "u", Request: Request{Noun: "ochazuke.history-entry", Verb: "undo"}}); err != nil {
		t.Fatal(err)
	}
	f.m.Width, f.m.Height = 60, f.height
	f.frame()
	return f
}

// frame draws the page and returns the stack's text.
func (f *relFixture) frame() string {
	f.m.View()
	return ansi.Strip(f.rel.View(60, f.height-6))
}

func (f *relFixture) press(keys ...string) {
	for _, seq := range keys {
		for _, k := range strings.Fields(seq) {
			keytest.Press(f.m, k)
			f.m.View()
		}
	}
}

func (f *relFixture) focus() string { return f.r.Focus }

func (f *relFixture) mustHave(t *testing.T, want ...string) {
	t.Helper()
	text := f.frame()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Fatalf("frame lacks %q:\n%s", w, text)
		}
	}
}

func TestRelatedSectionsDrawFromPropsAlone(t *testing.T) {
	t.Parallel()
	f := newRelFixture(t, nil)
	f.mustHave(t, "▾ Tasks (2)", "write", "test", "▾ Notes (project) (1)", "idea", "▸ Notes (origin) (0)", "▾ Links (1)", "docs")
	if strings.Contains(f.frame(), "ship") {
		t.Fatalf("another project's task shows:\n%s", f.frame())
	}
	rows, err := f.r.Execute(Request{Noun: RelatedNoun, Verb: "list"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range AnswerRows(rows).([]map[string]any) {
		ids = append(ids, fmt.Sprint(row["id"], ":", row["count"]))
	}
	if got, want := strings.Join(ids, " "), "task.project:2 note.project:1 note.origin:0 link.project:1"; got != want {
		t.Fatalf("sections %q, want %q", got, want)
	}
}

func (f *relFixture) selected(section string) string {
	it, _ := f.rel.List(section).Selected()
	return it.ID
}

func TestRelatedMotionsSpillAcrossSections(t *testing.T) {
	t.Parallel()
	f := newRelFixture(t, nil)
	tasks := f.rel.List("task.project").id
	if f.focus() != tasks+"/@head" {
		t.Fatalf("starts on %q, want the first heading", f.focus())
	}
	f.press("j", "j")
	if f.focus() != tasks+"/t2" {
		t.Fatalf("after j j: %q", f.focus())
	}
	f.press("j")
	notes := f.rel.List("note.project").id
	if f.focus() != notes+"/@head" {
		t.Fatalf("j past the last task: %q, want the next section's heading", f.focus())
	}
	f.press("k")
	if f.focus() != tasks+"/t2" {
		t.Fatalf("k back: %q", f.focus())
	}
	f.press("] ]")
	if f.focus() != notes+"/@head" {
		t.Fatalf("] ]: %q", f.focus())
	}
	f.press("[ [", "[ [")
	if f.focus() != tasks+"/@head" {
		t.Fatalf("[ [ [ [: %q (no wrap past the first)", f.focus())
	}
	f.press("}")
	if f.focus() != notes+"/@head" {
		t.Fatalf("} walks stops: %q", f.focus())
	}
}

func TestRelatedFoldKeysAndAll(t *testing.T) {
	t.Parallel()
	f := newRelFixture(t, nil)
	f.press("j", "z a")
	f.mustHave(t, "▸ Tasks (2)")
	if strings.Contains(f.frame(), "write") {
		t.Fatalf("a folded section shows its things:\n%s", f.frame())
	}
	if f.focus() != f.rel.List("task.project").id+"/@head" {
		t.Fatalf("a fold from a row leaves the focus on the heading: %q", f.focus())
	}
	f.press("enter")
	f.mustHave(t, "▾ Tasks (2)", "write")
	f.press("z c")
	f.press("z o")
	f.mustHave(t, "write")
	f.press("z M")
	if text := f.frame(); strings.Contains(text, "write") || strings.Contains(text, "idea") || strings.Contains(text, "docs") {
		t.Fatalf("z M leaves things showing:\n%s", text)
	}
	f.press("z R")
	f.mustHave(t, "write", "idea", "docs")
}

func TestRelatedSettings(t *testing.T) {
	t.Parallel()
	f := newRelFixture(t, nil)
	set := func(name string, v any) {
		t.Helper()
		if err := f.r.Settings().Set(name, v); err != nil {
			t.Fatal(err)
		}
	}
	order := func() string {
		var ids []string
		for _, row := range AnswerRows(must(f.r.Execute(Request{Noun: RelatedNoun, Verb: "list"}))).([]map[string]any) {
			ids = append(ids, fmt.Sprint(row["id"]))
		}
		return strings.Join(ids, " ")
	}
	set("rel.order", []string{"link.project", "task.project"})
	if got, want := order(), "link.project task.project note.project note.origin"; got != want {
		t.Fatalf("order %q, want %q", got, want)
	}
	if text := f.frame(); strings.Index(text, "Links") > strings.Index(text, "Tasks") {
		t.Fatalf("Links should draw before Tasks:\n%s", text)
	}
	set("rel.hidden", []string{"note.*"})
	if text := f.frame(); strings.Contains(text, "Notes") {
		t.Fatalf("hidden notes show:\n%s", text)
	}
	set("rel.hidden", []string{})
	set("rel.counts", false)
	f.mustHave(t, "▾ Tasks", "▸ Notes (origin)")
	if strings.Contains(f.frame(), "Tasks (") {
		t.Fatalf("counts off still counts:\n%s", f.frame())
	}
	set("rel.counts", true)
	set("rel.empty", RelatedEmptyOmit)
	if text := f.frame(); strings.Contains(text, "origin") {
		t.Fatalf("hide empty shows the empty section:\n%s", text)
	}
	set("rel.empty", RelatedEmptyHint)
	f.mustHave(t, "▸ Notes (origin) (0)", "no notes")
	set("rel.empty", RelatedEmptyHeader)
	if text := f.frame(); strings.Contains(text, "no notes") {
		t.Fatalf("header mode shows an empty row:\n%s", text)
	}
}

// Collapsed starts a section folded; the fold afterwards is view state.
func TestRelatedCollapsedStartsFromTheSetting(t *testing.T) {
	t.Parallel()
	f := newRelFixtureWith(t, func(r *Runtime) {
		if err := r.Settings().Set("rel.collapsed", []string{"task.project"}); err != nil {
			t.Fatal(err)
		}
	}, nil)
	if text := f.frame(); !strings.Contains(text, "▸ Tasks (2)") || strings.Contains(text, "write") {
		t.Fatalf("tasks start folded:\n%s", text)
	}
	f.press("enter")
	f.mustHave(t, "▾ Tasks (2)", "write")
	if err := f.r.Settings().Set("rel.collapsed", []string{"task.project", "note.project"}); err != nil {
		t.Fatal(err)
	}
	f.mustHave(t, "write") // the setting started the section; a change of it does not refold it
}

func must(v any, err error) any {
	if err != nil {
		panic(err)
	}
	return v
}

func (f *relFixture) taskProject(id string) any { return f.tasks.rows[f.tasks.find(id)]["project"] }

func TestRelatedNewAssociateUnassociateUndo(t *testing.T) {
	t.Parallel()
	f := newRelFixture(t, nil)
	// n on a heading: the noun's create with the referring prop filled.
	f.press("n")
	if len(f.tasks.rows) != 4 || f.tasks.rows[3]["project"] != "p1" {
		t.Fatalf("n did not create a task owned by p1: %v", f.tasks.rows)
	}
	f.mustHave(t, "▾ Tasks (3)")
	// associate with a named thing, then undo.
	if _, err := f.r.Execute(Request{Noun: RelatedNoun, Verb: "associate", Args: Arguments{"section": "task.project", "thing": "t3"}}); err != nil {
		t.Fatal(err)
	}
	if f.taskProject("t3") != "p1" {
		t.Fatalf("t3 project %v", f.taskProject("t3"))
	}
	f.mustHave(t, "▾ Tasks (4)", "ship")
	f.press("u")
	if f.taskProject("t3") != "p2" {
		t.Fatalf("u did not undo the associate: %v", f.taskProject("t3"))
	}
	f.mustHave(t, "▾ Tasks (3)")
	// d u on a row clears the referring prop: one undo step restores it.
	f.press("j", "d", "u")
	if f.taskProject("t1") != "" {
		t.Fatalf("d u left t1 owned: %v", f.taskProject("t1"))
	}
	if text := f.frame(); strings.Contains(text, "write") {
		t.Fatalf("t1 still in the section:\n%s", text)
	}
	if f.tasks.find("t1") < 0 {
		t.Fatal("unassociate deleted the task")
	}
	f.press("u")
	if f.taskProject("t1") != "p1" {
		t.Fatalf("u did not undo the unassociate: %v", f.taskProject("t1"))
	}
	f.mustHave(t, "write")
}

func TestRelatedUnassociateReadOnlyPropSaysWhy(t *testing.T) {
	t.Parallel()
	f := newRelFixture(t, nil)
	_, err := f.r.Execute(Request{Noun: RelatedNoun, Verb: "unassociate", Args: Arguments{"section": "note.origin"}})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("unassociate of a read-only prop: %v", err)
	}
	_, err = f.r.Execute(Request{Noun: RelatedNoun, Verb: "associate", Args: Arguments{"section": "note.origin", "thing": "n1"}})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("associate of a read-only prop: %v", err)
	}
}
