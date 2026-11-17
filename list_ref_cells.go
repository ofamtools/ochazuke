package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// Reference cells (run plan 5.6): a list of a noun's things (an index, an
// index tree, any list From a store) draws a reference prop's cell as the
// label of the thing it names, not its key. The things every reference
// column names are read once per fill, one Query per referenced noun when
// its store has one (readRefs, the batch read the label derivation uses),
// and kept with the batch's other things (Ctx.refs), so a fill that read
// them already (an index tree's parents) reads nothing more. A thing that
// cannot be read, or a store that answers from another process (Remote:
// reading it would start one per fill), shows its key. A prop with its
// own Cell draws that. index.ref-labels off shows the stored value.

// IndexRefLabelsSetting turns reference labels in list cells on (the
// default) or off.
const IndexRefLabelsSetting = "index.ref-labels"

// refCellLabels answers, per column prop, how a value of a reference prop
// reads as a cell (nil for a column that is not one), its things read for
// the batch first.
func (r *Runtime) refCellLabels(c Ctx, props []*PropSpec, things []Thing) []func(any) string {
	out := make([]func(any) string, len(props))
	if len(things) == 0 || !r.Settings().Bool(IndexRefLabelsSetting) {
		return out
	}
	got := c.refs
	if got == nil {
		got = map[string]Thing{}
	}
	if c.Runtime == nil {
		c.Runtime = r
	}
	for i, p := range props {
		if p == nil || p.Cell != nil {
			continue
		}
		nouns := r.refCellNouns(*p)
		if len(nouns) == 0 {
			continue
		}
		want := map[string][]string{}
		for _, t := range things {
			for _, text := range refCellValues(t[p.Name]) {
				ref := refCellRef(nouns, text)
				if !slices.Contains(want[ref.Noun], ref.Key) {
					want[ref.Noun] = append(want[ref.Noun], ref.Key)
				}
			}
		}
		for _, noun := range nouns {
			if len(want[noun]) > 0 {
				readRefs(c, r.Nouns[noun], want[noun], got)
			}
		}
		out[i] = func(v any) string {
			texts := refCellValues(v)
			words := make([]string, len(texts))
			for j, text := range texts {
				ref := refCellRef(nouns, text)
				words[j] = refLabel(r.Nouns[ref.Noun], ref.Key, got)
			}
			return strings.Join(words, ", ")
		}
	}
	return out
}

// refCellNouns are the nouns a reference prop's cell reads labels from:
// its declared nouns that have a local store with a key; none for a prop
// that is not a reference.
func (r *Runtime) refCellNouns(p PropSpec) []string {
	var out []string
	for _, name := range declaredRef(p).refNouns() {
		n := r.Nouns[r.CanonicalNoun(name)]
		if n == nil || n.Store == nil || n.Store.Remote || n.keyProp() == nil {
			continue
		}
		out = append(out, n.Name)
	}
	return out
}

// refCellRef is the thing a stored reference text names among nouns: the
// key itself when the prop names one noun, else the "noun:key" form of one
// of them (a bare key is the first noun's).
func refCellRef(nouns []string, text string) Ref {
	if len(nouns) > 1 {
		if ref, ok := ParseRef(text); ok && slices.Contains(nouns, ref.Noun) {
			return ref
		}
	}
	return Ref{Noun: nouns[0], Key: text}
}

// refCellValues are the references a cell's value holds: one, or each of a
// list's, empty ones left out.
func refCellValues(v any) []string {
	var out []string
	add := func(x any) {
		if !emptyProp(x) {
			out = append(out, fmt.Sprint(x))
		}
	}
	switch t := v.(type) {
	case []string:
		for _, x := range t {
			add(x)
		}
	case []any:
		for _, x := range t {
			add(x)
		}
	default:
		add(v)
	}
	return out
}
