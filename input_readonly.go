package ochazuke

// Read-only inputs (Input.ReadOnly): a viewer of text that keeps the
// editor's cursor, motions, visual selections and yanks. Compared with
// Vim's 'nomodifiable' (:help 'modifiable'): the same keys move, select
// and yank; an edit is refused, but silently (Vim says E21), and the
// editing verbs are unavailable at the input, so their rows stand aside
// and leave the hints and ? help's live view.

// InputWritableGuard is the verb guard of the editor's editing verbs
// (insert, replace, delete, kill, paste, begin, undo, redo): false while
// the focus is a read-only input.
const InputWritableGuard = "input-writable"

// OperatorWritableGuard is input.operator's: false while the focus is a
// read-only input and the request applies an operator that changes the
// text to the visual selection (a visual mode's d, c, x: no motion), so
// those rows stand aside and leave the hints. An operator over a motion
// (d w, d d) stays available: the operator pending mode its first key
// entered owns the key, and the handler leaves it a no-op.
const OperatorWritableGuard = "input-operator-writable"

// editorOperators says, for each operator input.operator takes, whether it
// changes the text; a read-only input runs only those that do not.
var editorOperators = map[string]bool{"y": false, "d": true, "c": true}

// editorOperatorChanges reports whether an operator changes the text: an
// operator the table does not know is taken to change it.
func editorOperatorChanges(op string) bool {
	changes, known := editorOperators[op]
	return changes || !known
}

// editorEditVerbs are the input verbs that change the text (input.operator
// has its own guard).
var editorEditVerbs = []string{"insert", "replace", "delete", "kill", "paste", "begin", "undo", "redo"}

// ReadOnly makes AddInput's input read-only (Input.ReadOnly).
func ReadOnly() InstanceOption {
	return func(s *instanceSpec) { s.readOnly = true }
}

// registerReadOnlyInputs defines the two guards and puts them on the
// editing verbs.
func registerReadOnlyInputs(r *Runtime) {
	readOnly := func(r *Runtime, ev KeyEvent) bool {
		in := r.Input(ev.Focus)
		return in != nil && in.ReadOnly
	}
	r.DefineGuard(InputWritableGuard, func(r *Runtime, ev KeyEvent) bool { return !readOnly(r, ev) })
	r.DefineGuard(OperatorWritableGuard, func(r *Runtime, ev KeyEvent) bool {
		op, named := ev.Args["operator"].(string)
		_, motion := ev.Args["motion"]
		return !named || motion || !editorOperatorChanges(op) || !readOnly(r, ev)
	})
	for _, verb := range editorEditVerbs {
		_ = r.AddVerbAvailability("ochazuke.input."+verb, InputWritableGuard)
	}
	_ = r.AddVerbAvailability("ochazuke.input.operator", OperatorWritableGuard)
}
