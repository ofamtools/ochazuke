package ochazuke

import (
	"strings"
	"testing"
)

// A target that names no instance names a thing by key on a verb that
// takes one: register set --target a writes register a whether or not it
// exists yet, as --name a does; get reads it; an instance target is still
// the instance; a verb without a key subject still refuses.
func TestTargetNamesThingKey(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	if _, err := r.Execute(Request{Noun: "ochazuke.register", Verb: "set", Target: "b", Args: Arguments{"prop": "text", "value": "new"}}); err != nil {
		t.Fatalf("set on a register that does not exist yet, by target: %v", err)
	}
	if got := r.Registers().Named["b"].Yank.Text; got != "new" {
		t.Fatalf("register b holds %q", got)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.register", Verb: "set", Target: "b", Args: Arguments{"props": map[string]any{"text": "again"}}}); err != nil {
		t.Fatalf("set on an existing register by target: %v", err)
	}
	// history.undo puts the old text back, as for a set by key.
	if _, err := r.Execute(Request{Noun: "ochazuke.history", Verb: "undo"}); err != nil {
		t.Fatal(err)
	}
	if got := r.Registers().Named["b"].Yank.Text; got != "new" {
		t.Fatalf("undo left register b %q", got)
	}
	v, err := r.Execute(Request{Noun: "ochazuke.register", Verb: "get", Target: "b", Args: Arguments{"prop": "text"}})
	if err != nil || v != "new" {
		t.Fatalf("get by target: %v %v", v, err)
	}
	// A key argument beside a target that is no instance is not read
	// twice: the target stays a target, and an unknown one refuses.
	if _, err := r.Execute(Request{Noun: "ochazuke.register", Verb: "set", Target: "b", Args: Arguments{"name": "c", "prop": "text", "value": "cee"}}); err == nil || !strings.Contains(err.Error(), `unknown target "b"`) {
		t.Fatalf("key and target: %v", err)
	}
	// A verb whose noun takes no key subject still refuses an unknown target.
	if _, err := r.Execute(Request{Noun: "ochazuke.register", Verb: "list", Target: "nowhere"}); err == nil || !strings.Contains(err.Error(), `unknown target "nowhere"`) {
		t.Fatalf("list with an unknown target: %v", err)
	}
	// An instance target is still the instance.
	if _, err := r.Execute(Request{Noun: "ochazuke.register", Verb: "set", Target: "app", Args: Arguments{"name": "d", "prop": "text", "value": "dee"}}); err != nil {
		t.Fatalf("instance target: %v", err)
	}
	// The history keeps a request that ran as a key, not a target.
	for _, e := range r.History(100) {
		if e.Result.OK && e.Request.Noun == "ochazuke.register" && e.Request.Target == "b" {
			t.Fatalf("history kept the thing as a target: %+v", e.Request)
		}
	}
}
