package ochazuke

import "fmt"

// RekeyReferences migrates references from an accepted selector to a durable
// key. It updates local typed reference props and both ends of relations, never
// foreign stores. It is migration bookkeeping, not a user-editable rename.
func (r *Runtime) RekeyReferences(c Ctx, noun, old, key string) error {
	noun = r.CanonicalNoun(noun)
	if old == "" || key == "" || old == key {
		return nil
	}
	if err := r.rekeyRelations(AnyRef(noun, old), AnyRef(noun, key)); err != nil {
		return err
	}
	for _, n := range r.Operations.Nouns() {
		if n.Store == nil || n.Store.Remote || n.Store.List == nil || n.Store.Patch == nil {
			continue
		}
		var props []PropSpec
		for _, p := range n.Props {
			if !p.ReadOnly && !p.CreateOnly && (p.Noun == AnyNoun || r.CanonicalNoun(p.Noun) == noun) {
				props = append(props, p)
			}
		}
		if len(props) == 0 {
			continue
		}
		ctx := c
		ctx.Noun = n.Name
		things, err := n.Store.List(ctx, ListQuery{Archived: true})
		if err != nil {
			return fmt.Errorf("migrate %s references: %w", n.Name, err)
		}
		for _, thing := range things {
			var changes Thing
			for _, p := range props {
				value, ok := thing[p.Name].(string)
				if !ok {
					continue
				}
				before, after := old, key
				if p.Noun == AnyNoun {
					refNoun, refKey, valid := SplitAnyRef(value)
					if !valid || r.CanonicalNoun(refNoun) != noun || refKey != old {
						continue
					}
					before, after = value, AnyRef(noun, key)
				}
				if value == before {
					if changes == nil {
						changes = Thing{}
					}
					changes[p.Name] = after
				}
			}
			if len(changes) == 0 {
				continue
			}
			id := ""
			if p := n.keyProp(); p != nil {
				id = fmt.Sprint(thing[p.Name])
			}
			if _, err := n.Store.Patch(ctx, id, changes); err != nil {
				return fmt.Errorf("migrate %s %s references: %w", n.Name, id, err)
			}
		}
	}
	return nil
}

func (r *Runtime) rekeyRelations(old, key string) error {
	s := r.RelationStore()
	if s == nil {
		return nil
	}
	rows, err := s.Relations(RelationQuery{Thing: old, Archived: true})
	if err != nil {
		return err
	}
	planned := make(map[string]bool, len(rows))
	afterRows := make([]Relation, 0, len(rows))
	for _, before := range rows {
		after := before.clone()
		if after.From == old {
			after.From = key
		}
		if after.To == old {
			after.To = key
		}
		id := after.ID()
		existing, err := s.Relations(RelationQuery{Kind: after.Kind, From: after.From, To: after.To, Archived: true})
		if err != nil {
			return err
		}
		if len(existing) != 0 || planned[id] {
			return fmt.Errorf("cannot migrate %s to %s: conflicting relation", old, key)
		}
		planned[id] = true
		afterRows = append(afterRows, after)
	}
	for i, after := range afterRows {
		if err := s.Put(after); err != nil {
			return err
		}
		if err := s.Remove(rows[i]); err != nil {
			return err
		}
	}
	if len(rows) > 0 {
		r.relationsChanged()
	}
	return nil
}
