package ochazuke

import (
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

const yankHistoryID = "__yank-history"

func registerYankHistory(r *Runtime) {
	r.Instance(yankHistoryID, Kind("yank-history"))
	r.Define("yank-history").Mode("").
		Bind("j", Do("move.yank-history", Arguments{"steps": 1})).
		Bind("down", Do("move.yank-history", Arguments{"steps": 1})).
		Bind("k", Do("move.yank-history", Arguments{"steps": -1})).
		Bind("up", Do("move.yank-history", Arguments{"steps": -1})).
		Bind("enter", Do("select.yank-history")).
		Bind("esc", Do("pop.modal"))
	add := func(verb, description string, params map[string]Parameter, fn Handler) {
		r.Register(Operation{Verb: verb, Noun: "yank-history", Description: description, Parameters: params, Result: "yank history state", Run: fn})
	}
	add("open", "Select a previous application yank without inserting it", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		r.yankSelection = 0
		err := r.Push(&Frame{ID: yankHistoryID, Focus: yankHistoryID})
		return r.YankHistory, err
	})
	active := func(r *Runtime) error {
		if r.top().ID != yankHistoryID {
			return errors.New("yank history is not active")
		}
		return nil
	}
	add("move", "Move the yank-history selection", map[string]Parameter{"steps": {Type: "integer", Required: true}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		if err := active(r); err != nil {
			return nil, err
		}
		r.yankSelection = max(0, min(r.yankSelection+IntArg(a, "steps", 0), len(r.YankHistory)-1))
		return r.yankSelection, nil
	})
	add("select", "Make a history entry the unnamed register and restore origin focus", map[string]Parameter{"index": {Type: "integer"}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		if err := active(r); err != nil {
			return nil, err
		}
		index := IntArg(a, "index", r.yankSelection)
		if index < 0 || index >= len(r.YankHistory) {
			return nil, errors.New("no yank at history index")
		}
		y := r.YankHistory[index]
		r.Registers["\""] = Register{Yank: y}
		r.ActiveRegister = ""
		err := r.Pop()
		return y, err
	})
}
func yankHistoryView(r *Runtime, f *Frame, width, height int) Block {
	return Text(yankHistoryText(r, width, height))
}

// yankHistoryText renders the rows around the selection that fit the body.
func yankHistoryText(r *Runtime, width, height int) string {
	if len(r.YankHistory) == 0 {
		return "No yanks yet. Yank or kill text in any field."
	}
	selection := r.yankSelection
	height = max(1, height)
	offset := max(0, selection-height+1)
	lines := make([]string, 0, height)
	for i := offset; i < min(len(r.YankHistory), offset+height); i++ {
		y := r.YankHistory[i]
		marker := "  "
		if i == selection {
			marker = "› "
		}
		text := strings.ReplaceAll(safeText(y.Text), "\n", " ↵ ")
		lines = append(lines, ansi.Truncate(fmt.Sprintf("%s%d [%s] %s", marker, i+1, y.Shape, text), max(1, width), ""))
	}
	return strings.Join(lines, "\n")
}
