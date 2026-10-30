package ochazuke

import (
	"errors"
	"slices"

	"github.com/charmbracelet/x/ansi"
)

// Fitting columns to their content: the explicit form of what the
// automatic layout guesses at.
//
//	z f   list.fit-column: the current column of a cell-mode table as wide
//	      as its widest cell over every row (the ones a filter hides
//	      included) or its title, whichever is wider, never wider than the
//	      table; with a count (N z f) N cells instead, as Vim's N ctrl-w |
//	      sets a window's width. Verb guard table-cells: a row-select table
//	      has no current column, so z f does not answer there.
//	z F   list.fit-columns: every column at its content's width when they
//	      all fit the table's width; when they do not, the widest is
//	      narrowed a cell at a time (ties the rightmost) until they fit or
//	      every column is at its least width (its Min, else
//	      defaultColumnMin or its title's width), so as few columns as can
//	      be are cut and the rest scroll (R5.9). A count is ignored, as in
//	      Vim's ctrl-w =. Verb guard table-fit-changes: any table,
//	      cell-mode or not, where it would change a column's width.
//	z =   list.reset-columns undoes both (every column automatic again);
//	      verb guard table-resets: a table with a width given or pins
//	      chosen.
//
// Both are sticky: they write fixed widths into the table's remembered
// widths (List.SetColumnWidth, TableState.Widths), the ones z > and z <
// give, so moving the cursor, scrolling or filtering never reflows the
// table; only the next z f, z F, z >, z < or z = does. New rows wider than
// a fitted column are cut until the next fit.
//
// The table.fit-follows-cursor setting (default off) makes a cell-mode
// table lay its current column out at its content's width, as Vim's
// 'winwidth' widens the current window: the column under the cursor
// fits as the cursor reaches it, and goes back to its own width when the
// cursor leaves. Nothing is written; a width the user gave the column
// (z f, z F, z >, z <) wins. Off by default because it shifts the columns
// as the cursor moves.

// FitFollowsCursorSetting is the setting that makes a cell-mode table's
// current column fit its content as the cursor moves (default off).
const FitFollowsCursorSetting = "table.fit-follows-cursor"

// contentWidth is the cells column i's content takes: its widest cell
// over every row, or its title when wider; at least 1.
func (l *List) contentWidth(i int) int {
	if i < 0 || i >= len(l.columns) {
		return 0
	}
	widest := l.cellWidths()
	return max(1, ansi.StringWidth(l.columns[i].Title), widest[i])
}

// fitCap is the widest a fitted column may be: the width the table's
// columns share in the last frame (0 before one: no cap).
func (l *List) fitCap(w int) int {
	if l.colWidth > 0 {
		return min(w, l.colWidth)
	}
	return w
}

// fitColumn fixes column i at its content's width (capped at the table's
// width) and remembers it.
func (l *List) fitColumn(i int) {
	l.SetColumnWidth(i, l.fitCap(l.contentWidth(i)))
}

// fitColumns fixes every column at its content's width, the widest
// narrowed first while they do not fit the table's width, and remembers
// the widths.
func (l *List) fitColumns() {
	if len(l.columns) == 0 {
		return
	}
	l.restore()
	l.colWidths = map[int]int{}
	for i, w := range l.fittedWidths() {
		l.colWidths[i] = w
	}
	l.follow()
	l.remember()
}

// fittedWidths is the widths z F gives the columns.
func (l *List) fittedWidths() []int {
	widths := make([]int, len(l.columns))
	for i := range l.columns {
		widths[i] = l.contentWidth(i)
	}
	if l.colWidth > 0 {
		widths = narrowToFit(widths, l.fitFloors(widths), l.colWidth)
	}
	return widths
}

// fitChanges reports whether z F would change a column's width: one is
// laid out wider or narrower than z F makes it (a table whose columns all
// show at their content's width has nothing to fit, so z F does not
// answer there and, on a row-select table that fits, z waits for nothing,
// R5.9).
func (l *List) fitChanges() bool {
	if len(l.columns) == 0 {
		return false
	}
	l.restore()
	for i, w := range l.fittedWidths() {
		if l.drawnWidth(i) != w {
			return true
		}
	}
	return false
}

// resets reports whether z = has something to undo: a width given to a
// column (z >, z <, z f, z F, SetColumnWidth) or pins the user chose.
func (l *List) resets() bool {
	if len(l.columns) == 0 {
		return false
	}
	l.restore()
	return len(l.colWidths) > 0 || l.pinsSet
}

// fitFloors is the least width z F narrows each column to: its least
// width as a flexible column (Min, else defaultColumnMin or its title's
// width), whatever width the application or the user gave it, never more
// than its content's.
func (l *List) fitFloors(content []int) []int {
	cols := slices.Clone(l.columns)
	for i := range cols {
		cols[i].Width, cols[i].Max = 0, 0
	}
	_, least := columnSizes(cols, l.cellWidths())
	floors := make([]int, len(cols))
	for i := range cols {
		floors[i] = min(content[i], max(minColumnWidth, least[i]))
	}
	return floors
}

// narrowToFit narrows the widest of widths (ties the rightmost) one cell
// at a time, never below its floor, until the widths and their gaps fit
// avail or every column is at its floor.
func narrowToFit(widths, floors []int, avail int) []int {
	widths = slices.Clone(widths)
	total := tableGap * max(0, len(widths)-1)
	for _, w := range widths {
		total += w
	}
	for total > avail {
		at := -1
		for i, w := range widths {
			if w > floors[i] && (at < 0 || w >= widths[at]) {
				at = i
			}
		}
		if at < 0 {
			break
		}
		widths[at]--
		total--
	}
	return widths
}

// fitsCursor reports whether the table lays its current column out at its
// content's width (the table.fit-follows-cursor setting on a cell-mode
// table).
func (l *List) fitsCursor() bool {
	if !l.cellMode() || l.r == nil {
		return false
	}
	v, _ := l.r.settingValue(FitFollowsCursorSetting)
	return v == true
}

func registerTableFit(r *Runtime, cells func(Context, Arguments) (*List, error)) {
	// table-fit-changes: z F would change a column's width; table-resets:
	// z = has a width or pins to undo. Both hold on any table, so neither
	// key answers (and z waits for nothing) where it would change nothing.
	r.DefineGuard("table-fit-changes", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.fitChanges()
	})
	r.DefineGuard("table-resets", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.resets()
	})
	guards := r.Nouns["ochazuke.list"].Guards
	guards["fit-column"] = "table-cells"
	guards["fit-columns"] = "table-fit-changes"
	guards["reset-columns"] = "table-resets"
	id := Parameter{Type: "string", Description: "The list (default: the focused one)"}
	r.Register(Operation{Verb: "fit-column", Noun: "ochazuke.list", Description: "Fit a cell-mode table's current column to its widest cell or title over every row, at most the table's width (z f), or give it width cells (a count: N z f); remembered per table, z = undoes it", Parameters: map[string]Parameter{
		"id":    id,
		"width": {Type: "integer", Description: "Cells instead of the content's width (a count: 20 z f; 0 or left out fits the content)"},
	}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		l, err := cells(c, a)
		if err != nil {
			return nil, err
		}
		if !l.cellMode() {
			return nil, errors.New("not a cell table (CellSelect)")
		}
		i := l.Column()
		if w := IntArg(a, "width", 0); w > 0 {
			l.SetColumnWidth(i, l.fitCap(max(minColumnWidth, w)))
		} else {
			l.fitColumn(i)
		}
		return l.state(), nil
	}})
	r.Register(Operation{Verb: "fit-columns", Noun: "ochazuke.list", Description: "Fit every column of a table to its content when the table's width allows, else narrow the widest first so as few columns as can be are cut (z F); remembered per table, z = undoes it", Parameters: map[string]Parameter{"id": id}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		l, err := cells(c, a)
		if err != nil {
			return nil, err
		}
		if len(l.columns) == 0 {
			return nil, errors.New("not a table")
		}
		l.fitColumns()
		return l.state(), nil
	}})
}
