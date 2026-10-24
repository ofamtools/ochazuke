package main

import (
	"ochazuke"
	"ochazuke/grid"
)

// bind installs the mode tables of the sketch. With a cell focused the stack
// is [cell, row, column, grid, sheet, app]: dd resolves on row, s on column,
// i on cell, ctrl+s on sheet. The three visual modes share one table on the
// grid noun; motions take counts through count_pending, which is exclusive,
// so the motion rows are installed there too.
func (a *App) bind() {
	r := a.r
	for _, mode := range []string{grid.ShapeRow, grid.ShapeColumn} {
		if err := r.DefineMode(mode, false); err != nil {
			panic(err)
		}
	}
	move := func(motion string) ochazuke.Binding {
		return ochazuke.Do("move.grid", ochazuke.Arguments{"motion": motion}).FromPayload("steps", ochazuke.SourceCount)
	}
	line := func(motion string) ochazuke.Binding {
		return ochazuke.Do("move.grid", ochazuke.Arguments{"motion": motion}).FromPayload("line", ochazuke.SourceLine)
	}
	motions := func(m *ochazuke.ModeBuilder) {
		m.Bind("h", move("left")).Bind("left", move("left")).
			Bind("l", move("right")).Bind("right", move("right")).
			Bind("j", move("down")).Bind("down", move("down")).
			Bind("k", move("up")).Bind("up", move("up")).
			Bind("0", move("row-start")).Bind("home", move("row-start")).
			Bind("$", move("row-end")).Bind("end", move("row-end")).
			Bind("g g", line("top")).Bind("G", line("bottom"))
	}
	shape := func(s string) ochazuke.Binding { return ochazuke.Do("shape.selection", ochazuke.Arguments{"shape": s}) }
	transform := func(name string) ochazuke.Binding {
		return ochazuke.Do("transform.selection", ochazuke.Arguments{"name": name})
	}
	transforms := func(m *ochazuke.ModeBuilder) {
		m.Bind("t u", transform("upper")).Bind("t l", transform("lower")).Bind("t t", transform("trim"))
	}

	cell := r.Define("cell").Mode("normal").
		Bind("i", ochazuke.Do("edit.cell")).Bind("enter", ochazuke.Do("edit.cell")).
		Bind("x", ochazuke.Do("clear.cell")).
		Bind("y", ochazuke.Do("yank.cell")).Bind("p", ochazuke.Do("paste.cell")).
		// y y lives beside y: a sequence waits only for rows at the same level.
		Bind("y y", ochazuke.Do("duplicate.row")).
		Bind("v", shape(grid.ShapeBlock)).Bind("V", shape(grid.ShapeRow)).Bind("ctrl+v", shape(grid.ShapeColumn))
	motions(cell)
	transforms(cell)
	motions(r.Define("cell").Mode("count_pending"))

	r.Define("row").Mode("normal").
		Bind("d d", ochazuke.Do("delete.row")).
		Bind("o", ochazuke.Do("insertBelow.row"))

	r.Define("column").Mode("normal").
		Bind("r", ochazuke.Do("rename.column")).
		Bind("D", ochazuke.Do("delete.column")).
		Bind("s", ochazuke.Do("sort.column")).
		Bind("/", ochazuke.Do("filter.column"))

	visual := r.Define("grid")
	for _, mode := range []string{grid.ShapeBlock, grid.ShapeRow, grid.ShapeColumn} {
		m := visual.Mode(mode)
		motions(m)
		transforms(m)
		m.Bind("o", ochazuke.Do("swap.selection")).
			Bind("v", shape(grid.ShapeBlock)).Bind("V", shape(grid.ShapeRow)).Bind("ctrl+v", shape(grid.ShapeColumn)).
			Bind("esc", ochazuke.Do("cancel.selection")).
			Bind("y", ochazuke.Do("yank.selection")).
			Bind("d", ochazuke.Do("delete.selection")).
			Bind("p", ochazuke.Do("paste.selection")).
			Bind("c", ochazuke.Do("clear.selection")).
			Bind("s", ochazuke.Do("sort.column")).
			Bind("f", transform("fillDown")).
			Bind("m", ochazuke.Do("mark.selection")).
			Bind("M", ochazuke.Do("unmark.selection")).
			Bind("ctrl+a", ochazuke.Do("all.selection")).
			// Counts: the app noun enters count_pending from normal only.
			Pattern("digit-0", ochazuke.Enter("count_pending").FromPayload("count", ochazuke.SourceDigit))
	}

	r.Define("sheet").Mode("normal").
		Bind("ctrl+s", ochazuke.Do("save.sheet")).
		Bind("ctrl+o", ochazuke.Do("open.sheet")).
		Bind("+", ochazuke.Do("addColumn.sheet")).
		Bind("S", ochazuke.Do("sort.sheet")).
		Bind("u", ochazuke.Do("undo.sheet")).
		Bind("ctrl+r", ochazuke.Do("redo.sheet"))
}
