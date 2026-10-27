package ochazuke

import (
	"fmt"
	"strings"
	"testing"
)

// motionPage builds a page of two stacked lists, a (40 rows, drawn 10
// lines high so it windows itself) above b (3 rows), with a NewModel so
// the app defaults and the count rows are in place, focused on a's first
// row and drawn once.
func motionPage(t *testing.T) (*Runtime, *Model, *List, *List) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	r.Define("row")
	rows := func(prefix string, n int) []ListItem {
		var out []ListItem
		for i := 1; i <= n; i++ {
			out = append(out, ListItem{ID: fmt.Sprintf("%s%d", prefix, i), Label: fmt.Sprintf("%s%d", prefix, i), Noun: "row"})
		}
		return out
	}
	var a, b *List
	if _, err := r.AddPage("p", "P", func(_ *Runtime, w, h int) Block {
		return Text(a.View(w, 10) + "\n" + b.View(w, 3))
	}); err != nil {
		t.Fatal(err)
	}
	var err error
	if a, err = r.AddList("a", rows("a", 40), Parent("p")); err != nil {
		t.Fatal(err)
	}
	if b, err = r.AddList("b", rows("b", 3), Parent("p")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	if err := a.Focus(); err != nil {
		t.Fatal(err)
	}
	m.Width, m.Height = 40, 24
	_ = m.View()
	return r, m, a, b
}

func motionKeys(t *testing.T, r *Runtime, m *Model, keys string) {
	t.Helper()
	for _, key := range strings.Fields(keys) {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatalf("key %q: %v", key, err)
		}
		_ = m.View()
	}
}

func TestListGotoEndsAndCounts(t *testing.T) {
	r, m, a, _ := motionPage(t)
	for _, step := range []struct{ keys, want string }{
		{"G", "a40"},
		{"g g", "a1"},
		{"5 G", "a5"},
		{"1 2 g g", "a12"},
		{"9 9 G", "a40"}, // a count past the end clamps (R9.4)
		{"g g", "a1"},
	} {
		motionKeys(t, r, m, step.keys)
		if r.Focus != step.want || a.selectedID() != step.want {
			t.Fatalf("%s: focus %s, selected %s, want %s", step.keys, r.Focus, a.selectedID(), step.want)
		}
		if r.Mode.Name != "normal" {
			t.Fatalf("%s left mode %q", step.keys, r.Mode.Name)
		}
	}
}

func TestListPageMotionsMoveTheWindowWithTheSelection(t *testing.T) {
	r, m, a, _ := motionPage(t)
	// a draws 10 lines: 9 rows and the range line, so a page is 9 rows.
	if got := a.pageRows(); got != 9 {
		t.Fatalf("page rows %d, want 9", got)
	}
	motionKeys(t, r, m, "j j j j j j") // a7, the window scrolled to keep the margin
	line := a.selected - a.offset
	motionKeys(t, r, m, "ctrl+d")
	if r.Focus != "a11" || a.selected-a.offset != line {
		t.Fatalf("ctrl+d: focus %s at line %d, want a11 at line %d", r.Focus, a.selected-a.offset, line)
	}
	motionKeys(t, r, m, "ctrl+f")
	if r.Focus != "a20" || a.selected-a.offset != line {
		t.Fatalf("ctrl+f: focus %s at line %d, want a20 at line %d", r.Focus, a.selected-a.offset, line)
	}
	motionKeys(t, r, m, "ctrl+u ctrl+b")
	if r.Focus != "a7" {
		t.Fatalf("ctrl+u ctrl+b: focus %s, want a7", r.Focus)
	}
	motionKeys(t, r, m, "3 ctrl+d") // a count is rows for the half-page motions
	if r.Focus != "a10" {
		t.Fatalf("3 ctrl+d: focus %s, want a10", r.Focus)
	}
	motionKeys(t, r, m, "2 ctrl+f") // and pages for the whole-page ones
	if r.Focus != "a28" {
		t.Fatalf("2 ctrl+f: focus %s, want a28", r.Focus)
	}
}

func TestListPageMotionsClampWithoutSpilling(t *testing.T) {
	r, m, a, _ := motionPage(t)
	for _, keys := range []string{"G", "ctrl+d", "ctrl+f", "G"} {
		motionKeys(t, r, m, keys)
		if r.Focus != "a40" {
			t.Fatalf("%s at the last row: focus %s, want a40 (no spill into b)", keys, r.Focus)
		}
	}
	motionKeys(t, r, m, "j") // j still spills
	if r.Focus != "b1" {
		t.Fatalf("j: focus %s, want b1", r.Focus)
	}
	for _, keys := range []string{"ctrl+u", "ctrl+b", "g g"} {
		motionKeys(t, r, m, keys)
		if r.Focus != "b1" {
			t.Fatalf("%s at the first row of b: focus %s, want b1", keys, r.Focus)
		}
	}
	if a.selectedID() != "a40" {
		t.Fatalf("a's selection moved: %s", a.selectedID())
	}
}

// A list that does not window itself pages by the page body's height.
func TestListPageRowsOfAnUnwindowedListIsThePageHeight(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	r.Define("row")
	var items []ListItem
	for i := 1; i <= 60; i++ {
		items = append(items, ListItem{ID: fmt.Sprintf("r%d", i), Label: fmt.Sprintf("r%d", i), Noun: "row"})
	}
	var l *List
	if _, err := r.AddPage("p", "P", func(_ *Runtime, w, _ int) Block { return Text(l.View(w, len(items))) }); err != nil {
		t.Fatal(err)
	}
	l, _ = r.AddList("l", items, Parent("p"))
	_ = r.OpenPage("p")
	_ = l.Focus()
	m.Width, m.Height = 40, 20
	_ = m.View()
	_, height := m.pageSize()
	if l.windowed || l.pageRows() != height {
		t.Fatalf("windowed %v, page rows %d, want the page height %d", l.windowed, l.pageRows(), height)
	}
	motionKeys(t, r, m, "ctrl+f")
	if want := fmt.Sprintf("r%d", 1+height); r.Focus != want {
		t.Fatalf("ctrl+f: focus %s, want %s", r.Focus, want)
	}
	if !strings.Contains(m.View().Content, r.Focus) {
		t.Fatalf("the page did not follow %s", r.Focus)
	}
}

func TestListMotionsAreMotionsOffTheFooter(t *testing.T) {
	r, _, _, _ := motionPage(t)
	for _, key := range []string{"ochazuke.list.goto", "ochazuke.list.scroll"} {
		if op := r.Operations[key]; !op.Motion {
			t.Fatalf("%s is not a motion", key)
		}
	}
	hints := BindingHints(r, r.Focus)
	for _, gone := range []string{"ochazuke.list.goto", "ochazuke.list.scroll", "ctrl+d", "G "} {
		if strings.Contains(hints, gone) {
			t.Fatalf("footer shows %q: %s", gone, hints)
		}
	}
	keys := map[string]bool{}
	for _, row := range BindingHelp(r, r.Focus) {
		if row.Section == MovementSection {
			keys[row.Key] = true
		}
	}
	for _, key := range []string{"g g", "G", "ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b"} {
		if !keys[key] {
			t.Fatalf("? help lists no %q under movement: %v", key, keys)
		}
	}
}

// An application's row on the row noun answers before the list's default.
func TestListMotionsYieldToTheRowNoun(t *testing.T) {
	r, m, _, _ := motionPage(t)
	ran := false
	r.Register(Operation{Verb: "bottom", Noun: "row", Run: func(*Runtime, Context, Arguments) (any, error) { ran = true; return nil, nil }})
	r.Define("row").Mode("normal").Bind("G", Do("row.bottom"))
	motionKeys(t, r, m, "G")
	if !ran || r.Focus != "a1" {
		t.Fatalf("row G ran %v, focus %s", ran, r.Focus)
	}
}

func (l *List) selectedID() string {
	item, _ := l.Selected()
	return item.ID
}
