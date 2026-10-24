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
type ListItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Noun  string `json:"noun"`
}

// List owns flat selection and scrolling on the runtime's event loop.
// Its path ends at the list layer; only the selected row's ID is appended.
type List struct {
	r        *Runtime
	path     []string
	items    []ListItem
	selected int
	offset   int
}

// NewList registers one list at path's final layer, which is also its operation ID.
// All layers must already exist. Existing layer bindings override the defaults.
func NewList(r *Runtime, path []string, items []ListItem) (*List, error) {
	if len(path) < 2 {
		return nil, errors.New("list path must include app and a list layer")
	}
	if err := r.validateContext(Context{Path: path}); err != nil {
		return nil, err
	}
	id := path[len(path)-1]
	if r.Lists[id] != nil {
		return nil, fmt.Errorf("list %q is already registered", id)
	}
	r.adoptPath(path)
	list := &List{r: r, path: slices.Clone(path), selected: -1}
	if err := list.SetItems(items); err != nil {
		return nil, err
	}
	r.Lists[id] = list
	bindings := make([]Binding, 0, 4+len(r.Layers[id].Bindings))
	for _, key := range []struct {
		name  string
		delta int
	}{{"j", 1}, {"down", 1}, {"k", -1}, {"up", -1}} {
		bindings = append(bindings, Binding{Mode: "normal", Keys: key.name, Request: Request{
			Verb: "move", Noun: "list", Args: Arguments{"id": id, "delta": key.delta},
		}})
	}
	r.Layers[id].Bindings = append(bindings, r.Layers[id].Bindings...)
	return list, nil
}

// SetItems copies rows, preserves selection by ID, and falls back to the nearest
// remaining index if that ID disappears. Invalid updates leave the list unchanged.
// It creates one instance per new row under the list and removes the instances
// of rows that disappear; a retained row keeps its instance and any bindings
// the application put on it.
func (l *List) SetItems(items []ListItem) error {
	id := l.path[len(l.path)-1]
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		if item.ID == "" || ids[item.ID] {
			return fmt.Errorf("list item ID %q must be nonempty and unique", item.ID)
		}
		if l.r.Noun(item.Noun) == nil {
			return fmt.Errorf("list item %q: unknown noun %q", item.ID, item.Noun)
		}
		if owner := l.r.rows[item.ID]; l.r.Layers[item.ID] != nil || l.r.Inputs[item.ID] != nil || owner != "" && owner != id {
			return fmt.Errorf("list item ID %q is already another instance", item.ID)
		}
		ids[item.ID] = true
	}
	for _, old := range l.items {
		if !ids[old.ID] {
			delete(l.r.rows, old.ID)
			delete(l.r.Instances, old.ID)
		}
	}
	for _, item := range items {
		l.r.rows[item.ID] = id
		inst := l.r.instance(item.ID)
		inst.Noun, inst.Parent = item.Noun, id
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
	l.syncContexts(previous)
	return nil
}

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
		l.syncContexts(previous)
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

// Focus replaces the active context with the list path and selected row.
func (l *List) Focus() error { return l.r.SelectContext(l.context()) }

// Focused derives focus from the active frame instead of retaining a stale flag.
func (l *List) Focused() bool {
	return l.contains(l.r.Frames[len(l.r.Frames)-1].Context)
}

func (l *List) contains(c Context) bool {
	return c.InputID == "" && (len(c.Path) == len(l.path) || len(c.Path) == len(l.path)+1) && slices.Equal(c.Path[:len(l.path)], l.path)
}

func (l *List) context() Context {
	size := len(l.path)
	if l.selected >= 0 {
		size++
	}
	path := make([]string, size)
	copy(path, l.path)
	if l.selected >= 0 {
		path[len(l.path)] = l.items[l.selected].ID
	}
	return Context{Path: path}
}

func (l *List) syncContexts(previous ListItem) {
	current, _ := l.Selected()
	if l.Focused() && (previous.ID != current.ID || previous.Noun != current.Noun) {
		l.r.CancelPendingBinding()
	}
	update := func(frame *Frame) {
		if !l.contains(frame.Context) {
			return
		}
		if l.selected < 0 && len(frame.Context.Path) == len(l.path) || l.selected >= 0 && len(frame.Context.Path) == len(l.path)+1 && frame.Context.Path[len(l.path)] == l.items[l.selected].ID {
			return
		}
		frame.Context = l.context()
	}
	// Hidden page and modal-parent frames must restore the current row, not a
	// removed item's bindings. Editor contexts never match this component path.
	for _, frame := range l.r.Frames {
		update(frame)
	}
	for _, page := range l.r.Pages {
		update(page.Frame)
	}
}

func (l *List) Snapshot() map[string]any {
	selected, _ := l.Selected()
	return map[string]any{"id": l.path[len(l.path)-1], "items": slices.Clone(l.items), "selected": l.selected, "selectedId": selected.ID, "offset": l.offset, "focused": l.Focused()}
}

// View fits the viewport and updates its scroll offset to keep selection visible.
func (l *List) View(width, height int) string {
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
	resolve := func(c Context, args Arguments) (*List, error) {
		if id := StringArg(args, "id", ""); id != "" {
			if list := r.Lists[id]; list != nil {
				return list, nil
			}
			return nil, fmt.Errorf("unknown list %q", id)
		}
		for i := len(c.Path) - 1; i >= 0; i-- {
			if list := r.Lists[c.Path[i]]; list != nil && list.contains(c) {
				return list, nil
			}
		}
		return nil, errors.New("no focused list")
	}
	for _, spec := range []struct {
		verb, description string
		parameters        map[string]Parameter
		run               func(*List, Arguments) error
	}{
		{"focus", "Focus a list and its selected row", map[string]Parameter{"id": {Type: "string", Required: true}}, func(l *List, _ Arguments) error { return l.Focus() }},
		{"move", "Move selection without stealing focus", map[string]Parameter{"id": {Type: "string"}, "delta": {Type: "integer", Required: true}}, func(l *List, a Arguments) error { l.Move(IntArg(a, "delta", 0)); return nil }},
		{"select", "Select a list item by stable ID without stealing focus", map[string]Parameter{"id": {Type: "string"}, "item": {Type: "string", Required: true}}, func(l *List, a Arguments) error { return l.Select(StringArg(a, "item", "")) }},
	} {
		r.Register(Operation{Verb: spec.verb, Noun: "list", Description: spec.description, Parameters: spec.parameters, Result: "list state", Run: func(_ *Runtime, c Context, args Arguments) (any, error) {
			list, err := resolve(c, args)
			if err != nil {
				return nil, err
			}
			if err := spec.run(list, args); err != nil {
				return nil, err
			}
			return list.Snapshot(), nil
		}})
	}
}
