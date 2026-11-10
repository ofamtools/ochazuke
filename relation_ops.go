package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The operations on relations (relation.go): relate, unrelate, a prop's
// whole list, detach. They sit on the RelationStore and add what the store
// does not hold: kind rules, positions, the exclusive data flags, the
// archive-first unrelate (delete.archive decides) and the notices.

const relationStep = 1024 // positions are sparse: a move writes one row

var errNoRelationStore = errors.New("no relation store: the application gives the runtime one with SetRelationStore")

// relationEnd reads an end as "noun:key": the noun canonical, the key as
// written. An end without a noun is refused (a thing is named by both).
func (r *Runtime) relationEnd(label, ref string) (string, error) {
	noun, key, ok := SplitAnyRef(strings.TrimSpace(ref))
	if !ok || key == "" {
		return "", &ArgumentError{fmt.Errorf("%s %q is not noun:key", label, ref)}
	}
	return AnyRef(r.CanonicalNoun(noun), key), nil
}

// RelationsOf answers the stored relations that meet q (position order).
func (r *Runtime) RelationsOf(q RelationQuery) ([]Relation, error) {
	s := r.RelationStore()
	if s == nil {
		return nil, errNoRelationStore
	}
	return s.Relations(q)
}

// relationGroup are the relations of one kind that one thing holds at one
// end, live and archived ones both, by position.
func (r *Runtime) relationGroup(kind, from, to string) ([]Relation, error) {
	return r.RelationsOf(RelationQuery{Kind: kind, From: from, To: to, Archived: true})
}

// Relate makes the relation x: a relation with the same kind and ends is
// updated (text replaced when given, data merged, an archived one brought
// back), otherwise a new one is put at the end of its (From, kind) order.
// A declared kind's rules apply: an end outside its nouns is refused; past
// a maximum the oldest is unrelated (replace) or a notice says so (keep);
// an exclusive data flag is moved to x when x holds it. It answers the
// stored relation.
func (r *Runtime) Relate(c Ctx, x Relation) (Relation, error) {
	s := r.RelationStore()
	if s == nil {
		return Relation{}, errNoRelationStore
	}
	if x.Kind = strings.TrimSpace(x.Kind); x.Kind == "" {
		x.Kind = DefaultRelationKind
	}
	from, err := r.relationEnd("from", x.From)
	if err != nil {
		return Relation{}, err
	}
	to, err := r.relationEnd("to", x.To)
	if err != nil {
		return Relation{}, err
	}
	x.From, x.To = from, to
	kind, declared := r.RelationKindOf(x.Kind)
	if declared {
		for _, end := range []struct{ label, ref, rule string }{{"from", from, kind.From}, {"to", to, kind.To}} {
			noun, _, _ := SplitAnyRef(end.ref)
			if !relationNounFits(end.rule, noun) {
				return Relation{}, &ArgumentError{fmt.Errorf("relation %s: the %s end is a %s, not a %s", kind.Name, end.label, r.DisplayNoun(noun), r.DisplayNoun(end.rule))}
			}
		}
	}
	mine, err := r.relationGroup(x.Kind, from, "")
	if err != nil {
		return Relation{}, err
	}
	var live []Relation
	var existing *Relation
	for i, o := range mine {
		if o.To == to {
			existing = &mine[i]
		}
		if !o.Archived && o.To != to {
			live = append(live, o)
		}
	}
	if existing != nil {
		prev := *existing
		if x.Text == "" {
			x.Text = prev.Text
		}
		x.Data = mergeRelationData(prev.Data, x.Data)
		if x.Position == 0 {
			x.Position = prev.Position
		}
	} else {
		if x.Position == 0 {
			x.Position = relationStep
			if n := len(mine); n > 0 {
				x.Position = mine[n-1].Position + relationStep
			}
		}
		x.Data = maps.Clone(x.Data)
	}
	x.Archived = false
	x.FormerFrom, x.FormerTo = "", ""
	if declared {
		if err := r.relationExclusive(c, kind, &x, live); err != nil {
			return Relation{}, err
		}
		if existing == nil || existing.Archived {
			if err := r.relationFull(c, kind, x, live); err != nil {
				return Relation{}, err
			}
		}
	}
	if err := r.putRelation(existing, x); err != nil {
		return Relation{}, err
	}
	r.relationsChanged()
	return x, nil
}

// mergeRelationData lays the new keys over the old; a nil value removes a
// key. Nothing to merge keeps the old object.
func mergeRelationData(old, add map[string]any) map[string]any {
	if len(add) == 0 {
		return maps.Clone(old)
	}
	out := maps.Clone(old)
	if out == nil {
		out = map[string]any{}
	}
	for k, v := range add {
		if v == nil {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// relationExclusive keeps the kind's exclusive flags: x holding one clears
// it from the others; when no other holds it, x takes it (the first
// relation made is the default).
func (r *Runtime) relationExclusive(c Ctx, kind RelationKind, x *Relation, others []Relation) error {
	for _, key := range kind.Exclusive {
		held := slices.ContainsFunc(others, func(o Relation) bool { return o.Flag(key) })
		switch {
		case x.Flag(key):
			for _, o := range others {
				if o.Flag(key) {
					prev := o
					o = o.clone()
					o.Data[key] = false
					if err := r.putRelation(&prev, o); err != nil {
						return err
					}
				}
			}
		case !held && x.Data[key] == nil:
			if x.Data == nil {
				x.Data = map[string]any{}
			}
			x.Data[key] = true
		}
	}
	return nil
}

// relationFull applies a maximum before x is added to live: replace
// unrelates the oldest (lowest position) until x fits, keep says so.
func (r *Runtime) relationFull(c Ctx, kind RelationKind, x Relation, live []Relation) error {
	limits := []struct {
		max   int
		held  []Relation
		words string
	}{{kind.MaxFrom, live, "from " + x.From}}
	if kind.MaxTo > 0 {
		toward, err := r.relationGroupTo(kind.Name, x.To)
		if err != nil {
			return err
		}
		limits = append(limits, struct {
			max   int
			held  []Relation
			words string
		}{kind.MaxTo, toward, "to " + x.To})
	}
	for _, l := range limits {
		if l.max <= 0 {
			continue
		}
		held := slices.DeleteFunc(slices.Clone(l.held), func(o Relation) bool { return o.Archived || o.ID() == x.ID() || o.From == x.From && o.To == x.To })
		for len(held) >= l.max {
			if kind.OnFull == RelationKeep {
				c.Notify(fmt.Sprintf("%s: %d relations %s, more than the %d it names", kind.Name, len(held)+1, l.words, l.max))
				break
			}
			oldest := held[0]
			held = held[1:]
			if _, err := r.unrelateOne(c, oldest); err != nil {
				return err
			}
		}
	}
	return nil
}

// relationGroupTo are the relations of kind that end at to.
func (r *Runtime) relationGroupTo(kind, to string) ([]Relation, error) {
	return r.RelationsOf(RelationQuery{Kind: kind, To: to, Archived: true})
}

// Unrelate removes the relation of kind between from and to. Under
// delete.archive a live relation is archived first (hidden, kept, a notice
// says to unrelate again) and unrelating an archived one removes it; with
// the setting off the relation is removed at once. It answers the relation
// as it was.
func (r *Runtime) Unrelate(c Ctx, kind, from, to string) (Relation, error) {
	if r.RelationStore() == nil {
		return Relation{}, errNoRelationStore
	}
	if kind = strings.TrimSpace(kind); kind == "" {
		kind = DefaultRelationKind
	}
	from, err := r.relationEnd("from", from)
	if err != nil {
		return Relation{}, err
	}
	to, err = r.relationEnd("to", to)
	if err != nil {
		return Relation{}, err
	}
	found, err := r.relationGroup(kind, from, to)
	if err != nil {
		return Relation{}, err
	}
	if len(found) == 0 {
		return Relation{}, &ArgumentError{fmt.Errorf("no %s relation from %s to %s", kind, from, to)}
	}
	// A live one first; its archived twin cannot exist (same identity).
	return r.unrelateOne(c, found[0])
}

// unrelateOne archives or removes one stored relation.
func (r *Runtime) unrelateOne(c Ctx, x Relation) (Relation, error) {
	if !x.Archived && r.helpFlag(DeleteArchiveSetting, false) {
		kept := x.clone()
		kept.Archived = true
		for _, key := range r.exclusiveKeys(x.Kind) {
			if kept.Flag(key) {
				kept.Data[key] = false
			}
		}
		if err := r.putRelation(&x, kept); err != nil {
			return Relation{}, err
		}
		c.Notify(fmt.Sprintf("archived %s relation %s to %s: unrelate it again to remove it for good (delete.archive)", x.Kind, x.From, x.To))
	} else if err := r.dropRelation(x); err != nil {
		return Relation{}, err
	}
	if !x.Archived {
		if err := r.relationPromote(c, x); err != nil {
			return Relation{}, err
		}
	}
	r.relationsChanged()
	return x, nil
}

// putRelation writes one row to the store and notes the change for undo
// (before nil: a new row).
func (r *Runtime) putRelation(before *Relation, x Relation) error {
	if err := r.RelationStore().Put(x); err != nil {
		return err
	}
	var b *Relation
	if before != nil {
		c := before.clone()
		b = &c
	}
	a := x.clone()
	r.noteRelation(RelationChange{Before: b, After: &a})
	return nil
}

// dropRelation removes one row from the store and notes the change.
func (r *Runtime) dropRelation(x Relation) error {
	if err := r.RelationStore().Remove(x); err != nil {
		return err
	}
	c := x.clone()
	r.noteRelation(RelationChange{Before: &c})
	return nil
}

func (r *Runtime) exclusiveKeys(kind string) []string {
	k, _ := r.RelationKindOf(kind)
	return k.Exclusive
}

// relationPromote hands the exclusive flags x held to the next live
// relation of its (From, kind) by position, and says so.
func (r *Runtime) relationPromote(c Ctx, x Relation) error {
	for _, key := range r.exclusiveKeys(x.Kind) {
		if !x.Flag(key) {
			continue
		}
		rest, err := r.RelationsOf(RelationQuery{Kind: x.Kind, From: x.From})
		if err != nil {
			return err
		}
		if len(rest) == 0 || slices.ContainsFunc(rest, func(o Relation) bool { return o.Flag(key) }) {
			continue
		}
		next := rest[0].clone()
		if next.Data == nil {
			next.Data = map[string]any{}
		}
		next.Data[key] = true
		if err := r.putRelation(&rest[0], next); err != nil {
			return err
		}
		c.Notify(fmt.Sprintf("%s is now the %s %s of %s", next.To, key, x.Kind, x.From))
	}
	return nil
}

// SetRelated makes the live relations of kind from `from` exactly tos, in
// that order: the missing ones are unrelated, the new ones related, the
// order written. Text and data of the ones kept stay. It is what writing a
// relation-backed prop does.
func (r *Runtime) SetRelated(c Ctx, kind, from string, tos []string) error {
	if r.RelationStore() == nil {
		return errNoRelationStore
	}
	if kind = strings.TrimSpace(kind); kind == "" {
		kind = DefaultRelationKind
	}
	from, err := r.relationEnd("from", from)
	if err != nil {
		return err
	}
	var want []string
	for _, t := range tos {
		end, err := r.relationEnd("to", t)
		if err != nil {
			return err
		}
		if !slices.Contains(want, end) {
			want = append(want, end)
		}
	}
	have, err := r.RelationsOf(RelationQuery{Kind: kind, From: from})
	if err != nil {
		return err
	}
	for _, o := range have {
		if !slices.Contains(want, o.To) {
			if _, err := r.unrelateOne(c, o); err != nil {
				return err
			}
		}
	}
	for _, to := range want {
		if !slices.ContainsFunc(have, func(o Relation) bool { return o.To == to }) {
			if _, err := r.Relate(c, Relation{Kind: kind, From: from, To: to}); err != nil {
				return err
			}
		}
	}
	return r.relationReorder(kind, from, want)
}

// relationReorder writes positions so the live relations of (From, kind)
// read in the order want (the ends named; others keep their place after).
func (r *Runtime) relationReorder(kind, from string, want []string) error {
	now, err := r.RelationsOf(RelationQuery{Kind: kind, From: from})
	if err != nil {
		return err
	}
	byTo := map[string]Relation{}
	for _, o := range now {
		byTo[o.To] = o
	}
	var order []string
	for _, to := range want {
		if _, ok := byTo[to]; ok {
			order = append(order, to)
		}
	}
	for _, o := range now {
		if !slices.Contains(order, o.To) {
			order = append(order, o.To)
		}
	}
	inOrder := slices.IsSortedFunc(now, func(a, b Relation) int { return slices.Index(order, a.To) - slices.Index(order, b.To) })
	if inOrder {
		return nil
	}
	for i, to := range order {
		prev := byTo[to]
		o := prev.clone()
		if o.Position == (i+1)*relationStep {
			continue
		}
		o.Position = (i + 1) * relationStep
		if err := r.putRelation(&prev, o); err != nil {
			return err
		}
	}
	r.relationsChanged()
	return nil
}

// DetachRelations is what deleting a thing does to its relations (record
// first, nothing is lost): every relation that has ref at an end keeps the
// row, the end moving to FormerFrom or FormerTo. It answers how many it
// changed. A runtime with no store changes none.
func (r *Runtime) DetachRelations(ref string) (int, error) {
	s := r.RelationStore()
	if s == nil {
		return 0, nil
	}
	all, err := s.Relations(RelationQuery{Thing: ref, Archived: true})
	if err != nil {
		return 0, err
	}
	for _, x := range all {
		moved := x.clone()
		if x.From == ref {
			moved.From, moved.FormerFrom = "", ref
		}
		if x.To == ref {
			moved.To, moved.FormerTo = "", ref
		}
		if err := r.dropRelation(x); err != nil {
			return 0, err
		}
		if err := r.putRelation(nil, moved); err != nil {
			return 0, err
		}
	}
	if len(all) > 0 {
		r.relationsChanged()
	}
	return len(all), nil
}

// RelationChange is one stored relation row's change, as history keeps it
// for undo: the row before (nil: it was made) and after (nil: removed).
type RelationChange struct {
	Before *Relation `json:"before,omitempty"`
	After  *Relation `json:"after,omitempty"`
}

// noteRelation adds a relation change to the running request's entry, unless
// a wrapped prop write or an undo is running (the thing's own change, or no
// change at all, covers it). The change names the From thing for the notice.
func (r *Runtime) noteRelation(ch RelationChange) {
	if r.relations == nil || r.relations.quiet > 0 || r.history.depth == 0 {
		return // not inside a request: there is no entry to note it on
	}
	row := ch.After
	if row == nil {
		row = ch.Before
	}
	ref := row.From
	if ref == "" {
		ref = row.To
	}
	noun, key, _ := SplitAnyRef(ref)
	r.noteChange(ThingChange{Noun: noun, Key: key, Relation: &ch})
}

// quietly runs f with relation changes not noted.
func (r *Runtime) quietly(f func() error) error {
	st := r.relationState()
	st.quiet++
	defer func() { st.quiet-- }()
	return f()
}

// undoRelation puts the row back as it was: the After row removed, the
// Before row put (replacing one with the same identity).
func (r *Runtime) undoRelation(ch RelationChange) error {
	s := r.RelationStore()
	if s == nil {
		return errNoRelationStore
	}
	if ch.After != nil && (ch.Before == nil || ch.Before.ID() != ch.After.ID()) {
		if err := s.Remove(*ch.After); err != nil {
			return err
		}
	}
	if ch.Before != nil {
		if err := s.Put(*ch.Before); err != nil {
			return err
		}
	}
	r.relationsChanged()
	return nil
}
