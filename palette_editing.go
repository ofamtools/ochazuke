package ochazuke

import (
	"errors"
	"fmt"
	"strings"
)

// filterCodes are the words that restrict the completion list instead of
// becoming part of the command.
var filterCodes = map[string]bool{":v": true, ":n": true, ":p": true}

// sync follows the line after an edit: a filter code typed as the current
// word is consumed into the filter, a filter lapses when the line moves to
// another word, an edit clears the highlight and leaves history recall, and choices are
// read again (sources are live).
func (p *Palette) sync() {
	if p.line == nil {
		return
	}
	text := p.Text()
	tokens, trailing := tokenize(text)
	if !trailing && filterCodes[tokens[len(tokens)-1].Text] && strings.HasSuffix(text, tokens[len(tokens)-1].Text) {
		p.filter = strings.TrimPrefix(tokens[len(tokens)-1].Text, ":")
		p.filterAt = len(tokens) - 1
		end := len(p.line.Text)
		if err := p.line.Replace(end-2, end, ""); err == nil {
			text = p.Text()
			tokens, trailing = tokenize(text)
		}
	}
	position := len(tokens)
	if !trailing {
		position--
	}
	if position != p.filterAt {
		p.filter = ""
	}
	if text != p.seen {
		p.seen = text
		p.highlight = -1
		p.message = ""
		p.choiceCache = nil
		p.recall = 0
	}
}

// recallLines are the command lines the palette ran, newest first, from
// the action history: entries with the source "palette" that no other
// palette entry encloses (a source a request reads while it runs is
// recorded too, one level deeper), spelled back as lines.
func (p *Palette) recallLines() []string {
	entries := p.r.History(0)
	var lines []string
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Context.Source != "palette" || p.actions[e.Request.Key()] {
			continue
		}
		nested := false
		for j := i + 1; j < len(entries); j++ {
			if entries[j].Depth < e.Depth {
				nested = entries[j].Context.Source == "palette"
				break
			}
		}
		if nested {
			continue
		}
		line := p.commandText(e.Request)
		if len(lines) == 0 || lines[len(lines)-1] != line {
			lines = append(lines, line)
		}
	}
	return lines
}

// commandText spells a request as a line: verb, noun, then its arguments
// in parameter order up to the first one it omits.
func (p *Palette) commandText(q Request) string {
	words := []string{q.Verb, p.r.DisplayNoun(q.Noun)}
	for _, name := range paramOrder(p.r.Operations[p.r.CanonicalKey(q.Key())]) {
		v, ok := q.Args[name]
		if !ok {
			break
		}
		words = append(words, quoteWord(fmt.Sprint(v)))
	}
	return strings.Join(words, " ")
}

// recallLine steps through the palette's history: delta 1 is older, -1
// newer; stepping past the newest restores the line being typed.
func (p *Palette) recallLine(delta int) error {
	if err := p.requireOpen(); err != nil {
		return err
	}
	lines := p.recallLines()
	if len(lines) == 0 {
		return errors.New("no palette history")
	}
	if p.recall == 0 {
		p.draft = p.Text()
	}
	step := min(max(p.recall+delta, 0), len(lines))
	text := p.draft
	if step > 0 {
		text = lines[step-1]
	}
	if err := p.setText(text); err != nil {
		return err
	}
	p.recall = step
	return nil
}
