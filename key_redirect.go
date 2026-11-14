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
// ochazuke.key.redirect action, with "to" naming the relation:
//
//	{"context": "subagent/normal", "bindings": [
//	  {"key": "unbound", "action": "ochazuke.key.redirect", "args": {"to": "parent"}}
//	]}
//
// The row lives on the focus's noun (or instance) table, so a user's
// bindings.json overrides or unbinds it by the same identity (noun, mode,
// pattern, when) as a key row, binding list lists it, binding check checks
// it, and the help shows it. The focus's own rows still answer first: the
// walk is the focus's own level, then the related thing's whole walk (its
// level, its list, its page, app), in place of the focus's outer levels.
// A row of the related thing's own noun acts on that thing (rowInstance);
// a row of an outer level (a list's j) keeps acting from the focus, so
// motions stay visual. One hop: the related thing's own redirect is not
// followed.
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

// rowRelation is the first live instance pick finds from the focus through
// the RowRelations component on its path, "" for none.
func rowRelation(r *Runtime, focus string, pick func(RowRelations) []string) string {
	for _, id := range r.Ancestry(focus) {
		rr, ok := r.Instances[id].Component.(RowRelations)
		if !ok {
			continue
		}
		for _, to := range pick(rr) {
			if to != "" && r.Instances[to] != nil {
				return to
			}
		}
	}
	return ""
}

// keyRedirectRelations are the relations a redirect's "to" names: each
// finds the related instance from the focus, "" for none.
var keyRedirectRelations = map[string]func(r *Runtime, focus string) string{
	// parent: the row the focus is drawn under (a tree's child row and
	// its parent row).
	"parent": func(r *Runtime, focus string) string {
		return rowRelation(r, focus, func(rr RowRelations) []string { return []string{rr.RowParent(focus)} })
	},
	// child: the first row drawn under the focus that is on screen (a row
	// folded away is not one).
	"child": func(r *Runtime, focus string) string {
		return rowRelation(r, focus, func(rr RowRelations) []string { return rr.RowChildren(focus) })
	},
}

// KeyRedirectRelations are the relation names a redirect row's "to" takes.
func KeyRedirectRelations() []string { return slices.Sorted(maps.Keys(keyRedirectRelations)) }

func registerKeyRedirect(r *Runtime) {
	r.Register(Operation{Key: KeyRedirectAction, KeyRedirect: true,
		Description: "Bound to the unbound pattern: the keys the focus does not bind go to the thing related to it by to (parent: the row it is drawn under; child: the first row drawn under it); run alone, it names that thing (nothing for a relation it does not know)",
		Parameters: map[string]Parameter{"to": {Type: "string", Required: true,
			Description: "The relation that names the thing: " + strings.Join(KeyRedirectRelations(), ", ")}},
		Result: "string",
		Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			return r.relatedThing(StringArg(a, "to", ""), focusOf(c)), nil
		}})
}

// relatedThing is the instance related to focus by the relation to, ""
// for none or a relation nobody defined.
func (r *Runtime) relatedThing(to, focus string) string {
	if find := keyRedirectRelations[to]; find != nil {
		return find(r, focus)
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

// KeyRedirect is the instance the focus's unbound keys go to in mode, ""
// for none.
func (r *Runtime) KeyRedirect(focus, mode string) string {
	inst := r.Instances[focus]
	if inst == nil {
		return ""
	}
	row := r.redirectRow(inst, mode)
	if row == nil {
		return ""
	}
	if to := r.relatedThing(StringArg(row.Request.Args, "to", ""), focus); to != focus {
		return to
	}
	return ""
}

// redirected is the stack inner with the focus's redirect applied: the
// levels up to and including the focus's own, then the related thing's
// stack. inner is unchanged when the focus has no redirect in mode.
func (r *Runtime) redirected(inner []level, focus string, mode string, modals []string) []level {
	to := r.KeyRedirect(focus, mode)
	if to == "" {
		return inner
	}
	at := slices.IndexFunc(inner, func(lv level) bool { return lv.inst != nil && lv.inst.ID == focus })
	if at < 0 {
		return inner
	}
	return append(slices.Clone(inner[:at+1]), r.stackIn(to, modals)...)
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
