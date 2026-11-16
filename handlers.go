package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
)

// The name table (spine S1 rule 2): Go code a declaration file cites by
// name. A noun file's verb row names its handler, its store names a
// source and its hooks name hooks; the Go side registers each once here
// (AddHandler, AddHook, AddSource) and the loader (cli.LoadNouns) looks
// them up. The table is a noun, ochazuke.handler (list, get), so every
// name can be listed from the CLI, MCP and the palette with where it is
// written in Go, the rows that use it and the parameter type it decodes;
// rule check handlers (rulecheck) fails on a row naming a missing name, a
// name no row uses and a row whose parameters differ from its handler's.

// Kinds of named Go code.
const (
	NamedHandler = "handler"
	NamedHook    = "hook"
	NamedSource  = "source"
)

// Named is one entry of the name table: its name and kind, the Go file
// and line that registered it, the parameter type its handler decodes
// (TypedHandler; "" for a handler that reads its Arguments itself), the
// parameters that type declares (nil for none) and the declaration rows
// that use it (verb keys, NOUN.store, NOUN.hooks.NAME).
type Named struct {
	Name      string               `json:"name"`
	Kind      string               `json:"kind"`
	File      string               `json:"file,omitempty"`
	Line      int                  `json:"line,omitempty"`
	ParamType string               `json:"paramType,omitempty"`
	Params    map[string]Parameter `json:"-"`
	Rows      []string             `json:"rows,omitempty"`
	Value     any                  `json:"-"`
}

// At is File:Line, "" when unknown.
func (n Named) At() string {
	if n.File == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", n.File, n.Line)
}

// DeclProblem is one problem a declaration loader found: the file, the
// row it is about (a noun, NOUN.VERB, a key) and what is wrong. Loading
// went on past it (record first, never refuse).
type DeclProblem struct {
	File string `json:"file"`
	Row  string `json:"row,omitempty"`
	// Name is the handler, hook or source a missing-name or params
	// problem is about.
	Name string `json:"name,omitempty"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func (p DeclProblem) String() string {
	place := p.File
	if p.Row != "" {
		place += " " + p.Row
	}
	return place + ": " + p.Text
}

// Declaration problem kinds.
const (
	ProblemMissingName = "missing-name" // a row names a handler, hook or source the table lacks
	ProblemParams      = "params"       // a row's parameters differ from its handler's type
	ProblemUnknownKey  = "unknown-key"  // a key the schema does not know, kept and warned
	ProblemBadRow      = "bad-row"      // a row that does not decode or apply
)

// NameTable is the runtime's name table (Runtime.Names) and the problems
// the declaration loaders reported (Problems).
type NameTable struct {
	Entries  map[string]*Named
	Order    []string
	Problems []DeclProblem
}

// HandlerOption adds to a handler's entry.
type HandlerOption func(*Named)

// WithParams declares the parameters a handler takes when they are not a
// typed struct's (a handler made from a data row, such as a daemon
// verb's): the verb row naming it then states none (rule check handlers).
func WithParams(params map[string]Parameter) HandlerOption {
	return func(n *Named) { n.Params = maps.Clone(params) }
}

// AddHandler registers a verb handler (a Handler or an AppHandler, as
// NounBuilder.Verb takes) under name. A name registered again is
// replaced, keeping its rows.
func (r *Runtime) AddHandler(name string, h any, opts ...HandlerOption) {
	appHandlerOf(h) // panics on a value that is not a handler, as Verb does
	r.addNamed(Named{Name: name, Kind: NamedHandler, Value: h}, 2, opts...)
}

// AddTypedHandler registers a handler that decodes its arguments into a
// P (TypedHandler): its parameters are P's fields, so a row naming it
// states none.
func AddTypedHandler[P any](r *Runtime, name string, fn func(Ctx, P) Result) {
	h, params, typ := TypedHandler(fn)
	file, line := funcAt(fn)
	r.addNamed(Named{Name: name, Kind: NamedHandler, Value: h, Params: params, ParamType: typ, File: file, Line: line}, 2)
}

// AddHook registers a hook (a store's or a prop's Go function a
// declaration cites by name).
func (r *Runtime) AddHook(name string, fn any) {
	r.addNamed(Named{Name: name, Kind: NamedHook, Value: fn}, 2)
}

// AddSource registers a source: a function a noun file's store names,
// called once when the noun loads, answering the noun's Store.
func (r *Runtime) AddSource(name string, fn func() Store) {
	r.addNamed(Named{Name: name, Kind: NamedSource, Value: fn}, 2)
}

func (r *Runtime) addNamed(n Named, skip int, opts ...HandlerOption) {
	// Where the Go code is written: the function's own file and line (a
	// method value's method, a closure's literal), else the registering call.
	if n.File == "" {
		n.File, n.Line = funcAt(n.Value)
	}
	if n.File == "" {
		if _, file, line, ok := runtime.Caller(skip); ok {
			n.File, n.Line = shortGoPath(file), line
		}
	}
	for _, opt := range opts {
		opt(&n)
	}
	t := &r.Names
	if t.Entries == nil {
		t.Entries = map[string]*Named{}
	}
	if old, ok := t.Entries[n.Name]; ok {
		n.Rows = old.Rows
	} else {
		t.Order = append(t.Order, n.Name)
	}
	t.Entries[n.Name] = &n
}

// funcAt is where function value v is written, "" when v is not one.
func funcAt(v any) (string, int) {
	fv := reflect.ValueOf(v)
	if fv.Kind() != reflect.Func || fv.IsNil() {
		return "", 0
	}
	fn := runtime.FuncForPC(fv.Pointer())
	if fn == nil {
		return "", 0
	}
	file, line := fn.FileLine(fn.Entry())
	return shortGoPath(file), line
}

// shortGoPath is a source file's path as -trimpath writes it, a
// dependency's module@version/ prefix dropped (ofam.dev/ochazuke/cli/x.go).
func shortGoPath(file string) string {
	file = filepath.ToSlash(file)
	if at := strings.Index(file, "@"); at >= 0 {
		if slash := strings.Index(file[at:], "/"); slash >= 0 {
			return file[:at] + file[at+slash:]
		}
	}
	return file
}

// Named is the entry name has, of kind (any kind when "").
func (r *Runtime) Named(kind, name string) (*Named, bool) {
	n, ok := r.Names.Entries[name]
	if !ok || kind != "" && n.Kind != kind {
		return nil, false
	}
	return n, true
}

// NamedAll are the table's entries by name.
func (r *Runtime) NamedAll() []Named {
	out := make([]Named, 0, len(r.Names.Order))
	for _, name := range slices.Sorted(maps.Keys(r.Names.Entries)) {
		out = append(out, *r.Names.Entries[name])
	}
	return out
}

// UseNamed records that row (a verb key, NOUN.store, NOUN.hooks.NAME)
// cites the entry, which must exist.
func (r *Runtime) UseNamed(name, row string) {
	if n, ok := r.Names.Entries[name]; ok && !slices.Contains(n.Rows, row) {
		n.Rows = append(n.Rows, row)
	}
}

// AddDeclProblem records a problem a declaration loader found.
func (r *Runtime) AddDeclProblem(p DeclProblem) {
	r.Names.Problems = append(r.Names.Problems, p)
}

// DeclProblems are the problems the declaration loaders reported.
func (r *Runtime) DeclProblems() []DeclProblem { return slices.Clone(r.Names.Problems) }

// TypedHandler is fn as an AppHandler that decodes the request's arguments
// into a P (Ctx.Decode) first, with the parameters P declares (ParamsOf)
// and P's type name. An argument of the wrong type is a needs / value
// outcome for it.
func TypedHandler[P any](fn func(Ctx, P) Result) (AppHandler, map[string]Parameter, string) {
	var zero P
	t := reflect.TypeOf(zero)
	h := func(c Ctx) Result {
		var p P
		if err := c.Decode(&p); err != nil {
			// A value of the wrong type is asked for again, never refused.
			param, reason := "", err.Error()
			var typed *json.UnmarshalTypeError
			if errors.As(err, &typed) {
				param, reason = typed.Field, "want "+jsonTypeOf(typed.Type)
			}
			return Failed(c.Need(param, reason))
		}
		return fn(c, p)
	}
	return h, ParamsOf(t), t.String()
}

// Decode fills p, a pointer to a parameter struct, from the request's
// arguments: each exported field from the argument its json tag names (a
// field's name lowercased without a tag); an argument the struct has no
// field for is left out. It is the one place a handler reads constant
// argument keys (spine S3).
func (c Ctx) Decode(p any) error {
	data, err := json.Marshal(c.Args)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, p)
}

// ParamsOf are the parameters a struct type declares, one per exported
// field: the name its json tag gives, the type from the field's Go type
// (string, boolean, integer, number, array, object), Required from the tag
// `param:"required"` and the description from `desc:"..."`.
func ParamsOf(t reflect.Type) map[string]Parameter {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	out := map[string]Parameter{}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		out[name] = Parameter{Type: jsonTypeOf(f.Type), Required: f.Tag.Get("param") == "required", Description: f.Tag.Get("desc")}
	}
	return out
}

// jsonTypeOf is the parameter type a Go type decodes from.
func jsonTypeOf(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	}
	return "object"
}

// SameParams says two parameter declarations agree: the same names, each
// with the same type and required flag (descriptions may differ).
func SameParams(a, b map[string]Parameter) bool {
	if len(a) != len(b) {
		return false
	}
	for name, p := range a {
		q, ok := b[name]
		if !ok || p.Type != q.Type || p.Required != q.Required {
			return false
		}
	}
	return true
}

// handlerFact is one ochazuke.handler thing.
type handlerFact struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	At        string   `json:"at"`
	ParamType string   `json:"paramType"`
	Rows      []string `json:"rows"`
}

// registerHandlers declares the name table's noun.
func registerHandlers(r *Runtime) {
	r.defineFacts(factsSpec{Noun: "ochazuke.handler", StableKeys: true, Sample: handlerFact{}, Key: "name", Label: "name",
		Docs: map[string]string{"name": "The name a declaration cites (a verb row's handler, a store's source, a hook)", "kind": "handler, hook or source",
			"at": "The Go file and line that registered it", "paramType": "The parameter type its handler decodes (empty: it reads its arguments itself)",
			"rows": "The declaration rows that cite it (verb keys, NOUN.store, NOUN.hooks.NAME)"},
		List: func(c Ctx, _ ListQuery) ([]Thing, error) {
			var rows []handlerFact
			for _, n := range c.Runtime.NamedAll() {
				rows = append(rows, handlerFact{Name: n.Name, Kind: n.Kind, At: n.At(), ParamType: n.ParamType, Rows: slices.Clone(n.Rows)})
			}
			return factThings(rows, nil)
		}})
}
