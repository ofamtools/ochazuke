package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestKeyTextResultsPendingAndLate(t *testing.T) {
	for _, change := range []string{"none", "focus", "modal", "off", "callback"} {
		t.Run(change, func(t *testing.T) {
			r, _ := textResultFixture(t)
			r.Define("record").Mode("normal").Bind("v", Do("record.later"))
			if change == "callback" {
				op := r.Operations.At("record.later")
				run := op.AppHandler
				op.AppHandler = func(c Ctx) Result {
					return run(c).Then(func(c Ctx, _ any, _ error) { c.Runtime.OpenTextView("callback", "newer") })
				}
				r.Operations.Set(op)
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

// An agent whose right is allow passes a verb's confirm prompt, and the key
// that ran it still presents the declared text answer.
func TestPromptedAllowKeyStillPresentsText(t *testing.T) {
	r, _ := textResultFixture(t)
	r.Define("record").
		Verb("peek", func(Ctx) Result { return Done("one\ntwo") }, "a prompted text").Confirm("Peek?").TextResult().Agent(AgentAllow)
	r.Define("record").Mode("normal").Bind("p", Do("record.peek"))
	restore := r.BindActor(testAgent("p"), false)
	defer restore()
	if _, err := r.Dispatch(MacroKey{Key: "p"}); err != nil {
		t.Fatal(err)
	}
	if in := r.Input(textViewInputID); in == nil || string(in.Text) != "one\ntwo" {
		t.Fatalf("text result missing: top modal %q", r.TopModal())
	}
}

// A binding row with present:false keeps its key's declared text answer out
// of the viewer; the same verb on another key still opens it.
func TestBindingPresentFalseKeepsAnswerOutOfViewer(t *testing.T) {
	r, _ := textResultFixture(t)
	r.Define("record").Mode("normal").Bind("w", Do("record.program").Unpresented()).Bind("x", Do("record.program"))
	if _, err := r.Dispatch(MacroKey{Key: "w"}); err != nil {
		t.Fatal(err)
	}
	if r.Input(textViewInputID) != nil {
		t.Fatal("present:false opened the viewer")
	}
	if _, err := r.Dispatch(MacroKey{Key: "x"}); err != nil {
		t.Fatal(err)
	}
	if r.Input(textViewInputID) == nil {
		t.Fatal("a row without the field must present")
	}
	doc, _, err := ParseBindings([]byte(`{"bindings":[{"context":"record/normal","bindings":[{"key":"w","action":"record.program","present":false}]}]}`))
	if err != nil || len(doc.Rows()) != 1 || doc.Rows()[0].Present == nil || *doc.Rows()[0].Present {
		t.Fatalf("present:false not read from a bindings file: %+v %v", doc.Rows(), err)
	}
	out, err := FormatBindings(doc)
	if err != nil || !strings.Contains(string(out), `"present": false`) {
		t.Fatalf("present lost in export: %s %v", out, err)
	}
}

// A verb that moves the focus itself and answers at once is not a late
// answer for a changed interaction: no "answer not opened" notice.
func TestSyncAnswerAfterVerbMovedFocusIsNotReportedStale(t *testing.T) {
	r, m := textResultFixture(t)
	r.Instance("other", Kind("record"))
	r.Define("record").
		Verb("jump", func(c Ctx) Result { c.Runtime.SetFocus("other"); return Done("moved") }, "moves then answers").TextResult()
	r.Define("record").Mode("normal").Bind("j", Do("record.jump"))
	if _, err := r.Dispatch(MacroKey{Key: "j"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ansi.Strip(m.View().Content), "answer not opened") {
		t.Fatal("a synchronous answer after the verb moved the focus was called stale")
	}
}
