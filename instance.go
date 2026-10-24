package ochazuke

import (
	"fmt"
	"slices"
	"sort"
)

// Instance is one addressable thing on screen: a page, a list, a list row, a
// modal body, or an input. Its Parent link is what the old Context.Path spelt
// out in full; walking the links from the focused instance outward rebuilds
// that path. State is the instance's own mode; "" inherits the nearest
// ancestor's state, and the root falls back to "normal" (see Runtime.Mode).
// Bindings are this instance's own rows, resolved above its noun's table.
type Instance struct {
	ID       string    `json:"id"`
	Noun     string    `json:"noun"`
	Parent   string    `json:"parent,omitempty"`
	State    string    `json:"state"`
	Bindings []Binding `json:"bindings,omitempty"`
}

// instance returns the instance for id, creating it without a parent.
func (r *Runtime) instance(id string) *Instance {
	inst := r.Instances[id]
	if inst == nil {
		inst = &Instance{ID: id}
		if r.Layers[id] != nil {
			inst.Noun = id
		}
		r.Instances[id] = inst
	}
	return inst
}

// link records parent as id's parent. The first parent an id is used under
// wins; a later, different parent is recorded as an ambiguity and the old
// path semantics stand for that call.
func (r *Runtime) link(id, parent string) {
	inst := r.instance(id)
	switch inst.Parent {
	case "":
		inst.Parent = parent
	case parent:
	default:
		note := fmt.Sprintf("%s: parent %s, also used under %s", id, inst.Parent, parent)
		if !slices.Contains(r.instanceConflicts, note) {
			r.instanceConflicts = append(r.instanceConflicts, note)
		}
	}
}

// adoptPath links every element of a validated interaction path under the
// element before it. Registration points call it with the paths they receive.
func (r *Runtime) adoptPath(path []string) {
	for i, id := range path {
		if i == 0 {
			r.instance(id)
			continue
		}
		r.link(id, path[i-1])
	}
}

// AmbiguousInstances lists ids that were used under two different parents.
func (r *Runtime) AmbiguousInstances() []string {
	out := slices.Clone(r.instanceConflicts)
	sort.Strings(out)
	return out
}

// Ancestry walks the parent links from id outward: id first, app last. An
// unknown id yields nil.
func (r *Runtime) Ancestry(id string) []string {
	r.syncNouns()
	var out []string
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		inst := r.Instances[id]
		if inst == nil {
			break
		}
		seen[id] = true
		out = append(out, id)
		id = inst.Parent
	}
	return out
}

// InstancePath is Ancestry reversed: app first, id last. It is the shape
// Context.Path has today.
func (r *Runtime) InstancePath(id string) []string {
	out := r.Ancestry(id)
	slices.Reverse(out)
	return out
}

// FocusID is the one leaf instance the active frame points at: its input when
// it has one, otherwise the last element of its path. Context() stays the
// source of truth in this phase; FocusID mirrors it.
func (r *Runtime) FocusID() string { return focusOf(r.Context()) }

// focusOf is the leaf instance a context points at.
func focusOf(c Context) string {
	if c.InputID != "" {
		return c.InputID
	}
	return c.Path[len(c.Path)-1]
}

// ContextFromFocus rebuilds the active context from FocusID and the parent
// links alone. It equals Context() whenever every path the application used
// agrees on each instance's parent; see AmbiguousInstances for the exceptions.
func (r *Runtime) ContextFromFocus() Context { return r.contextOf(r.FocusID()) }

// contextOf is the interaction context that addresses one instance: the path
// elements (layers and list rows) on its ancestry, plus InputID when the
// instance is an input. An unknown id yields an empty context.
func (r *Runtime) contextOf(id string) Context {
	var path []string
	for _, step := range r.InstancePath(id) {
		if r.isPathElement(step) {
			path = append(path, step)
		}
	}
	out := Context{Path: path}
	if r.Inputs[id] != nil {
		out.InputID = id
	}
	return out
}

// isPathElement reports whether id may appear in Context.Path: a layer, or a
// list row standing in for its noun.
func (r *Runtime) isPathElement(id string) bool {
	return r.Layers[id] != nil || r.rows[id] != ""
}

// layerFor is the layer view a path element resolves through: the layer of
// that id, or the layer of a row's noun. It is nil for an unknown element.
func (r *Runtime) layerFor(id string) *Layer {
	if layer := r.Layers[id]; layer != nil {
		return layer
	}
	if inst := r.Instances[id]; inst != nil {
		return r.Layers[inst.Noun]
	}
	return nil
}

// instanceBindings are the rows an instance carries above its noun's table.
func (r *Runtime) instanceBindings(id string) []Binding {
	if inst := r.Instances[id]; inst != nil {
		return inst.Bindings
	}
	return nil
}
