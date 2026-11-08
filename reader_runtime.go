package ochazuke

import (
	"slices"
	"strings"
)

// LexiconPunctuationSetting turns the reader's punctuation hints on: a.b
// reads as noun.action and a, b as action, noun (reader.go). Off, the
// marks are ordinary characters.
const LexiconPunctuationSetting = "lexicon.punctuation"

// Lexicon is the runtime's words for the reader: a noun by its shown word
// (DisplayNoun, exact or a prefix), its registered name and its aliases
// (typed whole), and with palette.noun-tails its last part (typed whole);
// on a noun, each action by its name (exact or a prefix), the action
// aliases filed under the noun (typed whole), and the noun's props (exact
// or a prefix) when its store reads or writes them.
func (r *Runtime) Lexicon() Lexicon {
	aliases := map[string][]ActionAlias{}
	for _, a := range r.ActionAliases() {
		if !a.Bare && a.Noun != "" {
			noun := r.CanonicalNoun(a.Noun)
			aliases[noun] = append(aliases[noun], a)
		}
	}
	tails := r.Settings().Bool(PaletteNounTailsSetting)
	return Lexicon{
		Nouns: func(word string) []Match { return r.lexNouns(word, tails) },
		Actions: func(n *Noun, word string) []Candidate {
			return append(append(r.Operations.actionWords(n, word), r.lexAliases(n, word, aliases[n.Name])...), r.lexProps(n, word)...)
		},
	}
}

// lexNouns is every noun word names, each by the best of its names.
func (r *Runtime) lexNouns(word string, tails bool) []Match {
	var out []Match
	for _, n := range r.Operations.Nouns() {
		if m, ok := r.lexNoun(n, word, tails); ok {
			out = append(out, m)
		}
	}
	return out
}

func (r *Runtime) lexNoun(n *Noun, word string, tails bool) (Match, bool) {
	m := Match{OriginalValue: word, PartOfSpeech: NounPart, Noun: n}
	shown := r.DisplayNoun(n.Name)
	via, ok := viaOf(shown, word)
	better := func(name string, as Via, alias string) {
		if v, hit := wholeVia(name, word, as); hit && (!ok || v.Rank() > via.Rank()) {
			via, ok, m.Alias = v, true, alias
		}
	}
	better(n.Name, ViaExact, "")
	better(n.FullName(), ViaExact, "")
	for _, a := range r.NounAliasesOf(n.Name) {
		if a.Alias != shown {
			better(a.Alias, ViaAlias, a.Alias)
		}
	}
	if own := nounWord(n.Name); tails && own != shown {
		better(own, ViaTail, "")
	}
	m.Via, m.Exact = via, via == ViaExact
	return m, ok
}

// lexAliases are the action aliases filed under n that word names (typed
// whole), each with the operation it runs; an alias named like one of n's
// own actions gives way to it.
func (r *Runtime) lexAliases(n *Noun, word string, aliases []ActionAlias) []Candidate {
	var out []Candidate
	for _, a := range aliases {
		via, hit := wholeVia(a.Verb, word, ViaAlias)
		op, found := r.Operations.Get(Key(a.Action))
		if !hit || !found || r.Operations.OperationFor(n, r.Operations.Action(a.Verb)) != nil {
			continue
		}
		out = append(out, Candidate{Operation: op, Action: Match{OriginalValue: word, PartOfSpeech: ActionPart, Action: &Action{Name: a.Verb}, Via: via, Alias: a.Alias}})
	}
	return out
}

// lexProps are n's props word names: a NOUN PROP reading runs the noun's
// set when it writes the prop, else its get (Runtime.ReadNounWord).
func (r *Runtime) lexProps(n *Noun, word string) []Candidate {
	if word == "" {
		return nil
	}
	var out []Candidate
	for _, o := range r.propOptions(n.Name) {
		via, hit := viaOf(o.Value, word)
		op := r.propOperation(n, o.Value)
		if hit && op != nil {
			out = append(out, Candidate{Operation: op, Prop: o.Value, Action: Match{OriginalValue: word, PartOfSpeech: PropPart, Via: via, Exact: via == ViaExact}})
		}
	}
	return out
}

// propOperation is the operation NOUN PROP runs: the noun's set when it
// writes the prop, else its get; nil when it has neither.
func (r *Runtime) propOperation(n *Noun, prop string) *Operation {
	if spec := n.prop(prop); spec != nil && spec.writable() {
		if set, ok := r.Operations.Get(KeyOf(n.Name, "set")); ok && set.Unavailable == "" {
			return set
		}
	}
	if get, ok := r.Operations.Get(KeyOf(n.Name, "get")); ok && get.Unavailable == "" {
		return get
	}
	return nil
}

// Namespace is the namespace the noun is in: its registered name's first
// part, DefaultNamespace for a name of one part.
func (n *Noun) Namespace() string { return nounNamespace(n.Name) }

// MainNoun is a reader's preference for the main noun of namespace
// (Noun.Main): a candidate whose noun it is comes first.
func MainNoun(namespace string) func(Candidate) []int {
	return func(c Candidate) []int {
		n := c.Noun.Noun
		return []int{boolKey(n.Main && n.Namespace() == namespace)}
	}
}

// ReadingWithin is a reading over the runtime's words, in scope, with
// punctuation as the lexicon.punctuation setting says.
func (r *Runtime) ReadingWithin(within func(Candidate) bool) Reading {
	return Reading{Lexicon: r.Lexicon(), Within: within, Punctuation: r.Settings().Bool(LexiconPunctuationSetting)}
}

// AmbiguityOf is Ambiguity with the runtime's noun spelling (DisplayNoun).
func (r *Runtime) AmbiguityOf(cs []Candidate) error { return Ambiguity(cs, r.DisplayNoun) }

// CandidateWords are the words that run a candidate in the long spelling:
// its noun's words (a dotted noun is one word per part), its action or
// prop, then the rest of the line.
func CandidateWords(c Candidate) []string {
	out := append(strings.Split(c.Noun.Noun.Name, "."), c.ActionWord())
	return append(out, c.Rest...)
}

// CompleteWords is Reading.Complete over the runtime's words, with the
// actions whose alias starts with the last word (an alias reads only typed
// whole, but completes from its start: dr offers forget-row, also drop).
// The palette's first-word list and the CLI's complete both list these.
func (r *Runtime) CompleteWords(rd Reading, words []string) []string {
	out := rd.Complete(words, r.DisplayNoun)
	if last := wordAt(words, len(words)-1); last != "" {
		for _, a := range r.ActionAliases() {
			if op, ok := r.Operations.Get(Key(a.Action)); ok && !a.Bare && hasPrefixFold(a.Verb, last) && (rd.Within == nil || rd.Within(Candidate{Operation: op, Noun: Match{Noun: r.Noun(op.Noun())}})) {
				out = append(out, op.Name())
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
