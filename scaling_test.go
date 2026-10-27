package ochazuke

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The scaling benchmarks: replacing every row of a list, building and
// expanding a tree, drawing one frame of a large table, and a keypress and
// frame on a page of many rows. Each should
// grow about linearly with its size.

func benchRows(prefix string, n int) []ListItem {
	items := make([]ListItem, n)
	for i := range items {
		items[i] = ListItem{ID: fmt.Sprintf("%s%d", prefix, i), Label: fmt.Sprintf("row %d", i), Noun: "a"}
	}
	return items
}

func benchPage(b *testing.B) *Runtime {
	b.Helper()
	r := NewRuntime()
	r.Define("a")
	if err := r.RegisterPage(&Page{ID: "page", Title: "page", Render: func(*Runtime, int, int) Block { return Text("") }}); err != nil {
		b.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		b.Fatal(err)
	}
	return r
}

// BenchmarkSetItemsReplace replaces every row of a focused list with new
// ids, so each round removes n row instances and adds n.
func BenchmarkSetItemsReplace(b *testing.B) {
	for _, n := range []int{1000, 4000, 16000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			r := benchPage(b)
			sets := [2][]ListItem{benchRows("x", n), benchRows("y", n)}
			list, err := r.AddList("l", sets[0], Parent("page"))
			if err != nil {
				b.Fatal(err)
			}
			if err := list.Focus(); err != nil {
				b.Fatal(err)
			}
			list.Move(n / 2)
			i := 0
			for b.Loop() {
				i++
				if err := list.SetItems(sets[i%2]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// benchNodes is a tree of n nodes, ten children per node, parents first.
func benchNodes(n int) []TreeNode {
	nodes := make([]TreeNode, n)
	for i := range nodes {
		nodes[i] = TreeNode{ID: fmt.Sprintf("n%d", i), Label: fmt.Sprintf("node %d", i)}
		if i > 0 {
			nodes[i].Parent = fmt.Sprintf("n%d", (i-1)/10)
		}
	}
	return nodes
}

// BenchmarkTreeBuild adds a tree of n nodes, all expanded.
func BenchmarkTreeBuild(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			nodes := benchNodes(n)
			r := benchPage(b)
			for b.Loop() {
				if _, err := r.AddTree("t", nodes, Parent("page")); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				if err := r.RemoveInstance("t"); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}

// BenchmarkTreeExpand collapses and expands the root of a tree of n nodes.
func BenchmarkTreeExpand(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			r := benchPage(b)
			tree, err := r.AddTree("t", benchNodes(n), Parent("page"))
			if err != nil {
				b.Fatal(err)
			}
			if err := tree.Focus(); err != nil {
				b.Fatal(err)
			}
			for b.Loop() {
				if err := tree.SetCollapsed("n0", true); err != nil {
					b.Fatal(err)
				}
				if err := tree.SetCollapsed("n0", false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkTableView draws one 80x24 frame of a focused table of 10,000 rows.
func BenchmarkTableView(b *testing.B) {
	r := benchPage(b)
	items := benchRows("x", 10000)
	for i := range items {
		items[i].Cells = []string{fmt.Sprintf("name %d", i), fmt.Sprintf("%d", i*7), "a longer description column"}
	}
	list, err := r.AddList("l", items, Parent("page"), Columns(Column{Title: "Name"}, Column{Title: "Size", Align: AlignRight}, Column{Title: "Description", Priority: 1}))
	if err != nil {
		b.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		b.Fatal(err)
	}
	list.Move(5000)
	for b.Loop() {
		_ = list.View(80, 24)
	}
}

// closeLog is a component that records its Close calls.
type closeLog struct {
	id  string
	log *[]string
}

func (c closeLog) HandleMessage(tea.Msg) bool { return false }
func (c closeLog) PendingCommand() tea.Cmd    { return nil }
func (c closeLog) Close()                     { *c.log = append(*c.log, c.id) }

// TestSetItemsRemovesRowsAsRemoveInstanceDoes pins the batched removal in
// SetItems to the old one-RemoveInstance-per-row behaviour: the same Close
// calls in the same order, and the same focus, mode and modal return, for a
// focus inside a removed row's subtree with a FocusOnClose target.
func TestSetItemsRemovesRowsAsRemoveInstanceDoes(t *testing.T) {
	for _, modal := range []bool{false, true} {
		build := func() (*Runtime, *List, *[]string) {
			r := NewRuntime()
			r.Define("a")
			if err := r.RegisterPage(&Page{ID: "page", Title: "page", Render: func(*Runtime, int, int) Block { return Text("") }}); err != nil {
				t.Fatal(err)
			}
			if err := r.OpenPage("page"); err != nil {
				t.Fatal(err)
			}
			list, err := r.AddList("l", benchRows("r", 10), Parent("page"))
			if err != nil {
				t.Fatal(err)
			}
			log := &[]string{}
			for _, row := range []string{"r2", "r3", "r5"} {
				child := r.Instance(row+"/c", Parent(row), FocusOnClose("r7"))
				child.Component = closeLog{id: child.ID, log: log}
				r.Instances[row].Component = closeLog{id: row, log: log}
			}
			if err := list.Select("r3"); err != nil {
				t.Fatal(err)
			}
			if err := r.SetFocus("r3/c"); err != nil {
				t.Fatal(err)
			}
			if modal {
				if err := r.Push("m", ""); err != nil {
					t.Fatal(err)
				}
			}
			return r, list, log
		}
		keep := []ListItem{}
		for _, item := range benchRows("r", 10) {
			if item.ID != "r2" && item.ID != "r3" && item.ID != "r5" {
				keep = append(keep, item)
			}
		}
		state := func(r *Runtime, l *List, log *[]string) []any {
			ret := ""
			if m := r.Instances["m"]; m != nil {
				ret = m.Payload.Return
			}
			return []any{*log, r.Focus, r.Mode.Name, ret, l.Snapshot(), len(r.Instances)}
		}
		r1, l1, log1 := build()
		if err := l1.SetItems(keep); err != nil {
			t.Fatal(err)
		}
		r2, l2, log2 := build()
		for _, id := range []string{"r2", "r3", "r5"} {
			if err := r2.RemoveInstance(id); err != nil {
				t.Fatal(err)
			}
		}
		if err := l2.SetItems(keep); err != nil {
			t.Fatal(err)
		}
		got, want := state(r1, l1, log1), state(r2, l2, log2)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("modal %v: batched removal\n got %v\nwant %v", modal, got, want)
		}
	}
}

// TestTreeIndexMatchesScans checks the tree's index against the scans it
// replaced: position, depth (a missing parent counts once) and children.
func TestTreeIndexMatchesScans(t *testing.T) {
	tree := &Tree{nodes: append(benchNodes(40), TreeNode{ID: "orphan", Parent: "gone"}, TreeNode{ID: "under", Parent: "orphan"})}
	scanParent := func(id string) string {
		for _, n := range tree.nodes {
			if n.ID == id {
				return n.Parent
			}
		}
		return ""
	}
	for _, n := range append(tree.nodes, TreeNode{ID: "missing"}, TreeNode{ID: ""}) {
		want := slices.IndexFunc(tree.nodes, func(m TreeNode) bool { return m.ID == n.ID })
		if got := tree.index(n.ID); got != want {
			t.Errorf("index(%q) = %d, want %d", n.ID, got, want)
		}
		d := 0
		for p := scanParent(n.ID); p != ""; p = scanParent(p) {
			d++
		}
		if got := tree.depth(n.ID); got != d {
			t.Errorf("depth(%q) = %d, want %d", n.ID, got, d)
		}
		kids := slices.ContainsFunc(tree.nodes, func(m TreeNode) bool { return m.Parent == n.ID })
		if got := tree.hasChildren(n.ID); got != kids {
			t.Errorf("hasChildren(%q) = %v, want %v", n.ID, got, kids)
		}
	}
}

// TestTableWidthsFollowTheItems checks the cached column widths: SetItems
// remeasures them, and so does a change in the number of columns.
func TestTableWidthsFollowTheItems(t *testing.T) {
	r := NewRuntime()
	r.Define("a")
	rows := func(cells ...[]string) []ListItem {
		out := make([]ListItem, len(cells))
		for i, c := range cells {
			out[i] = ListItem{ID: fmt.Sprint(i), Label: c[0], Noun: "a", Cells: c}
		}
		return out
	}
	list, err := r.AddList("l", rows([]string{"ab", "x"}), Columns(Column{Title: "A", Min: 1}, Column{Title: "B"}))
	if err != nil {
		t.Fatal(err)
	}
	fresh := func() string {
		cp := &List{r: r, id: "l", all: list.all, items: list.items, selected: list.selected, offset: list.offset, marks: list.marks, columns: list.columns}
		return cp.View(40, 5)
	}
	check := func(step string) {
		t.Helper()
		if got, want := list.View(40, 5), fresh(); got != want {
			t.Fatalf("%s: cached view\n%s\nwant\n%s", step, got, want)
		}
	}
	check("first frame")
	if err := list.SetItems(rows([]string{"a much wider cell", "x"}, []string{"c", "y"})); err != nil {
		t.Fatal(err)
	}
	check("after SetItems")
	list.SetColumns(Column{Title: "A", Min: 1}, Column{Title: "B"}, Column{Title: "C"})
	check("after SetColumns")
}

// framePage is a page shaped like a catalog page (cli.MountPage): a rail
// list of groups, a list of n rows, and, with detail, a detail added after
// the lists. It returns the model with the focus on the rows.
func framePage(tb testing.TB, n int, detail bool) *Model {
	tb.Helper()
	r := NewRuntime()
	r.Define("a")
	r.Instance("p")
	rail, err := r.AddList("p/groups", benchRows("g", 8), Parent("p"))
	if err != nil {
		tb.Fatal(err)
	}
	rows, err := r.AddList("p/rows", benchRows("x", n), Parent("p"))
	if err != nil {
		tb.Fatal(err)
	}
	var d *Detail
	if detail {
		d, err = r.AddDetail("p/detail", func() string { sel, _ := rows.Selected(); return sel.ID }, Parent("p"))
		if err != nil {
			tb.Fatal(err)
		}
	}
	text := strings.Repeat("detail line\n", 200)
	render := func(r *Runtime, w, h int) Block {
		r.Placed("p/groups")
		r.Placed("p/rows")
		out := rail.View(w/3, h) + "\n" + rows.View(w/3, h)
		if d != nil {
			body, marker := d.Window(text, w/3, h)
			out += "\n" + marker + "\n" + body
		}
		return Text(out)
	}
	if _, err := r.AddPage("p", "P", render, Focus("p/rows")); err != nil {
		tb.Fatal(err)
	}
	if err := r.OpenPage("p"); err != nil {
		tb.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 120, 40
	m.View()
	return m
}

// BenchmarkPageFrame is one keypress (j) and one frame on a page of n rows.
func BenchmarkPageFrame(b *testing.B) {
	for _, detail := range []bool{false, true} {
		for _, n := range []int{2000, 8000} {
			b.Run(fmt.Sprintf("detail=%v/%d", detail, n), func(b *testing.B) {
				m := framePage(b, n, detail)
				for b.Loop() {
					m.handleKey(MacroKey{Key: "j"})
					m.View()
				}
			})
		}
	}
}

// TestPageFrameScalesLinearly guards the per-key cost of a large page: a
// keypress, the footer hints and a frame on four times the rows must cost
// about four times as much, not sixteen (finding the page's detail once
// walked the page asking Children per node, quadratic in the rows). The
// bound is loose so only a quadratic path trips it; each size takes the
// fastest of several rounds, so a slow round does not.
func TestPageFrameScalesLinearly(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	const small, large, rounds = 1000, 4000, 7
	for _, detail := range []bool{false, true} {
		models := [2]*Model{framePage(t, small, detail), framePage(t, large, detail)}
		best := [2]time.Duration{}
		for round := 0; round < rounds; round++ {
			for i, m := range models {
				start := time.Now()
				for _, key := range []string{"j", "J", "k"} {
					m.handleKey(MacroKey{Key: key})
					_ = BindingHints(m.Runtime, m.Runtime.Focus)
					m.View()
				}
				if took := time.Since(start); round == 0 || took < best[i] {
					best[i] = took
				}
			}
		}
		if ratio := float64(best[1]) / float64(best[0]); ratio > 10 {
			t.Errorf("detail=%v: %d rows took %v, %d rows %v (x%.1f); want about x%d", detail, small, best[0], large, best[1], ratio, large/small)
		}
	}
}

// BenchmarkPageList asks for the pages on a page of n rows: it reads app's
// page order, so it should not grow with n.
func BenchmarkPageList(b *testing.B) {
	for _, n := range []int{2000, 8000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			r := benchPage(b)
			if _, err := r.AddList("l", benchRows("r", n), Parent("page")); err != nil {
				b.Fatal(err)
			}
			for b.Loop() {
				if len(r.PageList()) != 1 {
					b.Fatal("one page")
				}
			}
		})
	}
}

// updateKey is one keypress through the Bubble Tea adapter: Update runs the
// key, then delivers and drains through the runtime (Deliver,
// PendingCommands), as a real program does.
func updateKey(m *Model, key string) {
	_, _ = m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
}

// BenchmarkUpdateKey is one Update (a j or k) on a page of n rows, without
// a frame: the delivery path walks the component holders, not the rows.
func BenchmarkUpdateKey(b *testing.B) {
	for _, n := range []int{2000, 16000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			m := framePage(b, n, false)
			keys := []string{"j", "k"}
			i := 0
			for b.Loop() {
				updateKey(m, keys[i%2])
				i++
			}
		})
	}
}

// TestUpdateCostIsFlatInRows guards the per-Update cost of a large page:
// list rows are instances without components, so delivering a message and
// draining the components' commands must not visit them (components()
// once scanned and sorted every instance on every Update). Eight times the
// rows must cost about the same; the bound is loose (x3) and each size
// takes the fastest of several rounds.
func TestUpdateCostIsFlatInRows(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	const small, large, rounds, keys = 2000, 16000, 9, 40
	models := [2]*Model{framePage(t, small, false), framePage(t, large, false)}
	best := [2]time.Duration{}
	for round := 0; round < rounds; round++ {
		for i, m := range models {
			start := time.Now()
			for k := 0; k < keys; k++ {
				updateKey(m, []string{"j", "k"}[k%2])
			}
			if took := time.Since(start); round == 0 || took < best[i] {
				best[i] = took
			}
		}
	}
	if ratio := float64(best[1]) / float64(best[0]); ratio > 3 {
		t.Errorf("%d rows took %v, %d rows %v (x%.1f) for %d keys; want about x1", small, best[0], large, best[1], ratio, keys)
	}
}

// TestHolderIndexMatchesScan pins components() (the holder index) to a
// scan of every instance in registration order, through the ways
// instances come and go: constructors, rows, a component set after the
// instance, modals, removal, SetItems, and a direct write to r.Instances.
func TestHolderIndexMatchesScan(t *testing.T) {
	r := NewRuntime()
	r.Define("a")
	var log []string
	scan := func() []string {
		var all []*Instance
		for _, inst := range r.Instances {
			if _, ok := inst.Component.(Component); ok {
				all = append(all, inst)
			}
		}
		slices.SortFunc(all, func(a, b *Instance) int { return int(a.seq) - int(b.seq) })
		var out []string
		for _, inst := range all {
			out = append(out, inst.ID)
		}
		return out
	}
	check := func(when string) {
		t.Helper()
		var got []string
		for _, inst := range r.components() {
			got = append(got, inst.ID)
		}
		if want := scan(); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: components() = %v, want %v", when, got, want)
		}
	}
	page, err := r.AddPage("p", "P", func(*Runtime, int, int) Block { return Text("") })
	if err != nil || page == nil {
		t.Fatal(err)
	}
	r.Instance("c1", Parent("p")).Component = closeLog{id: "c1", log: &log}
	list, err := r.AddList("p/l", benchRows("x", 50), Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	r.Instance("x3/child", Parent("x3")).Component = closeLog{id: "x3/child", log: &log}
	check("built")
	if err := r.AddInput(NewInput("in", "", false), Parent("p")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenModal("dialog", Payload{}, WithInput(NewInput("dialog/in", "", false))); err != nil {
		t.Fatal(err)
	}
	check("modal open")
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	check("modal closed")
	if err := list.SetItems(benchRows("y", 20)); err != nil {
		t.Fatal(err)
	}
	check("rows replaced (x3/child gone with its row)")
	r.Instances["direct"] = &Instance{ID: "direct", Noun: "a", Parent: "app", Component: closeLog{id: "direct", log: &log}, seq: 1 << 40}
	check("direct write")
	if err := r.RemoveInstance("c1"); err != nil {
		t.Fatal(err)
	}
	delete(r.Instances, "direct")
	check("removed")
	// List.Patch removing and adding rows, and a row's own child.
	r.Instance("y2/child", Parent("y2")).Component = closeLog{id: "y2/child", log: &log}
	if err := list.Patch(ListPatch{Upsert: []ListItem{{ID: "z1", Label: "z1", Noun: "a"}}, Remove: []string{"y2", "y5"}}); err != nil {
		t.Fatal(err)
	}
	check("patched")
	// The Settings page registers its lists and detail when it opens and
	// removes them when it closes.
	r.Config().Dir = t.TempDir()
	if err := r.DeclareSetting(Setting{Name: "wrap", Type: SettingBoolean, Default: false, Description: "Wrap"}, Setting{Name: "ext.x.n", Type: SettingInteger, Default: 1, Description: "N"}); err != nil {
		t.Fatal(err)
	}
	NewModel(r)
	if err := r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "edit"}); err != nil {
		t.Fatal(err)
	}
	if r.Page != SettingsPageID {
		t.Fatalf("settings.edit: page %s", r.Page)
	}
	check("settings open")
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "close"}); err != nil {
		t.Fatal(err)
	}
	if r.Instances[SettingsPageID] != nil {
		t.Fatal("settings.close keeps the page")
	}
	check("settings closed")
}

// BenchmarkListFilter is one keystroke of the filter line on a list of n
// rows: the query grows by a character and every row is matched again.
func BenchmarkListFilter(b *testing.B) {
	for _, n := range []int{2000, 16000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			m := framePage(b, n, false)
			list := m.Runtime.List("p/rows")
			queries := []string{"row 1", "row 12", "row 1"}
			i := 0
			for b.Loop() {
				list.SetFilter(queries[i%len(queries)])
				i++
			}
		})
	}
}
