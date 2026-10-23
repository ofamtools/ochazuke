package ochazuke

import (
	"errors"
	"fmt"
	"strings"
)

// NewDemo creates a concrete app using the same registration surface available to consumers.
func NewDemo() *Model {
	r := NewRuntime()
	for _, id := range []string{"panes", "agent", "directory", "notes", "dialog"} {
		r.Layers[id] = &Layer{ID: id, Noun: id, Actions: map[string]Handler{}}
	}
	for _, item := range []struct {
		in   *Input
		path []string
	}{
		{NewInput("directory", "/tmp", false), []string{"app", "panes", "agent", "directory"}},
		{NewInput("notes", "An agent's notes.\nEdit with Vim motions and Readline.", true), []string{"app", "panes", "agent", "notes"}},
		{NewInput("dialog", "A separate modal draft", true), []string{"app", "dialog"}},
	} {
		if err := r.AddInput(item.in, item.path); err != nil {
			panic(err)
		}
	}
	_ = r.Focus("directory")
	register := func(verb, noun, description string, params map[string]Parameter, fn Handler) {
		r.Register(Operation{Verb: verb, Noun: noun, Description: description, Parameters: params, Result: "JSON value", Run: fn})
	}
	if _, ok := r.Operations["set.input"]; !ok {
		register("set", "input", "Replace an input's entire text", map[string]Parameter{"text": {Type: "string", Required: true}}, func(r *Runtime, c Context, a Arguments) (any, error) {
			in := r.Inputs[c.InputID]
			if in == nil {
				return nil, errors.New("no focused input")
			}
			if err := in.Replace(0, len(in.Text), StringArg(a, "text", "")); err != nil {
				return nil, err
			}
			return in.Snapshot(), nil
		})
	}
	register("next", "input", "Focus the next input in the active page", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		if len(r.Frames) > 1 {
			return nil, errors.New("close this modal before switching page fields")
		}
		id := "notes"
		if c.InputID == "notes" {
			id = "directory"
		}
		err := r.Focus(id)
		return r.Context(), err
	})
	register("inspect", "selection", "Report which layer handles the selection action", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		r.Frames[len(r.Frames)-1].State["lastLayer"] = "app"
		return "app", nil
	})
	for _, id := range []string{"panes", "agent", "directory"} {
		name := id
		r.Layers[id].Actions["inspect.selection"] = func(r *Runtime, c Context, a Arguments) (any, error) {
			r.Frames[len(r.Frames)-1].State["lastLayer"] = name
			return name, nil
		}
	}
	register("toggle", "help", "Show or hide keyboard help", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		state := r.Frames[len(r.Frames)-1].State
		on, _ := state["help"].(bool)
		state["help"] = !on
		return !on, nil
	})
	picker := NewPicker(r)
	var m *Model
	m = NewModel(r, func(r *Runtime) string {
		if picker.IsOpen() && r.Frames[len(r.Frames)-1] == picker.frame {
			return picker.View()
		}
		c := r.Context()
		if len(r.Frames) > 1 {
			return "Modal draft (Ctrl-G closes and restores parent)\n\n" + InputView(r.Inputs[c.InputID], m.Width-4, max(2, m.Height-9))
		}
		var b strings.Builder
		b.WriteString("Panes / Agent 7\n\n")
		for _, id := range []string{"directory", "notes"} {
			mark := "  "
			if c.InputID == id {
				mark = "> "
			}
			fmt.Fprintf(&b, "%s%s\n%s\n\n", mark, strings.ToUpper(id), InputView(r.Inputs[id], m.Width-4, 3))
		}
		if layer, ok := r.Frames[0].State["lastLayer"]; ok {
			fmt.Fprintf(&b, "Selection action handled by: %s\n", layer)
		}
		if help, _ := r.Frames[0].State["help"].(bool); help {
			b.WriteString("Normal: h j k l · w b e · 0 $ · gg G · i a I A o O · x · u · Ctrl-R\nInsert: Ctrl-A/E · Ctrl-B/F · Alt-B/F · Ctrl-W/U/K · Ctrl-Y · Esc\nCtrl-N draft modal · Ctrl-G close modal · F2 inspect layer · ? help\n")
		} else {
			b.WriteString("i insert · Esc normal · u undo · Ctrl-R redo · ? help · Ctrl-N modal")
		}
		return b.String()
	})
	bind := func(layer, mode, key, verb, noun string, args Arguments) {
		r.Layers[layer].Bindings = append(r.Layers[layer].Bindings, Binding{Mode: mode, Keys: key, Request: Request{Verb: verb, Noun: noun, Args: args}})
	}
	bind("app", "normal", "ctrl+p", "open", "palette", nil)
	bind("app", "", "ctrl+@", "open", "palette", nil)
	bind("app", "", "ctrl+c", "quit", "app", nil)
	bind("app", "normal", "q", "quit", "app", nil)
	bind("app", "", "tab", "next", "input", nil)
	bind("app", "", "shift+tab", "next", "input", nil)
	bind("app", "normal", "ctrl+o", "open", "picker", Arguments{"kind": "directory"})
	bind("app", "normal", "ctrl+t", "open", "picker", Arguments{"kind": "file"})
	// Ctrl-N is Readline next-line in insert mode; modal shortcut is normal-only.
	bind("app", "normal", "ctrl+n", "push", "modal", Arguments{"id": "draft", "input": "dialog"})
	bind("dialog", "", "ctrl+g", "pop", "modal", nil)
	bind("app", "normal", "?", "toggle", "help", nil)
	bind("app", "normal", "f2", "inspect", "selection", nil)
	return m
}
