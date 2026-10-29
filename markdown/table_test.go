package markdown

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// auditTables are two tables of o-plans' vim-parity-audit.md: a narrow
// one of numbers and a wide one of code spans whose cells wrap.
const auditTables = "## Status counts\n\n" +
	"| Area | same | similar | differs on purpose | missing | conflicts | rows |\n" +
	"|---|---|---|---|---|---|---|\n" +
	"| 1. Motions | 9 | 4 | 5 | 6 | 2 | 26 |\n" +
	"| 2. Operators and text objects (editor) | 6 | 0 | 2 | 8 | 2 | 18 |\n" +
	"| **Total** | 61 | 42 | 25 | 45 | 25 | 198 |\n\n" +
	"## 1. Motions\n\n" +
	"| Feature | Vim/Neovim | ochazuke today | Status | Suggested action |\n" +
	"|---|---|---|---|---|\n" +
	"| `j`/`k` on a list | `:help j` `:help k`: stop at the last/first line | `bindings/list.json` `ochazuke.list/normal j`/`k` → `list.move {delta}`; past the edge the focus moves to the next/previous stop | **differs on purpose** | Keep it: the invariant \"motions are visual\" (AGENTS.md). `NoSpill()` and modals clamp the way Vim does. |\n" +
	"| Counts (`3j`, `5G`) | `:help count` | `bindings/registers.json` `app/normal digit-0` → `count_pending`; rows take `from: {steps: count}` | **same** | — |\n" +
	"| `ctrl+d`/`ctrl+u` on lists | `:help CTRL-D`: half a page | `list.json` → `list.scroll {half}` | **similar** (the count does not stick) | Note it in the docs. |\n"

// columnsOf is the display columns of a line's separators (│ and ┼, |
// in the plain style).
func columnsOf(line string) []int {
	var at []int
	col := 0
	for _, r := range ansi.Strip(line) {
		if r == '│' || r == '┼' || r == '|' {
			at = append(at, col)
		}
		col += ansi.StringWidth(string(r))
	}
	return at
}

// tableBlocks groups the lines that hold a separator into tables (runs of
// such lines), each line's separator columns.
func tableBlocks(lines []string) [][][]int {
	var out [][][]int
	in := false
	for _, l := range lines {
		cols := columnsOf(l)
		if len(cols) == 0 {
			in = false
			continue
		}
		if !in {
			out = append(out, nil)
			in = true
		}
		out[len(out)-1] = append(out[len(out)-1], cols)
	}
	return out
}

// Every line of a table, a wrapped cell's continuation lines too, puts
// its separators in the columns of the header rule's junctions, in every
// style and width, drawn by Block and by a Doc (whose element markers
// once shifted each row's first line one column left).
func TestTableColumnsLineUp(t *testing.T) {
	for _, style := range []string{StyleDark, StyleLight, StylePlain} {
		for _, width := range []int{120, 80, 50} {
			els := Elements(auditTables)
			renders := map[string][]string{
				"block": render(auditTables, 0, width, style, options{}).lines,
				"doc":   layoutOf(auditTables, els, width, style, options{}, func(int) bool { return false }).lines,
			}
			for name, lines := range renders {
				tables := tableBlocks(lines)
				if len(tables) != 2 {
					t.Fatalf("%s %s %d: %d tables:\n%s", name, style, width, len(tables), ansi.Strip(strings.Join(lines, "\n")))
				}
				for _, tb := range tables {
					for i, cols := range tb {
						if !slices.Equal(cols, tb[0]) {
							t.Fatalf("%s %s %d: line %d separators %v, header %v:\n%s", name, style, width, i, cols, tb[0], ansi.Strip(strings.Join(lines, "\n")))
						}
					}
				}
				for _, l := range lines {
					if w := ansi.StringWidth(l); w > width {
						t.Fatalf("%s %s %d: line %d wide: %q", name, style, width, w, ansi.Strip(l))
					}
				}
			}
		}
	}
}

// A table narrower than the width stays as wide as its cells, and the
// rule under its header runs the table's whole width.
func TestTableCompactAndRuleComplete(t *testing.T) {
	src := "| a | bb |\n|---|---|\n| ccc | `d` |\n"
	lines := render(src, 0, 80, StyleDark, options{}).lines
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = strings.TrimRight(strings.ReplaceAll(ansi.Strip(l), "\u00a0", " "), " ")
	}
	want := []string{"   a   │ bb", "  ─────┼─────", "   ccc │  d"}
	if !slices.Equal(plain, want) {
		t.Fatalf("got\n%q\nwant\n%q", plain, want)
	}
	if !strings.HasSuffix(ansi.Strip(lines[1]), "─") || ansi.StringWidth(lines[1]) != ansi.StringWidth(lines[0]) {
		t.Fatalf("rule %q, header %q", ansi.Strip(lines[1]), ansi.Strip(lines[0]))
	}
}

// A table wider than the width wraps its cells: a column keeps its
// longest short word whole, the rest share the room by what they want.
func TestFitColumns(t *testing.T) {
	for _, tc := range []struct {
		natural, word []int
		avail         int
		want          []int
	}{
		{[]int{4, 7}, []int{4, 7}, 20, []int{4, 7}},
		{[]int{10, 100, 50}, []int{5, 30, 8}, 60, []int{7, 34, 19}},
		{[]int{38, 4, 7, 18}, []int{9, 4, 7, 7}, 40, []int{18, 4, 7, 11}},
		{[]int{30, 30}, []int{20, 20}, 8, []int{6, 6}},
	} {
		got := fitColumns(tc.natural, tc.word, tc.avail)
		if !slices.Equal(got, tc.want) {
			t.Fatalf("fitColumns(%v, %v, %d) = %v, want %v", tc.natural, tc.word, tc.avail, got, tc.want)
		}
		if sum(got) > max(tc.avail, sum(tc.want)) {
			t.Fatalf("%v over %d", got, tc.avail)
		}
	}
}

func sum(xs []int) int {
	n := 0
	for _, x := range xs {
		n += x
	}
	return n
}

// A Doc's table rows cover their wrapped lines: each row element's span
// runs from its first line to the line before the next row's.
func TestDocTableRowSpans(t *testing.T) {
	els := Elements(auditTables)
	lay := layoutOf(auditTables, els, 80, StyleDark, options{}, func(int) bool { return false })
	var rows []int
	for i, e := range els {
		if e.Kind == KindTable && e.Group == els[i].Group {
			rows = append(rows, i)
		}
	}
	if len(rows) != 8 {
		t.Fatalf("%d table rows", len(rows))
	}
	for k, e := range rows {
		s := lay.spans[e]
		if s[0] < 0 {
			t.Fatalf("row %d not drawn", e)
		}
		first := ansi.Strip(lay.lines[s[0]])
		if !strings.Contains(first, "│") {
			t.Fatalf("row %d starts at %q", e, first)
		}
		if k+1 < len(rows) && els[rows[k+1]].Group == els[e].Group {
			next := lay.spans[rows[k+1]][0]
			end := next - 1
			if els[e].Header {
				end-- // the rule
			}
			if s[1] != end {
				t.Fatalf("row %d spans %v, next row at %d:\n%s", e, s, next, ansi.Strip(strings.Join(lay.lines, "\n")))
			}
		}
	}
}

// A table in a list item is drawn where Glamour draws the item's next
// paragraph; text after a table keeps its own line; a table right after
// a paragraph line is drawn as a table; no placeholder or source is left.
func TestTablePlacement(t *testing.T) {
	src := "- item\n\n  | a | b |\n  |---|---|\n  | c | d |\n\nafter\n\npara\n| x | y |\n|---|---|\n| 1 | 2 |\n"
	for _, a := range []ochazuke.Appearance{{Theme: "mono"}, {Theme: "adaptive", Dark: true}} {
		text := strings.ReplaceAll(ansi.Strip(Block(src, 60, a).Text), "│", "|")
		for _, want := range []string{"item", "a | b", "c | d", "after", "para", "x | y", "1 | 2"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%q missing:\n%s", want, text)
			}
		}
		if strings.ContainsRune(text, tableMarker) || strings.Contains(text, "|---|") || strings.Contains(text, "| a |") {
			t.Fatalf("placeholder or source left:\n%s", text)
		}
	}
}
