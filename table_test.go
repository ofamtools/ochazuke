package ochazuke

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTableLayoutDropsByPriority(t *testing.T) {
	cols := []Column{{Title: "name"}, {Title: "kind", Priority: 2}, {Title: "state", Priority: 1}, {Title: "where", Priority: 2}}
	rows := [][]string{{"alpha-long-name", "claude", "idle", "~/proj/x"}}
	for _, c := range []struct {
		width int
		shown []int
	}{
		{80, []int{0, 1, 2, 3}},
		{40, []int{0, 1, 2, 3}},
		{30, []int{0, 1, 2}}, // kind and where tie at 2: the rightmost drops first
		{22, []int{0, 2}},
		{12, []int{0}},
		{3, []int{0}},
	} {
		shown, widths := tableLayout(cols, rows, c.width)
		if !slices.Equal(shown, c.shown) {
			t.Fatalf("width %d: shown %v, want %v", c.width, shown, c.shown)
		}
		total := tableGap * (len(widths) - 1)
		for _, w := range widths {
			total += w
		}
		if total > c.width {
			t.Fatalf("width %d: columns take %d (%v)", c.width, total, widths)
		}
	}
}

func TestTableRendersCellsAndFallsBackToLabel(t *testing.T) {
	r := NewRuntime()
	r.Define("row")
	r.Instance("page", Kind("ochazuke.page"))
	cols := []Column{{Title: "name"}, {Title: "n", Width: 3, Align: AlignRight}}
	l, err := r.AddList("t", []ListItem{
		{ID: "a", Label: "alpha", Noun: "row", Cells: []string{"alpha", "7"}},
		{ID: "b", Label: "beta only", Noun: "row"},
	}, Parent("page"), Columns(cols...))
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Columns(); !slices.Equal(got, cols) {
		t.Fatalf("columns %v", got)
	}
	lines := strings.Split(ansi.Strip(l.View(30, 4)), "\n")
	want := []string{"name         n", "alpha        7", "beta only"}
	for i, w := range want {
		if strings.TrimRight(lines[i], " ") != w {
			t.Fatalf("line %d %q, want %q\n%s", i, lines[i], w, strings.Join(lines, "\n"))
		}
	}
	l.SetColumns()
	if got := strings.TrimRight(ansi.Strip(strings.Split(l.View(30, 4), "\n")[0]), " "); got != "alpha" {
		t.Fatalf("plain list first line %q, want the label", got)
	}
}

func TestNounColumnsAreDeclaredData(t *testing.T) {
	r := NewRuntime()
	r.Define("ochazuke.pane").Columns(Column{Title: "name"}, Column{Title: "cwd", Priority: 1})
	if got := r.Noun("ochazuke.pane").Columns; len(got) != 2 || got[1].Title != "cwd" {
		t.Fatalf("noun columns %v", got)
	}
}

// TestTableText: the plain-text layout cuts and drops by Priority at a
// width, and at width 0 keeps every column at its natural width, a fixed
// Width included; control characters become spaces.
func TestTableText(t *testing.T) {
	cols := []Column{{Title: "name"}, {Title: "size", Align: AlignRight, Priority: 2}, {Title: "path", Width: 6, Priority: 1}}
	rows := [][]string{{"alpha", "12", "/very/long/path"}, {"b\tc", "3456"}}
	got := strings.Join(TableText(cols, rows, 0), "\n")
	want := "name   size  path\n" +
		"alpha    12  /very/long/path\n" +
		"b c    3456"
	if got != want {
		t.Fatalf("width 0:\n%s\nwant:\n%s", got, want)
	}
	got = strings.Join(TableText(cols, rows, 14), "\n")
	want = "name   path\n" +
		"alpha  /very…\n" +
		"b c"
	if got != want {
		t.Fatalf("width 14:\n%s\nwant:\n%s", got, want)
	}
	if TableText(nil, rows, 0) != nil {
		t.Fatal("no columns should be no lines")
	}
}

// Max caps a flexible column's growth; the room it leaves goes to the
// other flexible columns.
func TestTableMaxCapsAFlexibleColumn(t *testing.T) {
	cols := []Column{{Title: "args", Max: 6}, {Title: "description"}}
	rows := [][]string{{strings.Repeat("x", 30), strings.Repeat("y", 60)}}
	_, widths := tableLayout(cols, rows, 40)
	if !slices.Equal(widths, []int{6, 32}) {
		t.Fatalf("widths %v, want [6 32]", widths)
	}
}

// A Wrap column's long cell wraps onto further lines of its row; the
// other cells sit on the first line, and the selected row is highlighted
// on every one of its lines. A Section opens a run of rows with a label
// line, repeated at the top when the window starts inside the section.
func TestTableWrapsCellsAndLabelsSections(t *testing.T) {
	r := NewRuntime()
	r.Define("row")
	r.Instance("page", Kind("ochazuke.page"))
	cols := []Column{{Title: "key"}, {Title: "text", Min: 8, Wrap: true}}
	l, err := r.AddList("t", []ListItem{
		{ID: "a", Noun: "row", Cells: []string{"a", "one two three four"}, Section: "first"},
		{ID: "b", Noun: "row", Cells: []string{"b", "five"}, Section: "first"},
		{ID: "c", Noun: "row", Cells: []string{"c", "six seven"}, Section: "second"},
	}, Parent("page"), Columns(cols...))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Focus(); err != nil {
		t.Fatal(err)
	}
	strip := func(text string) []string {
		var out []string
		for _, line := range strings.Split(ansi.Strip(text), "\n") {
			out = append(out, strings.TrimRight(line, " "))
		}
		return out
	}
	text := l.View(13, 10)
	want := []string{"key  text", "first", "a    one two", "     three", "     four", "b    five", "second", "c    six", "     seven", ""}
	if got := strip(text); !slices.Equal(got, want) {
		t.Fatalf("view\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if lit := highlightedLines(text); !slices.Equal(lit, []string{"a    one two", "three", "four"}) {
		t.Fatalf("highlighted %q", lit)
	}
	l.Move(1)
	l.Move(1)
	got := strip(l.View(13, 5))
	want = []string{"key  text", "second", "c    six", "     seven", "3–3 / 3"}
	if !slices.Equal(got, want) {
		t.Fatalf("scrolled view\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	l.Move(-1)
	got = strip(l.View(13, 5))
	want = []string{"key  text", "first", "b    five", "second", "2–2 / 3"}
	if !slices.Equal(got, want) {
		t.Fatalf("window inside a section\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Columns without titles draw no header: the first line is the first row.
func TestTableWithoutTitlesHasNoHeader(t *testing.T) {
	r := NewRuntime()
	r.Define("row")
	r.Instance("page", Kind("ochazuke.page"))
	l, err := r.AddList("t", []ListItem{
		{ID: "a", Noun: "row", Cells: []string{"scrolloff", "2"}},
		{ID: "b", Noun: "row", Cells: []string{"", "item"}},
	}, Parent("page"), Columns(Column{}, Column{}))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(ansi.Strip(l.View(30, 4)), "\n")
	if strings.TrimRight(lines[0], " ") != "scrolloff  2" || strings.TrimRight(lines[1], " ") != "           item" {
		t.Fatalf("lines %q", lines)
	}
}

// A Group column draws a cell blank when it repeats the row above's in
// the same section; a new section, a different value and the first row a
// frame draws show it, and the rows keep their cells.
func TestGroupColumnShowsARepeatedCellOnce(t *testing.T) {
	r := NewRuntime()
	r.Define("ochazuke.binding")
	items := []ListItem{
		{ID: "1", Cells: []string{"", "q", "quit"}, Section: "app", Noun: "ochazuke.binding"},
		{ID: "2", Cells: []string{"g", "g", "top"}, Section: "app", Noun: "ochazuke.binding"},
		{ID: "3", Cells: []string{"g", "h", "home"}, Section: "app", Noun: "ochazuke.binding"},
		{ID: "4", Cells: []string{"g", "j", "down"}, Section: "list", Noun: "ochazuke.binding"},
		{ID: "5", Cells: []string{"z", "l", "right"}, Section: "list", Noun: "ochazuke.binding"},
		{ID: "6", Cells: []string{"z", "h", "left"}, Section: "list", Noun: "ochazuke.binding"},
	}
	l, err := r.AddList("l", items, Columns(Column{Title: "", Group: true}, Column{Title: "key"}, Column{Title: "does"}))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, line := range strings.Split(ansi.Strip(l.View(40, 20)), "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			got = append(got, line)
		}
	}
	want := []string{"key does", "app", "q quit", "g g top", "h home", "list", "g j down", "z l right", "h left"}
	if !slices.Equal(got, want) {
		t.Fatalf("lines %q, want %q", got, want)
	}
	if l.Items()[2].Cells[0] != "g" {
		t.Fatalf("the row lost its cell: %q", l.Items()[2].Cells)
	}
	// Scrolled so the window starts on the second z row: it shows its z.
	if err := l.Select("6"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(ansi.Strip(l.View(40, 3)), "\n")
	if !slices.ContainsFunc(lines, func(line string) bool { return strings.Join(strings.Fields(line), " ") == "z h left" }) {
		t.Fatalf("first drawn row lost its group cell:\n%s", strings.Join(lines, "\n"))
	}
	// One-line rows without sections (tableView) blank the same way.
	for i := range items {
		items[i].Section = ""
	}
	l.SetItems(items)
	got = nil
	for _, line := range strings.Split(ansi.Strip(l.View(40, 20)), "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			got = append(got, line)
		}
	}
	want = []string{"key does", "q quit", "g g top", "h home", "j down", "z l right", "h left"}
	if !slices.Equal(got, want) {
		t.Fatalf("one-line lines %q, want %q", got, want)
	}
}
