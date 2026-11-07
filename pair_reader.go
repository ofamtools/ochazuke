package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// The pair reader: the first two words of a line, read together. The first
// word may name several things (t is the verbs tidy, toggle, transform and
// the nouns terminal, text, timeline); the second word then decides which
// of them the line means, so an ambiguous first word is only an error when
// no second word, or more than one, fits it. The palette reads its line
// through it (Palette.parseTokens) and the CLI reads argv through it
// (Runtime.ReadPair), so both settle the same words the same way.

// pairRead is one reading of the first word: the candidate it took, how the
// second word read there (Second.Value is "" when it did not resolve), and
// Rank, how far the whole line resolved (a tie-break while no second word
// resolves anywhere).
type pairRead struct {
	First  option
	Second PaletteWord
	Rank   int
}

// label is the noun-verb pair a reading stands for, in the order a line
// writes it (noun verb).
func (p pairRead) label() string {
	if p.First.Kind == "verb" {
		return p.Second.Value + " " + p.First.Value
	}
	return p.First.Value + " " + p.Second.Value
}

// choosePair reads the second word against every candidate of the first.
// The readings where it resolved survive: one is the pair; several are
// ambiguous unless one reads the rest of the line further (ties, in
// candidate order, each noun verb pair once). When it resolved nowhere
// ok is false and nothing is chosen: the first word stays ambiguous.
func choosePair(candidates []option, read func(option) pairRead) (win pairRead, ties []pairRead, ok bool) {
	var survivors []pairRead
	for _, c := range candidates {
		r := read(c)
		if r.Second.Value != "" && !slices.ContainsFunc(survivors, func(s pairRead) bool { return s.label() == r.label() }) {
			survivors = append(survivors, r)
		}
	}
	switch {
	case len(survivors) == 1:
		return survivors[0], nil, true
	case len(survivors) > 1:
		// Several fit the second word: the one whose whole line resolves
		// further wins (the parameters after it fit, as t r up does for
		// transform row and not for to r), else they are ambiguous.
		best, kept := -1, []pairRead(nil)
		for _, r := range survivors {
			switch {
			case r.Rank > best:
				best, kept = r.Rank, []pairRead{r}
			case r.Rank == best:
				kept = append(kept, r)
			}
		}
		if len(kept) == 1 {
			return kept[0], nil, true
		}
		return pairRead{}, kept, false
	}
	return pairRead{}, nil, false
}

// pairAmbiguity is the error for a pair several readings fit: it names the
// words typed and every noun verb pair that fits.
func pairAmbiguity(first, second string, ties []pairRead) error {
	labels := make([]string, len(ties))
	for i, t := range ties {
		labels[i] = t.label()
	}
	return fmt.Errorf("%q is ambiguous: %s", strings.TrimSpace(first+" "+second), strings.Join(labels, ", "))
}

// readPair reads an ambiguous first word with the second (the palette's
// reader; the survivors parse the whole line as parseRest does).
func (p *Palette) readPair(ix lineIndex, candidates []option, tokens []lineToken) (commandLine, bool) {
	if len(tokens) < 2 || len(candidates) < 2 || !p.r.Settings().Bool(PalettePairWordsSetting) {
		return commandLine{}, false
	}
	parsed := map[string]commandLine{}
	win, ties, ok := choosePair(candidates, func(o option) pairRead {
		c := p.parseRest(ix, o, tokens)
		parsed[o.Kind+"\x00"+o.Value] = c
		r := pairRead{First: o, Rank: resolved(c)}
		// The second word resolved when it is the complement of the
		// first: a verb's noun, a noun's verb, prop or instance; a word
		// read as an argument did not.
		if len(c.Words) > 1 {
			switch c.Words[1].Kind {
			case "noun":
				if o.Kind == "verb" {
					r.Second = c.Words[1]
				}
			case "verb", "prop", "instance":
				if o.Kind == "noun" {
					r.Second = c.Words[1]
				}
			}
		}
		return r
	})
	if ok {
		return parsed[win.First.Kind+"\x00"+win.First.Value], true
	}
	if len(ties) > 1 {
		var firsts []option
		for _, t := range ties {
			firsts = appendResolved(firsts, t.First)
		}
		return commandLine{Args: Arguments{}, Words: []PaletteWord{
			{Text: tokens[0].Text, Candidates: optionValues(firsts)}, {Text: tokens[1].Text},
		}, Err: pairAmbiguity(tokens[0].Text, tokens[1].Text, ties)}, true
	}
	return commandLine{}, false
}

// Pair is a verb and the noun it runs on, read from the first words of an
// argv (Runtime.ReadPair).
type Pair struct {
	Noun string // canonical noun
	Verb string
	// Skip is how many leading words the pair used (1: the verb alone,
	// the noun implied; 2: verb and noun).
	Skip int
}

// PairScope limits and orders what ReadPair reads: Within keeps the nouns
// it accepts (nil: every noun), Prefer names the nouns that win a tie
// (nil: none does).
type PairScope struct {
	Within func(noun string) bool
	Prefer func(noun string) bool
}

// ReadPair reads words as VERB [NOUN] ..., the CLI's verb-first form.
// The verb is an exact verb of some noun, else a unique prefix. The nouns
// that have it are the candidates: when there is one, it is implied and
// the second word stays an argument; when there are several, the second
// word picks one by name or unique prefix (a word starting with - is a
// flag, never a noun), as the palette reads verb then noun. When the
// second word names none of them, scope.Prefer breaks the tie if exactly
// one candidate is preferred (an extension's own noun). The framework's
// own verbs (Operation.Builtin) are not read: a tool's argv words name
// its own nouns. err says why nothing was read: a verb on no noun, or an
// ambiguity listing the noun verb pairs that fit.
func (r *Runtime) ReadPair(words []string, scope PairScope) (pair Pair, err error) {
	if len(words) == 0 {
		return Pair{}, fmt.Errorf("no words")
	}
	ix := r.lineIndex(func(op Operation) bool {
		return !op.Builtin && (scope.Within == nil || scope.Within(op.Noun))
	}, func(a, b string) bool { return a < b })
	verb, verbs, _ := matchWord(words[0], withoutAliasVerbs(ix.byVerbs))
	cands := verbs
	if verb.Value != "" {
		cands = []option{verb}
	}
	if len(cands) == 0 {
		return Pair{}, fmt.Errorf("%q names no verb", words[0])
	}
	// Every noun verb pair the first word could be.
	type fit struct {
		verb string
		noun option
	}
	var fits []fit
	for _, c := range cands {
		for _, n := range ix.nounsOf(c.Value) {
			fits = append(fits, fit{c.Value, n})
		}
	}
	if len(fits) == 1 {
		return r.pairOf(ix, fits[0].verb, fits[0].noun.Value, 1), nil
	}
	if len(words) > 1 && !strings.HasPrefix(words[1], "-") {
		var named []pairRead
		for _, c := range cands {
			if hit, _, _ := matchWord(words[1], ix.nounOptions(ix.nounsOf(c.Value))); hit.Value != "" {
				named = append(named, pairRead{First: c, Second: PaletteWord{Kind: "noun", Value: hit.Value}})
			}
		}
		switch len(named) {
		case 1:
			return r.pairOf(ix, named[0].First.Value, named[0].Second.Value, 2), nil
		default:
			if len(named) > 1 {
				return Pair{}, pairAmbiguity(words[0], words[1], named)
			}
		}
	}
	if scope.Prefer != nil {
		var kept []fit
		for _, f := range fits {
			if scope.Prefer(r.CanonicalNoun(f.noun.Value)) {
				kept = append(kept, f)
			}
		}
		if len(kept) == 1 {
			return r.pairOf(ix, kept[0].verb, kept[0].noun.Value, 1), nil
		}
	}
	reads := make([]pairRead, len(fits))
	for i, f := range fits {
		reads[i] = pairRead{First: option{Kind: "verb", Value: f.verb}, Second: PaletteWord{Kind: "noun", Value: f.noun.Value}}
	}
	return Pair{}, pairAmbiguity(words[0], "", reads)
}

// withoutAliasVerbs drops the verbs that exist only as an action alias's
// word: the CLI mounts those under their own noun already.
func withoutAliasVerbs(options []option) []option {
	out := make([]option, 0, len(options))
	for _, o := range options {
		if !o.alias {
			out = append(out, o)
		}
	}
	return out
}

func (r *Runtime) pairOf(ix lineIndex, verb, nounWord string, skip int) Pair {
	noun := nounWord
	if op, ok := ix.ops[nounWord+"."+verb]; ok {
		noun = op.Noun
	}
	return Pair{Noun: noun, Verb: verb, Skip: skip}
}
