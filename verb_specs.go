package ochazuke

// verbSpec is one row of a table of verbs a registration function
// registers: the verb on its noun, what it does, the kind of answer it
// gives, its parameters and the named handler that runs it.
type verbSpec struct {
	verb, noun, description, result string
	params                          map[string]Parameter
	run                             Handler
	// guard names the verb guard that gates the verb on its noun; "" for none.
	guard string
}

// registerVerbSpecs registers every row as an operation.
func (r *Runtime) registerVerbSpecs(specs []verbSpec) {
	for _, s := range specs {
		r.Register(Operation{Verb: s.verb, Noun: s.noun, Description: s.description, Parameters: s.params, Result: s.result, Run: s.run})
		if s.guard == "" {
			continue
		}
		if n := r.Noun(s.noun); n.Guards == nil {
			n.Guards = map[string]string{s.verb: s.guard}
		} else {
			n.Guards[s.verb] = s.guard
		}
	}
}

// nounVerbRow is one row of a table of verbs declared through a noun's
// builder (Runtime.Define): the handler and the description it shows.
type nounVerbRow struct {
	noun, verb, description string
	run                     Handler
}

// defineNounVerbs declares every row on its noun, in order.
func (r *Runtime) defineNounVerbs(rows []nounVerbRow) {
	for _, row := range rows {
		r.Define(row.noun).Verb(row.verb, row.run, row.description)
	}
}

// guardRow is one named guard of a table of guards.
type guardRow struct {
	name string
	test Guard
}

// defineGuards defines every row's guard.
func (r *Runtime) defineGuards(rows []guardRow) {
	for _, g := range rows {
		r.DefineGuard(g.name, g.test)
	}
}
