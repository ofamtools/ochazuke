package ochazuke

import (
	"cmp"
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
//
// Inside a form (a RecordEditor), while text.noun-refs-fields is on, "#"
// also offers the form's other fields (not Hidden, ReadOnly, Secret, chips
// or lists), listed first with their current value, and a field shadows a
// noun of the same name. "#field:" lists that field's own choices, read as
// its picker reads them (its Enum, or its Source with the siblings'
// values), then "use TYPED" for a free field or Create "TYPED"… for a
// reference whose noun creates. Accepting one sets the field through the
// form's set path (written at once, one undo step, dependent fields
// refreshed and ClearStale applied) and takes the token out of the text
// (Suggestion.Act).

// NounRefsSetting turns on "#" noun references in every text input.
const NounRefsSetting = "text.noun-refs"

// NounRefsMatchSetting is how "#" narrows its lists, the names after
// "#" and the values after "#name:": fuzzy (the default), substring or
// regex, the modes of search.match, ranked by the Matcher's score.
const NounRefsMatchSetting = "text.noun-refs-match"

// NounRefFieldsSetting lets "#" in a form's text field offer and set the
// form's other fields (with text.noun-refs on).
const NounRefFieldsSetting = "text.noun-refs-fields"

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
	id := in.ID
	load := func(r *Runtime, q CompletionQuery) Result { return r.nounRefLoad(id, q) }
	act := func(r *Runtime, s Suggestion) (string, error) { return r.nounRefAct(id, s) }
	return append(slices.Clone(own), CompletionTrigger{Trigger: NounRefTrigger, Quoted: true, Load: load, Make: nounRefMake, Act: act})
}

// nounRefAct sets the field a "#field:value" suggestion names, in the form
// input id is a field of, to its Act; a Create row (Make) makes the thing
// through the field's noun first and sets the field to it. With no such
// field (the form closed meanwhile) the reference stays in the text.
func (r *Runtime) nounRefAct(id string, s Suggestion) (string, error) {
	ref, _ := ParseRef(s.Text)
	e, fields := r.nounRefFields(id)
	for _, f := range fields {
		if f.Name != ref.Noun {
			continue
		}
		v := s.Act
		if s.Make != "" {
			made, err := r.createNamed(f.Noun, s.Make)
			if err != nil {
				return "", err
			}
			v = made
		}
		return "", e.setByRef(f, v)
	}
	return s.Text, nil
}

// nounRefLoad answers a "#" token in input id: the form's fields and the
// nouns before a colon; after it, that field's choices or that noun's
// things.
func (r *Runtime) nounRefLoad(id string, q CompletionQuery) Result {
	e, fields := r.nounRefFields(id)
	ref, typed := ParseRef(q.Query)
	if !typed {
		rows, names := e.nounRefFieldRows(fields, q.Query)
		for _, row := range r.nounRefNouns(q.Query) {
			if !names[row.Label] {
				rows = append(rows, row)
			}
		}
		return Done(rows)
	}
	for _, f := range fields {
		if f.Name == ref.Noun {
			return e.nounRefFieldValues(f, unquoteRef(ref.Key))
		}
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

// nounRefNouns are the nouns with a picker whose name matches typed
// (nounRefRank).
func (r *Runtime) nounRefNouns(typed string) []Suggestion {
	var rows []Suggestion
	for _, d := range r.PickerDefs() {
		if d.Noun != "" {
			rows = append(rows, Suggestion{Text: Ref{Noun: d.Name}.String(), Label: d.Name, Detail: d.About, Continue: true})
		}
	}
	return r.nounRefRank(typed, rows, func(s Suggestion) []string { return []string{s.Label} })
}

// nounRefRank keeps the rows one of whose texts matches typed in
// text.noun-refs-match's mode, the best score first (ties keep their
// order).
func (r *Runtime) nounRefRank(typed string, rows []Suggestion, texts func(Suggestion) []string) []Suggestion {
	m := r.Matcher(r.matchSetting(NounRefsMatchSetting, MatchFuzzy), typed)
	type scored struct {
		row   Suggestion
		score int
	}
	var kept []scored
	for _, row := range rows {
		best, ok := 0, false
		for _, text := range texts(row) {
			if s, hit := m.ScorePrepared(m.Prepare(text)); hit && (!ok || s > best) {
				best, ok = s, true
			}
		}
		if ok {
			kept = append(kept, scored{row, best})
		}
	}
	slices.SortStableFunc(kept, func(a, b scored) int { return cmp.Compare(b.score, a.score) })
	out := make([]Suggestion, len(kept))
	for i, k := range kept {
		out[i] = k.row
	}
	return out
}

// nounRefThings are the things of a picker's noun whose label matches
// typed (nounRefRank), then Create "TYPED"… when the noun
// creates.
func (r *Runtime) nounRefThings(def PickerDef, name, typed string) Result {
	noun := r.CanonicalNoun(def.Noun)
	creates := typed != "" && r.pickerCreates(noun)
	ref := func(label string) string { return Ref{Noun: name, Key: quoteRef(label)}.String() }
	build := func(choices []Choice) []Suggestion {
		var rows []Suggestion
		for _, ch := range choices {
			label := ch.Label
			if label == "" {
				label = ch.ID
			}
			rows = append(rows, Suggestion{Text: ref(label), Label: label})
		}
		out := r.nounRefRank(typed, rows, func(s Suggestion) []string { return []string{s.Label} })
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

// nounRefFields are the fields "#" in input id offers: the other fields of
// the form it is a field of, less the Hidden, ReadOnly, Secret, chips and
// list ones; none outside a form or with text.noun-refs-fields off.
func (r *Runtime) nounRefFields(id string) (*RecordEditor, []*recordField) {
	if !r.helpFlag(NounRefFieldsSetting, true) {
		return nil, nil
	}
	e := r.recordEditorOf(id)
	if e == nil || e.closed || e.fieldAt(id) == nil {
		return nil, nil
	}
	var out []*recordField
	for _, f := range e.fields {
		if f.id == id || f.skipped() || f.Secret || f.chips || f.list {
			continue
		}
		out = append(out, f)
	}
	return e, out
}

// nounRefFieldRows are the fields whose name matches typed (nounRefRank),
// each with its value as detail, and the set of every
// offered field's name (a noun of that name is shadowed).
func (e *RecordEditor) nounRefFieldRows(fields []*recordField, typed string) ([]Suggestion, map[string]bool) {
	names := map[string]bool{}
	if e == nil {
		return nil, names
	}
	var rows []Suggestion
	for _, f := range fields {
		names[f.Name] = true
		rows = append(rows, Suggestion{Text: Ref{Noun: f.Name}.String(), Label: f.Name, Detail: e.nounRefValueLabel(f), Continue: true})
	}
	return e.r.nounRefRank(typed, rows, func(s Suggestion) []string { return []string{s.Label} }), names
}

// nounRefValueLabel is a field's value as the "#" list shows it: its
// choice's label, the text, or (none).
func (e *RecordEditor) nounRefValueLabel(f *recordField) string {
	v := f.text()
	if v == "" {
		return "(none)"
	}
	for _, c := range f.choices {
		if c.ID == v && c.Label != "" {
			return c.Label
		}
	}
	return v
}

// nounRefFieldValues are a field's choices whose label or id matches
// typed (nounRefRank; a source that takes the query has narrowed them
// itself), read as its picker reads them; then "use TYPED"
// for a field that takes free text, or Create "TYPED"… for a reference
// whose noun creates. Each sets the field when accepted.
func (e *RecordEditor) nounRefFieldValues(f *recordField, typed string) Result {
	r := e.r
	ref := func(label string) string { return Ref{Noun: f.Name, Key: quoteRef(label)}.String() }
	narrow := !r.ChoicesTakeQuery(f.Parameter)
	build := func(choices []Choice) []Suggestion {
		var out []Suggestion
		for _, ch := range choices {
			label := ch.Label
			if label == "" {
				label = ch.ID
			}
			row := Suggestion{Text: ref(label), Label: label, Act: ch.ID}
			if label != ch.ID {
				row.Detail = ch.ID
			}
			out = append(out, row)
		}
		if narrow {
			// The label, then the id the detail shows.
			out = r.nounRefRank(typed, out, func(s Suggestion) []string { return []string{s.Label, s.Detail} })
		}
		if typed == "" || hasChoice(choices, typed) {
			return out
		}
		switch {
		case !f.choose:
			out = append(out, Suggestion{Text: ref(typed), Label: "use " + strconv.Quote(typed), Act: typed})
		case r.pickerCreates(f.Noun):
			out = append(out, Suggestion{Text: ref(typed), Label: "Create " + strconv.Quote(typed) + "…", Act: typed, Make: typed})
		}
		return out
	}
	if f.Type == "boolean" && !paramHasChoices(f.Parameter) {
		return Done(build(booleanChoices()))
	}
	if !paramHasChoices(f.Parameter) {
		return Done(build(nil))
	}
	res := r.choicesLoad(r.contextOf(f.id), f.Parameter, typed, e.arguments())
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
