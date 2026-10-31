package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// A table is a List with columns, not a separate widget: focus, selection,
// marks, scrolling, spill and the one highlight are the list's. Columns
// change only how List.View draws the rows: a header line, then each row's
// Cells laid out to computed widths, cut with "…" at the display width,
// the columns that do not fit dropped by Priority, and the selected row of
// the focused list highlighted across the whole width, its current cell
// (cell mode, table_cells.go) in the list.cell.cursor tone (setting
// list.cell-cursor; underline draws it underlined inside the bar) and the
// current column's title underlined in the header.

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
// cuts); a newline in its cell is a line break. Lines makes List.View
// break a cell at its newlines and cut each line at the column's width
// ("…"), so a cell of one item per line keeps an item per line (the
// help's arguments); with Wrap too, a long line wraps instead. Group makes List.View draw a cell blank when it repeats the cell
// above it in the same section (and every Group column left of it does
// too), so a run of rows sharing a value (the help's key prefix) shows it
// once; the first row a frame draws always shows its cells, and the rows'
// Cells keep the value (TableText does not blank). Part marks a column
// that is not data (PartRowNumber, a row-number column; any word an
// application picks): its cells draw in the tone "list.<part>" and, where
// a highlight crosses them, "list.<part>.intersect" (ToneSpan.Layer;
// setting list.part-tones, default on).
type Column struct {
	Title    string `json:"title"`
	Width    int    `json:"width,omitempty"`
	Min      int    `json:"min,omitempty"`
	Max      int    `json:"max,omitempty"`
	Align    Align  `json:"align,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Wrap     bool   `json:"wrap,omitempty"`
	Lines    bool   `json:"lines,omitempty"`
	Pinned   bool   `json:"pinned,omitempty"`
	Group    bool   `json:"group,omitempty"`
	Part     string `json:"part,omitempty"`
}

// PartRowNumber is the Column.Part of a row-number column: tone
// list.rownum, list.rownum.intersect where a highlight crosses it.
const PartRowNumber = "rownum"

// partTone is the tone a part draws in: "list.<part>".
func partTone(part string) string { return "list." + part }

// partHeader is the header row's part (tone list.header).
const partHeader = "header"

// SetHeaderTones gives the header row's cells tones, as a row's Tones
// give its cells: Cell is the column. A highlight layer
// (ToneSpan.Layer) shows on the header of the column it covers, where it
// crosses the header part (list.header.intersect).
func (l *List) SetHeaderTones(tones ...ToneSpan) { l.headerTones = slices.Clone(tones) }

// HeaderTones is the header row's tones, a copy.
func (l *List) HeaderTones() []ToneSpan { return slices.Clone(l.headerTones) }

// withParts is a row's tones under its part columns' (Column.Part) part
// spans, first so the row's own tones draw over them; tones unchanged
// with list.part-tones off or no part column.
func (l *List) withParts(cols []Column, tones []ToneSpan) []ToneSpan {
	var parts []ToneSpan
	for i, c := range cols {
		if c.Part != "" {
			t := partTone(c.Part)
			parts = append(parts, ToneSpan{Cell: i, Tone: t, Layer: c.Part, part: t})
		}
	}
	if len(parts) == 0 || !l.r.settingOn(PartTonesSetting) {
		return tones
	}
	return append(parts, tones...)
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
// cell; control characters become spaces, a newline in a cell excepted: it
// is a line break in a Wrap or Lines column (tableRowLinesAt) and a space in
// any other (tableLineAt).
func cells(item ListItem, n int) []string {
	src := item.Cells
	if len(src) == 0 {
		src = []string{item.Label}
	}
	out := make([]string, n)
	for i := range min(n, len(src)) {
		out[i] = cellLabel(src[i])
	}
	return out
}

// cellLabel is modalLabel keeping newlines.
func cellLabel(text string) string {
	if !strings.Contains(text, "\n") {
		return modalLabel(text)
	}
	parts := strings.Split(text, "\n")
	for i, part := range parts {
		parts[i] = modalLabel(part)
	}
	return strings.Join(parts, "\n")
}

// cellTextWidth is a cell's display width: its widest line.
func cellTextWidth(text string) int {
	w := 0
	for line := range strings.SplitSeq(text, "\n") {
		w = max(w, ansi.StringWidth(line))
	}
	return w
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
			out[i] = max(out[i], cellTextWidth(row[i]))
		}
	}
	return out
}

// columnSizes is each column's natural width (its widest cell or title,
// Width when fixed, capped by Max) and its least width before it drops
// (Width, else Min, else defaultColumnMin or its title's width).
func columnSizes(cols []Column, widest []int) (natural, least []int) {
	natural = make([]int, len(cols))
	least = make([]int, len(cols))
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
	}
	return natural, least
}

// tableLayoutOf is tableLayout given each column's widest cell (cellWidths).
func tableLayoutOf(cols []Column, widest []int, width int) (shown, widths []int) {
	natural, least := columnSizes(cols, widest)
	for i := range cols {
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
	return shown, growColumns(shown, natural, least, width)
}

// growColumns gives the shown columns their least widths, then the room
// left grows the flexible ones toward their natural width, one cell at a
// time from the left; a lone column wider than width is cut to it.
func growColumns(shown, natural, least []int, width int) []int {
	widths := make([]int, len(shown))
	need := tableGap * (len(shown) - 1)
	for j, i := range shown {
		widths[j] = least[i]
		need += least[i]
	}
	if len(shown) == 1 && widths[0] > width {
		widths[0] = max(1, width)
		return widths
	}
	room := width - need
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
	return widths
}

// tableLine lays one row's cells out at the widths: each cut with "…" at
// its display width and padded by its alignment, two spaces between.
func tableLine(cols []Column, row []string, shown, widths []int) string {
	line, _ := tableLineAt(cols, row, shown, widths)
	return line
}

// cellAt is where tableLineAt put a shown cell: its text starts at cell x
// of the line and shows w cells (its "…" included when cut); its column's
// slot starts at sx and is sw cells wide.
type cellAt struct{ x, w, sx, sw int }

// tableLineAt is tableLine with where each shown cell's text sits, for
// the tones (tone.go).
func tableLineAt(cols []Column, row []string, shown, widths []int) (string, []cellAt) {
	parts := make([]string, len(shown))
	at := make([]cellAt, len(shown))
	x := 0
	for j, i := range shown {
		text := ansi.Truncate(strings.ReplaceAll(row[i], "\n", " "), widths[j], "…")
		w := ansi.StringWidth(text)
		pad := strings.Repeat(" ", max(0, widths[j]-w))
		at[j] = cellAt{x: x, w: w, sx: x, sw: widths[j]}
		if cols[i].Align == AlignRight {
			parts[j] = pad + text
			at[j].x += len(pad)
		} else {
			parts[j] = text + pad
		}
		x += widths[j] + tableGap
	}
	return strings.Join(parts, strings.Repeat(" ", tableGap)), at
}

// rowSegs places a table row's tones on its line: each shown cell's spans
// at the cell's place, after the mark gutter.
func rowSegs(toned *Styles, tones []ToneSpan, row []string, shown []int, at []cellAt, gutter int) []toneSeg {
	if toned == nil {
		return nil
	}
	var out []toneSeg
	for j, i := range shown {
		if i >= len(row) {
			continue
		}
		slot := cellAt{sx: gutter + at[j].sx, sw: at[j].sw}
		out = append(out, slotSegs(spansOf(tones, i), row[i], gutter+at[j].x, at[j].w, slot)...)
	}
	return out
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
	gutter := l.hasGutter()
	mark := 0
	if gutter {
		mark = 2
	}
	titles := make([]string, len(l.columns))
	for i, c := range l.columns {
		titles[i] = modalLabel(c.Title)
	}
	l.colHeader = header
	shown, widths := l.columnLayout(max(1, width-mark))
	cols, shown, widths, right := l.withEdges(titles, shown, widths)
	span := l.currentSpan(shown, widths, mark)
	var lines []string
	if header {
		heads := titles
		if l.edges.w > 0 {
			heads = append(slices.Clip(titles), l.edges.left)
		}
		head := strings.Repeat(" ", mark) + tableLine(cols, heads, shown, widths)
		if right != "" {
			head = ansi.Truncate(head, width-l.edges.w, "") + strings.Repeat(" ", max(0, width-l.edges.w-ansi.StringWidth(head)))
			head += strings.Repeat(" ", max(0, l.edges.w-ansi.StringWidth(right))) + right
		}
		lines = append(lines, l.headerLine(head, width, heads, cols, shown, widths, mark, span))
	}
	top := len(lines)
	if len(l.items) == 0 {
		lines = append(lines, l.emptyLine())
		return strings.Join(layoutRows(strings.Join(lines, "\n"), width, height), "\n"), 0, height
	}
	if l.tall() {
		return l.tallView(lines, width, height, body, cols, shown, widths, span)
	}
	visible := body
	if len(l.items) > body && body > 1 {
		visible--
	}
	visible = max(visible, 0)
	l.offset = windowOffset(l.offset, l.selected, max(1, visible), len(l.items), l.scrolloff())
	end := min(len(l.items), l.offset+visible)
	l.window, l.windowed = visible, len(l.items) > visible
	marked := l.marksSelection()
	toned, faded := l.listTones(), l.faded()
	cursor := l.cursorStyle(span, toned, faded)
	for i := l.offset; i < end; i++ {
		row := l.rowCells(l.groupedCells(i, l.offset))
		line, at := tableLineAt(cols, row, shown, widths)
		if gutter {
			prefix := l.rowGutter(l.items[i].ID)
			line = prefix + line
		}
		line = ansi.Truncate(line, width, "…")
		selected := i == l.selected && marked
		switch segs := rowSegs(toned, l.withParts(cols, l.rowTones(l.items[i])), row, shown, at, mark); {
		case len(segs) > 0:
			line = paintLine(line, width, segs, toned, selected, faded, span)
		case selected:
			line = highlightRow(line, width, span, cursor)
		default:
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

// headerLine draws the header row: in the header part's tone across the
// width (list.part-tones on, list.header) with the header tones
// (SetHeaderTones) over their columns and the current column underlined;
// bold, the current column bold and underlined, without the Model's
// styles or with list.part-tones off.
func (l *List) headerLine(head string, width int, heads []string, cols []Column, shown, widths []int, mark int, cur span) string {
	toned := l.listTones()
	if toned == nil || !l.r.settingOn(PartTonesSetting) {
		bold := lipgloss.NewStyle().Bold(true)
		return styleSpan(strings.TrimRight(head, " "), cur, bold, "1;4")
	}
	segs := []toneSeg{{x: 0, w: width, tone: ToneHeader, layer: partHeader, part: ToneHeader}}
	if len(l.headerTones) > 0 {
		_, at := tableLineAt(cols, heads, shown, widths)
		segs = append(segs, rowSegs(toned, l.headerTones, heads, shown, at, mark)...)
	}
	if cur.w > 0 {
		segs = append(segs, toneSeg{x: cur.x, w: cur.w, underline: true})
	}
	return paintLine(ansi.Truncate(head, width, ""), width, segs, toned, false, l.faded(), span{})
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

// hasGroup reports whether a column is Group.
func (l *List) hasGroup() bool {
	return slices.ContainsFunc(l.columns, func(c Column) bool { return c.Group })
}

// groupedCells is item i's cells with each Group cell blank when it and
// every Group cell left of it repeat the row above's, in the same
// section; the row at top (the first drawn) keeps its cells.
func (l *List) groupedCells(i, top int) []string {
	row := cells(l.items[i], len(l.columns))
	if i <= top || !l.hasGroup() || l.items[i-1].Section != l.items[i].Section {
		return row
	}
	above := cells(l.items[i-1], len(l.columns))
	for c, col := range l.columns {
		if !col.Group {
			continue
		}
		if row[c] == "" || row[c] != above[c] {
			break
		}
		row[c] = ""
	}
	return row
}

// tall reports whether the table's rows can take more than one line: a
// column wraps or breaks at newlines (Lines), or a row opens a section.
func (l *List) tall() bool {
	for _, c := range l.columns {
		if c.Wrap || c.Lines {
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
// its newlines, at spaces and after dots (a noun.verb breaks at its dot).
func wrapCell(text string, width int) []string {
	lines, _ := wrapCellAt(text, width)
	return lines
}

// wrapCellAt is wrapCell with the rune offset in text where each line
// starts, so a tone span keeps its place on the lines it falls on; starts
// is nil when a line cannot be found in text (the wrap changed it).
func wrapCellAt(text string, width int) (lines []string, starts []int) {
	return cellLinesAt(text, width, true)
}

// cellLinesAt is a cell's lines: broken at its newlines, and each line
// wrapped at width when wrap is set (else left to be cut where it is
// drawn); starts as wrapCellAt.
func cellLinesAt(text string, width int, wrap bool) (lines []string, starts []int) {
	if width <= 0 {
		return []string{""}, nil
	}
	placed := true
	offset := 0 // rune offset of the hard line in text
	for hard := range strings.SplitSeq(text, "\n") {
		at := 0 // byte offset in hard past the last line placed
		soft := hard
		if wrap {
			soft = ansi.Wrap(hard, width, ".")
		}
		for line := range strings.SplitSeq(soft, "\n") {
			lines = append(lines, line)
			i := strings.Index(hard[at:], line)
			if i < 0 {
				placed = false
				continue
			}
			starts = append(starts, offset+utf8.RuneCountInString(hard[:at+i]))
			at += i + len(line)
		}
		offset += utf8.RuneCountInString(hard) + 1
	}
	if !placed {
		return lines, nil
	}
	return lines, starts
}

// lineSpans are the spans of a cell that fall on one of its wrapped lines,
// the line starting at rune start and n runes long, as offsets into it.
func lineSpans(spans []ToneSpan, cellRunes, start, n int) []ToneSpan {
	var out []ToneSpan
	for _, sp := range spans {
		from, to := max(sp.From, 0), sp.To
		if to <= 0 || to > cellRunes {
			to = cellRunes
		}
		from, to = max(from, start), min(to, start+n)
		if from >= to {
			continue
		}
		sp.From, sp.To = from-start, to-start
		out = append(out, sp)
	}
	return out
}

// tableRowLines lays one row out on as many lines as its tallest wrapped
// cell; a cell of a column without Wrap or Lines sits on the first line, cut.
func tableRowLines(cols []Column, row []string, shown, widths []int) []string {
	lines, _ := tableRowLinesAt(nil, cols, nil, row, shown, widths)
	return lines
}

// tableRowLinesAt is tableRowLines with each line's tones (none without
// toned): a cell on one line keeps its spans; a cell wrapped onto several
// (at its width or its newlines) keeps each span on the lines it falls
// on; when the wrap cannot be placed back in the text, it keeps only the
// tones of the whole cell, on every line of it.
func tableRowLinesAt(toned *Styles, cols []Column, tones []ToneSpan, row []string, shown, widths []int) ([]string, [][]toneSeg) {
	parts := make([][]string, len(shown))
	starts := make([][]int, len(shown))
	n := 1
	for j, i := range shown {
		if cols[i].Wrap || cols[i].Lines {
			parts[j], starts[j] = cellLinesAt(row[i], widths[j], cols[i].Wrap)
		} else {
			parts[j] = []string{row[i]}
		}
		n = max(n, len(parts[j]))
	}
	lines := make([]string, n)
	segs := make([][]toneSeg, n)
	cell := make([]string, len(cols))
	for k := range n {
		for j, i := range shown {
			cell[i] = ""
			if k < len(parts[j]) {
				cell[i] = parts[j][k]
			}
		}
		var at []cellAt
		lines[k], at = tableLineAt(cols, cell, shown, widths)
		if toned == nil {
			continue
		}
		for j, i := range shown {
			if i >= len(row) {
				continue
			}
			spans := spansOf(tones, i)
			if cell[i] == "" {
				// A blank line of the cell keeps only its slot's layers and
				// part, so a highlighted column reads unbroken.
				spans = slices.DeleteFunc(spans, func(t ToneSpan) bool { return t.Layer == "" && t.part == "" })
			}
			var placed []ToneSpan
			if len(parts[j]) > 1 && starts[j] != nil && k < len(parts[j]) {
				// A text span keeps its place on the lines it falls on;
				// layers and parts stay the slot's, as below.
				text := slices.DeleteFunc(slices.Clone(spans), func(t ToneSpan) bool { return t.Layer != "" || t.part != "" })
				placed = lineSpans(text, utf8.RuneCountInString(row[i]), starts[j][k], utf8.RuneCountInString(cell[i]))
				spans = slices.DeleteFunc(spans, func(t ToneSpan) bool { return t.Layer == "" && t.part == "" })
			}
			if len(parts[j]) > 1 {
				spans = slices.DeleteFunc(spans, func(t ToneSpan) bool {
					return t.From > 0 || t.To > 0 && t.To < len([]rune(row[i]))
				})
				for x := range spans {
					spans[x].From, spans[x].To = 0, 0
				}
			}
			spans = append(spans, placed...)
			segs[k] = append(segs[k], slotSegs(spans, cell[i], at[j].x, at[j].w, at[j])...)
		}
	}
	return lines, segs
}

// tallView is tableView for rows of more than one line (wrapped cells,
// section labels). Each row is its section label when it opens one, then
// its lines. The window starts at the list's offset, never below the
// selected row, and moves down only until the selected row fits whole;
// when it starts inside a section, that section's label is repeated on
// its first line. The focused list's selected row is reversed across the
// whole width on every line, its section label not.
func (l *List) tallView(lines []string, width, height, body int, cols []Column, shown, widths []int, span span) (string, int, int) {
	section := lipgloss.NewStyle().Bold(true).Underline(true)
	gutter := l.hasGutter()
	rows := make([][]string, len(l.items))
	toneLines := make([][][]toneSeg, len(l.items))
	opens := make([]bool, len(l.items))
	toned, faded := l.listTones(), l.faded()
	cursor := l.cursorStyle(span, toned, faded)
	// layRow is item i's lines and tones, its Group cells blanked against
	// the row above unless i is the first row drawn (top).
	layRow := func(i, top int) ([]string, [][]toneSeg) {
		item := l.items[i]
		var out []string
		lines, segs := tableRowLinesAt(toned, cols, l.withParts(cols, l.rowTones(item)), l.rowCells(l.groupedCells(i, top)), shown, widths)
		for k, line := range lines {
			if gutter {
				prefix := "  "
				if k == 0 {
					prefix = l.rowGutter(item.ID)
				}
				line = prefix + line
				for x := range segs[k] {
					segs[k][x].x += 2
				}
			}
			out = append(out, line)
		}
		return out, segs
	}
	total := 0
	for i, item := range l.items {
		opens[i] = item.Section != "" && (i == 0 || l.items[i-1].Section != item.Section)
		rows[i], toneLines[i] = layRow(i, 0)
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
	if l.offset > 0 && l.hasGroup() {
		// The first row drawn shows its Group cells.
		rows[l.offset], toneLines[l.offset] = layRow(l.offset, l.offset)
	}
	top := len(lines)
	var out []string
	if sticky(l.offset) {
		out = append(out, section.Render(modalLabel(l.items[l.offset].Section)))
	}
	marked := l.marksSelection()
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
			selected := i == l.selected && marked
			switch segs := toneLines[i][k]; {
			case len(segs) > 0:
				line = paintLine(line, width, segs, toned, selected, faded, span)
			case selected:
				line = highlightRow(line, width, span, cursor)
			default:
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

// span is the cells of a line a table's current column takes: from x, w
// cells; w 0 for none (a table without a current column, or one not shown).
type span struct{ x, w int }

// currentSpan is where a cell-mode table's current column sits in its
// lines, after a gutter of mark cells.
func (l *List) currentSpan(shown, widths []int, mark int) span {
	if !l.cellMode() {
		return span{}
	}
	x, w, ok := cellSpan(shown, widths, l.col, mark)
	if !ok {
		return span{}
	}
	return span{x, w}
}

// highlightRow pads a plain line to width and reverses it, the cells of
// the current column (sp), the current cell, in cursor (list.cell.cursor,
// out of the bar) or, when cursor is nil, reversed and underlined.
func highlightRow(line string, width int, sp span, cursor ansi.Style) string {
	line += strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	reverse := lipgloss.NewStyle().Reverse(true)
	mark := "4;7"
	if len(cursor) > 0 {
		mark = strings.Join(cursor, ";")
	}
	return styleSpan(line, sp, reverse, mark)
}

// cursorStyle is the current cell's style for a frame (Runtime.cellCursor),
// given to the row tones' styles too so paintLine draws it; nil without a
// current cell or with list.cell-cursor underline.
func (l *List) cursorStyle(sp span, toned *Styles, faded bool) ansi.Style {
	if sp.w <= 0 {
		return nil
	}
	cursor := l.r.cellCursor(faded)
	if toned != nil {
		toned.cellCursor = cursor
	}
	return cursor
}

// styleSpan draws a plain line in base with the cells of sp in the SGR
// attributes mark, written as one run: Lip Gloss draws the spaces of an
// underlined text underlined alone, which would punch holes in a reversed
// row's current cell.
func styleSpan(line string, sp span, base lipgloss.Style, mark string) string {
	n := ansi.StringWidth(line)
	if sp.w <= 0 || sp.x >= n {
		return base.Render(line)
	}
	end := min(n, sp.x+sp.w)
	out := "\x1b[" + mark + "m" + ansi.Cut(line, sp.x, end) + "\x1b[m"
	if sp.x > 0 {
		out = base.Render(ansi.Cut(line, 0, sp.x)) + out
	}
	if end < n {
		out += base.Render(ansi.Cut(line, end, n))
	}
	return out
}

// colEdges is what a frame draws at a table's edges (table_columns.go):
// the cells one marker takes (0: no markers) and the two markers' text.
type colEdges struct {
	w           int
	left, right string
}

// withEdges adds the left edge marker's column to a frame's layout: a
// column of the marker's width after the pinned columns shown (index
// len(l.columns), blank in the rows, the left marker in the header). It
// returns the columns to draw with, the layout with the marker's column,
// and the right marker's text (drawn at the header's right edge).
func (l *List) withEdges(titles []string, shown, widths []int) ([]Column, []int, []int, string) {
	if l.edges.w == 0 {
		return l.columns, shown, widths, ""
	}
	pins := l.pinned()
	at := 0
	for at < len(shown) && slices.Contains(pins, shown[at]) {
		at++
	}
	cols := append(slices.Clone(l.columns), Column{})
	shown = slices.Insert(slices.Clone(shown), at, len(l.columns))
	widths = slices.Insert(slices.Clone(widths), at, l.edges.w)
	return cols, shown, widths, l.edges.right
}

// rowCells is a row's cells with the left edge marker's column after
// them, blank, when the frame draws markers.
func (l *List) rowCells(row []string) []string {
	if l.edges.w == 0 {
		return row
	}
	return append(slices.Clip(row), "")
}
