package ochazuke

// The action facts noun: one row per Action (an operation's name, across
// every noun that has it), read from Operations. Its list takes a noun and
// ochazuke.noun list takes an action; both read the one relation through
// the Operations relation, so the two sides always agree.

// actionFact is one action's row.
type actionFact struct {
	Name       string   `json:"name"`
	Nouns      []string `json:"nouns"`
	Operations []string `json:"operations"`
}

// relatedNames is the set side gives for the name args[param] holds:
// nil when the argument is absent (no filter), empty when it names
// nothing. ochazuke.noun list {action} reads it with nounsWithAction and
// ochazuke.action list {noun} with actionsOnNoun.
func relatedNames(r *Runtime, args Arguments, param string, side func(*Runtime, string) []string) map[string]bool {
	name := StringArg(args, param, "")
	if name == "" {
		return nil
	}
	out := map[string]bool{}
	for _, related := range side(r, name) {
		out[related] = true
	}
	return out
}

// nounsWithAction is the names of the nouns with the named action.
func nounsWithAction(r *Runtime, action string) []string {
	var out []string
	for _, n := range r.Operations.NounsWithAction(r.Operations.Action(action)) {
		out = append(out, n.Name)
	}
	return out
}

// actionsOnNoun is the names of the named noun's actions.
func actionsOnNoun(r *Runtime, noun string) []string {
	return r.Operations.ActionNames(r.Operations.Noun(r.CanonicalNoun(noun)))
}

// registerActionThings declares ochazuke.action.
func registerActionThings(r *Runtime) {
	r.defineFacts(factsSpec{Noun: "ochazuke.action", Sample: actionFact{}, Key: "name", Label: "name",
		Docs: map[string]string{"name": "The action's name (an operation's last part)", "nouns": "The nouns that have it",
			"operations": "Its operations' keys"},
		ListParams: map[string]Parameter{"noun": {Type: "string", Description: "Only the actions this noun has (ochazuke.noun list {action} is the other side)"}},
		List: func(c Ctx, q ListQuery) ([]Thing, error) {
			ops := &c.Runtime.Operations
			only := relatedNames(c.Runtime, q.Args, "noun", actionsOnNoun)
			var rows []actionFact
			for _, a := range ops.Actions() {
				if only != nil && !only[a.Name] {
					continue
				}
				row := actionFact{Name: a.Name}
				for _, n := range ops.NounsWithAction(a) {
					row.Nouns = append(row.Nouns, n.Name)
				}
				for _, op := range ops.OperationsOfAction(a) {
					row.Operations = append(row.Operations, op.Key.Short())
				}
				rows = append(rows, row)
			}
			return factThings(rows, nil)
		}})
}
