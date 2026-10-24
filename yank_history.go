package ochazuke

import (
	"errors"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

const yankHistoryID = "__yank-history"

func registerYankHistory(r *Runtime) {
	r.Layers[yankHistoryID] = &Layer{ID: yankHistoryID, Noun: "yank-history", Actions: map[string]Handler{}, Bindings: []Binding{
		{Keys: "j", Request: Request{Verb: "move", Noun: "yank-history", Args: Arguments{"steps": 1}}},
		{Keys: "down", Request: Request{Verb: "move", Noun: "yank-history", Args: Arguments{"steps": 1}}},
		{Keys: "k", Request: Request{Verb: "move", Noun: "yank-history", Args: Arguments{"steps": -1}}},
		{Keys: "up", Request: Request{Verb: "move", Noun: "yank-history", Args: Arguments{"steps": -1}}},
		{Keys: "enter", Request: Request{Verb: "select", Noun: "yank-history"}},
		{Keys: "esc", Request: Request{Verb: "pop", Noun: "modal"}},
	}}
	add := func(verb, description string, params map[string]Parameter, fn Handler) {
		r.Register(Operation{Verb: verb, Noun: "yank-history", Description: description, Parameters: params, Result: "yank history state", Run: fn})
	}
	add("open", "Select a previous application yank without inserting it", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.Push(&Frame{ID: yankHistoryID, Context: Context{Path: []string{"app", yankHistoryID}}, State: map[string]any{"selection": 0, "scroll": 0}})
		return r.YankHistory, err
	})
	active := func(r *Runtime) (*Frame, error) {
		f := r.Frames[len(r.Frames)-1]
		if f.ID != yankHistoryID {
			return nil, errors.New("yank history is not active")
		}
		return f, nil
	}
	add("move", "Move the yank-history selection", map[string]Parameter{"steps": {Type: "integer", Required: true}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		f, err := active(r)
		if err != nil {
			return nil, err
		}
		index := max(0, min(IntArg(Arguments(f.State), "selection", 0)+IntArg(a, "steps", 0), len(r.YankHistory)-1))
		f.State["selection"] = index
		return index, nil
	})
	add("select", "Make a history entry the unnamed register and restore origin focus", map[string]Parameter{"index": {Type: "integer"}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		f, err := active(r)
		if err != nil {
			return nil, err
		}
		index := IntArg(a, "index", IntArg(Arguments(f.State), "selection", 0))
		if index < 0 || index >= len(r.YankHistory) {
			return nil, errors.New("no yank at history index")
		}
		y := r.YankHistory[index]
		r.Registers["\""] = Register{Yank: y}
		r.ActiveRegister = ""
		err = r.Pop()
		return y, err
	})
}
func yankHistoryView(r *Runtime, f *Frame, width, height int) Block {
	return Text(yankHistoryText(r, f, width, height))
}

func yankHistoryText(r *Runtime, f *Frame, width, height int) string {
	if len(r.YankHistory) == 0 {
		return "No yanks yet. Yank or kill text in any field."
	}
	selection := IntArg(Arguments(f.State), "selection", 0)
	// The shared compositor owns scrolling; follow selection within its bounded body.
	offset := IntArg(Arguments(f.State), "scroll", 0)
	if selection < offset {
		offset = selection
	}
	if selection >= offset+max(1, height) {
		offset = selection - max(1, height) + 1
	}
	f.State["scroll"] = offset
	lines := make([]string, len(r.YankHistory))
	for i, y := range r.YankHistory {
		marker := "  "
		if i == selection {
			marker = "› "
		}
		text := strings.ReplaceAll(safeText(y.Text), "\n", " ↵ ")
		lines[i] = ansi.Truncate(fmt.Sprintf("%s%d [%s] %s", marker, i+1, y.Shape, text), max(1, width), "")
	}
	return strings.Join(lines, "\n")
}
