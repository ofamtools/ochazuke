package ochazuke

import (
	"errors"
	"testing"
)

func TestNounActionResolutionAndMissingOnlyFallback(t *testing.T) {
	r := NewRuntime()
	r.Instance("page")
	r.Instance("agent", Parent("page"))
	r.Instance("directory", Parent("agent"))
	if err := r.AddInput(NewInput("field", "", false), Parent("directory")); err != nil {
		t.Fatal(err)
	}
	_ = r.SetFocus("field")
	r.Register(Operation{Verb: "show", Noun: "owner", Run: func(*Runtime, Context, Arguments) (any, error) { return "app", nil }})
	for _, id := range []string{"page", "agent", "directory"} {
		name := id
		r.Define(r.Instances[id].Noun).Action("owner.show", func(*Runtime, Context, Arguments) (any, error) { return name, nil })
	}
	for _, want := range []string{"directory", "agent", "page", "app"} {
		got, err := r.Execute(Request{Verb: "show", Noun: "owner"})
		if err != nil || got != want {
			t.Fatalf("got %v, %v; want %q", got, err, want)
		}
		delete(r.Noun(want).Actions, "owner.show")
	}
	r.Define("directory").Action("owner.show", func(*Runtime, Context, Arguments) (any, error) { return nil, errors.New("directory failed") })
	if _, err := r.Execute(Request{Verb: "show", Noun: "owner"}); err == nil || err.Error() != "directory failed" {
		t.Fatalf("must not fall back on action failure: %v", err)
	}
}
func TestBindingsOverlayAndSequenceCancellation(t *testing.T) {
	r := NewRuntime()
	r.Instance("panel")
	_ = r.AddInput(NewInput("field", "", false), Parent("panel"))
	_ = r.SetFocus("field")
	r.Define("app").Mode("normal").Bind("x", Do("app.quit")).Mode("").Bind("g g", Do("app.quit"))
	r.Define("panel").Mode("").Bind("x", Do("ochazuke.input.insert", Arguments{"text": "inner"}))
	if handled, err := r.HandleBinding("x"); !handled || err != nil {
		t.Fatalf("binding failed: %v %v", handled, err)
	}
	if r.Quit || string(r.Input("field").Text) != "inner" {
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
	r.Instance("panel")
	r.Instance("child-panel")
	input := NewInput("editor", "draft", true)
	child := NewInput("child", "", false)
	_ = r.AddInput(input, Parent("panel"))
	_ = r.AddInput(child, Parent("child-panel"))
	_ = r.SetFocus("editor")
	input.Cursor = 2
	input.SetMode("insert")
	_ = input.Replace(2, 2, "X")
	_ = r.Push("dialog", "child")
	_, _ = r.Execute(Request{Verb: "insert", Noun: "ochazuke.input", Args: Arguments{"text": "child"}})
	_ = r.Pop()
	if r.Input(r.Context().InputID) != input || string(input.Text) != "drXaft" || input.Cursor != 3 || input.Mode != "insert" {
		t.Fatal("parent editing state was not restored")
	}
	_, err := r.Execute(Request{Verb: "undo", Noun: "ochazuke.input"})
	if err != nil {
		t.Fatal(err)
	}
	if string(input.Text) != "draft" || string(child.Text) != "child" {
		t.Fatal("undo must belong to originating input")
	}
}
func TestOperationArgumentErrorsDoNotMutateInput(t *testing.T) {
	r := NewRuntime()
	_ = r.AddInput(NewInput("field", "safe", false))
	_ = r.SetFocus("field")
	for _, args := range []Arguments{{"steps": -1}, {"steps": 1.5}, {"steps": "2"}, {"unknown": true}} {
		if _, err := r.Execute(Request{Verb: "undo", Noun: "ochazuke.input", Args: args}); err == nil {
			t.Fatalf("expected error for %#v", args)
		}
	}
	if string(r.Input("field").Text) != "safe" {
		t.Fatal("invalid requests mutated text")
	}
}
