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
	for _, e := range r.liveRefs(r.Instances[from]) {
		if noun != "" && e.Noun != noun {
			continue
		}
		id := r.associationInstance(e.Prop.Noun, e.Key)
		if id == "" || slices.ContainsFunc(out, func(a Association) bool { return a.From == from && a.id == id }) {
			continue
		}
		t := r.instanceThing(r.Instances[id])
		out = append(out, Association{Noun: e.Prop.Noun, Key: t.Key, Label: t.Label, Via: e.Prop.Name, From: from, id: id})
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

// nearestOf reads each focus-path level itself before what that level
// reaches through relations (graph_reach.go), nearest distance first. Several
// distinct things at the nearest distance, and a thing with no live instance
// to act on, are candidates to choose from, never an arbitrary choice.
func (r *Runtime) nearestOf(noun, focus string) (string, []focusThing) {
	return r.nearestWithin(noun, focus, true)
}

// nearestWithin is nearestOf; distant says whether one live thing more than
// one hop away may be taken without asking. A verb that changes something
// passes false, so a distant thing is only ever offered as a candidate.
func (r *Runtime) nearestWithin(noun, focus string, distant bool) (id string, things []focusThing) {
	id, things, _ = r.nearestDetail(noun, focus, distant)
	return id, things
}

// nearestDetail is nearestWithin plus whether the candidates are the several
// live things one hop away that always refused to pick one (an ambiguity). Any
// other candidate list (a stored thing, a distant thing under a change verb)
// is only something to offer: the request goes on with no subject, as before
// the graph walk, and the ask picker lists it.
func (r *Runtime) nearestDetail(noun, focus string, distant bool) (id string, things []focusThing, ambiguous bool) {
	noun = r.CanonicalNoun(noun)
	if c := r.renderCache.collecting; c != nil {
		defer func() { c.reaches = append(c.reaches, reachDependency{noun, focus, distant, reachAnswer(id, things)}) }()
	}
	for _, level := range r.Ancestry(focus) {
		if level != "app" && r.Instances[level].Noun == noun {
			return level, nil, false
		}
		found := r.nearestReached(noun, level)
		if len(found) == 0 {
			continue
		}
		if len(found) == 1 && found[0].inst != "" && (distant || found[0].Dist <= 1) {
			return found[0].inst, nil, false
		}
		things = make([]focusThing, len(found))
		for i, a := range found {
			things[i] = focusThing{Key: a.Key, Label: a.Label}
		}
		ambiguous = len(found) > 1 && !slices.ContainsFunc(found, func(a reached) bool { return a.inst == "" || a.Dist > 1 })
		return "", things, ambiguous
	}
	return "", nil, false
}

func reachAnswer(id string, things []focusThing) string { return id + "|" + fmt.Sprint(things) }

func (op Operation) byAssociation() bool {
	if op.ByAssociation {
		return true
	}
	for _, p := range op.Parameters {
		if p.Focus && !p.Required && p.Noun == op.Noun() {
			return true
		}
	}
	return false
}

// associationSubject admits only declared verbs and human sources. Existing
// noun-path behavior wins; a program must name its target explicitly.
func (r *Runtime) associationSubject(op Operation, q Request, focus string) (string, error) {
	if q.Target != "" || !personSource(r.history.source) || !op.byAssociation() || r.nounInstance(op.Noun(), focus) != "" {
		return "", nil
	}
	for name, p := range op.Parameters {
		if p.Focus && p.Noun == op.Noun() && !emptyProp(q.Args[name]) {
			return "", nil // an explicit subject argument wins
		}
	}
	id, candidates, ambiguous := r.nearestDetail(op.Noun(), focus, op.HistoryKindOf() == HistoryFetch)
	if ambiguous {
		if err := r.associationAmbiguity(op.Noun(), candidates); err != nil {
			return "", err
		}
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
	return fmt.Errorf("%s reached through the focus needs a choice: %s", r.DisplayNoun(noun), strings.Join(names, ", "))
}

// notifyAssociation shows an implicit subject after a successful action
// unless a modal is already showing what it acts on.
func (r *Runtime) notifyAssociation(noun, id, modal string, err error) {
	if inst := r.Instances[id]; err == nil && inst != nil && r.TopModal() == modal {
		t := r.instanceThing(inst)
		r.UI().Notify(r.DisplayNoun(noun) + " " + t.Label + " (from the focus)")
	}
}
