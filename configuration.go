package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
)

func registerConfiguration(r *Runtime) {
	r.Register(Operation{Verb: "register", Noun: "mode", Description: "Register an application-defined mode name; pending modes are exclusive and one-shot", Parameters: map[string]Parameter{"name": {Type: "string", Required: true}, "pending": {Type: "boolean"}}, Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		name := StringArg(a, "name", "")
		pending, _ := a["pending"].(bool)
		if err := r.DefineMode(name, pending); err != nil {
			return nil, err
		}
		return name, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "modes", Description: "List registered interaction modes, pending modes included", Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		names := make([]string, 0, len(r.Modes)+len(r.pendingModes))
		for name := range r.Modes {
			names = append(names, name)
		}
		for name := range r.pendingModes {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}})
	r.Register(Operation{Verb: "load", Noun: "bindings", Description: "Install binding rows from a JSON file: [{noun|instance, mode, keys|pattern, request|null, from, when}]", Parameters: map[string]Parameter{"path": {Type: "string", Required: true}}, Result: "binding[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		data, err := os.ReadFile(StringArg(a, "path", ""))
		if err != nil {
			return nil, err
		}
		return r.LoadBindings(data)
	}})
	r.Register(Operation{Verb: "select", Noun: "context", Description: "Focus an instance by id, or a nested path; omit input to leave text editing", Parameters: map[string]Parameter{"instance": {Type: "string", Description: "Instance ID: an input, list, row, page or modal body"}, "path": {Type: "string", Description: "JSON array of instance IDs, outermost first, beginning with app (when instance is omitted)"}, "input": {Type: "string"}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		id := StringArg(a, "instance", "")
		if id == "" {
			if _, ok := a["path"]; !ok {
				return nil, errors.New("instance or path is required")
			}
			var path []string
			if err := json.Unmarshal([]byte(StringArg(a, "path", "")), &path); err != nil {
				return nil, err
			}
			if len(path) == 0 || path[0] != "app" {
				return nil, errors.New("interaction path must start at app")
			}
			id = path[len(path)-1]
			if input := StringArg(a, "input", ""); input != "" {
				if r.Input(input) == nil || r.Instances[input].Parent != id {
					return nil, errors.New("input does not belong to this interaction path")
				}
				id = input
			}
			if got := r.InstancePath(id); len(got) == 0 || !samePath(got, path, r.Input(id) != nil) {
				return nil, fmt.Errorf("invalid path for %q", id)
			}
		}
		if err := r.Focus(id); err != nil {
			return nil, err
		}
		return r.Context(), nil
	}})
}

// samePath checks a declared path against an instance path, allowing the
// input itself to be omitted from the declared path.
func samePath(got, want []string, input bool) bool {
	if input {
		got = got[:len(got)-1]
	}
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// LoadBindings installs rows from a JSON array in the binding row shape and
// returns the installed rows.
func (r *Runtime) LoadBindings(data []byte) ([]Binding, error) {
	var rows []Binding
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("bindings: %w", err)
	}
	for i, row := range rows {
		if err := r.Bind(row); err != nil {
			return nil, fmt.Errorf("bindings[%d] %s: %w", i, row.Keys+row.Pattern, err)
		}
	}
	return rows, nil
}
