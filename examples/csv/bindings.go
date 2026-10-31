package main

import (
	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/grid"
)

// bind installs the mode tables of the sketch. With a cell focused the stack
// is [cell, row, column, grid, sheet, app]: dd resolves on row, s on column,
// i on cell, ctrl+s on sheet. A selection is a global mode (the grid's
// shapes), entered and left with mode.set. The visual-block table on the
// grid noun holds the selection rows; visual-row and visual-column extend
// it. Motions take counts through count_pending, which is exclusive, so the
// cell's count_pending extends its normal table. } and tab leave the grid
// for the next focus stop (the app noun binds them).
func (a *App) bind() {
	r := a.r
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
	// Every noun that resolves to a cell set (cell, row, column, selection)
	// has transform and the same t rows; the one on the stack answers.
	transforms := func(m *ochazuke.ModeBuilder, noun string) {
		for _, kn := range [][2]string{{"t u", "upper"}, {"t l", "lower"}, {"t t", "trim"}, {"t i", "title"}, {"t s", "slug"}} {
			m.Bind(kn[0], ochazuke.Do(noun+".transform", ochazuke.Arguments{"name": kn[1]}))
		}
	}

	cell := r.Define("cell").Mode("normal").
		Bind("i", ochazuke.Do("cell.edit")).Bind("enter", ochazuke.Do("cell.edit")).
		Bind("x", ochazuke.Do("cell.clear")).
		Bind("y", ochazuke.Do("cell.yank")).Bind("p", ochazuke.Do("cell.paste")).
		// y y lives beside y: a sequence waits only for rows at the same level.
		Bind("y y", ochazuke.Do("row.duplicate")).
		Bind("v", shape(grid.ShapeBlock)).Bind("V", shape(grid.ShapeRow)).Bind("ctrl+v", shape(grid.ShapeColumn))
	motions(cell)
	transforms(cell, "cell")
	r.Define("cell").Mode("count_pending").Extends("normal")

	row := r.Define("row").Mode("normal").
		Bind("d d", ochazuke.Do("row.delete")).
		Bind("o", ochazuke.Do("row.insertBelow"))
	transforms(row, "row")

	r.Define("column").Mode("normal").
		Bind("r", ochazuke.Do("column.rename")).
		Bind("s", ochazuke.Do("column.sort")).
		Bind("/", ochazuke.Do("column.filter"))

	visual := r.Define("grid")
	visual.Mode(grid.ShapeRow).Extends(grid.ShapeBlock)
	visual.Mode(grid.ShapeColumn).Extends(grid.ShapeBlock)
	m := visual.Mode(grid.ShapeBlock)
	motions(m)
	transforms(m, "selection")
	m.Bind("o", ochazuke.Do("selection.swap")).
		Bind("v", shape(grid.ShapeBlock)).Bind("V", shape(grid.ShapeRow)).Bind("ctrl+v", shape(grid.ShapeColumn)).
		Bind("esc", ochazuke.Do("mode.set", ochazuke.Arguments{"mode": "normal"})).
		Bind("y", ochazuke.Do("selection.yank")).
		Bind("d", ochazuke.Do("selection.delete")).
		Bind("p", ochazuke.Do("selection.paste")).
		Bind("c", ochazuke.Do("selection.clear")).
		Bind("s", ochazuke.Do("column.sort")).
		Bind("f", ochazuke.Do("selection.transform", ochazuke.Arguments{"name": "fillDown"})).
		Bind("m", ochazuke.Do("selection.mark")).
		Bind("M", ochazuke.Do("selection.unmark")).
		Bind("ctrl+a", ochazuke.Do("selection.all")).
		Bind(":", ochazuke.Do("palette.open")).
		// Counts: the app noun enters count_pending from normal only.
		Pattern("digit-0", ochazuke.Enter("count_pending").FromPayload("count", ochazuke.SourceDigit))

	r.Define("sheet").Mode("normal").
		Bind("ctrl+s", ochazuke.Do("sheet.save")).
		Bind("ctrl+o", ochazuke.Do("sheet.open")).
		Bind("+", ochazuke.Do("sheet.addColumn")).
		Bind("S", ochazuke.Do("sheet.sort")).
		Bind("u", ochazuke.Do("sheet.undo")).
		Bind("ctrl+r", ochazuke.Do("sheet.redo"))
}

// instanceRows are the rows the grid puts on its focus instances: D on the
// column instance deletes that column with its header as the argument the
// confirm prompt names ("Delete column c2 (email)?"). A noun table cannot
// carry the header; the instance row is rebuilt on every sync.
func (a *App) instanceRows(kind string, at grid.Ref) []ochazuke.Binding {
	if kind != "column" || at.Col < 0 || at.Col >= len(a.Header) {
		return nil
	}
	row := ochazuke.Do("column.delete", ochazuke.Arguments{"name": a.Header[at.Col]})
	row.Mode, row.Keys = "normal", "D"
	return []ochazuke.Binding{row}
}
