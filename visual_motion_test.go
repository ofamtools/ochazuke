package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// visualPage adds lists to one page in the given order, each with the given
// number of rows (row ids <list>1, <list>2, ...), and draws them top to
// bottom in drawOrder.
func visualPage(t *testing.T, added []string, rows map[string]int, drawOrder []string) (*Model, *Runtime) {
	t.Helper()
	r := NewRuntime()
	r.Instance("page")
	for _, id := range added {
		var items []ListItem
		for i := 1; i <= rows[id]; i++ {
			items = append(items, ListItem{ID: id + string(rune('0'+i)), Label: id + string(rune('0'+i)), Noun: "ochazuke.list"})
		}
		if _, err := r.AddList(id, items, Parent("page")); err != nil {
			t.Fatal(err)
		}
	}
	render := func(r *Runtime, width, height int) Block {
		var parts []string
		for _, id := range drawOrder {
			l := r.List(id)
			parts = append(parts, id, l.View(width, l.Len()))
		}
		return Text(strings.Join(parts, "\n"))
	}
	if _, err := r.AddPage("page", "Page", render, Focus(drawOrder[0])); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 60, 30
	m.View()
	return m, r
}

func focusedList(r *Runtime) string {
	for _, l := range []string{"a", "b", "c"} {
		if r.List(l) != nil && r.List(l).Focused() {
			return l
		}
	}
	return r.Focus
}

// Focus order is drawing order: lists added C, A, B (or B, A, C, which is
// no rotation of the drawing) but drawn A, B, C are walked A, B, C by } and
// tab, and back by { and shift+tab.
func TestFocusJumpsFollowDrawingOrder(t *testing.T) {
	for _, added := range [][]string{{"c", "a", "b"}, {"b", "a", "c"}} {
		m, r := visualPage(t, added, map[string]int{"a": 1, "b": 1, "c": 1}, []string{"a", "b", "c"})
		if got := focusedList(r); got != "a" {
			t.Fatalf("added %v: start %q", added, got)
		}
		for _, step := range []struct{ key, want string }{{"}", "b"}, {"}", "c"}, {"}", "a"}, {"{", "c"}, {"{", "b"}, {"tab", "c"}, {"tab", "a"}, {"shift+tab", "c"}} {
			keytest.Press(m, step.key)
			m.View()
			if got := focusedList(r); got != step.want {
				t.Fatalf("added %v, %s: focus %q (%s), want %s", added, step.key, got, r.Focus, step.want)
			}
		}
	}
}

// Before any frame (a CLI, a headless test) the walk is tree order; a frame
// that draws in another order takes over, and a stop the frame did not draw
// is skipped (it is not on screen) until a frame draws it again.
func TestFocusJumpsBeforeAFrameUseTreeOrder(t *testing.T) {
	r := NewRuntime()
	r.Instance("page")
	for _, id := range []string{"c", "a", "b"} {
		if _, err := r.AddList(id, []ListItem{{ID: id + "1", Label: id, Noun: "ochazuke.list"}}, Parent("page")); err != nil {
			t.Fatal(err)
		}
	}
	draw := []string{"a", "b"}
	render := func(r *Runtime, width, height int) Block {
		for _, id := range draw {
			r.List(id).View(width, 1)
		}
		return Text("")
	}
	if _, err := r.AddPage("page", "Page", render, Focus("c")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	if err := r.JumpFocus(1, "stop"); err != nil || focusedList(r) != "a" {
		t.Fatalf("tree order: %q %v", focusedList(r), err)
	}
	m := NewModel(r)
	m.Width, m.Height = 60, 30
	m.View()
	for _, want := range []string{"b", "a", "b"} {
		if err := r.JumpFocus(1, "stop"); err != nil || focusedList(r) != want {
			t.Fatalf("drawn a, b, c undrawn: got %q want %q (%v)", focusedList(r), want, err)
		}
	}
	draw = []string{"a", "c", "b"}
	m.View()
	for _, want := range []string{"a", "c", "b"} {
		if err := r.JumpFocus(1, "stop"); err != nil || focusedList(r) != want {
			t.Fatalf("c drawn again: got %q want %q (%v)", focusedList(r), want, err)
		}
	}
}

// Edge spill: j past the last row of a list on a page lands on the next
// list's first row, k before the first row on the previous list's last row,
// and the page's ends stay put. up and down are j and k.
func TestListMoveSpillsAcrossListsInDrawingOrder(t *testing.T) {
	m, r := visualPage(t, []string{"b", "a"}, map[string]int{"a": 2, "b": 3}, []string{"a", "b"})
	for _, step := range []struct{ key, want string }{
		{"k", "a1"}, {"j", "a2"}, {"j", "b1"}, {"k", "a2"}, {"down", "b1"}, {"j", "b2"}, {"j", "b3"}, {"j", "b3"},
		{"up", "b2"}, {"k", "b1"}, {"k", "a2"}, {"k", "a1"}, {"k", "a1"},
	} {
		keytest.Press(m, step.key)
		m.View()
		if r.Focus != step.want {
			t.Fatalf("%s: focus %q, want %q", step.key, r.Focus, step.want)
		}
	}
}

// NoSpill traps a list's motions; a list in a modal never spills; an empty
// list is passed through to the next stop.
func TestListSpillOptOutAndEmptyList(t *testing.T) {
	r := NewRuntime()
	r.Instance("page")
	if _, err := r.AddList("a", []ListItem{{ID: "a1", Label: "a1", Noun: "ochazuke.list"}}, Parent("page"), NoSpill()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddList("b", nil, Parent("page")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddList("c", []ListItem{{ID: "c1", Label: "c1", Noun: "ochazuke.list"}, {ID: "c2", Label: "c2", Noun: "ochazuke.list"}}, Parent("page")); err != nil {
		t.Fatal(err)
	}
	render := func(*Runtime, int, int) Block { return Text("") }
	if _, err := r.AddPage("page", "Page", render, Focus("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	for _, step := range []struct{ key, want string }{{"j", "a1"}, {"}", "b"}, {"j", "c1"}, {"k", "b"}, {"k", "a1"}, {"}", "b"}, {"k", "a1"}} {
		keytest.Press(m, step.key)
		if r.Focus != step.want {
			t.Fatalf("%s: focus %q, want %q", step.key, r.Focus, step.want)
		}
	}
	keytest.Press(m, "?")
	top := r.TopModal()
	if top == "" {
		t.Fatal("? opened no modal")
	}
	for range 200 {
		keytest.Press(m, "j")
	}
	if r.TopModal() != top || !slices.Contains(r.Ancestry(r.Focus), top) {
		t.Fatalf("j in a modal list left it: top %q focus %q", r.TopModal(), r.Focus)
	}
}
