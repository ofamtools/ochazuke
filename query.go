package ochazuke

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Predicate is one named test over an instance: a value, never a string
// selector. A query is a list of predicates, ANDed. Name is a registered
// predicate (noun, id, kind, within, child-of, focusable, region, state or
// one an application adds) and Arg its argument; the combinators any, all
// and not take nested predicate lists in Of instead. The wire form is a JSON
// array: ["noun","row"], ["any",[["noun","a"],["noun","b"]]].
type Predicate struct {
	Name string      `json:"name"`
	Arg  string      `json:"arg,omitempty"`
	Of   []Predicate `json:"of,omitempty"`
}

// PredicateFunc tests one instance; arg is the predicate's argument.
type PredicateFunc func(inst *Instance, r *Runtime, arg string) bool

// Where builds a predicate by name.
func Where(name, arg string) Predicate { return Predicate{Name: name, Arg: arg} }

// Go spellings of the framework predicates and combinators.
func ByNoun(noun string) Predicate            { return Where("noun", noun) }
func ByID(id string) Predicate                { return Where("id", id) }
func ByKind(kind string) Predicate            { return Where("kind", kind) }
func Within(id string) Predicate              { return Where("within", id) }
func ChildOf(id string) Predicate             { return Where("child-of", id) }
func IsFocusable() Predicate                  { return Where("focusable", "") }
func IsRegion() Predicate                     { return Where("region", "") }
func InState(mode string) Predicate           { return Where("state", mode) }
func AnyOf(of ...Predicate) Predicate         { return Predicate{Name: "any", Of: of} }
func AllOf(of ...Predicate) Predicate         { return Predicate{Name: "all", Of: of} }
func Not(of ...Predicate) Predicate           { return Predicate{Name: "not", Of: of} }
func isCombinator(name string) bool           { return name == "any" || name == "all" || name == "not" }
func (r *Runtime) hasPredicate(n string) bool { _, ok := r.predicates[n]; return ok || isCombinator(n) }

// builtinPredicates are the framework predicates. kind is the component an
// instance fronts, lowercased ("input", "list", "page", "terminal", ...),
// "row" for a list row, "modal" for a modal body, else "instance".
func builtinPredicates() map[string]PredicateFunc {
	return map[string]PredicateFunc{
		"noun": func(inst *Instance, _ *Runtime, arg string) bool { return inst.Noun == arg },
		"id":   func(inst *Instance, _ *Runtime, arg string) bool { return inst.ID == arg },
		"kind": func(inst *Instance, r *Runtime, arg string) bool { return r.kindOf(inst) == arg },
		"within": func(inst *Instance, r *Runtime, arg string) bool {
			return inst.ID != arg && slices.Contains(r.Ancestry(inst.ID), arg)
		},
		"child-of":  func(inst *Instance, _ *Runtime, arg string) bool { return inst.Parent == arg },
		"focusable": func(inst *Instance, r *Runtime, _ string) bool { return r.focusable(inst.ID) },
		"region": func(inst *Instance, r *Runtime, _ string) bool {
			noun := r.Nouns[inst.Noun]
			return noun != nil && noun.Region
		},
		"state": func(_ *Instance, r *Runtime, arg string) bool { return r.Mode.Name == arg || r.BaseMode() == arg },
	}
}

// kindOf names the component an instance fronts.
func (r *Runtime) kindOf(inst *Instance) string {
	if _, order := inst.Component.(*appPages); inst.Component != nil && !order {
		t := reflect.TypeOf(inst.Component)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		return strings.ToLower(t.Name())
	}
	switch {
	case r.rows[inst.ID] != "":
		return "row"
	case r.modalInstances[inst.ID]:
		return "modal"
	}
	return "instance"
}

// RegisterPredicate adds a named predicate an application's queries (and
// the CLI's instances.query) can use, such as marked.
func (r *Runtime) RegisterPredicate(name string, fn PredicateFunc) {
	if name == "" || fn == nil || isCombinator(name) {
		panic("invalid predicate registration: " + name)
	}
	r.predicates[name] = fn
}

// Query returns the instances matching every predicate, in tree order: app
// first, then depth-first with siblings in the order they were added.
// An unknown predicate matches nothing; instances.query reports it instead.
func (r *Runtime) Query(preds ...Predicate) []*Instance {
	children := map[string][]*Instance{}
	for _, inst := range r.instancesInOrder() {
		if inst.ID != "app" {
			children[inst.Parent] = append(children[inst.Parent], inst)
		}
	}
	var out []*Instance
	var visit func(inst *Instance)
	visit = func(inst *Instance) {
		if r.matchAll(inst, preds) {
			out = append(out, inst)
		}
		for _, child := range children[inst.ID] {
			visit(child)
		}
	}
	if app := r.Instances["app"]; app != nil {
		visit(app)
	}
	return out
}

func (r *Runtime) checkPredicates(preds []Predicate) error {
	for _, p := range preds {
		if !r.hasPredicate(p.Name) {
			return fmt.Errorf("unknown predicate %q", p.Name)
		}
		if isCombinator(p.Name) {
			if len(p.Of) == 0 {
				return fmt.Errorf("%s needs a predicate list", p.Name)
			}
			if err := r.checkPredicates(p.Of); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Runtime) matchAll(inst *Instance, preds []Predicate) bool {
	for _, p := range preds {
		if !r.match(inst, p) {
			return false
		}
	}
	return true
}

func (r *Runtime) match(inst *Instance, p Predicate) bool {
	switch p.Name {
	case "all":
		return r.matchAll(inst, p.Of)
	case "not":
		return !r.matchAll(inst, p.Of)
	case "any":
		for _, one := range p.Of {
			if r.match(inst, one) {
				return true
			}
		}
		return false
	}
	fn := r.predicates[p.Name]
	return fn != nil && fn(inst, r, p.Arg)
}

// ParsePredicates reads the wire form: a list of [name, arg] pairs, or
// [combinator, [nested pairs]] for any, all and not. A []Predicate from Go
// passes through.
func ParsePredicates(v any) ([]Predicate, error) {
	switch list := v.(type) {
	case nil:
		return nil, nil
	case []Predicate:
		return list, nil
	case []any:
		out := make([]Predicate, 0, len(list))
		for _, item := range list {
			p, err := parsePredicate(item)
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, nil
	}
	return nil, errors.New("where must be an array of predicates")
}

func parsePredicate(v any) (Predicate, error) {
	if p, ok := v.(Predicate); ok {
		return p, nil
	}
	pair, ok := v.([]any)
	if !ok || len(pair) == 0 || len(pair) > 2 {
		return Predicate{}, fmt.Errorf("predicate must be [name, arg]: %v", v)
	}
	name, ok := pair[0].(string)
	if !ok {
		return Predicate{}, fmt.Errorf("predicate name must be a string: %v", pair[0])
	}
	p := Predicate{Name: name}
	if len(pair) == 1 {
		return p, nil
	}
	if isCombinator(name) {
		of, err := ParsePredicates(pair[1])
		if err != nil {
			return Predicate{}, fmt.Errorf("%s: %w", name, err)
		}
		p.Of = of
		return p, nil
	}
	arg, ok := pair[1].(string)
	if !ok {
		return Predicate{}, fmt.Errorf("%s argument must be a string", name)
	}
	p.Arg = arg
	return p, nil
}

// QueryMatch is one instances.query result row.
type QueryMatch struct {
	ID     string `json:"id"`
	Noun   string `json:"noun"`
	Parent string `json:"parent,omitempty"`
}

func registerQuery(r *Runtime) {
	r.Register(Operation{Verb: "query", Noun: "ochazuke.instances", Description: "List the instances matching every predicate, in tree order: where is [[name, arg], ...] with noun, id, kind, within, child-of, focusable, region, state, registered predicates, and [any|all|not, [...]]", Parameters: map[string]Parameter{"where": {Type: "array", Description: "Predicates, ANDed"}}, Result: "instance[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		preds, err := ParsePredicates(a["where"])
		if err != nil {
			return nil, err
		}
		if err := r.checkPredicates(preds); err != nil {
			return nil, err
		}
		found := r.Query(preds...)
		out := make([]QueryMatch, 0, len(found))
		for _, inst := range found {
			out = append(out, QueryMatch{ID: inst.ID, Noun: inst.Noun, Parent: inst.Parent})
		}
		return out, nil
	}})
}
