package ochazuke

import (
	"fmt"
	"strconv"
	"strings"
)

// choiceList caches one argument's choices for the open form, so a Source
// runs once per form rather than once per key.
type choiceList struct {
	choices []Choice
	err     error
}

// pickField reports whether the focused argument field is a pick list.
func (p *Palette) pickField() (string, bool) {
	if p.stage != "arguments" || p.field >= len(p.fields) {
		return "", false
	}
	name := p.fields[p.field]
	param := p.op.Parameters[name]
	return name, len(param.Enum) > 0 || param.Source != ""
}

// fieldChoices lists an argument's choices in the palette's origin context.
func (p *Palette) fieldChoices(name string) ([]Choice, error) {
	if cached, ok := p.choiceCache[name]; ok {
		return cached.choices, cached.err
	}
	choices, err := p.r.Choices(p.origin.Clone(), p.op.Parameters[name])
	if p.choiceCache == nil {
		p.choiceCache = map[string]choiceList{}
	}
	p.choiceCache[name] = choiceList{choices, err}
	return choices, err
}

// choiceMatches filters a field's choices by its text, search-first like the
// palette's lists.
func (p *Palette) choiceMatches(name string) []Choice {
	choices, _ := p.fieldChoices(name)
	query := strings.ToLower(string(p.editor("argument/" + name).Text))
	if query == "" {
		return choices
	}
	var out []Choice
	for _, c := range choices {
		if strings.Contains(strings.ToLower(c.ID+" "+c.Label), query) {
			out = append(out, c)
		}
	}
	return out
}

// moveChoice moves the highlighted choice of a pick field, or the selection
// of a palette list.
func (p *Palette) moveChoice(delta int) error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	if p.stage != "arguments" {
		return p.move(delta)
	}
	name, ok := p.pickField()
	if !ok {
		return nil
	}
	if count := len(p.choiceMatches(name)); count > 0 {
		p.choice = ((p.choice+delta)%count + count) % count
	}
	p.message = ""
	return nil
}

// pick completes the focused pick field: text that already names a choice
// stays, otherwise the highlighted match replaces it. With no match the text
// stays only when the parameter is Free or its source failed.
func (p *Palette) pick(name string) error {
	text := string(p.editor("argument/" + name).Text)
	choices, err := p.fieldChoices(name)
	if hasChoice(choices, text) {
		return p.setArgument(name, text)
	}
	if matches := p.choiceMatches(name); len(matches) > 0 {
		p.choice = min(p.choice, len(matches)-1)
		return p.setArgument(name, matches[p.choice].ID)
	}
	if p.op.Parameters[name].Free || err != nil {
		return nil
	}
	return fmt.Errorf("%s: no choice matches %q", name, text)
}

// checkChoice rejects a drafted value outside a pick field's choices unless
// the parameter is Free or its source failed.
func (p *Palette) checkChoice(name, text string) error {
	param := p.op.Parameters[name]
	if param.Free || (len(param.Enum) == 0 && param.Source == "") {
		return nil
	}
	choices, err := p.fieldChoices(name)
	if err != nil || hasChoice(choices, text) {
		return nil
	}
	return choiceError(name, choices)
}

// writeChoices renders the focused pick field's filtered choices beneath it.
func (p *Palette) writeChoices(out *strings.Builder, name string, limit int) {
	_, err := p.fieldChoices(name)
	if err != nil {
		fmt.Fprintf(out, "    choices unavailable: %v\n", err)
	}
	matches := p.choiceMatches(name)
	if len(matches) == 0 {
		if err == nil {
			out.WriteString("    No matching choices\n")
		}
		return
	}
	selected := min(p.choice, len(matches)-1)
	start := max(0, selected-limit+1)
	for i := start; i < min(len(matches), start+limit); i++ {
		marker := "    "
		if i == selected {
			marker = "  * "
		}
		label := matches[i].Label
		if label != matches[i].ID {
			label += " [" + matches[i].ID + "]"
		}
		fmt.Fprintf(out, "%s%s\n", marker, label)
	}
	if hidden := len(matches) - min(len(matches), start+limit) + start; hidden > 0 {
		fmt.Fprintf(out, "    (%s more)\n", strconv.Itoa(hidden))
	}
}
