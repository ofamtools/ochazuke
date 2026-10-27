package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Cell mode: a table (a List with columns) has a current column as well as
// a selected row, the way a spreadsheet (o-csv, on grid) has a current
// cell. The row keeps the list's one highlight; the current cell is drawn
// underlined inside it and the header underlines the current column's
// title. Keys, rows of the list noun in normal mode that answer only on a
// cell-mode table (guard table-cells; the verbs' guards say the same):
//
//	h l left right   list.column {delta: ±1}: one column (a count is columns)
//	0 ^ home         list.column {to: first} (0 only without a count)
//	$ end            list.column {to: last}
//	|                list.column {index}: the column of the count (1 without one)
//	g c              list.choose-column: a chooser of the column titles
//	y / Y            list.yank {what: cell|row}: the cell's text, or the row's
//	                 cells joined by tabs, into the registers
//	z l / z h        list.scroll-columns: the view one column right / left,
//	                 the current column carried along when it scrolls off
//
// j/k keep the column. The column offset (table_columns.go) follows the
// current column: each frame scrolls the view as little as shows it, so a
// column dropped by Priority or past the right edge comes into view as the
// current column reaches it. The movement rows are motions (off the
// footer, under movement in ? help). A row noun binding one of these keys
// answers first (the walk is inner-first), so its table keeps the app's key.
//
// A table added with RowSelect() (a tree's list: h and l expand and
// collapse there, the help modal, the settings page) has no current
// column: it keeps row selection, and $, ^, 0 and z l/z h scroll its
// columns as before.

// RowSelect makes a table select rows only: no current column, no cell
// keys (a tree's list, whose rows answer h and l).
func RowSelect() InstanceOption { return func(s *instanceSpec) { s.rowSelect = true } }

// cellMode reports whether the list is a table with a current column.
func (l *List) cellMode() bool { return len(l.columns) > 0 && !l.rowSelect }

// Column is a cell-mode table's current column (an index into Columns);
// 0 for any other list.
func (l *List) Column() int {
	if !l.cellMode() {
		return 0
	}
	return min(max(l.col, 0), len(l.columns)-1)
}

// SetColumn makes column i (clamped) the current column of a cell-mode
// table; on any other list it does nothing.
func (l *List) SetColumn(i int) {
	if !l.cellMode() {
		return
	}
	l.col = min(max(i, 0), len(l.columns)-1)
	l.follow()
}

// layoutColumns is the columns the table lays out.
func (l *List) layoutColumns() []Column { return l.columns }

// follow lays the columns out again at the last frame's width, so the
// offset already shows the current column before the next frame.
func (l *List) follow() {
	if l.colWidth > 0 {
		l.columnLayout(l.colWidth)
	}
}

// offsetShowing is the column offset nearest cur whose view shows the
// current column: 0 for the first column (the start of the row, as 0 and
// ^ bring it); cur when it shows the column; when the column lies left of
// the view, 0 if that view shows it, else the offset that puts it first
// after the frozen one; else the smallest larger offset that reaches it.
func (l *List) offsetShowing(cols []Column, width, cur, most int) int {
	widest := l.cellWidths()
	showsAt := func(k int) bool {
		shown, _ := layoutAt(cols, widest, width, k)
		return slices.Contains(shown, l.col)
	}
	if l.col == 0 {
		return 0
	}
	if showsAt(cur) {
		return cur
	}
	if showsAt(0) && l.col < cur {
		return 0
	}
	if cur > 0 && l.col < cur {
		return min(l.col, most)
	}
	for k := max(cur+1, 1); k <= most; k++ {
		if showsAt(k) {
			return k
		}
	}
	return cur
}

// layoutAt is the columns shown at column offset k and their widths.
func layoutAt(cols []Column, widest []int, width, k int) (shown, widths []int) {
	if k == 0 {
		return tableLayoutOf(cols, widest, width)
	}
	return scrolledLayout(cols, widest, width, k)
}

// cellSpan is where column col sits in a laid-out line: its first cell and
// width, after a gutter of mark cells; ok is false when it is not shown.
func cellSpan(shown, widths []int, col, mark int) (x, w int, ok bool) {
	x = mark
	for j, i := range shown {
		if i == col {
			return x, widths[j], true
		}
		x += widths[j] + tableGap
	}
	return 0, 0, false
}

// cellText is the text of a row's cell in column i: its Cells entry, else
// its Label for the first column.
func cellText(item ListItem, i int) string {
	switch {
	case i < len(item.Cells):
		return item.Cells[i]
	case len(item.Cells) == 0 && i == 0:
		return item.Label
	}
	return ""
}

// rowText is a row's cells joined by tabs (its Label without cells).
func rowText(item ListItem, n int) string {
	if len(item.Cells) == 0 {
		return item.Label
	}
	parts := make([]string, n)
	for i := range n {
		parts[i] = cellText(item, i)
	}
	return strings.Join(parts, "\t")
}

// columnTitle names column i for the chooser and the status line.
func (l *List) columnTitle(i int) string {
	if t := strings.TrimSpace(modalLabel(l.columns[i].Title)); t != "" {
		return t
	}
	return "column " + strconv.Itoa(i+1)
}

func registerTableCells(r *Runtime) {
	cells := func(c Context, args Arguments) (*List, error) {
		if id := StringArg(args, "id", ""); id != "" {
			if l := r.List(id); l != nil {
				return l, nil
			}
			return nil, fmt.Errorf("unknown list %q", id)
		}
		if c.InputID == "" {
			for _, id := range r.Ancestry(focusOf(c)) {
				if l := r.List(id); l != nil {
					return l, nil
				}
			}
		}
		return nil, errors.New("no focused list")
	}
	r.DefineGuard("table-cells", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.cellMode()
	})
	// list-first-column: the focused list has no current column (a plain
	// list, a row-select table) or is on its first: a host's "back" row on
	// h stands aside while h still moves the column (cli's groups rail).
	r.DefineGuard("list-first-column", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l == nil || l.Column() == 0
	})
	// table-columns: a cell-mode table, or a row-select table whose columns
	// scroll: $, ^ and 0 move the column on the one and scroll on the other.
	r.DefineGuard("table-columns", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && (l.cellMode() || l.columnsScroll())
	})
	column := func(args Arguments) Binding { return Do("list.column", args) }
	steps := func(verb string, delta int) Binding {
		return Do(verb, Arguments{"delta": delta}).FromPayload("steps", SourceCount)
	}
	r.Define("list").Mode("normal").
		Bind("h", steps("list.column", -1).Guard("table-cells")).
		Bind("left", steps("list.column", -1).Guard("table-cells")).
		Bind("l", steps("list.column", 1).Guard("table-cells")).
		Bind("right", steps("list.column", 1).Guard("table-cells")).
		Bind("home", column(Arguments{"to": "first"}).Guard("table-cells")).
		Bind("end", column(Arguments{"to": "last"}).Guard("table-cells")).
		Bind("$", column(Arguments{"to": "last"})).
		Bind("^", column(Arguments{"to": "first"})).
		Bind("0", column(Arguments{"to": "first"}).Guard("no-count")).
		Bind("|", Do("list.column").FromPayload("index", SourceCount).Guard("table-cells")).
		Bind("g c", Do("list.choose-column")).
		Bind("y", Do("list.yank", Arguments{"what": "cell"}).Hide()).
		Bind("Y", Do("list.yank", Arguments{"what": "row"}).Hide())
	guards := r.Nouns["list"].Guards
	guards["column"] = "table-columns"
	for _, verb := range []string{"choose-column", "yank"} {
		guards[verb] = "table-cells"
	}
	id := Parameter{Type: "string", Description: "The list (default: the focused one)"}
	r.Register(Operation{Verb: "column", Noun: "list", Description: "Move a table's current column: to the first or last, to the column numbered index (|), or delta columns; the view scrolls to show it (a table that selects rows only scrolls its columns instead)", Parameters: map[string]Parameter{
		"id":    id,
		"to":    {Type: "string", Enum: []string{"first", "last"}, Description: "The first or the last column"},
		"index": {Type: "integer", Description: "The column number, 1 the first (a count: 3|)"},
		"delta": {Type: "integer", Description: "Columns to move, positive right (l), negative left (h)"},
		"steps": {Type: "integer", Description: "Repeat count (3l)"},
	}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		l, err := cells(c, a)
		if err != nil {
			return nil, err
		}
		to := StringArg(a, "to", "")
		if !l.cellMode() {
			// A row-select table: $, ^ and 0 scroll its columns as they did.
			switch most := l.maxColumnOffset(l.colWidth); to {
			case "first":
				l.colOffset = 0
			case "last":
				l.colOffset = most
			}
			return l.state(), nil
		}
		n := l.Column()
		switch {
		case a["index"] != nil:
			n = IntArg(a, "index", 1) - 1
		case to == "first":
			n = 0
		case to == "last":
			n = len(l.columns) - 1
		default:
			n += IntArg(a, "delta", 0) * max(1, IntArg(a, "steps", 1))
		}
		l.SetColumn(n)
		return l.state(), nil
	}})
	r.Register(Operation{Verb: "choose-column", Noun: "list", Description: "Choose a table's current column from a chooser of its titles", Parameters: map[string]Parameter{"id": id}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		l, err := cells(c, a)
		if err != nil {
			return nil, err
		}
		rows := make([]ChooserRow, len(l.columns))
		for i := range l.columns {
			rows[i] = ChooserRow{ID: strconv.Itoa(i), Label: l.columnTitle(i)}
		}
		_, err = r.OpenChooser(ChooserSpec{Kind: "table-column", Title: "Column", Rows: rows, Choose: func(row ChooserRow) Result {
			i, _ := strconv.Atoi(row.ID)
			l.SetColumn(i)
			return Done(l.state())
		}})
		return l.state(), err
	}})
	r.Register(Operation{Verb: "yank", Noun: "list", Description: "Copy the current cell's text (what: cell) or the row's cells joined by tabs (what: row) into the registers", Parameters: map[string]Parameter{
		"id":       id,
		"what":     {Type: "string", Enum: []string{"cell", "row"}, Description: "The cell (default) or the whole row"},
		"register": {Type: "string", Description: "Register name; the active register, else the unnamed one"},
	}, Result: "the yank", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		l, err := cells(c, a)
		if err != nil {
			return nil, err
		}
		item, ok := l.Selected()
		if !ok {
			return nil, errors.New("no row to copy")
		}
		what, text := StringArg(a, "what", "cell"), ""
		if what == "row" {
			text = rowText(item, len(l.columns))
		} else {
			text = cellText(item, l.Column())
		}
		y := Yank{Text: text}
		if err := r.storeYank(y, StringArg(a, "register", ""), false); err != nil {
			return nil, err
		}
		subject := "the row"
		if what != "row" {
			subject = l.columnTitle(l.Column())
		}
		r.UI().Status(fmt.Sprintf("copied %s (%d characters)", subject, len([]rune(text))))
		return y, nil
	}})
}
