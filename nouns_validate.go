package ochazuke

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
)

// Thing nouns and command nouns (DESIGN-001). A noun with a Store is a
// thing noun: it has every core verb (core_verbs.go), its own or the
// generated one, and a key prop (a singleton excepted: its one thing
// needs none). A noun without a store is a command noun: its verbs are
// named actions, never a core verb's name. ValidateNouns reports every
// noun that breaks one of these; nouncheck.Check holds an application's
// tests to it with a shrink-only baseline.

// Kinds of NounProblem.
const (
	// NounLacksCoreVerb: a thing noun has no verb (nor alias) of a core
	// verb's name.
	NounLacksCoreVerb = "lacks-core-verb"
	// NounCommandCoreVerb: a command noun registers a verb with a core
	// verb's name.
	NounCommandCoreVerb = "command-core-verb"
	// NounNoKey: a thing noun whose store is not a singleton has no key
	// prop.
	NounNoKey = "no-key"
)

// NounProblem is one way a noun breaks the thing and command noun rules:
// its kind, the noun and the verb concerned ("" for NounNoKey).
type NounProblem struct {
	Kind string
	Noun string
	Verb string
}

func (p *NounProblem) Error() string {
	switch p.Kind {
	case NounLacksCoreVerb:
		return fmt.Sprintf("thing noun %s lacks the core verb %s", p.Noun, p.Verb)
	case NounCommandCoreVerb:
		return fmt.Sprintf("command noun %s (no store) has a verb named like the core verb %s", p.Noun, p.Verb)
	default:
		return fmt.Sprintf("thing noun %s has no key prop", p.Noun)
	}
}

// Row is the problem as one tab-separated baseline line: kind, noun,
// verb.
func (p *NounProblem) Row() string { return p.Kind + "\t" + p.Noun + "\t" + p.Verb }

// ValidateNouns checks every noun against the thing and command noun
// rules and returns a *NounProblem for each break, sorted by noun, then
// kind, then verb; nil when there is none. Call it once every noun and
// verb is registered (an application's bindings test).
func (r *Runtime) ValidateNouns() []error {
	core := CoreVerbs()
	verbsOf := map[string][]string{}
	for _, op := range r.Operations.Entries() {
		verbsOf[op.Noun()] = append(verbsOf[op.Noun()], op.Name())
	}
	var out []*NounProblem
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		n := r.Nouns[name]
		if n.Store == nil {
			for _, verb := range slices.Sorted(slices.Values(verbsOf[name])) {
				if slices.Contains(core, verb) {
					out = append(out, &NounProblem{Kind: NounCommandCoreVerb, Noun: name, Verb: verb})
				}
			}
			continue
		}
		for _, verb := range core {
			if !slices.Contains(verbsOf[name], verb) && !r.aliasNamed(name+"."+verb) {
				out = append(out, &NounProblem{Kind: NounLacksCoreVerb, Noun: name, Verb: verb})
			}
		}
		if n.Store.Kind != Singleton && n.keyProp() == nil {
			out = append(out, &NounProblem{Kind: NounNoKey, Noun: name})
		}
	}
	slices.SortFunc(out, func(a, b *NounProblem) int {
		return cmp.Compare(a.Noun+"\x00"+a.Kind+"\x00"+a.Verb, b.Noun+"\x00"+b.Kind+"\x00"+b.Verb)
	})
	errs := make([]error, len(out))
	for i, p := range out {
		errs[i] = p
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// aliasNamed reports whether an alias of that name exists.
func (r *Runtime) aliasNamed(name string) bool {
	_, ok := r.actionNames.aliases[name]
	return ok
}
