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
// it; a cell is the prop's Cell text when it declares one, else the value
// as text). Cell text is derived on each fill, never stored, and reaches
// only the table: get, list and the CLI keep the store's values. The filter, sort, groups and pages of the list's view
// (ListQuery) stay the list's: it is handed every thing, archived ones
// included, and hides them as collection.hide-archived says.
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
func (l *Collection) From() string { return l.from }

// Refill reads the noun's things again and shows them, keeping the selection
// and the marks by row id. An application calls it only when a change of
// the store cannot say so with Runtime.Changed.
func (l *Collection) Refill() error {
	if l.from == "" {
		return errors.New("the list is fed rows, not a noun's store")
	}
	n := l.r.Nouns[l.from]
	c := Ctx{actor: l.r.Actor(), Runtime: l.r, Context: l.r.Context()}
	if n.Store.Remote {
		return l.refillRemote(n, c)
	}
	page, err := l.r.queryThings(c, n, ListQuery{Archived: true})
	if err != nil {
		return err
	}
	l.refills++
	return l.SetItems(l.r.thingItems(c, n, l.id, page.Items, l.columns))
}

// thingItems are the rows of things, listed under list. A cell is its
// prop's Cell text when the prop declares one, else the value's reading
// (cellValueText).
func (r *Runtime) thingItems(c Ctx, n *Noun, list string, things []Thing, columns []Column) []ListItem {
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
				switch {
				case p == nil:
				case p.Cell != nil:
					item.Cells[i] = p.Cell(c, t)
				default:
					item.Cells[i] = cellValueText(t[p.Name])
				}
			}
		}
		items = append(items, r.copyRowMark(n, t, item))
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
func (l *Collection) feed() error {
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
		l, ok := inst.Component.(*Collection)
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

// refillRemote fills the list from a remote store without waiting for it:
// the read is a job (remote.go) that shows the things when it is answered.
func (l *Collection) refillRemote(n *Noun, c Ctx) error {
	r := l.r
	var page ListPage
	body := func(c Ctx) (any, error) {
		p, err := r.queryThings(c, n, ListQuery{Archived: true})
		page = p
		return nil, err
	}
	finish := func(_ any, err error) (any, error) {
		if err != nil {
			return nil, err
		}
		l.refills++
		return nil, l.SetItems(r.thingItems(c, n, l.id, page.Items, l.columns))
	}
	res := r.runSteppedBody(c, n.Name+".list", body, finish)
	if res.err != nil {
		return res.err
	}
	if res.work == nil {
		return nil // answered at once
	}
	_, err := r.Launch("app", n.Name+".list", res.Then(func(c Ctx, _ any, err error) {
		if err != nil {
			c.Error(fmt.Sprintf("%s list: %v", r.DisplayNoun(n.Name), err))
		}
	}), c)
	return err
}
