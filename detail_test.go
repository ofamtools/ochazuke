package ochazuke

import (
	"fmt"
	"strings"
	"testing"
)

func detailLines(item string, n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s line %d", item, i+1)
	}
	return strings.Join(lines, "\n")
}

func TestDetailWindowResetsOnNewItemAndKeepsOnRefresh(t *testing.T) {
	d := &Detail{}
	body, marker := d.Window("a", detailLines("a", 30), 0, 5)
	if body != detailLines("a", 5) || marker != "1–5 / 30" {
		t.Fatalf("first window = %q, %q", body, marker)
	}
	d.Scroll(10)
	if _, marker = d.Window("a", detailLines("a", 30), 0, 5); marker != "11–15 / 30" {
		t.Fatalf("after scroll marker = %q", marker)
	}
	// The same item refreshing keeps the offset, clamped to the new text.
	if _, marker = d.Window("a", detailLines("a", 12), 0, 5); marker != "8–12 / 12" {
		t.Fatalf("refresh marker = %q", marker)
	}
	// A new item starts at its top.
	if body, marker = d.Window("b", detailLines("b", 30), 0, 5); !strings.HasPrefix(body, "b line 1\n") || marker != "1–5 / 30" {
		t.Fatalf("new item = %q, %q", body, marker)
	}
	if d.Scroll(-3) != 0 || d.Scroll(100) != 25 {
		t.Fatalf("scroll clamps to [0, total-height]: %d", d.Offset())
	}
	if _, marker = d.Window("b", "short", 0, 5); marker != "" || d.Overflows() {
		t.Fatalf("text that fits has no marker: %q", marker)
	}
}

func TestDetailWindowWrapsToWidth(t *testing.T) {
	d := &Detail{}
	_, marker := d.Window("a", strings.Repeat("word ", 20), 10, 3)
	if marker == "" || d.total < 8 {
		t.Fatalf("wrapped lines count as rows: total=%d marker=%q", d.total, marker)
	}
}

// detailModel is a page with a list on the left and a detail about the
// selected row on the right, lines lines long per row.
func detailModel(t *testing.T, lines int) (*Model, *List, *Detail) {
	t.Helper()
	r := NewRuntime()
	r.Define("thing")
	r.Instance("p")
	list, err := r.AddList("p/list", []ListItem{{ID: "a", Label: "a", Noun: "thing"}, {ID: "b", Label: "b", Noun: "thing"}}, Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.AddDetail("p/detail", Parent("p"))
	if err != nil {
		t.Fatal(err)
	}
	render := func(r *Runtime, w, h int) Block {
		sel, _ := list.Selected()
		body, marker := d.Window(sel.ID, detailLines(sel.ID, lines), w/2, h)
		return Text(list.View(w/2, h) + "\n" + marker + "\n" + body)
	}
	if _, err := r.AddPage("p", "P", render, Focus("p/list")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("p"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 80, 20
	m.View()
	return m, list, d
}

func TestDetailJKScrollWithFocusInTheList(t *testing.T) {
	m, _, d := detailModel(t, 100)
	r := m.Runtime
	focus := r.Focus
	for _, key := range []string{"J", "J", "1", "0", "J", "K"} {
		m.handleKey(MacroKey{Key: key})
		m.View()
	}
	if d.Offset() != 11 || r.Focus != focus || m.Status != "" {
		t.Fatalf("J J 10J K: offset=%d focus=%q status=%q; want 11 with focus kept", d.Offset(), r.Focus, m.Status)
	}
	if hints := BindingHints(r, r.Focus); !strings.Contains(hints, "J/K detail") || strings.Contains(hints, "detail.scroll") {
		t.Fatalf("hints = %q; want one J/K detail row", hints)
	}
	// Selecting another row starts its detail at the top.
	m.handleKey(MacroKey{Key: "j"})
	m.View()
	if d.Offset() != 0 || d.Item() != "b" {
		t.Fatalf("new selection: offset=%d item=%q", d.Offset(), d.Item())
	}
}

func TestDetailThatFitsHasNoHintAndJDoesNothing(t *testing.T) {
	m, _, d := detailModel(t, 3)
	r := m.Runtime
	if hints := BindingHints(r, r.Focus); strings.Contains(hints, "detail") {
		t.Fatalf("hints = %q; a detail that fits has none", hints)
	}
	m.handleKey(MacroKey{Key: "J"})
	if d.Offset() != 0 {
		t.Fatalf("offset = %d", d.Offset())
	}
}

func TestDetailRowsStandAsideForAnInnerJK(t *testing.T) {
	m, _, d := detailModel(t, 100)
	r := m.Runtime
	moved := 0
	r.Register(Operation{Verb: "nudge", Noun: "thing", Run: func(*Runtime, Context, Arguments) (any, error) { moved++; return nil, nil }})
	r.Define("thing").Mode("normal").Bind("J", Do("thing.nudge"))
	m.handleKey(MacroKey{Key: "J"})
	if moved != 1 || d.Offset() != 0 {
		t.Fatalf("inner J: moved=%d offset=%d", moved, d.Offset())
	}
	m.handleKey(MacroKey{Key: "K"})
	if d.Offset() != 0 {
		t.Fatalf("K at the top stays at 0: %d", d.Offset())
	}
}
