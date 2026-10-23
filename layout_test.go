package ochazuke

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestLayoutsStayInsideViewport(t *testing.T) {
	styles := NewStyles(Appearance{Theme: "mono"})
	body := strings.Repeat("\x1b[32m界 e\u0301 👩‍💻\x1b[0m\t"+strings.Repeat("x", 120)+"\n", 35)
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 18}, {12, 8}, {9, 7}, {3, 3}, {2, 2}, {1, 1}, {0, 10}, {10, 0}, {-1, 4}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			l := Layout{Width: size[0], Height: size[1], Styles: styles}
			for name, view := range map[string]string{
				"page":    l.Page("Tasks", "Workspace", l.Split("Files", body, "Preview", body), "Esc back"),
				"panel":   l.Panel("Preview", body, size[0], size[1], false),
				"sidebar": l.Page("Tasks", "Workspace", l.Sidebar("Files", []string{"界 e\u0301 👩‍💻", "main.go"}, 1, "Preview", body), "Esc back"),
			} {
				if size[0] <= 0 || size[1] <= 0 {
					if view != "" {
						t.Fatalf("%s rendered an empty viewport: %q", name, view)
					}
					continue
				}
				rows := strings.Split(view, "\n")
				if len(rows) != size[1] {
					t.Fatalf("%s: got %d rows, want %d", name, len(rows), size[1])
				}
				for i, row := range rows {
					if got := ansi.StringWidth(row); got != size[0] {
						t.Fatalf("%s row %d: %d cells, want %d", name, i, got, size[0])
					}
				}
			}
		})
	}
}

func TestSplitReflowsWithoutLosingEitherPane(t *testing.T) {
	for _, width := range []int{80, 60} {
		l := Layout{Width: width, Height: 24, Styles: NewStyles(Appearance{Theme: "mono"})}
		view := ansi.Strip(l.Split("Files", "left-content", "Preview", "right-content"))
		leftRow, rightRow := -1, -1
		for i, row := range strings.Split(view, "\n") {
			if strings.Contains(row, "left-content") {
				leftRow = i
			}
			if strings.Contains(row, "right-content") {
				rightRow = i
			}
		}
		if leftRow < 0 || rightRow < 0 {
			t.Fatalf("%d: lost content:\n%s", width, view)
		}
		if width == 80 && leftRow != rightRow {
			t.Fatalf("wide panes are not side by side:\n%s", view)
		}
		if width == 60 && leftRow >= rightRow {
			t.Fatalf("narrow panes are not stacked:\n%s", view)
		}
	}
}

func TestPanelMakesOverflowAndFocusVisibleWithoutColor(t *testing.T) {
	l := Layout{Styles: NewStyles(Appearance{Theme: "mono"})}
	body := "first\nsecond\nthird\nfourth\nfifth"
	view := ansi.Strip(l.Panel("Results", body, 24, 5, true))
	if !strings.Contains(view, "first") || !strings.Contains(view, "second") || !strings.Contains(view, "… 3 more") || strings.Contains(view, "third") {
		t.Fatalf("hidden rows not indicated correctly:\n%s", view)
	}
	if !strings.Contains(view, "━") || strings.Contains(ansi.Strip(l.Panel("Results", body, 24, 5, false)), "━") {
		t.Fatalf("focus requires color:\n%s", view)
	}
}

func TestModalsNeverReplaceTheBackdropPerimeter(t *testing.T) {
	styles := NewStyles(Appearance{Theme: "mono"})
	for _, size := range [][2]int{{80, 24}, {60, 18}, {3, 3}, {2, 2}, {1, 1}} {
		w, h := size[0], size[1]
		background := strings.TrimSuffix(strings.Repeat(strings.Repeat("b", w)+"\n", h), "\n")
		view := ansi.Strip(ComposeModal(background, "Title", strings.Repeat("modal\n", 40), "Esc close", w, h, styles))
		rows := strings.Split(view, "\n")
		if rows[0] != strings.Repeat("b", w) || rows[h-1] != strings.Repeat("b", w) {
			t.Fatalf("%dx%d: modal consumed backdrop:\n%s", w, h, view)
		}
		for _, row := range rows {
			if !strings.HasPrefix(row, "b") || !strings.HasSuffix(row, "b") {
				t.Fatalf("%dx%d: missing side gutter: %q", w, h, row)
			}
		}
	}
}

func ExampleLayout() {
	layout := Layout{
		Width: 80, Height: 24,
		Styles: NewStyles(Appearance{Theme: "adaptive"}),
	}
	body := layout.Split("Files", "main.go\nlayout.go", "Preview", "Selected file contents")
	page := layout.Page("Workspace", "Browse files", body, "Enter open · Esc back")
	fmt.Print(page)
}

func TestSidebarKeepsSelectionVisibleAcrossResize(t *testing.T) {
	items := make([]string, 30)
	for i := range items {
		items[i] = fmt.Sprintf("Item %02d", i+1)
	}
	for _, width := range []int{80, 120, 60} {
		l := Layout{Width: width, Height: 18, Styles: NewStyles(Appearance{Theme: "mono"})}
		view := ansi.Strip(l.Sidebar("Navigate", items, 29, "Content", "Selected item details"))
		if !strings.Contains(view, "▸ Item 30") || strings.Contains(view, "Item 01") || !strings.Contains(view, "/ 30") || !strings.Contains(view, "Selected item details") {
			t.Fatalf("%d columns lost selection, position hint, or main content:\n%s", width, view)
		}
		if width >= 72 {
			// The navigation rail must not grow when the main content gets wider.
			contentRow := strings.Split(view, "\n")[1]
			if ansi.StringWidth(strings.Split(contentRow, "│")[0]) != 29 {
				t.Fatalf("%d columns changed the navigation rail width: %q", width, contentRow)
			}
		}
	}
}

func ExampleLayout_Sidebar() {
	layout := Layout{Width: 80, Height: 24, Styles: NewStyles(Appearance{Theme: "adaptive"})}
	body := layout.Sidebar("Workspace", []string{"Files", "Search", "Settings"}, 1, "Search", "Search results")
	fmt.Print(layout.Page("Project", "Workspace navigation", body, "↑↓ select · Enter open"))
}

func TestModelSidebarHonorsPageRenderBounds(t *testing.T) {
	r := NewRuntime()
	page := pageTestRegister(t, r, "alpha")
	m := NewModel(r)
	m.Sidebar = true
	if err := r.OpenPage("alpha"); err != nil {
		t.Fatal(err)
	}
	area := 0
	page.Render = func(_ *Runtime, width, height int) string {
		area = width * height
		return strings.TrimSuffix(strings.Repeat(strings.Repeat("░", width)+"\n", height), "\n")
	}
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 18}} {
		m.Width, m.Height = size[0], size[1]
		view := ansi.Strip(m.View())
		if strings.Count(view, "░") != area || !strings.Contains(view, "▸ ALPHA") {
			t.Fatalf("%dx%d: sidebar clipped the page's advertised body area:\n%s", size[0], size[1], view)
		}
		rows := strings.Split(view, "\n")
		if len(rows) != size[1] {
			t.Fatalf("got %d rows, want %d", len(rows), size[1])
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != size[0] {
				t.Fatalf("unbounded sidebar row: %q", row)
			}
		}
	}
}
