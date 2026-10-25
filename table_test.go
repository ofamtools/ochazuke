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
