package ochazuke

import (
	"fmt"
	"slices"
)

// Related sections over relations: a section per relation kind that names
// the stack's thing. A relation-backed prop (PropSpec.Relation) of another
// noun makes the section of the things that point at it (like any referrer,
// read through the RelationStore instead of a filter on the store), and a
// declared kind (RelationKind) adds the section of the things the stack's
// thing is related to (Referrer.Outward) and, for a kind with no such prop,
// the things that point at it. Rows are the other end's things: read with
// the noun's Get, or, for a noun with no store, made from the key.
//
// Writes are the relate and unrelate verbs, so the kind's rules, history and
// undo are theirs: associate relates (a list prop appends), unassociate
// unrelates, new creates a thing with the relation-backed prop filled.

// RelationSectionsSetting turns the sections that read relations off
// (default on): relation-backed referrers and declared kinds draw nothing
// and read nothing, the stack otherwise as it was.
const RelationSectionsSetting = "relations.sections"

// dropRelationSections removes the relation sections the setting turned off.
func (rel *Related) dropRelationSections(on bool) {
	if on {
		return
	}
	for _, s := range slices.Clone(rel.sections) {
		if s.ref.Relation == "" {
			continue
		}
		s.remove()
		rel.sections = slices.DeleteFunc(rel.sections, func(o *relatedSection) bool { return o == s })
		delete(rel.byID, s.ref.ID())
	}
}

// syncRelations fills the relation sections again when relations changed
// since they were read (a relate, an unrelate, an undo, a detach).
func (rel *Related) syncRelations(already bool) {
	st := rel.r.relations
	if st == nil || st.gen == rel.relGen {
		return
	}
	rel.relGen = st.gen
	if already {
		return
	}
	for _, s := range rel.sections {
		if s.ref.Relation != "" {
			s.fill()
		}
	}
}

// readRelations are the other ends' things of the stack's thing in the
// section's kind, in position order.
func (s *relatedSection) readRelations(c Ctx) ([]Thing, error) {
	page, err := s.rel.r.readRelatedRelations(c, s.rel.noun, s.rel.key, s.ref, ListQuery{Archived: true})
	return page.Items, err
}

// relationRequest is the relate or unrelate request for the section's thing
// and one other thing (its key), the section's side deciding which end each
// is: a section of things pointing at the stack's thing relates them to it,
// an outward section relates the stack's thing to them.
func (s *relatedSection) relationRequest(verb, other string) Request {
	rel := s.rel
	n := rel.r.Nouns[s.ref.Noun]
	this := AnyRef(rel.noun, rel.key)
	args := Arguments{relationKindParam: s.ref.Relation}
	if s.ref.Outward {
		thisNoun := rel.r.Nouns[rel.noun]
		if kp := thisNoun.keyProp(); kp != nil {
			args[kp.Name] = rel.key
		}
		if other != "" {
			args[relationToParam] = AnyRef(s.ref.Noun, other)
		}
		return Request{Noun: rel.noun, Verb: verb, Args: args}
	}
	if kp := n.keyProp(); kp != nil && other != "" {
		args[kp.Name] = other
	}
	args[relationToParam] = this
	return Request{Noun: s.ref.Noun, Verb: verb, Args: args}
}

// relateSection is associate on a relation section: relate the named thing,
// or ask which with the request open.
func (s *relatedSection) relateSection(c Ctx) error {
	if thing := StringArg(c.Args, "thing", ""); thing != "" {
		return s.run(c, s.relationRequest("relate", thing), false)
	}
	return s.run(c, s.relationRequest("relate", ""), true)
}

// unrelateSection is unassociate on a relation section: unrelate the
// selected (or marked) things, one undo step.
func (s *relatedSection) unrelateSection(c Ctx) error {
	focus := c.Runtime.relatedFocus(c)
	subjects := s.subjects(focus)
	if len(subjects) == 0 {
		return fmt.Errorf("no %s selected in %s", shortNounName(s.ref.Noun), s.ref.Label)
	}
	defer c.Runtime.inHistoryGroup(c.Runtime.newHistoryGroup("unassociate " + s.ref.ID()))()
	for _, id := range subjects {
		inst := c.Runtime.Instances[id]
		key, err := c.Runtime.rowKey(s.n, inst)
		if err != nil {
			return err
		}
		if _, err := c.Runtime.ExecuteAt(c.Runtime.contextOf(focus), s.relationRequest("unrelate", key)); err != nil {
			return err
		}
	}
	s.fill()
	return nil
}

// newRelated is new on a relation section: create a thing of the section's
// noun with its relation-backed prop naming the stack's thing. A section
// with no such prop (a kind's section) has nothing to fill, so it says so.
func (s *relatedSection) newRelated(c Ctx) error {
	p := s.n.prop(s.ref.Prop)
	if s.ref.Outward || p == nil || p.Relation != s.ref.Relation {
		return fmt.Errorf("new is not offered in %s: it is a relation, not a prop of %s; create the thing, then associate it", s.ref.Label, c.Runtime.DisplayNoun(s.ref.Noun))
	}
	var v any = s.value()
	if p.listed() {
		v = []any{v}
	}
	return s.run(c, Request{Noun: s.ref.Noun, Verb: "create", Args: Arguments{s.ref.Prop: v}}, false)
}
