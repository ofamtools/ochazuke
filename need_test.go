package ochazuke

import (
	"errors"
	"testing"
)

// A handler that finds late it lacks a value answers Ctx.Need: the typed
// error carries the parameter and the reason, behind ArgumentError too for a
// request refused for its arguments, and reads as the plain message.
func TestNeedIsTyped(t *testing.T) {
	r := NewRuntime()
	r.Instance("app")
	r.Define("swatch").Verb("paint", func(c Ctx) Result {
		return Failed(c.Need("colour", "painting needs a colour"))
	}, "Paint")
	_, err := r.Execute(Request{Noun: "swatch", Verb: "paint"})
	mp := needed(err)
	if mp == nil || mp.Param != "colour" || mp.Reason != "painting needs a colour" {
		t.Fatalf("err = %v, want a MissingParameterError for colour", err)
	}
	if err.Error() != "colour is required: painting needs a colour" {
		t.Fatalf("message = %q", err)
	}
}

// A missing required argument is the same typed error, so every route asks
// for it alike; the CLI still reads "x is required".
func TestMissingRequiredIsTyped(t *testing.T) {
	r := NewRuntime()
	r.Instance("app")
	r.Define("swatch").Verb("paint", func(c Ctx) Result { return Done(nil) }, "Paint")
	op := r.Operations.At("swatch.paint")
	op.Parameters = map[string]Parameter{"colour": {Type: "string", Required: true}}
	r.Operations.Set(op)
	_, err := r.Execute(Request{Noun: "swatch", Verb: "paint"})
	var arg *ArgumentError
	if !errors.As(err, &arg) || needed(err) == nil || needed(err).Param != "colour" || err.Error() != "colour is required" {
		t.Fatalf("err = %v, want an ArgumentError holding a MissingParameterError for colour", err)
	}
}

// A guard refusal states which guards are false without changing its text.
func TestRefusalNamesHeldGuards(t *testing.T) {
	r, _, _ := applicabilityFixture(t)
	guardVerb(r, "row.poke", "open")
	_, err := r.Execute(Request{Noun: "row", Verb: "poke"})
	var na *NotApplicableError
	if !errors.As(err, &na) || len(na.Held) != 1 || na.Held[0] != "open" {
		t.Fatalf("err = %v, want a NotApplicableError held by open", err)
	}
}

// A person's standard verb guarded off because no thing of its noun is on
// screen runs headless when it names its thing by key (the palette reaches
// every action); without the key it asks for it; a program's request is
// still refused.
func TestStandardVerbRunsAwayFromItsNoun(t *testing.T) {
	r, _ := newTaskRuntime(t)
	r.Instance("app")
	r.Settings().Set(PaletteReachSetting, true)
	r.DefineGuard("never", func(*Runtime, KeyEvent) bool { return false })
	guardVerb(r, "task.get", "never")
	q := Request{Noun: "task", Verb: "get", Args: Arguments{"id": "t2"}}
	if _, err := r.Execute(q); err == nil {
		t.Fatal("a program's request guarded off must be refused")
	}
	defer r.forceSource("palette")()
	if _, err := r.Execute(q); err != nil {
		t.Fatalf("a person's request naming its thing runs away from the noun: %v", err)
	}
	_, err := r.Execute(Request{Noun: "task", Verb: "get"})
	if mp := needed(err); mp == nil || mp.Param != "id" {
		t.Fatalf("err = %v, want a need for id", err)
	}
}
