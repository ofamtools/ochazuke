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
		return ochazuke.Do("grid.move", ochazuke.Arguments{"motion": motion}).FromPayload("steps", ochazuke.SourceCount)
	}
	line := func(motion string) ochazuke.Binding {
		return ochazuke.Do("grid.move", ochazuke.Arguments{"motion": motion}).FromPayload("line", ochazuke.SourceLine)
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
	shape := func(s string) ochazuke.Binding { return ochazuke.Do("mode.set", ochazuke.Arguments{"mode": s}) }
	transform := func(name string) ochazuke.Binding {
		return ochazuke.Do("selection.transform", ochazuke.Arguments{"name": name})
	}
	transforms := func(m *ochazuke.ModeBuilder) {
		m.Bind("t u", transform("upper")).Bind("t l", transform("lower")).Bind("t t", transform("trim"))
	}

	cell := r.Define("cell").Mode("normal").
		Bind("i", ochazuke.Do("cell.edit")).Bind("enter", ochazuke.Do("cell.edit")).
		Bind("x", ochazuke.Do("cell.clear")).
		Bind("y", ochazuke.Do("cell.yank")).Bind("p", ochazuke.Do("cell.paste")).
		// y y lives beside y: a sequence waits only for rows at the same level.
		Bind("y y", ochazuke.Do("row.duplicate")).
		Bind("v", shape(grid.ShapeBlock)).Bind("V", shape(grid.ShapeRow)).Bind("ctrl+v", shape(grid.ShapeColumn))
	motions(cell)
	transforms(cell)
	motions(r.Define("cell").Mode("count_pending"))

	r.Define("row").Mode("normal").
		Bind("d d", ochazuke.Do("row.delete")).
		Bind("o", ochazuke.Do("row.insertBelow"))

	r.Define("column").Mode("normal").
		Bind("r", ochazuke.Do("column.rename")).
		Bind("D", ochazuke.Do("column.delete")).
		Bind("s", ochazuke.Do("column.sort")).
		Bind("/", ochazuke.Do("column.filter"))

	visual := r.Define("grid")
	for _, mode := range []string{grid.ShapeBlock, grid.ShapeRow, grid.ShapeColumn} {
		m := visual.Mode(mode)
		motions(m)
		transforms(m)
		m.Bind("o", ochazuke.Do("selection.swap")).
			Bind("v", shape(grid.ShapeBlock)).Bind("V", shape(grid.ShapeRow)).Bind("ctrl+v", shape(grid.ShapeColumn)).
			Bind("esc", ochazuke.Do("mode.set", ochazuke.Arguments{"mode": "normal"})).
			Bind("y", ochazuke.Do("selection.yank")).
			Bind("d", ochazuke.Do("selection.delete")).
			Bind("p", ochazuke.Do("selection.paste")).
			Bind("c", ochazuke.Do("selection.clear")).
			Bind("s", ochazuke.Do("column.sort")).
			Bind("f", transform("fillDown")).
			Bind("m", ochazuke.Do("selection.mark")).
			Bind("M", ochazuke.Do("selection.unmark")).
			Bind("ctrl+a", ochazuke.Do("selection.all")).
			// Counts: the app noun enters count_pending from normal only.
			Pattern("digit-0", ochazuke.Enter("count_pending").FromPayload("count", ochazuke.SourceDigit))
	}

	r.Define("sheet").Mode("normal").
		Bind("ctrl+s", ochazuke.Do("sheet.save")).
		Bind("ctrl+o", ochazuke.Do("sheet.open")).
		Bind("+", ochazuke.Do("sheet.addColumn")).
		Bind("S", ochazuke.Do("sheet.sort")).
		Bind("u", ochazuke.Do("sheet.undo")).
		Bind("ctrl+r", ochazuke.Do("sheet.redo"))
}
