package main

import (
	"fmt"
	"ochazuke"
)

func main() {
	r := ochazuke.NewRuntime()
	r.Instance("panes")
	r.Instance("agent", ochazuke.Parent("panes"))
	r.Instance("field", ochazuke.Parent("agent"), ochazuke.Kind("directory"))
	r.Instance("dialog")
	must(r.AddInput(ochazuke.NewInput("directory", "/tmp", false), ochazuke.Parent("field")))
	must(r.AddInput(ochazuke.NewInput("picker", "/home", false), ochazuke.Parent("dialog")))
	must(r.Focus("directory"))
	exec := func(q ochazuke.Request) any { v, e := r.Execute(q); must(e); return v }
	exec(ochazuke.Request{Verb: "insert", Noun: "input", Args: ochazuke.Arguments{"text": "x"}})
	exec(ochazuke.Request{Verb: "push", Noun: "modal", Args: ochazuke.Arguments{"id": "picker", "input": "picker"}})
	exec(ochazuke.Request{Verb: "insert", Noun: "input", Args: ochazuke.Arguments{"text": "y"}})
	exec(ochazuke.Request{Verb: "pop", Noun: "modal"})
	if r.Context().InputID != "directory" || string(r.Inputs["directory"].Text) != "x/tmp" {
		panic("modal failed to restore editor")
	}
	exec(ochazuke.Request{Verb: "undo", Noun: "input", Target: "focused-input"})
	if string(r.Inputs["directory"].Text) != "/tmp" || string(r.Inputs["picker"].Text) != "y/home" {
		panic("input histories leaked")
	}
	r.Register(ochazuke.Operation{Verb: "inspect", Noun: "selection", Description: "Demonstrate scoped actions", Result: "layer", Run: func(*ochazuke.Runtime, ochazuke.Context, ochazuke.Arguments) (any, error) { return "app", nil }})
	for _, id := range []string{"panes", "agent", "directory"} {
		name := id
		r.Define(id).Action("inspect.selection", func(*ochazuke.Runtime, ochazuke.Context, ochazuke.Arguments) (any, error) { return name, nil })
	}
	for _, expected := range []string{"directory", "agent", "panes", "app"} {
		got := exec(ochazuke.Request{Verb: "inspect", Noun: "selection"})
		if got != expected {
			panic(fmt.Sprintf("noun: got %v want %s", got, expected))
		}
		delete(r.Noun(expected).Actions, "inspect.selection")
	}
	fmt.Println("foundation: directory → agent → panes → app fallback; modal restoration; isolated focused-input undo: PASS")
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
