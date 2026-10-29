package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// PaletteInstance is a labeled object whose ID becomes the default action argument.
type PaletteInstance struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PaletteSource reads current instances in the context captured when the palette opened.
type PaletteSource func(*Runtime, Context) []PaletteInstance

// PaletteNoun explicitly chooses the action and string argument used for an instance.
type PaletteNoun struct {
	Source      PaletteSource
	DefaultVerb string
	Argument    string
}

// PaletteEntry is one completion: Value is the word it appends, Kind is
// verb, noun or choice, Label what the list shows.
type PaletteEntry struct {
	Value string `json:"value"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	ID    string `json:"id,omitempty"`
	Verb  string `json:"verb,omitempty"`
	// Aliases are the other verb words that name this entry's action
	// (ActionAlias): typing one narrows to this entry, which is listed
	// once under its canonical name.
	Aliases []string `json:"aliases,omitempty"`
	// Splice marks a rest parameter's completion (Parameter.Rest): taking
	// it puts Value in place of the line from From (a rune offset) to the
	// cursor, adding no space, instead of replacing the word being typed.
	Splice bool `json:"splice,omitempty"`
	From   int  `json:"from,omitempty"`
}

// RegisterNoun adds or replaces a live instance source without changing operation forms.
func (p *Palette) RegisterNoun(noun string, config PaletteNoun) error {
	noun = p.r.CanonicalNoun(noun)
	known := false
	for _, op := range p.r.Catalog() {
		if op.Noun == noun {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("palette: unknown noun %q", noun)
	}
	if config.Source == nil {
		return fmt.Errorf("palette noun %q: source is required", noun)
	}
	op, ok := p.r.Operations[Request{Verb: config.DefaultVerb, Noun: noun}.Key()]
	if !ok {
		return fmt.Errorf("palette noun %q: unknown default action %q", noun, noun+"."+config.DefaultVerb)
	}
	param, ok := op.Parameters[config.Argument]
	if !ok || param.Type != "string" {
		return fmt.Errorf("palette noun %q: default action %s requires a string instance argument; got %q", noun, op.Key(), config.Argument)
	}
	if guard := p.r.VerbGuard(op.Key()); guard != "" {
		return fmt.Errorf("palette noun %q: default action %s is guarded by %q; a default verb is never guarded", noun, op.Key(), guard)
	}
	for name, param := range op.Parameters {
		if name != config.Argument && param.Required {
			return fmt.Errorf("palette noun %q: default action %s also requires argument %q", noun, op.Key(), name)
		}
	}
	p.nouns[noun] = config
	return nil
}

// PagesPaletteSource reads the runtime registry on every listing, not at startup.
func PagesPaletteSource(r *Runtime, _ Context) []PaletteInstance {
	var instances []PaletteInstance
	for _, page := range r.PageList() {
		instances = append(instances, PaletteInstance{ID: page.ID, Label: page.Title})
	}
	return instances
}

// completions is the list under the line: the words the next position
// accepts, grouped verbs, nouns, then parameter choices, narrowed by the
// word being typed (a prefix, as abbreviation resolves) and by the filter
// code, if one was typed for this position.
func (p *Palette) completions() []PaletteEntry {
	if entries, ok := p.restCompletions(); ok {
		if p.filter != "" && p.filter != "p" {
			return nil
		}
		return entries
	}
	tokens, trailing := tokenize(p.Text())
	partial := ""
	partialStart := len([]rune(p.Text()))
	if !trailing {
		partial = tokens[len(tokens)-1].Text
		partialStart = tokens[len(tokens)-1].Start
		tokens = tokens[:len(tokens)-1]
	}
	ix := p.index()
	view := p.r.StandardVerbsView()
	var out []PaletteEntry
	verbs := func(names []string, noun string) {
		for _, verb := range ix.orderVerbs(names, noun, view) {
			label := verb
			var aliases []string
			if noun != "" {
				op := ix.ops[noun+"."+verb]
				label = verbLabel(op)
				aliases = ix.aliasWords(p.r, op, noun)
			} else {
				for _, n := range ix.verbs[verb] {
					aliases = append(aliases, ix.aliasWords(p.r, ix.ops[n+"."+verb], "")...)
				}
				slices.Sort(aliases)
				aliases = slices.Compact(aliases)
			}
			if view.Tag && ix.standardOnly(verb, noun) {
				label += " · std"
			}
			if len(aliases) > 0 {
				label += " · also " + strings.Join(aliases, ", ")
			}
			out = append(out, PaletteEntry{Value: verb, Kind: "verb", Label: label, Verb: verb, Aliases: aliases})
		}
	}
	props := func(noun string) {
		for _, prop := range p.propWords(ix, noun) {
			out = append(out, PaletteEntry{Value: prop.Name, Kind: "prop", Label: propLabel(prop)})
		}
	}
	nouns := func(names []string) {
		for _, noun := range names {
			out = append(out, PaletteEntry{Value: noun, Kind: "noun", Label: noun})
		}
	}
	choices := func(op Operation, name string, args Arguments) {
		list, _ := p.paramChoices(op, name, partial, args)
		pairs := op.Parameters[name].Pairs
		for _, choice := range list {
			entry := PaletteEntry{Value: choice.ID, Kind: "choice", Label: choiceLabel(choice), ID: choice.ID}
			if pairs && strings.HasSuffix(choice.ID, "=") {
				// NAME= of a pairs parameter: taking it leaves the cursor
				// after the =, for the value.
				entry.Splice, entry.From = true, partialStart
			}
			out = append(out, entry)
		}
	}
	if len(tokens) == 0 {
		var own []string
		for _, o := range ix.byVerbs {
			if !o.alias {
				own = append(own, o.Value)
			}
		}
		verbs(own, "")
		nouns(optionValues(ix.byNouns))
	} else {
		c := p.parseTokens(ix, tokens)
		switch {
		case c.Err != nil:
		case len(tokens) == 1 && c.Words[0].Kind == "verb":
			names := ix.verbs[c.Words[0].Value]
			if a, bare := p.r.bareAlias(tokens[0].Text); bare && a.Shadows {
				// set NOUN reads only one-value nouns (owner decision 17).
				names = p.r.shadowWords(names, a.Verb)
			}
			nouns(names)
		case len(tokens) == 1 && c.Words[0].Kind == "noun":
			noun := c.Words[0].Value
			verbs(ix.nouns[noun], noun)
			props(noun)
		}
		if slot, ok := p.slot(ix, c, tokens); ok {
			if param := slot.Op.Parameters[slot.Name]; param.Focus && len(slot.Next) > 0 && len(slot.Op.Parameters[slot.Next[0]].Enum) > 0 {
				// The optional key word: the next parameter's words (a
				// prop name) first, then the things.
				choices(slot.Op, slot.Next[0], slot.Args)
			}
			choices(slot.Op, slot.Name, slot.Args)
		}
	}
	kind := map[string]string{"v": "verb", "n": "noun", "p": "choice"}[p.filter]
	filtered := out[:0]
	for _, entry := range out {
		if (kind == "" || entry.Kind == kind) && (hasPrefixFold(entry.Value, partial) || partial != "" && slices.ContainsFunc(entry.Aliases, func(a string) bool { return hasPrefixFold(a, partial) })) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// aliasWords are the words that name op as a verb in the palette besides
// its own: its aliases' verbs on noun word (any noun when word is ""), and
// another noun's alias as word.verb.
func (ix lineIndex) aliasWords(r *Runtime, op Operation, word string) []string {
	if op.Verb == "" {
		return nil
	}
	var out []string
	for _, a := range r.AliasesOf(op.Key()) {
		if _, offered := ix.aliasOps[r.DisplayNoun(a.Noun)+"."+a.Verb]; !offered {
			continue
		}
		if word == "" || r.DisplayNoun(a.Noun) == word {
			out = append(out, a.Verb)
		} else {
			out = append(out, r.DisplayKey(a.Alias))
		}
	}
	return out
}

// offered reports whether the palette lists an operation: a verb whose
// guard (Verb(...).When) is false at the focus and mode the palette opened
// over is left out; a palette noun's default verb always stays.
func (p *Palette) offered(op Operation) bool {
	if config, ok := p.nouns[op.Noun]; ok && config.DefaultVerb == op.Verb {
		return true
	}
	return p.r.VerbShown(op.Key(), focusOf(p.origin), p.originMode)
}

// verbLabel names a verb in the palette with its kind when it is an
// application verb and marks a verb that asks for confirmation.
func verbLabel(op Operation) string {
	label := op.Verb
	if op.Kind == KindApplication {
		label += " · application"
	}
	if op.Confirm != "" {
		label += " · confirm"
	}
	return label
}

// restAt finds the rest parameter (Parameter.Rest) the palette's cursor is
// in: the line's operation, the parameter, where its text starts (a rune
// offset into the line) and the cursor in that text. A line whose rest
// has not started yet (set and a space) is in it at the end of the line.
func (p *Palette) restAt(ix lineIndex) (op Operation, name string, start, cursor int, ok bool) {
	text := p.Text()
	n := len([]rune(text))
	at := n
	if p.line != nil {
		at = max(0, min(p.line.Cursor, n))
	}
	tokens, trailing := tokenize(text)
	if len(tokens) == 0 {
		return Operation{}, "", 0, 0, false
	}
	c := p.parseTokens(ix, tokens)
	if c.Err == nil && c.Rest != nil && at >= c.Rest.Start {
		return c.Op, c.Rest.Param, c.Rest.Start, at - c.Rest.Start, true
	}
	if !trailing || c.Rest != nil {
		return Operation{}, "", 0, 0, false
	}
	if slot, ok := p.slot(ix, c, tokens); ok && slot.Op.Parameters[slot.Name].Rest {
		return slot.Op, slot.Name, n, at - n, at == n
	}
	return Operation{}, "", 0, 0, false
}

// restCompletions are the rest parameter's choices at the cursor, each
// spliced over the rest's text up to the cursor; false when the cursor is
// in no rest parameter. Cached per text and cursor until the line changes.
func (p *Palette) restCompletions() ([]PaletteEntry, bool) {
	ix := p.index()
	op, name, start, cursor, ok := p.restAt(ix)
	if !ok {
		return nil, false
	}
	rest := string([]rune(p.Text())[start:])
	key := fmt.Sprintf("%s/%s\x00rest\x00%d\x00%s", op.Key(), name, cursor, rest)
	cached, hit := p.choiceCache[key]
	if !hit {
		cached.choices, cached.err = p.r.restChoices(p.origin.Clone(), op, rest, cursor)
		if p.choiceCache == nil {
			p.choiceCache = map[string]choiceList{}
		}
		p.choiceCache[key] = cached
	}
	out := make([]PaletteEntry, 0, len(cached.choices))
	for _, choice := range cached.choices {
		label := choice.Label
		if label == "" {
			label = choice.ID
		}
		out = append(out, PaletteEntry{Value: choice.ID, Kind: "choice", Label: label, ID: choice.ID, Splice: true, From: start})
	}
	return out, true
}

// orderVerbs is a verb column's verbs as palette.standard-verbs orders
// them: last puts the noun's named verbs before its standard ones, hidden
// leaves the standard ones out, mixed keeps one alphabetical list.
func (ix lineIndex) orderVerbs(names []string, noun string, view StandardVerbsView) []string {
	if view.Order == StandardVerbsMixed {
		return names
	}
	var named, standard []string
	for _, verb := range names {
		if ix.standardOnly(verb, noun) {
			standard = append(standard, verb)
		} else {
			named = append(named, verb)
		}
	}
	if view.Order == StandardVerbsHidden {
		return named
	}
	return append(named, standard...)
}

// propWords are the props the palette offers after a noun (NOUN PROP
// [VALUE]): every prop shown where the palette opened, when the noun reads
// or writes its things here.
func (p *Palette) propWords(ix lineIndex, noun string) []PropSpec {
	get, _ := ix.op(noun, "get")
	set, _ := ix.op(noun, "set")
	n := p.r.Noun(noun)
	if n == nil || get.Verb == "" && set.Verb == "" {
		return nil
	}
	var out []PropSpec
	for _, prop := range n.Props {
		if p.r.propShown(prop, focusOf(p.origin), p.originMode, nil) {
			out = append(out, prop)
		}
	}
	return out
}

// propLabel names a prop in the completion list: the name, prop, and
// read-only when nothing writes it.
func propLabel(prop PropSpec) string {
	label := prop.Name + " · prop"
	if !prop.writable() {
		label += " · read-only"
	}
	return label
}
