package ochazuke

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// choiceList caches one parameter's choices until the line changes, so a
// Source runs once per edit rather than once per parse.
type choiceList struct {
	choices []Choice
	err     error
}

// paramChoices lists the values a parameter's word completes to in the
// palette's origin context: the instances of a palette noun for its
// default verb's argument, true and false for a boolean, else the
// parameter's Enum, Source or Noun (Runtime.Choices). word is the text
// typed for it so far: a source that declares ChoicesQueryParameter sees
// it (and is cached per word). args are the line's arguments so far: a
// ValueOf parameter's choices are those of the prop they name, and a
// Pairs parameter's word holding = completes that prop's values.
func (p *Palette) paramChoices(op Operation, name, word string, args Arguments) ([]Choice, error) {
	param, prop := p.r.choiceParameter(op, name, args)
	key := op.Key() + "/" + name + "\x00" + prop
	pairs := param.Pairs && (strings.Contains(word, "=") || len(param.Enum) == 0)
	switch {
	case pairs:
		before, _, _ := strings.Cut(word, "=")
		key += "\x00pairs\x00" + before + fmt.Sprint(strings.Contains(word, "="))
	case !p.r.ChoicesTakeQuery(param):
		word = ""
	default:
		key += "\x00" + word
	}
	if cached, ok := p.choiceCache[key]; ok {
		return cached.choices, cached.err
	}
	var choices []Choice
	var err error
	if config, ok := p.nouns[op.Noun]; ok && config.DefaultVerb == op.Verb && config.Argument == name {
		choices = p.instanceChoices(op.Noun)
	} else if pairs {
		choices, err = p.r.pairChoices(p.origin.Clone(), op, word)
	} else if param.Type == "boolean" {
		choices = []Choice{{ID: "true", Label: "true"}, {ID: "false", Label: "false"}}
	} else {
		choices, err = p.r.ChoicesFor(p.origin.Clone(), param, word)
	}
	if p.choiceCache == nil {
		p.choiceCache = map[string]choiceList{}
	}
	p.choiceCache[key] = choiceList{choices, err}
	return choices, err
}

// instanceChoices reads a palette noun's live instances.
func (p *Palette) instanceChoices(noun string) []Choice {
	config, ok := p.nouns[p.r.CanonicalNoun(noun)]
	if !ok {
		return nil
	}
	var out []Choice
	for _, instance := range config.Source(p.r, p.origin.Clone()) {
		label := instance.Label
		if label == "" {
			label = instance.ID
		}
		out = append(out, Choice{ID: instance.ID, Label: label, Word: instance.Word})
	}
	return out
}

// PaletteChoiceWordsSetting says whether the palette offers a choice's
// own word (Choice.Word, a page's title) or its id.
const PaletteChoiceWordsSetting = "palette.choice-words"

// choiceWords reports whether choices are offered and read by their words.
func (p *Palette) choiceWords() bool {
	return p.r.Settings().String(PaletteChoiceWordsSetting) != "id"
}

// choiceOptions are the options a choice word matches: each choice's word
// (resolving to its id) and its id (a prefix of either names it); without
// palette.choice-words the ids alone.
func (p *Palette) choiceOptions(kind string, choices []Choice) []option {
	words := p.choiceWords()
	out := make([]option, 0, len(choices))
	for _, choice := range choices {
		if words && choice.Word != "" && choice.Word != choice.ID {
			out = append(out, option{Kind: kind, Value: choice.Word, to: choice.ID}, option{Kind: kind, Value: choice.ID})
			continue
		}
		out = append(out, option{Kind: kind, Value: choice.ID})
	}
	return out
}

// choiceEntry is a choice's completion: its word, labelled word then id
// (JSON  ext/json), when it has one and palette.choice-words is word, else
// its id.
func (p *Palette) choiceEntry(choice Choice) PaletteEntry {
	if p.choiceWords() && choice.Word != "" {
		label := choice.Word
		if choice.ID != choice.Word {
			label += "  " + choice.ID
		}
		return PaletteEntry{Value: choice.Word, Kind: "choice", Label: label, ID: choice.ID}
	}
	return PaletteEntry{Value: choice.ID, Kind: "choice", Label: choiceLabel(choice), ID: choice.ID}
}

// choiceLabel shows a choice with its id when the label differs.
func choiceLabel(c Choice) string {
	if c.Label != "" && c.Label != c.ID {
		return fmt.Sprintf("%s  %s", c.ID, c.Label)
	}
	return c.ID
}

// hasPrefixFold reports whether s starts with prefix, ignoring case.
func hasPrefixFold(s, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix))
}

// paramSlot is the parameter the word being typed fills: Op's parameter
// Name, with Next the parameters still unfilled after it, in line order.
type paramSlot struct {
	Op   Operation
	Name string
	Next []string
	// Args are the line's arguments before the slot (a ValueOf
	// parameter's choices read them).
	Args Arguments
}

// slot finds the parameter the next word fills on a line resolved to
// tokens (the words before the one being typed): after a palette noun its
// default verb's argument, else the operation's first unfilled parameter.
func (p *Palette) slot(ix lineIndex, c commandLine, tokens []lineToken) (paramSlot, bool) {
	if c.Err != nil {
		return paramSlot{}, false
	}
	if len(tokens) == 1 && len(c.Words) == 1 && c.Words[0].Kind == "noun" {
		noun := c.Words[0].Value
		config, ok := p.nouns[p.r.CanonicalNoun(noun)]
		if !ok {
			return paramSlot{}, false
		}
		op, ok := ix.ops[noun+"."+config.DefaultVerb]
		if !ok {
			return paramSlot{}, false
		}
		var next []string
		for _, name := range paramOrder(op) {
			if name != config.Argument {
				next = append(next, name)
			}
		}
		return paramSlot{Op: op, Name: config.Argument, Next: next}, true
	}
	if !c.known() {
		return paramSlot{}, false
	}
	if c.Flag != "" {
		// --NAME waits for its value.
		return paramSlot{Op: c.Op, Name: c.Flag, Args: c.Args}, true
	}
	if c.Prop != "" {
		// NOUN PROP: the next word is its value, when the line writes it.
		if _, ok := c.Op.Parameters["value"]; ok && c.Op.Parameters["value"].ValueOf != "" {
			if _, set := c.Args["value"]; !set && c.Args["toggle"] == nil {
				return paramSlot{Op: c.Op, Name: "value", Args: c.Args}, true
			}
		}
		return paramSlot{}, false
	}
	next := remainingParams(c)
	if len(next) == 0 {
		return paramSlot{}, false
	}
	return paramSlot{Op: c.Op, Name: next[0], Next: next[1:], Args: c.Args}, true
}

// currentSlot is the slot of the palette's line as typed.
func (p *Palette) currentSlot() (paramSlot, bool) {
	if op, name, _, _, ok := p.restAt(p.index()); ok {
		return paramSlot{Op: op, Name: name}, true
	}
	tokens, trailing := tokenize(p.Text())
	if !trailing {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) == 0 {
		return paramSlot{}, false
	}
	ix := p.index()
	tokens = ix.nounRuns(p.r, tokens)
	return p.slot(ix, p.parseTokens(ix, tokens), tokens)
}

// paramHeader is the heading over a parameter's choices, one line of at
// most width cells: the name, then what it is (its type, required or
// optional, how many enum values, the operation its choices come from or
// the thing a noun-typed one names, whether free text is accepted), then
// its description and the parameters after it. Too wide, it drops the
// following parameters, cuts the description, then drops what it is from
// the end; the name is always kept.
func (p *Palette) paramHeader(s Styles, slot paramSlot, width int) string {
	param, prop := p.r.choiceParameter(slot.Op, slot.Name, slot.Args)
	if prop != "" {
		// A ValueOf parameter is headed by the prop it holds a value of.
		slot.Name = prop
	}
	var facts []string
	typ := param.Type
	if typ == "" {
		typ = "string"
	}
	facts = append(facts, typ)
	if param.Required {
		facts = append(facts, "required")
	} else {
		facts = append(facts, "optional")
	}
	if t, ok := p.r.focusValue(slot.Op, slot.Name, focusOf(p.origin)); ok {
		// Omitted, the value comes from the focus (Parameter.Focus):
		// the slot says which, so the fill is never silent.
		facts = append(facts, "focus: "+t.Label)
	}
	if len(param.Enum) > 0 {
		facts = append(facts, fmt.Sprintf("one of %d", len(param.Enum)))
	}
	if param.Source != "" {
		noun, verb := SplitKey(p.r.CanonicalKey(param.Source))
		facts = append(facts, "from "+p.r.DisplayNoun(noun)+"."+verb)
	} else if param.Noun != "" {
		facts = append(facts, article(p.r.DisplayNoun(param.Noun)))
	}
	if param.Free && (len(param.Enum) > 0 || param.Source != "" || param.Noun != "") {
		facts = append(facts, "or any text")
	}
	desc := param.Description
	next := ""
	if len(slot.Next) > 0 {
		next = " · next: " + strings.Join(slot.Next, ", ")
	}
	const sep, dash = " · ", " — "
	for {
		meta := ""
		if len(facts) > 0 {
			meta = sep + strings.Join(facts, sep)
		}
		plain := slot.Name + meta
		rest := ""
		if desc != "" {
			rest = dash + desc
		}
		rest += next
		if ansi.StringWidth(plain+rest) <= width {
			return s.Accent.Render(slot.Name) + s.Muted.Render(meta+rest)
		}
		switch room := width - ansi.StringWidth(plain+dash); {
		case next != "":
			next = ""
		case desc != "" && room >= 8:
			return s.Accent.Render(slot.Name) + s.Muted.Render(meta+dash+ansi.Truncate(desc, room, "…"))
		case desc != "":
			desc = ""
		case len(facts) > 0:
			facts = facts[:len(facts)-1]
		default:
			return s.Accent.Render(ansi.Truncate(slot.Name, max(1, width), "…"))
		}
	}
}

// article spells "a list", "an editor".
func article(noun string) string {
	if noun != "" && strings.ContainsRune("aeiou", rune(strings.ToLower(noun)[0])) {
		return "an " + noun
	}
	return "a " + noun
}
