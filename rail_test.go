package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

// railFixture is a page whose main area is a list of projects and whose
// rail lists the selected project's things: tasks (owner prop, split by
// kind), notes (a plain reference) and links (no store: the Rows hook,
// counted so a test can see when it is read).
type railFixture struct {
	r     *Runtime
	m     *Model
	rail  *Rail
	tasks *relMem
	notes *relMem
	hooks int
	width int
	last  string
}

func newRailFixture(t *testing.T, width int, mod func(*RailSpec), init func(*Runtime)) *railFixture {
	t.Helper()
	f := &railFixture{tasks: &relMem{}, notes: &relMem{}, width: width}
	r := NewRuntime()
	f.r = r
	projects := &relMem{}
	projects.add("id", "p1", "name", "Alpha")
	projects.add("id", "p2", "name", "Beta")
	r.Define("project").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel}).Store(projects.store())
	f.tasks.add("id", "t1", "title", "write", "kind", "link", "project", "p1")
	f.tasks.add("id", "t2", "title", "test", "kind", "file", "project", "p1")
	f.tasks.add("id", "t3", "title", "ship", "kind", "link", "project", "p1")
	f.tasks.add("id", "t4", "title", "plan", "kind", "file", "project", "p2", "today", true)
	r.Define("task").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel}, PropSpec{Name: "kind"},
		PropSpec{Name: "today", Parameter: Parameter{Type: "boolean"}},
		PropSpec{Name: "project", Role: RoleOwner, Parameter: Parameter{Noun: "project"}}).
		Filters(SavedFilter{Name: "today", Where: map[string]any{"today": true}}).Store(f.tasks.store())
	f.notes.add("id", "n1", "title", "idea", "project", "p1")
	r.Define("note").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "title", Role: RoleLabel},
		PropSpec{Name: "project", Parameter: Parameter{Noun: "project"}}).Store(f.notes.store())
	r.Define("link").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel},
		PropSpec{Name: "project", Role: RoleOwner, Parameter: Parameter{Noun: "project"}})
	if init != nil {
		init(r)
	}
	r.Instance("page")
	left, err := r.AddList("page/left", []ListItem{{ID: "p1", Label: "Alpha", Noun: "project", Data: Thing{"id": "p1"}}, {ID: "p2", Label: "Beta", Noun: "project", Data: Thing{"id": "p2"}}}, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	spec := RailSpec{Stack: RelatedSpec{Target: r.SelectionTarget(func() *Collection { return left }),
		Rows: func(_ Ctx, ref Referrer, key string) ([]Thing, error) {
			f.hooks++
			if key == "p1" {
				return []Thing{{"id": "l1", "name": "docs", "project": "p1"}}, nil
			}
			return nil, nil
		}}}
	if mod != nil {
		mod(&spec)
	}
	f.rail, err = r.AddRail("page/rail", spec, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	styles := NewStyles(Appearance{})
	if _, err := r.AddPage("page", "Page", func(r *Runtime, w, h int) Block {
		fit := f.rail.Fit(w, h)
		main := Text(left.View(fit.MainWidth, fit.MainHeight))
		b := f.rail.Compose(Layout{Width: w, Height: h, Styles: styles}, fit, main)
		f.last = b.Text
		return b
	}, Focus(left.FocusTarget())); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	f.m = NewModel(r)
	f.m.Width, f.m.Height = width, 20
	f.m.View()
	return f
}

func (f *railFixture) set(t *testing.T, name string, v any) {
	t.Helper()
	if err := f.r.Settings().Set(name, v); err != nil {
		t.Fatal(err)
	}
	f.m.View()
}

// body is the page body as drawn, without styles.
func (f *railFixture) body() string {
	f.m.View()
	return ansi.Strip(f.last)
}

func (f *railFixture) press(keys ...string) {
	for _, seq := range keys {
		for _, k := range strings.Fields(seq) {
			keytest.Press(f.m, k)
			f.m.View()
		}
	}
}

// The width steps: the preferred column while the main keeps
// rail.min-main, then 28 cells, then the float, then the strip; a
// focused strip draws as the float; pane.only gives the rail the area.
func TestFitRailSteps(t *testing.T) {
	t.Parallel()
	params := RailParams{Look: RailLookFull, Width: "25%", MinMain: 60, FloatHeight: 50, Lines: 9, HeadLines: 3}
	for _, tc := range []struct {
		name  string
		width int
		edit  func(*RailParams)
		want  string
	}{
		{"200 full at 25%", 200, nil, "full 50 main 149"},
		{"120 full at 25%", 120, nil, "full 30 main 89"},
		{"90 full at the 28 floor", 90, nil, "full 28 main 61"},
		{"100 full stepped to 28", 100, func(p *RailParams) { p.Width = "40%" }, "full 28 main 71 narrow"},
		{"80 float", 80, nil, "float 28 h 5 main 80 narrow"},
		{"80 float focused grows", 80, func(p *RailParams) { p.Focused = true }, "float 28 h 10 main 80 narrow"},
		{"40 strip", 40, nil, "strip 40 main 19 narrow"},
		{"40 focused strip floats", 40, func(p *RailParams) { p.Focused = true }, "float 24 h 10 main 40 narrow"},
		{"120 float look", 120, func(p *RailParams) { p.Look = RailLookFloat }, "float 30 h 5 main 120"},
		{"cells width", 200, func(p *RailParams) { p.Width = "36" }, "full 36 main 163"},
		{"only", 120, func(p *RailParams) { p.Look = RailLookOnly }, "alone 120 main 0"},
	} {
		p := params
		if tc.edit != nil {
			tc.edit(&p)
		}
		fit := FitRail(tc.width, 20, p)
		got := fmt.Sprintf("%s %d", fit.Look, fit.RailWidth)
		if fit.Look == RailLookFloat {
			got += fmt.Sprintf(" h %d", fit.RailHeight)
		}
		got += fmt.Sprintf(" main %d", fit.MainWidth)
		if fit.Look == RailLookStrip {
			got = fmt.Sprintf("%s %d main %d", fit.Look, fit.RailWidth, fit.MainHeight)
		}
		if fit.Narrow {
			got += " narrow"
		}
		if got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// One golden per look: the column beside the main, and the float over
// its corner with the headings alone.
func TestRailLooks(t *testing.T) {
	t.Parallel()
	f := newRailFixture(t, 120, func(s *RailSpec) { s.Stack.GroupBy = map[string]string{"task.project": "kind"} },
		func(r *Runtime) {
			_ = r.DeclareSetting(Setting{Name: RailCollapsedSetting, Type: SettingList, Default: []string{"*", "!task.kind=link"}})
		})
	full := f.body()
	for _, want := range []string{"╭─ rail", "▾ Links (2)", "write", "ship", "▸ Files (1)", "▸ Notes (project) (1)", "▸ Links (1)"} {
		if !strings.Contains(full, want) {
			t.Fatalf("full look lacks %q:\n%s", want, full)
		}
	}
	if line := strings.Split(full, "\n")[0]; !strings.HasPrefix(line, "Alpha") || !strings.Contains(line, " ╭─ rail") {
		t.Fatalf("the main is not left of the column:\n%s", full)
	}
	f.set(t, RailLookSetting, RailLookFloat)
	float := f.body()
	lines := strings.Split(float, "\n")
	if !strings.HasPrefix(lines[0], "Alpha") || !strings.Contains(lines[0], "╭─ rail") || strings.Contains(float, "write") {
		t.Fatalf("float look: headings over the main's corner, no rows while unfocused:\n%s", float)
	}
	if got := len(strings.TrimSpace(lines[6])); got != 0 && strings.Contains(lines[6], "│") {
		t.Fatalf("the float is taller than its headings:\n%s", float)
	}
}

// Focus reaches the rail by } and leaves by {; j and k stay at its first
// and last rows (rail.ends stay) and spill with spill; h on a row folds
// its section to the heading, l on a heading unfolds it: all four edges.
func TestRailFocusAndEdges(t *testing.T) {
	t.Parallel()
	f := newRailFixture(t, 120, nil, func(r *Runtime) {
		_ = r.DeclareSetting(Setting{Name: RailCollapsedSetting, Type: SettingList, Default: []string{}})
	})
	f.press("}")
	if !f.r.focusInside(f.rail.ID()) {
		t.Fatalf("} did not reach the rail: %s", f.r.Focus)
	}
	first := f.r.Focus
	f.press("k")
	if f.r.Focus != first {
		t.Fatalf("k at the rail's first row left it: %s", f.r.Focus)
	}
	f.press("j")
	row := f.r.Focus
	f.press("h")
	if f.r.Focus != first || strings.Contains(f.body(), "write") {
		t.Fatalf("h on a row did not fold its section to the heading: %s\n%s", f.r.Focus, f.body())
	}
	f.press("l")
	if !strings.Contains(f.body(), "write") {
		t.Fatalf("l on a heading did not unfold it:\n%s", f.body())
	}
	_ = row
	f.press("G")
	for range 10 {
		f.press("j")
	}
	if !f.r.focusInside(f.rail.ID()) {
		t.Fatalf("j at the rail's last row left it: %s", f.r.Focus)
	}
	f.set(t, RailEndsSetting, "spill")
	if err := f.r.SetFocus(first); err != nil {
		t.Fatal(err)
	}
	f.press("k")
	if f.r.focusInside(f.rail.ID()) {
		t.Fatalf("with rail.ends spill, k at the first row stays in the rail: %s", f.r.Focus)
	}
	f.press("}")
	f.press("{")
	if f.r.focusInside(f.rail.ID()) {
		t.Fatalf("{ did not leave the rail: %s", f.r.Focus)
	}
}

// One test per setting value the rail adds: show on and off (off reads
// nothing), counts, empty hide and header, the groups heading, and the
// collapsed negation.
func TestRailSettingValues(t *testing.T) {
	t.Parallel()
	f := newRailFixture(t, 120, nil, nil)
	if !strings.Contains(f.body(), "▸ Tasks (3)") {
		t.Fatalf("rail.collapsed * folds every section:\n%s", f.body())
	}
	f.set(t, RailCountsSetting, false)
	if strings.Contains(f.body(), "Tasks (3)") || !strings.Contains(f.body(), "▸ Tasks") {
		t.Fatalf("rail.counts off still counts:\n%s", f.body())
	}
	f.set(t, RailCountsSetting, true)
	f.set(t, RailGroupsSetting, []string{"needs-you = task, note"})
	if body := f.body(); !strings.Contains(body, "▸ Needs you (4)") || strings.Contains(body, "Tasks (3)") {
		t.Fatalf("rail.groups: one heading over tasks and notes:\n%s", body)
	}
	f.set(t, RailCollapsedSetting, []string{"*", "!needs-you"})
	f.rail.Stack().seen = map[string]bool{} // a new start (the setting is read once per section)
	f.rail.Stack().collapse = map[string]bool{}
	f.rail.Stack().Refill()
	f.m.View()
	if body := f.body(); !strings.Contains(body, "▾ Needs you (4)") || !strings.Contains(body, "idea") {
		t.Fatalf("!needs-you keeps the group open:\n%s", body)
	}
	f.set(t, RailGroupsSetting, []string{})
	f.press("j") // select Beta: no notes, one task
	if body := f.body(); strings.Contains(body, "Notes") {
		t.Fatalf("rail.empty hide still draws an empty section:\n%s", body)
	}
	f.set(t, RailEmptySetting, RelatedEmptyHeader)
	if !strings.Contains(f.body(), "Notes (project) (0)") {
		t.Fatalf("rail.empty header leaves the empty section out:\n%s", f.body())
	}
	f.set(t, RailShowSetting, false)
	hooks := f.hooks
	f.press("k")
	if body := f.body(); strings.Contains(body, "rail") || f.hooks != hooks {
		t.Fatalf("rail.show off draws or reads (hooks %d -> %d):\n%s", hooks, f.hooks, body)
	}
	f.set(t, RailShowSetting, true)
	if f.hooks == hooks || !strings.Contains(f.body(), "rail") {
		t.Fatalf("rail.show on again neither reads nor draws")
	}
}

// GroupBy splits one section into one per value of a prop; a value
// section's new fills the value. A list section lists a saved filter
// whatever the thing, and refills on its noun's watch. rows answers the
// sections and things for a thing, headless.
func TestRailSplitListsAndRows(t *testing.T) {
	t.Parallel()
	f := newRailFixture(t, 120, func(s *RailSpec) {
		s.Stack.GroupBy = map[string]string{"task.project": "kind"}
		s.Stack.Lists = []RelatedList{{ID: "today", Noun: "task", Filter: "today", Label: "Today"}}
	}, func(r *Runtime) {
		_ = r.DeclareSetting(Setting{Name: RailCollapsedSetting, Type: SettingList, Default: []string{}})
	})
	body := f.body()
	for _, want := range []string{"Links (2)", "Files (1)", "Today (1)", "plan"} {
		if !strings.Contains(body, want) {
			t.Fatalf("lacks %q:\n%s", want, body)
		}
	}
	if _, err := f.r.Execute(Request{Noun: RelatedNoun, Verb: "new", Args: Arguments{"related": "page/rail/stack", "section": "task.kind=file"}}); err != nil {
		t.Fatal(err)
	}
	last := f.tasks.rows[len(f.tasks.rows)-1]
	if last["kind"] != "file" || last["project"] != "p1" {
		t.Fatalf("new in Files made %v", last)
	}
	f.m.View()
	if !strings.Contains(f.body(), "Files (2)") {
		t.Fatalf("the split did not refill:\n%s", f.body())
	}
	f.tasks.add("id", "t9", "title", "review", "kind", "link", "project", "p2", "today", true)
	f.r.Changed("task")
	f.m.View()
	if !strings.Contains(f.body(), "Today (2)") {
		t.Fatalf("the list section did not refill on its noun's change:\n%s", f.body())
	}
	out, err := f.r.Execute(Request{Noun: RailNoun, Verb: "inspect", Args: Arguments{"thing": "project:p2"}})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range out.([]map[string]any) {
		ids = append(ids, fmt.Sprint(row["id"], ":", row["count"]))
	}
	if got, want := strings.Join(ids, " "), "task.kind=link:1 task.kind=file:1 note.project:0 link.project:0 today:2"; got != want {
		t.Fatalf("rows for project:p2 %q, want %q", got, want)
	}
	if noun, key := f.rail.Stack().target(); noun != "project" || key != "p1" {
		t.Fatalf("rows left the rail on %s:%s", noun, key)
	}
}
