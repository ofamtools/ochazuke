package ochazuke

import (
	"fmt"
	"slices"
)

// readRelatedThings is the shared reader of show-form sections and graph
// pickers. The caller owns paging; remote stores are replayed on the loop by
// runSteppedBody, exactly as a windowed collection's reads are.
func (r *Runtime) readRelatedThings(c Ctx, spec RelatedSpec, noun, key string, ref Referrer, q ListQuery) (ListPage, error) {
	if key == "" {
		return ListPage{}, nil
	}
	n := r.Noun(ref.Noun)
	if n == nil || n.keyProp() == nil {
		return ListPage{}, nil
	}
	c.Noun = n.Name
	if ref.Relation != "" {
		return r.readRelatedRelations(c, noun, key, ref, q)
	}
	value := ref.Value(noun, key)
	if n.Store == nil || n.Store.List == nil && n.Store.Query == nil {
		if spec.Rows == nil {
			return ListPage{}, nil
		}
		things, err := spec.Rows(c, ref, value)
		return r.queryAll(n, things, q), err
	}
	if n.Store.Remote && c.tape == nil {
		return ListPage{}, nil // show sections do not start remote work themselves
	}
	q.Filters = append(clonePropFilters(q.Filters), PropFilter{Prop: ref.Prop, Op: "=", Value: value})
	return r.queryThings(c, n, q)
}

func (r *Runtime) readRelatedRelations(c Ctx, noun, key string, ref Referrer, q ListQuery) (ListPage, error) {
	if r.RelationStore() == nil {
		return ListPage{}, nil
	}
	n := r.Noun(ref.Noun)
	c.Noun = n.Name
	this := AnyRef(noun, key)
	query := RelationQuery{Kind: ref.Relation, To: this}
	if ref.Outward {
		query = RelationQuery{Kind: ref.Relation, From: this}
	}
	rels, err := r.RelationsOf(query)
	if err != nil {
		return ListPage{}, err
	}
	var keys []string
	for _, x := range rels {
		otherNoun, otherKey, ok := SplitAnyRef(x.Other(this))
		if ok && otherNoun == ref.Noun {
			keys = append(keys, otherKey)
		}
	}
	if len(keys) == 0 {
		return ListPage{}, nil
	}
	// A paged graph column searches and windows at the store, not by
	// fetching every related thing and cutting its rows afterwards.
	if q.PerPage > 0 && n.Store != nil && n.Store.Query != nil {
		q.Filters = append(clonePropFilters(q.Filters), PropFilter{Prop: n.keyProp().Name, Op: "in", Value: keys})
		return r.queryThings(c, n, q)
	}
	var things []Thing
	for _, otherKey := range keys {
		var t Thing
		if n.Store != nil && n.Store.Get != nil && (!n.Store.Remote || c.tape != nil) {
			got, err := n.Store.Get(c, otherKey)
			if err != nil && n.Store.Remote {
				return ListPage{}, err
			}
			t = got
		}
		if t == nil {
			t = Thing{n.keyProp().Name: otherKey}
			if lp := n.roleProp(RoleLabel); lp != nil {
				t[lp.Name] = otherKey
			}
		}
		things = append(things, t)
	}
	return r.queryAll(n, things, q), nil
}

// relatedGraphRefs includes the ordinary referrers and the concrete ends of
// any-noun or undeclared relation kinds. It uses the same section labels and
// direction as Related; a graph can also follow a relation whose endpoint
// noun cannot be known until its stored row is read.
func (r *Runtime) relatedGraphRefs(noun, key string) ([]Referrer, error) {
	refs := r.Referrers(noun)
	if r.RelationStore() == nil {
		return refs, nil
	}
	this := AnyRef(noun, key)
	rels, err := r.RelationsOf(RelationQuery{Thing: this})
	if err != nil {
		return nil, err
	}
	for _, x := range rels {
		other, _, ok := SplitAnyRef(x.Other(this))
		if !ok || r.Noun(other) == nil {
			continue
		}
		outward := x.From == this
		if slices.ContainsFunc(refs, func(f Referrer) bool {
			return f.Relation == x.Kind && f.Noun == other && f.Outward == outward
		}) {
			continue
		}
		label := fmt.Sprintf("%s (%s)", titleWord(r.choicesPlural(Parameter{Noun: other})), x.Kind)
		refs = append(refs, Referrer{Noun: other, Prop: x.Kind, Relation: x.Kind, Outward: outward, Multi: true, Label: label})
	}
	return refs, nil
}
