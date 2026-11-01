package ochazuke

import (
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

const yankHistoryID = "__yank-history"

func registerYankHistory(r *Runtime) {
	r.Instance(yankHistoryID, Kind("ochazuke.yank-history")).Component = &yankHistory{r: r}
	add := func(verb, description string, params map[string]Parameter, fn Handler) {
		r.Register(Operation{Verb: verb, Noun: "ochazuke.yank-history", Description: description, Parameters: params, Result: "yank history state", Run: fn})
	}
	add("open", "Select a previous application yank without inserting it", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		g := r.Registers()
		g.selection = 0
		err := r.Push(yankHistoryID, yankHistoryID)
		return g.History, err
	})
	active := func(r *Runtime) error {
		if r.TopModal() != yankHistoryID {
			return errors.New("yank history is not active")
		}
		return nil
	}
	add("move", "Move the yank-history selection", map[string]Parameter{"steps": {Type: "integer", Required: true}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		if err := active(r); err != nil {
			return nil, err
		}
		g := r.Registers()
		g.selection = max(0, min(g.selection+IntArg(a, "steps", 0), len(g.History)-1))
		return g.selection, nil
	})
	add("select", "Make a history entry the unnamed register and restore origin focus", map[string]Parameter{"index": {Type: "integer"}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		if err := active(r); err != nil {
			return nil, err
		}
		g := r.Registers()
		index := IntArg(a, "index", g.selection)
		if index < 0 || index >= len(g.History) {
			return nil, errors.New("no yank at history index")
		}
		y := g.History[index]
		g.Named["\""] = Register{Yank: y}
		g.Active = ""
		err := r.Pop()
		return y, err
	})
}

// yankHistory is the yank history modal's component: it answers the
// shared motions (g g, G, a count's G, the page keys; CursorMotions) over
// the history's selection, a page being the rows its last frame showed.
type yankHistory struct {
	r *Runtime
	// height is the rows the last frame showed.
	height int
}

// MotionEdges is first and last while there are yanks.
func (y *yankHistory) MotionEdges() []SpillEdge {
	if len(y.r.Registers().History) == 0 {
		return nil
	}
	return []SpillEdge{EdgeFirst, EdgeLast}
}

// MoveToEdge selects the first or last yank, or yank number row.
func (y *yankHistory) MoveToEdge(edge SpillEdge, row int) {
	g := y.r.Registers()
	switch {
	case row > 0:
		g.selection = row - 1
	case edge == EdgeFirst:
		g.selection = 0
	case edge == EdgeLast:
		g.selection = len(g.History) - 1
	}
	g.selection = max(0, min(g.selection, len(g.History)-1))
}

// PageLines is the rows the last frame showed (a default before the
// first), 0 with no yanks.
func (y *yankHistory) PageLines() int {
	if len(y.r.Registers().History) == 0 {
		return 0
	}
	if y.height > 0 {
		return y.height
	}
	return defaultPageRows
}

// MovePage moves the selection by lines, clamped.
func (y *yankHistory) MovePage(lines int) {
	g := y.r.Registers()
	g.selection = max(0, min(g.selection+lines, len(g.History)-1))
}

func yankHistoryView(r *Runtime, inst *Instance, width, height int) Block {
	if inst != nil {
		if y, ok := inst.Component.(*yankHistory); ok {
			y.height = max(1, height)
		}
	}
	return Text(yankHistoryText(r, width, height))
}

// yankHistoryText renders the rows around the selection that fit the body.
func yankHistoryText(r *Runtime, width, height int) string {
	g := r.Registers()
	if len(g.History) == 0 {
		return "No yanks yet. Yank or kill text in any field."
	}
	selection := g.selection
	height = max(1, height)
	offset := max(0, selection-height+1)
	lines := make([]string, 0, height)
	for i := offset; i < min(len(g.History), offset+height); i++ {
		y := g.History[i]
		marker := "  "
		if i == selection {
			marker = "› "
		}
		text := strings.ReplaceAll(safeText(y.Text), "\n", " ↵ ")
		lines = append(lines, ansi.Truncate(fmt.Sprintf("%s%d [%s] %s", marker, i+1, y.Shape, text), max(1, width), ""))
	}
	return strings.Join(lines, "\n")
}
