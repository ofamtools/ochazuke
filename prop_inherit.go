package ochazuke

import (
	"fmt"
	"slices"
)

// Inheritance as declared data (spine S5 rule 3, decisions 38 and 40): a
// prop that declares Inherit names a reference prop of its noun (the
// parent, a pane's session); while the thing's own value of the prop is
// not set, a read shows the value of the same prop of the thing that
// reference names, and so on up the references until one is set. An empty
// value is not set: a thing whose own value was cleared follows its
// parent. One mechanism for every noun; no noun has code of its own.
//
// The framework reads it as a derivation of the inherit kind (addProp
// turns Inherit into one), so get's record, list's rows, filters, sorts
// and change tokens all show the inherited value; the store keeps only the
// thing's own value, and a write writes that. Application code that reads
// stored records itself (off the update loop, from its own database) walks
// the same declarations through Runtime.Inheritance.

// inheritKind is the derivation kind a prop with Inherit is read by.
const inheritKind = "inherit"

// inheritDerivation is the derivation a prop declaring Inherit gets: its
// own value, else REF.PROP walked.
func inheritDerivation(p PropSpec) *Derivation {
	return &Derivation{Of: inheritKind, Reads: []string{p.Name, p.Inherit + "." + p.Name}}
}

// declaredRef is p with what its reference declarations say written out:
// Inherit read as the inherit derivation (an explicit Derivation wins), and
// a reference over one noun naming that noun (its picker, its label).
func declaredRef(p PropSpec) PropSpec {
	if p.Inherit != "" && p.Derivation == nil {
		p.Derivation = inheritDerivation(p)
	}
	if p.Noun == "" && len(p.Ref) == 1 {
		p.Noun = p.Ref[0]
	}
	return p
}

// refNouns are the nouns a reference prop may name: its Ref, else its
// Noun; none for a prop that is not a reference.
func (p PropSpec) refNouns() []string {
	if len(p.Ref) > 0 {
		return p.Ref
	}
	if p.Noun != "" {
		return []string{p.Noun}
	}
	return nil
}

// inheritLink is how one prop of a noun inherits: the reference prop it
// follows, the nouns that reference may name, and the nouns the prop
// itself names (a reference prop's own nouns; none for a plain value).
type inheritLink struct {
	ref   string
	nouns []string
	self  []string
}

// Inheritance is a copy of a runtime's inheritance declarations (which
// prop of which noun inherits through which reference): a plain value, so
// code off the update loop may walk it (Value) without reading the
// runtime.
type Inheritance struct {
	links map[string]map[string]inheritLink // noun -> prop -> link
}

// Inheritance is the runtime's inheritance declarations as they are now
// (every noun's props that declare Inherit).
func (r *Runtime) Inheritance() Inheritance {
	h := Inheritance{links: map[string]map[string]inheritLink{}}
	canonical := func(nouns []string) []string {
		out := make([]string, 0, len(nouns))
		for _, noun := range nouns {
			out = append(out, r.CanonicalNoun(noun))
		}
		return out
	}
	for name, n := range r.Nouns {
		for _, p := range n.Props {
			if p.Inherit == "" {
				continue
			}
			ref := n.prop(p.Inherit)
			if ref == nil || ref.Name == p.Name {
				continue
			}
			if h.links[name] == nil {
				h.links[name] = map[string]inheritLink{}
			}
			h.links[name][p.Name] = inheritLink{ref: ref.Name, nouns: canonical(ref.refNouns()), self: canonical(p.refNouns())}
		}
	}
	return h
}

// Inherits reports whether noun's prop is declared to inherit.
func (h Inheritance) Inherits(noun, prop string) bool {
	_, ok := h.links[noun][prop]
	return ok
}

// Value is prop as a read of the thing t of noun shows it: t's own value
// when set; else, through the reference the prop inherits by (read as it
// shows, so itself inherited): when that reference names a thing of a
// noun the prop itself references, that thing (a pane's project is its
// owner when the owner is a project); otherwise the value of the prop on
// the thing it names (read gives that thing's stored record, its props by
// name, nil when there is none), walked up until one is set. Nil when no
// thing on the way sets it; a walk that comes back to a thing ends.
func (h Inheritance) Value(noun string, t Thing, prop string, read func(noun, key string) Thing) any {
	return h.value(noun, t, prop, read, map[string]bool{})
}

func (h Inheritance) value(noun string, t Thing, prop string, read func(noun, key string) Thing, seen map[string]bool) any {
	if t == nil {
		return nil
	}
	if v := t[prop]; !emptyProp(v) {
		return v
	}
	link, ok := h.links[noun][prop]
	if !ok {
		return nil
	}
	target, key, ok := link.target(h.value(noun, t, link.ref, read, seen))
	if !ok {
		return nil
	}
	if slices.Contains(link.self, target) {
		return Ref{Noun: target, Key: key}.Stored(link.self)
	}
	id := target + "\x00" + key + "\x00" + prop
	if seen[id] {
		return nil
	}
	seen[id] = true
	return h.value(target, read(target, key), prop, read, seen)
}

// target is the thing a reference value names: its noun and key. A
// reference that may name one noun holds the key; one over several nouns
// holds "noun:key" (Ref.Stored). False for an empty value or a noun the
// reference may not name.
func (l inheritLink) target(v any) (noun, key string, ok bool) {
	if emptyProp(v) {
		return "", "", false
	}
	text := fmt.Sprint(v)
	if len(l.nouns) == 1 {
		return l.nouns[0], RefOf(l.nouns[0], text).Key, true
	}
	ref, typed := ParseRef(text)
	if !typed || !slices.Contains(l.nouns, ref.Noun) {
		return "", "", false
	}
	return ref.Noun, ref.Key, true
}

// deriveInherit is the inherit kind: each thing whose own value of the
// prop is not set reads it through the noun's inheritance (Value), each
// parent read once for the batch through its noun's store (the parents of
// the batch in one query when the store has one), never written: a read
// for a derivation is read-only (Ctx.ReadOnly).
func deriveInherit(c Ctx, n *Noun, p PropSpec, things []Thing) {
	h := c.Runtime.Inheritance()
	link, ok := h.links[n.Name][p.Name]
	if !ok {
		return
	}
	got := batchRefs(c)
	if len(link.nouns) == 1 && !slices.Contains(link.self, link.nouns[0]) {
		readRefs(c, c.Runtime.Noun(link.nouns[0]), batchKeys(things, link.ref), got)
	}
	read := func(noun, key string) Thing {
		id := noun + "\x00" + key
		if t, ok := got[id]; ok {
			return t
		}
		var t Thing
		if other := c.Runtime.Noun(noun); other != nil && other.Store != nil && other.Store.Get != nil {
			oc := c
			oc.Noun, oc.underived = other.Name, true
			t, _ = other.Store.Get(oc, key)
		}
		got[id] = t
		return t
	}
	for _, t := range things {
		if !emptyProp(t[p.Name]) {
			continue
		}
		if v := h.Value(n.Name, t, p.Name, read); v != nil {
			t[p.Name] = v
		}
	}
}
