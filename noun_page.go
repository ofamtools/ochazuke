package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The noun page (nouns/NOUN): one page for every noun, whatever its kind,
// built from what the registry and the noun's store can answer and never
// from the noun's name. The questions it asks are plain values the page
// only draws:
//
//   - NounAnswers: what the noun's store can answer (list, page, get, is
//     it remote, are its things keyed), how many live instances it has,
//     its props, who refers to it and what it refers to, and so where the
//     page's rows come from (NounRows);
//   - ThingProps: a thing's props, every declared one first, then the ones
//     it carries that are not declared;
//   - OwnReferences beside Referrers: the relations of a thing in both
//     directions, which the page's related stack draws one collapsed
//     branch each, with its count (related.go).
//
// The rows come from the store when it lists keyed things; else from its
// one thing (a store without keys: a singleton, a snapshot); else from the
// noun's live instances; else the page has one row, the noun's own
// declaration (an ochazuke.noun thing), saying why. A store whose list
// fails shows the declaration row with the failure. So a noun page is never
// empty without saying why, and opening it never fails.

// Where a noun page's rows come from (NounAnswers.Rows).
const (
	// NounRowsStore: the store lists the noun's keyed things.
	NounRowsStore = "store"
	// NounRowsUnkeyed: a store without keys; its thing (or things) read once.
	NounRowsUnkeyed = "unkeyed"
	// NounRowsInstances: no store lists; the noun's live instances.
	NounRowsInstances = "instances"
	// NounRowsDeclaration: nothing to list; the noun's own declaration.
	NounRowsDeclaration = "declaration"
)

// instanceRowNoun is the noun of a noun page's row standing for a live
// instance of a noun without a store: the instance as a thing, so the
// row never takes the keys of a drawn part (a form field's i).
const instanceRowNoun = "ochazuke.instance"

// NounAnswers is what a noun's store and the registry can answer about
// it: the noun page reads it to choose its rows and to say why.
type NounAnswers struct {
	Noun string `json:"noun"`
	Kind string `json:"kind"`
	// Store reports a store; Lists, Pages, Gets and Remote what it does;
	// Keyed that its things have a key prop to name rows by.
	Store  bool `json:"store"`
	Lists  bool `json:"lists"`
	Pages  bool `json:"pages"`
	Gets   bool `json:"gets"`
	Remote bool `json:"remote"`
	Keyed  bool `json:"keyed"`
	// Instances counts the noun's live instances now.
	Instances int `json:"instances"`
	// Props are the declared props' names, in order.
	Props []string `json:"props"`
	// Referrers counts the props of other nouns (and declared relation
	// kinds) that name this noun's things; References the noun's own
	// reference props.
	Referrers  int `json:"referrers"`
	References int `json:"references"`
	// Rows is where the page's rows come from (NounRowsStore, ...), Why
	// says so in words when they are not the store's things.
	Rows string `json:"rows"`
	Why  string `json:"why,omitempty"`
}

// NounAnswers answers what the noun's store and the registry can say about
// it. An unknown noun answers the declaration source with why.
func (r *Runtime) NounAnswers(noun string) NounAnswers {
	n := r.Nouns[r.CanonicalNoun(noun)]
	if n == nil {
		return NounAnswers{Noun: noun, Rows: NounRowsDeclaration, Why: "no such noun"}
	}
	a := NounAnswers{Noun: n.Name, Kind: r.NounKind(n.Name), Keyed: n.keyProp() != nil, Props: []string{}}
	if s := n.Store; s != nil {
		a.Store, a.Lists, a.Pages, a.Gets, a.Remote = true, s.List != nil || s.Query != nil, s.Query != nil, s.Get != nil, s.Remote
	}
	for _, p := range n.Props {
		a.Props = append(a.Props, p.Name)
	}
	for _, inst := range r.Instances {
		if inst.Noun == n.Name {
			a.Instances++
		}
	}
	a.Referrers, a.References = len(r.Referrers(n.Name)), len(r.OwnReferences(n.Name))
	a.Rows, a.Why = nounRowsOf(a)
	return a
}

// nounRowsOf is where a noun page's rows come from, and why when it is
// not the store's things: the first of store, thing, instances and
// declaration the answers allow.
func nounRowsOf(a NounAnswers) (rows, why string) {
	switch {
	case a.Lists && a.Keyed:
		return NounRowsStore, ""
	case a.Store && !a.Remote && (a.Lists || a.Gets):
		return NounRowsUnkeyed, "its store has no key prop: its thing as it reads now"
	case a.Instances > 0:
		return NounRowsInstances, instancesWhy(a)
	}
	return NounRowsDeclaration, declarationWhy(a)
}

func instancesWhy(a NounAnswers) string {
	if a.Store {
		return "its store cannot list here: its live instances"
	}
	return "it has no store: its live instances"
}

func declarationWhy(a NounAnswers) string {
	switch {
	case a.Store && a.Remote && !a.Keyed:
		return "its store is remote and has no key prop, and nothing of it is live now: its declaration"
	case a.Store:
		return "its store cannot list, and nothing of it is live now: its declaration"
	case a.Kind == NounKindCommand:
		return "a command noun: no store and nothing live now; its declaration and its verbs"
	}
	return "no store and nothing of it is live now: its declaration"
}

// ThingProp is one prop of a thing as the noun page shows it: its name and
// value as text, whether the noun declares it, and whether it is secret
// (its value is not shown).
type ThingProp struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Declared bool   `json:"declared"`
	Secret   bool   `json:"secret,omitempty"`
}

// ThingProps are a thing's props: every prop the noun declares, in
// declaration order (an unset one with an empty value, a secret one with
// its value left out), then every prop the thing carries that is not
// declared, by name.
func (r *Runtime) ThingProps(noun string, t Thing) []ThingProp {
	n := r.Nouns[r.CanonicalNoun(noun)]
	out := []ThingProp{}
	declared := map[string]bool{}
	if n != nil {
		for _, p := range n.Props {
			declared[p.Name] = true
			prop := ThingProp{Name: p.Name, Declared: true, Secret: p.Secret}
			if v, ok := t[p.Name]; ok && v != nil && !p.Secret {
				prop.Value = cellValueText(v)
			}
			out = append(out, prop)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(t)) {
		if !declared[name] {
			out = append(out, ThingProp{Name: name, Value: cellValueText(t[name])})
		}
	}
	return out
}

// thingPropsText is props as name: value lines, the names padded to one
// width; undeclared ones only with extras.
func thingPropsText(props []ThingProp, declared bool) string {
	width := 0
	for _, p := range props {
		if declared || !p.Declared {
			width = max(width, len(p.Name))
		}
	}
	var b strings.Builder
	for _, p := range props {
		if !declared && p.Declared {
			continue
		}
		value := p.Value
		if p.Secret {
			value = "(secret)"
		}
		fmt.Fprintf(&b, "%-*s  %s\n", width, p.Name, value)
	}
	return strings.TrimRight(b.String(), "\n")
}

// OwnReferences are the noun's own props that name things of another noun
// (a reference prop, or a list of them): the relations its things hold
// outward, one Referrer each, marked Outward with no Relation, its Noun the
// named noun and Prop the noun's own prop. A related stack with an Outward
// setting draws a section of the things each one names. A computed or
// collection prop, an any-noun one (its things are of mixed nouns) and a
// relation prop (a declared kind's section lists those) are left out.
func (r *Runtime) OwnReferences(noun string) []Referrer {
	n := r.Nouns[r.CanonicalNoun(noun)]
	if n == nil {
		return nil
	}
	out := []Referrer{}
	for _, p := range n.Props {
		target, multi := p.Noun, false
		if p.Items != nil && p.Items.Noun != "" {
			target, multi = p.Items.Noun, true
		}
		if target == "" || target == AnyNoun || p.Relation != "" || p.computed() || r.Nouns[r.CanonicalNoun(target)] == nil {
			continue
		}
		target = r.CanonicalNoun(target)
		label := titleWord(p.Name) + " → " + r.choicesPlural(Parameter{Noun: target})
		out = append(out, Referrer{Noun: target, Prop: p.Name, ReadOnly: true, Outward: true, Multi: multi, Label: label, Section: n.Name + "." + p.Name + ">"})
	}
	return out
}

// own reports whether the referrer is one of the thing's own reference
// props (OwnReferences): outward with no relation kind behind it.
func (f Referrer) own() bool { return f.Outward && f.Relation == "" && !f.List && !f.History }

// refKeys are the keys a reference prop's value names: one key, or a
// list of them; empty values left out.
func refKeys(v any) []string {
	var out []string
	switch x := v.(type) {
	case nil:
	case string:
		if s := strings.TrimSpace(x); s != "" {
			out = append(out, s)
		}
	case []string:
		for _, s := range x {
			out = append(out, refKeys(s)...)
		}
	case []any:
		for _, s := range x {
			out = append(out, refKeys(s)...)
		}
	default:
		out = append(out, fmt.Sprint(x))
	}
	return out
}

// readOwnReferences lists the things the thing's own reference prop
// names: the thing read from its store, its prop's keys, each read from
// the named noun's store (a stub with only the key when it cannot read).
func (r *Runtime) readOwnReferences(c Ctx, noun, key string, ref Referrer, q ListQuery) (ListPage, error) {
	n := r.Noun(noun)
	if n == nil || n.Store == nil || n.Store.Get == nil || n.Store.Remote && c.tape == nil {
		return ListPage{}, nil
	}
	c.Noun = n.Name
	t, err := n.Store.Get(c, key)
	if err != nil {
		return ListPage{}, err
	}
	keys := refKeys(t[ref.Prop])
	if len(keys) == 0 {
		return ListPage{}, nil
	}
	return r.thingsOfKeys(c, r.Noun(ref.Noun), keys, q)
}

// nounPageItems are a noun page's rows when they are not its store's From
// list, from rows (NounAnswers.Rows, or the declaration when the store's
// first read failed) with why: the store's one thing, the live instances,
// or the declaration row. list is the page list's id; a store read that
// fails gives the declaration row with the failure.
func (r *Runtime) nounPageItems(c Ctx, noun, list, rows, why string) []ListItem {
	n := r.Nouns[r.CanonicalNoun(noun)]
	if n == nil {
		return nil
	}
	switch rows {
	case NounRowsUnkeyed:
		things, err := r.storeThings(c, n)
		if err == nil && len(things) > 0 {
			items := make([]ListItem, 0, len(things))
			for i, t := range things {
				label := r.DisplayNoun(n.Name)
				if len(things) > 1 {
					label = fmt.Sprintf("%s %d", label, i+1)
				}
				if lp := n.roleProp(RoleLabel); lp != nil && t[lp.Name] != nil {
					label = cellValueText(t[lp.Name])
				}
				items = append(items, ListItem{ID: fmt.Sprintf("%s/@thing/%d", list, i), Label: label, Noun: n.Name, Data: t})
			}
			return items
		}
		why := "its store has no thing now"
		if err != nil {
			why = "its store did not answer: " + err.Error()
		}
		return []ListItem{r.declarationRow(n, list, why)}
	case NounRowsInstances:
		var items []ListItem
		for _, inst := range r.instancesInOrder() {
			if inst.Noun != n.Name {
				continue
			}
			t := Thing{"id": inst.ID, "noun": inst.Noun, "parent": inst.Parent}
			if inst.Label != "" {
				t["label"] = inst.Label
			}
			if n.Store != nil {
				maps.Copy(t, r.liveThing(n, inst))
			}
			label := inst.ID
			if inst.Label != "" {
				label = inst.Label + "  " + inst.ID
			}
			items = append(items, ListItem{ID: list + "/@live/" + inst.ID, Label: label, Noun: instanceRowNoun, Data: t})
		}
		return items
	}
	return []ListItem{r.declarationRow(n, list, why)}
}

// storeThings reads a store without keys whole: its List, else its Get of
// the empty key.
func (r *Runtime) storeThings(c Ctx, n *Noun) ([]Thing, error) {
	c.Noun = n.Name
	if n.Store.List != nil || n.Store.Query != nil {
		page, err := r.queryThings(c, n, ListQuery{Archived: true})
		return page.Items, err
	}
	t, err := n.Store.Get(c, "")
	if err != nil || t == nil {
		return nil, err
	}
	return []Thing{t}, nil
}

// declarationRow is a noun page's one row for a noun with nothing to
// list: the noun as an ochazuke.noun thing, its label saying why.
func (r *Runtime) declarationRow(n *Noun, list, why string) ListItem {
	fact := r.nounFactOf(n, r.NounKind(n.Name))
	t, _ := ThingOf(fact)
	if t == nil {
		t = Thing{"noun": n.Name}
	}
	t["why"] = why
	return ListItem{ID: list + "/@noun", Label: r.DisplayNoun(n.Name) + " · " + why, Noun: "ochazuke.noun", Data: t}
}

// rowsSignature is what a fed noun page's rows are made of, so the rows
// are set again only when it changes.
func rowsSignature(items []ListItem) string {
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "%s|%s|%v\n", it.ID, it.Label, it.Data)
	}
	return b.String()
}
