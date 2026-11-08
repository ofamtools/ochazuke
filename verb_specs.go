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
	// requires is the operation's Requires: the guard that must hold at
	// the focus for the verb to have what it acts on; "" for none.
	requires string
	// traits carries the operation's classification fields (Motion,
	// FocusBound, Typing, Staying, Repeatable, HistoryKind); only those
	// are read, the row's other columns fill the rest.
	traits Operation
}

// registerVerbSpecs registers every row as an operation.
func (r *Runtime) registerVerbSpecs(specs []verbSpec) {
	r.InvalidateBindings()
	for _, s := range specs {
		op := s.traits
		op.Key = KeyOf(s.noun, s.verb)
		op.Description, op.Parameters, op.Result, op.Requires, op.Run = s.description, s.params, s.result, s.requires, s.run
		r.Register(op)
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
	// traits classify the verb (asMotion, asHistory, ...); nil for none.
	traits []opTrait
}

// defineNounVerbs declares every row on its noun, in order.
func (r *Runtime) defineNounVerbs(rows []nounVerbRow) {
	for _, row := range rows {
		b := r.Define(row.noun).Verb(row.verb, row.run, row.description)
		op := r.Operations.At(Key(b.last))
		applyTraits(&op, row.traits)
		r.Operations.Set(op)
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

// opTrait sets classification fields on an operation while it is
// declared (Motion, Typing, Staying, Repeatable, FocusBound, HistoryKind);
// a registration helper takes them after its fixed arguments.
type opTrait func(*Operation)

// The classification traits a registration helper takes.
var (
	asMotion     opTrait = func(o *Operation) { o.Motion = true }
	asFocusBound opTrait = func(o *Operation) { o.FocusBound = true }
	asTyping     opTrait = func(o *Operation) { o.Typing = true }
	asStaying    opTrait = func(o *Operation) { o.Staying = true }
	asRepeatable opTrait = func(o *Operation) { o.Repeatable = true }
)

// asHistory sets the kind of history entry the operation records.
func asHistory(kind string) opTrait { return func(o *Operation) { o.HistoryKind = kind } }

// applyTraits sets each trait on o.
func applyTraits(o *Operation, traits []opTrait) {
	for _, t := range traits {
		t(o)
	}
}
