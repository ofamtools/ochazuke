package ochazuke

import (
	"slices"
	"sort"
	"testing"
)

// notBuiltin lists the operations of r not marked Builtin, sorted.
func notBuiltin(r *Runtime) []string {
	var out []string
	for key, op := range r.Operations {
		if !op.Builtin {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func TestFrameworkOperationsAreBuiltin(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	NewPicker(r)
	if got := notBuiltin(r); len(got) > 0 {
		t.Fatalf("framework operations not builtin: %v", got)
	}
	for _, key := range []string{"outline.add", "chooser.pick", "settings.edit", "surface.scroll", "palette.open", "picker.open", "appearance.set"} {
		op, ok := r.Operations[key]
		if !ok || !op.Builtin {
			t.Fatalf("%s: registered %v, builtin %v", key, ok, op.Builtin)
		}
	}
}

func TestApplicationVerbsAreNotBuiltin(t *testing.T) {
	r := NewRuntime()
	done := func(Ctx) Result { return Done(nil) }
	r.Define("worktree").Verb("remove", done).Confirm("Remove {{label}}?")
	// An application verb on a framework noun stays the application's.
	r.Define("history").Verb("capture-hook", done)
	NewModel(r)
	// A framework constructor run after the application's verbs marks
	// only what it registers.
	NewPicker(r)
	r.Define("worktree").Verb("add", done)
	if got, want := notBuiltin(r), []string{"history.capture-hook", "worktree.add", "worktree.remove"}; !slices.Equal(got, want) {
		t.Fatalf("not builtin = %v, want %v", got, want)
	}
	if op := r.Operations["picker.open"]; !op.Builtin {
		t.Fatal("picker.open registered after the application's verbs is not builtin")
	}
	if op := r.Operations["worktree.remove"]; op.Confirm == "" || op.Kind != KindApplication {
		t.Fatalf("worktree.remove = %+v", op)
	}
}

func TestBuiltinNests(t *testing.T) {
	r := NewRuntime()
	done := func(Ctx) Result { return Done(nil) }
	r.Builtin(func() {
		r.Define("outer").Verb("a", done)
		r.Builtin(func() { r.Define("inner").Verb("b", done) })
		r.Define("outer").Verb("c", done)
	})
	for _, key := range []string{"outer.a", "inner.b", "outer.c"} {
		if !r.Operations[key].Builtin {
			t.Fatalf("%s not builtin", key)
		}
	}
}
