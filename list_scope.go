package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// A list's scope as data (docstore-for-nouns): a noun's list narrows to the
// things of the current scope (the pinned project) unless a parameter
// widens it (all), or to the value a list parameter gives. The store
// declares how (Store.ListScope rows), the application supplies the values
// it does not hold itself (Store.ScopeValue), and the framework adds each
// condition to the request before the store runs it: a store with a Query
// (DocStore over a sqlite table) runs it in the table, paged, and a
// condition on a derived prop runs where every such condition runs
// (withDerivations).

// ListScope is one condition a list adds. Prop is the prop the condition is
// on and Op its operator (PropFilter.Op: "" is equals). The condition's
// value is the list parameter Arg when the request gives it, else the
// value named Value that the store's ScopeValue answers. Unless names a
// boolean list parameter that, when true, leaves the condition out (all:
// every thing, not only those in scope). With no value (Arg not given,
// ScopeValue answering nil or ""), the condition is left out: nothing
// pinned is everything.
//
// A request that asks for every thing (ListQuery.Every: the Nouns page's
// counts, a thing picker, a walk, a derivation's read) has no scope unless
// the row says Every: pickers show all things, and narrowing is optional,
// never a pre-filter (the owner's rule), so false is the default. A store
// whose every read must stay in scope sets Every on that row.
//
// The values ScopeValue answers are part of the store's change token
// (withListScope), so a list refreshes when the pin moves.
type ListScope struct {
	Prop   string `json:"prop"`
	Op     string `json:"op,omitempty"`
	Arg    string `json:"arg,omitempty"`
	Value  string `json:"value,omitempty"`
	Unless string `json:"unless,omitempty"`
	Every  bool   `json:"every,omitempty"`
}

// scopeFilters are the conditions s's scope rows add to q.
func scopeFilters(c Ctx, s Store, q ListQuery) []PropFilter {
	var out []PropFilter
	for _, row := range s.ListScope {
		if q.Every && !row.Every {
			continue
		}
		if row.Unless != "" {
			if on, _ := q.Args[row.Unless].(bool); on {
				continue
			}
		}
		var v any
		if row.Arg != "" {
			v = q.Args[row.Arg]
		}
		if emptyProp(v) && row.Value != "" && s.ScopeValue != nil {
			v = s.ScopeValue(c, row.Value)
		}
		if emptyProp(v) {
			continue
		}
		out = append(out, PropFilter{Prop: row.Prop, Op: row.Op, Value: v})
	}
	return out
}

// withListScope adds the store's scope conditions to every List and Query
// request of noun; a store with no ListScope rows is as it is. A List
// answer is held to the conditions too (a List that reads no Filters). The
// store's change token is joined with the values its rows read from
// ScopeValue (scopeToken).
func withListScope(noun string, s Store) Store {
	if len(s.ListScope) == 0 {
		return s
	}
	raw := s
	scoped := func(c Ctx, q ListQuery) ListQuery {
		if extra := scopeFilters(c, raw, q); len(extra) > 0 {
			q.Filters = slices.Concat(clonePropFilters(q.Filters), extra)
		}
		return q
	}
	if raw.List != nil {
		s.List = func(c Ctx, q ListQuery) ([]Thing, error) {
			extra := scopeFilters(c, raw, q)
			things, err := raw.List(c, scoped(c, q))
			if err != nil || len(extra) == 0 || c.Runtime == nil || c.Runtime.Nouns[noun] == nil {
				return things, err
			}
			return c.Runtime.queryAll(c.Runtime.Nouns[noun], things, ListQuery{Filters: extra, Archived: true}).Items, nil
		}
	}
	if raw.Query != nil {
		s.Query = func(c Ctx, q ListQuery) (ListPage, error) { return raw.Query(c, scoped(c, q)) }
	}
	if raw.ChangeToken != nil && raw.ScopeValue != nil {
		s.ChangeToken = func(c Ctx) (string, error) {
			own, err := raw.ChangeToken(c)
			if err != nil || own == "" {
				return own, err
			}
			return own + scopeToken(c, raw), nil
		}
	}
	return s
}

// scopeToken is what a change token adds for the store's scope: each value
// a row reads from ScopeValue, by its name. A row's list parameter (Arg,
// Unless) is the request's own, so it needs no token: a request that gives
// another is another request.
func scopeToken(c Ctx, s Store) string {
	var b strings.Builder
	var seen []string
	for _, row := range s.ListScope {
		if row.Value == "" || slices.Contains(seen, row.Value) {
			continue
		}
		seen = append(seen, row.Value)
		fmt.Fprintf(&b, "|scope.%s=%v", row.Value, s.ScopeValue(c, row.Value))
	}
	return b.String()
}
