package ochazuke

import (
	"errors"
	"testing"
)

func TestLayerResolutionAndMissingOnlyFallback(t *testing.T) {
	r := NewRuntime()
	for _, id := range []string{"page", "agent", "directory"} {
		r.Layers[id] = &Layer{ID: id, Actions: map[string]Handler{}}
	}
	if err := r.AddInput(NewInput("field", "", false), []string{"app", "page", "agent", "directory"}); err != nil {
		t.Fatal(err)
	}
	_ = r.Focus("field")
	r.Register(Operation{Verb: "show", Noun: "owner", Run: func(*Runtime, Context, Arguments) (any, error) { return "app", nil }})
	for _, id := range []string{"page", "agent", "directory"} {
		name := id
		r.Layers[id].Actions["show.owner"] = func(*Runtime, Context, Arguments) (any, error) { return name, nil }
	}
	for _, want := range []string{"directory", "agent", "page", "app"} {
		got, err := r.Execute(Request{Verb: "show", Noun: "owner"})
		if err != nil || got != want {
			t.Fatalf("got %v, %v; want %q", got, err, want)
		}
		delete(r.Layers[want].Actions, "show.owner")
	}
	r.Layers["directory"].Actions["show.owner"] = func(*Runtime, Context, Arguments) (any, error) { return nil, errors.New("directory failed") }
	if _, err := r.Execute(Request{Verb: "show", Noun: "owner"}); err == nil || err.Error() != "directory failed" {
		t.Fatalf("must not fall back on action failure: %v", err)
	}
}
func TestBindingsOverlayAndSequenceCancellation(t *testing.T) {
	r := NewRuntime()
	r.Layers["input"] = &Layer{ID: "input"}
	_ = r.AddInput(NewInput("field", "", false), []string{"app", "input"})
	_ = r.Focus("field")
	r.Layers["app"].Bindings = []Binding{{Mode: "normal", Keys: "x", Request: Request{Verb: "quit", Noun: "app"}}, {Keys: "g g", Request: Request{Verb: "quit", Noun: "app"}}}
	r.Layers["input"].Bindings = []Binding{{Keys: "x", Request: Request{Verb: "insert", Noun: "input", Args: Arguments{"text": "inner"}}}}
	if handled, err := r.HandleBinding("x"); !handled || err != nil {
		t.Fatalf("binding failed: %v %v", handled, err)
	}
	if r.Quit || string(r.Inputs["field"].Text) != "inner" {
		t.Fatal("inner mode-independent binding must override outer mode-specific binding")
	}
	_, _ = r.HandleBinding("g")
	_, _ = r.HandleBinding("esc")
	if r.Pending != "" || r.Quit {
		t.Fatal("Escape must cancel pending sequence without execution")
	}
	_, _ = r.HandleBinding("g")
	_, _ = r.HandleBinding("g")
	if !r.Quit {
		t.Fatal("completed sequence must execute")
	}
}
func TestModalRestoresExactInputInstance(t *testing.T) {
	r := NewRuntime()
	r.Layers["editor"] = &Layer{ID: "editor"}
	r.Layers["child"] = &Layer{ID: "child"}
	input := NewInput("editor", "draft", true)
	child := NewInput("child", "", false)
	_ = r.AddInput(input, []string{"app", "editor"})
	_ = r.AddInput(child, []string{"app", "child"})
	_ = r.Focus("editor")
	input.Cursor = 2
	input.SetMode("insert")
	_ = input.Replace(2, 2, "X")
	_ = r.Push(&Frame{ID: "dialog", Context: Context{Path: []string{"app", "child"}, InputID: "child"}, State: map[string]any{}})
	_, _ = r.Execute(Request{Verb: "insert", Noun: "input", Args: Arguments{"text": "child"}})
	_ = r.Pop()
	if r.Inputs[r.Context().InputID] != input || string(input.Text) != "drXaft" || input.Cursor != 3 || input.Mode != "insert" {
		t.Fatal("parent editing state was not restored")
	}
	_, err := r.Execute(Request{Verb: "undo", Noun: "input"})
	if err != nil {
		t.Fatal(err)
	}
	if string(input.Text) != "draft" || string(child.Text) != "child" {
		t.Fatal("undo must belong to originating input")
	}
}
func TestOperationArgumentErrorsDoNotMutateInput(t *testing.T) {
	r := NewRuntime()
	_ = r.AddInput(NewInput("field", "safe", false), []string{"app"})
	_ = r.Focus("field")
	for _, args := range []Arguments{{"steps": -1}, {"steps": 1.5}, {"steps": "2"}, {"unknown": true}} {
		if _, err := r.Execute(Request{Verb: "undo", Noun: "input", Args: args}); err == nil {
			t.Fatalf("expected error for %#v", args)
		}
	}
	if string(r.Inputs["field"].Text) != "safe" {
		t.Fatal("invalid requests mutated text")
	}
}
