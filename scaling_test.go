package ochazuke

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The scaling benchmarks: replacing every row of a list, building and
// expanding a tree, and drawing one frame of a large table. Each should
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
		cp := &List{r: r, id: "l", items: list.items, selected: list.selected, offset: list.offset, marks: list.marks, columns: list.columns}
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
