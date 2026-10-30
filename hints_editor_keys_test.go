package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// An input's hint line names the keys of what holds it (a modal's own
// rows), never the editor's insert-mode keys (ctrl+k input.kill, ctrl+y
// input.paste, backspace input.delete, tab completion.open, esc
// mode.set) or the palette key (ctrl+space palette.open): they are hidden
// rows, as motions are, and bindings list and ? help still list them
// (found by o-json's edit modal).
func TestInputHintsLeaveOutEditorInsertKeys(t *testing.T) {
	r, _ := focusFixture(t)
	NewModel(r)
	r.Define("ask").Verb("submit", func(Ctx) Result { return Done(nil) }).Mode("insert").Bind("enter", Do("ask.submit"))
	field := NewInput("answer", "", false)
	if _, err := r.OpenModal("ask", Payload{Prompt: "Name?"}, WithInput(field, Kind("ochazuke.prompt"))); err != nil {
		t.Fatal(err)
	}
	if r.Mode.Name != "insert" {
		t.Fatalf("mode %q", r.Mode.Name)
	}
	hints := BindingHints(r, r.Focus)
	if !strings.Contains(hints, "enter ask.submit") {
		t.Fatalf("the modal's own key is missing: %q", hints)
	}
	for _, op := range []string{"input.kill", "input.paste", "input.delete", "input.insert", "palette.open", "completion.open", "mode.set"} {
		if strings.Contains(hints, op) {
			t.Errorf("hints name %s: %q", op, hints)
		}
	}
	listed := r.ListBindings(r.Focus)
	for _, key := range []string{"ctrl+k", "ctrl+space"} {
		if !slices.ContainsFunc(listed, func(b Binding) bool { return b.Keys == key }) {
			t.Errorf("bindings list lost %s", key)
		}
	}
}
