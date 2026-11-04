package ochazuke

import "testing"

func TestKeyTextResultsPendingAndLate(t *testing.T) {
	for _, change := range []string{"none", "focus", "modal", "off", "callback"} {
		t.Run(change, func(t *testing.T) {
			r, _ := textResultFixture(t)
			r.Define("record").Mode("normal").Bind("v", Do("record.later"))
			if change == "callback" {
				op := r.Operations["record.later"]
				run := op.App
				op.App = func(c Ctx) Result {
					return run(c).Then(func(c Ctx, _ any, _ error) { c.Runtime.OpenTextView("callback", "newer") })
				}
				r.Operations[op.Key()] = op
			}
			if _, err := r.Dispatch(MacroKey{Key: "v"}); err != nil {
				t.Fatal(err)
			}
			pending := r.PendingCommands()
			switch change {
			case "focus":
				r.Instance("other", Kind("record"))
				r.SetFocus("other")
			case "modal":
				r.OpenTextView("newer", "keep me")
			case "off":
				r.Settings().Set(KeyTextResultsSetting, false)
			}
			for _, msg := range runCmd(pending) {
				if _, err := r.Deliver(msg); err != nil {
					t.Fatal(err)
				}
			}
			switch change {
			case "none":
				if in := r.Input(textViewInputID); in == nil || string(in.Text) != "one\ntwo\nthree" {
					t.Fatalf("text result missing: %s", r.TopModal())
				}
			case "modal", "callback":
				want := "keep me"
				if change == "callback" {
					want = "newer"
				}
				if in := r.Input(textViewInputID); in == nil || string(in.Text) != want {
					t.Fatal("late result replaced newer interaction")
				}
			default:
				if r.TopModal() != "" {
					t.Fatal("late result opened a modal")
				}
			}
		})
	}
}

func TestKeyTextResultsAllRequiresDeclaredText(t *testing.T) {
	for _, answer := range []any{"ordinary answer", map[string]any{"answer": "structured"}} {
		r, _ := textResultFixture(t)
		if err := r.Settings().Set(PaletteTextResultsSetting, "all"); err != nil {
			t.Fatal(err)
		}
		r.Define("record").Verb("ordinary", func(Ctx) Result { return Done(answer) }).
			Mode("normal").Bind("v", Do("record.ordinary"))
		if _, err := r.Dispatch(MacroKey{Key: "v"}); err != nil {
			t.Fatal(err)
		}
		if r.TopModal() != "" || r.Focus != "origin" {
			t.Fatalf("ordinary key answer opened modal %q, focus %q", r.TopModal(), r.Focus)
		}
	}
}
