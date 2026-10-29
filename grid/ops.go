package grid

import (
	_ "embed"
	"errors"
	"fmt"

	"github.com/unixsuperhero/ochazuke"
)

// defaultBindings are the grid noun's keys: the one place they are written.
//
//go:embed bindings.json
var defaultBindings []byte

// Register defines the grid and selection operations and the selection
// shape modes once per runtime. The grid noun answers move and goto; the
// selection noun answers swap, mark, unmark and all. A selection starts and
// ends with mode.set (a shape, or normal); the grid follows the mode. Each resolves the grid from args.id or the nearest
// grid on the context's ancestry, so a cell, row or column focus reaches it.
// The application binds keys to these rows; the grid binds only its column
// width keys (z >, z <, z =) on the grid noun.
func Register(r *ochazuke.Runtime) {
	if _, ok := r.Operations["ochazuke.grid.move"]; ok {
		return
	}
	r.Builtin(func() { register(r) })
}

func register(r *ochazuke.Runtime) {
	// Each shape is a selection mode (its shape a mode property the
	// framework reads, Runtime.SelectionShape); visual-block is the
	// editor's block too.
	for mode, shape := range map[string]string{ShapeBlock: ochazuke.SelectionBlock, ShapeRow: "row", ShapeColumn: "column"} {
		if !r.Modes[mode] {
			_ = r.DefineMode(mode, false)
		}
		if r.SelectionShape(mode) == "" {
			_ = r.DefineSelection(mode, shape)
		}
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
	op := func(verb, noun, description string, params map[string]ochazuke.Parameter, fn func(*Grid, ochazuke.Context, ochazuke.Arguments) error) *ochazuke.NounBuilder {
		b := r.Define(noun).Verb(verb, func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
			g, err := Resolve(r, c, a)
			if err != nil {
				return nil, err
			}
			if err := fn(g, c, a); err != nil {
				return nil, err
			}
			return g.Snapshot(), nil
		}, description)
		o := r.Operations[r.CanonicalKey(noun+"."+verb)]
		o.Parameters, o.Result = with(params), "grid state"
		r.Operations[o.Key()] = o
		return b
	}
	// The grid's guards: "in-grid" holds while the focus is a grid or sits
	// beneath one; "selecting" while, in addition, the mode is a selection
	// shape. The selection verbs are offered only while selecting; an
	// application guards its row and column verbs with in-grid.
	r.DefineGuard(GuardInGrid, func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool { return under(r, ev.Focus) })
	r.DefineGuard(GuardSelecting, func(r *ochazuke.Runtime, ev ochazuke.KeyEvent) bool {
		mode := ev.Mode.Name
		if ev.Mode.Payload.Return != "" {
			mode = ev.Mode.Payload.Return
		}
		return isShape(mode) && under(r, ev.Focus)
	})
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
	op("resize-column", "grid", "Widen (delta 1) or narrow (-1) the cursor's column by ColumnWidthStep cells; remembered under the grid's id", map[string]ochazuke.Parameter{
		"delta": {Type: "integer", Required: true}, "steps": {Type: "integer"},
	}, func(g *Grid, _ ochazuke.Context, a ochazuke.Arguments) error {
		g.ResizeColumn(ochazuke.IntArg(a, "delta", 0) * max(1, ochazuke.IntArg(a, "steps", 1)))
		return nil
	})
	op("reset-columns", "grid", "Make every column's width automatic again", nil, func(g *Grid, _ ochazuke.Context, _ ochazuke.Arguments) error {
		g.ResetColumnWidths()
		return nil
	})
	op("swap", "selection", "Exchange the selection anchor and the cursor", nil, func(g *Grid, _ ochazuke.Context, _ ochazuke.Arguments) error { return g.SwapAnchor() }).When(GuardSelecting)
	op("mark", "selection", "Toggle the mark on the cell, row or column under the cursor by the selection shape", nil, func(g *Grid, _ ochazuke.Context, _ ochazuke.Arguments) error {
		if g.Shape() == "" {
			return errors.New("start a selection before marking")
		}
		g.ToggleMark()
		return nil
	}).When(GuardSelecting)
	op("unmark", "selection", "Clear the marks, keeping the selection", nil, func(g *Grid, _ ochazuke.Context, _ ochazuke.Arguments) error { g.ClearMarks(); return nil }).When(GuardSelecting)
	op("all", "selection", "Span the whole visible sheet", nil, func(g *Grid, _ ochazuke.Context, _ ochazuke.Arguments) error { g.SelectAll(); return nil }).When(GuardSelecting)
	// The width keys (bindings.json) are the list table's (z >, z <, z =),
	// on the grid noun so every grid's cells answer them; the rest of the
	// grid's keys are the application's.
	r.MustLoadDefaultBindings(ochazuke.FrameworkBindingsSource+"grid/bindings.json", defaultBindings)
}

// The guard names Register defines.
const (
	GuardInGrid    = "in-grid"
	GuardSelecting = "selecting"
)

// under reports whether a focus is a grid or sits beneath one.
func under(r *ochazuke.Runtime, focus string) bool {
	for _, id := range r.Ancestry(focus) {
		if Of(r, id) != nil {
			return true
		}
	}
	return false
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
