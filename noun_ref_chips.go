package ochazuke

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"unicode"
)

// Noun reference chips and links. While text.noun-refs is on, every
// reference "#noun:Label" in a text input whose noun has a picker is a
// chip: drawn in the chip tone (ToneChip, the tone of a form's chips) over
// its own cells while text.noun-ref-chips is on, and erased whole by
// backspace at its end (delete at its start). The text keeps the
// reference as written, so undo, yanks, the CLI and a stored value read
// it. The token the completion list is open on is not a chip yet.
//
// A record editor whose RecordSpec names its Subject links on each write:
// a reference the written text gains is related from the subject to the
// thing it names (Subject's NOUN.relate, kind NounRefKind), and one it
// loses, held by none of the editor's other fields, is unrelated. A
// reference that names no one thing (a label two things share, a thing
// gone) links nothing. A field outside a saved thing (a palette line, a
// filter, a form with no Subject) links nothing.

// NounRefChipsSetting draws noun references in text inputs as chips
// (with text.noun-refs on).
const NounRefChipsSetting = "text.noun-ref-chips"

// NounRefKind is the relation kind a noun reference links by: the thing
// whose text holds the reference mentions the thing it names.
const NounRefKind = "mentions"

// NounRefSpan is one noun reference in a text: its rune range [Start,
// End) and what it names (Ref.Key is the label as typed, unquoted; the
// noun is canonical).
type NounRefSpan struct {
	Start, End int
	Ref        Ref
}

// NounRefsIn are the noun references in text, in order: tokens
// "#noun:Label" at a word start, the label possibly quoted, whose noun
// (or picker name) has a picker.
func (r *Runtime) NounRefsIn(text []rune) []NounRefSpan {
	if !strings.ContainsRune(string(text), '#') {
		return nil
	}
	defs := r.PickerDefs()
	nounOf := func(name string) string {
		canon := r.CanonicalNoun(name)
		for _, d := range defs {
			if d.Noun != "" && (d.Name == name || r.CanonicalNoun(d.Noun) == canon) {
				return r.CanonicalNoun(d.Noun)
			}
		}
		return ""
	}
	trigger := []rune(NounRefTrigger)
	var out []NounRefSpan
	for i := 0; i < len(text); i++ {
		if text[i] != trigger[0] || i > 0 && !unicode.IsSpace(text[i-1]) {
			continue
		}
		start, end := quotedSpan(text, i+1, trigger, unicode.IsSpace)
		if start != i {
			continue
		}
		ref, typed := ParseRef(string(text[i+len(trigger) : end]))
		label := unquoteRef(ref.Key)
		if noun := nounOf(ref.Noun); typed && label != "" && noun != "" {
			out = append(out, NounRefSpan{Start: start, End: end, Ref: Ref{Noun: noun, Key: label}})
		}
		i = end
	}
	return out
}

// nounRefChips are the chips an input draws: its references while both
// settings are on, less the token the completion list is open on.
func (in *Input) nounRefChips() []NounRefSpan {
	r := in.r
	if r == nil || in.Secret || !r.Settings().Bool(NounRefsSetting) || !r.helpFlag(NounRefChipsSetting, true) {
		return nil
	}
	spans := r.NounRefsIn(in.Text)
	if c := r.Completer(); c != nil && c.open && c.input == in.ID {
		for i, s := range spans {
			if s.Start == c.token.Start {
				spans = append(spans[:i:i], spans[i+1:]...)
				break
			}
		}
	}
	return spans
}

// nounRefChipAt is the chip an erase in direction removes whole: the one
// ending at the cursor (backward) or starting at it (forward).
func (in *Input) nounRefChipAt(direction string) (NounRefSpan, bool) {
	for _, s := range in.nounRefChips() {
		if direction == "backward" && s.End == in.Cursor || direction != "backward" && s.Start == in.Cursor {
			return s, true
		}
	}
	return NounRefSpan{}, false
}

// chipPainter walks an input's chips as it is drawn, rune by rune.
type chipPainter struct {
	spans       []NounRefSpan
	open, close string
}

func (in *Input) chipPainter() *chipPainter {
	p := &chipPainter{spans: in.nounRefChips()}
	if len(p.spans) > 0 {
		p.open, p.close = in.chipEscapes()
	}
	return p
}

// at is what goes before the rune at i: the chip's opening when one
// starts there.
func (p *chipPainter) at(i int) string {
	for len(p.spans) > 0 && p.spans[0].End <= i {
		p.spans = p.spans[1:]
	}
	if len(p.spans) > 0 && p.spans[0].Start == i {
		return p.open
	}
	return ""
}

// past is what goes after the runes before end: the chip's closing when
// one ends there.
func (p *chipPainter) past(end int) string {
	if len(p.spans) > 0 && p.spans[0].End == end {
		p.spans = p.spans[1:]
		return p.close
	}
	return ""
}

// chipEscapes are the sequences around a chip in a drawn input: the chip
// tone's, closed back to the field tone; underline without a theme.
func (in *Input) chipEscapes() (open, close string) {
	styles := in.r.toneStyles(in.ID)
	if styles != nil && styles.hasTone(ToneChip) {
		if open = toneOpen(styles, ToneChip); open != "" {
			field := ""
			if styles.hasTone(ToneInputField) && styles.Tone(ToneInputField).GetBackground() != nil {
				field = toneOpen(styles, ToneInputField)
			}
			return open, "\x1b[m" + field
		}
	}
	return "\x1b[4m", "\x1b[24m"
}

// toneOpen is the sequence a tone's style starts its text with ("" when
// it draws nothing).
func toneOpen(styles *Styles, tone string) string {
	const mark = "\x00"
	open, _, ok := strings.Cut(styles.Tone(tone).Render(mark), mark)
	if !ok {
		return ""
	}
	return open
}

// nounRefKey is the key of the one thing a reference names (its label or
// key, through keyResolvers); false when none or several do.
func (r *Runtime) nounRefKey(c Ctx, ref Ref) (string, bool) {
	n := r.Noun(ref.Noun)
	if n == nil || !n.resolvable() {
		return "", false
	}
	for _, res := range keyResolvers {
		keys, err := res.find(n, c, ref.Key)
		if err != nil || len(keys) > 1 {
			return "", false
		}
		if len(keys) == 1 {
			return keys[0], true
		}
	}
	return "", false
}

// nounRefTargets are the things text's references name, as "noun:key".
func (r *Runtime) nounRefTargets(c Ctx, text string) map[string]bool {
	out := map[string]bool{}
	for _, s := range r.NounRefsIn([]rune(text)) {
		if key, ok := r.nounRefKey(c, s.Ref); ok {
			out[Ref{Noun: s.Ref.Noun, Key: key}.String()] = true
		}
	}
	return out
}

// linkNounRefs relates subject to the things after names that before did
// not, and unrelates the ones before named that after and kept do not,
// through subject's relate and unrelate verbs (kind NounRefKind).
func (r *Runtime) linkNounRefs(subject Ref, before, after string, kept []string) {
	n := r.Noun(subject.Noun)
	if n == nil || n.keyProp() == nil || r.RelationStore() == nil {
		return
	}
	if _, ok := r.Operation(subject.Noun + ".relate"); !ok {
		return
	}
	c := Ctx{Runtime: r, Context: r.Context()}
	was, now := r.nounRefTargets(c, before), r.nounRefTargets(c, after)
	held := maps.Clone(now)
	for _, text := range kept {
		maps.Copy(held, r.nounRefTargets(c, text))
	}
	self := subject.String()
	run := func(verb, to string) {
		_, err := r.Execute(Request{Noun: subject.Noun, Verb: verb, Args: Arguments{n.keyProp().Name: subject.Key, relationToParam: to, relationKindParam: NounRefKind}})
		var arg *ArgumentError
		if err != nil && !(verb == "unrelate" && errors.As(err, &arg)) {
			r.UI().Error(err.Error())
		}
	}
	for _, ref := range slices.Sorted(maps.Keys(now)) {
		if !was[ref] && ref != self {
			run("relate", ref)
		}
	}
	for _, ref := range slices.Sorted(maps.Keys(was)) {
		if !held[ref] {
			run("unrelate", ref)
		}
	}
}
