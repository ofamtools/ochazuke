package ochazuke

// Noun kinds (noun scaffold plan, C1): what kind of noun a noun is, so a
// generic page knows what to draw for it with no code per noun. A noun with
// a store is a stored kind, read off its store row; a noun without one is a
// command noun (its verbs are actions that run on their own) or a part (a
// drawn row or widget: a turn's line, a form field, an input), which a
// noun declares (Noun.Kind, NounBuilder.Kind) or the runtime derives.
const (
	// NounKindRecord is a noun whose things are records held by the
	// application (a Collection store that is not facts): the full scaffold.
	NounKindRecord = "record"
	// NounKindSingleton is a noun with one thing (a Singleton store): show and
	// edit, no index.
	NounKindSingleton = "singleton"
	// NounKindFacts is a noun whose things are system facts read from a
	// registry (Store.Facts): index and show, read-only.
	NounKindFacts = "facts"
	// NounKindInstances is a noun whose things are live runtime instances (an
	// Instances store: pages, panes).
	NounKindInstances = "instances"
	// NounKindCommand is a noun with no store whose verbs are actions (agent,
	// daemon, setup): no index; its actions are listed.
	NounKindCommand = "command"
	// NounKindPart is a noun with no store whose instances are drawn parts of
	// a component (a form field, an outline line): no page of its own.
	NounKindPart = "part"
)

// NounKindList is the noun kinds, stored kinds first.
var NounKindList = []string{NounKindRecord, NounKindSingleton, NounKindFacts, NounKindInstances, NounKindCommand, NounKindPart}

// nounKindRow is what the runtime knows of one noun kind: whether its
// nouns have a store, and whether a noun may declare it (NounBuilder.Kind)
// where its store cannot say.
// group is the heading of the kind's group on the Nouns page.
type nounKindRow struct {
	stored, declared bool
	group            string
}

// nounKindTable is the noun kinds' rows, by kind.
var nounKindTable = map[string]nounKindRow{
	NounKindRecord:    {stored: true, group: "records"},
	NounKindSingleton: {stored: true, group: "singletons"},
	NounKindFacts:     {stored: true, declared: true, group: "facts"},
	NounKindInstances: {stored: true, group: "instances"},
	NounKindCommand:   {declared: true, group: "commands"},
	NounKindPart:      {declared: true, group: "parts"},
}

// DeclaredKind reports whether a noun may declare the kind
// (NounBuilder.Kind): part, command, or facts over a store not marked so.
func DeclaredKind(kind string) bool { return nounKindTable[kind].declared }

// Kind declares what kind of noun a noun is where its store cannot say
// (Noun.Kind): NounKindPart or NounKindCommand for a noun without a store, or
// NounKindFacts for a stored noun whose things are facts somewhere else (a
// mounted extension's facts, over a proxy store). Any other kind is read off
// the store (NounKind). It panics on another kind, as a builder does on a
// bad declaration.
func (b *NounBuilder) Kind(kind string) *NounBuilder {
	if kind != "" && !DeclaredKind(kind) {
		panic("noun " + b.n.Name + ": kind " + kind + " is not " + NounKindPart + ", " + NounKindCommand + " or " + NounKindFacts + " (a stored noun takes its kind from its store)")
	}
	b.n.Kind = kind
	return b
}

// NounKind is the kind of the noun of that name (NounKindList), "" for no such
// noun. A noun with a store is read off it: facts (Store.Facts), a
// singleton, live instances, else a record. A noun without one is the kind
// it declares (NounBuilder.Kind); declaring none, it is a command noun when
// one of its verbs is an application verb (a handler that may run work on
// its own: Operation.Kind), else a part, whose verbs only act on what is
// drawn.
func (r *Runtime) NounKind(name string) string {
	n := r.Nouns[r.CanonicalNoun(name)]
	if n == nil {
		return ""
	}
	if kind := storedNounKind(n); kind != "" {
		return kind
	}
	for _, op := range r.Operations.Entries() {
		if op.Noun() == n.Name && op.Kind == KindApplication {
			return NounKindCommand
		}
	}
	return NounKindPart
}

// AllNounKinds is NounKind of every noun, by name, read in one pass over
// the operations.
func (r *Runtime) AllNounKinds() map[string]string {
	runs := map[string]bool{}
	for _, op := range r.Operations.Entries() {
		if op.Kind == KindApplication {
			runs[op.Noun()] = true
		}
	}
	out := make(map[string]string, len(r.Nouns))
	for name, n := range r.Nouns {
		switch kind := storedNounKind(n); {
		case kind != "":
			out[name] = kind
		case runs[name]:
			out[name] = NounKindCommand
		default:
			out[name] = NounKindPart
		}
	}
	return out
}

// storedNounKind is the kind a noun's store gives it, else the kind it
// declares; "" when it has neither.
func storedNounKind(n *Noun) string {
	if s := n.Store; s != nil {
		switch {
		case s.Facts || n.Kind == NounKindFacts:
			return NounKindFacts
		case s.Kind == Singleton:
			return NounKindSingleton
		case s.Kind == Instances:
			return NounKindInstances
		}
		return NounKindRecord
	}
	if n.Kind == NounKindFacts {
		return "" // facts need a store: derived as any storeless noun
	}
	return n.Kind
}

// StoredKind reports whether a noun kind has a store: a record, a
// singleton, facts or live instances.
func StoredKind(kind string) bool { return nounKindTable[kind].stored }
