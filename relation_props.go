package ochazuke

import (
	"fmt"
	"maps"
	"strings"
)

// Relation-backed props (PropSpec.Relation): the value is held by the
// runtime's RelationStore, not by the noun's own store. The noun's store is
// wrapped when the noun has such a prop (wrapRelations), so every reader and
// writer of a thing (get, set, create, the edit form, undo, the related
// sections) sees the prop in the thing without a special path.

// relationNoun is the noun the prop's relations point at: Items.Noun of a
// list, Noun of a single prop; AnyNoun when the To ends are of any noun.
func (p PropSpec) relationNoun() string {
	if p.Type == SettingList {
		if p.Items == nil {
			return ""
		}
		return p.Items.Noun
	}
	return p.Noun
}

// relationProps are the noun's relation-backed props.
func (n *Noun) relationProps() []PropSpec {
	var out []PropSpec
	for _, p := range n.Props {
		if p.Relation != "" {
			out = append(out, p)
		}
	}
	return out
}

// storeRaw are a store's handlers before wrapRelations wrapped them.
type storeRaw struct {
	Get    func(Ctx, string) (Thing, error)
	Patch  func(Ctx, string, Thing) (Thing, error)
	Create func(Ctx, Thing) (Thing, error)
}

// propRelationEnd is the To end a prop's item names: the item as it is for
// an any-noun prop ("noun:key"), else the noun and the key.
func (p PropSpec) propRelationEnd(item string) string {
	if noun := p.relationNoun(); noun != AnyNoun {
		return AnyRef(noun, item)
	}
	return item
}

// readRelationProp is the prop's value for the thing key of n: the To keys
// (a list in position order, or the one for a single prop, nil for none).
func (r *Runtime) readRelationProp(p PropSpec, n *Noun, key string) (any, error) {
	list := p.Type == SettingList
	if r.RelationStore() == nil || key == "" {
		if list {
			return []string{}, nil
		}
		return nil, nil
	}
	rels, err := r.RelationsOf(RelationQuery{Kind: p.Relation, From: AnyRef(n.Name, key)})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, x := range rels {
		noun, k, _ := SplitAnyRef(x.To)
		switch {
		case p.relationNoun() == AnyNoun:
			out = append(out, x.To)
		case noun == p.relationNoun():
			out = append(out, k)
		}
	}
	if list {
		if out == nil {
			out = []string{}
		}
		return out, nil
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out[0], nil
}

// writeRelationProp makes the prop's relations the value (a []string, one
// key, or none).
func (r *Runtime) writeRelationProp(c Ctx, p PropSpec, n *Noun, key string, v any) error {
	var items []string
	switch x := v.(type) {
	case nil:
	case []string:
		items = x
	case string:
		if x != "" {
			items = []string{x}
		}
	default:
		return fmt.Errorf("%s.%s: relation values are keys", r.DisplayNoun(n.Name), p.Name)
	}
	ends := make([]string, len(items))
	for i, it := range items {
		ends[i] = p.propRelationEnd(it)
	}
	return r.SetRelated(c, p.Relation, AnyRef(n.Name, key), ends)
}

// wrapRelations (re)builds the noun's store handlers around its relation
// props; a noun with none gets its own handlers back. It runs whenever the
// store or the props change (applyStore).
func (r *Runtime) wrapRelations(n *Noun) {
	s := n.Store
	if s == nil {
		return
	}
	if s.raw != nil {
		s.Get, s.Patch, s.Create = s.raw.Get, s.raw.Patch, s.raw.Create
		s.raw = nil
	}
	if len(n.relationProps()) == 0 || s.Kind == Singleton {
		return
	}
	raw := &storeRaw{Get: s.Get, Patch: s.Patch, Create: s.Create}
	s.raw = raw
	name := n.Name
	fill := func(c Ctx, t Thing, key string) (Thing, error) {
		noun := c.Runtime.Nouns[name]
		t = maps.Clone(t)
		for _, p := range noun.relationProps() {
			v, err := c.Runtime.readRelationProp(p, noun, key)
			if err != nil {
				return nil, err
			}
			t[p.Name] = v
		}
		return t, nil
	}
	split := func(noun *Noun, values Thing) (rest, rel Thing) {
		rest, rel = Thing{}, Thing{}
		for k, v := range values {
			if p := noun.prop(k); p != nil && p.Relation != "" {
				rel[k] = v
			} else {
				rest[k] = v
			}
		}
		return rest, rel
	}
	apply := func(c Ctx, noun *Noun, key string, rel Thing) error {
		for _, p := range noun.relationProps() {
			if v, ok := rel[p.Name]; ok {
				if err := c.Runtime.quietly(func() error { return c.Runtime.writeRelationProp(c, p, noun, key, v) }); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if raw.Get != nil {
		s.Get = func(c Ctx, id string) (Thing, error) {
			t, err := raw.Get(c, id)
			if err != nil || t == nil {
				return t, err
			}
			return fill(c, t, id)
		}
	}
	if raw.Patch != nil {
		s.Patch = func(c Ctx, id string, ch Thing) (Thing, error) {
			noun := c.Runtime.Nouns[name]
			rest, rel := split(noun, ch)
			if len(rel) == 0 {
				return raw.Patch(c, id, ch)
			}
			var t Thing
			if len(rest) > 0 {
				var err error
				if t, err = raw.Patch(c, id, rest); err != nil {
					return nil, err
				}
			}
			if err := apply(c, noun, id, rel); err != nil {
				return nil, err
			}
			if t == nil && raw.Get != nil {
				var err error
				if t, err = raw.Get(c, id); err != nil {
					return nil, err
				}
			}
			if t == nil {
				return nil, nil
			}
			return fill(c, t, id)
		}
	}
	if raw.Create != nil {
		s.Create = func(c Ctx, values Thing) (Thing, error) {
			noun := c.Runtime.Nouns[name]
			rest, rel := split(noun, values)
			t, err := raw.Create(c, rest)
			if err != nil || len(rel) == 0 {
				return t, err
			}
			if t == nil {
				t = rest
			}
			kp := noun.keyProp()
			if kp == nil || emptyProp(t[kp.Name]) {
				return nil, fmt.Errorf("%s: the created thing has no key to relate", c.Runtime.DisplayNoun(name))
			}
			key := strings.TrimSpace(fmt.Sprint(t[kp.Name]))
			if err := apply(c, noun, key, rel); err != nil {
				return nil, err
			}
			return fill(c, t, key)
		}
	}
}
