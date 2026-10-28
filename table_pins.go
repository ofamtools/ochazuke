package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Pinned columns: a table's pinned columns draw first, at the left, in
// their declared order, and never scroll (table_columns.go); the rest
// scroll after them. The default pins are the columns declared with
// Column.Pinned, else the first column, so a table that declares none
// draws as before pins existed. The user's pins are the list's own once
// set (List.PinColumn, UnpinColumn, UnpinColumns, the verbs below) and are
// remembered per table id in the state directory with the widths
// (TableState.Pins, cell-mode or not); ResetPins (and z =) puts the
// default back.
//
// Verbs on the list noun (verb guard list-table: any table):
//
//	pin-column {column}         pin a column
//	unpin-column {column}       unpin a column
//	toggle-pin-column {column}  pin or unpin (z p in cell mode)
//	unpin-columns               leave no column pinned
//
// column is a title (any case) or a column's number in declaration order,
// 1 the first. Without it a cell-mode table uses its current column and a
// row-select table opens the column chooser (as g c does on a cell-mode
// table) and acts on the column chosen.

// defaultPins is the columns declared Pinned, else the first one.
func (l *List) defaultPins() []int {
	var pins []int
	for i, c := range l.columns {
		if c.Pinned {
			pins = append(pins, i)
		}
	}
	if len(pins) == 0 && len(l.columns) > 0 {
		pins = []int{0}
	}
	return pins
}

// pinned is the table's pinned columns in declared order: the user's when
// set, else the defaults.
func (l *List) pinned() []int {
	if !l.pinsSet {
		return l.defaultPins()
	}
	var pins []int
	for _, i := range l.pins {
		if i >= 0 && i < len(l.columns) && !slices.Contains(pins, i) {
			pins = append(pins, i)
		}
	}
	slices.Sort(pins)
	return pins
}

// PinnedColumns is the table's pinned columns (indexes into Columns) in
// declared order.
func (l *List) PinnedColumns() []int {
	l.restore()
	return l.pinned()
}

// order is the columns in drawing order: the pinned ones, then the rest.
func (l *List) order() []int {
	pins := l.pinned()
	out := slices.Clone(pins)
	for i := range l.columns {
		if !slices.Contains(pins, i) {
			out = append(out, i)
		}
	}
	return out
}

// position is the current column's place in drawing order.
func (l *List) position() int {
	return max(0, slices.Index(l.order(), l.Column()))
}

// SetPinnedColumns makes these columns (indexes into Columns) the table's
// pins and remembers them; the default pins are remembered as no choice.
func (l *List) SetPinnedColumns(pins []int) {
	l.restore()
	var clean []int
	for _, i := range pins {
		if i >= 0 && i < len(l.columns) && !slices.Contains(clean, i) {
			clean = append(clean, i)
		}
	}
	slices.Sort(clean)
	if slices.Equal(clean, l.defaultPins()) {
		l.pins, l.pinsSet = nil, false
	} else {
		l.pins, l.pinsSet = clean, true
		if l.pins == nil {
			l.pins = []int{}
		}
	}
	l.follow()
	l.remember()
}

// PinColumn pins column i; UnpinColumn unpins it.
func (l *List) PinColumn(i int) { l.SetPinnedColumns(append(l.PinnedColumns(), i)) }

// UnpinColumn unpins column i.
func (l *List) UnpinColumn(i int) {
	l.SetPinnedColumns(slices.DeleteFunc(l.PinnedColumns(), func(p int) bool { return p == i }))
}

// UnpinColumns leaves no column pinned.
func (l *List) UnpinColumns() { l.SetPinnedColumns(nil) }

// ResetPins puts the default pins back (Column.Pinned, else the first).
func (l *List) ResetPins() {
	l.restore()
	l.pins, l.pinsSet = nil, false
	l.follow()
	l.remember()
}

// columnNamed is the column a title (any case) or a number in declaration
// order (1 the first) names.
func (l *List) columnNamed(name string) (int, error) {
	name = strings.TrimSpace(name)
	for i := range l.columns {
		if strings.EqualFold(l.columnTitle(i), name) || strings.EqualFold(strings.TrimSpace(l.columns[i].Title), name) {
			return i, nil
		}
	}
	if n, err := strconv.Atoi(name); err == nil && n >= 1 && n <= len(l.columns) {
		return n - 1, nil
	}
	return 0, fmt.Errorf("no column %q", name)
}

func registerTablePins(r *Runtime, cells func(Context, Arguments) (*List, error)) {
	id := Parameter{Type: "string", Description: "The list (default: the focused one)"}
	column := Parameter{Type: "string", Description: "A column title, or its number in declaration order (1 the first); default the current column (cell mode), else a chooser"}
	// act runs change on the column the request names, else the current
	// column of a cell-mode table, else the column chosen from a chooser.
	act := func(verb string, change func(l *List, i int)) Operation {
		return Operation{Verb: verb, Noun: "ochazuke.list", Parameters: map[string]Parameter{"id": id, "column": column}, Positional: []string{"column"}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
			l, err := cells(c, a)
			if err != nil {
				return nil, err
			}
			if len(l.columns) == 0 {
				return nil, errors.New("not a table")
			}
			if name := StringArg(a, "column", ""); name != "" {
				i, err := l.columnNamed(name)
				if err != nil {
					return nil, err
				}
				change(l, i)
				return l.state(), nil
			}
			if l.cellMode() {
				change(l, l.Column())
				return l.state(), nil
			}
			rows := make([]ChooserRow, len(l.columns))
			pins := l.PinnedColumns()
			for i := range l.columns {
				rows[i] = ChooserRow{ID: strconv.Itoa(i), Label: l.columnTitle(i)}
				if slices.Contains(pins, i) {
					rows[i].Label += " (pinned)"
				}
			}
			_, err = r.OpenChooser(ChooserSpec{Kind: "table-column", Title: "Column", Rows: rows, Choose: func(row ChooserRow) Result {
				i, _ := strconv.Atoi(row.ID)
				change(l, i)
				return Done(l.state())
			}})
			return l.state(), err
		}}
	}
	pin := act("pin-column", func(l *List, i int) { l.PinColumn(i) })
	pin.Description = "Pin a table column: it draws at the left with the other pinned columns and never scrolls; remembered per table"
	unpin := act("unpin-column", func(l *List, i int) { l.UnpinColumn(i) })
	unpin.Description = "Unpin a table column: it scrolls with the rest again; remembered per table"
	toggle := act("toggle-pin-column", func(l *List, i int) {
		if slices.Contains(l.PinnedColumns(), i) {
			l.UnpinColumn(i)
		} else {
			l.PinColumn(i)
		}
	})
	toggle.Description = "Pin a table column, or unpin it when pinned (z p in cell mode: the current column)"
	for _, op := range []Operation{pin, unpin, toggle} {
		r.Register(op)
	}
	r.Register(Operation{Verb: "unpin-columns", Noun: "ochazuke.list", Description: "Leave no column of a table pinned: every column scrolls; z = puts the default pins back", Parameters: map[string]Parameter{"id": id}, Result: "list state", Run: func(_ *Runtime, c Context, a Arguments) (any, error) {
		l, err := cells(c, a)
		if err != nil {
			return nil, err
		}
		l.UnpinColumns()
		return l.state(), nil
	}})
	guards := r.Nouns["ochazuke.list"].Guards
	for _, verb := range []string{"pin-column", "unpin-column", "toggle-pin-column", "unpin-columns"} {
		guards[verb] = "list-table"
	}
}
