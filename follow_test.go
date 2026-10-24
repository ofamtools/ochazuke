package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// followPage is a page of three ten-row lists (a, b, c), each under a
// heading line, on a 60x24 screen: list k's row i is body line 11k+1+i.
func followPage(t *testing.T, lists int) (*Model, *Runtime) {
	t.Helper()
	r := NewRuntime()
	r.Instance("page")
	names := []string{"a", "b", "c"}[:lists]
	for _, id := range names {
		var items []ListItem
		for i := range 10 {
			items = append(items, ListItem{ID: fmt.Sprintf("%s%d", id, i), Label: fmt.Sprintf("row %s%d", id, i), Noun: "list"})
		}
		if _, err := r.AddList(id, items, Parent("page")); err != nil {
			t.Fatal(err)
		}
	}
	render := func(r *Runtime, width, height int) Block {
		var parts []string
		for _, id := range names {
			parts = append(parts, "heading "+id, r.List(id).View(width, 10))
		}
		return Text(strings.Join(parts, "\n"))
	}
	if _, err := r.AddPage("page", "Page", render, Focus("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 60, 24
	m.View()
	return m, r
}

// focusedLine is the body line of the focused row, from the page's layout
// (not from the placement record under test).
func focusedLine(t *testing.T, r *Runtime) int {
	t.Helper()
	for k, id := range []string{"a", "b", "c"} {
		if l := r.List(id); l != nil && l.Focused() {
			selected, _ := l.Selected()
			var i int
			fmt.Sscanf(selected.ID[1:], "%d", &i)
			return 11*k + 1 + i
		}
	}
	t.Fatalf("no focused list (focus %q)", r.Focus)
	return 0
}

// assertVisible checks the focused row is on screen with two lines of
// margin, except where the body's top or bottom edge stops the scroll.
func assertVisible(t *testing.T, m *Model, r *Runtime, step string) {
	t.Helper()
	screen := ansi.Strip(m.View().Content)
	if strings.Contains(m.View().Content, placeMark) {
		t.Fatalf("%s: a placement marker reached the screen", step)
	}
	_, height := m.pageSize()
	offset, line, total := m.scroll["page"], focusedLine(t, r), 33
	if line < offset || line >= offset+height {
		t.Fatalf("%s: focused line %d outside [%d, %d)", step, line, offset, offset+height)
	}
	if offset > 0 && line < offset+2 {
		t.Fatalf("%s: line %d inside the top margin of offset %d", step, line, offset)
	}
	if offset < total-height && line > offset+height-3 {
		t.Fatalf("%s: line %d inside the bottom margin of offset %d (height %d)", step, line, offset, height)
	}
	selected := ""
	for _, id := range []string{"a", "b", "c"} {
		if l := r.List(id); l.Focused() {
			s, _ := l.Selected()
			selected = s.Label
		}
	}
	if !strings.Contains(screen, selected) {
		t.Fatalf("%s: %q not on screen:\n%s", step, selected, screen)
	}
}

func TestPageScrollFollowsListSelection(t *testing.T) {
	m, r := followPage(t, 3)
	for i := range 15 {
		keytest.Press(m, "j")
		assertVisible(t, m, r, fmt.Sprintf("j %d", i+1))
	}
	if r.Focus != "b5" || m.scroll["page"] == 0 {
		t.Fatalf("after 15 j: focus %q offset %d", r.Focus, m.scroll["page"])
	}
}

func TestPageScrollFollowsFocusJumps(t *testing.T) {
	m, r := followPage(t, 3)
	keytest.Press(m, "}")
	keytest.Press(m, "}")
	assertVisible(t, m, r, "} }")
	if !r.List("c").Focused() || m.scroll["page"] == 0 {
		t.Fatalf("} } to c: focus %q offset %d", r.Focus, m.scroll["page"])
	}
	down := m.scroll["page"]
	keytest.Press(m, "{")
	keytest.Press(m, "{")
	assertVisible(t, m, r, "{ {")
	if !r.List("a").Focused() || m.scroll["page"] >= down {
		t.Fatalf("{ { back to a: focus %q offset %d (was %d)", r.Focus, m.scroll["page"], down)
	}
}

// pgdown moves the surface only, leaving the focus off screen; the next
// motion brings the focused row back.
func TestPageScrollPgdownThenMotion(t *testing.T) {
	m, r := followPage(t, 3)
	keytest.Press(m, "pgdown")
	m.View()
	keytest.Press(m, "pgdown")
	m.View()
	if m.scroll["page"] <= 1 {
		t.Fatalf("pgdown did not scroll: %d", m.scroll["page"])
	}
	scrolled := m.scroll["page"]
	m.View()
	if m.scroll["page"] != scrolled {
		t.Fatalf("a frame without a focus move scrolled: %d -> %d", scrolled, m.scroll["page"])
	}
	keytest.Press(m, "j")
	assertVisible(t, m, r, "pgdown j")
	if r.Focus != "a1" {
		t.Fatalf("focus %q", r.Focus)
	}
}

func TestPageShorterThanViewportNeverScrolls(t *testing.T) {
	m, r := followPage(t, 1)
	for _, key := range []string{"j", "j", "j", "j", "j", "j", "j", "j", "j", "k"} {
		keytest.Press(m, key)
		m.View()
		if m.scroll["page"] != 0 {
			t.Fatalf("%s: short page scrolled to %d (focus %q)", key, m.scroll["page"], r.Focus)
		}
	}
}

// A list that draws a screen of rows windows itself; the page does not
// move a second time for the same motion.
func TestPageScrollComposesWithListWindow(t *testing.T) {
	r := NewRuntime()
	r.Instance("page")
	var items []ListItem
	for i := range 200 {
		items = append(items, ListItem{ID: fmt.Sprintf("r%d", i), Label: fmt.Sprintf("row %d", i), Noun: "list"})
	}
	if _, err := r.AddList("big", items, Parent("page")); err != nil {
		t.Fatal(err)
	}
	render := func(r *Runtime, width, height int) Block {
		return Text("heading\n" + r.List("big").View(width, height-2))
	}
	if _, err := r.AddPage("page", "Page", render, Focus("big")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 60, 24
	m.View()
	for i := range 60 {
		keytest.Press(m, "j")
		screen := ansi.Strip(m.View().Content)
		if want := fmt.Sprintf("row %d", i+1); !strings.Contains(screen, want+"\n") && !strings.Contains(screen, want+" ") {
			t.Fatalf("j %d: %q not on screen:\n%s", i+1, want, screen)
		}
		if m.scroll["page"] != 0 {
			t.Fatalf("j %d: the page scrolled (%d) though the list windows itself", i+1, m.scroll["page"])
		}
	}
}

// The marker finds a view's line through any composition: here two lists
// inside bordered panels joined side by side under a heading.
func TestPlacementLinesSurviveComposition(t *testing.T) {
	r := NewRuntime()
	r.Instance("page")
	for _, id := range []string{"left", "right"} {
		var items []ListItem
		for i := range 5 {
			items = append(items, ListItem{ID: fmt.Sprintf("%s%d", id, i), Label: fmt.Sprintf("%s %d", id, i), Noun: "list"})
		}
		if _, err := r.AddList(id, items, Parent("page")); err != nil {
			t.Fatal(err)
		}
	}
	r.List("right").Select("right3")
	render := func(r *Runtime, width, height int) Block {
		box := lipgloss.NewStyle().Border(lipgloss.NormalBorder())
		left := box.Render(r.List("left").View(20, 5))
		right := box.Render("title\n" + r.List("right").View(20, 5))
		return Text("one\ntwo\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, right))
	}
	page, err := r.AddPage("page", "Page", render, Focus("left"))
	if err != nil {
		t.Fatal(err)
	}
	body := page.render(r, 60, 20).Text
	if strings.Contains(body, placeMark) {
		t.Fatalf("marker left in body:\n%q", body)
	}
	want := map[string]Placement{
		"left":  {ID: "left", Line: 3, Height: 5, Focus: 0, FocusHeight: 1},
		"right": {ID: "right", Line: 4, Height: 5, Focus: 3, FocusHeight: 1},
	}
	if len(page.drawn) != 2 {
		t.Fatalf("drawn %+v", page.drawn)
	}
	for _, got := range page.drawn {
		if got != want[got.ID] {
			t.Fatalf("placement %+v, want %+v", got, want[got.ID])
		}
	}
}
