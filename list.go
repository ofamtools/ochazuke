package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ListItem is application-owned identity, display text, and the registered
// noun the row answers to. Each row is an Instance whose ID is the item ID and
// whose Parent is the list; its bindings and actions come from its noun.
// Data is an opaque value the application keeps on the row (a host's
// record of the row, an extension's own id); SetItems copies it to the
// row instance's Data, where a verb reads it as Ctx.Instance.Data. The
// framework never reads it.
type ListItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Noun  string `json:"noun"`
	Data  any    `json:"data,omitempty"`
}

// List owns flat selection, marks and scrolling on the runtime's event loop.
// Focus sits on the selected row when there is one, else on the list.
type List struct {
	r        *Runtime
	id       string
	items    []ListItem
	selected int
	offset   int
	marks    map[string]bool
}

// NewList is AddList for callers that hold the runtime as a value.
func NewList(r *Runtime, id string, items []ListItem, opts ...InstanceOption) (*List, error) {
	return r.AddList(id, items, opts...)
}

// AddList registers a list instance of the list noun with its rows. Options:
// Parent(id) (default app) and Kind(noun) for a list that answers to its
// own noun as well.
func (r *Runtime) AddList(id string, items []ListItem, opts ...InstanceOption) (*List, error) {
	if id == "" {
		return nil, errors.New("list ID must be nonempty")
	}
	if r.Instances[id] != nil {
		return nil, fmt.Errorf("list %q is already registered", id)
	}
	spec := instanceSpec{parent: "app", noun: "list"}
	for _, opt := range opts {
		opt(&spec)
	}
	if r.Instances[spec.parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", spec.parent)
	}
	list := &List{r: r, id: id, selected: -1, marks: map[string]bool{}}
	r.Instance(id, Parent(spec.parent), Kind(spec.noun), FocusOnClose(spec.closeFocus))
	r.Instances[id].Component = list
	if err := list.SetItems(items); err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	return list, nil
}

// SetItems copies rows, preserves selection by ID, and falls back to the nearest
// remaining index if that ID disappears. Invalid updates leave the list unchanged.
// It creates one instance per new row under the list and removes the instances
// of rows that disappear; a retained row keeps its instance and any bindings
// the application put on it.
func (l *List) SetItems(items []ListItem) error {
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		if item.ID == "" || ids[item.ID] {
			return fmt.Errorf("list item ID %q must be nonempty and unique", item.ID)
		}
		if l.r.Noun(item.Noun) == nil {
			return fmt.Errorf("list item %q: unknown noun %q", item.ID, item.Noun)
		}
		if owner := l.r.rows[item.ID]; owner != l.id && l.r.Instances[item.ID] != nil {
			return fmt.Errorf("list item ID %q is already another instance", item.ID)
		}
		ids[item.ID] = true
	}
	gone := map[string]bool{}
	for _, old := range l.items {
		if !ids[old.ID] {
			gone[old.ID] = true
			delete(l.marks, old.ID)
			_ = l.r.RemoveInstance(old.ID)
		}
	}
	for _, item := range items {
		l.r.rows[item.ID] = l.id
		inst := l.r.Instances[item.ID]
		if inst == nil {
			inst = l.r.Instance(item.ID, Parent(l.id), Kind(item.Noun))
		}
		inst.Noun, inst.Parent, inst.Label, inst.Data = item.Noun, l.id, item.Label, item.Data
	}
	previous, _ := l.Selected()
	selected := min(max(l.selected, 0), len(items)-1)
	for i, item := range items {
		if item.ID == previous.ID {
			selected = i
			break
		}
	}
	l.items, l.selected = slices.Clone(items), selected
	l.offset = min(l.offset, max(0, len(items)-1))
	l.syncFocus(previous, gone)
	return nil
}

// Items returns a copy of the rows in display order.
func (l *List) Items() []ListItem { return slices.Clone(l.items) }

// Len is the number of rows.
func (l *List) Len() int { return len(l.items) }

// Selected returns a value copy; empty lists have no selected item.
func (l *List) Selected() (ListItem, bool) {
	if l.selected < 0 {
		return ListItem{}, false
	}
	return l.items[l.selected], true
}

// Move clamps at either end and does not take focus from another component.
func (l *List) Move(delta int) {
	if l.selected < 0 {
		return
	}
	next := l.selected + min(max(delta, -l.selected), len(l.items)-1-l.selected)
	if next != l.selected {
		previous := l.items[l.selected]
		l.selected = next
		l.syncFocus(previous, nil)
	}
}

// Select changes the selected row by stable ID without taking focus.
func (l *List) Select(id string) error {
	for i, item := range l.items {
		if item.ID == id {
			l.Move(i - l.selected)
			return nil
		}
	}
	return fmt.Errorf("unknown list item %q", id)
}

// ToggleMark marks or unmarks a row for fan-out verbs.
func (l *List) ToggleMark(id string) error {
	if !slices.ContainsFunc(l.items, func(item ListItem) bool { return item.ID == id }) {
		return fmt.Errorf("unknown list item %q", id)
	}
	if l.marks[id] {
		delete(l.marks, id)
	} else {
		l.marks[id] = true
	}
	return nil
}

// Marked lists the marked rows in list order.
func (l *List) Marked() []ListItem {
	var out []ListItem
	for _, item := range l.items {
		if l.marks[item.ID] {
			out = append(out, item)
		}
	}
	return out
}

// Focus points the active frame at the list's selected row, or the list.
func (l *List) Focus() error { return l.r.SetFocus(l.focusTarget()) }

// Focused derives focus from the active frame instead of retaining a stale flag.
func (l *List) Focused() bool { return l.holds(l.r.Focus) }

func (l *List) holds(focus string) bool { return focus == l.id || l.r.rows[focus] == l.id }

// FocusTarget is where a focus jump to the list lands: its selected row.
func (l *List) FocusTarget() string { return l.focusTarget() }

func (l *List) focusTarget() string {
	if l.selected >= 0 {
		return l.items[l.selected].ID
	}
	return l.id
}

// syncFocus moves every focus reference that pointed at this list or one of
// its rows to the current row, so hidden pages and modal returns restore
// the live selection rather than a removed row's bindings.
func (l *List) syncFocus(previous ListItem, gone map[string]bool) {
	current, _ := l.Selected()
	if l.Focused() && (previous.ID != current.ID || previous.Noun != current.Noun) {
		l.r.CancelPendingBinding()
	}
	l.r.RetargetFocus(func(id string) bool {
		return l.holds(id) || gone[id] || id == previous.ID && previous.ID != ""
	}, l.focusTarget())
}

func (l *List) Snapshot() map[string]any {
	selected, _ := l.Selected()
	marked := make([]string, 0, len(l.marks))
	for _, item := range l.Marked() {
		marked = append(marked, item.ID)
	}
	return map[string]any{"id": l.id, "items": slices.Clone(l.items), "selected": l.selected, "selectedId": selected.ID, "offset": l.offset, "focused": l.Focused(), "marked": marked}
}

// View fits the viewport and updates its scroll offset to keep selection
// visible. Calling it places the list in its page's drawing order (Placed),
// even at zero height.
func (l *List) View(width, height int) string {
	l.r.Placed(l.id)
	if width <= 0 || height <= 0 {
		return ""
	}
	if len(l.items) == 0 {
		return strings.Join(layoutRows("(empty list)", width, height), "\n")
	}
	visible := height
	if len(l.items) > height && height > 1 {
		visible--
	}
	l.offset = min(l.offset, max(0, len(l.items)-visible))
	if l.selected < l.offset {
		l.offset = l.selected
	} else if l.selected >= l.offset+visible {
		l.offset = l.selected - visible + 1
	}
	end := min(len(l.items), l.offset+visible)
	rows := make([]string, 0, height)
	focused := l.Focused()
	for i := l.offset; i < end; i++ {
		mark := "  "
		if l.marks[l.items[i].ID] {
			mark = "* "
		}
		if i == l.selected {
			mark = "> "
		}
		row := ansi.Truncate(mark+modalLabel(l.items[i].Label), width, "…")
		if i == l.selected && focused {
			row = lipgloss.NewStyle().Reverse(true).Render(row)
		}
		rows = append(rows, row)
	}
	if len(l.items) > height && height > 1 {
		rows = append(rows, fmt.Sprintf("%d–%d / %d", l.offset+1, end, len(l.items)))
	}
	return strings.Join(layoutRows(strings.Join(rows, "\n"), width, height), "\n")
}

func registerLists(r *Runtime) {
	r.Define("list").Focusable().Mode("normal").
		Bind("j", Do("list.move", Arguments{"delta": 1})).
		Bind("down", Do("list.move", Arguments{"delta": 1})).
		Bind("k", Do("list.move", Arguments{"delta": -1})).
		Bind("up", Do("list.move", Arguments{"delta": -1})).
		Bind("space", Do("list.mark"))
	resolve := func(c Context, args Arguments) (*List, error) {
		if id := StringArg(args, "id", ""); id != "" {
			if list := r.List(id); list != nil {
				return list, nil
			}
			return nil, fmt.Errorf("unknown list %q", id)
		}
		for _, id := range r.Ancestry(focusOf(c)) {
			if list := r.List(id); list != nil && c.InputID == "" {
				return list, nil
			}
		}
		return nil, errors.New("no focused list")
	}
	// rowOf names the row an operation addresses: args.item, else the row on
	// the context, else the selection.
	rowOf := func(l *List, c Context, args Arguments) (string, error) {
		if item := StringArg(args, "item", ""); item != "" {
			return item, nil
		}
		if focus := focusOf(c); l.r.rows[focus] == l.id {
			return focus, nil
		}
		if selected, ok := l.Selected(); ok {
			return selected.ID, nil
		}
		return "", errors.New("list has no selected row")
	}
	for _, spec := range []struct {
		verb, description string
		parameters        map[string]Parameter
		run               func(*List, Context, Arguments) (any, error)
	}{
		{"focus", "Focus a list and its selected row", map[string]Parameter{"id": {Type: "string", Required: true}}, func(l *List, _ Context, _ Arguments) (any, error) { return l.Snapshot(), l.Focus() }},
		{"move", "Move selection without stealing focus", map[string]Parameter{"id": {Type: "string"}, "delta": {Type: "integer", Required: true}}, func(l *List, _ Context, a Arguments) (any, error) {
			l.Move(IntArg(a, "delta", 0))
			return l.Snapshot(), nil
		}},
		{"select", "Select a list item by stable ID without stealing focus", map[string]Parameter{"id": {Type: "string"}, "item": {Type: "string", Required: true}}, func(l *List, _ Context, a Arguments) (any, error) {
			return l.Snapshot(), l.Select(StringArg(a, "item", ""))
		}},
		{"mark", "Toggle the mark on a row (the selected row by default) for fan-out verbs", map[string]Parameter{"id": {Type: "string"}, "item": {Type: "string"}}, func(l *List, c Context, a Arguments) (any, error) {
			id, err := rowOf(l, c, a)
			if err != nil {
				return nil, err
			}
			return l.Snapshot(), l.ToggleMark(id)
		}},
		{"apply", "Run <row noun>.verb for every marked row (the selected row when none is marked), skipping and reporting rows whose noun lacks the verb", map[string]Parameter{"id": {Type: "string"}, "verb": {Type: "string", Required: true}}, func(l *List, c Context, a Arguments) (any, error) {
			rows := l.Marked()
			if len(rows) == 0 {
				if selected, ok := l.Selected(); ok {
					rows = []ListItem{selected}
				}
			}
			verb := StringArg(a, "verb", "")
			applied, skipped := []string{}, []string{}
			var failure error
			for _, row := range rows {
				q := Request{Verb: verb, Noun: row.Noun, Target: row.ID}
				if _, ok := r.Operations[q.Key()]; !ok {
					skipped = append(skipped, row.ID+": "+q.Key()+" is not registered")
					continue
				}
				if _, err := r.Execute(q); err != nil {
					skipped = append(skipped, row.ID+": "+err.Error())
					if failure == nil {
						failure = err
					}
					continue
				}
				applied = append(applied, row.ID)
			}
			return map[string]any{"verb": verb, "applied": applied, "skipped": skipped}, failure
		}},
	} {
		r.Register(Operation{Verb: spec.verb, Noun: "list", Description: spec.description, Parameters: spec.parameters, Result: "list state", Run: func(_ *Runtime, c Context, args Arguments) (any, error) {
			list, err := resolve(c, args)
			if err != nil {
				return nil, err
			}
			return spec.run(list, c, args)
		}})
	}
}
