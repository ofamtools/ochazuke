package ochazuke

import (
	"errors"
	"fmt"
	"strings"
)

// The relation verbs of every noun with a store (rows of the legacy verb
// table, offered once the runtime has a RelationStore): NOUN.relate and
// NOUN.unrelate. Both ends of a relation have them and they share one
// handler: the thing the verb acts on is one end and the other end is a
// noun-typed parameter, `to` (this thing is the From end) or `from` (this
// thing is the To end). The kind is any word (default DefaultRelationKind);
// text and data ride along.

const (
	relationKindParam = "kind"
	relationToParam   = "to"
	relationFromParam = "from"
	relationTextParam = "text"
	relationDataParam = "data"
)

var needRelations = storeNeed{Doc: "a relation store (Runtime.SetRelationStore)", Lacks: "the application keeps no relations (no relation store)",
	Has: func(r *Runtime, _ *Noun) bool { return r.RelationStore() != nil }}

func relationVerbParams(n *Noun) map[string]Parameter {
	return map[string]Parameter{
		relationToParam:   {Type: "string", Noun: AnyNoun, Free: true, Excludes: []string{relationFromParam}, Description: "The other end, noun:key, when this thing is the From end"},
		relationFromParam: {Type: "string", Noun: AnyNoun, Free: true, Excludes: []string{relationToParam}, Description: "The other end, noun:key, when this thing is the To end"},
		relationKindParam: {Type: "string", Free: true, Source: "ochazuke.relation-kind.list", Description: "The kind of relation, any word (default " + DefaultRelationKind + "); a declared kind adds rules"},
	}
}

func relateVerbParams(n *Noun) map[string]Parameter {
	params := relationVerbParams(n)
	params[relationTextParam] = Parameter{Type: "string", Description: "Free text kept with the relation"}
	params[relationDataParam] = Parameter{Type: "object", Description: "A JSON object kept with the relation (a key set to null is removed)"}
	return params
}

// relationEnds reads the two ends of a relate or unrelate request: this
// thing and the other, in From, To order.
func (r *Runtime) relationEnds(n *Noun, c Ctx) (from, to, subject string, err error) {
	key, err := r.subjectKey(n, c)
	if err != nil {
		return "", "", "", err
	}
	this := AnyRef(n.Name, key)
	other, asTo := StringArg(c.Args, relationToParam, ""), true
	if other == "" {
		other, asTo = StringArg(c.Args, relationFromParam, ""), false
	}
	switch {
	case StringArg(c.Args, relationToParam, "") != "" && StringArg(c.Args, relationFromParam, "") != "":
		return "", "", "", &ArgumentError{errors.New("name the other end with to or from, not both")}
	case other == "":
		return "", "", "", &ArgumentError{errors.New("name the other end: to (this thing is the From end) or from (this thing is the To end), as noun:key")}
	}
	if asTo {
		return this, other, key, nil
	}
	return other, this, key, nil
}

func relationVerbs() []standardVerb {
	base := standardVerb{Kinds: []string{KindCollection}, Needs: []storeNeed{needWriter, needRelations}, TakesSubject: true,
		Positional: func(*Noun) []string { return []string{relationToParam, relationKindParam} }, HistoryKind: HistoryChange, Agent: AgentAllow}
	relate := base
	relate.Verb = "relate"
	relate.Description = "Relate this thing to another (to) or another to this thing (from), as noun:key, in any kind (default relation) with text and data; relating again updates the relation; a declared kind's rules apply. Every noun has it, both ends share one handler"
	relate.Params = relateVerbParams
	relate.Aliases = []string{"associate"} // rich verbs: associate is relate, on both nouns
	relate.Run = func(r *Runtime, n *Noun, c Ctx) (any, error) {
		from, to, _, err := r.relationEnds(n, c)
		if err != nil {
			return nil, err
		}
		x := Relation{Kind: StringArg(c.Args, relationKindParam, ""), From: from, To: to, Text: StringArg(c.Args, relationTextParam, "")}
		if raw, ok := c.Args[relationDataParam]; ok && !emptyProp(raw) {
			data, err := propValue(PropSpec{Name: relationDataParam, Parameter: Parameter{Type: PropObject}}, raw)
			if err != nil {
				return nil, &ArgumentError{fmt.Errorf("data %w", err)}
			}
			x.Data = data.(map[string]any)
		}
		done, err := r.Relate(c, x)
		if err != nil {
			return nil, err
		}
		return ThingOf(done)
	}
	unrelate := base
	unrelate.Verb = "unrelate"
	unrelate.Description = "Unrelate this thing from another (to) or another from this thing (from): under delete.archive the relation is archived first and unrelating an archived one removes it, otherwise it is removed; the things are never deleted"
	unrelate.Params = relationVerbParams
	unrelate.Run = func(r *Runtime, n *Noun, c Ctx) (any, error) {
		from, to, _, err := r.relationEnds(n, c)
		if err != nil {
			return nil, err
		}
		done, err := r.Unrelate(c, StringArg(c.Args, relationKindParam, ""), from, to)
		if err != nil {
			return nil, err
		}
		return ThingOf(done)
	}
	return []standardVerb{relate, unrelate}
}

// relationKindNoun is the fact noun that lists the declared relation kinds.
const relationKindNoun = "ochazuke.relation-kind"

// registerRelationKinds declares the relation-kind fact noun: list answers
// the kinds the program declared, key NAME, so stories, agents and the
// palette see the rules.
func registerRelationKinds(r *Runtime) {
	r.defineFacts(factsSpec{Noun: relationKindNoun, Sample: RelationKind{}, Key: "name", Label: "name",
		Docs: map[string]string{"name": "The kind: any word relates; a declared kind adds rules", "description": "What the kind says",
			"from": "The noun allowed at the From end (empty: any)", "to": "The noun allowed at the To end (empty: any)",
			"maxFrom": "Relations one From thing may hold (0: any)", "maxTo": "Relations one To thing may have (0: any)",
			"onFull": "Past a maximum: replace (the oldest is unrelated) or keep (a notice)", "ordered": "Whether position order is meaningful",
			"exclusive": "Data flags at most one relation per From thing holds (a default)", "prop": "The word the From end reads the kind by", "inverse": "The word the To end reads it by"},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) {
			return factThings(c.Runtime.RelationKinds(), nil)
		}})
}

// relationLabel is the words a relation reads as in a row: the other end
// and its text.
func relationLabel(x Relation, other string) string {
	if x.Text == "" {
		return other
	}
	return other + " - " + strings.TrimSpace(strings.SplitN(x.Text, "\n", 2)[0])
}
