package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Cell mode: a table (a List with columns) added with CellSelect() has a
// current column as well as a selected row, the way a spreadsheet (o-csv,
// on grid) has a current cell; every other table selects rows (opt-in, so
// a table whose rows answer h or l keeps them without a guard). The row keeps the list's one highlight; the current cell is drawn
// underlined inside it and the header underlines the current column's
// title. Keys, rows of the list noun in normal mode that answer only on a
// cell-mode table (verb guard table-cells), except where noted:
//
//	h l left right   list.column {delta: ±1}: one column (a count is columns)
//	0 ^ home         list.column {to: first} (0 only without a count)
//	$ end            list.column {to: last}
//	|                list.column {index}: the column of the count (1 without one)
//	g c              list.choose-column: a chooser of the column titles
//	y                list.yank {what: cell}: the cell's text into the registers
//	Y                list.yank {what: row}: the row's cells joined by tabs,
//	                 on every table (verb guard list-table)
//	z > / z <        list.resize-column {delta: ±1}: widen or narrow the
//	                 current column by ColumnWidthStep cells (a count is steps)
//	z =              list.reset-columns: every column automatic again
//	z l / z h        list.scroll-columns: the view one column right / left,
//	                 the current column carried along when it scrolls off
//	                 (every table that scrolls, table_columns.go)
//
// The width keys pick the current column, so a row-select table has none:
// widths there are the application's (SetColumnWidth), not remembered.
// j/k keep the column. The column offset (table_columns.go) follows the
// current column: each frame scrolls the view as little as shows it, so a
// column dropped by Priority or past the right edge comes into view as the
// current column reaches it. The movement rows are motions (off the
// footer, under movement in ? help). A row noun binding one of these keys
// answers first (the walk is inner-first), so its table keeps the app's key.
//
// A table without CellSelect (the default: a tree's list, where h and l
// expand and collapse, the help modal, the settings page, a cli catalog
// table) has no current column: it keeps row selection, h and l are free
// for its row nouns, and $, ^ and 0 (list.scroll-columns, bound after the
// cell rows so a cell table's column move answers first) and z l/z h
// scroll its columns.
//
// A cell-mode table's current column and the widths given with z > and
// z < are remembered per table id in the state directory (TablesFile,
// Runtime.TableState).

// ColumnWidthStep is the cells z > and z < add to or take from a column.
const ColumnWidthStep = 2

// minColumnWidth is the narrowest z < makes a column (grid and o-csv's
// least cell width too).
const minColumnWidth = 3

// CellSelect makes a table (AddList with Columns) cell-based: a current
// column beside the selected row, moved with h/l, 0/$, N| and g c, and
// the cell keys y and z >/z </z =. For a table one reads cell by cell (a
// JSON or CSV view). AddTree ignores it: a tree's rows answer h and l.
func CellSelect() InstanceOption { return func(s *instanceSpec) { s.cellSelect = true } }

// RowSelect is a no-op kept for code written against v0.12.0, where every
// table was cell-based unless added with it.
//
// Deprecated: tables select rows unless added with CellSelect; drop it.
func RowSelect() InstanceOption { return func(*instanceSpec) {} }

// cellMode reports whether the list is a table with a current column.
func (l *List) cellMode() bool { return len(l.columns) > 0 && l.cellSelect }

// Column is a cell-mode table's current column (an index into Columns);
// 0 for any other list.
func (l *List) Column() int {
	if !l.cellMode() {
		return 0
	}
	l.restore()
	return min(max(l.col, 0), len(l.columns)-1)
}

// SetColumn makes column i (clamped) the current column of a cell-mode
// table and remembers it; on any other list it does nothing.
func (l *List) SetColumn(i int) {
	if !l.cellMode() {
		return
	}
	l.restore()
	l.col = min(max(i, 0), len(l.columns)-1)
	l.follow()
	l.remember()
}

// ColumnWidths is the widths given to columns with z > and z < (column
// index to cells); a column left out is laid out automatically.
func (l *List) ColumnWidths() map[int]int {
	l.restore()
	return maps.Clone(l.colWidths)
}

// SetColumnWidth fixes column i at width cells (0 or less: automatic
// again) and remembers it.
func (l *List) SetColumnWidth(i, width int) {
	if i < 0 || i >= len(l.columns) {
		return
	}
	l.restore()
	if width <= 0 {
		delete(l.colWidths, i)
	} else {
		if l.colWidths == nil {
			l.colWidths = map[int]int{}
		}
		l.colWidths[i] = width
	}
	l.follow()
	l.remember()
}

// ResetColumnWidths makes every column automatic again and remembers it.
func (l *List) ResetColumnWidths() {
	l.restore()
	l.colWidths = nil
	l.follow()
	l.remember()
}

// restore reads the table's remembered view once.
func (l *List) restore() {
	if l.restored {
		return
	}
	l.restored = true
	if !l.cellMode() {
		return // a row-select table remembers nothing (one cell-based in v0.12.0 may have left widths)
	}
	s := l.r.TableState(l.id)
	l.col, l.colWidths = s.Column, s.Widths
}

// remember writes a cell-mode table's view (Runtime.SetTableState).
func (l *List) remember() {
	if !l.cellMode() {
		return
	}
	l.r.SetTableState(l.id, TableState{Column: l.col, Widths: l.colWidths})
}

// layoutColumns is the columns with the widths given by z > and z < as
// fixed widths.
func (l *List) layoutColumns() []Column {
	if len(l.colWidths) == 0 {
		return l.columns
	}
	cols := slices.Clone(l.columns)
	for i, w := range l.colWidths {
		if i >= 0 && i < len(cols) && w > 0 {
			cols[i].Width, cols[i].Max = w, 0
		}
	}
	return cols
}

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

// drawnWidth is the width the current column was drawn at in the last
// frame, else its natural width.
func (l *List) drawnWidth(i int) int {
	if w, ok := l.colWidths[i]; ok {
		return w
	}
	if j := slices.Index(l.drawn, i); j >= 0 && j < len(l.drawnWidths) {
		return l.drawnWidths[j]
	}
	natural, _ := columnSizes(l.layoutColumns(), l.cellWidths())
	return natural[i]
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
	// cellTable is cells for the verbs that need a current column.
	cellTable := func(c Context, args Arguments) (*List, error) {
		l, err := cells(c, args)
		if err == nil && !l.cellMode() {
			err = errors.New("not a cell table (CellSelect)")
		}
		return l, err
	}
	r.DefineGuard("table-cells", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && l.cellMode()
	})
	// list-first-column: the focused list has no current column (a plain
	// list, a row-select table) or is on its first: a host's "back" row on
	// h stands aside while h still moves the column (cli's groups rail).
	// table-cells is the cell rows' verb guard; list-table Y's (any table).
	r.DefineGuard("list-first-column", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l == nil || l.Column() == 0
	})
	r.DefineGuard("list-table", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return l != nil && len(l.columns) > 0
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
		Bind("$", column(Arguments{"to": "last"}).Guard("table-cells")).
		Bind("^", column(Arguments{"to": "first"}).Guard("table-cells")).
		Bind("0", column(Arguments{"to": "first"}).Guard("no-count")).
		Bind("|", Do("list.column").FromPayload("index", SourceCount).Guard("table-cells")).
		Bind("g c", Do("list.choose-column")).
		Bind("y", Do("list.yank", Arguments{"what": "cell"}).Guard("table-cells").Hide()).
		Bind("Y", Do("list.yank", Arguments{"what": "row"}).Hide()).
		Bind("z >", steps("list.resize-column", 1).Hide()).
		Bind("z <", steps("list.resize-column", -1).Hide()).
		Bind("z =", Do("list.reset-columns").Hide())
	// A row-select table's $, ^ and 0 scroll its columns (verb guard
	// table-scrolls), after the cell rows above: on a cell table those
	// answer, here the list.column rows are guarded off. 0's row needs a
	// guard of its own (row-table-no-count): a row with the same keys and
	// guard would replace the cell row.
	r.DefineGuard("row-table-no-count", func(r *Runtime, ev KeyEvent) bool {
		l := r.focusedList(ev.Focus)
		return (l == nil || !l.cellMode()) && r.guard("no-count")(r, ev)
	})
	scroll := func(args Arguments) Binding { return Do("list.scroll-columns", args) }
	r.Define("list").Mode("normal").
		Bind("$", scroll(Arguments{"to": "last"})).
		Bind("^", scroll(Arguments{"to": "first"})).
		Bind("0", scroll(Arguments{"to": "first"}).Guard("row-table-no-count"))
	guards := r.Nouns["list"].Guards
	for _, verb := range []string{"column", "choose-column", "resize-column", "reset-columns"} {
		guards[verb] = "table-cells"
	}
	guards["yank"] = "list-table"
	id := Parameter{Type: "string", Description: "The list (default: the focused one)"}
	r.Register(Operation{Verb: "column", Noun: "list", Description: "Move a cell-mode table's current column: to the first or last, to the column numbered index (|), or delta columns; the view scrolls to show it", Parameters: map[string]Parameter{
		"id":    id,
		"to":    {Type: "string", Enum: []string{"first", "last"}, Description: "The first or the last column"},
		"index": {Type: "integer", Description: "The column number, 1 the first (a count: 3|)"},
		"delta": {Type: "integer", Description: "Columns to move, positive right (l), negative left (h)"},
		"steps": {Type: "integer", Description: "Repeat count (3l)"},
	}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		l, err := cellTable(c, a)
		if err != nil {
			return nil, err
		}
		to, n := StringArg(a, "to", ""), l.Column()
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
		l, err := cellTable(c, a)
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
	r.Register(Operation{Verb: "yank", Noun: "list", Description: "Copy the current cell's text (what: cell, a cell-mode table) or the row's cells joined by tabs (what: row, any table) into the registers", Parameters: map[string]Parameter{
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
		if what != "row" && !l.cellMode() {
			return nil, errors.New("not a cell table (CellSelect): only the row copies")
		}
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
	r.Register(Operation{Verb: "resize-column", Noun: "list", Description: "Widen (delta 1) or narrow (-1) a table's current column by ColumnWidthStep cells; remembered per table", Parameters: map[string]Parameter{
		"id":    id,
		"delta": {Type: "integer", Required: true, Description: "1 wider, -1 narrower"},
		"steps": {Type: "integer", Description: "Repeat count (3z>)"},
	}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		l, err := cellTable(c, a)
		if err != nil {
			return nil, err
		}
		i := l.Column()
		width := l.drawnWidth(i) + IntArg(a, "delta", 0)*ColumnWidthStep*max(1, IntArg(a, "steps", 1))
		if l.colWidth > 0 {
			width = min(width, l.colWidth)
		}
		l.SetColumnWidth(i, max(minColumnWidth, width))
		return l.state(), nil
	}})
	r.Register(Operation{Verb: "reset-columns", Noun: "list", Description: "Lay every column of a table out automatically again (z =)", Parameters: map[string]Parameter{"id": id}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		l, err := cells(c, a)
		if err != nil {
			return nil, err
		}
		l.ResetColumnWidths()
		return l.state(), nil
	}})
}
