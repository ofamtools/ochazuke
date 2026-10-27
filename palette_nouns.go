package ochazuke

import "fmt"

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
	tokens, trailing := tokenize(p.Text())
	partial := ""
	if !trailing {
		partial = tokens[len(tokens)-1].Text
		tokens = tokens[:len(tokens)-1]
	}
	ix := p.index()
	var out []PaletteEntry
	verbs := func(names []string, noun string) {
		for _, verb := range names {
			label := verb
			if noun != "" {
				label = verbLabel(ix.ops[noun+"."+verb])
			}
			out = append(out, PaletteEntry{Value: verb, Kind: "verb", Label: label, Verb: verb})
		}
	}
	nouns := func(names []string) {
		for _, noun := range names {
			out = append(out, PaletteEntry{Value: noun, Kind: "noun", Label: noun})
		}
	}
	choices := func(op Operation, name string) {
		list, _ := p.paramChoices(op, name)
		for _, choice := range list {
			out = append(out, PaletteEntry{Value: choice.ID, Kind: "choice", Label: choiceLabel(choice), ID: choice.ID})
		}
	}
	if len(tokens) == 0 {
		verbs(optionValues(ix.byVerbs), "")
		nouns(optionValues(ix.byNouns))
	} else {
		c := p.parseTokens(ix, tokens)
		switch {
		case c.Err != nil:
		case len(tokens) == 1 && c.Words[0].Kind == "verb":
			nouns(ix.verbs[c.Words[0].Value])
			if c.known() {
				if next := remainingParams(c); len(next) > 0 {
					choices(c.Op, next[0])
				}
			}
		case len(tokens) == 1 && c.Words[0].Kind == "noun":
			noun := c.Words[0].Value
			verbs(ix.nouns[noun], noun)
			if config, ok := p.nouns[p.r.CanonicalNoun(noun)]; ok {
				if op, ok := ix.ops[noun+"."+config.DefaultVerb]; ok {
					choices(op, config.Argument)
				}
			}
		case c.known():
			if next := remainingParams(c); len(next) > 0 {
				choices(c.Op, next[0])
			}
		}
	}
	kind := map[string]string{"v": "verb", "n": "noun", "p": "choice"}[p.filter]
	filtered := out[:0]
	for _, entry := range out {
		if (kind == "" || entry.Kind == kind) && hasPrefixFold(entry.Value, partial) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
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
