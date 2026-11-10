package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

// Relations: anything points to anything. A relation is one row of a many
// to many with two any-noun ends (From and To, each "noun:key", AnyRef), a
// Kind (any word), free Text, a JSON-like Data object and a Position for
// order. The library models relations and enforces the rules a declared
// kind adds; it never persists them. The application (or a tool, in its own
// store) gives the runtime a RelationStore (Runtime.SetRelationStore), and
// ochazuke does the rest: relation-backed props (PropSpec.Relation), the
// derived verbs NOUN.relate and NOUN.unrelate, the related sections that
// list a thing's relations, and the catalog's relation kinds.
//
// The words follow the lexicon (a word is a noun or a verb): "relation" is
// the noun; "relate" and "unrelate" are the verbs on both ends of one. The
// default kind, for a relation made with none, is DefaultRelationKind.

// DefaultRelationKind is the kind of a relation made without one (record
// first: a relation is never refused for lacking a kind).
const DefaultRelationKind = "relation"

// Relation is one stored relation. The key is (Kind, From, To): relating
// the same two things in the same kind again updates the row. Position
// orders the relations of one (From, Kind); Archived hides a relation
// without losing it (unrelate archives first under delete.archive).
// FormerFrom and FormerTo hold the end a deleted thing used to be: the
// relation stays, its end empty (record first, nothing is lost).
type Relation struct {
	Kind       string         `json:"kind"`
	From       string         `json:"from,omitempty"`
	To         string         `json:"to,omitempty"`
	Text       string         `json:"text,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	Position   int            `json:"position,omitempty"`
	Archived   bool           `json:"archived,omitempty"`
	FormerFrom string         `json:"formerFrom,omitempty"`
	FormerTo   string         `json:"formerTo,omitempty"`
}

// ID is the relation's identity, stable across updates: its kind and ends
// (and the former ends of a detached one). Stores key their rows by it.
func (x Relation) ID() string {
	return strings.Join([]string{x.Kind, x.From, x.To, x.FormerFrom, x.FormerTo}, "\x00")
}

// Flag reports whether the relation's data holds key as the boolean true
// (the default flag of a directory is Flag("default")).
func (x Relation) Flag(key string) bool {
	b, _ := x.Data[key].(bool)
	return b
}

// Other is the end of the relation that is not ref ("" when ref is neither).
func (x Relation) Other(ref string) string {
	switch ref {
	case x.From:
		return x.To
	case x.To:
		return x.From
	}
	return ""
}

func (x Relation) clone() Relation {
	x.Data = maps.Clone(x.Data)
	return x
}

// RelationQuery selects relations. Empty fields match everything; Thing
// matches either end. Archived relations are left out unless Archived is
// set; Detached answers only the relations that lost an end (FormerFrom or
// FormerTo set), which are otherwise left out too. Answers are in position
// order, ties in the order they were put.
type RelationQuery struct {
	Kind     string
	From     string
	To       string
	Thing    string
	Archived bool
	Detached bool
}

// Match reports whether the relation meets the query.
func (q RelationQuery) Match(x Relation) bool {
	detached := x.FormerFrom != "" || x.FormerTo != ""
	switch {
	case q.Kind != "" && x.Kind != q.Kind,
		q.From != "" && x.From != q.From,
		q.To != "" && x.To != q.To,
		q.Thing != "" && x.From != q.Thing && x.To != q.Thing,
		x.Archived && !q.Archived,
		detached != q.Detached:
		return false
	}
	return true
}

// RelationStore is what an application (or a tool, in its own file) gives
// the runtime to keep relations. It is plain storage: Put inserts or
// replaces the row with the same ID, Remove deletes it (no error when it is
// gone), Relations answers the rows that meet the query in position order.
// The runtime enforces kinds, positions, archive and detach on top, so a
// store holds no rules.
type RelationStore interface {
	Relations(q RelationQuery) ([]Relation, error)
	Put(x Relation) error
	Remove(x Relation) error
}

// MemRelations is an in-memory RelationStore for tests, o-story and apps
// that keep relations for one run.
type MemRelations struct {
	mu   sync.Mutex
	rows []Relation
}

// NewMemRelations is an empty in-memory store.
func NewMemRelations() *MemRelations { return &MemRelations{} }

// Relations answers the rows that meet q, by position (stable).
func (m *MemRelations) Relations(q RelationQuery) ([]Relation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Relation
	for _, x := range m.rows {
		if q.Match(x) {
			out = append(out, x.clone())
		}
	}
	slices.SortStableFunc(out, func(a, b Relation) int { return a.Position - b.Position })
	return out, nil
}

// Put inserts the relation, or replaces the row with its ID in place.
func (m *MemRelations) Put(x Relation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := x.ID()
	if i := slices.IndexFunc(m.rows, func(o Relation) bool { return o.ID() == id }); i >= 0 {
		m.rows[i] = x.clone()
		return nil
	}
	m.rows = append(m.rows, x.clone())
	return nil
}

// Remove deletes the row with the relation's ID.
func (m *MemRelations) Remove(x Relation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := x.ID()
	m.rows = slices.DeleteFunc(m.rows, func(o Relation) bool { return o.ID() == id })
	return nil
}

// Len is how many rows the store holds, archived and detached ones too.
func (m *MemRelations) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}

// The values of RelationKind.OnFull.
const (
	// RelationReplace relates the new one and unrelates the oldest that
	// pushed past a maximum (the default): setting a note's one "about"
	// replaces it.
	RelationReplace = "replace"
	// RelationKeep relates it anyway and says so in a notice.
	RelationKeep = "keep"
)

// RelationKind is the rules of a kind, declared as data
// (Runtime.DeclareRelationKind, or an application's catalog or settings).
// A kind that is not declared is accepted with no rules: any word is a
// kind, declared ones add rules and labels.
//
// From and To name the nouns allowed at each end ("" or AnyNoun: any); a
// relation with an end outside them is refused. MaxFrom is how many
// relations of the kind one From thing may hold, MaxTo how many one To
// thing may have (0: any); OnFull says what relating past one does
// (RelationReplace, the default, or RelationKeep). Ordered says Position
// is meaningful (a reorder is allowed and kept). Exclusive lists data keys
// that at most one relation per (From, kind) holds as true: setting one
// clears the others, the first relation made takes it, and when its holder
// is unrelated the next by position takes it (a project's default
// directory is Exclusive: ["default"]).
//
// Prop and Inverse are the words the two ends read the kind by (a project's
// "directories", a directory's "projects"); a relation-backed prop of that
// name, or the related sections, use them as labels.
type RelationKind struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	From        string   `json:"from,omitempty"`
	To          string   `json:"to,omitempty"`
	MaxFrom     int      `json:"maxFrom,omitempty"`
	MaxTo       int      `json:"maxTo,omitempty"`
	OnFull      string   `json:"onFull,omitempty"`
	Ordered     bool     `json:"ordered,omitempty"`
	Exclusive   []string `json:"exclusive,omitempty"`
	Prop        string   `json:"prop,omitempty"`
	Inverse     string   `json:"inverse,omitempty"`
}

// relationNounFits reports whether noun fits a kind's rule for one end.
func relationNounFits(rule, noun string) bool {
	return rule == "" || rule == AnyNoun || rule == noun
}

// relationState is a runtime's relation store and declared kinds.
type relationState struct {
	store RelationStore
	kinds map[string]RelationKind
	order []string
	// dirty is set when relations changed since the related stacks last
	// read them (Related.sync reads and clears it per stack).
	gen uint64
	// quiet is above 0 while relation changes are not noted for undo.
	quiet int
}

// SetRelationStore gives the runtime the store that keeps its relations
// (nil removes it). Nouns with a store gain the verbs relate and unrelate;
// props declared with PropSpec.Relation read and write through it. The
// application owns persistence: ochazuke only calls the interface.
func (r *Runtime) SetRelationStore(s RelationStore) {
	r.relationState().store = s
	for _, name := range slices.Sorted(maps.Keys(r.Nouns)) {
		if n := r.Nouns[name]; n.Store != nil {
			r.ensureStandardVerbs(n)
		}
	}
	r.relationsChanged()
}

// RelationStore is the store set with SetRelationStore, nil when none.
func (r *Runtime) RelationStore() RelationStore {
	if r.relations == nil {
		return nil
	}
	return r.relations.store
}

func (r *Runtime) relationState() *relationState {
	if r.relations == nil {
		r.relations = &relationState{kinds: map[string]RelationKind{}}
	}
	return r.relations
}

// DeclareRelationKind adds a kind's rules; declaring a name again replaces
// it. It refuses a kind with no name, a negative maximum, an unknown OnFull
// or an end that names a noun the runtime lacks.
func (r *Runtime) DeclareRelationKind(k RelationKind) error {
	k.Name = strings.TrimSpace(k.Name)
	switch {
	case k.Name == "":
		return fmt.Errorf("relation kind needs a name")
	case strings.ContainsAny(k.Name, " \t\n\x00"):
		return fmt.Errorf("relation kind %q is one word", k.Name)
	case k.MaxFrom < 0 || k.MaxTo < 0:
		return fmt.Errorf("relation kind %s: a maximum is 0 (any) or more", k.Name)
	case k.OnFull != "" && k.OnFull != RelationReplace && k.OnFull != RelationKeep:
		return fmt.Errorf("relation kind %s: onFull is %s or %s", k.Name, RelationReplace, RelationKeep)
	}
	for _, end := range []*string{&k.From, &k.To} {
		if *end == "" || *end == AnyNoun {
			continue
		}
		if r.Noun(*end) == nil {
			return fmt.Errorf("relation kind %s: no such noun %q", k.Name, *end)
		}
		*end = r.CanonicalNoun(*end)
	}
	if k.OnFull == "" {
		k.OnFull = RelationReplace
	}
	k.Exclusive = slices.Clone(k.Exclusive)
	st := r.relationState()
	if _, ok := st.kinds[k.Name]; !ok {
		st.order = append(st.order, k.Name)
	}
	st.kinds[k.Name] = k
	return nil
}

// RelationKinds are the declared kinds in declaration order.
func (r *Runtime) RelationKinds() []RelationKind {
	if r.relations == nil {
		return nil
	}
	out := make([]RelationKind, 0, len(r.relations.order))
	for _, name := range r.relations.order {
		out = append(out, r.relations.kinds[name])
	}
	return out
}

// RelationKindOf is the declared kind of that name.
func (r *Runtime) RelationKindOf(name string) (RelationKind, bool) {
	if r.relations == nil {
		return RelationKind{}, false
	}
	k, ok := r.relations.kinds[name]
	return k, ok
}

// relationsChanged tells the related stacks that relations changed, so
// they read their relation sections again at the next sync.
func (r *Runtime) relationsChanged() {
	if r.relations != nil {
		r.relations.gen++
	}
}
