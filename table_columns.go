package ochazuke

import (
	"errors"
	"slices"
	"strconv"
)

// Column motions: a table with more columns than its width shows scrolls
// them horizontally, the way a spreadsheet does with frozen columns.
//
// Pinned columns (table_pins.go) draw first, at the left, in their own
// order, and never scroll; the others (the scrolling columns) follow them
// in order. By default the first column is pinned (it names the row); an
// application declares other defaults with Column.Pinned, and the user
// changes them with list.pin-column, list.unpin-column,
// list.toggle-pin-column (z p in cell mode) and list.unpin-columns.
//
// The list keeps a column offset (List.ColumnOffset):
//
//   - 0, the default, is the table as it has always been drawn: every
//     column at its least width, the ones that do not fit dropped by
//     Priority (scrolling columns down to one before any pin, then pins
//     from the right down to the first), the room left growing the
//     flexible ones.
//   - k > 0 is a scrolled view: the pinned columns, then the scrolling
//     columns from the k-th onward (with the default pins, column k), as
//     many as fit at their least widths, the room left growing them;
//     Priority plays no part. When the pinned columns leave no room for
//     the k-th, pins are left out of the view from the right until it fits
//     (the saved pins do not change), the k-th alone at the narrowest. The
//     largest offset is the smallest k whose view reaches the last
//     scrolling column, so it is always reachable and the view never
//     scrolls past it. An offset of 1 that draws what 0 draws is 0.
//
// Edge markers: a table with a header that hides a column at its width
// reserves cells at both edges of the scrolling columns and draws in its
// header ‹N at the left (N scrolling columns hidden before the view) and
// N› at the right edge (hidden after it), each only when N > 0, so the
// user sees whether h and l will scroll or move on. A table that fits
// reserves nothing and draws none.
//
// Keys, rows of the list noun in normal mode:
//
//	z l / z h  list.scroll-columns {delta: ±1}: one column right / left
//	           (Vim's zl/zh; a count is columns), verb guard table-scrolls
//	           (the focused list is a table whose last frame hid a column,
//	           or is scrolled), so on a table that fits and on a plain list
//	           they do not answer (and z waits for nothing)
//	$          list.scroll-columns {to: last}: the view that shows the last
//	           column (bound in table_cells.go, after the cell rows)
//	^ and 0    list.scroll-columns {to: first}: offset 0 (0 only without a
//	           count: after a digit 0 is the count's, as in 10j)
//	l / h      scroll, then move the focus (table_cells.go): on a
//	           row-select table l scrolls one column right (guard
//	           table-scrolls-right) until the last column shows and h one
//	           left (table-scrolls-left) until offset 0; at that edge they
//	           move the focus to the next / previous stop (guards
//	           table-spills-right / table-spills-left, focus.next /
//	           focus.previous) as j and k spill past the last and first
//	           row; in a modal, on a NoSpill list and at the page's last or
//	           first stop they clamp
//
// On a row-select table (the default) these move the view only; a row
// noun's own h or l (open, collapse, cycle a setting) answers first, as
// the walk is inner-first, so the scroll rows apply only where the row
// noun does not bind the key. On a CellSelect table (table_cells.go) h and
// l move the current column, $, ^ and 0 move it to the ends, the current
// column is carried along when z l/z h scroll it off, and every frame
// scrolls the view to show the current column. All are motions (off the
// footer, under movement in ? help). The offset is clamped to the width of
// each frame, so a terminal grown wide enough for every column draws
// offset 0 again.

// ColumnOffset is the table's column offset: 0 unscrolled, else k for the
// view that starts the scrolling columns at the k-th.
func (l *List) ColumnOffset() int { return l.colOffset }

// colView is a table's columns as one frame lays them out: the columns
// with the widths z > and z < gave, each column's widest cell, the pinned
// columns (in order) and the scrolling ones, and the width the columns
// share (the frame's, less the edge markers' cells).
type colView struct {
	cols   []Column
	widest []int
	pins   []int
	rest   []int
	width  int
	edge   int // cells of one edge marker; 0: no markers
}

// marked reports whether the view draws edge markers.
func (v colView) marked() bool { return v.edge > 0 }

// markerWidth is the cells one edge marker takes for a table whose
// scrolling columns number n: ‹ or › and the count's digits.
func markerWidth(n int) int { return 1 + len(strconv.Itoa(max(n, 1))) }

// view is the table laid out at width (the frame's width less the mark
// gutter): every column, pinned ones first, and, when the table draws a
// header and hides a column at width, the cells left once the edge
// markers are reserved.
func (l *List) view(width int) colView {
	l.restore()
	pins := l.pinned()
	v := colView{cols: l.layoutColumns(), widest: l.cellWidths(), pins: pins, width: width}
	for i := range l.columns {
		if !slices.Contains(pins, i) {
			v.rest = append(v.rest, i)
		}
	}
	if l.colHeader && len(v.rest) > 0 && v.maxOffset() > 0 {
		edge := markerWidth(len(v.rest))
		if reserved := width - 2*(edge+tableGap); reserved >= 2*edge {
			v.width, v.edge = reserved, edge
		}
	}
	return v
}

// columnLayout is the columns a table draws at width and their widths:
// the unscrolled layout at offset 0, else the scrolled view at the
// offset, clamped to the largest offset this width has. It records the
// width for the column motions.
func (l *List) columnLayout(width int) (shown, widths []int) {
	l.colWidth = width
	v := l.view(width)
	most := v.maxOffset()
	l.colOffset = min(max(l.colOffset, 0), most)
	if l.cellMode() {
		l.col = min(max(l.col, 0), len(l.columns)-1)
		l.colOffset = l.offsetShowing(v, l.colOffset, most)
	}
	if l.colOffset == 1 && v.sameAsStart() {
		l.colOffset = 0
	}
	shown, widths = v.at(l.colOffset)
	l.drawn, l.drawnWidths = shown, widths
	l.edges = colEdges{}
	if v.marked() {
		l.edges.w = v.edge
		left, right := v.hidden(l.colOffset)
		if left > 0 {
			l.edges.left = "‹" + strconv.Itoa(left)
		}
		if right > 0 {
			l.edges.right = strconv.Itoa(right) + "›"
		}
	}
	return shown, widths
}

// at is the columns shown at column offset k and their widths.
func (v colView) at(k int) (shown, widths []int) {
	natural, least := columnSizes(v.cols, v.widest)
	need := func(ids []int) int {
		n := tableGap * max(0, len(ids)-1)
		for _, i := range ids {
			n += least[i]
		}
		return n
	}
	if len(v.rest) == 0 || k == 0 {
		return v.start(natural, least, need)
	}
	from := max(1, min(k, len(v.rest))) - 1
	first := v.rest[from]
	pins := slices.Clone(v.pins)
	for len(pins) > 0 && need(pins)+tableGap+least[first] > v.width {
		pins = pins[:len(pins)-1] // the view only: the saved pins stay
	}
	shown = pins
	used := need(pins)
	for j, i := range v.rest[from:] {
		add := least[i]
		if len(shown) > 0 {
			add += tableGap
		}
		if j > 0 && used+add > v.width {
			break
		}
		shown = append(shown, i)
		used += add
	}
	return shown, growColumns(shown, natural, least, v.width)
}

// start is the unscrolled layout: pinned columns then the scrolling ones.
// While they do not fit, scrolling columns drop by Priority (the largest
// first, ties the rightmost) down to one, then pins drop from the right
// down to the first (in the view only), then the last scrolling column,
// so a pinned first column too wide to share the row is shown alone, as
// before pins existed, and the scrolled views show the rest.
func (v colView) start(natural, least []int, need func([]int) int) (shown, widths []int) {
	pins, rest := slices.Clone(v.pins), slices.Clone(v.rest)
	all := func() []int { return append(slices.Clone(pins), rest...) }
	for len(rest) > 1 && need(all()) > v.width {
		drop := 0
		for j, i := range rest {
			if v.cols[i].Priority >= v.cols[rest[drop]].Priority {
				drop = j
			}
		}
		rest = slices.Delete(rest, drop, drop+1)
	}
	for len(pins) > 1 && need(all()) > v.width {
		pins = pins[:len(pins)-1]
	}
	if len(pins) > 0 && len(rest) > 0 && need(all()) > v.width {
		rest = nil
	}
	shown = all()
	return shown, growColumns(shown, natural, least, v.width)
}

// sameAsStart reports whether offset 1 draws the columns offset 0 does, so
// a scroll from the start skips it.
func (v colView) sameAsStart() bool {
	a, _ := v.at(0)
	b, _ := v.at(1)
	return slices.Equal(a, b)
}

// maxOffset is the largest column offset: 0 when the unscrolled layout
// shows every column, else the smallest offset whose view reaches the
// last scrolling column.
func (v colView) maxOffset() int {
	n := len(v.rest)
	if n == 0 || len(v.cols) < 2 || v.width <= 0 {
		return 0
	}
	if shown, _ := v.at(0); len(shown) == len(v.cols) {
		return 0
	}
	for k := 1; k < n; k++ {
		if shown, _ := v.at(k); shown[len(shown)-1] == v.rest[n-1] {
			return k
		}
	}
	return n
}

// hidden is how many scrolling columns the view at offset k leaves out
// before (left) and after (right) the ones it shows; at offset 0 every
// hidden one is after (the view starts at the first).
func (v colView) hidden(k int) (left, right int) {
	shown, _ := v.at(k)
	if k > 0 {
		left = min(k, len(v.rest)) - 1
	}
	count := 0
	for _, i := range v.rest {
		if slices.Contains(shown, i) {
			count++
		}
	}
	return left, max(0, len(v.rest)-left-count)
}

// maxColumnOffset is the largest column offset at width (the frame's,
// less the gutter).
func (l *List) maxColumnOffset(width int) int {
	if len(l.columns) < 2 || width <= 0 {
		return 0
	}
	return l.view(width).maxOffset()
}

// columnsScroll reports whether the column motions have anything to do:
// a table drawn at least once whose width hides a column, or one scrolled.
func (l *List) columnsScroll() bool {
	return len(l.columns) > 1 && l.colWidth > 0 && (l.colOffset > 0 || l.maxColumnOffset(l.colWidth) > 0)
}

// scrollsRight reports whether l on a row-select table scrolls: the view
// does not show the last column yet.
func (l *List) scrollsRight() bool {
	return !l.cellMode() && l.columnsScroll() && l.colOffset < l.maxColumnOffset(l.colWidth)
}

// scrollsLeft reports whether h on a row-select table scrolls: the view
// is scrolled.
func (l *List) scrollsLeft() bool {
	return !l.cellMode() && len(l.columns) > 1 && l.colWidth > 0 && l.colOffset > 0
}

// atColumnEdge reports whether the table stands at its edge in direction
// step: a cell-mode table's current column is the first (step < 0) or
// last drawn in order; a row-select table's view is at offset 0 or shows
// its last column (one that fits is at both).
func (l *List) atColumnEdge(step int) bool {
	if len(l.columns) == 0 {
		return false
	}
	if l.cellMode() {
		order, pos := l.order(), l.position()
		return step < 0 && pos == 0 || step > 0 && pos == len(order)-1
	}
	if step < 0 {
		return !l.scrollsLeft()
	}
	return !l.scrollsRight()
}

// scrollColumns moves the view delta columns (to "first"/"last" ends),
// clamped; from the start a scroll right skips an offset 1 that draws
// what 0 draws.
func (l *List) scrollColumns(to string, delta int) {
	v := l.view(l.colWidth)
	most := v.maxOffset()
	switch to {
	case "first":
		l.colOffset = 0
	case "last":
		l.colOffset = most
	default:
		cur := l.colOffset
		same := v.sameAsStart()
		if cur == 0 && delta > 0 && same {
			cur = 1
		}
		next := min(max(cur+delta, 0), most)
		if next == 1 && same {
			next = 0
		}
		l.colOffset = next
	}
	l.carryColumn()
}

func registerColumnMotions(r *Runtime) {
	steps := func(delta int) Binding {
		return Do("ochazuke.list.scroll-columns", Arguments{"delta": delta}).FromPayload("steps", SourceCount)
	}
	r.Define("ochazuke.list").Mode("normal").
		Bind("z l", steps(1)).
		Bind("z h", steps(-1))
	r.DefineGuard("table-scrolls", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.columnsScroll()
	})
	r.DefineGuard("table-scrolls-right", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.scrollsRight()
	})
	r.DefineGuard("table-scrolls-left", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.scrollsLeft()
	})
	spills := func(step int) Guard {
		return func(r *Runtime, ev KeyEvent) bool {
			l := r.focusedList(ev.Focus)
			if l == nil || !l.atColumnEdge(step) || l.noSpill || l.visual || !l.Focused() {
				return false
			}
			_, ok := l.neighbour(step)
			return ok
		}
	}
	r.DefineGuard("table-spills-right", spills(1))
	r.DefineGuard("table-spills-left", spills(-1))
	if r.Nouns["ochazuke.list"].Guards == nil {
		r.Nouns["ochazuke.list"].Guards = map[string]string{}
	}
	r.Nouns["ochazuke.list"].Guards["scroll-columns"] = "table-scrolls"
	r.Register(Operation{Verb: "scroll-columns", Noun: "ochazuke.list", Description: "Scroll a table's columns: to the first or the last, or delta columns right or left; the pinned columns stay", Parameters: map[string]Parameter{
		"id":    {Type: "string", Description: "The list (default: the focused one)"},
		"to":    {Type: "string", Enum: []string{"first", "last"}, Description: "The unscrolled view (first) or the one showing the last column (last)"},
		"delta": {Type: "integer", Description: "Columns to scroll, positive right (zl), negative left (zh)"},
		"steps": {Type: "integer", Description: "Repeat count (3zl)"},
	}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		var l *List
		if id := StringArg(a, "id", ""); id != "" {
			l = r.List(id)
		} else if c.InputID == "" {
			for _, id := range r.Ancestry(focusOf(c)) {
				if l = r.List(id); l != nil {
					break
				}
			}
		}
		if l == nil {
			return nil, errors.New("no focused list")
		}
		l.scrollColumns(StringArg(a, "to", ""), IntArg(a, "delta", 0)*max(1, IntArg(a, "steps", 1)))
		return l.state(), nil
	}})
}

// carryColumn keeps a cell-mode table's current column on screen after the
// view scrolled (z l, z h): a column the scroll took off is replaced by
// the shown scrolling column nearest it in drawing order, a pinned one
// only when no scrolling column is shown.
func (l *List) carryColumn() {
	if !l.cellMode() || l.colWidth <= 0 {
		return
	}
	v := l.view(l.colWidth)
	shown, _ := v.at(l.colOffset)
	// A pinned column does not hold the current column through a scroll:
	// the frame would scroll back to the start to show it (0).
	pinned := slices.Contains(v.pins, l.col) && l.colOffset > 0
	if len(shown) == 0 || slices.Contains(shown, l.col) && !pinned {
		return
	}
	candidates := shown
	if l.colOffset > 0 {
		if scrolling := slices.DeleteFunc(slices.Clone(shown), func(i int) bool { return slices.Contains(v.pins, i) }); len(scrolling) > 0 {
			candidates = scrolling
		}
	}
	order := l.order()
	at := slices.Index(order, l.col)
	best := candidates[0]
	for _, i := range candidates {
		if abs(slices.Index(order, i)-at) < abs(slices.Index(order, best)-at) {
			best = i
		}
	}
	l.col = best
	l.remember()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
