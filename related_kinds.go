package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// The kinds of section a related stack makes beside one per referrer:
// value sections of a split referrer (RelatedSpec.GroupBy), group
// headings (RelatedSpec.Groups) and list sections (RelatedSpec.Lists).
// Each is a relatedSection, so the stack's settings, verbs, motions and
// drawing treat them as any other; what differs is where its things come
// from, which is data in its Referrer.

// addSplit registers a split of ref by prop: its value sections are made
// as its things are read. The noun is watched as a section's would be.
func (rel *Related) addSplit(ref Referrer, prop string) {
	if rel.splits[ref.ID()] != nil {
		return
	}
	n := rel.r.Nouns[ref.Noun]
	if n == nil || n.keyProp() == nil {
		return
	}
	stored := n.Store != nil && (n.Store.List != nil || n.Store.Query != nil)
	if !stored && rel.spec.Rows == nil && ref.Relation == "" {
		return
	}
	rel.splits[ref.ID()] = &relatedSplit{ref: ref, prop: prop, at: len(rel.sections)}
	if stored && ref.Relation == "" {
		rel.watch(ref.Noun)
	}
}

// splitValue is the value a thing holds in the split prop, as text; ""
// when it holds none.
func splitValue(t Thing, prop string) string {
	v, ok := t[prop]
	if !ok || emptyProp(v) {
		return ""
	}
	return fmt.Sprint(v)
}

// valueRef is the referrer of one value of a split.
func (sp *relatedSplit) valueRef(value string) Referrer {
	ref := sp.ref
	label := "Other " + pluralWord(shortNounName(ref.Noun))
	if value != "" {
		label = titleWord(pluralWord(value))
	}
	ref.Section = ref.Noun + "." + sp.prop + "=" + value
	ref.Label = label
	ref.Match = map[string]any{sp.prop: value}
	return ref
}

// fillSplit reads the split's things once and shows each value's in its
// section, in the order the values first appear; a value no thing holds
// any more keeps its section, empty (hidden in the hide mode).
func (rel *Related) fillSplit(sp *relatedSplit) {
	r := rel.r
	things, err := rel.readRef(sp.ref)
	if err != nil {
		r.UI().Error(fmt.Sprintf("%s section: %v", r.DisplayNoun(sp.ref.Noun), err))
		things = nil
	}
	byValue := map[string][]Thing{}
	var values []string
	for _, t := range things {
		v := splitValue(t, sp.prop)
		if _, ok := byValue[v]; !ok {
			values = append(values, v)
		}
		byValue[v] = append(byValue[v], t)
	}
	for _, v := range values {
		ref := sp.valueRef(v)
		s := rel.byID[ref.ID()]
		if s == nil {
			if s = rel.addSection(ref); s == nil {
				continue
			}
			s.split = sp
			rel.placeValueSection(s)
		}
		s.show(byValue[v])
	}
	for _, s := range slices.Clone(rel.sections) {
		if s.split == sp && byValue[splitValue(s.ref.Match, sp.prop)] == nil {
			s.show(nil)
		}
	}
}

// placeValueSection moves a new value section from the end of the
// sections to its split's place: after the split's other values, else
// where the split's referrer came in the referrers' order.
func (rel *Related) placeValueSection(s *relatedSection) {
	rel.sections = slices.DeleteFunc(rel.sections, func(o *relatedSection) bool { return o == s })
	at := -1
	for i, o := range rel.sections {
		if o.split == s.split {
			at = i + 1
		}
	}
	if at < 0 {
		at = min(s.split.at, len(rel.sections))
	}
	rel.sections = slices.Insert(rel.sections, at, s)
}

// ensureLists makes the list sections the spec names, whatever the
// stack's thing, and reads each once.
func (rel *Related) ensureLists() {
	for _, l := range rel.spec.Lists {
		if l.ID == "" || rel.byID[l.ID] != nil {
			continue
		}
		noun := rel.r.CanonicalNoun(l.Noun)
		label := l.Label
		if label == "" {
			label = titleWord(rel.r.choicesPlural(Parameter{Noun: noun}))
		}
		s := rel.addSection(Referrer{Noun: noun, Section: l.ID, Filter: l.Filter, New: l.New, List: true, Label: label})
		if s != nil {
			s.fill()
		}
	}
}

// readList lists a list section's things: its noun's saved filter
// (conditions and sort), through the store's standard list, or the Rows
// hook for a noun without a store (with an empty key).
func (rel *Related) readList(c Ctx, ref Referrer) ([]Thing, error) {
	r := rel.r
	n := r.Noun(ref.Noun)
	if n == nil {
		return nil, nil
	}
	c.Noun = n.Name
	q := ListQuery{Archived: true}
	if ref.Filter != "" {
		filters, err := r.SavedFilters("", n.Name)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(filters, func(f SavedFilter) bool { return f.Name == ref.Filter })
		if i < 0 {
			return nil, fmt.Errorf("%s has no saved filter %q", r.DisplayNoun(n.Name), ref.Filter)
		}
		for _, cond := range filters[i].conditions() {
			typed, err := typedFilter(n, cond)
			if err != nil {
				return nil, err
			}
			q.Filters = append(q.Filters, typed)
		}
		q.Sort = slices.Clone(filters[i].Sort)
	}
	if n.Store == nil || n.Store.List == nil && n.Store.Query == nil {
		if rel.spec.Rows == nil {
			return nil, nil
		}
		things, err := rel.spec.Rows(c, ref, "")
		return r.queryAll(n, things, q).Items, err
	}
	if n.Store.Remote && c.tape == nil {
		return nil, nil // as any section: a stack starts no remote work itself
	}
	page, err := r.queryThings(c, n, q)
	return page.Items, err
}

// relatedGroupEntry is one entry of the groups setting.
type relatedGroupEntry struct {
	id      string
	members []string
}

// parseRelatedGroups reads the groups setting's entries, "ID = A, B";
// an entry without an id or members is skipped.
func parseRelatedGroups(entries []string) []relatedGroupEntry {
	var out []relatedGroupEntry
	for _, e := range entries {
		id, list, ok := strings.Cut(e, "=")
		id = strings.TrimSpace(id)
		if !ok || id == "" {
			continue
		}
		g := relatedGroupEntry{id: id}
		for _, m := range strings.Split(list, ",") {
			if m = strings.TrimSpace(m); m != "" {
				g.members = append(g.members, m)
			}
		}
		if len(g.members) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// names reports whether the group entry takes s in: a member that is its
// noun, or that names its id (a prefix ending in *).
func (g relatedGroupEntry) names(s *relatedSection) bool {
	for _, m := range g.members {
		if m == s.ref.Noun || noSpillMatches(m, s.ref.ID()) {
			return true
		}
	}
	return false
}

// relatedGroupLabel is a group heading's text: its id with dashes as spaces,
// the first letter upper case (needs-you is Needs you).
func relatedGroupLabel(id string) string {
	label := strings.ReplaceAll(id, "-", " ")
	return strings.ToUpper(label[:1]) + label[1:]
}

// syncGroups makes the group headings the groups setting names over the
// sections there are now, when the setting or the sections changed: a
// section in a group draws its rows under the group's heading. A section
// is in the first group that names it; the history section is never in
// one.
func (rel *Related) syncGroups() {
	if rel.spec.Groups == "" {
		return
	}
	entries := rel.listSetting(rel.spec.Groups)
	if fmt.Sprint(rel.shape, entries) == rel.grouped {
		return
	}
	was := map[*relatedSection]*relatedSection{}
	for _, s := range rel.sections {
		if !s.grouping {
			was[s] = s.group
			s.group = nil
		}
	}
	keep := rel.assignGroups(parseRelatedGroups(entries))
	for _, s := range slices.Clone(rel.sections) {
		if s.grouping && !keep[s.ref.ID()] {
			s.remove()
			rel.sections = slices.DeleteFunc(rel.sections, func(o *relatedSection) bool { return o == s })
			delete(rel.byID, s.ref.ID())
			rel.shape++
		}
	}
	for _, s := range rel.sections {
		if !s.grouping && was[s] != s.group && s.list != nil {
			s.render() // a heading of its own again, or none
		}
	}
	for _, s := range rel.sections {
		if s.grouping {
			s.render()
		}
	}
	rel.grouped = fmt.Sprint(rel.shape, entries)
}

// assignGroups puts each section in the first group that names it,
// making the group headings that have members; it answers the ids of the
// groups kept.
func (rel *Related) assignGroups(groups []relatedGroupEntry) map[string]bool {
	keep := map[string]bool{}
	for _, g := range groups {
		var members []*relatedSection
		for _, s := range rel.sections {
			if !s.grouping && s.group == nil && !s.ref.History && g.names(s) {
				members = append(members, s)
			}
		}
		head := rel.byID[g.id]
		if len(members) == 0 || head != nil && !head.grouping {
			continue
		}
		if head == nil {
			if head = rel.addGroup(g.id); head == nil {
				continue
			}
		}
		head.members = members
		for _, m := range members {
			m.group = head
		}
		keep[g.id] = true
	}
	return keep
}

// addGroup makes a group heading: a section with a list holding only its
// heading row.
func (rel *Related) addGroup(id string) *relatedSection {
	r := rel.r
	s := &relatedSection{rel: rel, ref: Referrer{Section: id, Label: relatedGroupLabel(id)}, id: rel.id + "/" + id, grouping: true}
	r.Instance(s.id, Parent(rel.id), Kind(RelatedNoun))
	r.Instances[s.id].Component = s
	r.Instances[s.id].Label = s.ref.Label
	list, err := r.AddList(s.id+"/list", nil, Parent(s.id))
	if err != nil {
		_ = r.RemoveInstance(s.id)
		return nil
	}
	s.list = list
	rel.sections = append(rel.sections, s)
	rel.byID[id] = s
	rel.shape++
	return s
}

// RelatedEndsStay is the value of a stack's Ends setting that keeps j and
// k inside it at its first and last drawn rows.
const RelatedEndsStay = "stay"

// edgeTrapper is a component on a list's ancestry that keeps the list's
// motions inside it at an edge (Collection.trappedAt asks it): a related
// stack whose Ends setting says stay, at its first and last drawn rows.
type edgeTrapper interface {
	trapsEdge(list string, edge SpillEdge) bool
}

// trapsEdge reports whether j or k past edge of list stays: the stack's
// Ends setting is "stay" and list is its first drawn section (EdgeFirst)
// or its last (EdgeLast).
func (rel *Related) trapsEdge(list string, edge SpillEdge) bool {
	if rel.spec.Ends == "" {
		return false
	}
	if v, ok := rel.r.settingValue(rel.spec.Ends); !ok || fmt.Sprint(v) != RelatedEndsStay {
		return false
	}
	v := rel.visible()
	if len(v) == 0 {
		return false
	}
	switch edge {
	case EdgeFirst:
		return v[0].list.id == list
	case EdgeLast:
		return v[len(v)-1].list.id == list
	}
	return false
}
