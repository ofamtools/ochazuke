package ochazuke

import (
	"strings"
	"testing"
)

// BeforeQuit: with something to lose, app.quit asks in the confirm modal
// (n leaves the app running, y quits); force quits without asking; with
// nothing to lose it quits at once. The check runs from a key too.
func TestBeforeQuitAsksThenQuits(t *testing.T) {
	r := NewRuntime()
	dirty := true
	if err := r.BeforeQuit("unsaved", func(*Runtime, Context) Verdict {
		if dirty {
			return AskFirst("Quit without writing?")
		}
		return Allow()
	}); err != nil {
		t.Fatal(err)
	}
	if got := r.VerbChecks("app.quit"); len(got) != 1 || got[0] != "unsaved" {
		t.Fatalf("checks %v", got)
	}
	v, err := r.Execute(Request{Noun: "app", Verb: "quit"})
	if err != nil || r.Quit {
		t.Fatalf("quit ran without asking: %v %v", v, err)
	}
	top := r.Instances[r.TopModal()]
	if top == nil || top.Noun != "ochazuke.confirm" || top.Payload.Prompt != "Quit without writing?" {
		t.Fatalf("no confirm modal: %+v", top)
	}
	if err := r.Pop(); err != nil || r.Quit {
		t.Fatalf("n must leave the app running: %v", err)
	}
	if _, err := r.Execute(Request{Noun: "app", Verb: "quit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.confirm", Verb: "accept"}); err != nil || !r.Quit {
		t.Fatalf("y must quit: %v quit=%v", err, r.Quit)
	}
	r.Quit = false
	if _, err := r.Execute(Request{Noun: "app", Verb: "quit", Args: Arguments{"force": true}}); err != nil || !r.Quit || r.TopModal() != "" {
		t.Fatalf("force must quit without asking: %v quit=%v modal=%q", err, r.Quit, r.TopModal())
	}
	r.Quit, dirty = false, false
	if _, err := r.Execute(Request{Noun: "app", Verb: "quit"}); err != nil || !r.Quit {
		t.Fatalf("nothing to lose quits at once: %v", err)
	}
}

// A refusing check fails the request with its reason; the first check that
// does not allow decides; a check of the same name replaces the first;
// RemoveVerbCheck takes it off.
func TestVerbCheckRefusesInOrderAndReplaces(t *testing.T) {
	r := NewRuntime()
	ran := 0
	r.Define("doc").Verb("write", func(Ctx) Result { ran++; return Done(nil) })
	allow := VerbCheck{Name: "a", Check: func(*Runtime, Context, Arguments) Verdict { return Allow() }}
	refuse := VerbCheck{Name: "b", Check: func(*Runtime, Context, Arguments) Verdict { return Refuse("the disk is read-only") }}
	ask := VerbCheck{Name: "c", Check: func(*Runtime, Context, Arguments) Verdict { return AskFirst("sure?") }}
	for _, c := range []VerbCheck{allow, refuse, ask} {
		if err := r.AddVerbCheck("doc.write", c); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Execute(Request{Noun: "doc", Verb: "write"}); err == nil || !strings.Contains(err.Error(), "read-only") || ran != 0 {
		t.Fatalf("refuse: %v ran=%d", err, ran)
	}
	if err := r.AddVerbCheck("doc.write", VerbCheck{Name: "b", Check: allow.Check}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.VerbChecks("doc.write"), ","); got != "a,b,c" {
		t.Fatalf("replace keeps the order: %s", got)
	}
	if _, err := r.Execute(Request{Noun: "doc", Verb: "write"}); err != nil || ran != 0 || r.TopModal() == "" {
		t.Fatalf("ask: %v ran=%d", err, ran)
	}
	_ = r.Pop()
	if !r.RemoveVerbCheck("doc.write", "c") || r.RemoveVerbCheck("doc.write", "c") {
		t.Fatal("remove once")
	}
	if _, err := r.Execute(Request{Noun: "doc", Verb: "write"}); err != nil || ran != 1 {
		t.Fatalf("all allow: %v ran=%d", err, ran)
	}
	if err := r.AddVerbCheck("doc.nothing", allow); err == nil {
		t.Fatal("unknown verb must be refused")
	}
	if err := r.AddVerbCheck("doc.write", VerbCheck{Name: "x"}); err == nil {
		t.Fatal("a check needs a func")
	}
}

// The quit check reaches a key: ctrl+c through the Model's capture row asks.
func TestBeforeQuitFromKey(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 80, 20
	_ = r.BeforeQuit("unsaved", func(*Runtime, Context) Verdict { return AskFirst("Lose edits?") })
	pressKeys(m, "ctrl+c")
	if r.Quit || r.Instances[r.TopModal()] == nil {
		t.Fatalf("ctrl+c must ask: quit=%v modal=%q", r.Quit, r.TopModal())
	}
	pressKeys(m, "y")
	if !r.Quit {
		t.Fatal("y quits")
	}
}
