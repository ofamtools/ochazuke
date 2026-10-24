package grid

import (
	"errors"
	"fmt"

	"ochazuke"
)

// Register defines the grid and selection operations once per runtime. The
// grid noun answers move; the selection noun answers shape, cancel, swap,
// mark, unmark and all. Each resolves the grid from args.id or the nearest
// grid on the context's ancestry, so a cell, row or column focus reaches it.
// The application binds keys to these rows; the grid binds nothing itself.
func Register(r *ochazuke.Runtime) {
	if _, ok := r.Operations["grid.move"]; ok {
		return
	}
	id := map[string]ochazuke.Parameter{"id": {Type: "string", Description: "Grid instance (defaults to the one under the focus)"}}
	with := func(params map[string]ochazuke.Parameter) map[string]ochazuke.Parameter {
		out := map[string]ochazuke.Parameter{}
		for k, v := range id {
			out[k] = v
		}
		for k, v := range params {
			out[k] = v
		}
		return out
	}
	op := func(verb, noun, description string, params map[string]ochazuke.Parameter, fn func(*Grid, ochazuke.Context, ochazuke.Arguments) error) {
		r.Register(ochazuke.Operation{Verb: verb, Noun: noun, Description: description, Parameters: with(params), Result: "grid state", Run: func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
			g, err := Resolve(r, c, a)
			if err != nil {
				return nil, err
			}
			if err := fn(g, c, a); err != nil {
				return nil, err
			}
			return g.Snapshot(), nil
		}})
	}
	op("move", "grid", "Move the cursor: left, right, up, down, row-start, row-end, top, bottom; line is the 1-based row for top and bottom", map[string]ochazuke.Parameter{
		"motion": {Type: "string", Required: true}, "steps": {Type: "integer"}, "line": {Type: "integer"},
	}, func(g *Grid, _ ochazuke.Context, a ochazuke.Arguments) error {
		return g.Move(ochazuke.StringArg(a, "motion", ""), ochazuke.IntArg(a, "steps", 1), ochazuke.IntArg(a, "line", 0))
	})
	op("goto", "grid", "Move the cursor to a cell by row and column index", map[string]ochazuke.Parameter{
		"row": {Type: "integer", Required: true}, "col": {Type: "integer", Required: true},
	}, func(g *Grid, _ ochazuke.Context, a ochazuke.Arguments) error {
		return g.Goto(Ref{Row: ochazuke.IntArg(a, "row", 0), Col: ochazuke.IntArg(a, "col", 0)})
	})
	op("shape", "selection", "Start a selection of a shape (visual-block, visual-row, visual-column) at the cursor, or switch the active one's shape", map[string]ochazuke.Parameter{
		"shape": {Type: "string", Required: true},
	}, func(g *Grid, _ ochazuke.Context, a ochazuke.Arguments) error {
		return g.Select(ochazuke.StringArg(a, "shape", ""))
	})
	op("cancel", "selection", "End the selection and drop its marks", nil, func(g *Grid, _ ochazuke.Context, _ ochazuke.Arguments) error { g.Cancel(); return nil })
	op("swap", "selection", "Exchange the selection anchor and the cursor", nil, func(g *Grid, _ ochazuke.Context, _ ochazuke.Arguments) error { return g.SwapAnchor() })
	op("mark", "selection", "Toggle the mark on the cell, row or column under the cursor by the selection shape", nil, func(g *Grid, _ ochazuke.Context, _ ochazuke.Arguments) error {
		if g.Shape() == "" {
			return errors.New("start a selection before marking")
		}
		g.ToggleMark()
		return nil
	})
	op("unmark", "selection", "Clear the marks, keeping the selection", nil, func(g *Grid, _ ochazuke.Context, _ ochazuke.Arguments) error { g.ClearMarks(); return nil })
	op("all", "selection", "Span the whole visible sheet", nil, func(g *Grid, _ ochazuke.Context, _ ochazuke.Arguments) error { g.SelectAll(); return nil })
}

// Resolve finds the grid an operation addresses: args.id, else the nearest
// grid on the context's ancestry.
func Resolve(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (*Grid, error) {
	if id := ochazuke.StringArg(a, "id", ""); id != "" {
		if g := Of(r, id); g != nil {
			return g, nil
		}
		return nil, fmt.Errorf("unknown grid %q", id)
	}
	focus := c.InputID
	if focus == "" && len(c.Path) > 0 {
		focus = c.Path[len(c.Path)-1]
	}
	for _, id := range r.Ancestry(focus) {
		if g := Of(r, id); g != nil {
			return g, nil
		}
	}
	return nil, errors.New("no grid under the focus")
}
