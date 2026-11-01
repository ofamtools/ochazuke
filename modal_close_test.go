package ochazuke

import (
	"strings"
	"testing"
)

// modal.close runs the top modal's close request (CloseWith), else pops it;
// esc and q on the modal noun answer with it; modal.pop is its old name.
func TestModalCloseRunsTheCloseRequestElsePops(t *testing.T) {
	r := NewRuntime()
	closed := 0
	r.Register(Operation{Verb: "dismiss", Noun: "dialog", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		closed++
		return nil, r.Pop()
	}})
	r.Define("dialog")
	if _, err := r.OpenModal("dialog", Payload{}, CloseWith(Request{Noun: "dialog", Verb: "dismiss"})); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(MacroKey{Key: "esc"}); err != nil {
		t.Fatal(err)
	}
	if closed != 1 || r.TopModal() != "" {
		t.Fatalf("esc: close request ran %d times, top modal %q", closed, r.TopModal())
	}
	// q answers the same verb in normal mode.
	if _, err := r.OpenModal("dialog", Payload{}, CloseWith(Request{Noun: "dialog", Verb: "dismiss"})); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(MacroKey{Key: "q"}); err != nil {
		t.Fatal(err)
	}
	if closed != 2 || r.TopModal() != "" || r.Registers().Macro.Recording != "" || r.Mode.Name != "normal" {
		t.Fatalf("q: close request ran %d times, top modal %q, mode %q", closed, r.TopModal(), r.Mode.Name)
	}
	// Without a close request modal.close pops; the old name still runs it.
	if err := r.Push("plain", "app"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.modal", Verb: "pop"}); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != "" {
		t.Fatalf("modal.pop (alias) left %q open", r.TopModal())
	}
	if a, ok := r.ActionAlias("ochazuke.modal.pop"); !ok || a.Action != "ochazuke.modal.close" || !a.Deprecated() {
		t.Fatalf("modal.pop alias: %+v %v", a, ok)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.modal", Verb: "close"}); err == nil || !strings.Contains(err.Error(), "no modal") {
		t.Fatalf("modal.close with no modal: %v", err)
	}
}

// A close request that closes through modal.close again pops instead of
// recurring; SetModalClose with a zero request clears it.
func TestModalCloseRequestReentryPops(t *testing.T) {
	r := NewRuntime()
	if err := r.Push("loop", "app"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetModalClose("loop", Request{Noun: "ochazuke.modal", Verb: "close"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.modal", Verb: "close"}); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != "" {
		t.Fatalf("re-entrant close left %q open", r.TopModal())
	}
	if err := r.Push("loop", "app"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetModalClose("loop", Request{}); err != nil {
		t.Fatal(err)
	}
	if r.Instances["loop"].close != nil {
		t.Fatal("a zero request must clear the close request")
	}
	if err := r.SetModalClose("nope", Request{Noun: "x", Verb: "y"}); err == nil {
		t.Fatal("an unknown instance must be refused")
	}
}

// The palette closes through modal.close with its own close request: esc
// clears a typed line first and closes only an empty one.
func TestPaletteEscClearsThenCloses(t *testing.T) {
	r, p, _ := lineRuntime(t)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteKeys(t, p, "s", "a")
	paletteKeys(t, p, "esc")
	if !p.IsOpen() || p.Text() != "" {
		t.Fatalf("first esc: open %v line %q", p.IsOpen(), p.Text())
	}
	paletteKeys(t, p, "esc")
	if p.IsOpen() || r.TopModal() != "" {
		t.Fatalf("second esc: open %v top %q", p.IsOpen(), r.TopModal())
	}
}
