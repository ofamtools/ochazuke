package ochazuke

import (
	"reflect"
	"slices"
	"testing"
)

// forceMode installs a mode state for a test that needs one no public
// request builds in a single step (a pending mode with a stored request, a
// prefix payload, a mode a fixture's focus would not land in). It goes
// through the one writer, so the guard (TestStateWriteOwnership) holds for
// tests too: observers run and the generation advances as for any change.
func forceMode(r *Runtime, m ModeState) { r.changeMode(m, modeNamed) }

// forceModeCount sets the current mode's pending count, as count.append
// digits would.
func forceModeCount(r *Runtime, count int) {
	next := r.Mode
	next.Payload.Count = count
	r.changeMode(next, modePayloadUpdate)
}

// modeSpy is a ModeObserver that records each call with the mode and
// generation the runtime shows while it runs.
type modeSpy struct {
	id    string
	r     *Runtime
	calls *[]modeSpyCall
}

type modeSpyCall struct {
	id, from, to, live string
	generation         uint64
}

func (s modeSpy) ModeChanged(from, to string) {
	*s.calls = append(*s.calls, modeSpyCall{s.id, from, to, s.r.Mode.Name, s.r.modeGeneration})
}

// The one writer assigns, advances the generation, then tells the
// observers: the new focus path innermost first, then the path being left,
// each instance once, and only when the base mode changed.
func TestModeWriterOrdering(t *testing.T) {
	r := NewRuntime()
	var calls []modeSpyCall
	for _, spec := range []struct{ id, parent string }{{"a", "app"}, {"b", "a"}, {"c", "app"}} {
		r.Instance(spec.id, Parent(spec.parent)).Component = modeSpy{spec.id, r, &calls}
	}
	if err := r.SetFocus("b"); err != nil {
		t.Fatal(err)
	}
	calls = nil
	before := r.modeGeneration
	r.changeMode(ModeState{Name: "visual"}, modeNamed, "c", "a")
	if r.Mode.Name != "visual" || r.modeGeneration != before+1 {
		t.Fatalf("one assignment and one generation step: mode %q generation %d -> %d", r.Mode.Name, before, r.modeGeneration)
	}
	var order []string
	for _, c := range calls {
		order = append(order, c.id)
		if c.from != "normal" || c.to != "visual" || c.live != "visual" || c.generation != before+1 {
			t.Fatalf("observer %s ran before the change was in place: %+v", c.id, c)
		}
	}
	if want := []string{"b", "a", "c"}; !slices.Equal(order, want) {
		t.Fatalf("observer order %v, want %v (new path first, leaving path after, once each)", order, want)
	}

	// A pending mode over the same base and a payload update tell nobody.
	calls = nil
	r.enterMode("count_pending", Payload{Count: 2})
	forceModeCount(r, 23)
	r.leaveMode()
	if len(calls) != 0 {
		t.Fatalf("the base mode never changed, yet observers ran: %+v", calls)
	}
	if r.BaseMode() != "visual" || r.Mode.Name != "visual" {
		t.Fatalf("leave returns to the base: %+v", r.Mode)
	}
}

// Every request advances the mode generation once it is through, even when
// the name and payload stay the same, so runRow sees the handler took
// charge of the mode; SetMode and mode.switch share the request and its
// result, with an input focused or not.
func TestModeGenerationSameNameAndPayload(t *testing.T) {
	r := NewRuntime()
	step := func(what string, do func()) {
		t.Helper()
		before, mode := r.modeGeneration, cloneModeState(r.Mode)
		do()
		if r.modeGeneration == before {
			t.Fatalf("%s: the generation did not advance", what)
		}
		if what != "count.append" && !reflect.DeepEqual(r.Mode, mode) {
			t.Fatalf("%s: the mode changed: %+v -> %+v", what, mode, r.Mode)
		}
	}
	step("SetMode same", func() {
		if err := r.SetMode("normal"); err != nil {
			t.Fatal(err)
		}
	})
	step("mode.switch same", func() {
		if _, err := r.Execute(Do("ochazuke.mode.switch", Arguments{"mode": "normal"}).Request); err != nil {
			t.Fatal(err)
		}
	})
	step("changeMode same value", func() { r.changeMode(r.Mode, modeNamed) })
	r.enterMode("count_pending", Payload{Count: 1})
	step("count.append", func() {
		if _, err := r.Execute(Do("ochazuke.count.append", Arguments{"digit": 4}).Request); err != nil {
			t.Fatal(err)
		}
	})
	if r.Mode.Name != "count_pending" || r.Mode.Payload.Count != 14 {
		t.Fatalf("count.append keeps the mode and grows the count: %+v", r.Mode)
	}
	r.leaveMode()

	// SetMode and mode.switch reach the same state from the same start.
	for _, focused := range []bool{false, true} {
		results := map[string][]any{}
		for _, via := range []string{"SetMode", "mode.switch"} {
			r := NewRuntime()
			in := NewInput("field", "abc", false)
			if err := r.AddInput(in); err != nil {
				t.Fatal(err)
			}
			if focused {
				if err := r.SetFocus("field"); err != nil {
					t.Fatal(err)
				}
			}
			var steps []uint64
			for _, mode := range []string{"insert", "insert", "normal", ModeDeclared, "visual"} {
				before := r.modeGeneration
				var err error
				if via == "SetMode" {
					err = r.SetMode(mode)
				} else {
					_, err = r.Execute(Do("ochazuke.mode.switch", Arguments{"mode": mode}).Request)
				}
				if err != nil {
					t.Fatal(err)
				}
				steps = append(steps, r.modeGeneration-before)
			}
			// A name nobody registered becomes a mode (ensureMode).
			if r.SetMode("no-such") != nil || r.Mode.Name != "no-such" {
				t.Fatalf("SetMode no-such: mode %q", r.Mode.Name)
			}
			if _, err := r.Execute(Do("ochazuke.mode.switch", Arguments{"mode": "no-such"}).Request); err != nil || !r.IsBaseMode("no-such") {
				t.Fatalf("mode.switch no-such: %v", err)
			}
			results[via] = []any{cloneModeState(r.Mode), in.Mode, in.Cursor, in.Anchor, steps}
		}
		if !reflect.DeepEqual(results["SetMode"], results["mode.switch"]) {
			t.Fatalf("focused %v: SetMode %v, mode.switch %v", focused, results["SetMode"], results["mode.switch"])
		}
	}
}

// A payload update (count.append) keeps the armed prefix and its deadline;
// a timer from a prefix that was left cannot finish a newer one.
func TestPendingModeStaleTimer(t *testing.T) {
	r, calls := sequenceFixture(t)
	r.Define("app").Mode("normal").Bind("x x", Do("test.outer")).Bind("y y", Do("test.inner"))
	press(t, r, "x")
	if r.Pending != "x" {
		t.Fatalf("x waits: %q", r.Pending)
	}
	armed := r.prefix
	stale := BindingTimeoutMsg{runtime: r, version: r.bindingVersion, due: true}
	if _, err := r.Execute(Do("ochazuke.count.append", Arguments{"digit": 3}).Request); err != nil {
		t.Fatal(err)
	}
	if r.prefix != armed || r.Pending != "x" || r.Mode.Payload.Prefix != "x" || r.Mode.Payload.Count != 3 {
		t.Fatalf("a payload update reset the prefix: %+v, mode %+v", r.prefix, r.Mode)
	}
	if _, err := r.Execute(Do("ochazuke.mode.leave").Request); err != nil {
		t.Fatal(err)
	}
	press(t, r, "y")
	if r.Pending != "y" {
		t.Fatalf("y waits: %q", r.Pending)
	}
	if ran, err := r.HandleBindingTimeout(stale); ran || err != nil {
		t.Fatalf("the left prefix's timer acted on the newer one: ran %v err %v", ran, err)
	}
	if r.Pending != "y" || r.Mode.Payload.Prefix != "y" || len(*calls) != 0 {
		t.Fatalf("the newer prefix was consumed: pending %q mode %+v calls %v", r.Pending, r.Mode, *calls)
	}
	fresh := BindingTimeoutMsg{runtime: r, version: r.bindingVersion, due: true}
	if ran, err := r.HandleBindingTimeout(fresh); !ran || err != nil {
		t.Fatalf("the newer prefix's own timer: ran %v err %v", ran, err)
	}
	if r.Pending != "" || r.pending() || r.Mode.Name != "normal" {
		t.Fatalf("the timeout leaves the prefix: pending %q mode %+v", r.Pending, r.Mode)
	}
}
