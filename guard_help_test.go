package ochazuke

import "testing"

// A row whose guard is false at the focus would not answer, so ? help
// leaves it out as the footer does; bindings list keeps it (UX review 2 F9).
func TestHelpHidesARowWhoseGuardIsFalse(t *testing.T) {
	r := NewRuntime()
	empty := true
	r.DefineGuard("empty", func(*Runtime, KeyEvent) bool { return empty })
	r.Define("thing").
		Verb("ensure", func(Ctx) Result { return Done(nil) }, "Start the daemon").
		Verb("open", func(Ctx) Result { return Done(nil) }, "Open it").
		Mode("normal").
		Bind("e", Do("thing.ensure").Guard("empty")).
		Bind("o", Do("thing.open"))
	r.Instance("t1", Kind("thing"))
	if err := r.SetFocus("t1"); err != nil {
		t.Fatal(err)
	}
	keys := func() map[string]bool {
		out := map[string]bool{}
		for _, row := range BindingHelp(r, r.Focus) {
			out[row.Key] = true
		}
		return out
	}
	if got := keys(); !got["e"] || !got["o"] {
		t.Fatalf("guard true: help keys %v", got)
	}
	empty = false
	if got := keys(); got["e"] || !got["o"] {
		t.Fatalf("guard false: help keys %v", got)
	}
	listed := false
	for _, b := range r.ListBindings(r.Focus) {
		listed = listed || b.Keys == "e"
	}
	if !listed {
		t.Fatal("bindings list should keep the guarded row")
	}
}
