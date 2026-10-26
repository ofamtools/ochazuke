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
// Max caps a flexible column's growth (0: up to its widest cell). Wrap
// makes List.View wrap a cell longer than its column onto further lines
// of the row instead of cutting it: the row is as tall as its tallest
// wrapped cell, the other cells sit on its first line (TableText still
// cuts).
type Column struct {
	Title    string `json:"title"`
	Width    int    `json:"width,omitempty"`
	Min      int    `json:"min,omitempty"`
	Max      int    `json:"max,omitempty"`
	Align    Align  `json:"align,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Wrap     bool   `json:"wrap,omitempty"`
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
		if c.Width == 0 && c.Max > 0 {
			natural[i] = min(natural[i], max(c.Max, title))
		}
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

// tableView is View for a list with columns: the header line (bold; none
// when no column has a title), then
// the rows in the list's window, the "x–y / n" line when they overflow;
// the focused list's selected row is reversed across the whole width.
func (l *List) tableView(width, height int) (string, int, int) {
	// Columns without titles (a list of name and value) draw no header.
	header := height > 1 && slices.ContainsFunc(l.columns, func(c Column) bool { return c.Title != "" })
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
		lines = append(lines, l.emptyLine())
		return strings.Join(layoutRows(strings.Join(lines, "\n"), width, height), "\n"), 0, height
	}
	if l.tall() {
		return l.tallView(lines, width, height, body, shown, widths)
	}
	visible := body
	if len(l.items) > body && body > 1 {
		visible--
	}
	visible = max(visible, 0)
	l.offset = windowOffset(l.offset, l.selected, max(1, visible), len(l.items), l.scrolloff())
	end := min(len(l.items), l.offset+visible)
	l.window, l.windowed = visible, len(l.items) > visible
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
	return strings.Join(layoutRows(strings.Join(lines, "\n"), width, height), "\n"), focus, focusHeight
}

// cellWidths is the widest cell per column over every row, measured once
// per item set: a frame draws only the rows in its window, while the
// widths depend on all of them. A row's cells depend only on the item and
// the number of columns, so SetItems drops the cache and a change in the
// column count remeasures.
func (l *List) cellWidths() []int {
	if l.widest == nil || len(l.widest) != len(l.columns) {
		// Every row, the ones a filter hides included, so the columns
		// keep their widths while the filter narrows the rows.
		rows := make([][]string, len(l.all))
		for i, item := range l.all {
			rows[i] = cells(item, len(l.columns))
		}
		l.widest = cellWidths(rows, len(l.columns))
	}
	return l.widest
}

// TableText lays a table out as plain text lines, the header line first,
// with the layout a list with columns draws (List.View) and no styling,
// gutter or highlight: a printer outside the TUI (the command line's
// --format table) reads the same column declarations. At width > 0 the
// columns that do not fit drop by Priority and cells cut with "…" at their
// width; at width <= 0 nothing drops or cuts, every column is as wide as
// its widest cell or title (Width and Min are ignored). A row with fewer
// cells than columns has empty ones; control characters in a cell become
// spaces; trailing spaces are trimmed. No columns is no lines.
func TableText(cols []Column, rows [][]string, width int) []string {
	if len(cols) == 0 {
		return nil
	}
	clean := make([][]string, len(rows))
	for r, row := range rows {
		clean[r] = make([]string, len(cols))
		for i := range min(len(cols), len(row)) {
			clean[r][i] = modalLabel(row[i])
		}
	}
	titles := make([]string, len(cols))
	for i, c := range cols {
		titles[i] = modalLabel(c.Title)
	}
	layout := cols
	if width <= 0 {
		layout = make([]Column, len(cols))
		widest := cellWidths(clean, len(cols))
		width = tableGap * (len(cols) - 1)
		for i, c := range cols {
			layout[i] = Column{Title: titles[i], Align: c.Align}
			width += max(1, ansi.StringWidth(titles[i]), widest[i])
		}
	}
	shown, widths := tableLayout(layout, clean, width)
	lines := make([]string, 0, len(rows)+1)
	lines = append(lines, strings.TrimRight(tableLine(cols, titles, shown, widths), " "))
	for _, row := range clean {
		lines = append(lines, strings.TrimRight(tableLine(cols, row, shown, widths), " "))
	}
	return lines
}

// tall reports whether the table's rows can take more than one line: a
// column wraps, or a row opens a section.
func (l *List) tall() bool {
	for _, c := range l.columns {
		if c.Wrap {
			return true
		}
	}
	for _, item := range l.items {
		if item.Section != "" {
			return true
		}
	}
	return false
}

// wrapCell wraps a cell into lines of at most width cells, breaking at
// spaces and after dots (a noun.verb breaks at its dot).
func wrapCell(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	return strings.Split(ansi.Wrap(text, width, "."), "\n")
}

// tableRowLines lays one row out on as many lines as its tallest wrapped
// cell; a cell of a column without Wrap sits on the first line, cut.
func tableRowLines(cols []Column, row []string, shown, widths []int) []string {
	parts := make([][]string, len(shown))
	n := 1
	for j, i := range shown {
		if cols[i].Wrap {
			parts[j] = wrapCell(row[i], widths[j])
		} else {
			parts[j] = []string{row[i]}
		}
		n = max(n, len(parts[j]))
	}
	lines := make([]string, n)
	cell := make([]string, len(cols))
	for k := range n {
		for j, i := range shown {
			cell[i] = ""
			if k < len(parts[j]) {
				cell[i] = parts[j][k]
			}
		}
		lines[k] = tableLine(cols, cell, shown, widths)
	}
	return lines
}

// tallView is tableView for rows of more than one line (wrapped cells,
// section labels). Each row is its section label when it opens one, then
// its lines. The window starts at the list's offset, never below the
// selected row, and moves down only until the selected row fits whole;
// when it starts inside a section, that section's label is repeated on
// its first line. The focused list's selected row is reversed across the
// whole width on every line, its section label not.
func (l *List) tallView(lines []string, width, height, body int, shown, widths []int) (string, int, int) {
	section := lipgloss.NewStyle().Bold(true).Underline(true)
	gutter := len(l.marks) > 0
	rows := make([][]string, len(l.items))
	opens := make([]bool, len(l.items))
	total := 0
	for i, item := range l.items {
		opens[i] = item.Section != "" && (i == 0 || l.items[i-1].Section != item.Section)
		for k, line := range tableRowLines(l.columns, cells(item, len(l.columns)), shown, widths) {
			if gutter {
				prefix := "  "
				if k == 0 && l.marks[item.ID] {
					prefix = "* "
				}
				line = prefix + line
			}
			rows[i] = append(rows[i], line)
		}
		total += len(rows[i])
		if opens[i] {
			total++
		}
	}
	visible := body
	if len(l.items) > 1 && body > 1 && total > body {
		visible--
	}
	visible = max(visible, 1)
	sticky := func(from int) bool {
		return visible > 1 && l.items[from].Section != "" && !opens[from]
	}
	fits := func(from int) bool {
		n := 0
		if sticky(from) {
			n++
		}
		for i := from; i <= l.selected; i++ {
			n += len(rows[i])
			if opens[i] {
				n++
			}
		}
		return n <= visible
	}
	l.offset = max(0, min(l.offset, l.selected))
	for l.offset < l.selected && !fits(l.offset) {
		l.offset++
	}
	top := len(lines)
	var out []string
	if sticky(l.offset) {
		out = append(out, section.Render(modalLabel(l.items[l.offset].Section)))
	}
	focused := l.Focused()
	focus, focusHeight := 0, height
	end := l.offset
	for i := l.offset; i < len(l.items) && len(out) < visible; i++ {
		if opens[i] {
			out = append(out, section.Render(modalLabel(l.items[i].Section)))
		}
		if i == l.selected && len(out) < visible {
			focus, focusHeight = top+len(out), min(len(rows[i]), visible-len(out))
		}
		for k, line := range rows[i] {
			if len(out) == visible {
				break
			}
			if k == 0 {
				end = i + 1
			}
			line = ansi.Truncate(line, width, "…")
			if i == l.selected && focused {
				line += strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
				line = lipgloss.NewStyle().Reverse(true).Render(line)
			} else {
				line = strings.TrimRight(line, " ")
			}
			out = append(out, line)
		}
	}
	lines = append(lines, out...)
	l.window, l.windowed = max(1, end-l.offset), visible < body
	if visible < body {
		lines = append(lines, fmt.Sprintf("%d–%d / %d", l.offset+1, end, len(l.items)))
	}
	return strings.Join(layoutRows(strings.Join(lines, "\n"), width, height), "\n"), focus, focusHeight
}
