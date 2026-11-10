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
	if p.listed() && p.Items != nil {
		return p.Items.Noun
	}
	if p.listed() {
		return ""
	}
	return p.Noun
}

// listed reports whether the prop holds a list (the one place the type is
// read, so a list prop is a fact of its declaration).
func (p PropSpec) listed() bool { return p.Type == SettingList }

// relationError reports whether a relation prop is declared wrongly: it
// needs a noun for its ends, is a list with Items or a string, and is none
// of a key, a role, a Through write or a computed prop.
func (p PropSpec) relationError() bool {
	if p.Relation == "" {
		return false
	}
	kinds := p.listed() && p.Items != nil || !p.listed() && p.Type == SettingString
	return p.relationNoun() == "" || p.Key || p.Read != nil || p.Subject != "" || p.Role != "" || p.Through || !kinds
}

// relationPropInit finishes a relation prop's declaration: it is not
// filtered (its value is not in the row) and its item noun is canonical.
func (r *Runtime) relationPropInit(p *PropSpec) {
	if p.Relation == "" {
		return
	}
	p.NoFilter = true
	if p.Items != nil {
		items := *p.Items
		if items.Noun != "" && items.Noun != AnyNoun {
			items.Noun = r.CanonicalNoun(items.Noun)
		}
		p.Items = &items
	}
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
	list := p.listed()
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
	w := relationWrap{noun: n.Name, raw: raw}
	if raw.Get != nil {
		s.Get = w.get
	}
	if raw.Patch != nil {
		s.Patch = w.patch
	}
	if raw.Create != nil {
		s.Create = w.create
	}
}

// relationWrap is the wrapped handlers of one noun's store.
type relationWrap struct {
	noun string
	raw  *storeRaw
}

// fill adds the relation props to the thing t of key.
func (w relationWrap) fill(c Ctx, t Thing, key string) (Thing, error) {
	n := c.Runtime.Nouns[w.noun]
	t = maps.Clone(t)
	for _, p := range n.relationProps() {
		v, err := c.Runtime.readRelationProp(p, n, key)
		if err != nil {
			return nil, err
		}
		t[p.Name] = v
	}
	return t, nil
}

// split separates the values of relation props from the rest.
func (w relationWrap) split(n *Noun, values Thing) (rest, rel Thing) {
	rest, rel = Thing{}, Thing{}
	for k, v := range values {
		if p := n.prop(k); p != nil && p.Relation != "" {
			rel[k] = v
		} else {
			rest[k] = v
		}
	}
	return rest, rel
}

// apply writes the relation props' values for the thing key, without noting
// relation changes (the thing's own change covers undo).
func (w relationWrap) apply(c Ctx, n *Noun, key string, rel Thing) error {
	for _, p := range n.relationProps() {
		if v, ok := rel[p.Name]; ok {
			if err := c.Runtime.quietly(func() error { return c.Runtime.writeRelationProp(c, p, n, key, v) }); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w relationWrap) get(c Ctx, id string) (Thing, error) {
	t, err := w.raw.Get(c, id)
	if err != nil || t == nil {
		return t, err
	}
	return w.fill(c, t, id)
}

func (w relationWrap) patch(c Ctx, id string, ch Thing) (Thing, error) {
	n := c.Runtime.Nouns[w.noun]
	rest, rel := w.split(n, ch)
	if len(rel) == 0 {
		return w.raw.Patch(c, id, ch)
	}
	var t Thing
	var err error
	if len(rest) > 0 {
		if t, err = w.raw.Patch(c, id, rest); err != nil {
			return nil, err
		}
	}
	if err := w.apply(c, n, id, rel); err != nil {
		return nil, err
	}
	if t == nil && w.raw.Get != nil {
		if t, err = w.raw.Get(c, id); err != nil {
			return nil, err
		}
	}
	if t == nil {
		return nil, nil
	}
	return w.fill(c, t, id)
}

func (w relationWrap) create(c Ctx, values Thing) (Thing, error) {
	n := c.Runtime.Nouns[w.noun]
	rest, rel := w.split(n, values)
	t, err := w.raw.Create(c, rest)
	if err != nil || len(rel) == 0 {
		return t, err
	}
	if t == nil {
		t = rest
	}
	kp := n.keyProp()
	if kp == nil || emptyProp(t[kp.Name]) {
		return nil, fmt.Errorf("%s: the created thing has no key to relate", c.Runtime.DisplayNoun(w.noun))
	}
	key := strings.TrimSpace(fmt.Sprint(t[kp.Name]))
	if err := w.apply(c, n, key, rel); err != nil {
		return nil, err
	}
	return w.fill(c, t, key)
}
