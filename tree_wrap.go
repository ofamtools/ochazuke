package ochazuke

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// treeWrap keeps display lines separate from row identity. Only geometry and
// plain cell text are cached; tones and selection are painted for each frame.
// The line index lets a large outline draw and scroll without rendering every
// off-screen row on every frame.
type treeWrap struct {
	source        []ListItem
	widths, shown []int
	columns       []Column
	spacing       bool
	editID        string
	editText      []rune
	editCursor    int
	rows          []wrappedTreeRow
	starts        []int // first display line of each row, with a terminal total
	top, visible  int
	selected      string
	cursor        *Pos
	active        bool
}

type wrappedTreeRow struct {
	prefix  string
	text    string
	suffix  string
	room    int
	lines   []string
	starts  []int // rune offsets into the editable text
	lead    int   // section and/or space before this row
	other   [][]string
	offsets [][]int
	height  int
}

func (t *Tree) wrapEnabled() bool { return t.r.Settings().Bool("tree.wrap") }

func (t *Tree) wrapping() bool {
	return t.wrapEnabled() && t.wrap.active && len(t.list.items) > 0 && len(t.wrap.source) == len(t.list.items) && &t.wrap.source[0] == &t.list.items[0]
}

// prepareWrap leaves rows that already fit on the existing list path,
// including tables whose other columns explicitly wrap.
func (t *Tree) prepareWrap(cols []Column, shown, widths []int) bool {
	if !t.wrapEnabled() {
		return false
	}
	t.measureWrapped(cols, shown, widths)
	return t.wrap.active
}

func (t *Tree) wrapLayout(width int) ([]Column, []int, []int) {
	l := t.list
	if l.hasGutter() {
		width -= 2
	}
	if len(l.columns) == 0 {
		return []Column{{Width: max(1, width)}}, []int{0}, []int{max(1, width)}
	}
	shown, widths := l.columnLayout(max(1, width))
	titles := make([]string, len(l.columns))
	for i, c := range l.columns {
		titles[i] = modalLabel(l.sortTitle(c))
	}
	cols, shown, widths, _ := l.withEdges(titles, shown, widths)
	return cols, shown, widths
}

// rowText separates display notation from the editable label. Cell zero can
// decorate the label with any prefix: bullets, boxes, numbers and glyphs all
// use the same measured prefix. No notation is interpreted by the tree.
func (t *Tree) rowText(item ListItem) (prefix, text, suffix string) {
	index := t.index(item.ID)
	if index < 0 {
		return "", modalLabel(item.Label), ""
	}
	n := t.nodes[index]
	prefix, text = t.prefix(n), n.Label
	if len(n.Cells) > 0 {
		cell := n.Cells[0]
		if at := strings.Index(cell, n.Label); at >= 0 {
			prefix += cell[:at]
			suffix = cell[at+len(n.Label):]
		} else {
			text = cell
		}
	}
	return modalLabel(prefix), cellLabel(text), modalLabel(suffix)
}

// wrappedText uses the input wrapper, so read and edit views have identical
// word breaks, wide-grapheme widths and long-word behavior. The prefix is
// outside that input's width and repeats as spaces, not as notation.
func wrappedText(text string, width int, cursor int) ([]string, []int) {
	in := Input{Text: []rune(text), Wrap: true, WrapIndent: func(string) int { return 0 }}
	breaks, _ := in.wrapBreaks(max(1, width), cursor)
	at := make([]int, 0, len(breaks)+1)
	at = append(at, 0)
	for i := range breaks {
		at = append(at, i)
	}
	for i, r := range in.Text {
		if r == '\n' {
			at = append(at, i+1)
		}
	}
	sort.Ints(at)
	lines := make([]string, len(at))
	for k, start := range at {
		end := len(in.Text)
		if k+1 < len(at) {
			end = at[k+1]
		}
		lines[k] = strings.TrimRight(string(in.Text[start:end]), " \n")
	}
	return lines, at
}

func (t *Tree) measureWrapped(cols []Column, shown, widths []int) {
	w, l := &t.wrap, t.list
	spaced := l.spacing()
	same := len(w.source) == len(l.items) && len(l.items) > 0 && &w.source[0] == &l.items[0] && slices.Equal(w.shown, shown) && slices.Equal(w.widths, widths) && slices.Equal(w.columns, cols) && w.spacing == spaced
	var editText []rune
	editCursor := -1
	if t.input != nil {
		editText, editCursor = t.input.Text, t.input.Cursor
	}
	if same && w.editID == t.editing && slices.Equal(w.editText, editText) && w.editCursor == editCursor {
		return
	}
	if !same {
		w.source = l.items
		w.shown = append(w.shown[:0], shown...)
		w.widths = append(w.widths[:0], widths...)
		w.columns = append(w.columns[:0], cols...)
		w.spacing = spaced
		w.rows = make([]wrappedTreeRow, len(l.items))
		w.starts = make([]int, len(l.items)+1)
		w.selected = ""
		w.active = false
	}
	for i, item := range l.items {
		if !same || item.ID == t.editing || item.ID == w.editID {
			w.rows[i] = t.measureWrappedRow(i, cols, shown, widths, spaced)
		}
		row := w.rows[i]
		w.starts[i+1] = w.starts[i] + row.lead + row.height
	}
	w.editID, w.editCursor = t.editing, editCursor
	w.editText = append(w.editText[:0], editText...)
	w.active = t.editing != "" || slices.ContainsFunc(w.rows, func(row wrappedTreeRow) bool { return len(row.lines) > 1 })
}

func (t *Tree) measureWrappedRow(i int, cols []Column, shown, widths []int, spaced bool) wrappedTreeRow {
	item := t.list.items[i]
	row := wrappedTreeRow{height: 1}
	if spaced && i > 0 && item.SpaceAbove {
		row.lead++
	}
	if item.Section != "" && (i == 0 || t.list.items[i-1].Section != item.Section) {
		row.lead++
	}
	row.prefix, row.text, row.suffix = t.rowText(item)
	cursor := -1
	if item.ID == t.editing && t.input != nil {
		row.text, cursor = string(t.input.Text), t.input.Cursor
	}
	if col := slices.Index(shown, 0); col >= 0 {
		row.prefix = ansi.Truncate(row.prefix, max(0, widths[col]-2), "")
		row.suffix = ansi.Truncate(row.suffix, max(0, widths[col]-ansi.StringWidth(row.prefix)-2), "")
		row.room = max(1, widths[col]-ansi.StringWidth(row.prefix)-ansi.StringWidth(row.suffix))
		row.lines, row.starts = wrappedText(row.text, row.room, cursor)
		row.height = len(row.lines)
	}
	for j, col := range shown {
		if col == 0 || !cols[col].Wrap && !cols[col].Lines {
			continue
		}
		if row.other == nil {
			row.other = make([][]string, len(cols))
			row.offsets = make([][]int, len(cols))
		}
		cells := t.list.rowCells(t.list.groupedCells(i, 0))
		row.other[col], row.offsets[col] = cellLinesAt(cells[col], widths[j], cols[col].Wrap)
		row.height = max(row.height, len(row.other[col]))
	}
	return row
}

func (w *treeWrap) scrollable() bool {
	return w.visible > 0 && len(w.starts) > 0 && w.starts[len(w.starts)-1] > w.visible
}
func (w *treeWrap) limit() int {
	if len(w.starts) == 0 {
		return 0
	}
	return max(0, w.starts[len(w.starts)-1]-w.visible)
}
func (w *treeWrap) rowAt(line int) int {
	return max(0, min(len(w.rows)-1, sort.Search(len(w.rows), func(i int) bool { return w.starts[i+1] > line })))
}
func (w *treeWrap) span(i int) (int, int) {
	start := w.starts[i] + w.rows[i].lead
	return start, start + w.rows[i].height
}

// followWrapped follows selection changes, not every redraw: an explicit
// line/page scroll inside a tall row must not snap back to its first line.
func (t *Tree) followWrapped() {
	w, l := &t.wrap, t.list
	if l.selected < 0 || l.selected >= len(w.rows) {
		return
	}
	id := l.items[l.selected].ID
	from, to := w.span(l.selected)
	if w.selected != id {
		if to-from > w.visible {
			to = from + 1
		}
		w.top = scrollToShow(w.top, from, to, w.visible, w.starts[len(w.rows)], l.scrolloff())
		w.selected = id
	}
	if t.input != nil && t.editing == id {
		row := w.rows[l.selected]
		at := sort.Search(len(row.starts), func(i int) bool { return row.starts[i] > t.input.Cursor }) - 1
		line := from + max(0, at)
		w.top = scrollToShow(w.top, line, line+1, w.visible, w.starts[len(w.rows)], l.scrolloff())
	}
	w.top = max(0, min(w.top, w.limit()))
}

func (t *Tree) wrappedView(header []string, width, height, body int, cols []Column, shown, widths []int, sp span) (string, int, int) {
	w, l := &t.wrap, t.list
	w.cursor = nil
	// prepareWrap measured this frame before choosing the multiline path.
	w.visible = max(1, body)
	total := w.starts[len(w.rows)]
	if total > body && body > 1 {
		w.visible--
	}
	t.followWrapped()
	top := w.top
	// A blank space above a row is omitted at the start of a window.
	first := w.rowAt(top)
	if top == w.starts[first] && w.rows[first].lead > 0 && l.items[first].SpaceAbove && first > 0 {
		top++
		w.top = top
	}
	out := make([]string, 0, height)
	out = append(out, header...)
	focus, focusHeight := 0, height
	end := first
	for i := first; i < len(w.rows) && len(out)-len(header) < w.visible; i++ {
		row := w.rows[i]
		start, _ := w.span(i)
		for at := max(top, w.starts[i]); at < start && len(out)-len(header) < w.visible; at++ {
			text := ""
			if at == start-1 && l.items[i].Section != "" && (i == 0 || l.items[i-1].Section != l.items[i].Section) {
				text = lipgloss.NewStyle().Bold(true).Underline(true).Render(modalLabel(l.groupHeading(l.items[i].Section)))
			}
			out = append(out, ansi.Truncate(text, width, "…"))
		}
		skip := max(0, top-start)
		if skip >= row.height {
			continue
		}
		count := min(row.height-skip, w.visible-(len(out)-len(header)))
		if count <= 0 {
			break
		}
		at := len(out)
		lines, cursor := t.drawWrapped(i, skip, count, width, cols, shown, widths, sp)
		out = append(out, lines...)
		if i == l.selected {
			focus, focusHeight = at, count
			l.selLine = at
		}
		if cursor != nil {
			w.cursor = &Pos{X: cursor.X, Y: at + cursor.Y}
		}
		end = i + 1
	}
	l.offset = first
	l.window, l.windowed, l.budget = max(1, end-first), total > w.visible, w.visible
	if total > body && body > 1 {
		out = append(out, fmt.Sprintf("%d–%d / %d", first+1, end, len(l.items)))
	}
	return strings.Join(layoutRows(strings.Join(out, "\n"), width, height), "\n"), focus, focusHeight
}

func (t *Tree) drawWrapped(i, skip, count, width int, cols []Column, shown, widths []int, sp span) ([]string, *Pos) {
	w, l := &t.wrap, t.list
	row, item := w.rows[i], l.items[i]
	cells := l.rowCells(l.groupedCells(i, l.offset))
	if len(l.columns) == 0 {
		cells = []string{item.Label}
	}
	toned, faded := l.listTones(), l.faded()
	tones := l.withParts(cols, l.rowTones(item))
	gutter := l.hasGutter()
	mark := 0
	if gutter {
		mark = 2
	}
	selected := i == l.selected && l.marksSelection()
	cursorStyle, bar := l.cursorStyle(sp, toned, faded), l.bar(faded)
	var edit Block
	editing := t.input != nil && t.editing == item.ID && slices.Contains(shown, 0)
	if editing {
		room := row.room
		t.input.Wrap = true
		t.input.WrapIndent = func(string) int { return 0 }
		edit = inputBlockFrom(t.input, room, count, skip)
	}
	editLines := strings.Split(edit.Text, "\n")
	out := make([]string, count)
	var cursor *Pos
	for k := range count {
		lineNo := skip + k
		lineCells := row.wrappedCells(cells, len(cols), lineNo)
		line, at := tableLineAt(cols, lineCells, shown, widths)
		if editing {
			col := slices.Index(shown, 0)
			x := at[col].x + ansi.StringWidth(row.prefix)
			room := row.room
			content := ""
			if k < len(editLines) {
				content = editLines[k]
			}
			content += strings.Repeat(" ", max(0, room-ansi.StringWidth(content)))
			line = ansi.Cut(line, 0, x) + content + ansi.Cut(line, x+room, ansi.StringWidth(line))
			if edit.Cursor != nil && edit.Cursor.Y == k {
				cursor = &Pos{X: mark + x + edit.Cursor.X, Y: k}
			}
		}
		if gutter {
			prefix := "  "
			if lineNo == 0 {
				prefix = l.rowGutter(item.ID)
			}
			line = prefix + line
		}
		lineTones := wrappedRowTones(tones, cells, row, lineNo)
		line = ansi.Truncate(line, width, "…")
		switch segs := rowSegs(toned, lineTones, lineCells, shown, at, mark); {
		case len(segs) > 0 && !editing:
			line = paintLine(line, width, segs, toned, selected, faded, sp)
		case selected && !editing:
			line = highlightRow(line, width, sp, cursorStyle, bar)
		default:
			line = strings.TrimRight(line, " ")
		}
		out[k] = line
	}
	return out, cursor
}
func (row wrappedTreeRow) wrappedCells(cells []string, n, line int) []string {
	out := make([]string, n)
	if line == 0 {
		copy(out, cells)
	}
	prefix := row.prefix
	if line > 0 {
		prefix = strings.Repeat(" ", ansi.StringWidth(prefix))
	}
	if len(row.lines) > 0 {
		out[0] = prefix
		if line < len(row.lines) {
			out[0] += row.lines[line]
		}
		if line == 0 && row.suffix != "" {
			out[0] += strings.Repeat(" ", max(0, ansi.StringWidth(row.prefix)+row.room-ansi.StringWidth(out[0]))) + row.suffix
		}
	}
	for col, parts := range row.other {
		if line < len(parts) {
			out[col] = parts[line]
		}
	}
	return out
}

// wrappedRowTones relocates text spans onto continuation lines; other
// columns retain their explicit Wrap/Lines behavior.
func wrappedRowTones(tones []ToneSpan, cells []string, row wrappedTreeRow, line int) []ToneSpan {
	var out []ToneSpan
	for col, cell := range cells {
		spans := spansOf(tones, col)
		switch {
		case col == 0 && line < len(row.lines):
			start := utf8.RuneCountInString(row.prefix) + row.starts[line]
			n := utf8.RuneCountInString(row.lines[line])
			if line == 0 {
				start = 0
				n += utf8.RuneCountInString(row.prefix)
			}
			spans = lineSpans(spans, utf8.RuneCountInString(cell), start, n)
			if line > 0 {
				spans = shiftTones(spans, utf8.RuneCountInString(row.prefix))
			}
		case col < len(row.other) && line < len(row.other[col]):
			if row.offsets[col] != nil {
				spans = lineSpans(spans, utf8.RuneCountInString(cell), row.offsets[col][line], utf8.RuneCountInString(row.other[col][line]))
			}
		case line > 0:
			continue
		}
		out = append(out, spans...)
	}
	return out
}

// wrapMotion retains row-wise j/k, but reveals a tall row's remaining lines
// before leaving it and caps scrolling at half a viewport (R3.14).
func (t *Tree) wrapMotion(delta int) bool {
	w, l := &t.wrap, t.list
	if w.visible == 0 || len(w.rows) != len(l.items) || l.selected < 0 {
		return false
	}
	from, to := w.span(l.selected)
	if delta > 0 && to > w.top+w.visible {
		w.top = min(w.limit(), w.top+MotionScroll(to-w.top-w.visible, w.visible))
		return true
	}
	if delta < 0 && from < w.top {
		w.top = max(0, w.top+MotionScroll(from-w.top, w.visible))
		return true
	}
	next := min(max(0, l.selected+delta), len(l.items)-1)
	if next == l.selected {
		return false
	}
	from, to = w.span(next)
	want := scrollToShow(w.top, from, min(to, from+w.visible), w.visible, w.starts[len(w.rows)], 0)
	w.top = max(0, min(w.limit(), w.top+MotionScroll(want-w.top, w.visible)))
	if SpanInView(from-w.top, to-1-w.top, w.visible) {
		l.Move(next - l.selected)
		w.selected = l.items[next].ID
	}
	return true
}

func (t *Tree) scrollWrapped(lines int) {
	w, l := &t.wrap, t.list
	if !w.scrollable() || l.selected < 0 {
		return
	}
	w.top = max(0, min(w.limit(), w.top+lines))
	from, to := w.span(l.selected)
	if !SpanInView(from-w.top, to-1-w.top, w.visible) {
		line := max(w.top, min(from, w.top+w.visible-1))
		l.Move(w.rowAt(line) - l.selected)
	}
	w.selected = l.items[l.selected].ID
}
func (t *Tree) pageWrapped(lines int) {
	w, l := &t.wrap, t.list
	if len(w.rows) == 0 || l.selected < 0 {
		return
	}
	from, _ := w.span(l.selected)
	from = max(w.top, min(from, w.top+w.visible-1))
	target := max(0, min(w.starts[len(w.rows)]-1, from+lines))
	l.Move(w.rowAt(target) - l.selected)
	w.top = max(0, min(w.limit(), w.top+lines))
	w.selected = l.items[l.selected].ID
}
