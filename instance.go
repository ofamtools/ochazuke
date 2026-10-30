package ochazuke

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
)

// Instance is one addressable thing on screen: a page, a list, a list row, a
// modal body, or an input. Parent links form the focus stack; walking them
// from the focused instance outward is the path Context.Path spells out.
// An instance holds no mode: the mode is global (Runtime.Mode). Payload is
// the data a modal body carries (a confirm's prompt and request). Bindings
// are this instance's own rows, resolved above its noun's table at the same
// stack level. Component holds the typed component the instance fronts.
// Unfocusable opts the instance out of being a focus stop although its noun
// is Focusable (a disabled panel). Label is an optional display name a
// confirm prompt reads as {{label}}. Data is an opaque application value
// (a list row's ListItem.Data); the framework never reads it.
type Instance struct {
	ID          string    `json:"id"`
	Noun        string    `json:"noun"`
	Parent      string    `json:"parent,omitempty"`
	Payload     Payload   `json:"payload"`
	Bindings    []Binding `json:"bindings,omitempty"`
	Unfocusable bool      `json:"unfocusable,omitempty"`
	Label       string    `json:"label,omitempty"` // a display name (a list row's label, a header name); {{label}} in a confirm prompt
	Data        any       `json:"data,omitempty"`  // an opaque application value (a list row's ListItem.Data)
	Component   any       `json:"-"`
	seq         uint64
	returnMode  ModeState // an open modal's mode to restore on Pop, beside Payload.Return
	lastFocus   string    // the focus last inside this instance when focus left it; a jump lands there
	borrowed    string    // an open modal's adopted focus (Push of a focus outside it) and that focus's own parent, restored on Pop
	lentParent  string
	closeFocus  string // FocusOnClose: where the focus goes when this instance is removed while it holds the focus
	inner       string // an instance whose rows answer before this one's while this one is the focus (an input's open suggestions)
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
	// Anchor is the instance a modal is opened for (a form field, for its
	// chooser): the modal is drawn beside it (modal.anchor).
	Anchor string `json:"anchor,omitempty"`
}

func (p Payload) empty() bool {
	return p.Count == 0 && p.OpCount == 0 && p.Op == "" && p.Prefix == "" && p.Prompt == "" && p.Request == nil && p.Shadow == nil && p.Anchor == ""
}

type instanceSpec struct {
	parent, noun, focus, closeFocus string
	closeOnDetach, noSpill, noMarks bool
	clearFilterOnEnter              bool
	cellSelect                      bool
	defaultHidden                   bool   // DefaultHidden: a page out of the tabs until shown
	followTail                      bool   // FollowTail: a timeline sticks to its last card
	headerLines                     int    // HeaderLines: a timeline's closed-card header cap
	parentStop                      string // ParentStop: where h on a timeline's closed card goes
	columns                         []Column
	completions                     []CompletionTrigger
}

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

// CloseOnDetach makes terminal.detach remove the terminal instance after
// returning focus, so the stream is released (see TerminalReleaser) instead
// of kept for a later attach. Only AddTerminal reads it.
func CloseOnDetach() InstanceOption { return func(s *instanceSpec) { s.closeOnDetach = true } }

// FocusOnClose names the instance the focus moves to when this instance is
// removed while the focus is on it or inside it: a CloseOnDetach detach,
// Pop of a modal body, a list row dropped by SetItems, RemoveInstance. A
// target that is gone (or removed with it) falls back to the default (the
// parent, or the return Pop restores). Every constructor reads it.
func FocusOnClose(id string) InstanceOption {
	return func(s *instanceSpec) { s.closeFocus = id }
}

// NoSpill makes a list trap its motions: list.move past its last (first)
// row stays on that row instead of moving the focus to the next (previous)
// stop on the page. For a list that must keep the focus, such as a picker.
func NoSpill() InstanceOption { return func(s *instanceSpec) { s.noSpill = true } }

// NoMarks makes a list (or a tree's list) a read-only view: its rows take
// no marks, so space does not answer with list.mark there (the verb's
// guard, list-marks, is false; the footer and ? help leave it out) and
// list.mark refuses. list.apply still runs on the selected row.
func NoMarks() InstanceOption { return func(s *instanceSpec) { s.noMarks = true } }

// ClearFilterOnEnter makes enter in a list's (or a tree's list's) filter
// line choose the selected match: list.filter-choose clears the filter
// with the selection staying on that row, every row shows again, the mode
// is normal and the page keeps the row on screen. For a list one picks a
// row from, such as a pane list. With no match enter keeps the filter, as
// on every other list (list.filter-keep; its verb guard, list-chooses, is
// false there and off this option).
func ClearFilterOnEnter() InstanceOption {
	return func(s *instanceSpec) { s.clearFilterOnEnter = true }
}

// Instance registers a plain instance: a page body, a panel, a modal body.
// Its noun is defined when it does not exist yet; a framework alias names
// the framework's noun (Kind("prompt") is ochazuke.prompt). Registering an existing id
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
	spec.noun = r.instanceNoun(spec.noun) // an alias names its noun (Instance("page") answers to ochazuke.page)
	r.seq++
	inst := &Instance{ID: id, Noun: spec.noun, Parent: spec.parent, seq: r.seq, closeFocus: spec.closeFocus}
	r.register(inst)
	return inst
}

// instanceNoun is the noun an instance of kind names: an alias resolved
// (Kind("prompt") is ochazuke.prompt), defined when it does not exist yet.
func (r *Runtime) instanceNoun(kind string) string {
	if r.builtinDepth > 0 {
		if _, aliased := r.aliases[kind]; !aliased {
			kind = frameworkName(kind)
		}
	}
	name := r.CanonicalNoun(kind)
	r.ensureNoun(name)
	return name
}

// register files a new instance in the registry and, unless it is a list
// row (SetItems files r.rows before it creates the row), in the holder
// index components() walks, so delivery costs the number of non-row
// instances, never the number of rows.
func (r *Runtime) register(inst *Instance) {
	r.Instances[inst.ID] = inst
	if r.rows[inst.ID] == "" {
		r.holders = append(r.holders, inst.ID)
		r.notePlacement(placementNote{id: inst.ID})
	}
}

// unregister drops an instance from the registry, the row index and the
// holder index.
func (r *Runtime) unregister(id string) {
	delete(r.Instances, id)
	if _, row := r.rows[id]; row {
		delete(r.rows, id)
		return
	}
	if i := slices.Index(r.holders, id); i >= 0 {
		r.holders = slices.Delete(r.holders, i, i+1)
	}
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

// childIndex is Children for every instance at once, one pass over the
// instances: a walk of a tree asks it once instead of scanning the whole
// map per node (a page of thousands of rows made that quadratic).
func (r *Runtime) childIndex() map[string][]*Instance {
	out := map[string][]*Instance{}
	for _, inst := range r.Instances {
		if inst.Parent != inst.ID {
			out[inst.Parent] = append(out[inst.Parent], inst)
		}
	}
	for _, kids := range out {
		sort.Slice(kids, func(i, j int) bool { return kids[i].seq < kids[j].seq })
	}
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

// rowSourceField is the field a "row:<field>" binding source names.
func rowSourceField(source string) (string, bool) {
	return strings.CutPrefix(source, SourceRow+":")
}

// rowData is the Data of the row a key acted on: the innermost instance
// carrying Data from the focus out to the level that answered (to app for
// a noun-only level).
func (r *Runtime) rowData(ev KeyEvent) any {
	for id := ev.Focus; id != ""; {
		inst := r.Instances[id]
		if inst == nil {
			return nil
		}
		if inst.Data != nil {
			return inst.Data
		}
		if id == ev.Level {
			return nil
		}
		id = inst.Parent
	}
	return nil
}

// jsonShaped is an application value as its JSON decoding (a struct a
// map[string]any, numbers float64), so a whole row passes as an object
// argument the way the CLI would send it; a map[string]any stays as it is.
func jsonShaped(data any) any {
	switch data.(type) {
	case nil, map[string]any, string, bool, int, float64:
		return data
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	var out any
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

// validRowField reports whether a row source's field is dot-separated
// non-empty names ("path", "repo.path").
func validRowField(field string) bool {
	for _, part := range strings.Split(field, ".") {
		if strings.TrimSpace(part) == "" || strings.ContainsAny(part, " \t") {
			return false
		}
	}
	return true
}

// dataField reads a dotted field of an application value: a map's key, or
// a struct's JSON field (the value read through its JSON encoding, so
// numbers are float64); nil when a step is missing.
func dataField(data any, field string) any {
	for _, name := range strings.Split(field, ".") {
		switch m := data.(type) {
		case nil:
			return nil
		case map[string]any:
			data = m[name]
		case map[string]string:
			v, ok := m[name]
			if !ok {
				return nil
			}
			data = v
		default:
			raw, err := json.Marshal(data)
			if err != nil {
				return nil
			}
			var decoded map[string]any
			if json.Unmarshal(raw, &decoded) != nil {
				return nil
			}
			data = decoded[name]
		}
	}
	return data
}
