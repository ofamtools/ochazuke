package ochazuke

import "testing"

// mode.create registers an application-defined mode name (it was
// mode.register): the standard create over the modes the registry holds.
func TestModeCreateRegistersAMode(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	if _, err := r.Execute(Request{Noun: "ochazuke.mode", Verb: "create", Args: Arguments{"name": "review", "pending": true}}); err != nil {
		t.Fatal(err)
	}
	if !r.modeDefined("review") || !r.modes["review"].Pending {
		t.Fatalf("mode review not registered pending: %+v", r.modes["review"])
	}
	if _, ok := r.Operation("ochazuke.mode.register"); ok {
		t.Fatal("mode.register still runs")
	}
}
