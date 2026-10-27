package ochazuke

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// The palette's command line grammar. Words resolve left to right:
//
//	first:  a verb or a noun (the union)          transform | row
//	second: the complement: a noun that has the   row | transform
//	        verb, or a verb (or instance) of the
//	        noun; omitted, the noun is the nearest
//	        instance's on the focus path that has
//	        the verb
//	then:   the operation's parameters in order   upper
//	        (required first, then by name)
//
// Each word may be a unique prefix (tr r up); an ambiguous prefix stays
// unresolved and the completion list shows its candidates. The one case
// the following words settle: a first word that is a prefix of exactly one
// verb and one noun takes the reading that resolves further.

// lineToken is one word of the line; Text is unquoted.
type lineToken struct {
	Text string
}

// tokenize splits the line on spaces; a word in double quotes may hold
// spaces. trailing reports that the line ends between words, so the next
// word is empty rather than the last one being typed.
func tokenize(line string) (tokens []lineToken, trailing bool) {
	var word strings.Builder
	inWord, quoted := false, false
	for _, r := range line {
		switch {
		case quoted && r == '"':
			quoted = false
		case quoted:
			word.WriteRune(r)
		case r == '"':
			quoted, inWord = true, true
		case unicode.IsSpace(r):
			if inWord {
				tokens = append(tokens, lineToken{Text: word.String()})
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		tokens = append(tokens, lineToken{Text: word.String()})
	}
	return tokens, !inWord
}

// quoteWord spells a value as one word of the line.
func quoteWord(value string) string {
	if value == "" || strings.ContainsAny(value, " \t\"") {
		return `"` + strings.ReplaceAll(value, `"`, "") + `"`
	}
	return value
}

// PaletteWord is one word of the line and what it resolved to: Kind is
// verb, noun, instance or argument; Value is the full word ("" while it is
// unresolved); Candidates are the words an ambiguous prefix could be.
type PaletteWord struct {
	Text       string   `json:"text"`
	Kind       string   `json:"kind,omitempty"`
	Value      string   `json:"value,omitempty"`
	Param      string   `json:"param,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

// commandLine is a parsed line. It is complete when the operation is known,
// no word failed and no required parameter is missing.
type commandLine struct {
	Words   []PaletteWord
	Op      Operation
	Implied bool // the noun was omitted
	Args    Arguments
	// Missing names the first word the command still needs: "verb or
	// noun", "verb", "noun" or a parameter name.
	Missing string
	Err     error
}

func (c commandLine) known() bool { return c.Op.Verb != "" }

// Complete reports whether the line runs as typed.
func (c commandLine) Complete() bool { return c.known() && c.Err == nil && c.Missing == "" }

// Request is the request the line runs: no target, so the operation acts
// for the nearest instance of its noun on the focus path.
func (c commandLine) Request() Request {
	return Request{Verb: c.Op.Verb, Noun: c.Op.Noun, Args: c.Args}
}

// option is one word a position accepts.
type option struct {
	Kind  string // verb, noun, instance, argument
	Value string
}

// matchWord resolves word among options: an exact match (ignoring case)
// wins, else a unique prefix. Several exact matches (a word that is both a
// verb and a noun) come back as candidates with exact set.
func matchWord(word string, options []option) (hit option, candidates []option, exact bool) {
	lower := strings.ToLower(word)
	var exacts, prefixes []option
	for _, o := range options {
		v := strings.ToLower(o.Value)
		if v == lower {
			exacts = append(exacts, o)
		} else if strings.HasPrefix(v, lower) {
			prefixes = append(prefixes, o)
		}
	}
	switch {
	case len(exacts) == 1:
		return exacts[0], nil, true
	case len(exacts) > 1:
		return option{}, exacts, true
	case len(prefixes) == 1:
		return prefixes[0], nil, false
	}
	return option{}, prefixes, false
}

func optionValues(options []option) []string {
	out := make([]string, len(options))
	for i, o := range options {
		out[i] = o.Value
	}
	return out
}

// lineIndex is what the line can name where the palette opened: the
// offered operations by verb and by noun.
type lineIndex struct {
	ops     map[string]Operation // by word.verb: the noun's word (DisplayNoun) and the verb
	canon   map[string]string    // a noun's canonical name -> its word, where they differ (app.list -> list)
	verbs   map[string][]string  // verb -> nouns that have it
	nouns   map[string][]string  // noun -> its verbs
	byVerbs []option
	byNouns []option
}

func (p *Palette) index() lineIndex {
	ix := lineIndex{ops: map[string]Operation{}, canon: map[string]string{}, verbs: map[string][]string{}, nouns: map[string][]string{}}
	for _, op := range p.r.Catalog() {
		if !p.offered(op) {
			continue
		}
		// A noun is one word, its shortest name (settings for
		// app.settings while no app noun shadows it); the canonical
		// spelling is accepted as typed (canonWords) but not completed.
		word := p.r.DisplayNoun(op.Noun)
		if word != op.Noun {
			ix.canon[op.Noun] = word
		}
		ix.ops[word+"."+op.Verb] = op
		ix.verbs[op.Verb] = append(ix.verbs[op.Verb], word)
		ix.nouns[word] = append(ix.nouns[word], op.Verb)
	}
	for verb := range ix.verbs {
		ix.byVerbs = append(ix.byVerbs, option{"verb", verb})
	}
	for noun := range ix.nouns {
		ix.byNouns = append(ix.byNouns, option{"noun", noun})
	}
	sort.Slice(ix.byVerbs, func(i, j int) bool { return ix.byVerbs[i].Value < ix.byVerbs[j].Value })
	sort.Slice(ix.byNouns, func(i, j int) bool { return p.nounBefore(ix.byNouns[i].Value, ix.byNouns[j].Value) })
	for _, nouns := range ix.verbs {
		sort.Slice(nouns, func(i, j int) bool { return p.nounBefore(nouns[i], nouns[j]) })
	}
	for _, verbs := range ix.nouns {
		sort.Strings(verbs)
	}
	return ix
}

// nounBefore orders nouns as the list shows them: the nouns of the focus
// path the palette opened over first, the focused noun first, then the rest
// by name.
func (p *Palette) nounBefore(a, b string) bool {
	da, db := p.depth(a), p.depth(b)
	if (da > 0) != (db > 0) {
		return da > 0
	}
	if da != db {
		return da < db
	}
	return a < b
}

func (p *Palette) depth(noun string) int {
	for i, id := range p.r.Ancestry(focusOf(p.origin)) {
		if inst := p.r.Instances[id]; inst != nil && p.r.DisplayNoun(inst.Noun) == noun {
			return i + 1
		}
	}
	return 0
}

// impliedNoun is the noun a verb acts on when the line omits it: the
// nearest instance's noun on the focus path that has the verb, else the
// only noun that has it.
func (p *Palette) impliedNoun(ix lineIndex, verb string) string {
	for _, id := range p.r.Ancestry(focusOf(p.origin)) {
		if inst := p.r.Instances[id]; inst != nil {
			if word := p.r.DisplayNoun(inst.Noun); ix.ops[word+"."+verb].Verb != "" {
				return word
			}
		}
	}
	if nouns := ix.verbs[verb]; len(nouns) == 1 {
		return nouns[0]
	}
	return ""
}

// canonWords reads a noun typed in its canonical spelling (app.settings)
// as the noun's word (settings) in the two positions a noun takes.
func (ix lineIndex) canonWords(tokens []lineToken) []lineToken {
	out := tokens
	for i := 0; i < len(tokens) && i < 2; i++ {
		if word, ok := ix.canon[tokens[i].Text]; ok {
			if &out[0] == &tokens[0] {
				out = append([]lineToken(nil), tokens...)
			}
			out[i].Text = word
		}
	}
	return out
}

// paramOrder lists an operation's parameters in line order
// (Operation.ParameterOrder).
func paramOrder(op Operation) []string { return op.ParameterOrder() }

// parse resolves a line in the palette's origin context.
func (p *Palette) parse(line string) commandLine {
	tokens, _ := tokenize(line)
	return p.parseTokens(p.index(), tokens)
}

func (p *Palette) parseTokens(ix lineIndex, tokens []lineToken) commandLine {
	c := commandLine{Args: Arguments{}}
	if len(tokens) == 0 {
		c.Missing = "verb or noun"
		return c
	}
	tokens = ix.canonWords(tokens)
	word := tokens[0].Text
	hit, candidates, exact := matchWord(word, append(append([]option{}, ix.byVerbs...), ix.byNouns...))
	switch {
	case hit.Value != "":
		return p.parseRest(ix, hit, tokens)
	case exact:
		// A word that is a verb and a noun: the reading that resolves
		// further wins, the verb on a tie.
		verb, noun := p.parseRest(ix, option{"verb", word}, tokens), p.parseRest(ix, option{"noun", word}, tokens)
		if resolved(noun) > resolved(verb) {
			return noun
		}
		return verb
	}
	// A prefix of exactly one verb and one noun (tr: transform and
	// transformations) is settled by the words after it: the reading that
	// resolves further wins; a tie stays ambiguous.
	if len(candidates) == 2 && candidates[0].Kind != candidates[1].Kind {
		a, b := p.parseRest(ix, candidates[0], tokens), p.parseRest(ix, candidates[1], tokens)
		switch ra, rb := resolved(a), resolved(b); {
		case ra > rb:
			return a
		case rb > ra:
			return b
		}
	}
	c.Words = append(c.Words, PaletteWord{Text: word, Candidates: optionValues(candidates)})
	c.Err = unresolvedError(word, "verb or noun", candidates)
	return c
}

// resolved counts the words of a parse before its first failure, and one
// more for a complete one.
func resolved(c commandLine) int {
	n := 0
	for _, w := range c.Words {
		if w.Value == "" {
			break
		}
		n++
	}
	if c.Complete() {
		n++
	}
	return n
}

func unresolvedError(word, what string, candidates []option) error {
	if len(candidates) > 1 {
		return fmt.Errorf("%q is ambiguous: %s", word, strings.Join(optionValues(candidates), ", "))
	}
	return fmt.Errorf("%q names no %s here", word, what)
}

// parseRest resolves the second word against the complement of the first,
// then the parameters.
func (p *Palette) parseRest(ix lineIndex, first option, tokens []lineToken) commandLine {
	c := commandLine{Args: Arguments{}}
	c.Words = append(c.Words, PaletteWord{Text: tokens[0].Text, Kind: first.Kind, Value: first.Value})
	rest := tokens[1:]
	if first.Kind == "verb" {
		verb := first.Value
		var nouns []option
		for _, noun := range ix.verbs[verb] {
			nouns = append(nouns, option{"noun", noun})
		}
		if len(rest) > 0 {
			hit, candidates, _ := matchWord(rest[0].Text, nouns)
			if hit.Value != "" {
				c.Words = append(c.Words, PaletteWord{Text: rest[0].Text, Kind: "noun", Value: hit.Value})
				c.Op = ix.ops[hit.Value+"."+verb]
				p.parseParams(&c, rest[1:], "")
				return c
			}
			if len(candidates) > 1 {
				c.Words = append(c.Words, PaletteWord{Text: rest[0].Text, Candidates: optionValues(candidates)})
				c.Err = unresolvedError(rest[0].Text, "noun", candidates)
				return c
			}
		}
		if len(rest) > 0 {
			// A noun whose verb is guarded off here is named as such, not
			// read as an argument.
			if _, ok := p.r.Operation(rest[0].Text + "." + verb); ok {
				c.Words = append(c.Words, PaletteWord{Text: rest[0].Text})
				c.Err = fmt.Errorf("%s.%s is not applicable now", rest[0].Text, verb)
				return c
			}
		}
		noun := p.impliedNoun(ix, verb)
		if noun == "" {
			c.Missing = "noun"
			if len(rest) > 0 {
				c.Words = append(c.Words, PaletteWord{Text: rest[0].Text})
				c.Err = unresolvedError(rest[0].Text, "noun for "+verb, nil)
			}
			return c
		}
		c.Op, c.Implied = ix.ops[noun+"."+verb], true
		p.parseParams(&c, rest, "")
		return c
	}
	noun := first.Value
	if len(rest) == 0 {
		c.Missing = "verb"
		return c
	}
	var verbs []option
	for _, verb := range ix.nouns[noun] {
		verbs = append(verbs, option{"verb", verb})
	}
	hit, candidates, _ := matchWord(rest[0].Text, verbs)
	if hit.Value != "" {
		c.Words = append(c.Words, PaletteWord{Text: rest[0].Text, Kind: "verb", Value: hit.Value})
		c.Op = ix.ops[noun+"."+hit.Value]
		p.parseParams(&c, rest[1:], "")
		return c
	}
	if len(candidates) == 0 {
		// An instance of a palette noun runs its default verb.
		if config, ok := p.nouns[p.r.CanonicalNoun(noun)]; ok {
			var instances []option
			for _, choice := range p.instanceChoices(noun) {
				instances = append(instances, option{"instance", choice.ID})
			}
			hit, candidates, _ = matchWord(rest[0].Text, instances)
			if hit.Value != "" {
				c.Words = append(c.Words, PaletteWord{Text: rest[0].Text, Kind: "instance", Value: hit.Value, Param: config.Argument})
				c.Op = ix.ops[noun+"."+config.DefaultVerb]
				c.Args[config.Argument] = hit.Value
				p.parseParams(&c, rest[1:], config.Argument)
				return c
			}
		}
	}
	c.Words = append(c.Words, PaletteWord{Text: rest[0].Text, Candidates: optionValues(candidates)})
	c.Err = unresolvedError(rest[0].Text, "verb of "+noun, candidates)
	c.Missing = "verb"
	return c
}

// remainingParams lists the parameters a parse has not filled, in order.
func remainingParams(c commandLine) []string {
	var out []string
	for _, name := range paramOrder(c.Op) {
		if _, set := c.Args[name]; !set {
			out = append(out, name)
		}
	}
	return out
}

// parseParams fills the operation's parameters from the words in order,
// skipping preset (an instance word's argument), and names the first
// missing required one.
func (p *Palette) parseParams(c *commandLine, tokens []lineToken, preset string) {
	order := remainingParams(*c)
	for i, token := range tokens {
		if i >= len(order) {
			c.Words = append(c.Words, PaletteWord{Text: token.Text})
			c.Err = fmt.Errorf("unexpected word %q: %s takes %d argument(s)", token.Text, c.Op.Key(), len(c.Op.Parameters))
			return
		}
		name := order[i]
		value, display, candidates, err := p.argument(c.Op, name, token.Text)
		word := PaletteWord{Text: token.Text, Kind: "argument", Param: name, Value: display, Candidates: candidates}
		c.Words = append(c.Words, word)
		if err != nil {
			c.Err = err
			return
		}
		c.Args[name] = value
	}
	for _, name := range remainingParams(*c) {
		if c.Op.Parameters[name].Required {
			c.Missing = name
			return
		}
	}
}

// argument converts one word to a parameter's value: a choice (exact or a
// unique prefix of its id) when the parameter has choices, else the text;
// then the parameter's type.
func (p *Palette) argument(op Operation, name, word string) (value any, display string, candidates []string, err error) {
	param := op.Parameters[name]
	text := word
	choices, sourceErr := p.paramChoices(op, name)
	if len(choices) > 0 || p.r.ChoicesSource(param) != "" {
		options := make([]option, len(choices))
		for i, choice := range choices {
			options[i] = option{"argument", choice.ID}
		}
		hit, matches, _ := matchWord(word, options)
		switch {
		case hit.Value != "":
			text = hit.Value
		case len(matches) > 1:
			return nil, "", optionValues(matches), fmt.Errorf("%s: %q is ambiguous: %s", name, word, strings.Join(optionValues(matches), ", "))
		case !param.Free && sourceErr == nil:
			return nil, "", nil, choiceError(name, choices)
		}
	}
	switch param.Type {
	case "", "string":
		return text, text, nil, nil
	case "integer":
		n, err := strconv.Atoi(text)
		if err != nil {
			return nil, "", nil, fmt.Errorf("%s must be integer", name)
		}
		return n, text, nil, nil
	case "boolean":
		if text != "true" && text != "false" {
			return nil, "", nil, fmt.Errorf("%s must be boolean (true or false)", name)
		}
		return text == "true", text, nil, nil
	}
	return nil, "", nil, fmt.Errorf("unsupported argument type %q for %s", param.Type, name)
}
