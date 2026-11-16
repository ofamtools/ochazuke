package ochazuke

// Stored field names (PropSpec.Field): where a document a store keeps
// holds a prop under another name (a task's project in project_id), the
// noun says so on the prop and DocStore renames between the two. Every
// helper here is a no-op for a noun whose props declare no Field.

// fieldOf is the stored field that holds the prop of that name (or alias,
// or @role); a name that is no prop is its own field.
func (n *Noun) fieldOf(name string) string {
	p := n.prop(name)
	switch {
	case p == nil:
		return name
	case p.Field != "":
		return p.Field
	}
	return p.Name
}

// renamedProps are the props stored under another field name.
func (n *Noun) renamedProps() []PropSpec {
	if n == nil {
		return nil
	}
	var out []PropSpec
	for _, p := range n.Props {
		if p.Field != "" && p.Field != p.Name && p.Derivation == nil {
			out = append(out, p)
		}
	}
	return out
}

// docThing is a stored document as the noun's thing: each renamed field
// under its prop's name, every other field as it is (a copy).
func (n *Noun) docThing(doc Thing) Thing {
	if doc == nil {
		return nil
	}
	out := cloneThing(doc)
	for _, p := range n.renamedProps() {
		if v, ok := doc[p.Field]; ok {
			delete(out, p.Field)
			out[p.Name] = v
		}
	}
	return out
}

// storedDoc is a thing as its stored document: each renamed prop under its
// field, no derived prop, every other field as it is (a copy).
func (n *Noun) storedDoc(t Thing) Thing {
	if t == nil {
		return nil
	}
	out := cloneThing(t)
	if n == nil {
		return out
	}
	for _, p := range n.derivedProps() {
		delete(out, p.Name)
	}
	for _, p := range n.renamedProps() {
		if v, ok := t[p.Name]; ok {
			delete(out, p.Name)
			out[p.Field] = v
		}
	}
	return out
}

// propOfField is the prop name a stored field holds; a field no prop
// renames is its own name.
func (n *Noun) propOfField(field string) string {
	for _, p := range n.renamedProps() {
		if p.Field == field {
			return p.Name
		}
	}
	return field
}

// fieldQuery is a DocQuery in prop names as the table reads it: every
// name its field.
func (n *Noun) fieldQuery(q DocQuery) DocQuery {
	return renameQuery(q, n.fieldOf)
}

// propQuery is a DocQuery in field names back in prop names.
func (n *Noun) propQuery(q DocQuery) DocQuery {
	if n == nil {
		return q
	}
	return renameQuery(q, n.propOfField)
}

func renameQuery(q DocQuery, name func(string) string) DocQuery {
	q.Filters = clonePropFilters(q.Filters)
	for i := range q.Filters {
		q.Filters[i].Prop = name(q.Filters[i].Prop)
	}
	q.Sort, q.GroupSort = renameKeys(q.Sort, name), renameKeys(q.GroupSort, name)
	q.GroupBy = renameAll(q.GroupBy, name)
	q.TextFields = renameAll(q.TextFields, name)
	if q.Hide != "" {
		q.Hide = name(q.Hide)
	}
	return q
}

func renameKeys(keys []SortKey, name func(string) string) []SortKey {
	if keys == nil {
		return nil
	}
	out := make([]SortKey, len(keys))
	for i, k := range keys {
		out[i] = SortKey{Field: name(k.Field), Desc: k.Desc}
	}
	return out
}

func renameAll(names []string, name func(string) string) []string {
	if names == nil {
		return nil
	}
	out := make([]string, len(names))
	for i, s := range names {
		out[i] = name(s)
	}
	return out
}
