package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// A key redirect sends the keys a focused thing does not bind to another
// thing, named by its relation to the focus, never by its noun. It is a
// binding row like any other: the "unbound" pattern and the
// ochazuke.key.redirect action, with "to" naming the relation (and "of",
// optionally, the noun the related row must be: the first child that is
// an item):
//
//	{"context": "subagent/normal", "bindings": [
//	  {"key": "unbound", "action": "ochazuke.key.redirect", "args": {"to": "parent"}}
//	]}
//
// The row lives on the focus's noun (or instance) table, so a user's
// bindings.json overrides or unbinds it by the same identity (noun, mode,
// pattern, when) as a key row, binding list lists it, binding check checks
// it, and the help shows it. The focus's own rows still answer first:
// the walk is the focus's own level, then the related row's, then the
// levels they share (the list, the page, app), so a list's j or row d d
// and app's undo answer only what neither row binds. A row of the related
// thing's own noun acts on that thing (rowInstance). Redirects chain: the
// related thing's own redirect is followed (a subagent's parent pane, then
// that pane's first item), each hop's row after the one before, a hop
// back to a thing already in the chain ending it (KeyRedirects).
//
// The relations are keyRedirectRelations; a new one is a row of that table.

// Unbound is the pattern of a key-redirect row: it matches no key in the
// walk (classOf never yields it), it names the table's redirect.
const Unbound = "unbound"

// KeyRedirectAction is the action of a key-redirect row.
const KeyRedirectAction = "ochazuke.key.redirect"

// RowRelations is a component that draws rows under other rows (a tree):
// the row a row is drawn under ("" for a top row or a row it does not
// hold) and the rows drawn under it, in drawing order.
type RowRelations interface {
	RowParent(id string) string
	RowChildren(id string) []string
}

// RowParent is the node id is drawn under, "" for a root or a node the
// tree does not hold.
func (t *Tree) RowParent(id string) string { return t.parentOf(id) }

// RowChildren are the nodes drawn under id, in order.
func (t *Tree) RowChildren(id string) []string {
	if id == "" {
		return nil
	}
	return t.children(id)
}

// rowRelation is what pick finds from the focus through the first
// RowRelations component on its path that holds the focus.
func rowRelation(r *Runtime, focus string, pick func(RowRelations) []string) []string {
	for _, id := range r.Ancestry(focus) {
		if rr, ok := r.Instances[id].Component.(RowRelations); ok {
			if got := pick(rr); len(got) > 0 {
				return got
			}
		}
	}
	return nil
}

// keyRedirectRelations are the relations a redirect's "to" names: each
// lists the related rows from the focus, nearest first; the first one on
// screen (of the row's "of" noun, when it names one) is the thing.
var keyRedirectRelations = map[string]func(r *Runtime, focus string) []string{
	// parent: the row the focus is drawn under (a tree's child row and
	// its parent row).
	"parent": func(r *Runtime, focus string) []string {
		return rowRelation(r, focus, func(rr RowRelations) []string {
			if p := rr.RowParent(focus); p != "" {
				return []string{p}
			}
			return nil
		})
	},
	// child: the rows drawn under the focus, in order (a row folded away
	// is not on screen).
	"child": func(r *Runtime, focus string) []string {
		return rowRelation(r, focus, func(rr RowRelations) []string { return rr.RowChildren(focus) })
	},
}

// KeyRedirectRelations are the relation names a redirect row's "to" takes.
func KeyRedirectRelations() []string { return slices.Sorted(maps.Keys(keyRedirectRelations)) }

func registerKeyRedirect(r *Runtime) {
	r.Register(Operation{Key: KeyRedirectAction, KeyRedirect: true,
		Description: "Bound to the unbound pattern: the keys the focus does not bind go to the thing related to it by to (parent: the row it is drawn under; child: the first row drawn under it); run alone, it names that thing (nothing for a relation it does not know)",
		Parameters: map[string]Parameter{"to": {Type: "string", Required: true,
			Description: "The relation that names the thing: " + strings.Join(KeyRedirectRelations(), ", ")},
			"of": {Type: "string", Description: "Only a related row of this noun (the first child that is an item, not a subagent)"}},
		Result: "string",
		Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			return r.relatedThing(a, focusOf(c)), nil
		}})
}

// relatedThing is the instance related to focus by the redirect's
// arguments: the first row on screen its relation "to" lists, of the noun
// "of" when it names one; "" for none or a relation nobody defined.
func (r *Runtime) relatedThing(args Arguments, focus string) string {
	find := keyRedirectRelations[StringArg(args, "to", "")]
	if find == nil {
		return ""
	}
	of := StringArg(args, "of", "")
	for _, id := range find(r, focus) {
		inst := r.Instances[id]
		if inst != nil && (of == "" || inst.Noun == r.CanonicalNoun(of)) {
			return id
		}
	}
	return ""
}

// redirectRow is the focus's key-redirect row in mode, nil for none: the
// first row on the unbound pattern whose action is a key redirect, in the
// instance's table, then its noun's tables for the mode (and the modes it
// extends), then the noun's any-mode table.
func (r *Runtime) redirectRow(inst *Instance, mode string) *Binding {
	var tables [][]Binding
	tables = append(tables, rowsForMode(inst.Bindings, mode, false))
	if n := r.Nouns[inst.Noun]; n != nil {
		tables = append(append(tables, r.nounTables(n, mode)...), n.Bindings[""])
	}
	for _, rows := range tables {
		for i := range rows {
			if rows[i].Pattern == Unbound && r.Operations.At(Key(rows[i].Request.Key())).KeyRedirect {
				return &rows[i]
			}
		}
	}
	return nil
}

// keyRedirectDepth bounds a redirect chain: the related things the focus's
// keys can reach, one hop per redirect row followed.
const keyRedirectDepth = 4

// KeyRedirect is the instance the focus's unbound keys go to first in
// mode (the first hop of KeyRedirects), "" for none.
func (r *Runtime) KeyRedirect(focus, mode string) string {
	if chain := r.KeyRedirects(focus, mode); len(chain) > 0 {
		return chain[0]
	}
	return ""
}

// KeyRedirects is the chain of instances the focus's unbound keys go to
// in mode, nearest first: the focus's redirect names the first, that
// thing's own redirect the next, and so on up to keyRedirectDepth hops. A
// hop back to a thing already in the chain (the focus included: pane →
// child → parent) ends it.
func (r *Runtime) KeyRedirects(focus, mode string) []string {
	seen := map[string]bool{focus: true}
	var out []string
	for at := focus; len(out) < keyRedirectDepth; {
		inst := r.Instances[at]
		if inst == nil {
			break
		}
		row := r.redirectRow(inst, mode)
		if row == nil {
			break
		}
		to := r.relatedThing(row.Request.Args, at)
		if to == "" || seen[to] {
			break
		}
		seen[to] = true
		out = append(out, to)
		at = to
	}
	return out
}

// redirected is the stack inner with the focus's redirect chain applied:
// each related thing's own levels (its row, up to the first ancestor it
// shares with the focus) go in just before that shared ancestor's level,
// in chain order, so the focus's row answers first, then the first related
// row, then the next, then the list, page and app they share (a list's j,
// its row d d, app's undo). With no shared ancestor the related levels
// follow the focus's own. A level already on the walk is not added twice.
// inner is unchanged when the focus has no redirect in mode.
func (r *Runtime) redirected(inner []level, focus string, mode string, modals []string) []level {
	chain := r.KeyRedirects(focus, mode)
	if len(chain) == 0 {
		return inner
	}
	at := slices.IndexFunc(inner, func(lv level) bool { return lv.inst != nil && lv.inst.ID == focus })
	if at < 0 {
		return inner
	}
	shared := map[string]bool{}
	for _, id := range r.Ancestry(focus) {
		shared[id] = id != focus
	}
	onWalk := map[string]bool{}
	for _, lv := range inner {
		if lv.inst != nil {
			onWalk[lv.inst.ID] = true
		}
	}
	// inserts[j] are the related levels that go in before inner[j].
	inserts := map[int][]level{}
	floor := at + 1
	for _, to := range chain {
		related := r.stackIn(to, modals)
		k := slices.IndexFunc(related, func(lv level) bool { return lv.inst != nil && shared[lv.inst.ID] })
		j := at + 1
		if k >= 0 {
			id := related[k].inst.ID
			if i := slices.IndexFunc(inner, func(lv level) bool { return lv.inst != nil && lv.inst.ID == id }); i > at {
				j = i
			}
		} else {
			k = len(related)
		}
		// A later hop never answers before an earlier one.
		j = max(j, floor)
		floor = j
		for _, lv := range related[:k] {
			if lv.inst != nil {
				if onWalk[lv.inst.ID] {
					continue
				}
				onWalk[lv.inst.ID] = true
			}
			lv.related = true
			inserts[j] = append(inserts[j], lv)
		}
	}
	out := make([]level, 0, len(inner)+len(chain))
	for i := 0; i <= len(inner); i++ {
		out = append(out, inserts[i]...)
		if i < len(inner) {
			out = append(out, inner[i])
		}
	}
	return out
}

// redirectWarnings are the key-redirect rows of rows that load but do
// nothing, as binding check warns of them: a row on the unbound pattern
// whose action is not a key redirect (it never answers), a key redirect
// bound to a key (it only names the thing), and a relation nobody defined
// (the keys stay where they are).
func (r *Runtime) redirectWarnings(rows []Binding) []string {
	var out []string
	for _, b := range rows {
		if b.Unbind || b.Request.Empty() {
			continue
		}
		redirect := r.Operations.At(Key(b.Request.Key())).KeyRedirect
		to := StringArg(b.Request.Args, "to", "")
		var why string
		switch {
		case b.Pattern == Unbound && !redirect:
			why = fmt.Sprintf("%s on the %s pattern never answers; bind %s there", b.Request.Key(), Unbound, KeyRedirectAction)
		case redirect && b.Pattern != Unbound:
			why = fmt.Sprintf("%s on a key only names the related thing; the %s pattern redirects", b.Request.Key(), Unbound)
		case redirect && keyRedirectRelations[to] == nil:
			why = fmt.Sprintf("to %q is no relation (%s): the keys stay where they are", to, strings.Join(KeyRedirectRelations(), ", "))
		default:
			continue
		}
		out = append(out, fmt.Sprintf("%s %q: %s", tableLabel(b), b.Keys+b.Pattern, why))
	}
	return out
}

// RedirectWarnings are redirectWarnings over every stored table.
func (r *Runtime) RedirectWarnings() []string {
	var out []string
	for _, rows := range r.storedTables() {
		out = append(out, r.redirectWarnings(rows)...)
	}
	return out
}
