package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// SubjectFromAssociationSetting enables declared focus associations for people.
const SubjectFromAssociationSetting = "subject.from-association"

// Association is a live thing referenced by a declared association prop on
// the focus path. From is the referring instance; Via is its prop's name.
type Association struct {
	Noun  string `json:"noun"`
	Key   string `json:"key"`
	Label string `json:"label"`
	Via   string `json:"via"`
	From  string `json:"from"`
	id    string
}

// registerAssociations declares the association noun.
func registerAssociations(r *Runtime) {
	r.defineFacts(factsSpec{Noun: "ochazuke.association", Sample: Association{}, Key: "id", Label: "label", Extra: []PropSpec{{Name: "id"}},
		Docs: map[string]string{"id": "The association's place: referring instance, prop, noun and key", "noun": "The referenced thing's noun", "key": "The referenced thing's key",
			"label": "The referenced thing's label", "via": "The referring prop", "from": "The referring instance"},
		ListParams: map[string]Parameter{"noun": {Type: "string", Description: "Only associations of this noun"}},
		List: func(c Ctx, q ListQuery) ([]Thing, error) {
			rows := c.Runtime.Associations(focusOf(c.Context), StringArg(q.Args, "noun", ""))
			return factThings(rows, func(i int, t Thing) {
				t["id"] = strings.Join([]string{rows[i].From, rows[i].Via, rows[i].Noun, rows[i].Key}, "/")
			})
		}})
}

// Associations reads declared reference props from each instance's Data,
// innermost first and in prop order. It never reads a store. An empty noun
// includes every noun; missing or stale references are omitted.
func (r *Runtime) Associations(focus, noun string) []Association {
	noun = r.CanonicalNoun(noun)
	out := []Association{}
	for _, from := range r.Ancestry(focus) {
		out = r.associationsAt(out, from, noun)
	}
	return out
}

func (r *Runtime) associationsAt(out []Association, from, noun string) []Association {
	inst := r.Instances[from]
	if inst == nil {
		return out
	}
	n := r.Nouns[inst.Noun]
	if n == nil {
		return out
	}
	for _, p := range n.Props {
		if !p.Association || noun != "" && p.Noun != noun {
			continue
		}
		value := dataField(inst.Data, p.Name)
		var values []any
		switch v := value.(type) {
		case []any:
			values = v
		case []string:
			for _, key := range v {
				values = append(values, key)
			}
		default:
			values = []any{value}
		}
		for _, value := range values {
			if emptyProp(value) {
				continue
			}
			id := r.associationInstance(p.Noun, fmt.Sprint(value))
			if id == "" || slices.ContainsFunc(out, func(a Association) bool { return a.From == from && a.id == id }) {
				continue
			}
			t := r.instanceThing(r.Instances[id])
			out = append(out, Association{Noun: p.Noun, Key: t.Key, Label: t.Label, Via: p.Name, From: from, id: id})
		}
	}
	return out
}

func (r *Runtime) associationInstance(noun, key string) string {
	if id, err := r.TargetInstance(key); err == nil && r.Instances[id].Noun == noun {
		return id
	}
	var first *Instance
	for _, inst := range r.Instances {
		if inst.Noun == noun && (first == nil || inst.seq < first.seq) && r.instanceThing(inst).Key == key {
			first = inst
		}
	}
	if first != nil {
		return first.ID
	}
	return ""
}

// nearestOf reads each focus-path level itself before that level's
// associations. Several distinct candidates at the nearest level are an
// ambiguity, never an arbitrary choice. No associations are followed twice.
func (r *Runtime) nearestOf(noun, focus string) (string, []focusThing) {
	noun = r.CanonicalNoun(noun)
	for _, id := range r.Ancestry(focus) {
		if id != "app" && r.Instances[id].Noun == noun {
			return id, nil
		}
		if !r.Settings().Bool(SubjectFromAssociationSetting) {
			continue
		}
		candidates := r.associationsAt(nil, id, noun)
		if len(candidates) == 1 {
			return candidates[0].id, nil
		}
		if len(candidates) > 1 {
			things := make([]focusThing, len(candidates))
			for i, a := range candidates {
				things[i] = focusThing{Key: a.Key, Label: a.Label}
			}
			return "", things
		}
	}
	return "", nil
}

func (op Operation) byAssociation() bool {
	if op.ByAssociation {
		return true
	}
	for _, p := range op.Parameters {
		if p.Focus && !p.Required && p.Noun == op.Noun {
			return true
		}
	}
	return false
}

// associationSubject admits only declared verbs and human sources. Existing
// noun-path behavior wins; a program must name its target explicitly.
func (r *Runtime) associationSubject(op Operation, q Request, focus string) (string, error) {
	if q.Target != "" || !personSource(r.history.source) || !op.byAssociation() || r.nounInstance(op.Noun, focus) != "" {
		return "", nil
	}
	for name, p := range op.Parameters {
		if p.Focus && p.Noun == op.Noun && !emptyProp(q.Args[name]) {
			return "", nil // an explicit subject argument wins
		}
	}
	id, candidates := r.nearestOf(op.Noun, focus)
	if err := r.associationAmbiguity(op.Noun, candidates); err != nil {
		return "", err
	}
	return id, nil
}

func (r *Runtime) associationAmbiguity(noun string, candidates []focusThing) error {
	if len(candidates) == 0 {
		return nil
	}
	names := make([]string, len(candidates))
	for i, t := range candidates {
		names[i] = t.Label + " (" + t.Key + ")"
	}
	return fmt.Errorf("ambiguous %s association: %s", r.DisplayNoun(noun), strings.Join(names, ", "))
}

// notifyAssociation shows an implicit subject after a successful action
// unless a modal is already showing what it acts on.
func (r *Runtime) notifyAssociation(noun, id, modal string, err error) {
	if inst := r.Instances[id]; err == nil && inst != nil && r.TopModal() == modal {
		t := r.instanceThing(inst)
		r.UI().Notify(r.DisplayNoun(noun) + " " + t.Label + " (from the focus)")
	}
}
