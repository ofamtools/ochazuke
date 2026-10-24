package ochazuke

import "strings"

// guardVerb guards noun.verb with a named row guard. On the csv-pass merge
// its body becomes Define(noun).Verb-builder When, or:
//
//	noun, verb, _ := strings.Cut(key, ".")
//	if r.Nouns[noun].Guards == nil { r.Nouns[noun].Guards = map[string]string{} }
//	r.Nouns[noun].Guards[verb] = guard
func guardVerb(r *Runtime, key, guard string) {
	noun, verb, _ := strings.Cut(key, ".")
	n := r.Nouns[noun]
	if n == nil {
		n = r.Define(noun).Noun()
	}
	if n.Guards == nil {
		n.Guards = map[string]string{}
	}
	n.Guards[verb] = guard
}
