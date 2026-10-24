package ochazuke

import (
	"errors"
	"fmt"
	"sort"
)

// Noun is the behaviour shared by every instance of one kind: the verbs it
// answers to, its key bindings per mode, and the action handlers that override
// an operation's default when the noun is on the focus stack.
//
// Phase 3 of the migration keeps the layer API as the write surface: every
// registered Layer is also a noun of the same name, the layer's Actions map is
// the noun's Actions map, and the noun's per-mode binding table is rebuilt from
// the layer's rows whenever the runtime consults it. Write bindings through
// the layer (or set.binding) and read them through either.
type Noun struct {
	Name  string   `json:"name"`
	Verbs []string `json:"verbs,omitempty"`
	// Bindings is keyed by mode name; "" holds the rows that apply in any mode.
	Bindings map[string][]Binding `json:"bindings"`
	Actions  map[string]Handler   `json:"-"`
}

// RegisterNoun declares a noun that has no pre-existing layer. The runtime
// also creates the layer view of the same name so the old API sees it.
func (r *Runtime) RegisterNoun(n *Noun) error {
	if n == nil || n.Name == "" {
		return errors.New("noun name must be nonempty")
	}
	if r.Nouns[n.Name] != nil || r.Layers[n.Name] != nil {
		return fmt.Errorf("noun %q is already registered", n.Name)
	}
	if n.Actions == nil {
		n.Actions = map[string]Handler{}
	}
	r.Nouns[n.Name] = n
	r.Layers[n.Name] = &Layer{ID: n.Name, Noun: n.Name, Bindings: flattenBindings(n.Bindings), Actions: n.Actions}
	r.syncNouns()
	return nil
}

// Noun returns the registered noun of that name, or nil.
func (r *Runtime) Noun(name string) *Noun {
	r.syncNouns()
	return r.Nouns[name]
}

// syncNouns derives the noun and instance tables from the layer and input
// registries. It is idempotent and runs before every read of the new model, so
// layers created by direct map assignment are covered without a hook.
func (r *Runtime) syncNouns() {
	for id, layer := range r.Layers {
		n := r.Nouns[id]
		if n == nil {
			n = &Noun{Name: id}
			r.Nouns[id] = n
		}
		n.Actions = layer.Actions
		n.Bindings = groupBindings(layer.Bindings)
		r.instance(id)
	}
	for name := range r.Nouns {
		if r.Layers[name] == nil {
			delete(r.Nouns, name)
		}
	}
	for id, path := range r.InputPaths {
		r.instance(id)
		if parent := path[len(path)-1]; parent != id {
			r.link(id, parent)
		}
	}
	for id, inst := range r.Instances {
		if r.Layers[id] == nil && r.InputPaths[id] == nil {
			delete(r.Instances, id)
			continue
		}
		inst.Noun = ""
		if r.Layers[id] != nil {
			inst.Noun = id
		}
	}
}

// groupBindings splits layer rows by mode, keeping each mode's row order.
func groupBindings(rows []Binding) map[string][]Binding {
	table := map[string][]Binding{}
	for _, b := range rows {
		table[b.Mode] = append(table[b.Mode], b)
	}
	return table
}

// flattenBindings is the inverse: any-mode rows first, then modes by name.
func flattenBindings(table map[string][]Binding) []Binding {
	modes := make([]string, 0, len(table))
	for mode := range table {
		modes = append(modes, mode)
	}
	sort.Strings(modes)
	var rows []Binding
	for _, mode := range modes {
		rows = append(rows, table[mode]...)
	}
	return rows
}
