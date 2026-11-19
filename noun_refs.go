package ochazuke

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Noun references in text: while text.noun-refs is on, every input that
// takes text (not a Secret or ReadOnly one) completes a "#" token. "#"
// lists the nouns that have a picker (PickerDefs naming a noun: the
// derived noun pickers and the registered ones); picking one leaves
// "#noun:" and lists that noun's things, from the source its picker uses
// (loaded off the loop when the source is Pending). The last row, for a
// noun whose pickers create (pickerCreates), is Create "TYPED"…, which
// makes the thing through the noun's standard create (createNamed, the
// form's create row) and inserts its reference. A reference is
// "#noun:Label", the label quoted when it holds a space
// (#project:"North Star"): the Ref text form (Ref.String, ParseRef) with
// the label a person reads in place of the key; ResolveRef reads a label
// back to the thing. An input's own triggers win: "#" is not added beside
// a trigger that is "#" or completes every token.

// NounRefsSetting turns on "#" noun references in every text input.
const NounRefsSetting = "text.noun-refs"

// NounRefTrigger is the trigger of a noun reference.
const NounRefTrigger = "#"

// nounRefTriggers are an input's triggers with the noun references' added
// when text.noun-refs is on and the input takes it.
func (r *Runtime) nounRefTriggers(in *Input, own []CompletionTrigger) []CompletionTrigger {
	if in.Secret || in.ReadOnly || !r.Settings().Bool(NounRefsSetting) {
		return own
	}
	if slices.ContainsFunc(own, func(t CompletionTrigger) bool { return t.Every || t.Trigger == NounRefTrigger }) {
		return own
	}
	return append(slices.Clone(own), CompletionTrigger{Trigger: NounRefTrigger, Quoted: true, Load: nounRefLoad, Make: nounRefMake})
}

// nounRefLoad answers a "#" token: the nouns before a colon, that noun's
// things after it.
func nounRefLoad(r *Runtime, q CompletionQuery) Result {
	ref, typed := ParseRef(q.Query)
	if !typed {
		return Done(r.nounRefNouns(q.Query))
	}
	def, ok := r.PickerDef(ref.Noun)
	if !ok || def.Noun == "" {
		return Done([]Suggestion(nil))
	}
	return r.nounRefThings(def, ref.Noun, unquoteRef(ref.Key))
}

// nounRefMake creates the thing a Create row names, of the noun its text
// names, through the noun's standard create, and answers its reference.
func nounRefMake(r *Runtime, s Suggestion) (string, error) {
	ref, _ := ParseRef(s.Text)
	name := ref.Noun
	noun := r.CanonicalNoun(name)
	if def, ok := r.PickerDef(name); ok && def.Noun != "" {
		noun = r.CanonicalNoun(def.Noun)
	}
	if _, err := r.createNamed(noun, s.Make); err != nil {
		return "", err
	}
	return s.Text, nil
}

// nounRefNouns are the nouns with a picker whose name holds typed, the
// ones it begins first.
func (r *Runtime) nounRefNouns(typed string) []Suggestion {
	cm := r.CaseFor(typed)
	var first, more []Suggestion
	for _, d := range r.PickerDefs() {
		if d.Noun == "" || !cm.Contains(d.Name, typed) {
			continue
		}
		row := Suggestion{Text: Ref{Noun: d.Name}.String(), Label: d.Name, Detail: d.About, Continue: true}
		if cm.HasPrefix(d.Name, typed) {
			first = append(first, row)
		} else {
			more = append(more, row)
		}
	}
	return append(first, more...)
}

// nounRefThings are the things of a picker's noun whose label holds
// typed, then Create "TYPED"… when the noun creates.
func (r *Runtime) nounRefThings(def PickerDef, name, typed string) Result {
	noun := r.CanonicalNoun(def.Noun)
	cm := r.CaseFor(typed)
	creates := typed != "" && r.pickerCreates(noun)
	ref := func(label string) string { return Ref{Noun: name, Key: quoteRef(label)}.String() }
	build := func(choices []Choice) []Suggestion {
		var out []Suggestion
		for _, ch := range choices {
			label := ch.Label
			if label == "" {
				label = ch.ID
			}
			if !cm.Contains(label, typed) {
				continue
			}
			row := Suggestion{Text: ref(label), Label: label}
			if ch.ID != label && !ch.Bare {
				row.Detail = ch.ID
			}
			out = append(out, row)
		}
		if creates {
			out = append(out, Suggestion{Text: ref(typed), Label: "Create " + strconv.Quote(typed) + "…", Make: typed})
		}
		return out
	}
	res := r.choicesLoad(r.Context(), pickerParam(def, def.Source), typed)
	if res.work == nil {
		if res.err != nil {
			return res
		}
		choices, _ := res.value.([]Choice)
		return Done(build(choices))
	}
	return res.bare().Map(func(value any, err error) (any, error) {
		if err != nil {
			return nil, err
		}
		choices, _ := value.([]Choice)
		return build(choices), nil
	})
}

// quoteRef is a label as a reference writes it: quoted when it holds
// whitespace or a quote.
func quoteRef(label string) string {
	if strings.ContainsFunc(label, func(r rune) bool { return unicode.IsSpace(r) || r == '"' }) {
		return strconv.Quote(label)
	}
	return label
}

// unquoteRef is the label typed after "noun:", its quotes taken off (the
// closing one may not be typed yet).
func unquoteRef(text string) string {
	if !strings.HasPrefix(text, `"`) {
		return text
	}
	if s, err := strconv.Unquote(text); err == nil {
		return s
	}
	return strings.TrimSuffix(strings.TrimPrefix(text, `"`), `"`)
}
