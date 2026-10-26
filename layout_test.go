package ochazuke

import (
	"fmt"
	"regexp"
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
	page.Render = func(_ *Runtime, width, height int) Block {
		area = width * height
		return Text(strings.TrimSuffix(strings.Repeat(strings.Repeat("░", width)+"\n", height), "\n"))
	}
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 18}} {
		m.Width, m.Height = size[0], size[1]
		view := ansi.Strip(m.View().Content)
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

// An input's cursor travels through the panel border and padding and then the
// page gutter and header rows; the string wrappers render the same cells.
func TestPageAndPanelOffsetTheInputCursor(t *testing.T) {
	l := Layout{Width: 48, Height: 15, Styles: NewStyles(Appearance{Theme: "mono"})}
	in := NewInput("field", "hello\nworld", true)
	in.Cursor = 8 // Row 1, column 2.
	bw, bh := l.BodySize()
	cw, ch := PanelContentSize(bw, bh)
	editor := InputBlock(in, cw, ch)
	if editor.Cursor == nil || *editor.Cursor != (Pos{2, 1}) {
		t.Fatalf("input cursor = %+v", editor.Cursor)
	}
	panel := l.PanelBlock("Editor", editor, bw, bh, true)
	margin := (bw - 2 - cw) / 2
	if panel.Cursor == nil || *panel.Cursor != (Pos{1 + margin + 2, 1 + 1}) {
		t.Fatalf("panel cursor = %+v, want border and margin offsets", panel.Cursor)
	}
	page := l.PageBlock("Tasks", "Workspace", panel, "Esc back")
	want := Pos{layoutGutter + 1 + margin + 2, 3 + 1 + 1}
	if page.Cursor == nil || *page.Cursor != want {
		t.Fatalf("page cursor = %+v, want %+v", page.Cursor, want)
	}
	rows := strings.Split(ansi.Strip(page.Text), "\n")
	if len(rows) != 15 {
		t.Fatalf("got %d rows, want 15", len(rows))
	}
	for i, row := range rows {
		if ansi.StringWidth(row) != 48 {
			t.Fatalf("row %d has %d cells", i, ansi.StringWidth(row))
		}
	}
	if cell := cellIndex(rows[want.Y], "world"); cell+2 != want.X {
		t.Fatalf("cursor %+v is not on the r of world (column %d):\n%s", want, cell+2, ansi.Strip(page.Text))
	}
	if text := l.Page("Tasks", "Workspace", l.Panel("Editor", InputView(in, cw, ch), bw, bh, true), "Esc back"); text != page.Text {
		t.Fatal("string wrappers render different cells than the block composers")
	}
	// Split and Sidebar move the right or bottom panel's cursor by the left one.
	for _, width := range []int{80, 48} {
		wide := Layout{Width: width, Height: 24, Styles: l.Styles}
		bw, bh := wide.BodySize()
		split := wide.SplitBlock("Files", Text("main.go"), "Editor", InputBlock(in, 10, 2))
		sidebar := wide.SidebarBlock("Pages", []string{"Home"}, 0, "Editor", InputBlock(in, 10, 2))
		for name, block := range map[string]Block{"split": split, "sidebar": sidebar} {
			if block.Cursor == nil {
				t.Fatalf("%dx%d %s lost the cursor", width, 24, name)
			}
			row := strings.Split(ansi.Strip(block.Text), "\n")[block.Cursor.Y]
			if cell := cellIndex(row, "world"); cell < 0 || cell+2 != block.Cursor.X {
				t.Fatalf("%dx%d %s cursor %+v is not on world in %q", width, 24, name, block.Cursor, row)
			}
		}
		if width >= layoutStackAt && split.Cursor.X <= (bw-1)*2/5 || width < layoutStackAt && split.Cursor.Y <= bh*2/5 {
			t.Fatalf("%d: split cursor %+v is inside the left panel", width, split.Cursor)
		}
		if left := wide.SplitBlock("Editor", InputBlock(in, 10, 2), "Files", Text("main.go")); left.Cursor == nil || left.Cursor.X != 1+1+2 { // border, centering margin, column
			t.Fatalf("left panel cursor = %+v", left.Cursor)
		}
	}
	if (Layout{Width: 0, Height: 0}).PageBlock("T", "S", InputBlock(in, 4, 2), "F").Cursor != nil {
		t.Fatal("empty viewport reported a cursor")
	}
}

// tabCounts reads a stripped tab row: the tabs it shows and the pages its
// ‹N and N› markers say are hidden.
func tabCounts(row string) (shown, hidden int) {
	for _, field := range strings.Fields(row) {
		var n int
		switch {
		case strings.HasPrefix(field, "‹"):
			fmt.Sscanf(strings.TrimPrefix(field, "‹"), "%d", &n)
			hidden += n
		case strings.HasSuffix(field, "›") && field != "›":
			fmt.Sscanf(strings.TrimSuffix(field, "›"), "%d", &n)
			hidden += n
		case field == "›":
		default:
			shown++
		}
	}
	return shown, hidden
}

var rightMarker = regexp.MustCompile(`[0-9]+›`)

func TestTabsMarkHiddenPagesOnEachSide(t *testing.T) {
	l := Layout{Styles: NewStyles(Appearance{Theme: "mono"})}
	titles := make([]string, 50)
	for i := range titles {
		titles[i] = fmt.Sprintf("p%02d", i+1)
	}
	for _, width := range []int{30, 50, 80, 120} {
		for _, active := range []int{0, 24, 49} {
			row := ansi.Strip(l.tabs(titles, active, width))
			if w := ansi.StringWidth(row); w > width {
				t.Fatalf("width %d active %d: row is %d cells: %q", width, active, w, row)
			}
			if !strings.Contains(row, "› "+titles[active]) {
				t.Fatalf("width %d: active %s missing: %q", width, titles[active], row)
			}
			if shown, hidden := tabCounts(row); shown+hidden != len(titles) {
				t.Fatalf("width %d active %d: %d shown + %d hidden != %d: %q", width, active, shown, hidden, len(titles), row)
			}
			if active == 0 && strings.Contains(row, "‹") || active == 49 && rightMarker.MatchString(row) {
				t.Fatalf("width %d active %d: marker on a side with nothing hidden: %q", width, active, row)
			}
		}
	}
	if row := ansi.Strip(l.tabs(titles[:3], 1, 80)); strings.ContainsAny(row, "‹") || strings.Count(row, "›") != 1 {
		t.Fatalf("everything fits, yet markers: %q", row)
	}
}

// SplitFocusBlock draws the named panel focused (accent title) and the
// other plain, side by side and stacked; SplitBlock keeps the left focused.
// With both bodies carrying a cursor, the focused panel's wins.
func TestSplitFocusBlockTakesTheFocusedSide(t *testing.T) {
	for _, width := range []int{100, 60} {
		l := Layout{Width: width, Height: 20, Styles: NewStyles(Appearance{Theme: "adaptive"})}
		accent := func(title string) string { return l.Styles.Accent.Render(" " + title + " ") }
		withCursor := func(text string) Block { return Block{Text: text, Cursor: &Pos{}} }
		for _, tc := range []struct {
			side          SplitSide
			left, right   bool
			cursorOnRight bool
		}{{SplitLeft, true, false, false}, {SplitRight, false, true, true}, {SplitNone, false, false, false}} {
			b := l.SplitFocusBlock(tc.side, "Tree", withCursor("left"), "Detail", withCursor("right"))
			if strings.Contains(b.Text, accent("Tree")) != tc.left || strings.Contains(b.Text, accent("Detail")) != tc.right {
				t.Fatalf("width %d side %d: focused titles left=%v right=%v", width, tc.side, strings.Contains(b.Text, accent("Tree")), strings.Contains(b.Text, accent("Detail")))
			}
			left := l.SplitFocusBlock(SplitLeft, "Tree", withCursor("left"), "Detail", withCursor("right"))
			if tc.cursorOnRight && (b.Cursor == nil || *b.Cursor == *left.Cursor) || !tc.cursorOnRight && b.Cursor != nil && *b.Cursor != *left.Cursor {
				t.Fatalf("width %d side %d: cursor %+v (left focused %+v)", width, tc.side, b.Cursor, left.Cursor)
			}
		}
		if a, b := l.SplitBlock("Tree", Text("x"), "Detail", Text("y")), l.SplitFocusBlock(SplitLeft, "Tree", Text("x"), "Detail", Text("y")); a.Text != b.Text {
			t.Fatal("SplitBlock is not SplitFocusBlock(SplitLeft)")
		}
	}
}

// splitDivider is the cell (side by side) or row (stacked) where the second
// panel of a mono split starts: the column of the second top-left corner on
// the first body row, or the row of the second top-left corner.
func splitDivider(t *testing.T, view string, stacked bool) int {
	t.Helper()
	rows := strings.Split(ansi.Strip(view), "\n")
	if stacked {
		seen := 0
		for i, row := range rows {
			if strings.Contains(row, "╭") {
				seen++
				if seen == 2 {
					return i
				}
			}
		}
		t.Fatalf("no second panel:\n%s", strings.Join(rows, "\n"))
	}
	for _, row := range rows {
		if strings.Count(row, "╭") == 2 {
			runes := []rune(row)
			for i := len(runes) - 1; i >= 0; i-- {
				if runes[i] == '╭' {
					return i
				}
			}
		}
	}
	t.Fatalf("no panel pair:\n%s", strings.Join(rows, "\n"))
	return 0
}

func TestSplitShiftMovesTheDividerWithinLimits(t *testing.T) {
	styles := NewStyles(Appearance{Theme: "mono"})
	for _, tc := range []struct {
		width, height int
		stacked       bool
	}{{100, 30, false}, {60, 40, true}} {
		bw, bh := Layout{Width: tc.width, Height: tc.height}.BodySize()
		at := func(shift int) int {
			l := Layout{Width: tc.width, Height: tc.height, Styles: styles, Shift: shift}
			view := l.Split("Files", "left", "Preview", "right")
			rows := strings.Split(view, "\n")
			if len(rows) != bh {
				t.Fatalf("shift %d: %d rows, want %d", shift, len(rows), bh)
			}
			for i, row := range rows {
				if w := ansi.StringWidth(row); w != bw {
					t.Fatalf("shift %d row %d: %d cells, want %d", shift, i, w, bw)
				}
			}
			return splitDivider(t, view, tc.stacked)
		}
		base, grown, shrunk := at(0), at(PaneStep), at(-PaneStep)
		if !(shrunk < base && base < grown) {
			t.Fatalf("%dx%d: divider at shrink %d, default %d, grow %d; want rising", tc.width, tc.height, shrunk, base, grown)
		}
		least, total, gutter := PaneMinWidth, bw-1, 1 // the gap cell side by side
		if tc.stacked {
			least, total, gutter = PaneMinHeight, bh, 0
		}
		if got, want := at(1000), gutter+total-least; got != want {
			t.Fatalf("%dx%d: grown past the limit, divider %d, want %d", tc.width, tc.height, got, want)
		}
		if got, want := at(-1000), gutter+least; got != want {
			t.Fatalf("%dx%d: shrunk past the limit, divider %d, want %d", tc.width, tc.height, got, want)
		}
	}
}

func TestDivideKeepsTheDefaultWhenTooSmall(t *testing.T) {
	if got := (Layout{Shift: 20}).Divide(10, 20, PaneMinWidth); got != 10 {
		t.Fatalf("a split narrower than two minimum panels moved: %d", got)
	}
	if got := (Layout{}).Divide(37, 95, PaneMinWidth); got != 37 {
		t.Fatalf("no shift moved the divider: %d", got)
	}
}
