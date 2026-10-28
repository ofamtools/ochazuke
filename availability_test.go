package ochazuke

import (
	"slices"
	"testing"
)

// availabilityFixture: page > box > item (focused, noun "item"); three
// actions on the item noun, "a" available while *aOn, "b" while *bOn, "c"
// always; ran records what ran.
func availabilityFixture(t *testing.T) (r *Runtime, aOn, bOn *bool, ran *[]string) {
	t.Helper()
	r = NewRuntime()
	r.Config().Disable()
	aOn, bOn, ran = new(bool), new(bool), new([]string)
	r.DefineGuard("a-on", func(*Runtime, KeyEvent) bool { return *aOn })
	r.DefineGuard("b-on", func(*Runtime, KeyEvent) bool { return *bOn })
	for _, verb := range []string{"a", "b", "c", "outer"} {
		r.Register(Operation{Verb: verb, Noun: "item", Run: func(*Runtime, Context, Arguments) (any, error) {
			*ran = append(*ran, verb)
			return nil, nil
		}})
	}
	r.SetAvailability("item.a", "a-on")
	r.SetAvailability("item.b", "b-on")
	r.Instance("page")
	r.Instance("box", Kind("box"), Parent("page"))
	r.Instance("item", Kind("item"), Parent("box"))
	if err := r.SetFocus("item"); err != nil {
		t.Fatal(err)
	}
	return r, aOn, bOn, ran
}

func pressOutcome(t *testing.T, r *Runtime, key string) KeyOutcome {
	t.Helper()
	outcome, err := r.DispatchOutcome(MacroKey{Key: key})
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return outcome
}

// One key, several rows in one context: the first whose action is
// available runs, in line order.
func TestResolverRunsTheFirstAvailableRow(t *testing.T) {
	r, aOn, bOn, ran := availabilityFixture(t)
	if err := r.Bind(
		Binding{Noun: "item", Mode: "normal", Keys: "x", Request: Request{Noun: "item", Verb: "a"}},
		Binding{Noun: "item", Mode: "normal", Keys: "x", Request: Request{Noun: "item", Verb: "b"}},
		Binding{Noun: "item", Mode: "normal", Keys: "x", Request: Request{Noun: "item", Verb: "c"}},
	); err != nil {
		t.Fatal(err)
	}
	if n := len(r.Nouns["item"].Bindings["normal"]); n != 3 {
		t.Fatalf("one Bind call keeps every row on x as alternatives, got %d rows", n)
	}
	steps := []struct {
		a, b bool
		want string
	}{{true, true, "a"}, {false, true, "b"}, {false, false, "c"}, {true, false, "a"}}
	for _, s := range steps {
		*aOn, *bOn, *ran = s.a, s.b, nil
		if got := pressOutcome(t, r, "x"); got != KeyHandled {
			t.Fatalf("a=%v b=%v: outcome %v", s.a, s.b, got)
		}
		if !slices.Equal(*ran, []string{s.want}) {
			t.Errorf("a=%v b=%v: ran %v, want %s", s.a, s.b, *ran, s.want)
		}
		// The hints and bindings list show the row that would answer.
		rows := r.ListBindings(r.Focus)
		i := slices.IndexFunc(rows, func(b Binding) bool { return b.Keys == "x" })
		if i < 0 || rows[i].Request.Verb != s.want {
			t.Errorf("a=%v b=%v: bindings list shows %+v, want item.%s", s.a, s.b, rows, s.want)
		}
	}
}

// No available row at a level: the key bubbles to the next level out; no
// level at all: unhandled.
func TestResolverBubblesWhenNoneIsAvailable(t *testing.T) {
	r, aOn, bOn, ran := availabilityFixture(t)
	if err := r.Bind(
		Binding{Noun: "item", Mode: "normal", Keys: "x", Request: Request{Noun: "item", Verb: "a"}},
		Binding{Noun: "item", Mode: "normal", Keys: "x", Request: Request{Noun: "item", Verb: "b"}},
	); err != nil {
		t.Fatal(err)
	}
	*aOn, *bOn = false, false
	if got := pressOutcome(t, r, "x"); got != KeyUnhandled || len(*ran) != 0 {
		t.Fatalf("nothing available anywhere: outcome %v, ran %v", got, *ran)
	}
	r.Define("box").Mode("normal").Bind("x", Do("item.outer"))
	if got := pressOutcome(t, r, "x"); got != KeyHandled || !slices.Equal(*ran, []string{"outer"}) {
		t.Fatalf("bubbled to the box: outcome %v, ran %v", got, *ran)
	}
	*aOn, *ran = true, nil
	pressOutcome(t, r, "x")
	if !slices.Equal(*ran, []string{"a"}) {
		t.Fatalf("the inner level answers once its action is available: ran %v", *ran)
	}
}

// A clamping container binds the key to the noop: an inner level with
// nothing available leaves it there, consumed, and nothing outside runs.
func TestResolverConsumedByNoop(t *testing.T) {
	r, aOn, _, ran := availabilityFixture(t)
	r.Define("item").Mode("normal").Bind("j", Do("item.a"))
	r.Define("box").Mode("normal").Bind("j", Do(NoopAction))
	r.Define("page").Mode("normal").Bind("j", Do("item.outer"))
	if got := pressOutcome(t, r, "j"); got != KeyConsumed || len(*ran) != 0 {
		t.Fatalf("clamped: outcome %v, ran %v", got, *ran)
	}
	*aOn = true
	if got := pressOutcome(t, r, "j"); got != KeyHandled || !slices.Equal(*ran, []string{"a"}) {
		t.Fatalf("available inside: outcome %v, ran %v", got, *ran)
	}
	if ok, _ := r.Dispatch(MacroKey{Key: "j"}); !ok {
		t.Fatal("Dispatch reports a consumed key as claimed")
	}
}

// Rows with a guard (the older form) and rows on actions with availability
// mix on one key: each row answers when its own guard (if any) and its
// action's availability both hold, first in table order.
func TestResolverMixesGuardRowsAndAvailability(t *testing.T) {
	r, aOn, bOn, ran := availabilityFixture(t)
	guard := false
	r.DefineGuard("row-guard", func(*Runtime, KeyEvent) bool { return guard })
	if err := r.Bind(
		Binding{Noun: "item", Mode: "normal", Keys: "x", Request: Request{Noun: "item", Verb: "c"}, When: "row-guard"},
		Binding{Noun: "item", Mode: "normal", Keys: "x", Request: Request{Noun: "item", Verb: "a"}},
		Binding{Noun: "item", Mode: "normal", Keys: "x", Request: Request{Noun: "item", Verb: "b"}},
	); err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		guard, a, b bool
		want        string
	}{{true, true, true, "c"}, {false, true, true, "a"}, {false, false, true, "b"}, {true, false, false, "c"}, {false, false, false, ""}} {
		guard, *aOn, *bOn, *ran = s.guard, s.a, s.b, nil
		pressOutcome(t, r, "x")
		want := []string{}
		if s.want != "" {
			want = []string{s.want}
		}
		if !slices.Equal(append([]string{}, *ran...), want) {
			t.Errorf("guard=%v a=%v b=%v: ran %v, want %v", s.guard, s.a, s.b, *ran, want)
		}
	}
}

// Availability reads the request's arguments, so a call naming its target
// is judged by that target; Execute refuses an unavailable call.
func TestAvailabilityReadsTheRequestArguments(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	r.DefineGuard("even", func(_ *Runtime, ev KeyEvent) bool { return IntArg(ev.Args, "n", 0)%2 == 0 })
	r.Register(Operation{Verb: "halve", Noun: "num", Parameters: map[string]Parameter{"n": {Type: "integer"}}, Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
	r.SetAvailability("num.halve", "even")
	if !r.Available(Request{Noun: "num", Verb: "halve", Args: Arguments{"n": 4}}, "app", r.Mode) {
		t.Fatal("4 is even")
	}
	if _, err := r.Execute(Request{Noun: "num", Verb: "halve", Args: Arguments{"n": 3}}); err == nil {
		t.Fatal("an unavailable call is refused")
	}
	if _, err := r.Execute(Request{Noun: "num", Verb: "halve", Args: Arguments{"n": 2}}); err != nil {
		t.Fatal(err)
	}
	r.SetAvailability("num.halve", "")
	if _, err := r.Execute(Request{Noun: "num", Verb: "halve", Args: Arguments{"n": 3}}); err != nil {
		t.Fatalf("always available again: %v", err)
	}
}

// A later Bind call (or a later file) on the same key and guard replaces
// every row the earlier one put there: alternatives are one batch's.
func TestBindReplacesAnEarlierBatchOnTheKey(t *testing.T) {
	r, _, _, _ := availabilityFixture(t)
	if err := r.Bind(
		Binding{Noun: "item", Mode: "normal", Keys: "x", Request: Request{Noun: "item", Verb: "a"}},
		Binding{Noun: "item", Mode: "normal", Keys: "x", Request: Request{Noun: "item", Verb: "b"}},
	); err != nil {
		t.Fatal(err)
	}
	if err := r.Bind(Binding{Noun: "item", Mode: "normal", Keys: "x", Request: Request{Noun: "item", Verb: "c"}}); err != nil {
		t.Fatal(err)
	}
	rows := r.Nouns["item"].Bindings["normal"]
	if len(rows) != 1 || rows[0].Request.Verb != "c" {
		t.Fatalf("rows %+v, want x → item.c alone", rows)
	}
}
