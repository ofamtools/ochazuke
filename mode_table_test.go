package ochazuke

import (
	"slices"
	"testing"
)

// The mode table holds every mode's traits: the built-in modes declare
// theirs as data, the default files and DefineMode add the rest, and the
// exported Modes view lists the base modes.
func TestModeTableTraits(t *testing.T) {
	r := NewRuntime()
	for name, want := range map[string]ModeSpec{
		"normal":           {CursorOnChar: true},
		"insert":           {Typing: true},
		"visual":           {Selection: SelectionChar},
		"visual-line":      {Selection: SelectionLine, Extends: "normal"},
		"count_pending":    {Pending: true},
		"operator_pending": {Pending: true},
	} {
		got, ok := r.ModeTraits(name)
		if !ok || got != want {
			t.Errorf("ModeTraits(%q) = %+v, %v; want %+v, true", name, got, ok, want)
		}
		if r.IsBaseMode(name) == want.Pending || r.IsPendingMode(name) != want.Pending || r.Modes[name] == want.Pending {
			t.Errorf("%q: base %v, pending %v, Modes %v; want pending %v", name, r.IsBaseMode(name), r.IsPendingMode(name), r.Modes[name], want.Pending)
		}
	}
	if _, ok := r.ModeTraits("nope"); ok || r.modeDefined("nope") || r.IsBaseMode("nope") {
		t.Fatal("an undefined mode reads as defined")
	}

	// A defined pending mode and a passthrough base mode read back.
	if err := r.DefineMode("shortcut", true, ModeOutside("normal")); err != nil {
		t.Fatal(err)
	}
	if err := r.DefineMode("captured", false, ModeExtends("insert")); err != nil {
		t.Fatal(err)
	}
	if err := r.DefinePassthrough("captured"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ModeTraits("shortcut"); got != (ModeSpec{Pending: true, Outside: "normal"}) {
		t.Fatalf("shortcut = %+v", got)
	}
	if got, _ := r.ModeTraits("captured"); got != (ModeSpec{Passthrough: true, Extends: "insert"}) {
		t.Fatalf("captured = %+v", got)
	}
	if !r.IsPendingMode("shortcut") || r.Modes["shortcut"] || !r.Modes["captured"] || !r.IsPassthrough("captured") {
		t.Fatal("the Modes view or the trait queries disagree with the table")
	}
	if !slices.Contains(r.PendingModes(), "shortcut") || slices.Contains(r.PendingModes(), "captured") {
		t.Fatalf("PendingModes = %v", r.PendingModes())
	}
	// Typing follows Extends; the own trait does not.
	if !typingMode(r, "captured") || r.modes["captured"].Typing || typingMode(r, "normal") {
		t.Fatal("typing does not follow the extends chain")
	}
	if !charCursorMode(r, "visual-line") || charCursorMode(r, "insert") {
		t.Fatal("the char cursor does not follow the extends chain")
	}
	// Redefining a mode pending keeps its other traits.
	if err := r.DefineMode("captured", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ModeTraits("captured"); got != (ModeSpec{Pending: true, Passthrough: true, Extends: "insert"}) || r.Modes["captured"] {
		t.Fatalf("redefined captured = %+v, Modes %v", got, r.Modes["captured"])
	}
	// An application declares a trait with a ModeOption.
	if err := r.DefineMode("query", false, func(s *ModeSpec) { s.Typing = true }); err != nil {
		t.Fatal(err)
	}
	if !typingMode(r, "query") {
		t.Fatal("a typing ModeOption is not read")
	}
	// The traits a bindings file does not carry stay out of its "modes".
	if _, ok := r.bindingModes()["normal"]; ok {
		t.Fatal("normal, with no file trait, is listed in a bindings file's modes")
	}
	// No runtime: the built-in modes' traits.
	if !modeSpecOf(nil, "normal").CursorOnChar || !typingMode(nil, "insert") || typingMode(nil, "normal") {
		t.Fatal("the built-in traits without a runtime")
	}
}
