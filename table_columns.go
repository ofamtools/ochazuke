package ochazuke

import "errors"

// Column motions: a table with more columns than its width shows scrolls
// them horizontally, the way a spreadsheet does with its first column
// frozen. The list keeps a column offset (List.ColumnOffset):
//
//   - 0, the default, is the table as it has always been drawn: every
//     column at its least width, the ones that do not fit dropped by
//     Priority, the room left growing the flexible ones.
//   - k > 0 is a scrolled view: the first column stays (it names the row),
//     then columns k, k+1, ... in order, as many as fit at their least
//     widths, the room left growing them; Priority plays no part. When the
//     first column and column k do not fit together, column k is shown
//     alone. The largest offset is the smallest k whose view reaches the
//     last column, so the last column is always reachable and the view
//     never scrolls past it.
//
// Keys, rows of the list noun in normal mode guarded by the verb guard
// table-scrolls (the focused list is a table whose last frame hid a
// column, or is scrolled), so on a table that fits and on a plain list
// they do not answer (and z waits for nothing):
//
//	$          list.scroll-columns {to: last}: the view that shows the last column
//	^ and 0    list.scroll-columns {to: first}: offset 0 (0 only without a
//	           count: after a digit 0 is the count's, as in 10j)
//	z l / z h  list.scroll-columns {delta: ±1}: one column right / left
//	           (Vim's zl/zh); a count is columns
//
// The selection is a row, not a cell, so these move the view only; they
// are motions (off the footer, under movement in ? help). The offset is
// clamped to the width of each frame, so a terminal grown wide enough for
// every column draws offset 0 again. h and l stay free: row nouns bind
// them (open, collapse, cycle a setting).

// ColumnOffset is the table's column offset: 0 unscrolled, else the first
// column shown after the frozen first one.
func (l *List) ColumnOffset() int { return l.colOffset }

// columnLayout is the columns a table draws at width and their widths:
// tableLayoutOf at offset 0, else the scrolled view at the offset, clamped
// to the largest offset this width has. It records the width for the
// column motions.
func (l *List) columnLayout(width int) (shown, widths []int) {
	l.colWidth = width
	l.colOffset = min(max(l.colOffset, 0), l.maxColumnOffset(width))
	if l.colOffset == 0 {
		return tableLayoutOf(l.columns, l.cellWidths(), width)
	}
	return scrolledLayout(l.columns, l.cellWidths(), width, l.colOffset)
}

// scrolledLayout is the view at column offset from (>= 1): the first
// column, then columns from onward that fit in order at their least
// widths (from itself always, alone when the first does not fit beside it).
func scrolledLayout(cols []Column, widest []int, width, from int) (shown, widths []int) {
	natural, least := columnSizes(cols, widest)
	need := 0
	if least[0]+tableGap+least[from] <= width {
		shown, need = []int{0}, least[0]
	}
	for i := from; i < len(cols); i++ {
		add := least[i]
		if len(shown) > 0 {
			add += tableGap
		}
		if i > from && need+add > width {
			break
		}
		shown = append(shown, i)
		need += add
	}
	return shown, growColumns(shown, natural, least, width)
}

// maxColumnOffset is the largest column offset at width: 0 when the
// unscrolled layout shows every column, else the smallest offset whose
// view reaches the last column.
func (l *List) maxColumnOffset(width int) int {
	n := len(l.columns)
	if n < 2 || width <= 0 {
		return 0
	}
	widest := l.cellWidths()
	if shown, _ := tableLayoutOf(l.columns, widest, width); len(shown) == n {
		return 0
	}
	for k := 1; k < n-1; k++ {
		if shown, _ := scrolledLayout(l.columns, widest, width, k); shown[len(shown)-1] == n-1 {
			return k
		}
	}
	return n - 1
}

// columnsScroll reports whether the column motions have anything to do:
// a table drawn at least once whose width hides a column, or one scrolled.
func (l *List) columnsScroll() bool {
	return len(l.columns) > 1 && l.colWidth > 0 && (l.colOffset > 0 || l.maxColumnOffset(l.colWidth) > 0)
}

func registerColumnMotions(r *Runtime) {
	scroll := func(args Arguments) Binding { return Do("list.scroll-columns", args) }
	steps := func(delta int) Binding {
		return scroll(Arguments{"delta": delta}).FromPayload("steps", SourceCount)
	}
	r.Define("list").Mode("normal").
		Bind("$", scroll(Arguments{"to": "last"})).
		Bind("^", scroll(Arguments{"to": "first"})).
		Bind("0", scroll(Arguments{"to": "first"}).Guard("no-count")).
		Bind("z l", steps(1)).
		Bind("z h", steps(-1))
	r.DefineGuard("table-scrolls", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.columnsScroll()
	})
	if r.Nouns["list"].Guards == nil {
		r.Nouns["list"].Guards = map[string]string{}
	}
	r.Nouns["list"].Guards["scroll-columns"] = "table-scrolls"
	r.Register(Operation{Verb: "scroll-columns", Noun: "list", Description: "Scroll a table's columns: to the first or the last, or delta columns right or left; the first column stays", Parameters: map[string]Parameter{
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
		most := l.maxColumnOffset(l.colWidth)
		switch StringArg(a, "to", "") {
		case "first":
			l.colOffset = 0
		case "last":
			l.colOffset = most
		default:
			l.colOffset = min(max(l.colOffset+IntArg(a, "delta", 0)*max(1, IntArg(a, "steps", 1)), 0), most)
		}
		return l.state(), nil
	}})
}
