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
	r.Instance("page", Kind("page"))
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
	r.Define("pane").Columns(Column{Title: "name"}, Column{Title: "cwd", Priority: 1})
	if got := r.Noun("pane").Columns; len(got) != 2 || got[1].Title != "cwd" {
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
