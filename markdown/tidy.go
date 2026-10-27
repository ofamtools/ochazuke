package markdown

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// tidy is src after an edit at byte at: the table holding it with its
// pipes aligned, the ordered list holding it (or ending just before it)
// numbered on from its first item. Nothing else changes.
func tidy(src string, at int) string {
	els := Elements(src)
	e := -1
	for i, el := range els {
		if at <= el.End {
			e = i
			break
		}
	}
	if e >= 0 && els[e].Kind == KindTable {
		src = alignTable(src, els, els[e].Group)
		els = Elements(src)
	}
	lists := map[int]bool{}
	for _, k := range []int{e - 1, e} {
		if k >= 0 && k < len(els) && els[k].IsItem() && els[k].Ordered() {
			lists[els[k].List] = true
		}
	}
	for list := range lists {
		src = renumber(src, Elements(src), list)
	}
	return src
}

// renumber numbers an ordered list's items on from its first (a list
// numbered "1." throughout is left as it is), rewriting only the digits.
func renumber(src string, els []Element, list int) string {
	if lazyNumbers(els, list) {
		return src
	}
	var items []Element
	for _, e := range els {
		if e.List == list && e.IsItem() && e.Ordered() {
			items = append(items, e)
		}
	}
	if len(items) == 0 {
		return src
	}
	first := items[0].Number
	for k := len(items) - 1; k >= 0; k-- {
		e := items[k]
		want := first + k
		if e.Number == want {
			continue
		}
		at := e.Start + e.Indent
		end := at
		for end < len(src) && src[end] >= '0' && src[end] <= '9' {
			end++
		}
		src = src[:at] + strconv.Itoa(want) + src[end:]
	}
	return src
}

// cells splits a table line into its cells, trimmed (escaped pipes kept).
func cells(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	if strings.HasSuffix(t, "|") && !strings.HasSuffix(t, `\|`) {
		t = t[:len(t)-1]
	}
	var out []string
	var b strings.Builder
	for i := 0; i < len(t); i++ {
		if t[i] == '\\' && i+1 < len(t) {
			b.WriteByte(t[i])
			b.WriteByte(t[i+1])
			i++
			continue
		}
		if t[i] == '|' {
			out = append(out, strings.TrimSpace(b.String()))
			b.Reset()
			continue
		}
		b.WriteByte(t[i])
	}
	return append(out, strings.TrimSpace(b.String()))
}

// alignTable pads every cell of the table group to its column's width,
// so the pipes line up; the delimiter row keeps its colons.
func alignTable(src string, els []Element, group int) string {
	var rows []Element
	for _, e := range els {
		if e.Kind == KindTable && e.Group == group {
			rows = append(rows, e)
		}
	}
	if len(rows) == 0 {
		return src
	}
	start, end := rows[0].Start, rows[len(rows)-1].End
	lines := strings.Split(src[start:end], "\n")
	indent := strings.Repeat(" ", rows[0].Indent)
	grid := make([][]string, len(lines))
	cols := 0
	for i, l := range lines {
		grid[i] = cells(strings.TrimRight(l, "\r"))
		cols = max(cols, len(grid[i]))
	}
	delim := -1
	if rows[0].Header && len(lines) > 1 {
		delim = 1
	}
	width := make([]int, cols)
	for i, row := range grid {
		for c, cell := range row {
			if i != delim {
				width[c] = max(width[c], ansi.StringWidth(cell))
			}
		}
	}
	for c := range width {
		width[c] = max(3, width[c])
	}
	for i, row := range grid {
		var b strings.Builder
		b.WriteString(indent + "|")
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			if i == delim {
				left, right := strings.HasPrefix(cell, ":"), strings.HasSuffix(cell, ":")
				dash := width[c]
				if left {
					dash--
				}
				if right {
					dash--
				}
				cell = strings.Repeat("-", max(1, dash))
				if left {
					cell = ":" + cell
				}
				if right {
					cell += ":"
				}
			}
			b.WriteString(" " + cell + strings.Repeat(" ", max(0, width[c]-ansi.StringWidth(cell))) + " |")
		}
		cr := ""
		if strings.HasSuffix(lines[i], "\r") {
			cr = "\r"
		}
		lines[i] = b.String() + cr
	}
	return src[:start] + strings.Join(lines, "\n") + src[end:]
}
