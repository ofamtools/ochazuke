package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"ofam.dev/ochazuke/keytest"
)

func TestParseNoSpillEntry(t *testing.T) {
	for _, tc := range []struct {
		entry, name string
		edge        SpillEdge
	}{
		{"panes/working", "panes/working", ""},
		{"panes/working:last", "panes/working", EdgeLast},
		{"panes/working:first", "panes/working", EdgeFirst},
		{"t:first-col", "t", EdgeFirstColumn},
		{"t:last-col", "t", EdgeLastColumn},
		{"panes/*:last", "panes/*", EdgeLast},
		{" a : last ", "a", EdgeLast},
		{"a:down", "a:down", ""}, // not an edge: part of the id, names nothing
		{"host:8080", "host:8080", ""},
	} {
		name, edge := ParseNoSpillEntry(tc.entry)
		if name != tc.name || edge != tc.edge {
			t.Errorf("ParseNoSpillEntry(%q) = %q, %q; want %q, %q", tc.entry, name, edge, tc.name, tc.edge)
		}
	}
}

// spillKind builds one list-like component "s" between two plain lists
// ("before" drawn above it, "after" below) and says whether it moves
// sideways (a table's h/l spill at its column edges).
type spillKind struct {
	name    string
	columns bool
	add     func(t *testing.T, r *Runtime) (view func(width int) string, rows []string)
}

func spillKinds() []spillKind {
	cols := Columns(Column{Title: "name"}, Column{Title: "n"})
	return []spillKind{
		{"list", false, func(t *testing.T, r *Runtime) (func(int) string, []string) {
			l, err := r.AddList("s", []ListItem{{ID: "s1", Label: "s1", Noun: "ochazuke.list"}, {ID: "s2", Label: "s2", Noun: "ochazuke.list"}}, Parent("page"))
			if err != nil {
				t.Fatal(err)
			}
			return func(w int) string { return l.View(w, 2) }, []string{"s1", "s2"}
		}},
		{"table", true, func(t *testing.T, r *Runtime) (func(int) string, []string) {
			l, err := r.AddList("s", []ListItem{
				{ID: "s1", Label: "s1", Noun: "ochazuke.list", Cells: []string{"s1", "1"}},
				{ID: "s2", Label: "s2", Noun: "ochazuke.list", Cells: []string{"s2", "2"}},
			}, Parent("page"), cols)
			if err != nil {
				t.Fatal(err)
			}
			return func(w int) string { return l.View(w, 3) }, []string{"s1", "s2"}
		}},
		{"tree", true, func(t *testing.T, r *Runtime) (func(int) string, []string) {
			if err := r.Settings().Set("tree.spill", "both"); err != nil {
				t.Fatal(err)
			}
			tr, err := r.AddTree("s", []TreeNode{{ID: "s1", Label: "s1", Cells: []string{"s1", "1"}}, {ID: "s2", Label: "s2", Cells: []string{"s2", "2"}}}, Parent("page"), cols)
			if err != nil {
				t.Fatal(err)
			}
			return func(w int) string { return tr.View(w, 3) }, []string{"s1", "s2"}
		}},
		{"timeline", false, func(t *testing.T, r *Runtime) (func(int) string, []string) {
			tl, err := r.AddTimeline("s", []Card{{ID: "s1", Header: "s1"}, {ID: "s2", Header: "s2"}}, Parent("page"))
			if err != nil {
				t.Fatal(err)
			}
			return func(w int) string { return tl.View(w, 12) }, []string{"s1", "s2"}
		}},
	}
}

func spillEdgePage(t *testing.T, kind spillKind) (*Model, *Runtime, []string) {
	t.Helper()
	r := NewRuntime()
	r.Instance("page")
	if _, err := r.AddList("before", []ListItem{{ID: "b1", Label: "b1", Noun: "ochazuke.list"}}, Parent("page")); err != nil {
		t.Fatal(err)
	}
	view, rows := kind.add(t, r)
	if _, err := r.AddList("after", []ListItem{{ID: "a1", Label: "a1", Noun: "ochazuke.list"}}, Parent("page")); err != nil {
		t.Fatal(err)
	}
	render := func(r *Runtime, width, height int) Block {
		return Text(strings.Join([]string{r.List("before").View(width, 1), view(width), r.List("after").View(width, 1)}, "\n"))
	}
	if _, err := r.AddPage("page", "Page", render, Focus("before")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 60, 30
	m.View()
	return m, r, rows
}

// Every edge (SpillEdges) × every list-like component × every
// list.no-spill form (none, all edges, each single edge): the motion
// leaving by that edge spills unless the setting traps that edge, and a
// component with no horizontal motion (a plain list, a timeline) never
// spills sideways (the column edges are a no-op there).
func TestSpillEdgesMatrix(t *testing.T) {
	keys := map[SpillEdge]string{EdgeFirst: "k", EdgeLast: "j", EdgeFirstColumn: "h", EdgeLastColumn: "l"}
	configs := []string{"", "s", "s:first", "s:last", "s:first-col", "s:last-col", "s*:last"}
	for _, kind := range spillKinds() {
		for _, config := range configs {
			for _, edge := range SpillEdges {
				name := fmt.Sprintf("%s/%q/%s", kind.name, config, edge)
				t.Run(name, func(t *testing.T) {
					m, r, rows := spillEdgePage(t, kind)
					start := rows[len(rows)-1]
					if edge == EdgeFirst {
						start = rows[0]
					}
					if err := r.SetFocus(start); err != nil {
						t.Fatal(err)
					}
					m.View()
					var entries []string
					if config != "" {
						entries = []string{config}
					}
					if err := r.Settings().Set(ListNoSpillSetting, entries); err != nil {
						t.Fatal(err)
					}
					_, at := ParseNoSpillEntry(config)
					trapped := config != "" && (at == "" || at == edge)
					sideways := edge == EdgeFirstColumn || edge == EdgeLastColumn
					keytest.Press(m, keys[edge])
					m.View()
					want := start
					switch {
					case trapped, sideways && !kind.columns:
					case edge == EdgeFirst || edge == EdgeFirstColumn:
						want = "b1"
					default:
						want = "a1"
					}
					if r.Focus != want {
						t.Fatalf("%s from %s: focus %q, want %q", keys[edge], start, r.Focus, want)
					}
				})
			}
		}
	}
}

// At a trapped edge the motion is unavailable, not a silent no-op: the
// help leaves j out on the last row (k still shown), and on the first row
// k stays (it spills up).
func TestSpillEdgeMotionUnavailableInHelp(t *testing.T) {
	m, r := visualPage(t, []string{"a", "b"}, map[string]int{"a": 2, "b": 2}, []string{"a", "b"})
	if err := r.Settings().Set(ListNoSpillSetting, []string{"a:last"}); err != nil {
		t.Fatal(err)
	}
	moves := func(focus string) map[string]bool {
		out := map[string]bool{}
		for _, row := range BindingHelp(r, focus) {
			if strings.HasSuffix(row.Request, "list.move") {
				out[row.Key] = true
			}
		}
		return out
	}
	move := func(delta int) bool {
		return r.Available(Request{Noun: "ochazuke.list", Verb: "move", Args: Arguments{"delta": delta}}, r.Focus, r.Mode)
	}
	if err := r.SetFocus("a2"); err != nil {
		t.Fatal(err)
	}
	m.View()
	if got := moves("a2"); got["j"] || got["down"] || !got["k"] {
		t.Fatalf("a:last on the last row: help list.move keys %v, want k without j/down", got)
	}
	if move(1) || !move(-1) {
		t.Fatalf("a:last on the last row: move down %v, up %v", move(1), move(-1))
	}
	if r.Available(Request{Noun: "ochazuke.list", Verb: "move", Args: Arguments{"delta": 1, "clamp": true}}, r.Focus, r.Mode) != true {
		t.Fatal("a clamped move stays available at a trapped edge")
	}
	keytest.Press(m, "j")
	if r.Focus != "a2" {
		t.Fatalf("j at the trapped last row: focus %q", r.Focus)
	}
	if err := r.SetFocus("a1"); err != nil {
		t.Fatal(err)
	}
	m.View()
	if got := moves("a1"); !got["j"] || !got["k"] {
		t.Fatalf("a:last on the first row: help list.move keys %v, want j and k", got)
	}
	if err := r.SetFocus("b1"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "k")
	if r.Focus != "a2" {
		t.Fatalf("k from b1 into a: focus %q, want a2", r.Focus)
	}
	keytest.Press(m, "k")
	keytest.Press(m, "k")
	if r.Focus != "a1" {
		t.Fatalf("k at a's first row (first not trapped, page's first stop): focus %q", r.Focus)
	}
}

// A trapped key falls through: with list.move unavailable at the trapped
// edge, the next row bound to j (here an app row on the page) answers.
func TestSpillEdgeKeyFallsThrough(t *testing.T) {
	m, r := visualPage(t, []string{"a", "b"}, map[string]int{"a": 2, "b": 2}, []string{"a", "b"})
	if err := r.Settings().Set(ListNoSpillSetting, []string{"a:last"}); err != nil {
		t.Fatal(err)
	}
	ran := 0
	r.Register(Operation{Verb: "probe", Noun: "ochazuke.page", Run: func(*Runtime, Context, Arguments) (any, error) { ran++; return nil, nil }})
	row := Do("ochazuke.page.probe", nil)
	row.Instance, row.Mode, row.Keys = "page", "normal", "j"
	if err := r.Bind(row); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("a2"); err != nil {
		t.Fatal(err)
	}
	m.View()
	keytest.Press(m, "j")
	if ran != 1 || r.Focus != "a2" {
		t.Fatalf("j at a trapped last row: probe ran %d, focus %q", ran, r.Focus)
	}
	if err := r.SetFocus("a1"); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "j")
	if ran != 1 || r.Focus != "a2" {
		t.Fatalf("j inside the list: probe ran %d, focus %q", ran, r.Focus)
	}
}

// A table's row motions honour :last as a plain list's do: a count stops
// at the edge and the next j stays; paging (G, ctrl+d) never spills.
func TestSpillEdgeTableRowsCountsAndPaging(t *testing.T) {
	kind := spillKinds()[1]
	m, r, _ := spillEdgePage(t, kind)
	if err := r.Settings().Set(ListNoSpillSetting, []string{"s:last"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("s1"); err != nil {
		t.Fatal(err)
	}
	m.View()
	for _, keys := range [][]string{{"5", "j"}, {"j"}, {"G"}, {"j"}, {"ctrl+d"}, {"j"}} {
		for _, k := range keys {
			keytest.Press(m, k)
		}
		m.View()
		if r.Focus != "s2" {
			t.Fatalf("after %v: focus %q, want s2", keys, r.Focus)
		}
	}
	keytest.Press(m, "k")
	keytest.Press(m, "k")
	if r.Focus != "b1" {
		t.Fatalf("s:last leaves the first edge: k k focus %q, want b1", r.Focus)
	}
}

// A NoSpill() list traps every edge whatever the setting says.
func TestSpillEdgeNoSpillOptionTrapsAll(t *testing.T) {
	r := NewRuntime()
	r.Instance("page")
	l, err := r.AddList("x", []ListItem{{ID: "x1", Label: "x1", Noun: "ochazuke.list"}}, Parent("page"), NoSpill())
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range SpillEdges {
		if !l.trappedAt(edge) {
			t.Errorf("NoSpill list not trapped at %s", edge)
		}
	}
}

// Inside a timeline's last card, j on the last step at a trapped :last
// edge is unavailable (timeline.step) and the focus stays in the body;
// untrapped it spills to the next stop.
func TestSpillEdgeTimelineStep(t *testing.T) {
	for _, trapped := range []bool{true, false} {
		r := NewRuntime()
		r.Instance("page")
		tl, err := r.AddTimeline("s", []Card{{ID: "s1", Header: "s1"}, {ID: "s2", Header: "s2", Body: TextBody("only paragraph")}}, Parent("page"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.AddList("after", []ListItem{{ID: "a1", Label: "a1", Noun: "ochazuke.list"}}, Parent("page")); err != nil {
			t.Fatal(err)
		}
		render := func(r *Runtime, width, height int) Block {
			return Text(tl.View(width, 12) + "\n" + r.List("after").View(width, 1))
		}
		if _, err := r.AddPage("page", "Page", render, Focus("s")); err != nil {
			t.Fatal(err)
		}
		if err := r.OpenPage("page"); err != nil {
			t.Fatal(err)
		}
		m := NewModel(r)
		m.Width, m.Height = 60, 30
		m.View()
		if err := r.SetFocus("s2"); err != nil {
			t.Fatal(err)
		}
		m.View()
		keytest.Press(m, "l")
		m.View()
		keytest.Press(m, "l")
		m.View()
		if !tl.InBody() {
			t.Fatalf("not in the body: focus %q", r.Focus)
		}
		entries := []string{}
		if trapped {
			entries = []string{"s:last"}
		}
		if err := r.Settings().Set(ListNoSpillSetting, entries); err != nil {
			t.Fatal(err)
		}
		avail := r.Available(Request{Noun: timelineNoun, Verb: "step", Args: Arguments{"delta": 1}}, r.Focus, r.Mode)
		if avail == trapped {
			t.Fatalf("trapped %v: timeline.step down available %v", trapped, avail)
		}
		keytest.Press(m, "j")
		m.View()
		if trapped && !tl.InBody() || !trapped && r.Focus != "a1" {
			t.Fatalf("trapped %v: j on the last step: focus %q, in body %v", trapped, r.Focus, tl.InBody())
		}
	}
}
