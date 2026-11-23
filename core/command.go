// Package core is Ochazuke's pure modal kernel: identity and context chains,
// command specs, modes, layers, keymaps, the frame stack, and Engine.Step.
//
// core performs no I/O and starts no goroutines. It imports only the Go
// standard library and the Tier 0 packages trie, expr and layout.
package core

import (
	"slices"
	"sort"
	"strings"
)

// Ref identifies a domain record without carrying it.
type Ref struct {
	Kind string `json:"kind"` // e.g. "task", "pane", "git.branch", "ui.page", "cell"
	ID   string `json:"id"`   // "" when type-only (empty list or create context)
}

// Actor names who issued a command.
type Actor struct {
	Kind  string `json:"kind"`            // "user", "agent", "system"
	ID    string `json:"id,omitempty"`    // pane/session ID for agents
	Label string `json:"label,omitempty"` // display label
}

// ContextFrame is one level of the context chain. Chains run from the
// innermost focused item (index 0) outward to "app".
type ContextFrame struct {
	Scope    string         `json:"scope"`              // matches Binding.Scope
	NodeID   string         `json:"nodeId,omitempty"`   // UI node ID, "" for a pure domain Ref
	Subject  *Ref           `json:"subject,omitempty"`  // active record at this scope
	Subjects []Ref          `json:"subjects,omitempty"` // active multi-selection
	Facts    map[string]any `json:"facts,omitempty"`    // plain data read by Binding.When
}

// ParamType is the declared type of a command parameter.
type ParamType string

// Parameter types.
const (
	ParamString   ParamType = "string"
	ParamInt      ParamType = "int"
	ParamFloat    ParamType = "float"
	ParamBool     ParamType = "bool"
	ParamDuration ParamType = "duration"
	ParamPath     ParamType = "path"
	ParamRef      ParamType = "ref"
	ParamList     ParamType = "list"
	ParamObject   ParamType = "object"
)

// ParamSpec declares one command parameter.
type ParamSpec struct {
	Name        string    `json:"name"`
	Type        ParamType `json:"type"`
	Required    bool      `json:"required,omitempty"`
	Default     any       `json:"default,omitempty"`
	Enum        []string  `json:"enum,omitempty"`
	RefKinds    []string  `json:"refKinds,omitempty"`
	SourceQuery string    `json:"sourceQuery,omitempty"` // Data API query for dynamic picker choices
	Description string    `json:"description,omitempty"`
	VisibleWhen string    `json:"visibleWhen,omitempty"`
	Repeatable  bool      `json:"repeatable,omitempty"`
	AddLabel    string    `json:"addLabel,omitempty"`
}

// CommandSpec describes a command. Tags are open; core reads them only for
// Layer.ExcludeTags filtering.
type CommandSpec struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	Params      []ParamSpec    `json:"params,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"` // opaque host/CLI/MCP metadata; core ignores it
}

// HasTag reports whether the spec carries tag.
func (c CommandSpec) HasTag(tag string) bool { return slices.Contains(c.Tags, tag) }

// Command is one invocation of a command.
type Command struct {
	ID      string         `json:"id"`
	Target  *Ref           `json:"target,omitempty"`
	Targets []Ref          `json:"targets,omitempty"`
	Args    map[string]any `json:"args,omitempty"`
	Count   int            `json:"count,omitempty"`  // pending numeric prefix; 0 = none
	Source  string         `json:"source,omitempty"` // "key:d d", "palette", "cli", "mcp"
	Actor   Actor          `json:"actor,omitempty"`
}

// Arg returns Args[name] as a string ("" when absent or not a string).
func (c Command) Arg(name string) string {
	s, _ := c.Args[name].(string)
	return s
}

// Times returns Count, or 1 when no count was given.
func (c Command) Times() int { return max(c.Count, 1) }

// Registry maps command IDs to their specs.
type Registry map[string]CommandSpec

// Register adds or replaces specs.
func (r Registry) Register(specs ...CommandSpec) {
	for _, s := range specs {
		r[s.ID] = s
	}
}

// Lookup returns the spec for id.
func (r Registry) Lookup(id string) (CommandSpec, bool) {
	s, ok := r[id]
	return s, ok
}

// HasAnyTag reports whether command id carries any of tags.
func (r Registry) HasAnyTag(id string, tags []string) bool {
	spec, ok := r[id]
	if !ok {
		return false
	}
	for _, t := range tags {
		if spec.HasTag(t) {
			return true
		}
	}
	return false
}

// List returns all specs sorted by ID.
func (r Registry) List() []CommandSpec {
	out := make([]CommandSpec, 0, len(r))
	for _, s := range r {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Built-in command IDs handled by Engine.
const (
	CmdFocusNext  = "focus.next"
	CmdFocusPrev  = "focus.prev"
	CmdFocusLeft  = "focus.left"
	CmdFocusRight = "focus.right"
	CmdFocusUp    = "focus.up"
	CmdFocusDown  = "focus.down"
	CmdFocusTo    = "focus.to"
	CmdModeSet    = "mode.set"
	CmdPageOpen   = "page.open"
	CmdPageNext   = "page.next"
	CmdPagePrev   = "page.prev"
	CmdModalOpen  = "modal.open"
	CmdModalClose = "modal.close"
	CmdLayerPush  = "layer.push"
	CmdLayerPop   = "layer.pop"
	CmdCountPush  = "count.push"
	CmdAppQuit    = "app.quit"
)

func str(name string, required bool, desc string) ParamSpec {
	return ParamSpec{Name: name, Type: ParamString, Required: required, Description: desc}
}

// BuiltinSpecs returns the specs of the commands Engine handles itself.
func BuiltinSpecs() []CommandSpec {
	focus := []string{"focus"}
	return []CommandSpec{
		{ID: CmdFocusNext, Description: "Focus the next focusable node", Tags: focus},
		{ID: CmdFocusPrev, Description: "Focus the previous focusable node", Tags: focus},
		{ID: CmdFocusLeft, Description: "Focus the node to the left", Tags: focus},
		{ID: CmdFocusRight, Description: "Focus the node to the right", Tags: focus},
		{ID: CmdFocusUp, Description: "Focus the node above", Tags: focus},
		{ID: CmdFocusDown, Description: "Focus the node below", Tags: focus},
		{ID: CmdFocusTo, Description: "Focus a node by ID", Tags: focus, Params: []ParamSpec{str("id", true, "node ID")}},
		{ID: CmdModeSet, Description: "Set the active frame's mode", Params: []ParamSpec{str("mode", true, "mode name")}},
		{ID: CmdPageOpen, Description: "Open or switch to a page", Params: []ParamSpec{str("id", true, "page ID"), str("mode", false, "initial mode"), str("focus", false, "initial focus")}},
		{ID: CmdPageNext, Description: "Switch to the next page"},
		{ID: CmdPagePrev, Description: "Switch to the previous page"},
		{ID: CmdModalOpen, Description: "Open a modal frame", Params: []ParamSpec{str("id", true, "modal ID"), str("mode", false, "initial mode"), str("focus", false, "initial focus"), {Name: "data", Type: ParamObject}}},
		{ID: CmdModalClose, Description: "Close the top modal (or the one named by id)", Params: []ParamSpec{str("id", false, "modal ID")}},
		{ID: CmdLayerPush, Description: "Push a transient key layer", Params: []ParamSpec{{Name: "layer", Type: ParamObject, Required: true}}},
		{ID: CmdLayerPop, Description: "Pop the active key layer"},
		{ID: CmdCountPush, Description: "Append a digit to the pending count", Params: []ParamSpec{{Name: "digit", Type: ParamInt, Required: true}}},
		{ID: CmdAppQuit, Description: "Quit the application"},
	}
}

// motionTable lists the standard zero-arg motions and their default keys
// (space-separated sequences).
var motionTable = [...]struct{ id, keys, desc string }{
	{"motion.left", "h", "Move left"},
	{"motion.down", "j", "Move down"},
	{"motion.up", "k", "Move up"},
	{"motion.right", "l", "Move right"},
	{"motion.word.next", "w", "Next word"},
	{"motion.word.prev", "b", "Previous word"},
	{"motion.word.end", "e", "End of word"},
	{"motion.block.next", "}", "Next block"},
	{"motion.block.prev", "{", "Previous block"},
	{"motion.group.next", ")", "Next group"},
	{"motion.group.prev", "(", "Previous group"},
	{"motion.top", "g g", "Go to top"},
	{"motion.bottom", "G", "Go to bottom"},
	{"motion.start", "0", "Go to start"},
	{"motion.end", "$", "Go to end"},
	{"motion.page.down", "ctrl+f", "Page down"},
	{"motion.page.up", "ctrl+b", "Page up"},
	{"motion.half.down", "ctrl+d", "Half page down"},
	{"motion.half.up", "ctrl+u", "Half page up"},
	{"motion.scroll.down", "ctrl+e", "Scroll viewport down"},
	{"motion.scroll.up", "ctrl+y", "Scroll viewport up"},
}

// MotionSpecs returns the standard motion specs, tagged "motion". Each
// spec's Meta["keys"] holds its default key sequence.
func MotionSpecs() []CommandSpec {
	out := make([]CommandSpec, len(motionTable))
	for i, m := range motionTable {
		out[i] = CommandSpec{ID: m.id, Description: m.desc, Tags: []string{"motion", "repeatable"}, Meta: map[string]any{"keys": m.keys}}
	}
	return out
}

// StandardBindings returns the default motion keys for mode plus count
// accumulation: digits push onto the count, and "0" runs motion.start only
// while no count is pending.
func StandardBindings(mode string) []Binding {
	out := make([]Binding, 0, len(motionTable)+1)
	for _, m := range motionTable {
		b := Binding{Mode: mode, Command: m.id, Hint: m.desc}
		if m.keys == "0" {
			b.Class, b.When = ClassDigit0, "count == null"
		} else {
			b.Keys = strings.Fields(m.keys)
		}
		out = append(out, b)
	}
	return append(out, Binding{Mode: mode, Class: ClassDigit, Command: CmdCountPush, Args: map[string]any{"digit": "$key"}, Hidden: true})
}

// DefaultRegistry returns a registry holding BuiltinSpecs and MotionSpecs.
func DefaultRegistry() Registry {
	r := Registry{}
	r.Register(BuiltinSpecs()...)
	r.Register(MotionSpecs()...)
	return r
}
