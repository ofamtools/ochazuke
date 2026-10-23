package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Arguments map[string]any

type Parameter struct {
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

type Request struct {
	Verb   string    `json:"verb"`
	Noun   string    `json:"noun"`
	Target string    `json:"target,omitempty"`
	Args   Arguments `json:"args,omitempty"`
}

func (q Request) Key() string { return q.Verb + "." + q.Noun }

type Context struct {
	Path    []string `json:"path"`
	InputID string   `json:"inputId,omitempty"`
}

func (c Context) Clone() Context {
	return Context{Path: append([]string(nil), c.Path...), InputID: c.InputID}
}

type Handler func(*Runtime, Context, Arguments) (any, error)

type Operation struct {
	Verb        string               `json:"verb"`
	Noun        string               `json:"noun"`
	Description string               `json:"description"`
	Parameters  map[string]Parameter `json:"parameters"`
	Result      string               `json:"result"`
	Run         Handler              `json:"-"`
}

func (o Operation) Key() string { return o.Verb + "." + o.Noun }

type Binding struct {
	Mode    string  `json:"mode,omitempty"`
	Keys    string  `json:"keys"`
	Request Request `json:"request"`
}

type Layer struct {
	ID       string             `json:"id"`
	Noun     string             `json:"noun"`
	Bindings []Binding          `json:"bindings"`
	Actions  map[string]Handler `json:"-"`
}

type Frame struct {
	ID      string         `json:"id"`
	Context Context        `json:"context"`
	State   map[string]any `json:"state"`
}

// Runtime is owned by one event loop. Transport callers submit messages to that loop.
type Runtime struct {
	Layers     map[string]*Layer
	Inputs     map[string]*Input
	InputPaths map[string][]string
	Frames     []*Frame
	Modes      map[string]bool
	Operations map[string]Operation
	Quit       bool
	Pending    string
}

func NewRuntime() *Runtime {
	r := &Runtime{
		Layers: map[string]*Layer{"app": {ID: "app", Noun: "app", Actions: map[string]Handler{}}},
		Inputs: map[string]*Input{}, InputPaths: map[string][]string{},
		Frames: []*Frame{{ID: "main", Context: Context{Path: []string{"app"}}, State: map[string]any{}}},
		Modes:  map[string]bool{"normal": true, "insert": true}, Operations: map[string]Operation{},
	}
	registerCore(r)
	registerConfiguration(r)
	RegisterInputOperations(r)
	return r
}

func (r *Runtime) Context() Context { return r.Frames[len(r.Frames)-1].Context.Clone() }
func (r *Runtime) Mode(c Context) string {
	if in := r.Inputs[c.InputID]; in != nil {
		return in.Mode
	}
	if mode, ok := r.Frames[len(r.Frames)-1].State["mode"].(string); ok {
		return mode
	}
	return "normal"
}
func (r *Runtime) Register(o Operation) {
	if o.Verb == "" || o.Noun == "" || o.Run == nil {
		panic("invalid operation registration")
	}
	if _, exists := r.Operations[o.Key()]; exists {
		panic("duplicate operation: " + o.Key())
	}
	if o.Parameters == nil {
		o.Parameters = map[string]Parameter{}
	}
	r.Operations[o.Key()] = o
}
func (r *Runtime) AddInput(in *Input, path []string) error {
	if in.ID == "" || r.Inputs[in.ID] != nil {
		return errors.New("input ID must be unique and nonempty")
	}
	if err := r.validateContext(Context{Path: path, InputID: ""}); err != nil {
		return err
	}
	r.Inputs[in.ID] = in
	r.InputPaths[in.ID] = append([]string(nil), path...)
	return nil
}
func (r *Runtime) validateContext(c Context) error {
	if len(c.Path) == 0 || c.Path[0] != "app" {
		return errors.New("interaction path must start at app")
	}
	seen := map[string]bool{}
	for _, id := range c.Path {
		if r.Layers[id] == nil || seen[id] {
			return fmt.Errorf("invalid layer %q", id)
		}
		seen[id] = true
	}
	if c.InputID != "" && r.Inputs[c.InputID] == nil {
		return fmt.Errorf("unknown input %q", c.InputID)
	}
	return nil
}
func (r *Runtime) Focus(id string) error {
	path, ok := r.InputPaths[id]
	if !ok {
		return fmt.Errorf("unknown input %q", id)
	}
	r.Frames[len(r.Frames)-1].Context = Context{Path: append([]string(nil), path...), InputID: id}
	r.Pending = ""
	return nil
}
func (r *Runtime) Push(frame *Frame) error {
	if frame == nil || frame.ID == "" {
		return errors.New("modal ID is required")
	}
	for _, f := range r.Frames {
		if f.ID == frame.ID {
			return errors.New("modal is already open")
		}
	}
	if err := r.validateContext(frame.Context); err != nil {
		return err
	}
	frame.Context = frame.Context.Clone()
	if frame.State == nil {
		frame.State = map[string]any{}
	}
	r.Frames = append(r.Frames, frame)
	r.Pending = ""
	return nil
}
func (r *Runtime) Pop() error {
	if len(r.Frames) == 1 {
		return errors.New("no modal to close")
	}
	r.Frames = r.Frames[:len(r.Frames)-1]
	r.Pending = ""
	return nil
}
func (r *Runtime) Execute(q Request) (any, error) { return r.ExecuteAt(r.Context(), q) }
func (r *Runtime) ExecuteAt(c Context, q Request) (any, error) {
	op, ok := r.Operations[q.Key()]
	if !ok {
		return nil, fmt.Errorf("unknown operation %q", q.Key())
	}
	if err := validateArguments(op, q.Args); err != nil {
		return nil, err
	}
	if q.Target != "" && q.Target != "focused-input" {
		path, ok := r.InputPaths[q.Target]
		if !ok {
			return nil, fmt.Errorf("unknown target %q", q.Target)
		}
		c = Context{Path: append([]string(nil), path...), InputID: q.Target}
	}
	if err := r.validateContext(c); err != nil {
		return nil, err
	}
	for i := len(c.Path) - 1; i >= 0; i-- {
		if fn := r.Layers[c.Path[i]].Actions[q.Key()]; fn != nil {
			return fn(r, c, q.Args)
		}
	}
	return op.Run(r, c, q.Args)
}
func validateArguments(op Operation, args Arguments) error {
	for name, p := range op.Parameters {
		if _, ok := args[name]; p.Required && !ok {
			return fmt.Errorf("%s is required", name)
		}
	}
	for name, v := range args {
		p, ok := op.Parameters[name]
		if !ok {
			return fmt.Errorf("unknown argument %q", name)
		}
		valid := false
		switch p.Type {
		case "string":
			_, valid = v.(string)
		case "integer":
			switch n := v.(type) {
			case int:
				valid = true
			case float64:
				valid = n == float64(int(n))
			}
		case "boolean":
			_, valid = v.(bool)
		}
		if !valid {
			return fmt.Errorf("%s must be %s", name, p.Type)
		}
	}
	return nil
}
func StringArg(a Arguments, name, fallback string) string {
	if v, ok := a[name].(string); ok {
		return v
	}
	return fallback
}
func IntArg(a Arguments, name string, fallback int) int {
	switch v := a[name].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return fallback
}
func (r *Runtime) Catalog() []Operation {
	out := make([]Operation, 0, len(r.Operations))
	for _, op := range r.Operations {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// EffectiveBindings overlays complete key sequences, not individual keystrokes.
func (r *Runtime) EffectiveBindings(c Context) []Binding {
	byKey := map[string]Binding{}
	mode := r.Mode(c)
	for _, id := range c.Path {
		layer := r.Layers[id]
		for _, b := range layer.Bindings {
			if b.Mode == "" {
				byKey[b.Keys] = b
			}
		}
		for _, b := range layer.Bindings {
			if b.Mode == mode && b.Mode != "" {
				byKey[b.Keys] = b
			}
		}
	}
	out := make([]Binding, 0, len(byKey))
	for _, b := range byKey {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Keys < out[j].Keys })
	return out
}

// HandleBinding returns handled=true for both complete and pending sequences.
func (r *Runtime) HandleBinding(key string) (bool, error) {
	if key == "esc" && r.Pending != "" {
		r.Pending = ""
		return true, nil
	}
	sequence := key
	if r.Pending != "" {
		sequence = r.Pending + " " + key
	}
	bindings := r.EffectiveBindings(r.Context())
	for _, b := range bindings {
		if b.Keys == sequence {
			r.Pending = ""
			_, err := r.Execute(b.Request)
			return true, err
		}
	}
	for _, b := range bindings {
		if strings.HasPrefix(b.Keys, sequence+" ") {
			r.Pending = sequence
			return true, nil
		}
	}
	hadPending := r.Pending != ""
	r.Pending = ""
	if hadPending {
		return r.HandleBinding(key)
	}
	return false, nil
}
func (r *Runtime) Snapshot() map[string]any {
	inputs := map[string]any{}
	for id, in := range r.Inputs {
		inputs[id] = in.Snapshot()
	}
	layers := map[string]any{}
	for id, layer := range r.Layers {
		actions := make([]string, 0, len(layer.Actions))
		for key := range layer.Actions {
			actions = append(actions, key)
		}
		sort.Strings(actions)
		layers[id] = map[string]any{"noun": layer.Noun, "bindings": layer.Bindings, "actions": actions}
	}
	modes := make([]string, 0, len(r.Modes))
	for mode := range r.Modes {
		modes = append(modes, mode)
	}
	sort.Strings(modes)
	return map[string]any{"frames": r.Frames, "inputs": inputs, "context": r.Context(), "mode": r.Mode(r.Context()), "modes": modes, "layers": layers, "bindings": r.EffectiveBindings(r.Context()), "pending": r.Pending, "quit": r.Quit}
}
func JSONPointer(value any, path string) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var node any
	if err = json.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	if path == "" {
		return node, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, errors.New("state path must be a JSON Pointer starting with /")
	}
	for _, raw := range strings.Split(path[1:], "/") {
		key := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch v := node.(type) {
		case map[string]any:
			var ok bool
			node, ok = v[key]
			if !ok {
				return nil, fmt.Errorf("unknown state path %q", path)
			}
		case []any:
			n, e := strconv.Atoi(key)
			if e != nil || n < 0 || n >= len(v) {
				return nil, fmt.Errorf("invalid array index %q", key)
			}
			node = v[n]
		default:
			return nil, fmt.Errorf("cannot descend into %q", key)
		}
	}
	return node, nil
}

func registerCore(r *Runtime) {
	r.Register(Operation{Verb: "get", Noun: "state", Description: "Inspect public state using a JSON Pointer", Parameters: map[string]Parameter{"path": {Type: "string"}}, Result: "JSON value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return JSONPointer(r.Snapshot(), StringArg(a, "path", ""))
	}})
	r.Register(Operation{Verb: "list", Noun: "operations", Description: "List registered verbs, nouns, and argument schemas", Result: "operation[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) { return r.Catalog(), nil }})
	r.Register(Operation{Verb: "list", Noun: "bindings", Description: "List effective bindings for the current context", Result: "binding[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) { return r.EffectiveBindings(c), nil }})
	r.Register(Operation{Verb: "focus", Noun: "input", Description: "Focus a registered input", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.Focus(StringArg(a, "id", ""))
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "set", Noun: "mode", Description: "Set the focused input or active frame's registered interaction mode", Parameters: map[string]Parameter{"mode": {Type: "string", Required: true}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		mode := StringArg(a, "mode", "")
		if !r.Modes[mode] {
			return nil, fmt.Errorf("unknown mode %q", mode)
		}
		if in := r.Inputs[c.InputID]; in != nil {
			in.SetMode(mode)
		} else {
			r.Frames[len(r.Frames)-1].State["mode"] = mode
		}
		return mode, nil
	}})
	r.Register(Operation{Verb: "push", Noun: "modal", Description: "Open a modal focused on a registered input", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}, "input": {Type: "string", Required: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := StringArg(a, "input", "")
		path, ok := r.InputPaths[id]
		if !ok {
			return nil, errors.New("unknown modal input")
		}
		err := r.Push(&Frame{ID: StringArg(a, "id", ""), Context: Context{Path: path, InputID: id}, State: map[string]any{}})
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "pop", Noun: "modal", Description: "Close the active modal and restore its parent", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) { err := r.Pop(); return r.Context(), err }})
	r.Register(Operation{Verb: "quit", Noun: "app", Description: "Exit the application", Result: "boolean", Run: func(r *Runtime, c Context, a Arguments) (any, error) { r.Quit = true; return true, nil }})
}
