package ochazuke

import (
	"slices"
	"strings"
)

// AnyNoun is the Parameter.Noun of a reference to a thing of any noun (a
// note's "about", a tag's target, a comment's subject). The prop holds the
// noun and the key together, "noun:key" (AnyRef): the key may hold colons
// and slashes (a path), so the value splits at the first colon only
// (SplitAnyRef). Every noun with a picker (a derived noun picker, or a
// declared one) can be the target; the chooser asks for the noun, then for
// the thing. A prop that names AnyNoun is Free (props.go): it is never
// refused for naming a noun or a thing that does not exist, the reference
// is kept as written (record first, never refuse).
const AnyNoun = "*"

// AnyRef is the stored value of a reference to the thing key of noun.
func AnyRef(noun, key string) string { return noun + ":" + key }

// SplitAnyRef splits a stored any-noun reference at its first colon: the
// noun and the key (which may hold colons). ok is false when there is no
// noun before a colon.
func SplitAnyRef(value string) (noun, key string, ok bool) {
	noun, key, ok = strings.Cut(value, ":")
	if !ok || noun == "" {
		return "", "", false
	}
	return noun, key, true
}

// AnyRefNounsSetting says which nouns the any-noun chooser offers: "stored"
// (the default) the application's nouns with a store, not the framework's
// own (registered inside Builtin) and not a facts store (Store.Facts, a
// read-only view), "pickers" every noun with a picker.
const AnyRefNounsSetting = "anyref.nouns"

// anyRefNouns are the nouns an any-noun reference can name: those with a
// picker (and, for "stored", a store that is not a facts store), by name.
func (r *Runtime) anyRefNouns() []string {
	stored := r.Settings().String(AnyRefNounsSetting) != "pickers"
	var out []string
	for _, d := range r.PickerDefs() {
		if d.Noun == "" {
			continue
		}
		noun := r.CanonicalNoun(d.Noun)
		if n := r.Nouns[noun]; stored && (n == nil || n.Builtin || n.Store == nil || n.Store.Facts) {
			continue
		}
		if !slices.Contains(out, noun) {
			out = append(out, noun)
		}
	}
	slices.Sort(out)
	return out
}

// anyRefChoices are the choices of an any-noun reference for the text
// typed so far. Without a colon the choices are the nouns (Opens, NoPick:
// the picker browses into one, the query becoming "noun:"); with
// "noun:text" they are that noun's things whose label or key holds text,
// each as the stored value "noun:key". A noun that is not a target gives
// no choices.
func (r *Runtime) anyRefChoices(c Context, query string) ([]Choice, error) {
	word, rest, typed := strings.Cut(query, ":")
	if !typed {
		var out []Choice
		for _, noun := range r.anyRefNouns() {
			label := r.DisplayNoun(noun)
			keywords := r.NounKeywords(noun)
			if !strings.Contains(strings.ToLower(label+" "+noun+" "+strings.Join(keywords, " ")), strings.ToLower(strings.TrimSpace(word))) {
				continue
			}
			out = append(out, Choice{ID: noun + ":", Label: label, Word: noun, Keywords: keywords, RowFlags: RowFlags{Opens: true, NoPick: true, Bare: true}})
		}
		return out, nil
	}
	noun := r.CanonicalNoun(strings.TrimSpace(word))
	if !slices.Contains(r.anyRefNouns(), noun) {
		return nil, nil
	}
	things, err := r.ChoicesFor(c, Parameter{Noun: noun}, rest)
	if err != nil {
		return nil, err
	}
	rest = strings.ToLower(strings.TrimSpace(rest))
	var out []Choice
	for _, ch := range things {
		if rest != "" && !strings.Contains(strings.ToLower(ch.Label+" "+ch.ID), rest) {
			continue
		}
		ch.ID = AnyRef(noun, ch.ID)
		out = append(out, ch)
	}
	return out, nil
}

// anyRefNavigation gives a chooser source over an any-noun reference its
// browsing: enter on a noun opens it, and up goes back to the nouns.
func anyRefNavigation(src *ChooserSource) {
	src.Open = func(row ChooserRow) (string, bool) { return row.ID, row.Opens }
	src.Up = func(query string) (string, bool) { return "", strings.Contains(query, ":") }
}
