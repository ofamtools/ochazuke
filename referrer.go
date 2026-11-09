package ochazuke

import (
	"sort"
	"strings"
	"sync/atomic"
)

// nounSeq numbers nouns as they are first registered, across runtimes: only
// the order of one runtime's nouns is read (Referrers).
var nounSeq atomic.Int64

// Referrer is one prop that names a noun's things: the noun that declares
// it, the prop, its role (RoleOwner for a prop that says the thing belongs
// to the referenced one, "" for a plain reference), whether the prop is
// read-only, and the label a section of those things carries (the plural
// of the declaring noun; a plain reference adds the prop in brackets).
// Related sections (AddRelated) are one per referrer.
type Referrer struct {
	Noun     string `json:"noun"`
	Prop     string `json:"prop"`
	Role     string `json:"role,omitempty"`
	ReadOnly bool   `json:"readOnly,omitempty"`
	Label    string `json:"label"`
}

// ID is the referrer's place, the section id: NOUN.PROP.
func (f Referrer) ID() string { return f.Noun + "." + f.Prop }

// Referrers lists every registered noun's prop that names a thing of noun
// (Parameter.Noun), owner or plain reference, nouns in registration order
// and each noun's props in declaration order. It is computed from the
// declared props, never from a table, so a noun that adds a reference prop
// is listed with no other change. An empty noun lists every reference of
// every noun. A computed or collection-scope prop is never a reference.
func (r *Runtime) Referrers(noun string) []Referrer {
	if noun != "" {
		noun = r.CanonicalNoun(noun)
	}
	nouns := make([]*Noun, 0, len(r.Nouns))
	for _, n := range r.Nouns {
		nouns = append(nouns, n)
	}
	sort.Slice(nouns, func(i, j int) bool { return nouns[i].seq < nouns[j].seq })
	out := []Referrer{}
	for _, n := range nouns {
		for _, p := range n.Props {
			if p.Noun == "" || noun != "" && p.Noun != noun || p.Read != nil || p.Subject != "" {
				continue
			}
			label := r.choicesPlural(Parameter{Noun: n.Name})
			if p.Role == "" {
				label += " (" + p.Name + ")"
			}
			out = append(out, Referrer{Noun: n.Name, Prop: p.Name, Role: p.Role, ReadOnly: p.ReadOnly, Label: strings.ToUpper(label[:1]) + label[1:]})
		}
	}
	return out
}

// referrerNoun is the fact noun that lists the referrers.
const referrerNoun = "ochazuke.referrer"

// registerReferrers declares the referrer noun: list {noun} answers the
// props that name that noun's things (all of them without a noun), so
// stories, agents and the palette can see which sections a thing has.
func registerReferrers(r *Runtime) {
	r.defineFacts(factsSpec{Noun: referrerNoun, Sample: Referrer{}, Key: "id", Label: "label", Extra: []PropSpec{{Name: "id"}},
		Docs: map[string]string{"id": "The referring prop's place: NOUN.PROP", "noun": "The noun that declares the prop", "prop": "The referring prop",
			"role": "The prop's role (owner: the thing belongs to the referenced one)", "readOnly": "Whether set refuses to write the prop",
			"label": "The heading of a section of the referring noun's things"},
		ListParams: map[string]Parameter{"noun": {Type: "string", Description: "Only props that name this noun's things", Noun: ""}},
		List: func(c Ctx, q ListQuery) ([]Thing, error) {
			rows := c.Runtime.Referrers(StringArg(q.Args, "noun", ""))
			return factThings(rows, func(i int, t Thing) { t["id"] = rows[i].ID() })
		}})
}
