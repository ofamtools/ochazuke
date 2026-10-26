package ochazuke

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// A table is a List with columns, not a separate widget: focus, selection,
// marks, scrolling, spill and the one highlight are the list's. Columns
// change only how List.View draws the rows: a header line, then each row's
// Cells laid out to computed widths, cut with "…" at the display width,
// the columns that do not fit dropped by Priority, and the selected row of
// the focused list highlighted across the whole width.

// Align is a column's alignment: AlignLeft (the zero value) or AlignRight.
type Align string

// Alignments.
const (
	AlignLeft  Align = ""
	AlignRight Align = "right"
)

// Column is one column of a table. Width fixes the column's cells (0: a
// flexible column, as wide as its widest cell or title when there is
// room); Min is a flexible column's least width before columns drop
// (default: 10 cells or its title's width when wider, at most its widest
// cell or title). Priority orders
// dropping when the width is short: the column with the largest Priority
// drops first, ties the rightmost first; 0, the default, is the most
// important, and the last column left never drops (it is cut instead).
type Column struct {
	Title    string `json:"title"`
	Width    int    `json:"width,omitempty"`
	Min      int    `json:"min,omitempty"`
	Align    Align  `json:"align,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

// tableGap is the space between two columns; defaultColumnMin a flexible
// column's least width without Min (its title's when wider).
const (
	tableGap         = 2
	defaultColumnMin = 10
)

// Columns makes AddList's list a table with these columns (List.SetColumns
// changes them later). A list without columns draws its labels.
func Columns(cols ...Column) InstanceOption {
	return func(s *instanceSpec) { s.columns = slices.Clone(cols) }
}

// SetColumns replaces the list's columns; none makes it a plain list again.
func (l *List) SetColumns(cols ...Column) { l.columns = slices.Clone(cols) }

// Columns returns a copy of the list's columns (nil for a plain list).
func (l *List) Columns() []Column { return slices.Clone(l.columns) }

// cells is a row's text per column: its Cells, else its Label as the first
// cell; control characters become spaces.
func cells(item ListItem, n int) []string {
	src := item.Cells
	if len(src) == 0 {
		src = []string{item.Label}
	}
	out := make([]string, n)
	for i := range min(n, len(src)) {
		out[i] = modalLabel(src[i])
	}
	return out
}

// tableLayout picks the columns that fit width and their widths: every
// column at its least width (Width, else Min) plus the gaps must fit, else
// the column with the largest Priority (ties the rightmost) drops, until
// one is left; the room left grows flexible columns toward their natural
// width (widest cell or title), one cell at a time from the left.
func tableLayout(cols []Column, rows [][]string, width int) (shown, widths []int) {
	return tableLayoutOf(cols, cellWidths(rows, len(cols)), width)
}

// cellWidths is each column's widest cell over the rows.
func cellWidths(rows [][]string, n int) []int {
	out := make([]int, n)
	for _, row := range rows {
		for i := range out {
			out[i] = max(out[i], ansi.StringWidth(row[i]))
		}
	}
	return out
}

// tableLayoutOf is tableLayout given each column's widest cell (cellWidths).
func tableLayoutOf(cols []Column, widest []int, width int) (shown, widths []int) {
	natural := make([]int, len(cols))
	least := make([]int, len(cols))
	for i, c := range cols {
		title := ansi.StringWidth(c.Title)
		natural[i] = max(title, widest[i])
		switch {
		case c.Width > 0:
			natural[i], least[i] = c.Width, c.Width
		case c.Min > 0:
			least[i] = min(c.Min, natural[i])
		default:
			least[i] = min(max(title, defaultColumnMin), natural[i])
		}
		least[i] = max(1, least[i])
		natural[i] = max(1, natural[i])
		shown = append(shown, i)
	}
	need := func() int {
		n := tableGap * (len(shown) - 1)
		for _, i := range shown {
			n += least[i]
		}
		return n
	}
	for len(shown) > 1 && need() > width {
		drop := 0
		for j, i := range shown {
			if cols[i].Priority >= cols[shown[drop]].Priority {
				drop = j
			}
		}
		shown = slices.Delete(shown, drop, drop+1)
	}
	widths = make([]int, len(shown))
	for j, i := range shown {
		widths[j] = least[i]
	}
	if len(shown) == 1 && widths[0] > width {
		widths[0] = max(1, width)
		return shown, widths
	}
	room := width - need()
	for room > 0 {
		grew := false
		for j, i := range shown {
			if room > 0 && widths[j] < natural[i] {
				widths[j]++
				room--
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	return shown, widths
}

// tableLine lays one row's cells out at the widths: each cut with "…" at
// its display width and padded by its alignment, two spaces between.
func tableLine(cols []Column, row []string, shown, widths []int) string {
	parts := make([]string, len(shown))
	for j, i := range shown {
		text := ansi.Truncate(row[i], widths[j], "…")
		pad := strings.Repeat(" ", max(0, widths[j]-ansi.StringWidth(text)))
		if cols[i].Align == AlignRight {
			parts[j] = pad + text
		} else {
			parts[j] = text + pad
		}
	}
	return strings.Join(parts, strings.Repeat(" ", tableGap))
}

// tableView is View for a list with columns: the header line (bold), then
// the rows in the list's window, the "x–y / n" line when they overflow;
// the focused list's selected row is reversed across the whole width.
func (l *List) tableView(width, height int) string {
	header := height > 1
	body := height
	if header {
		body--
	}
	gutter := len(l.marks) > 0
	mark := 0
	if gutter {
		mark = 2
	}
	titles := make([]string, len(l.columns))
	for i, c := range l.columns {
		titles[i] = modalLabel(c.Title)
	}
	shown, widths := tableLayoutOf(l.columns, l.cellWidths(), max(1, width-mark))
	var lines []string
	if header {
		head := strings.Repeat(" ", mark) + tableLine(l.columns, titles, shown, widths)
		lines = append(lines, lipgloss.NewStyle().Bold(true).Render(strings.TrimRight(head, " ")))
	}
	top := len(lines)
	if len(l.items) == 0 {
		lines = append(lines, "(empty list)")
		return l.r.PlacedAt(l.id, strings.Join(layoutRows(strings.Join(lines, "\n"), width, height), "\n"), 0, height)
	}
	visible := body
	if len(l.items) > body && body > 1 {
		visible--
	}
	visible = max(visible, 0)
	l.offset = windowOffset(l.offset, l.selected, max(1, visible), len(l.items), l.scrolloff())
	end := min(len(l.items), l.offset+visible)
	focused := l.Focused()
	for i := l.offset; i < end; i++ {
		line := tableLine(l.columns, cells(l.items[i], len(l.columns)), shown, widths)
		if gutter {
			prefix := "  "
			if l.marks[l.items[i].ID] {
				prefix = "* "
			}
			line = prefix + line
		}
		line = ansi.Truncate(line, width, "…")
		if i == l.selected && focused {
			line += strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
			line = lipgloss.NewStyle().Reverse(true).Render(line)
		} else {
			line = strings.TrimRight(line, " ")
		}
		lines = append(lines, line)
	}
	if len(l.items) > body && body > 1 {
		lines = append(lines, fmt.Sprintf("%d–%d / %d", l.offset+1, end, len(l.items)))
	}
	focus, focusHeight := 0, height
	if l.selected >= l.offset && l.selected < end {
		focus, focusHeight = top+l.selected-l.offset, 1
	}
	return l.r.PlacedAt(l.id, strings.Join(layoutRows(strings.Join(lines, "\n"), width, height), "\n"), focus, focusHeight)
}

// cellWidths is the widest cell per column over every row, measured once
// per item set: a frame draws only the rows in its window, while the
// widths depend on all of them. A row's cells depend only on the item and
// the number of columns, so SetItems drops the cache and a change in the
// column count remeasures.
func (l *List) cellWidths() []int {
	if l.widest == nil || len(l.widest) != len(l.columns) {
		rows := make([][]string, len(l.items))
		for i, item := range l.items {
			rows[i] = cells(item, len(l.columns))
		}
		l.widest = cellWidths(rows, len(l.columns))
	}
	return l.widest
}
