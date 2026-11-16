package ochazuke

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Derived props as data (overlay-every-store plan, lane STF): a prop the
// store does not hold, computed whenever a thing is read, declared as a
// PropSpec.Derivation instead of a Go function. Unlike PropSpec.Read (a
// computed prop, read only when asked for), a derived prop is part of the
// thing: get's record, list's rows and columns, filters and sorts.
//
// The framework runs the derivation over every thing a store answers
// (withDerivations wraps the store's Get, List, Query, Create and Patch
// once, in NounBuilder.Store), so the store's own code never sees the prop:
// a write leaves it out (set answers the typed read-only outcome, as for
// any read-only prop), and a list request that filters, sorts or groups on
// it reads the store's matching things unpaged and runs those parts here.

// Derivation is how a derived prop is computed: Of names one of the
// framework's derivations (Derivations), Reads what it reads. Each entry of
// Reads is a prop of the noun (PROP) or, for a derivation over another
// noun's things, NOUN.PROP (the noun is everything before the last dot).
// The nouns it reads (the NOUN of NOUN.PROP, the noun a reference prop
// names) are the noun's dependencies: a change there marks the noun's
// lists changed (Runtime.Changed, the store's ChangeToken).
type Derivation struct {
	Of    string   `json:"of"`
	Reads []string `json:"reads"`
}

// String is the derivation as one phrase: "count of note.task".
func (d Derivation) String() string {
	return strings.TrimSpace(d.Of + " of " + strings.Join(d.Reads, ", "))
}

// derivationKind is one derivation the framework runs: its name (Of), the
// type a prop declaring it gets when it declares none, whether its Reads
// name another noun's props (NOUN.PROP), and what it computes for each
// thing of a batch (things are written in place; a value it cannot compute
// is left out).
type derivationKind struct {
	Name  string
	About string
	Type  string
	Other bool
	run   func(c Ctx, n *Noun, p PropSpec, things []Thing)
}

// derivationKinds are the derivations a prop may name. They are data: a
// new derivation is one more row.
var derivationKinds = []derivationKind{
	{Name: "count", About: "How many things of NOUN have PROP naming this thing (Reads: NOUN.PROP)", Type: SettingInteger, Other: true, run: deriveCount},
	{Name: "label", About: "The label of the thing a reference prop names (Reads: PROP); its key when it has none or is gone", run: deriveLabel},
	{Name: "since", About: "How long ago a time prop was (Reads: PROP), a duration", Type: SettingDuration, run: deriveSince},
	{Name: "first-line", About: "The first line of a text prop that is not blank (Reads: PROP)", run: deriveFirstLine},
}

// Derivations are the derivations a PropSpec.Derivation may name (Of), with
// what each computes.
func Derivations() map[string]string {
	out := make(map[string]string, len(derivationKinds))
	for _, k := range derivationKinds {
		out[k.Name] = k.About
	}
	return out
}

func derivationKindOf(of string) *derivationKind {
	for i := range derivationKinds {
		if derivationKinds[i].Name == of {
			return &derivationKinds[i]
		}
	}
	return nil
}

// derivedProps are the noun's props with a Derivation.
func (n *Noun) derivedProps() []PropSpec {
	if n == nil {
		return nil
	}
	var out []PropSpec
	for _, p := range n.Props {
		if p.Derivation != nil {
			out = append(out, p)
		}
	}
	return out
}

// derivedProp reports whether the noun's prop of that name (or role) is a
// derived one.
func (n *Noun) derivedProp(name string) bool {
	p := n.prop(name)
	return p != nil && p.Derivation != nil
}

// splitRead is a Reads entry NOUN.PROP as its noun and prop; a plain PROP
// answers noun "".
func splitRead(read string) (noun, prop string) {
	if i := strings.LastIndex(read, "."); i >= 0 {
		return read[:i], read[i+1:]
	}
	return "", read
}

// dependencies are the nouns the noun's derived props read.
func (r *Runtime) dependencies(n *Noun) []string {
	var out []string
	for _, p := range n.derivedProps() {
		k := derivationKindOf(p.Derivation.Of)
		for _, read := range p.Derivation.Reads {
			noun, prop := splitRead(read)
			if k == nil || !k.Other {
				if ref := n.prop(prop); ref != nil {
					noun = ref.Noun
				}
			}
			if noun = r.CanonicalNoun(noun); noun != "" && noun != AnyNoun && noun != n.Name && !slices.Contains(out, noun) {
				out = append(out, noun)
			}
		}
	}
	return out
}

// withDependents is names with every noun whose derived props read one of
// them, transitively.
func (r *Runtime) withDependents(names []string) []string {
	out := slices.Clone(names)
	for grew := true; grew; {
		grew = false
		for _, name := range sortedNounNames(r) {
			if slices.Contains(out, name) {
				continue
			}
			if slices.ContainsFunc(r.dependencies(r.Nouns[name]), func(d string) bool { return slices.Contains(out, d) }) {
				out, grew = append(out, name), true
			}
		}
	}
	return out
}

func sortedNounNames(r *Runtime) []string {
	names := make([]string, 0, len(r.Nouns))
	for name, n := range r.Nouns {
		if len(n.derivedProps()) > 0 {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// derive fills the derived props of each thing (copies; the store's own
// maps are never written).
func (r *Runtime) derive(c Ctx, n *Noun, things []Thing) []Thing {
	props := n.derivedProps()
	if len(props) == 0 || len(things) == 0 {
		return things
	}
	out := make([]Thing, len(things))
	for i, t := range things {
		out[i] = cloneThing(t)
	}
	c.Runtime = r
	for _, p := range props {
		for _, t := range out {
			delete(t, p.Name) // never the store's value: the derivation's
		}
		if k := derivationKindOf(p.Derivation.Of); k != nil && len(p.Derivation.Reads) > 0 {
			k.run(c, n, p, out)
		}
	}
	return out
}

func cloneThing(t Thing) Thing {
	out := make(Thing, len(t)+2)
	for k, v := range t {
		out[k] = v
	}
	return out
}

// deriveCount counts, for each thing, the things of NOUN whose PROP holds
// its key (a list prop counts once per thing it holds it): one List of
// NOUN for the whole batch, every thing it lists, archived ones included
// (as CountProp counts).
func deriveCount(c Ctx, n *Noun, p PropSpec, things []Thing) {
	noun, prop := splitRead(p.Derivation.Reads[0])
	other := c.Runtime.Noun(noun)
	key := n.keyProp()
	if other == nil || other.Store == nil || other.Store.List == nil || key == nil {
		return
	}
	oc := c
	oc.Noun, oc.underived = other.Name, true
	rows, err := other.Store.List(oc, ListQuery{Archived: true, Every: true, Args: Arguments{}})
	if err != nil {
		return
	}
	field := prop
	if spec := other.prop(prop); spec != nil {
		field = spec.Name
	}
	counts := map[string]int{}
	for _, row := range rows {
		for _, k := range keysIn(row[field]) {
			counts[k]++
		}
	}
	for _, t := range things {
		t[p.Name] = counts[fmt.Sprint(t[key.Name])]
	}
}

// keysIn are the keys a reference value holds: one, or each of a list's.
func keysIn(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			out = append(out, fmt.Sprint(e))
		}
		return out
	}
	if s := fmt.Sprint(v); s != "" {
		return []string{s}
	}
	return nil
}

// deriveLabel is the label of the thing a reference prop names, read once
// per key through its noun's Get; the key itself when the thing has no
// label or cannot be read.
func deriveLabel(c Ctx, n *Noun, p PropSpec, things []Thing) {
	ref := n.prop(p.Derivation.Reads[0])
	if ref == nil {
		return
	}
	other := c.Runtime.Noun(ref.Noun)
	labels := map[string]string{}
	for _, t := range things {
		key := fmt.Sprint(t[ref.Name])
		if emptyProp(t[ref.Name]) {
			continue
		}
		if _, ok := labels[key]; !ok {
			labels[key] = refLabel(c, other, key)
		}
		t[p.Name] = labels[key]
	}
}

func refLabel(c Ctx, other *Noun, key string) string {
	if other == nil || other.Store == nil || other.Store.Get == nil {
		return key
	}
	oc := c
	oc.Noun, oc.underived = other.Name, true
	t, err := other.Store.Get(oc, key)
	if err != nil || t == nil {
		return key
	}
	if label := other.displayLabel(func(name string) any { return t[name] }); label != "" {
		return label
	}
	return key
}

// deriveSince is how long ago a time prop was, to the second: a time
// (ParseTime) or a whole number of Unix milliseconds; left out when the
// prop holds neither.
func deriveSince(c Ctx, n *Noun, p PropSpec, things []Thing) {
	name := p.Derivation.Reads[0]
	if spec := n.prop(name); spec != nil {
		name = spec.Name
	}
	now := time.Now()
	for _, t := range things {
		var at time.Time
		switch v := t[name].(type) {
		case time.Time:
			at = v
		case nil:
			continue
		default:
			parsed, ok := timeOrUnixMilli(fmt.Sprint(v))
			if !ok {
				continue
			}
			at = parsed
		}
		t[p.Name] = now.Sub(at).Truncate(time.Second).String()
	}
}

// deriveFirstLine is the first line of a text prop that is not blank,
// trimmed; left out when there is none.
func deriveFirstLine(c Ctx, n *Noun, p PropSpec, things []Thing) {
	name := p.Derivation.Reads[0]
	if spec := n.prop(name); spec != nil {
		name = spec.Name
	}
	for _, t := range things {
		for line := range strings.Lines(cellValueText(t[name])) {
			if line = strings.TrimSpace(line); line != "" {
				t[p.Name] = line
				break
			}
		}
	}
}

// withDerivations wraps the store of noun so every thing it answers holds
// its derived props, every write leaves them out, and a list request on
// them runs here (see the file comment). A noun with no derived props, read
// when each call runs, calls the store as it is.
func withDerivations(noun string, s Store) Store {
	w := derivationWrap{noun: noun, raw: s}
	if s.Get != nil {
		s.Get = w.get
	}
	if s.List != nil {
		s.List = w.list
	}
	if s.Query != nil {
		s.Query = w.query
	}
	if s.Create != nil {
		s.Create = w.create
	}
	if s.Patch != nil {
		s.Patch = w.patch
	}
	if s.ChangeToken != nil {
		s.ChangeToken = w.token
	}
	return s
}

type derivationWrap struct {
	noun string
	raw  Store
}

// of is the noun when it has derived props, else nil (call through).
func (w derivationWrap) of(c Ctx) *Noun {
	if c.Runtime == nil || c.underived {
		return nil
	}
	n := c.Runtime.Nouns[w.noun]
	if len(n.derivedProps()) == 0 {
		return nil
	}
	return n
}

func (w derivationWrap) one(c Ctx, n *Noun, t Thing, err error) (Thing, error) {
	if err != nil || t == nil || n == nil {
		return t, err
	}
	return c.Runtime.derive(c, n, []Thing{t})[0], nil
}

func (w derivationWrap) get(c Ctx, key string) (Thing, error) {
	t, err := w.raw.Get(c, key)
	return w.one(c, w.of(c), t, err)
}

// stored is values without the derived props.
func stored(n *Noun, values Thing) Thing {
	out := cloneThing(values)
	for _, p := range n.derivedProps() {
		delete(out, p.Name)
		for _, a := range p.Aliases {
			delete(out, a)
		}
	}
	return out
}

func (w derivationWrap) create(c Ctx, values Thing) (Thing, error) {
	n := w.of(c)
	if n != nil {
		values = stored(n, values)
	}
	t, err := w.raw.Create(c, values)
	return w.one(c, n, t, err)
}

func (w derivationWrap) patch(c Ctx, key string, changes Thing) (Thing, error) {
	n := w.of(c)
	if n != nil {
		changes = stored(n, changes)
	}
	t, err := w.raw.Patch(c, key, changes)
	return w.one(c, n, t, err)
}

// splitFilters separates the conditions on derived props from the rest.
func splitFilters(n *Noun, filters []PropFilter) (rest, derived []PropFilter) {
	for _, f := range filters {
		if n.derivedProp(f.Prop) {
			derived = append(derived, f)
		} else {
			rest = append(rest, f)
		}
	}
	return rest, derived
}

func (w derivationWrap) list(c Ctx, q ListQuery) ([]Thing, error) {
	n := w.of(c)
	if n == nil {
		return w.raw.List(c, q)
	}
	rest, derived := splitFilters(n, q.Filters)
	inner := q
	inner.Filters = rest
	things, err := w.raw.List(c, inner)
	if err != nil {
		return nil, err
	}
	things = c.Runtime.derive(c, n, things)
	if len(derived) == 0 {
		return things, nil
	}
	return c.Runtime.queryAll(n, things, ListQuery{Filters: derived, Archived: true}).Items, nil
}

// onDerived reports whether a list request filters, sorts or groups on a
// derived prop: the store cannot run that part.
func onDerived(n *Noun, q ListQuery) bool {
	keys := slices.Concat(q.Sort, q.GroupSort)
	return slices.ContainsFunc(q.Filters, func(f PropFilter) bool { return n.derivedProp(f.Prop) }) ||
		slices.ContainsFunc(keys, func(k SortKey) bool { return n.derivedProp(k.Field) }) ||
		slices.ContainsFunc(q.GroupBy, n.derivedProp)
}

// query answers a page. A request that filters, sorts or groups on a
// derived prop asks the store for every thing matching its other
// conditions (its text search and archive hiding too), unpaged and
// unsorted, derives them all and runs the whole request here; any other
// request is the store's own page, derived.
func (w derivationWrap) query(c Ctx, q ListQuery) (ListPage, error) {
	n := w.of(c)
	if n == nil || !onDerived(n, q) {
		page, err := w.raw.Query(c, q)
		if err == nil && n != nil {
			page.Items = c.Runtime.derive(c, n, page.Items)
		}
		return page, err
	}
	rest, derived := splitFilters(n, q.Filters)
	inner := q
	inner.Filters, inner.Sort, inner.GroupBy, inner.GroupSort, inner.Page, inner.PerPage = rest, nil, nil, nil, 0, 0
	page, err := w.raw.Query(c, inner)
	if err != nil {
		return ListPage{}, err
	}
	all := q
	all.Filters, all.Text, all.Archived = derived, "", true // the store ran the text and the hiding
	return c.Runtime.queryAll(n, c.Runtime.derive(c, n, page.Items), all), nil
}

// token is the store's change token joined with the tokens of the nouns
// its derived props read, so a change there moves it; "" (unknown) when
// one of them keeps none.
func (w derivationWrap) token(c Ctx) (string, error) {
	own, err := w.raw.ChangeToken(c)
	n := w.of(c)
	if err != nil || n == nil || own == "" {
		return own, err
	}
	parts := []string{own}
	oc := c
	oc.underived = true // their own tokens, never theirs joined with ours
	for _, dep := range c.Runtime.dependencies(n) {
		d := c.Runtime.Nouns[dep]
		if d == nil || d.Store == nil {
			continue
		}
		t := storeToken(d.Store, oc)
		if t == "" {
			return "", nil
		}
		parts = append(parts, dep+"="+t)
	}
	return strings.Join(parts, "|"), nil
}

// derivationOf is the name of the prop's derivation, "" for none.
func derivationOf(p PropSpec) string {
	if p.Derivation == nil {
		return ""
	}
	return p.Derivation.Of
}

// derivationProp is a derived prop as the noun keeps it: read-only, its
// own copy of the derivation, nothing stored (no Field). A prop with a
// Read is computed by it, so its Derivation is dropped.
func derivationProp(p PropSpec) PropSpec {
	if p.Read != nil || p.Subject != "" {
		p.Derivation = nil
		return p
	}
	d := *p.Derivation
	d.Reads = slices.Clone(d.Reads)
	p.Derivation, p.ReadOnly, p.Field = &d, true, ""
	return p
}

// derivationText is the prop's derivation as one phrase, "" for none.
func derivationText(p PropSpec) string {
	if p.Derivation == nil {
		return ""
	}
	return p.Derivation.String()
}

// timeOrUnixMilli reads a time's text, or a whole number as Unix
// milliseconds (the stamps an application's documents hold).
func timeOrUnixMilli(text string) (time.Time, bool) {
	if t, ok := ParseTime(text); ok {
		return t, true
	}
	ms, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err != nil || ms <= 0 {
		return time.Time{}, false
	}
	return time.UnixMilli(ms), true
}
