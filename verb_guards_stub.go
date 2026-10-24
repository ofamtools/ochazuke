package ochazuke

import "sync"

// Stub until the csv-pass branch merges: noun.go there supplies verb guards
// (Noun.Guards, NounBuilder.When after Verb, Runtime.VerbGuard and
// Runtime.VerbShown). Delete this file and verb_guards_stub_test.go's body
// of guardVerb on merge; resolve.go and runtime.go already call VerbShown.

var stubVerbGuards sync.Map // *Runtime -> map[string]string (noun.verb -> guard name)

// VerbGuard names the guard on noun.verb, "" when the verb is unguarded.
func (r *Runtime) VerbGuard(key string) string {
	if guards, ok := stubVerbGuards.Load(r); ok {
		return guards.(map[string]string)[key]
	}
	return ""
}

// VerbShown reports whether noun.verb applies at a focus in a mode: true
// for an unguarded verb or one whose guard holds there.
func (r *Runtime) VerbShown(key, focus string, mode ModeState) bool {
	name := r.VerbGuard(key)
	if name == "" {
		return true
	}
	guard := r.guard(name)
	return guard == nil || guard(r, KeyEvent{Mode: mode, Focus: focus, Level: focus})
}
