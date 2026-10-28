package ochazuke

// registerEditorNoun declares the prompt noun, which pins insert mode. The
// editor noun's tables, the one a stack with an input falls back to, are
// bindings/editor.json (its pending modes, the motion, insertion, operator
// and Readline rows; every row that runs an input operation is guarded by
// "input"), and the registers component's rows on the app noun are
// bindings/registers.json; registerFramework loads both.
func registerEditorNoun(r *Runtime) {
	r.Define("ochazuke.prompt").Pin("insert")
}
