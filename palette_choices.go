package ochazuke

import (
	"fmt"
	"strings"
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
// parameter's Enum, Source or Noun (Runtime.Choices).
func (p *Palette) paramChoices(op Operation, name string) ([]Choice, error) {
	key := op.Key() + "/" + name
	if cached, ok := p.choiceCache[key]; ok {
		return cached.choices, cached.err
	}
	var choices []Choice
	var err error
	param := op.Parameters[name]
	if config, ok := p.nouns[op.Noun]; ok && config.DefaultVerb == op.Verb && config.Argument == name {
		choices = p.instanceChoices(op.Noun)
	} else if param.Type == "boolean" {
		choices = []Choice{{ID: "true", Label: "true"}, {ID: "false", Label: "false"}}
	} else {
		choices, err = p.r.Choices(p.origin.Clone(), param)
	}
	if p.choiceCache == nil {
		p.choiceCache = map[string]choiceList{}
	}
	p.choiceCache[key] = choiceList{choices, err}
	return choices, err
}

// instanceChoices reads a palette noun's live instances.
func (p *Palette) instanceChoices(noun string) []Choice {
	config, ok := p.nouns[noun]
	if !ok {
		return nil
	}
	var out []Choice
	for _, instance := range config.Source(p.r, p.origin.Clone()) {
		label := instance.Label
		if label == "" {
			label = instance.ID
		}
		out = append(out, Choice{ID: instance.ID, Label: label})
	}
	return out
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
