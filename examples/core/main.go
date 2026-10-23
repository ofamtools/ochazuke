package main

import (
	"fmt"
	"ochazuke"
)

func main() {
	r := ochazuke.NewRuntime()
	for _, id := range []string{"panes", "agent", "directory", "modal"} {
		r.Layers[id] = &ochazuke.Layer{ID: id, Noun: id, Actions: map[string]ochazuke.Handler{}}
	}
	must(r.AddInput(ochazuke.NewInput("directory", "/tmp", false), []string{"app", "panes", "agent", "directory"}))
	must(r.AddInput(ochazuke.NewInput("picker", "/home", false), []string{"app", "modal"}))
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
		r.Layers[id].Actions["inspect.selection"] = func(*ochazuke.Runtime, ochazuke.Context, ochazuke.Arguments) (any, error) { return name, nil }
	}
	for _, expected := range []string{"directory", "agent", "panes", "app"} {
		got := exec(ochazuke.Request{Verb: "inspect", Noun: "selection"})
		if got != expected {
			panic(fmt.Sprintf("layer: got %v want %s", got, expected))
		}
		delete(r.Layers[expected].Actions, "inspect.selection")
	}
	fmt.Println("foundation: directory → agent → panes → app fallback; modal restoration; isolated focused-input undo: PASS")
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
