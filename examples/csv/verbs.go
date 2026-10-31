package main

import (
	"errors"
	"fmt"
	"sort"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/grid"
)

// registerVerbs declares the nouns of the sketch and their verbs. Keys are
// bound separately (bindings.go); a verb without a key is reached through
// the palette. Every verb builds one Patch and hands it to Apply.
func (a *App) registerVerbs() {
	r := a.r
	verb := func(fn func(c ochazuke.Context, args ochazuke.Arguments) (any, error)) ochazuke.Handler {
		return func(_ *ochazuke.Runtime, c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			return fn(c, args)
		}
	}
	params := func(key string, p map[string]ochazuke.Parameter) {
		key = r.CanonicalKey(key) // selection.transform is the grid's ochazuke.selection.transform
		op := r.Operations[key]
		op.Parameters = p
		r.Operations[key] = op
	}
	name := map[string]ochazuke.Parameter{"name": {Type: "string", Description: "Omit to be asked in a prompt"}}
	path := map[string]ochazuke.Parameter{"path": {Type: "string", Description: "Omit to be asked in a prompt (ctrl+o there opens the picker)"}}

	r.Define("sheet").
		Verb("open", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			if p := ochazuke.StringArg(args, "path", ""); p != "" {
				return a.state(), a.Load(p)
			}
			return nil, a.prompt("Open file", a.Path, "path", func(text string) error { return a.Load(text) })
		}), "Open a CSV file, replacing the sheet").
		Verb("save", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			if a.Path == "" {
				return nil, a.prompt("Save as", "", "path", a.Save)
			}
			return a.state(), a.Save(a.Path)
		}), "Write the sheet to its path").
		Verb("saveAs", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			if p := ochazuke.StringArg(args, "path", ""); p != "" {
				return a.state(), a.Save(p)
			}
			return nil, a.prompt("Save as", a.Path, "path", a.Save)
		}), "Write the sheet to another path and keep editing it there").
		Verb("addColumn", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			add := func(name string) error {
				a.Apply(Patch{Columns: []ColumnChange{{Index: len(a.Header), After: &Column{Name: name, Values: make([]string, len(a.Rows))}}}})
				return nil
			}
			if n := ochazuke.StringArg(args, "name", ""); n != "" {
				return a.state(), add(n)
			}
			return nil, a.prompt("New column name", "", "", add)
		}), "Append a column").
		Verb("sort", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			return a.state(), a.sortBy(a.grid.At(c).Col)
		}), "Sort the rows by the column under the cursor").
		Verb("undo", verb(func(ochazuke.Context, ochazuke.Arguments) (any, error) { return a.state(), a.UndoPatch() }), "Revert the last sheet edit").
		Verb("redo", verb(func(ochazuke.Context, ochazuke.Arguments) (any, error) { return a.state(), a.RedoPatch() }), "Replay the last undone sheet edit")
	params("sheet.open", path)
	params("sheet.saveAs", path)
	params("sheet.addColumn", name)

	// cells resolves the cells a verb acts on: the selection when one is
	// active, else the focused cell. A visual verb ends the selection.
	cells := func(c ochazuke.Context) []grid.Ref {
		if sel := a.grid.Selected(); len(sel) > 0 {
			return sel
		}
		if at := a.grid.At(c); at.Row >= 0 {
			return []grid.Ref{at}
		}
		return nil
	}
	yank := verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
		set := cells(c)
		if len(set) == 0 {
			return nil, errors.New("nothing to yank")
		}
		y := a.yankOf(set, a.grid.Shape())
		a.grid.Cancel()
		return y, r.StoreYank(y, ochazuke.StringArg(args, "register", ""), false)
	})
	paste := verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
		y, err := r.ReadYank(ochazuke.StringArg(args, "register", ""))
		if err != nil {
			return nil, err
		}
		at := a.grid.At(c)
		if set := a.grid.Selected(); len(set) > 0 {
			at = set[0]
		}
		a.grid.Cancel()
		if at.Row < 0 && y.Shape != "line" && y.Shape != "column" {
			return nil, errors.New("nothing to paste onto")
		}
		a.Apply(a.pastePatch(y, at))
		return a.state(), nil
	})
	clear := verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
		var p Patch
		for _, ref := range cells(c) {
			if before := a.Cell(ref); before != "" {
				p.Cells = append(p.Cells, CellChange{At: ref, Before: before})
			}
		}
		a.grid.Cancel()
		a.Apply(p)
		return a.state(), nil
	})
	transform := verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
		p, err := a.Transform(ochazuke.StringArg(args, "name", ""), cells(c))
		if err != nil {
			return nil, err
		}
		a.grid.Cancel()
		a.Apply(p)
		return a.state(), nil
	})
	register := map[string]ochazuke.Parameter{"register": {Type: "string", Description: "Register name; the active register, else the unnamed one"}}
	transformParams := map[string]ochazuke.Parameter{"name": {Type: "string", Required: true, Source: "transformations.list", Description: "A transformation; transformations.list names them"}}

	r.Define("cell").
		Verb("edit", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			at := a.grid.At(c)
			if at.Row < 0 {
				return nil, errors.New("no cell to edit")
			}
			return nil, a.prompt("Edit "+at.ID(), a.Cell(at), "", func(text string) error {
				if before := a.Cell(at); before != text {
					a.Apply(Patch{Cells: []CellChange{{At: at, Before: before, After: text}}})
				}
				return nil
			})
		}), "Edit the cell in a prompt; enter commits, esc cancels").
		Verb("clear", clear, "Empty the cell").
		Verb("yank", yank, "Yank the cell into the shared registers").
		Verb("paste", paste, "Paste the register at the cell by its shape: a block fills, rows go below, columns go after").
		Verb("transform", transform, "Apply a named transformation to the cell, or to the selection when one is active")
	params("cell.yank", register)
	params("cell.paste", register)
	params("cell.transform", transformParams)

	rowOf := func(c ochazuke.Context) (int, error) {
		at := a.grid.At(c)
		if at.Row < 0 {
			return 0, errors.New("no row under the cursor")
		}
		return at.Row, nil
	}
	r.Define("row").
		Verb("delete", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			i, err := rowOf(c)
			if err != nil {
				return nil, err
			}
			a.Apply(Patch{Rows: []RowChange{{Index: i, Before: append([]string(nil), a.Rows[i]...)}}})
			return a.state(), nil
		}), "Delete the row under the cursor").Confirm("Delete row {{id}}?").When(grid.GuardInGrid).
		Verb("insertBelow", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			i := a.grid.At(c).Row
			a.Apply(Patch{Rows: []RowChange{{Index: i + 1, After: make([]string, len(a.Header))}}})
			return a.state(), a.grid.Goto(grid.Ref{Row: i + 1, Col: a.grid.Cursor().Col})
		}), "Insert an empty row below the cursor").When(grid.GuardInGrid).
		Verb("duplicate", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			i, err := rowOf(c)
			if err != nil {
				return nil, err
			}
			a.Apply(Patch{Rows: []RowChange{{Index: i + 1, After: append([]string(nil), a.Rows[i]...)}}})
			return a.state(), nil
		}), "Copy the row below itself").When(grid.GuardInGrid).
		Verb("transform", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			i, err := rowOf(c)
			if err != nil {
				return nil, err
			}
			set := make([]grid.Ref, 0, len(a.Header))
			for j := range a.Header {
				set = append(set, grid.Ref{Row: i, Col: j})
			}
			p, err := a.Transform(ochazuke.StringArg(args, "name", ""), set)
			if err != nil {
				return nil, err
			}
			a.Apply(p)
			return a.state(), nil
		}), "Apply a named transformation to every cell of the row").When(grid.GuardInGrid)
	params("row.transform", transformParams)

	colOf := func(c ochazuke.Context) (int, error) {
		at := a.grid.At(c)
		if at.Col < 0 {
			return 0, errors.New("no column under the cursor")
		}
		return at.Col, nil
	}
	r.Define("column").
		Verb("rename", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			j, err := colOf(c)
			if err != nil {
				return nil, err
			}
			rename := func(name string) error {
				before := a.column(j)
				after := before
				after.Name = name
				if name != before.Name {
					a.Apply(Patch{Columns: []ColumnChange{{Index: j, Before: &before, After: &after}}})
				}
				return nil
			}
			if n := ochazuke.StringArg(args, "name", ""); n != "" {
				return a.state(), rename(n)
			}
			return nil, a.prompt("Rename column "+grid.ColID(j), a.Header[j], "", rename)
		}), "Rename the column under the cursor").When(grid.GuardInGrid).
		Verb("delete", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			j, err := colOf(c)
			if err != nil {
				return nil, err
			}
			if n := ochazuke.StringArg(args, "name", ""); n != a.Header[j] {
				return nil, fmt.Errorf("column %s is %q, not %q", grid.ColID(j), a.Header[j], n)
			}
			before := a.column(j)
			a.Apply(Patch{Columns: []ColumnChange{{Index: j, Before: &before}}})
			return a.state(), nil
		}), "Delete the column under the cursor").Confirm("Delete column {{id}} ({{name}})?").When(grid.GuardInGrid).
		Verb("sort", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			j, err := colOf(c)
			if err != nil {
				return nil, err
			}
			return a.state(), a.sortBy(j)
		}), "Sort the rows by this column").When(grid.GuardInGrid).
		Verb("filter", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			j, err := colOf(c)
			if err != nil {
				return nil, err
			}
			if q, ok := args["query"].(string); ok {
				return a.state(), a.filterBy(j, q)
			}
			return nil, a.focusFilter()
		}), "Show only rows whose cell in this column contains the query (omit it to type one in the filter field); an empty query shows every row").When(grid.GuardInGrid).
		Verb("transform", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			j, err := colOf(c)
			if err != nil {
				return nil, err
			}
			set := make([]grid.Ref, 0, len(a.Rows))
			for _, i := range a.grid.Visible() {
				set = append(set, grid.Ref{Row: i, Col: j})
			}
			p, err := a.Transform(ochazuke.StringArg(args, "name", ""), set)
			if err != nil {
				return nil, err
			}
			a.Apply(p)
			return a.state(), nil
		}), "Apply a named transformation to every visible cell of the column").When(grid.GuardInGrid)
	params("column.rename", name)
	// name is the column's header: the confirm prompt names it and the
	// delete refuses a column whose header differs. The D row on each
	// column instance fills it (see columnRows).
	params("column.delete", map[string]ochazuke.Parameter{"name": {Type: "string", Required: true, Description: "The column's header, which the confirm names; D fills it"}})
	params("column.filter", map[string]ochazuke.Parameter{"query": {Type: "string", Description: "Omit to type it in the filter field"}})
	params("column.transform", transformParams)

	// The selection noun: the grid registered shape, cancel, swap, mark,
	// unmark and all; the application adds the verbs that edit.
	r.Define("selection").
		Verb("yank", yank, "Yank the selected cells with their shape").When(grid.GuardSelecting).
		Verb("delete", verb(func(c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			set := a.grid.Selected()
			if len(set) == 0 {
				return nil, errors.New("no selection")
			}
			shape := a.grid.Shape()
			y := a.yankOf(set, shape)
			var p Patch
			switch shape {
			case grid.ShapeRow:
				for _, i := range rowsOf(set) {
					p.Rows = append(p.Rows, RowChange{Index: i, Before: append([]string(nil), a.Rows[i]...)})
				}
			case grid.ShapeColumn:
				for _, j := range colsOf(set) {
					before := a.column(j)
					p.Columns = append(p.Columns, ColumnChange{Index: j, Before: &before})
				}
			default:
				for _, ref := range set {
					if before := a.Cell(ref); before != "" {
						p.Cells = append(p.Cells, CellChange{At: ref, Before: before})
					}
				}
			}
			a.grid.Cancel()
			a.Apply(p)
			return a.state(), r.StoreYank(y, ochazuke.StringArg(args, "register", ""), true)
		}), "Delete the selection into the registers: rows and columns are removed, a block is emptied").When(grid.GuardSelecting).
		Verb("paste", paste, "Paste the register over the selection's first cell by its shape").When(grid.GuardSelecting).
		Verb("clear", clear, "Empty the selected cells").When(grid.GuardSelecting).
		Verb("transform", transform, "Apply a named transformation to the selected cells").When(grid.GuardSelecting)
	params("selection.yank", register)
	params("selection.delete", register)
	params("selection.paste", register)
	params("selection.transform", transformParams)

	r.Define("transformations").
		Verb("list", verb(func(ochazuke.Context, ochazuke.Arguments) (any, error) { return TransformationChoices(), nil }), "List the transformations transform accepts: ids with labels, the transform name's pick list")
}

// sortBy orders the rows by a column, stably, as one row-replacing patch.
func (a *App) sortBy(j int) error {
	if j < 0 || j >= len(a.Header) {
		return errors.New("no column to sort by")
	}
	order := make([]int, len(a.Rows))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool { return a.Rows[order[x]][j] < a.Rows[order[y]][j] })
	var p Patch
	for i, from := range order {
		if from != i {
			p.Rows = append(p.Rows, RowChange{Index: i, Before: append([]string(nil), a.Rows[i]...), After: append([]string(nil), a.Rows[from]...)})
		}
	}
	a.Apply(p)
	return nil
}

// filterBy shows the rows whose cell in column j contains query, case as
// the search.case setting says (Runtime.CaseFor); "" shows every row.
func (a *App) filterBy(j int, query string) error {
	if j < 0 || j >= len(a.Header) {
		return errors.New("no column to filter by")
	}
	a.Filter = FilterState{Col: j, Query: query}
	a.syncFilterField()
	if query == "" {
		a.grid.SetFilter(nil)
		return nil
	}
	cm := a.r.CaseFor(query)
	a.grid.SetFilter(func(row []string) bool { return j < len(row) && cm.Contains(row[j], query) })
	return nil
}

func rowsOf(set []grid.Ref) []int {
	seen := map[int]bool{}
	var out []int
	for _, ref := range set {
		if !seen[ref.Row] {
			seen[ref.Row] = true
			out = append(out, ref.Row)
		}
	}
	sort.Ints(out)
	return out
}

func colsOf(set []grid.Ref) []int {
	seen := map[int]bool{}
	var out []int
	for _, ref := range set {
		if !seen[ref.Col] {
			seen[ref.Col] = true
			out = append(out, ref.Col)
		}
	}
	sort.Ints(out)
	return out
}

// state is the value a sheet verb returns.
func (a *App) state() map[string]any {
	return map[string]any{"path": a.Path, "dirty": a.Dirty, "rows": len(a.Rows), "columns": len(a.Header), "undo": len(a.Undo), "redo": len(a.Redo), "cursor": a.grid.Cursor(), "filter": a.Filter}
}

// title is the status line above the grid.
func (a *App) title() string {
	name := a.Path
	if name == "" {
		name = "(untitled)"
	}
	if a.Dirty {
		name += " [+]"
	}
	cursor := a.grid.Cursor()
	where := "no cell"
	if cursor.Row >= 0 {
		where = cursor.ID()
	}
	out := fmt.Sprintf("%s · %d rows × %d columns · %s", name, len(a.Rows), len(a.Header), where)
	if a.Filter.Query != "" && a.Filter.Col < len(a.Header) {
		out += fmt.Sprintf(" · filter %s~%q (%d shown)", a.Header[a.Filter.Col], a.Filter.Query, a.grid.VisibleCount())
	}
	if marks := a.grid.Marks(); len(marks) > 0 {
		out += fmt.Sprintf(" · %d marked", len(marks))
	}
	return out
}
