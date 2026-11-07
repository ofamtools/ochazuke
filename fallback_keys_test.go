package ochazuke_test

import (
	"slices"
	"testing"

	o "ofam.dev/ochazuke"
)

// viewer is an application's own component, no Input: it declares through
// KeyOwner whether it owns its fallback table's keys.
type viewer struct{ owns bool }

func (v *viewer) OwnsKeys() bool { return v.owns }

// keymapFixture is a custom viewer under a container: the viewer's noun
// names a keymap noun as its fallback (NounBuilder.Fallback), the keymap
// binds m (available), x (guarded off) and w (bound nowhere else), and the
// container binds x, m and R.
func keymapFixture(t *testing.T, owns bool) (*o.Runtime, *[]string) {
	t.Helper()
	r := o.NewRuntime()
	r.Config().Disable()
	var calls []string
	record := func(name string) o.AppHandler {
		return func(o.Ctx) o.Result { calls = append(calls, name); return o.Done(nil) }
	}
	r.DefineGuard("can-change-document", func(*o.Runtime, o.KeyEvent) bool { return false })
	r.Define("document-keymap").Verb("step", record("keymap.step"), "Move the custom cursor")
	r.Define("document-keymap").Verb("change", record("keymap.change"), "Change the document").When("can-change-document")
	r.Define("document-keymap").Verb("word", record("keymap.word"), "Next word")
	r.Define("document-keymap").Mode("normal").
		Bind("m", o.Do("document-keymap.step")).
		Bind("x", o.Do("document-keymap.change")).
		Bind("w", o.Do("document-keymap.word"))
	r.Define("outer").Verb("change", record("outer.change"), "The container's action")
	r.Define("outer").Mode("normal").Bind("x", o.Do("outer.change")).Bind("m", o.Do("outer.change")).Bind("R", o.Do("outer.change"))
	r.Instance("container", o.Kind("outer"))
	r.Define("custom-viewer").Fallback("document-keymap")
	view := r.Instance("view", o.Parent("container"), o.Kind("custom-viewer"))
	view.Component = &viewer{owns: owns}
	if err := r.SetFocus("view"); err != nil {
		t.Fatal(err)
	}
	if r.Input("view") != nil {
		t.Fatal("the viewer became an Input")
	}
	if _, err := r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
	return r, &calls
}

// A custom component owns its keys through the same rule a read-only input
// does: its keymap answers right after it, before its container, a keymap
// key that does not run there is consumed (not passed to the container),
// and a key the keymap does not bind still bubbles.
func TestCustomComponentOwnsItsKeys(t *testing.T) {
	r, calls := keymapFixture(t, true)
	for _, row := range r.ListBindings("view") {
		if row.Keys == "x" || row.Keys == "m" && row.Request.Noun == "outer" {
			t.Fatalf("a container row leaked through the owning keymap: %+v", row)
		}
	}
	for _, row := range o.BindingHelp(r, "view") {
		if row.Key == "x" {
			t.Fatalf("an unavailable owned key offered in help: %+v", row)
		}
	}
	if outcome, err := r.DispatchOutcome(o.MacroKey{Key: "x"}); err != nil || outcome != o.KeyConsumed {
		t.Fatalf("x: %v %v", outcome, err)
	}
	for _, key := range []string{"m", "w", "R"} {
		if outcome, err := r.DispatchOutcome(o.MacroKey{Key: key}); err != nil || outcome != o.KeyHandled {
			t.Fatalf("%s: %v %v", key, outcome, err)
		}
	}
	if want := []string{"keymap.step", "keymap.word", "outer.change"}; !slices.Equal(*calls, want) {
		t.Fatalf("calls %v, want %v", *calls, want)
	}
	// A modal cuts the viewer off with its container.
	r.Define("dialog-body")
	if _, err := r.OpenModal("dialog-body", o.Payload{}); err != nil {
		t.Fatal(err)
	}
	if outcome, err := r.DispatchOutcome(o.MacroKey{Key: "x"}); err != nil || outcome != o.KeyUnhandled {
		t.Fatalf("x in a modal: %v %v", outcome, err)
	}
}

// Without ownership the keymap is the walk's fallback, read after app:
// the container's rows answer first, and the keymap answers only the keys
// nothing above it binds.
func TestCustomComponentFallbackAnswersLast(t *testing.T) {
	r, calls := keymapFixture(t, false)
	for _, key := range []string{"x", "m", "w"} {
		if outcome, err := r.DispatchOutcome(o.MacroKey{Key: key}); err != nil || outcome != o.KeyHandled {
			t.Fatalf("%s: %v %v", key, outcome, err)
		}
	}
	if want := []string{"outer.change", "outer.change", "keymap.word"}; !slices.Equal(*calls, want) {
		t.Fatalf("calls %v, want %v", *calls, want)
	}
	// Ownership is read live: the same viewer owning its keys now.
	r.Instances["view"].Component.(*viewer).owns = true
	*calls = nil
	if _, err := r.DispatchOutcome(o.MacroKey{Key: "m"}); err != nil || !slices.Equal(*calls, []string{"keymap.step"}) {
		t.Fatalf("m once owned: %v %v", *calls, err)
	}
}

// The editor noun declares itself its fallback (Noun.Fallback), so a focus
// on it, live or hypothetical (the help's pick view), reads the editor's
// rows after app and not at its own level.
func TestEditorNounIsItsOwnFallback(t *testing.T) {
	r := o.NewRuntime()
	o.NewModel(r)
	if n := r.Noun(o.EditorNoun); n == nil || n.Fallback != o.EditorNoun {
		t.Fatalf("editor noun: %+v", n)
	}
	rows := o.NounHelp(r, o.EditorNoun, o.ModeState{Name: "normal"})
	if !slices.ContainsFunc(rows, func(h o.HelpRow) bool { return h.Noun == o.EditorNoun && h.Key == "x" }) {
		t.Fatal("no editor rows in the editor noun's help")
	}
}
