package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func registerConfiguration(r *Runtime) {
	r.Register(Operation{Verb: "register", Noun: "mode", Description: "Register an application-defined mode name", Parameters: map[string]Parameter{"name": {Type: "string", Required: true}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		name := StringArg(a, "name", "")
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("mode name cannot be empty")
		}
		r.Modes[name] = true
		return name, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "modes", Description: "List registered interaction modes", Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		names := make([]string, 0, len(r.Modes))
		for name := range r.Modes {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}})
	r.Register(Operation{Verb: "set", Noun: "binding", Description: "Override a key sequence in a layer and optional mode", Parameters: map[string]Parameter{
		"layer": {Type: "string", Required: true}, "keys": {Type: "string", Required: true}, "mode": {Type: "string"}, "request": {Type: "string", Required: true, Description: "JSON request object: verb, noun, optional args and target"},
	}, Result: "binding", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := StringArg(a, "layer", "")
		layer := r.Layers[id]
		if layer == nil {
			return nil, fmt.Errorf("unknown layer %q", id)
		}
		mode := StringArg(a, "mode", "")
		if mode != "" && !r.Modes[mode] {
			return nil, fmt.Errorf("unknown mode %q", mode)
		}
		keys := strings.TrimSpace(StringArg(a, "keys", ""))
		if keys == "" {
			return nil, errors.New("keys cannot be empty")
		}
		var q Request
		if err := decodeRequest([]byte(StringArg(a, "request", "")), &q); err != nil {
			return nil, err
		}
		op, ok := r.Operations[q.Key()]
		if !ok {
			return nil, fmt.Errorf("unknown operation %q", q.Key())
		}
		if err := validateArguments(op, q.Args); err != nil {
			return nil, err
		}
		binding := Binding{Mode: mode, Keys: keys, Request: q}
		r.CancelPendingBinding()
		for i, b := range layer.Bindings {
			if b.Mode == mode && b.Keys == keys {
				layer.Bindings[i] = binding
				return binding, nil
			}
		}
		layer.Bindings = append(layer.Bindings, binding)
		return binding, nil
	}})
	r.Register(Operation{Verb: "select", Noun: "context", Description: "Focus an instance by id, or a nested path; omit input to leave text editing", Parameters: map[string]Parameter{"instance": {Type: "string", Description: "Instance ID: an input, list, row, page or modal body"}, "path": {Type: "string", Description: "JSON array of layer IDs, outermost first, beginning with app (when instance is omitted)"}, "input": {Type: "string"}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if id := StringArg(a, "instance", ""); id != "" {
			if err := r.Focus(id); err != nil {
				return nil, err
			}
			return r.Context(), nil
		}
		if _, ok := a["path"]; !ok {
			return nil, errors.New("instance or path is required")
		}
		var path []string
		if err := json.Unmarshal([]byte(StringArg(a, "path", "")), &path); err != nil {
			return nil, err
		}
		next := Context{Path: path, InputID: StringArg(a, "input", "")}
		if err := r.SelectContext(next); err != nil {
			return nil, err
		}
		return next, nil
	}})
}
