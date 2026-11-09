package ochazuke

// EditorNoun is the noun whose tables hold the Readline and Vim rows every
// input reads (bindings/editor.json), and AddInput's default Kind.
const EditorNoun = "ochazuke.editor"

// InputFocusGuard is the built-in guard that holds while the focus is an
// input: what the input verbs act on (Operation.Requires).
const InputFocusGuard = "input"

// FallbackKeys is implemented by a component whose focus reads another
// noun's table as the walk's fallback (Noun.Fallback) when its instance's
// noun declares none. Every Input names EditorNoun, so an input of an
// application's Kind reads the editor's rows after app.
type FallbackKeys interface {
	FallbackNoun() string
}

// KeyOwner is implemented by a component that owns its fallback table's
// keys while OwnsKeys holds: the table answers right after the focused
// instance (before its parents, the page and app), and a key the table
// binds in the mode that does not run there (unavailable, or guarded off)
// is consumed (KeyConsumed) instead of reaching an outer row on the same
// key. Keys the table does not bind still bubble. A read-only Input owns
// the editor's keys; a custom viewer names its keymap noun as its noun's
// Fallback (NounBuilder.Fallback) and owns it the same way.
type KeyOwner interface {
	OwnsKeys() bool
}

// FallbackNoun is the editor noun: an input reads its rows (FallbackKeys).
func (in *Input) FallbackNoun() string { return EditorNoun }

// OwnsKeys holds for a read-only input, a viewer whose keys are the
// editor's (KeyOwner).
func (in *Input) OwnsKeys() bool { return in.ReadOnly }

// registerEditorNoun declares the editor noun, the fallback that reads its
// own table after app (Noun.Fallback), and the prompt noun, which pins
// insert mode. The editor noun's tables are bindings/editor.json (its
// pending modes, the motion, insertion, operator and Readline rows), and
// the registers component's rows on the app noun are
// bindings/registers.json; registerFramework loads both.
func registerEditorNoun(r *Runtime) {
	r.Define(EditorNoun).Fallback(EditorNoun).Unpersisted()
	r.Define("ochazuke.prompt").Pin("insert")
	// input-unpinned: the focused input can leave insert (a modal's text
	// input, unless modal.esc-closes pins it there).
	r.DefineGuard("input-unpinned", func(r *Runtime, ev KeyEvent) bool {
		in := r.Input(ev.Focus)
		return in != nil && in.pin == ""
	})
}
