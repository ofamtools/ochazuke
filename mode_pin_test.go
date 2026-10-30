package ochazuke

import "testing"

func TestFocusMovedInstancePin(t *testing.T) {
	r, _, _ := jumpFixture(t)
	for _, m := range []string{"table", "flat", "edit"} {
		if err := r.DefineMode(m, false, ModeExtends("normal")); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.PinInstance("items", "table"); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "normal" {
		t.Fatalf("pin away from the focus changed the mode: %q", r.Mode.Name)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(r.SetFocus("i1"))
	if r.Mode.Name != "table" {
		t.Fatalf("focus on a pinned list's row: mode %q, want table", r.Mode.Name)
	}
	// A lifted mode survives moves inside the stop.
	must(r.SetMode("edit"))
	must(r.SetFocus("i2"))
	if r.Mode.Name != "edit" {
		t.Fatalf("a move inside the stop left the lifted mode: %q", r.Mode.Name)
	}
	// mode.set declared lands on the pin.
	if _, err := r.Execute(Request{Verb: "set", Noun: "ochazuke.mode", Args: Arguments{"mode": ModeDeclared}}); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "table" {
		t.Fatalf("declared: mode %q, want table", r.Mode.Name)
	}
	// A re-pin applies at once while standing in the declared mode.
	must(r.PinInstance("items", "flat"))
	if r.Mode.Name != "flat" {
		t.Fatalf("re-pin: mode %q, want flat", r.Mode.Name)
	}
	// ...but not over a lifted mode; declared then reads the new pin.
	must(r.SetMode("edit"))
	must(r.PinInstance("items", "table"))
	if r.Mode.Name != "edit" {
		t.Fatalf("re-pin over a lifted mode changed it: %q", r.Mode.Name)
	}
	must(r.SetMode(ModeDeclared))
	if r.Mode.Name != "table" {
		t.Fatalf("declared after re-pin: %q", r.Mode.Name)
	}
	// Leaving for an unpinned stop goes back to normal; declared there is normal.
	must(r.SetFocus("s1"))
	if r.Mode.Name != "normal" || r.DeclaredMode("s1") != "normal" {
		t.Fatalf("unpinned stop: mode %q declared %q", r.Mode.Name, r.DeclaredMode("s1"))
	}
	// An ancestor's pin covers every stop beneath it.
	must(r.PinInstance("left", "flat"))
	if r.Mode.Name != "flat" {
		t.Fatalf("pin on the focus's ancestor: mode %q, want flat", r.Mode.Name)
	}
	must(r.PinInstance("left", ""))
	if r.Mode.Name != "normal" {
		t.Fatalf("unpin: mode %q, want normal", r.Mode.Name)
	}
	if err := r.PinInstance("items", "nope"); err == nil {
		t.Fatal("pin to an unknown mode was accepted")
	}
	if err := r.DefineMode(ModeDeclared, false); err == nil {
		t.Fatal("the reserved mode name was accepted")
	}
}

func TestModePinVerbDefaultsToTheFocusedStop(t *testing.T) {
	r, _, _ := jumpFixture(t)
	if err := r.DefineMode("shape", false, ModeExtends("normal")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Verb: "pin", Noun: "ochazuke.mode", Args: Arguments{"mode": "shape"}}); err != nil {
		t.Fatal(err)
	}
	if r.Instances["sidebar"].Pin != "shape" || r.Mode.Name != "shape" {
		t.Fatalf("pin verb: sidebar pin %q mode %q", r.Instances["sidebar"].Pin, r.Mode.Name)
	}
}

func TestInstancePinUnderAModalFollowsOnClose(t *testing.T) {
	r, _, _ := jumpFixture(t)
	if err := r.DefineMode("shape", false, ModeExtends("normal")); err != nil {
		t.Fatal(err)
	}
	if err := r.PinInstance("sidebar", "shape"); err != nil {
		t.Fatal(err)
	}
	r.Define("dlg")
	if _, err := r.OpenModal("dlg", Payload{}); err != nil {
		t.Fatal(err)
	}
	if err := r.PinInstance("sidebar", ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "normal" {
		t.Fatalf("closing the modal after an unpin: mode %q, want normal", r.Mode.Name)
	}
}
