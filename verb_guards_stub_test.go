package ochazuke

// guardVerb guards noun.verb with a named row guard. On the csv-pass merge
// its body becomes Define(noun).Verb-builder When, or:
//
//	noun, verb, _ := strings.Cut(key, ".")
//	if r.Nouns[noun].Guards == nil { r.Nouns[noun].Guards = map[string]string{} }
//	r.Nouns[noun].Guards[verb] = guard
func guardVerb(r *Runtime, key, guard string) {
	guards := map[string]string{}
	if existing, ok := stubVerbGuards.Load(r); ok {
		guards = existing.(map[string]string)
	}
	guards[key] = guard
	stubVerbGuards.Store(r, guards)
}
