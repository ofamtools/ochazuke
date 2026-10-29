package ochazuke

import (
	"fmt"
	"slices"
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

// lineToken is one word of the line; Text is unquoted. Start is where
// the word starts (a rune offset into the line) and Tail the line from
// there to its end, as typed: what a rest parameter (Parameter.Rest) takes.
type lineToken struct {
	Text  string
	Start int
	Tail  string
}

// tokenize splits the line on spaces; a word in double quotes may hold
// spaces. trailing reports that the line ends between words, so the next
// word is empty rather than the last one being typed.
func tokenize(line string) (tokens []lineToken, trailing bool) {
	rs := []rune(line)
	var word strings.Builder
	inWord, quoted := false, false
	start := 0
	end := func() {
		tokens = append(tokens, lineToken{Text: word.String(), Start: start, Tail: string(rs[start:])})
		word.Reset()
		inWord = false
	}
	for i, r := range rs {
		switch {
		case quoted && r == '"':
			quoted = false
		case quoted:
			word.WriteRune(r)
		case r == '"':
			if !inWord {
				start = i
			}
			quoted, inWord = true, true
		case unicode.IsSpace(r):
			if inWord {
				end()
			}
		default:
			if !inWord {
				start = i
			}
			word.WriteRune(r)
			inWord = true
		}
	}
	trailing = !inWord
	if inWord {
		end()
	}
	return tokens, trailing
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
	Via     string // the alias the line named Op by ("" for its own name)
	Implied bool   // the noun was omitted
	Args    Arguments
	// Missing names the first word the command still needs: "verb or
	// noun", "verb", "noun" or a parameter name.
	Missing string
	Err     error
	// Rest is where a rest parameter's text starts (Parameter.Rest), nil
	// when the line has none.
	Rest *restSpan
	// Prop is the prop of a NOUN PROP [VALUE] line (the shorthand, plan
	// 2.6): Op is the noun's set when it writes the prop, else its get,
	// and Request is prop-shaped.
	Prop string
	// Flag is a --NAME word still waiting for its value (the last word).
	Flag string
	// Omitted are the subject parameters the line left to the focus (the
	// optional key word, Operation.SubjectOmitted): no later word fills
	// them.
	Omitted []string
}

// restSpan is the rest parameter a line fills and the rune offset its
// text starts at.
type restSpan struct {
	Param string
	Start int
}

func (c commandLine) known() bool { return c.Op.Verb != "" }

// Complete reports whether the line runs as typed.
func (c commandLine) Complete() bool { return c.known() && c.Err == nil && c.Missing == "" }

// Request is the request the line runs: no target, so the operation acts
// for the nearest instance of its noun on the focus path.
func (c commandLine) Request() Request {
	if c.Prop != "" {
		// NOUN PROP [VALUE]: a read, a write, or a toggle, for the thing
		// the key argument names, else the focus.
		var key Arguments
		for name, v := range c.Args {
			if name != "prop" && name != "value" && name != "toggle" {
				if key == nil {
					key = Arguments{}
				}
				key[name] = v
			}
		}
		toggle, _ := c.Args["toggle"].(bool)
		return PropShorthand(c.Op.Noun, c.Prop, c.Args["value"], toggle, key)
	}
	return Request{Verb: c.Op.Verb, Noun: c.Op.Noun, Args: c.Args, Via: c.Via}
}

// option is one word a position accepts.
type option struct {
	Kind  string // verb, noun, instance, argument
	Value string
	alias bool // an action alias's word: it resolves when typed whole, never as a prefix or a completion
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
		} else if strings.HasPrefix(v, lower) && !o.alias {
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
	ops map[string]Operation // by word.verb: the noun's word (DisplayNoun) and the verb
	// aliasOps are the offered actions by an alias's word.verb, aliasVerbs
	// an alias verb's noun words and aliasNouns a noun word's alias verbs:
	// a typed alias resolves to its action, but completions list only the
	// canonical names (with the aliases on their labels).
	aliasOps   map[string]Operation
	aliasVia   map[string]string // an alias's word.verb -> its name in the alias table
	aliasVerbs map[string][]string
	aliasNouns map[string][]string
	canon      map[string]string   // a noun's canonical name -> its word, where they differ (ochazuke.list -> list)
	verbs      map[string][]string // verb -> nouns that have it
	nouns      map[string][]string // noun -> its verbs
	byVerbs    []option
	byNouns    []option
}

func (p *Palette) index() lineIndex {
	ix := lineIndex{ops: map[string]Operation{}, canon: map[string]string{}, verbs: map[string][]string{}, nouns: map[string][]string{}, aliasOps: map[string]Operation{}, aliasVia: map[string]string{}, aliasVerbs: map[string][]string{}, aliasNouns: map[string][]string{}}
	for _, op := range p.r.Catalog() {
		if !p.offered(op) {
			continue
		}
		// A noun is one word, its shortest name (settings for
		// ochazuke.settings while no app noun shadows it); the canonical
		// spelling is accepted as typed (canonWords) but not completed.
		word := p.r.DisplayNoun(op.Noun)
		if word != op.Noun {
			ix.canon[op.Noun] = word
		}
		ix.ops[word+"."+op.Verb] = op
		ix.verbs[op.Verb] = append(ix.verbs[op.Verb], word)
		ix.nouns[word] = append(ix.nouns[word], op.Verb)
	}
	for _, a := range p.r.ActionAliases() {
		op, ok := p.r.Operations[a.Action]
		if !ok || !p.offered(op) {
			continue
		}
		word := p.r.DisplayNoun(a.Noun)
		if _, taken := ix.ops[word+"."+a.Verb]; taken {
			continue
		}
		ix.aliasOps[word+"."+a.Verb] = op
		ix.aliasVia[word+"."+a.Verb] = a.Alias
		ix.aliasVerbs[a.Verb] = append(ix.aliasVerbs[a.Verb], word)
		ix.aliasNouns[word] = append(ix.aliasNouns[word], a.Verb)
	}
	for verb := range ix.verbs {
		ix.byVerbs = append(ix.byVerbs, option{Kind: "verb", Value: verb})
	}
	for verb := range ix.aliasVerbs {
		if _, own := ix.verbs[verb]; !own {
			ix.byVerbs = append(ix.byVerbs, option{Kind: "verb", Value: verb, alias: true})
		}
	}
	for noun := range ix.nouns {
		ix.byNouns = append(ix.byNouns, option{Kind: "noun", Value: noun})
	}
	sort.Slice(ix.byVerbs, func(i, j int) bool { return ix.byVerbs[i].Value < ix.byVerbs[j].Value })
	sort.Slice(ix.byNouns, func(i, j int) bool { return p.nounBefore(ix.byNouns[i].Value, ix.byNouns[j].Value) })
	for _, nouns := range ix.verbs {
		sort.Slice(nouns, func(i, j int) bool { return p.nounBefore(nouns[i], nouns[j]) })
	}
	for _, verbs := range ix.nouns {
		sort.Strings(verbs)
	}
	for _, nouns := range ix.aliasVerbs {
		sort.Slice(nouns, func(i, j int) bool { return p.nounBefore(nouns[i], nouns[j]) })
	}
	return ix
}

// op is the offered action word.verb names, and the alias it was named by
// ("" for its own name).
func (ix lineIndex) op(word, verb string) (Operation, string) {
	if op, ok := ix.ops[word+"."+verb]; ok {
		return op, ""
	}
	if op, ok := ix.aliasOps[word+"."+verb]; ok {
		return op, ix.aliasVia[word+"."+verb]
	}
	return Operation{}, ""
}

// nounsOf lists the noun words that have verb, its own name's nouns first,
// then the nouns where verb is an alias.
func (ix lineIndex) nounsOf(verb string) []option {
	var out []option
	for _, noun := range ix.verbs[verb] {
		out = append(out, option{Kind: "noun", Value: noun})
	}
	for _, noun := range ix.aliasVerbs[verb] {
		if _, own := ix.ops[noun+"."+verb]; !own {
			out = append(out, option{Kind: "noun", Value: noun})
		}
	}
	return out
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
			if word := p.r.DisplayNoun(inst.Noun); func() bool { op, _ := ix.op(word, verb); return op.Verb != "" }() {
				return word
			}
		}
	}
	if nouns := ix.nounsOf(verb); len(nouns) == 1 {
		return nouns[0].Value
	}
	return ""
}

// canonWords reads a noun typed in its canonical spelling (ochazuke.settings)
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
	// A bare alias typed whole (w!) is its action, on its own noun, before
	// any verb of that name (aliases.shadow allow lets one share a verb's
	// name; refuse keeps them apart).
	// A bare alias that shadows a verb (set) reads by the word after it
	// (ReadBareAlias): a noun that has the verb makes the line that noun's
	// verb, read below as any line is; aliases of the same action after
	// it (verbose set) add their presets.
	if a, ok := p.r.bareAlias(word); ok {
		if op, offered := p.r.Operations[a.Action]; offered && p.offered(op) {
			words := make([]string, len(tokens))
			for i, t := range tokens {
				words[i] = t.Text
			}
			reading, _, err := p.r.ReadBareAlias(p.origin.Clone(), words, optionValues(ix.nounsOf(a.Verb)))
			switch {
			case err != nil:
				c.Words = append(c.Words, PaletteWord{Text: word, Kind: "verb", Value: a.Verb}, PaletteWord{Text: tokens[1].Text})
				c.Err = err
				return c
			case reading.Noun == "":
				for _, t := range tokens[:reading.Skip] {
					c.Words = append(c.Words, PaletteWord{Text: t.Text, Kind: "verb", Value: t.Text})
				}
				c.Op, c.Via, c.Implied = op, a.Alias, true
				p.parseParams(&c, tokens[reading.Skip:], "")
				// The presets fill what the words left out: a typed word wins.
				for k, v := range reading.Args {
					if _, set := c.Args[k]; !set {
						c.Args[k] = v
					}
				}
				return c
			}
		}
	}
	hit, candidates, exact := matchWord(word, append(append([]option{}, ix.byVerbs...), ix.byNouns...))
	switch {
	case hit.Value != "":
		return p.parseRest(ix, hit, tokens)
	case exact:
		// A word that is a verb and a noun: the reading that resolves
		// further wins, the verb on a tie.
		verb, noun := p.parseRest(ix, option{Kind: "verb", Value: word}, tokens), p.parseRest(ix, option{Kind: "noun", Value: word}, tokens)
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
		nouns := ix.nounsOf(verb)
		if len(rest) > 0 {
			hit, candidates, _ := matchWord(rest[0].Text, nouns)
			if hit.Value != "" {
				c.Words = append(c.Words, PaletteWord{Text: rest[0].Text, Kind: "noun", Value: hit.Value})
				c.Op, c.Via = ix.op(hit.Value, verb)
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
		c.Op, c.Via = ix.op(noun, verb)
		c.Implied = true
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
		verbs = append(verbs, option{Kind: "verb", Value: verb})
	}
	for _, verb := range ix.aliasNouns[noun] {
		verbs = append(verbs, option{Kind: "verb", Value: verb, alias: true})
	}
	// verb > prop > key/instance (owner decision 18).
	read := p.r.readNounWord(noun, rest[0].Text, verbs)
	switch read.Kind {
	case NounWordVerb:
		c.Words = append(c.Words, PaletteWord{Text: rest[0].Text, Kind: "verb", Value: read.Value})
		c.Op, c.Via = ix.op(noun, read.Value)
		p.parseParams(&c, rest[1:], "")
		return c
	case NounWordProp:
		c.Words = append(c.Words, PaletteWord{Text: rest[0].Text, Kind: "prop", Value: read.Value})
		p.parseProp(ix, &c, noun, read.Value, rest[1:])
		return c
	}
	var candidates []option
	for _, word := range read.Candidates {
		candidates = append(candidates, option{Value: word})
	}
	if len(candidates) == 0 {
		// An instance of a palette noun runs its default verb.
		if config, ok := p.nouns[p.r.CanonicalNoun(noun)]; ok {
			var instances []option
			for _, choice := range p.instanceChoices(noun) {
				instances = append(instances, option{Kind: "instance", Value: choice.ID})
			}
			var hit option
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
		if _, set := c.Args[name]; !set && !slices.Contains(c.Omitted, name) {
			out = append(out, name)
		}
	}
	return out
}

// parseParams fills the operation's parameters from the words in order,
// skipping preset (an instance word's argument), and names the first
// missing required one. A --NAME word naming a parameter sets it from the
// next word (--NAME=VALUE in one word; a boolean takes no word unless it
// is true or false), as the CLI reads it. The subject (a Focus parameter)
// may be left out: a first word that is the next parameter's skips it
// (Operation.SubjectOmitted).
func (p *Palette) parseParams(c *commandLine, tokens []lineToken, preset string) {
	if !p.fillParams(c, tokens, remainingParams(*c)) {
		return
	}
	// An alias's preset arguments (w! is write {force: true}) fill what
	// the words left out: a typed word wins.
	if a, ok := p.r.actionNames.aliases[c.Via]; ok && c.Via != "" {
		for k, v := range a.Args {
			if _, set := c.Args[k]; !set {
				c.Args[k] = v
			}
		}
	}
	for _, name := range remainingParams(*c) {
		if c.Op.Parameters[name].Required {
			c.Missing = name
			return
		}
	}
}

// fillParams fills order's parameters from the words, a --NAME flag's
// where it stands; false when a word failed (Err) or a flag waits for its
// value (Flag).
func (p *Palette) fillParams(c *commandLine, tokens []lineToken, order []string) bool {
	positional := 0
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if len(order) > 0 && c.Op.Parameters[order[0]].Rest {
			// The rest of the line, verbatim, is this parameter's value.
			name := order[0]
			c.Words = append(c.Words, PaletteWord{Text: token.Tail, Kind: "argument", Param: name, Value: token.Tail})
			c.Args[name] = token.Tail
			c.Rest = &restSpan{Param: name, Start: token.Start}
			return true
		}
		if name, value, inline, ok := p.flagWord(c.Op, token.Text); ok {
			word := PaletteWord{Text: token.Text, Kind: "flag", Value: "--" + name, Param: name}
			order = slices.DeleteFunc(order, func(n string) bool { return n == name })
			if !inline {
				param := c.Op.Parameters[name]
				switch next := i + 1; {
				case param.Type == "boolean" && (next >= len(tokens) || tokens[next].Text != "true" && tokens[next].Text != "false"):
					c.Words = append(c.Words, word)
					c.Args[name] = true
					continue
				case next >= len(tokens):
					c.Words = append(c.Words, word)
					c.Flag, c.Missing = name, name
					return false
				}
				c.Words = append(c.Words, word)
				i++
				token = tokens[i]
				value = token.Text
			}
			v, display, candidates, err := p.argument(c.Op, name, value, c.Args)
			c.Words = append(c.Words, PaletteWord{Text: token.Text, Kind: "argument", Param: name, Value: display, Candidates: candidates})
			if err != nil {
				c.Err = err
				return false
			}
			c.Args[name] = v
			continue
		}
		if positional == 0 && c.Op.SubjectOmitted(order, token.Text) {
			// The optional key word: the subject comes from the focus.
			c.Omitted = append(c.Omitted, order[0])
			order = order[1:]
		}
		positional++
		if len(order) == 0 {
			c.Words = append(c.Words, PaletteWord{Text: token.Text})
			c.Err = fmt.Errorf("unexpected word %q: %s takes %d argument(s)", token.Text, c.Op.Key(), len(c.Op.Parameters))
			return false
		}
		name := order[0]
		order = order[1:]
		value, display, candidates, err := p.argument(c.Op, name, token.Text, c.Args)
		word := PaletteWord{Text: token.Text, Kind: "argument", Param: name, Value: display, Candidates: candidates}
		c.Words = append(c.Words, word)
		if err != nil {
			c.Err = err
			return false
		}
		c.Args[name] = value
	}
	return true
}

// flagWord reads --NAME or --NAME=VALUE naming a parameter of op; false
// for any other word (which fills a parameter in order, as before).
func (p *Palette) flagWord(op Operation, word string) (name, value string, inline, ok bool) {
	text, isFlag := strings.CutPrefix(word, "--")
	if !isFlag || text == "" {
		return "", "", false, false
	}
	name, value, inline = strings.Cut(text, "=")
	if _, known := op.Parameters[name]; !known {
		return "", "", false, false
	}
	return name, value, inline, true
}

// parseProp reads the words after NOUN PROP (plan 2.6): none reads the
// prop, one VALUE writes it, --toggle flips a boolean, --KEY names the
// thing (else it is the focus). The line's operation is the noun's set
// when it writes the prop here (so the value completes the prop's
// values), else its get.
func (p *Palette) parseProp(ix lineIndex, c *commandLine, noun, prop string, tokens []lineToken) {
	c.Prop = prop
	get, _ := ix.op(noun, "get")
	set, _ := ix.op(noun, "set")
	spec := p.r.Noun(noun).prop(prop)
	switch {
	case set.Verb != "" && spec != nil && spec.writable():
		c.Op = set
	case get.Verb != "":
		c.Op = get
	default:
		c.Err = fmt.Errorf("%s %s: the %s cannot be read or written here", noun, prop, noun)
		return
	}
	c.Args["prop"] = prop
	order := []string{}
	if c.Op.Verb == set.Verb && c.Op.Noun == set.Noun {
		order = append(order, "value")
	}
	if !p.fillParams(c, tokens, order) && c.Err != nil && len(order) == 0 && c.Words[len(c.Words)-1].Kind == "" {
		c.Err = fmt.Errorf("%s.%s is read-only here: %s %s reads it", noun, prop, noun, prop)
	}
}

// argument converts one word to a parameter's value: a choice (exact or a
// unique prefix of its id) when the parameter has choices, else the text;
// then the parameter's type.
func (p *Palette) argument(op Operation, name, word string, args Arguments) (value any, display string, candidates []string, err error) {
	param := op.Parameters[name]
	text := word
	choices, sourceErr := p.paramChoices(op, name, word, args)
	// A ValueOf parameter is checked against the prop it holds a value of.
	check, _ := p.r.choiceParameter(op, name, args)
	if len(choices) > 0 || p.r.ChoicesSource(check) != "" {
		options := make([]option, len(choices))
		for i, choice := range choices {
			options[i] = option{Kind: "argument", Value: choice.ID}
		}
		hit, matches, _ := matchWord(word, options)
		switch {
		case hit.Value != "":
			text = hit.Value
		case len(matches) > 1:
			return nil, "", optionValues(matches), fmt.Errorf("%s: %q is ambiguous: %s", name, word, strings.Join(optionValues(matches), ", "))
		case !check.Free && sourceErr == nil:
			return nil, "", nil, choiceError(name, choices)
		}
	}
	switch param.Type {
	case "", "string", PathParameterType, AnyParameterType:
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
