package ochazuke

import "testing"

func escFixture(t *testing.T) *Runtime {
	t.Helper()
	r := NewRuntime()
	r.EnsureConfigSettings()
	return r
}

// With modal.esc-closes off (the default), esc in a modal's text input
// leaves insert for the modal's normal mode; the modal stays open, esc in
// normal does nothing, and q closes it.
func TestEscLeavesInsertAndQClosesAModalPrompt(t *testing.T) {
	t.Parallel()
	r := escFixture(t)
	if _, err := r.OpenModal("ask", Payload{Prompt: "Name"}, WithInput(NewInput("ask-line", "x", false), Kind("prompt"))); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "insert" {
		t.Fatalf("opens in %q", r.Mode.Name)
	}
	press(t, r, "esc")
	if r.TopModal() != "ask" || r.Mode.Name != "normal" {
		t.Fatalf("esc: modal %q mode %q", r.TopModal(), r.Mode.Name)
	}
	press(t, r, "esc")
	if r.TopModal() != "ask" || r.Mode.Name != "normal" {
		t.Fatalf("esc in normal: modal %q mode %q", r.TopModal(), r.Mode.Name)
	}
	press(t, r, "q")
	if r.TopModal() != "" {
		t.Fatalf("q left the modal %q open", r.TopModal())
	}
}

// esc does nothing in a modal that has no text input; q closes it.
// The palette's line follows the same two keys.
func TestEscLeavesInsertAndQClosesThePalette(t *testing.T) {
	t.Parallel()
	r := escFixture(t)
	NewPalette(r)
	if _, err := r.Execute(Request{Noun: "ochazuke.palette", Verb: "open"}); err != nil {
		t.Fatal(err)
	}
	press(t, r, "esc")
	if r.TopModal() != paletteID || r.Mode.Name != "normal" {
		t.Fatalf("esc: modal %q mode %q", r.TopModal(), r.Mode.Name)
	}
	press(t, r, "q")
	if r.TopModal() != "" {
		t.Fatalf("q left the palette open")
	}
}

// q in the palette's normal mode closes it at once, even with a typed line.
func TestQClosesThePaletteAtOnceWithATypedLine(t *testing.T) {
	t.Parallel()
	r := escFixture(t)
	NewPalette(r)
	if _, err := r.Execute(Request{Noun: "ochazuke.palette", Verb: "open"}); err != nil {
		t.Fatal(err)
	}
	press(t, r, "a", "b", "esc", "q")
	if r.TopModal() != "" {
		t.Fatalf("q left the palette open")
	}
}

func TestEscDoesNothingInANormalModeModal(t *testing.T) {
	t.Parallel()
	r := escFixture(t)
	if _, err := r.OpenModal("view", Payload{}); err != nil {
		t.Fatal(err)
	}
	press(t, r, "esc")
	if r.TopModal() != "view" {
		t.Fatalf("esc closed the modal")
	}
	press(t, r, "q")
	if r.TopModal() != "" {
		t.Fatalf("q left the modal open")
	}
}

// modal.esc-closes on brings the old behaviour back: a text input stays
// pinned to insert and esc closes the modal.
func TestModalEscClosesSettingRestoresEsc(t *testing.T) {
	t.Parallel()
	r := escFixture(t)
	if err := r.Settings().Set(ModalEscClosesSetting, true); err != nil {
		t.Fatal(err)
	}
	NewPalette(r)
	if _, err := r.Execute(Request{Noun: "ochazuke.palette", Verb: "open"}); err != nil {
		t.Fatal(err)
	}
	press(t, r, "esc")
	if r.TopModal() != "" {
		t.Fatalf("esc left the palette %q open with modal.esc-closes on", r.TopModal())
	}
	if _, err := r.OpenModal("view", Payload{}); err != nil {
		t.Fatal(err)
	}
	press(t, r, "esc")
	if r.TopModal() != "" {
		t.Fatalf("esc left the plain modal open with modal.esc-closes on")
	}
}

// "setting:<name>" is a row guard that holds while that boolean setting is on.
func TestSettingGuardFollowsTheSetting(t *testing.T) {
	t.Parallel()
	r := escFixture(t)
	g := r.guard("setting:" + ModalEscClosesSetting)
	if g == nil {
		t.Fatal("no setting guard")
	}
	if g(r, KeyEvent{}) {
		t.Fatal("holds with the setting off")
	}
	if err := r.Settings().Set(ModalEscClosesSetting, true); err != nil {
		t.Fatal(err)
	}
	if !g(r, KeyEvent{}) {
		t.Fatal("does not hold with the setting on")
	}
}
