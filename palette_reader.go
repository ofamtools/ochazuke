package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// The palette reads its line's leading words through the reader
// (reader.go), as the CLI reads argv: the pair both orders, an omitted
// noun filled from the focus path, then the scope's main noun. The words
// after the pair are the operation's parameters (parseParams) or a prop's
// value (parseProp); a noun the reader read as no action of it reads its
// next word as an instance or says why it is none (parseRest).

// reading is the palette's reader: its offered operations, the line's
// parse as the binding of the rest, the focus path's nouns first, then the
// program's main noun.
func (p *Palette) reading(ix lineIndex, tokens []lineToken) Reading {
	rd := p.r.ReadingWithin(func(c Candidate) bool { return p.offered(*c.Operation) })
	rd.Bind = func(c Candidate) int { return bound(p.lineOf(ix, c, tokens)) }
	main := MainNoun(DefaultNamespace)
	rd.Prefer = func(c Candidate) []int {
		depth := p.depth(p.r.DisplayNoun(c.Noun.Noun.Name))
		return append([]int{boolKey(depth > 0), -depth}, main(c)...)
	}
	rd.WordsOnly = !p.r.Settings().Bool(PalettePairWordsSetting)
	return rd
}

// bound is how far a parsed line's words bind: the words read before the
// first that failed, and one more when none failed. A parameter still
// missing is no failure (the args form asks for it), so a verb that needs
// more is not read below one that needs nothing.
func bound(c commandLine) int {
	n := 0
	for _, w := range c.Words {
		if w.Value == "" {
			break
		}
		n++
	}
	if c.Err == nil {
		n++
	}
	return n
}

// readLine reads a line's words: the reader's one candidate is the line;
// ties over the typed words are an ambiguity; ties over an omitted noun
// leave it missing; a noun read with no action of it reads on (parseRest).
func (p *Palette) readLine(ix lineIndex, tokens []lineToken, old []string) commandLine {
	cs := p.reading(ix, tokens).Read(tokenTexts(tokens))
	var c commandLine
	switch {
	case cs == nil:
		c = p.unknownLine(ix, tokens)
	case slices.ContainsFunc(cs, func(o Candidate) bool { return !sameReading(o, cs[0]) }):
		c = p.ambiguousLine(cs)
	case cs[0].NounOnly():
		c = p.parseRest(ix, option{Kind: "noun", Value: p.r.DisplayNoun(cs[0].Noun.Noun.Name)}, tokens)
	case slices.ContainsFunc(cs, func(o Candidate) bool { return o.Operation != cs[0].Operation }):
		c = p.nounMissing(cs[0], tokens)
	default:
		c = p.lineOf(ix, cs[0], tokens)
	}
	c.Old = append(old, c.Old...)
	return c
}

// sameReading reports whether two candidates read the typed words the same.
func sameReading(a, b Candidate) bool { return slices.Equal(a.TypedReading(), b.TypedReading()) }

// lineOf is the line a candidate reads: its typed words, its operation
// (by the alias it was typed as), then the rest of the words as the
// operation's parameters or the prop's value.
func (p *Palette) lineOf(ix lineIndex, cand Candidate, tokens []lineToken) commandLine {
	c := commandLine{Args: Arguments{}, Op: *cand.Operation, Via: cand.Action.Alias, Implied: cand.Noun.Via == ViaOmitted}
	for i, m := range cand.typedParts() {
		c.Words = append(c.Words, p.partWord(cand, m, i > 0))
	}
	if alias := strings.ToLower(cand.Noun.Alias); ix.old[alias] != "" {
		c.Old = append(c.Old, ix.old[alias])
	}
	rest := tokens[min(cand.Words, len(tokens)):]
	if cand.Prop != "" {
		p.parseProp(ix, &c, p.r.DisplayNoun(cand.Noun.Noun.Name), cand.Prop, rest)
		return c
	}
	if err := p.guardedNoun(cand, rest); err != nil {
		c.Words, c.Err = append(c.Words, PaletteWord{Text: rest[0].Text}), err
		return c
	}
	p.parseParams(&c, rest, "")
	return c
}

// partWord is the palette word of one typed part.
func (p *Palette) partWord(c Candidate, m Match, complement bool) PaletteWord {
	w := PaletteWord{Text: m.OriginalValue, Complement: complement}
	switch {
	case m.Noun != nil:
		w.Kind, w.Value = "noun", p.r.DisplayNoun(m.Noun.Name)
	case c.Prop != "":
		w.Kind, w.Value = "prop", c.Prop
	default:
		w.Kind, w.Value = "verb", c.ActionWord()
	}
	return w
}

// guardedNoun is the error for a verb whose noun was left out when the
// next word names a noun whose verb of that name is guarded off here.
func (p *Palette) guardedNoun(c Candidate, rest []lineToken) error {
	if c.Noun.Via != ViaOmitted || len(rest) == 0 {
		return nil
	}
	if _, ok := p.r.Operation(rest[0].Text + "." + c.ActionWord()); ok {
		return fmt.Errorf("%s.%s is not applicable now", rest[0].Text, c.ActionWord())
	}
	return nil
}

// nounMissing is the line of a verb whose omitted noun several nouns fit:
// the noun is missing, or the next word names none of them.
func (p *Palette) nounMissing(c Candidate, tokens []lineToken) commandLine {
	line := commandLine{Args: Arguments{}, Missing: "noun"}
	line.Words = []PaletteWord{p.partWord(c, c.Action, false)}
	if rest := tokens[min(c.Words, len(tokens)):]; len(rest) > 0 {
		if err := p.guardedNoun(c, rest); err != nil {
			line.Words, line.Err = append(line.Words, PaletteWord{Text: rest[0].Text}), err
			return line
		}
		line.Words = append(line.Words, PaletteWord{Text: rest[0].Text})
		line.Err = unresolvedError(rest[0].Text, "noun for "+c.ActionWord(), nil)
	}
	return line
}

// ambiguousLine is the line of ties over the typed words: each word with
// the readings the ties give it, and the ambiguity.
func (p *Palette) ambiguousLine(cs []Candidate) commandLine {
	c := commandLine{Args: Arguments{}, Err: p.r.AmbiguityOf(cs)}
	for i, text := range typedWords(cs[0]) {
		w := PaletteWord{Text: text}
		for _, cand := range cs {
			if parts := cand.typedParts(); i < len(parts) {
				value := p.partWord(cand, parts[i], i > 0).Value
				if !slices.Contains(w.Candidates, value) {
					w.Candidates = append(w.Candidates, value)
				}
			}
		}
		c.Words = append(c.Words, w)
	}
	return c
}

// unknownLine is the line of a first word that names nothing here.
func (p *Palette) unknownLine(ix lineIndex, tokens []lineToken) commandLine {
	c := commandLine{Args: Arguments{}}
	word := tokens[0].Text
	c.Words = append(c.Words, PaletteWord{Text: word})
	c.Err = unresolvedError(word, "verb or noun", nil)
	return c
}

// NounOnly reports whether the candidate is a typed noun whose action was
// left out and is not its default action: the noun alone, its action
// still to come.
func (c Candidate) NounOnly() bool {
	return c.Action.Via == ViaOmitted && c.Noun.Via != ViaOmitted && c.ActionWord() != c.Noun.Noun.DefaultAction
}
