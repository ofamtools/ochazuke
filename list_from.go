package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// From makes a list the view of a noun's store (AddList(id, nil, From(noun))):
// the list fills itself with the noun's things, one row each, and fills
// again after any change of the noun, so an application stops writing
// ListItem rows and refreshing them in its store handlers. A row's ID is
// "LIST/KEY" (two lists of one noun never share an instance), its Label the
// noun's label prop, its Data the thing, and, when the list is a table, its
// Cells the props the columns name (Noun.Columns unless Columns is given; a
// column's prop is its title lower-cased, spaces as dashes, or an alias of
// it). The filter, sort, groups and pages of the list's view
// (ListQuery) stay the list's: it is handed every thing, archived ones
// included, and hides them as list.hide-archived says.
//
// A change is any request of the change kind, a job of one, Runtime.Changed
// and, for a store with a Watch, its events: the same watch that the watch
// verb runs, one per noun for all its lists.
func From(noun string) InstanceOption { return func(s *instanceSpec) { s.from = noun } }

// fromHub is the lists fed by one noun and the watch that tells them to
// fill again.
type fromHub struct {
	noun  string
	watch *Watch
	lists []string
	dirty bool
}

// listSource is the noun a From list reads: one with a store that lists and
// a key prop to name its rows by.
func (r *Runtime) listSource(noun string) (*Noun, error) {
	n := r.Nouns[r.CanonicalNoun(noun)]
	switch {
	case n == nil:
		return nil, fmt.Errorf("list from %q: no such noun", noun)
	case n.Store == nil || n.Store.List == nil && n.Store.Query == nil:
		return nil, fmt.Errorf("list from %s: its store has no List or Query", r.DisplayNoun(n.Name))
	case n.keyProp() == nil:
		return nil, fmt.Errorf("list from %s: it has no key prop to name its rows by", r.DisplayNoun(n.Name))
	}
	return n, nil
}

// From is the noun the list shows the things of, "" for a list fed rows
// (SetItems).
func (l *List) From() string { return l.from }

// Refill reads the noun's things again and shows them, keeping the selection
// and the marks by row id. An application calls it only when a change of
// the store cannot say so with Runtime.Changed.
func (l *List) Refill() error {
	if l.from == "" {
		return errors.New("the list is fed rows, not a noun's store")
	}
	n := l.r.Nouns[l.from]
	c := Ctx{actor: l.r.Actor(), Runtime: l.r, Context: l.r.Context()}
	page, err := l.r.queryThings(c, n, ListQuery{Archived: true})
	if err != nil {
		return err
	}
	l.refills++
	return l.SetItems(l.r.thingItems(n, l.id, page.Items, l.columns))
}

// thingItems are the rows of things, listed under list.
func (r *Runtime) thingItems(n *Noun, list string, things []Thing, columns []Column) []ListItem {
	key := n.keyProp()
	props := make([]*PropSpec, len(columns))
	for i, col := range columns {
		props[i] = n.prop(columnField(col.Title))
	}
	items := make([]ListItem, 0, len(things))
	seen := map[string]bool{}
	for _, t := range things {
		id := fmt.Sprint(t[key.Name])
		if emptyProp(t[key.Name]) || seen[id] {
			continue
		}
		seen[id] = true
		item := ListItem{ID: list + "/" + id, Label: n.displayLabel(func(name string) any { return t[name] }), Noun: n.Name, Data: Thing(maps.Clone(t))}
		if item.Label == "" {
			item.Label = id
		}
		if len(columns) > 0 {
			item.Cells = make([]string, len(columns))
			for i, p := range props {
				if p != nil {
					item.Cells[i] = cellValueText(t[p.Name])
				}
			}
		}
		items = append(items, item)
	}
	return items
}

// cellValueText is a prop value as a table cell: text as it is, a list
// joined by commas, nothing for nothing, else what the value reads as.
func cellValueText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []string:
		return strings.Join(t, ", ")
	case []any:
		words := make([]string, len(t))
		for i, w := range t {
			words[i] = cellValueText(w)
		}
		return strings.Join(words, ", ")
	}
	return fmt.Sprint(looseText(v))
}

// feed fills the list and joins it to the watch of its noun.
func (l *List) feed() error {
	if err := l.Refill(); err != nil {
		return err
	}
	r := l.r
	hubs := r.watchHub()
	if hubs.from == nil {
		hubs.from = map[string]*fromHub{}
	}
	h := hubs.from[l.from]
	if h == nil || h.watch == nil || h.watch.Stopped() {
		old := h
		h = &fromHub{noun: l.from}
		if old != nil {
			h.lists = old.lists
		}
		w, err := r.Watch(l.from, nil, func(WatchEvent) { h.dirty = true })
		if err != nil {
			return err
		}
		w.batch = h.flush
		h.watch = w
		h.dirty = false
		hubs.from[l.from] = h
	}
	h.lists = append(h.lists, l.id)
	return nil
}

// flush fills every list of the noun again when a change was heard, and
// ends the watch once no list is left.
func (h *fromHub) flush() {
	if !h.dirty {
		return
	}
	h.dirty = false
	r := h.watch.r
	live := h.lists[:0]
	for _, id := range h.lists {
		inst := r.Instances[id]
		if inst == nil {
			continue
		}
		l, ok := inst.Component.(*List)
		if !ok {
			continue
		}
		live = append(live, id)
		if err := l.Refill(); err != nil {
			r.UI().Error(fmt.Sprintf("%s list: %v", r.DisplayNoun(h.noun), err))
		}
	}
	h.lists = slices.Clone(live)
	if len(h.lists) == 0 {
		h.watch.Stop()
		delete(r.watchHub().from, h.noun)
	}
}
