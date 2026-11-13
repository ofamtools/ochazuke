package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// AmbiguousNounError is a bare noun word several nouns share that the
// setting cli.ambiguous leaves unresolved: it names each noun in full
// (NS.NOUN), the spelling that always reaches one.
type AmbiguousNounError struct {
	Word  string
	Nouns []string
}

func (e *AmbiguousNounError) Error() string {
	return fmt.Sprintf("%q is ambiguous: %s (write one in full, or set %s to %s)", e.Word, strings.Join(e.Nouns, ", "), CLIAmbiguousSetting, CLIAmbiguousFirst)
}

// SharedNoun is the noun a bare word several nouns share reads as (the
// last part of json.doc and md.doc, an extension noun's bare name a host
// mounts), as the setting cli.ambiguous says: host (the default) gives it
// to the host's own noun named exactly word, first-registered to the noun
// registered first, and ask to none. Unresolved, the error is an
// *AmbiguousNounError naming each noun in full, sorted. nouns are names or
// aliases; the answer is a canonical name. One noun is itself under every
// value; none is an error. The palette, the CLI reader and a host's bare
// names (matomeru's extension nouns) share this one rule.
func (r *Runtime) SharedNoun(word string, nouns []string) (string, error) {
	var names []string
	for _, n := range nouns {
		if c := r.CanonicalNoun(n); !slices.Contains(names, c) {
			names = append(names, c)
		}
	}
	slices.Sort(names)
	if len(names) == 1 {
		return names[0], nil
	}
	refused := &AmbiguousNounError{Word: word, Nouns: names}
	if len(names) == 0 {
		return "", refused
	}
	switch r.Settings().String(CLIAmbiguousSetting) {
	case CLIAmbiguousAsk:
		return "", refused
	case CLIAmbiguousFirst:
		return slices.MinFunc(names, func(a, b string) int { return r.nounOrder(a) - r.nounOrder(b) }), nil
	}
	if slices.Contains(names, word) && r.Nouns[word] != nil {
		return word, nil
	}
	return "", refused
}

// nounOrder is a registered noun's place in registration order (Noun's
// seq), after every registered noun for a name with none.
func (r *Runtime) nounOrder(name string) int {
	if n := r.Nouns[name]; n != nil {
		return int(n.seq)
	}
	return int(nounSeq.Load()) + 1
}
