package ochazuke

import (
	"slices"
	"sort"
)

// Instance is one addressable thing on screen: a page, a list, a list row, a
// modal body, or an input. Parent links form the focus stack; walking them
// from the focused instance outward is the path Context.Path spells out.
// An instance holds no mode: the mode is global (Runtime.Mode). Payload is
// the data a modal body carries (a confirm's prompt and request). Bindings
// are this instance's own rows, resolved above its noun's table at the same
// stack level. Component holds the typed component the instance fronts.
// Unfocusable opts the instance out of being a focus stop although its noun
// is Focusable (a disabled panel).
type Instance struct {
	ID          string    `json:"id"`
	Noun        string    `json:"noun"`
	Parent      string    `json:"parent,omitempty"`
	Payload     Payload   `json:"payload"`
	Bindings    []Binding `json:"bindings,omitempty"`
	Unfocusable bool      `json:"unfocusable,omitempty"`
	Component   any       `json:"-"`
	seq         uint64
	returnMode  ModeState // an open modal's mode to restore on Pop, beside Payload.Return
	lastFocus   string    // the focus last inside this instance when focus left it; a jump lands there
}

// Payload is the small typed state a mode carries (Runtime.Mode.Payload).
// For a pending mode Return is the mode it returns to on leave.
// Count and OpCount are the Vim counts before and after an operator; Op is
// the pending operator; Prefix is the key sequence typed so far; Request is a
// stored request (the completed match a sequence prefix falls back to, or a
// confirm's deferred action); Prompt is the text a confirm modal shows;
// Shadow is the mode the macro_recording shadow mode re-dispatches in.
type Payload struct {
	Return  string     `json:"return,omitempty"`
	Count   int        `json:"count,omitempty"`
	OpCount int        `json:"opCount,omitempty"`
	Op      string     `json:"op,omitempty"`
	Prefix  string     `json:"prefix,omitempty"`
	Prompt  string     `json:"prompt,omitempty"`
	Request *Request   `json:"request,omitempty"`
	Shadow  *ModeState `json:"shadow,omitempty"`
}

func (p Payload) empty() bool {
	return p.Count == 0 && p.OpCount == 0 && p.Op == "" && p.Prefix == "" && p.Prompt == "" && p.Request == nil && p.Shadow == nil
}

type instanceSpec struct{ parent, noun, focus string }

// InstanceOption configures the instance constructors: Instance, AddPage,
// AddList, AddInput, AddTerminal and RegisterPage.
type InstanceOption func(*instanceSpec)

// Parent places the instance under another instance (default app).
func Parent(id string) InstanceOption { return func(s *instanceSpec) { s.parent = id } }

// Kind names the noun the instance answers to (default: the instance id for
// plain instances, "editor" for inputs, "list" for lists, "page" for pages,
// "terminal" for terminals).
func Kind(noun string) InstanceOption { return func(s *instanceSpec) { s.noun = noun } }

// Focus names the instance a page starts focused on (default: the page
// itself). Other constructors ignore it.
func Focus(id string) InstanceOption { return func(s *instanceSpec) { s.focus = id } }

// Instance registers a plain instance: a page body, a panel, a modal body.
// Its noun is defined when it does not exist yet. Registering an existing id
// returns it unchanged.
func (r *Runtime) Instance(id string, opts ...InstanceOption) *Instance {
	if inst := r.Instances[id]; inst != nil {
		return inst
	}
	spec := instanceSpec{parent: "app", noun: id}
	if id == "app" {
		spec.parent = ""
	}
	for _, opt := range opts {
		opt(&spec)
	}
	r.Define(spec.noun)
	r.seq++
	inst := &Instance{ID: id, Noun: spec.noun, Parent: spec.parent, seq: r.seq}
	r.Instances[id] = inst
	return inst
}

// Children lists the instances parented at id in registration order.
func (r *Runtime) Children(id string) []*Instance {
	var out []*Instance
	for _, inst := range r.Instances {
		if inst.Parent == id && inst.ID != id {
			out = append(out, inst)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out
}

// Ancestry walks the parent links from id outward: id first, app last. An
// unknown id yields nil.
func (r *Runtime) Ancestry(id string) []string {
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

// InstancePath is Ancestry reversed: app first, id last.
func (r *Runtime) InstancePath(id string) []string {
	out := r.Ancestry(id)
	slices.Reverse(out)
	return out
}

// focusOf is the leaf instance a context points at.
func focusOf(c Context) string {
	if c.InputID != "" {
		return c.InputID
	}
	if len(c.Path) == 0 {
		return ""
	}
	return c.Path[len(c.Path)-1]
}

// contextOf is the interaction context that addresses one instance: the
// non-input instances on its ancestry, plus InputID when it is an input.
func (r *Runtime) contextOf(id string) Context {
	path := r.InstancePath(id)
	out := Context{Path: path}
	if r.Input(id) != nil {
		out.InputID = id
		out.Path = path[:len(path)-1]
	}
	return out
}

// ActionFor returns the innermost noun handler for an operation key on the
// focus stack, or nil when the operation's own Run applies.
func (r *Runtime) ActionFor(focus, key string) Handler {
	for _, id := range r.Ancestry(focus) {
		if noun := r.Nouns[r.Instances[id].Noun]; noun != nil {
			if fn := noun.Actions[key]; fn != nil {
				return fn
			}
		}
	}
	return nil
}
