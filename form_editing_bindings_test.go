package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Check live form ancestry against the editor's non-motion normal-mode keys.
// A form may navigate with motions, but editing belongs to its text input.
func formEditingShadows(r *Runtime) []string {
	savedFocus, savedMode := r.Focus, r.Mode
	defer func() { r.Focus, r.Mode = savedFocus, savedMode }()
	editing := map[string]bool{}
	for _, row := range r.Noun("ochazuke.editor").Bindings["normal"] {
		op, ok := r.Operation(row.Request.Key())
		operator := op.Noun == "ochazuke.mode" && row.Request.Args["op"] != nil
		if ok && (op.Noun == "ochazuke.input" && !op.Motion || operator) {
			editing[strings.Split(row.Keys, " ")[0]] = true
		}
	}
	var out []string
	for id, inst := range r.Instances {
		if r.Input(id) == nil || inst.Unfocusable || r.recordEditorOf(id) == nil {
			continue
		}
		r.Focus, r.Mode = id, ModeState{Name: "normal"}
		for _, parent := range r.Ancestry(id) {
			at := r.Instances[parent]
			rows := append(slices.Clone(at.Bindings), r.Noun(at.Noun).Bindings["normal"]...)
			rows = append(rows, r.Noun(at.Noun).Bindings[""]...)
			for _, row := range rows {
				if row.Mode != "" && row.Mode != "normal" || !editing[strings.Split(row.Keys, " ")[0]] {
					continue
				}
				op, ok := r.Operation(row.Request.Key())
				target := r.rowInstance(level{inst: at, noun: r.Noun(at.Noun)}, row.Request, id)
				if !ok || op.Noun == "ochazuke.input" || !r.Available(row.Request, target, r.Mode) {
					continue
				}
				if !r.rowGuardHolds(row.When, KeyEvent{Focus: id, Level: parent, Mode: r.Mode, Args: row.Request.Args}) {
					continue
				}
				out = append(out, fmt.Sprintf("%s: %s %s shadows text editing on %s", id, at.Noun, row.Keys, op.Key()))
			}
		}
	}
	slices.Sort(out)
	return out
}

func TestFormTextEditingBindings(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	_, err := r.OpenRecordEditor("record", RecordSpec{Clearable: true, Fields: []RecordField{{Name: "title"}, {Name: "body", Multiline: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if problems := formEditingShadows(r); len(problems) > 0 {
		t.Fatal(problems)
	}
	// The validation must catch a new form's ancestor sequence as well.
	r.Define("bad-form").Verb("delete", func(Ctx) Result { return Done(nil) })
	r.Define(FormNoun).Mode("normal").Bind("d d", Do("bad-form.delete"))
	if problems := formEditingShadows(r); len(problems) == 0 {
		t.Fatal("validation missed a form ancestor stealing d d")
	}
	r, m, _ := compactFixture(t)
	compactKeys(m, "z", "/")
	compactField(t, m, "status")
	if problems := formEditingShadows(r); len(problems) > 0 {
		t.Fatal(problems)
	}
}
