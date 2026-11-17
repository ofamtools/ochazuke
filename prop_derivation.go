package ochazuke

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
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
//
// One kind keeps a value of its own (derivationKind.Own): first, whose
// Reads may name the prop itself for the value the store holds for it
// (under its Field). Such a prop is stored and writable like any other
// prop; the derivation only decides what a read shows, and a write that
// the read would not show answers nothing with the reason (writeReasons).

// Derivation is how a derived prop is computed: Of names one of the
// framework's derivations (Derivations), Reads what it reads. Each entry of
// Reads is a prop of the noun (PROP) or, for a derivation over another
// noun's things, NOUN.PROP (the noun is everything before the last dot).
// For a derivation that follows a reference (first), REF.PROP is the prop
// PROP of the thing the noun's reference prop REF names. The nouns it
// reads (the NOUN of NOUN.PROP, the noun a reference prop names) are the
// noun's dependencies: a change there marks the noun's lists changed
// (Runtime.Changed, the store's ChangeToken).
//
// A first derivation's Reads entry may also be text with placeholders,
// task:{task}: each {READ} (PROP or REF.PROP) is replaced by its value,
// and the entry holds none when one of them is empty. REF.PROP where REF
// is an object prop (no reference) is the field PROP of REF's value.
//
// The rest shapes what any derivation gives, in this order: Blank trims
// text and counts text of only spaces as empty (for first, an entry that
// is only spaces is passed over); Max cuts text to that many cells, Tail
// (counted in Max) ending a cut; Else is the value when the derivation
// gives none. FirstOnly and Untrimmed are first-line's: the text's first
// line whether or not it is blank (a blank one gives none), and the line
// as written, spaces kept.
type Derivation struct {
	Of        string   `json:"of"`
	Reads     []string `json:"reads"`
	Blank     bool     `json:"blank,omitempty"`
	Max       int      `json:"max,omitempty"`
	Tail      string   `json:"tail,omitempty"`
	Else      any      `json:"else,omitempty"`
	FirstOnly bool     `json:"firstOnly,omitempty"`
	Untrimmed bool     `json:"untrimmed,omitempty"`
}

// String is the derivation as one phrase: "count of note.task".
func (d Derivation) String() string {
	return strings.TrimSpace(d.Of + " of " + strings.Join(d.Reads, ", "))
}

// derivationKind is one derivation the framework runs: its name (Of), the
// type a prop declaring it gets when it declares none, whether its Reads
// name another noun's props (Other: NOUN.PROP) or a prop of the thing a
// reference prop names (Via: REF.PROP), whether a Reads entry naming the
// prop itself is the prop's own stored value (Own: the prop is then stored
// and writable), what it computes for each thing of a batch (things are
// written in place; a value it cannot compute is left out) and, for an Own
// kind, why a write would not show (write: "" when it would).
type derivationKind struct {
	Name  string
	About string
	Type  string
	Other bool
	Via   bool
	Own   bool
	run   func(c Ctx, n *Noun, p PropSpec, things []Thing)
	write func(c Ctx, n *Noun, p PropSpec, after Thing) string
}

// derivationKinds are the derivations a prop may name. They are data: a
// new derivation is one more row.
var derivationKinds = []derivationKind{
	{Name: "count", About: "How many things of NOUN have PROP naming this thing (Reads: NOUN.PROP)", Type: SettingInteger, Other: true, run: deriveCount},
	{Name: "label", About: "The label of the thing a reference prop names (Reads: PROP); its key when it has none or is gone", run: deriveLabel},
	{Name: "since", About: "How long ago a time prop was (Reads: PROP), a duration", Type: SettingDuration, run: deriveSince},
	{Name: "first-line", About: "The first line of a text prop that is not blank, trimmed (Reads: PROP); FirstOnly takes the first line blank or not, Untrimmed keeps its spaces", run: deriveFirstLine},
	{Name: inheritKind, About: "The prop's own value, else (a prop that declares Inherit) the value its parent reference reaches, walked until one is set; an empty value is not set (Reads: PROP, REF.PROP, written for Inherit)", Via: true, Own: true, run: deriveInherit},
	{Name: "first", About: "The first of Reads that holds a value: REF.PROP is the prop PROP of the thing the reference prop REF names (or the field of an object prop), the prop's own name its own stored value, text with {READ} placeholders that text when each holds a value (Reads: task.project, project; about, task:{task}); stored and writable when Reads names it", Via: true, Own: true, run: deriveFirst, write: firstWriteReason},
}

// DerivationKind is a derivation an application adds to a runtime
// (Runtime.RegisterDerivation): About says what it computes, Type is the
// type a prop declaring it gets when it declares none, and Derive computes
// the value for one thing (its props by name; d is the prop's Derivation,
// Reads naming the props it reads), false when it gives none. A Reads
// entry naming a reference prop makes the referenced noun a dependency, as
// for the framework's kinds. A derived prop of a registered kind is like
// any other: part of get's record and list's rows, filterable, sortable,
// change-tracked, shaped by Blank, Max, Tail and Else; it never keeps a
// value of its own.
type DerivationKind struct {
	About  string
	Type   string
	Derive func(c Ctx, d Derivation, t Thing) (any, bool)
}

// RegisterDerivation adds a derivation kind under name for this runtime's
// props, before the nouns that declare it: a name the framework already
// has keeps the framework's.
func (r *Runtime) RegisterDerivation(name string, kind DerivationKind) {
	if r.derivations == nil {
		r.derivations = map[string]*derivationKind{}
	}
	derive := kind.Derive
	r.derivations[name] = &derivationKind{Name: name, About: kind.About, Type: kind.Type, run: func(c Ctx, _ *Noun, p PropSpec, things []Thing) {
		if derive == nil {
			return
		}
		for _, t := range things {
			if v, ok := derive(c, *p.Derivation, t); ok {
				t[p.Name] = v
			}
		}
	}}
}

// derivationKind is the kind a derivation names: the framework's, else one
// the application registered on r.
func (r *Runtime) derivationKind(of string) *derivationKind {
	if k := derivationKindOf(of); k != nil || r == nil {
		return k
	}
	return r.derivations[of]
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

// derivedOnly reports whether the noun's prop of that name is derived and
// holds no stored value of its own (derivedOnly on the prop).
func (n *Noun) derivedOnly(name string) bool {
	p := n.prop(name)
	return p != nil && p.derivedOnly()
}

// derivedOnly reports whether the prop is derived and holds no value of
// its own: never stored, read-only. A prop whose derivation is of an Own
// kind and whose Reads name the prop itself keeps its own stored value.
func (p PropSpec) derivedOnly() bool {
	return p.Derivation != nil && !p.ownsValue()
}

// ownsValue reports whether the prop's derivation reads the prop's own
// stored value (an Own kind whose Reads name the prop).
func (p PropSpec) ownsValue() bool {
	if p.Derivation == nil {
		return false
	}
	k := derivationKindOf(p.Derivation.Of)
	return k != nil && k.Own && slices.Contains(p.Derivation.Reads, p.Name)
}

// derivedOnlyProps are the noun's props that are derived and hold no
// value of their own.
func (n *Noun) derivedOnlyProps() []PropSpec {
	var out []PropSpec
	for _, p := range n.derivedProps() {
		if p.derivedOnly() {
			out = append(out, p)
		}
	}
	return out
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
		k := r.derivationKind(p.Derivation.Of)
		for _, read := range readsOf(p.Derivation.Reads) {
			noun, prop := splitRead(read)
			switch {
			case k != nil && k.Via:
				// REF.PROP reads the thing REF names; the prop's own
				// value or another own prop reads nothing outside.
				ref := noun
				noun = ""
				if spec := n.prop(ref); ref != "" && spec != nil {
					for _, other := range spec.refNouns()[min(1, len(spec.refNouns())):] {
						if other = r.CanonicalNoun(other); other != n.Name && !slices.Contains(out, other) {
							out = append(out, other)
						}
					}
					if nouns := spec.refNouns(); len(nouns) > 0 {
						noun = nouns[0]
					}
				}
			case k == nil || !k.Other:
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

// readsOf are the entries of Reads with each text entry's placeholders
// (task:{task}) in its place.
func readsOf(reads []string) []string {
	var out []string
	for _, read := range reads {
		if holes := placeholders(read); holes != nil {
			out = append(out, holes...)
		} else {
			out = append(out, read)
		}
	}
	return out
}

// placeholders are the {READ} names of a text entry, nil for an entry with
// none.
func placeholders(entry string) []string {
	var out []string
	for rest := entry; ; {
		i := strings.IndexByte(rest, '{')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(rest[i:], '}')
		if j < 0 {
			return out
		}
		out = append(out, rest[i+1:i+j])
		rest = rest[i+j+1:]
	}
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
	if c.refs == nil {
		c.refs = map[string]Thing{} // one read of each other thing for the batch's props
	}
	for _, p := range props {
		if !p.ownsValue() {
			for _, t := range out {
				delete(t, p.Name) // never the store's value: the derivation's
			}
		}
		if k := r.derivationKind(p.Derivation.Of); k != nil && len(p.Derivation.Reads) > 0 {
			k.run(c, n, p, out)
		}
		shapeDerived(p, out)
	}
	return out
}

// shapeDerived applies the derivation's Blank, Max, Tail and Else to the
// value each thing got (see Derivation); a derivation that sets none of
// them leaves the values as they are.
func shapeDerived(p PropSpec, things []Thing) {
	d := p.Derivation
	if !d.Blank && d.Max <= 0 && d.Else == nil {
		return
	}
	for _, t := range things {
		v := t[p.Name]
		if text, ok := v.(string); ok {
			if d.Blank {
				text = strings.TrimSpace(text)
			}
			if d.Max > 0 {
				text = ansi.Truncate(text, d.Max, d.Tail)
			}
			v = text
		}
		if emptyProp(v) && d.Else != nil {
			v = d.Else
		}
		if v == nil {
			delete(t, p.Name)
		} else {
			t[p.Name] = v
		}
	}
}

// blankOut is v as a derivation with Blank counts it: text of only spaces
// is empty.
func blankOut(d *Derivation, v any) any {
	if text, ok := v.(string); ok && d.Blank && strings.TrimSpace(text) == "" {
		return nil
	}
	return v
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
	got := batchRefs(c)
	readRefs(c, other, batchKeys(things, ref.Name), got)
	for _, t := range things {
		if emptyProp(t[ref.Name]) {
			continue
		}
		t[p.Name] = refLabel(other, fmt.Sprint(t[ref.Name]), got)
	}
}

// refLabel is the label of the thing of other with that key, read into got
// (readRefs); the key itself when it has none or was not read.
func refLabel(other *Noun, key string, got map[string]Thing) string {
	if other == nil {
		return key
	}
	t := got[other.Name+"\x00"+key]
	if t == nil {
		return key
	}
	if label := other.displayLabel(func(name string) any { return t[name] }); label != "" {
		return label
	}
	return key
}

// batchKeys are the distinct keys the reference prop holds over the batch.
func batchKeys(things []Thing, prop string) []string {
	var out []string
	for _, t := range things {
		if v := t[prop]; !emptyProp(v) {
			if key := fmt.Sprint(v); !slices.Contains(out, key) {
				out = append(out, key)
			}
		}
	}
	return out
}

// batchRefs is the map of other nouns' things the batch's derived props
// read (Ctx.refs, by noun, NUL, key), a new one outside a batch.
func batchRefs(c Ctx) map[string]Thing {
	if c.refs != nil {
		return c.refs
	}
	return map[string]Thing{}
}

// readRefs reads the things of other with these keys into got (by
// other.Name, NUL, key; a thing that cannot be read is nil there), for a
// derivation over a batch: one Query for every key not yet read when the
// store has one (every thing, archived ones too, without its own derived
// props), then the store's Get for each key the query did not answer, or
// for every key when it has no Query.
func readRefs(c Ctx, other *Noun, keys []string, got map[string]Thing) {
	if other == nil || other.Store == nil || len(keys) == 0 {
		return
	}
	id := func(key string) string { return other.Name + "\x00" + key }
	var want []any
	for _, key := range keys {
		if _, ok := got[id(key)]; !ok {
			want = append(want, key)
		}
	}
	if len(want) == 0 {
		return
	}
	oc := c
	oc.Noun, oc.underived = other.Name, true
	if kp := other.keyProp(); kp != nil && other.Store.Query != nil {
		page, err := other.Store.Query(oc, ListQuery{Filters: []PropFilter{{Prop: kp.Name, Op: "in", Value: want}}, Archived: true, Every: true, Args: Arguments{}})
		if err == nil {
			for _, t := range page.Items {
				got[id(fmt.Sprint(t[kp.Name]))] = t
			}
		}
	}
	for _, key := range want {
		k := fmt.Sprint(key)
		if _, ok := got[id(k)]; ok {
			continue
		}
		var t Thing
		if other.Store.Get != nil {
			t, _ = other.Store.Get(oc, k)
		}
		got[id(k)] = t
	}
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
	d := p.Derivation
	name := d.Reads[0]
	if spec := n.prop(name); spec != nil {
		name = spec.Name
	}
	for _, t := range things {
		for line := range strings.Lines(cellValueText(t[name])) {
			line = strings.TrimRight(line, "\r\n")
			if strings.TrimSpace(line) == "" {
				if d.FirstOnly {
					break
				}
				continue
			}
			if !d.Untrimmed {
				line = strings.TrimSpace(line)
			}
			t[p.Name] = line
			break
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

// stored is values without the derived props that hold no value of their
// own (a prop that keeps its own value is written like any other).
func stored(n *Noun, values Thing) Thing {
	out := cloneThing(values)
	for _, p := range n.derivedOnlyProps() {
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

// derivationProp is a derived prop as the noun keeps it: its own copy of
// the derivation; read-only and nothing stored (no Field) unless it keeps
// a value of its own (ownsValue: stored under its Field, writable as
// declared). A prop with a Read is computed by it, so its Derivation is
// dropped.
func derivationProp(p PropSpec) PropSpec {
	if p.Read != nil || p.Subject != "" {
		p.Derivation = nil
		return p
	}
	d := *p.Derivation
	d.Reads = slices.Clone(d.Reads)
	p.Derivation = &d
	if !p.ownsValue() {
		p.ReadOnly, p.Field = true, ""
	}
	return p
}

// firstEntry is the value one Reads entry of a first derivation gives the
// thing t, and where it came from: the prop's own value or another prop of
// t (from ""), or (REF.PROP) the prop PROP of the thing t's reference prop
// REF names, read through its noun's Get once per key (got caches it; a
// thing that is not found gives none).
func firstEntry(c Ctx, n *Noun, read string, t Thing, got map[string]Thing) (any, string) {
	if holes := placeholders(read); holes != nil {
		text := read
		for _, hole := range holes {
			v, _ := firstEntry(c, n, hole, t, got)
			if emptyProp(v) {
				return nil, ""
			}
			text = strings.Replace(text, "{"+hole+"}", cellValueText(v), 1)
		}
		return text, ""
	}
	refName, prop := splitRead(read)
	if refName == "" {
		name := prop
		if spec := n.prop(prop); spec != nil {
			name = spec.Name
		}
		return t[name], ""
	}
	ref := n.prop(refName)
	if ref == nil || emptyProp(t[ref.Name]) {
		return nil, ""
	}
	if ref.Noun == "" {
		// An object prop: the field of its value.
		switch obj := t[ref.Name].(type) {
		case map[string]any:
			return obj[prop], ""
		case Thing:
			return obj[prop], ""
		}
		return nil, ""
	}
	key := fmt.Sprint(t[ref.Name])
	other := c.Runtime.Noun(ref.Noun)
	if other == nil || other.Store == nil || other.Store.Get == nil {
		return nil, ""
	}
	id := other.Name + "\x00" + key
	thing, ok := got[id]
	if !ok {
		oc := c
		oc.Noun, oc.underived = other.Name, true
		thing, _ = other.Store.Get(oc, key)
		got[id] = thing
	}
	if thing == nil {
		return nil, ""
	}
	name := prop
	if spec := other.prop(prop); spec != nil {
		name = spec.Name
	}
	return thing[name], c.Runtime.DisplayNoun(other.Name) + " " + key + "'s " + name
}

// deriveFirst is, for each thing, the first of Reads that holds a value
// (firstEntry); a thing where none does keeps its own value as it is.
func deriveFirst(c Ctx, n *Noun, p PropSpec, things []Thing) {
	got := batchRefs(c)
	// The things every REF.PROP names, read for the whole batch at once.
	for _, read := range readsOf(p.Derivation.Reads) {
		if refName, _ := splitRead(read); refName != "" {
			if ref := n.prop(refName); ref != nil && ref.Noun != "" {
				readRefs(c, c.Runtime.Noun(ref.Noun), batchKeys(things, ref.Name), got)
			}
		}
	}
	for _, t := range things {
		as := cloneThing(t) // every entry reads the thing as the store answered it
		for _, read := range p.Derivation.Reads {
			if v, _ := firstEntry(c, n, read, as, got); !emptyProp(blankOut(p.Derivation, v)) {
				t[p.Name] = v
				break
			}
		}
	}
}

// firstWriteReason is why a write of a first prop would not show: an entry
// before the prop's own name in Reads gives the thing as written (after:
// the thing with the write's changes) another value, which a read shows
// instead. "" when the write shows: no entry before it holds a value, or
// the one that does holds the value written.
func firstWriteReason(c Ctx, n *Noun, p PropSpec, after Thing) string {
	got := map[string]Thing{}
	for _, read := range p.Derivation.Reads {
		if read == p.Name {
			return ""
		}
		v, from := firstEntry(c, n, read, after, got)
		switch {
		case emptyProp(v):
			continue
		case fmt.Sprint(v) == fmt.Sprint(after[p.Name]):
			return ""
		case from == "":
			from = read
		}
		return fmt.Sprintf("%s comes from %s (%v), which wins over its own value (%s)", p.Name, from, v, p.Derivation)
	}
	return ""
}

// writeReasons are the props of changes that keep a value of their own
// and whose write would not show on the thing after it (old with
// changes), each with the reason (derivationKind.write).
func (r *Runtime) writeReasons(c Ctx, n *Noun, old, changes Thing) map[string]string {
	var out map[string]string
	var after Thing
	c.Runtime = r
	for _, p := range n.derivedProps() {
		if _, written := changes[p.Name]; !written || !p.ownsValue() {
			continue
		}
		k := r.derivationKind(p.Derivation.Of)
		if k == nil || k.write == nil {
			continue
		}
		if after == nil {
			after = cloneThing(old)
			for name, v := range changes {
				after[name] = v
			}
		}
		if reason := k.write(c, n, p, after); reason != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[p.Name] = reason
		}
	}
	return out
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
