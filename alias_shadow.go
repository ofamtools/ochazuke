package ochazuke

import (
	"fmt"
	"strings"
)

// A bare alias that shadows a verb (ActionAlias.Shadows) reads by what
// follows it. One rule, from data, for every such alias:
//
//	alias alone                        the alias
//	alias NOUN ...  (NOUN exact)       NOUN's verb of the alias's name
//	alias WORD ...  (its rest reads    the alias: WORD starts the rest
//	                 WORD exactly)      parameter's text
//	alias WORD ...  (a noun's prefix   an ambiguity error naming both
//	                 and the rest
//	                 reads it)
//	alias WORD ...  (the rest reads    the alias
//	                 it)
//	alias WORD ...  (a unique noun     that noun's verb
//	                 prefix)
//	otherwise                          the alias (its action says what
//	                                   is wrong with the words)
//
// "The rest reads WORD" asks the action's rest parameter's choices
// source (Parameter.Rest) for WORD with the cursor at its end: any choice
// means it reads, a choice whose id is WORD itself means exactly. For set
// (settings.apply), that is an exact noun name, then a setting name
// (exact, then a unique prefix); a word that is both a noun's prefix and
// a setting's is ambiguous.

// BareReading is how a command line that starts with a bare alias reads
// (ReadBareAlias): the alias and its presets, with the presets of any
// further bare aliases of the same action right after it (verbose set is
// settings.apply {verbose: true}), Skip the words they took; or, for an
// alias that shadows a verb, Noun: the line is that noun's verb of the
// alias's name (set mode normal is mode set normal).
type BareReading struct {
	Alias ActionAlias
	Args  Arguments
	Skip  int
	Noun  string
}

// ReadBareAlias reads words (a palette line's or the CLI's) that start
// with a bare alias; false when the first word is none. nouns are the
// noun words that may follow a shadowing alias; nil reads every noun that
// has the verb. The CLI and the palette read a line with this one rule.
func (r *Runtime) ReadBareAlias(c Context, words []string, nouns []string) (BareReading, bool, error) {
	if len(words) == 0 {
		return BareReading{}, false, nil
	}
	a, ok := r.bareAlias(words[0])
	if !ok {
		return BareReading{}, false, nil
	}
	out := BareReading{Alias: a, Args: Arguments{}, Skip: 1}
	for len(words) > out.Skip {
		b, ok := r.bareAlias(words[out.Skip])
		if !ok || b.Action != a.Action {
			break
		}
		for k, v := range b.Args {
			out.Args[k] = v
		}
		out.Skip++
	}
	for k, v := range a.Args {
		out.Args[k] = v
	}
	if a.Shadows && out.Skip == 1 && len(words) > 1 {
		if nouns == nil {
			nouns = r.shadowNouns(a.Verb)
		}
		noun, err := r.shadowNoun(c, a, words[1], nouns)
		if err != nil {
			return out, true, &ArgumentError{Err: err}
		}
		out.Noun = noun
	}
	return out, true, nil
}

// ChoicesCursorParameter is the parameter a rest parameter's choices
// source declares to see where the cursor is in the text (a rune offset
// into the query); without it the cursor is at the end.
const ChoicesCursorParameter = "cursor"

// restChoices asks op's rest parameter's choices source for text with the
// cursor at cursor (a rune offset into text). Each choice's id is text up
// to the cursor with the completion in place. No rest parameter or no
// source answers nothing.
func (r *Runtime) restChoices(c Context, op Operation, text string, cursor int) ([]Choice, error) {
	name := op.RestParameter()
	if name == "" {
		return nil, nil
	}
	source := r.ChoicesSource(op.Parameters[name])
	if source == "" {
		return nil, nil
	}
	src, ok := r.Operations[source]
	if !ok {
		return nil, fmt.Errorf("unknown choices source %q", source)
	}
	if src.Confirm != "" {
		return nil, fmt.Errorf("choices source %q asks for confirmation", source)
	}
	args := Arguments{}
	if _, takes := src.Parameters[ChoicesQueryParameter]; takes {
		args[ChoicesQueryParameter] = text
	}
	if _, takes := src.Parameters[ChoicesCursorParameter]; takes {
		args[ChoicesCursorParameter] = max(0, min(cursor, len([]rune(text))))
	}
	result, err := r.ExecuteAt(c, Request{Verb: src.Verb, Noun: src.Noun, Args: args})
	if err != nil {
		return nil, fmt.Errorf("choices source %q: %w", source, err)
	}
	return ParseChoices(result)
}

// shadowNoun reads the word after a bare alias that shadows a verb: the
// noun word (one of nouns, the words of the nouns that have the alias's
// verb where the line is read) whose verb the line runs, or "" when the
// line is the alias. A word both readings take is an ambiguity error.
func (r *Runtime) shadowNoun(c Context, a ActionAlias, word string, nouns []string) (string, error) {
	options := make([]option, len(nouns))
	for i, n := range nouns {
		options[i] = option{Kind: "noun", Value: n}
	}
	hit, candidates, exact := matchWord(word, options)
	if hit.Value != "" && exact {
		return hit.Value, nil
	}
	op, ok := r.Operations[a.Action]
	if !ok {
		return "", nil
	}
	choices, _ := r.restChoices(c, op, word, len([]rune(word)))
	for _, choice := range choices {
		if strings.EqualFold(choice.ID, word) {
			return "", nil
		}
	}
	prefixed := candidates
	if hit.Value != "" {
		prefixed = []option{hit}
	}
	switch {
	case len(choices) > 0 && len(prefixed) > 0:
		ids := make([]string, len(choices))
		for i, choice := range choices {
			ids[i] = choice.ID
		}
		if len(ids) > setAmbiguousShown {
			ids = append(ids[:setAmbiguousShown], "…")
		}
		var named []string
		for _, n := range optionValues(prefixed) {
			named = append(named, fmt.Sprintf("%s (%s %s)", n, n, a.Verb))
		}
		return "", fmt.Errorf("%q is ambiguous: noun %s, or %s %s", word, strings.Join(named, ", "), op.RestParameter(), strings.Join(ids, ", "))
	case len(choices) > 0:
		return "", nil
	case hit.Value != "":
		return hit.Value, nil
	case len(candidates) > 1:
		return "", unresolvedError(word, "noun", candidates)
	}
	return "", nil
}

// shadowNouns are the words of the nouns that have verb, for a bare alias
// read outside the palette (the CLI): every registered noun with an
// action of that verb, by its display word.
func (r *Runtime) shadowNouns(verb string) []string {
	seen := map[string]bool{}
	var out []string
	for _, op := range r.Catalog() {
		if op.Verb != verb {
			continue
		}
		if word := r.DisplayNoun(op.Noun); !seen[word] {
			seen[word] = true
			out = append(out, word)
		}
	}
	return out
}
