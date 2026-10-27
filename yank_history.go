package ochazuke

import (
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

const yankHistoryID = "__yank-history"

func registerYankHistory(r *Runtime) {
	r.Instance(yankHistoryID, Kind("app.yank-history"))
	r.Define("app.yank-history").Mode("").
		Bind("j", Do("app.yank-history.move", Arguments{"steps": 1})).
		Bind("down", Do("app.yank-history.move", Arguments{"steps": 1})).
		Bind("k", Do("app.yank-history.move", Arguments{"steps": -1})).
		Bind("up", Do("app.yank-history.move", Arguments{"steps": -1})).
		Bind("enter", Do("app.yank-history.select")).
		Bind("esc", Do("app.modal.pop"))
	add := func(verb, description string, params map[string]Parameter, fn Handler) {
		r.Register(Operation{Verb: verb, Noun: "app.yank-history", Description: description, Parameters: params, Result: "yank history state", Run: fn})
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
func yankHistoryView(r *Runtime, _ *Instance, width, height int) Block {
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
