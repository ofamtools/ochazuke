package main

import (
	"fmt"
	"slices"
	"strings"

	"ochazuke"
)

// Handler names are application code; operation names and schemas live in config.
func handlerRegistry(cfg Config) map[string]ochazuke.Handler {
	itemsByPage := make(map[string][]ochazuke.ListItem)
	for _, page := range cfg.Pages {
		if page.Renderer == "list" {
			itemsByPage[page.ID] = slices.Clone(page.Items)
		}
	}
	save := func(label string) ochazuke.Handler {
		return func(r *ochazuke.Runtime, c ochazuke.Context, _ ochazuke.Arguments) (any, error) {
			values := map[string]string{}
			for _, id := range []string{"title", "notes"} {
				if input := r.Inputs[id]; input != nil {
					values[id] = string(input.Text)
				}
			}
			frame := r.Frames[len(r.Frames)-1]
			frame.State["saved"] = values
			frame.State["lastAction"] = label
			return label, nil
		}
	}
	return map[string]ochazuke.Handler{
		"save":        save("workspace saved"),
		"save-editor": save("editor saved"),
		"refresh-items": func(r *ochazuke.Runtime, _ ochazuke.Context, args ochazuke.Arguments) (any, error) {
			for _, page := range cfg.Pages {
				if page.ID != r.ActivePage || page.Renderer != "list" {
					continue
				}
				list := r.Lists[page.Context.Path[len(page.Context.Path)-1]]
				rows := slices.Clone(itemsByPage[page.ID])
				if remove, _ := args["remove"].(bool); remove {
					selected, ok := list.Selected()
					if ok {
						for i, row := range rows {
							if row.ID == selected.ID {
								rows = slices.Delete(rows, i, i+1)
								break
							}
						}
					}
				} else if len(rows) > 1 {
					first := rows[0]
					copy(rows, rows[1:])
					rows[len(rows)-1] = first
				}
				if err := list.SetItems(rows); err != nil {
					return nil, err
				}
				itemsByPage[page.ID] = rows
				return list.Snapshot(), nil
			}
			return nil, fmt.Errorf("open a list page before refreshing items")
		},
		"cycle-input": func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
			if len(r.Frames) != 1 {
				return nil, fmt.Errorf("close modals before changing page fields")
			}
			for _, page := range cfg.Pages {
				if page.ID != r.ActivePage {
					continue
				}
				if len(page.Inputs) == 0 {
					return nil, fmt.Errorf("page %q has no inputs", page.ID)
				}
				index := 0
				for i, id := range page.Inputs {
					if id == c.InputID {
						index = i
						break
					}
				}
				delta := ochazuke.IntArg(a, "delta", 1) % len(page.Inputs)
				index = (index + delta + len(page.Inputs)) % len(page.Inputs)
				err := r.Focus(page.Inputs[index])
				return r.Context(), err
			}
			return nil, fmt.Errorf("no active configured page")
		},
		"summarize": func(r *ochazuke.Runtime, _ ochazuke.Context, _ ochazuke.Arguments) (any, error) {
			notes := r.Inputs["notes"]
			if notes == nil {
				return nil, fmt.Errorf("summarize requires the notes input")
			}
			result := fmt.Sprintf("%d words", len(strings.Fields(string(notes.Text))))
			r.Frames[len(r.Frames)-1].State["summary"] = result
			return result, nil
		},
		"inspect": func(r *ochazuke.Runtime, c ochazuke.Context, _ ochazuke.Arguments) (any, error) {
			inspection := map[string]any{"page": r.ActivePage, "path": c.Path, "input": c.InputID, "mode": r.Mode(c)}
			r.Frames[len(r.Frames)-1].State["inspection"] = inspection
			return inspection, nil
		},
		"greet": func(r *ochazuke.Runtime, _ ochazuke.Context, a ochazuke.Arguments) (any, error) {
			count := ochazuke.IntArg(a, "repeat", 1)
			if count < 1 || count > 20 {
				return nil, fmt.Errorf("repeat must be between 1 and 20")
			}
			greeting := "Hello, " + ochazuke.StringArg(a, "name", "") + "!"
			result := strings.TrimSuffix(strings.Repeat(greeting+" ", count), " ")
			r.Frames[len(r.Frames)-1].State["greeting"] = result
			return result, nil
		},
		"open-modal": func(r *ochazuke.Runtime, _ ochazuke.Context, a ochazuke.Arguments) (any, error) {
			id := ochazuke.StringArg(a, "id", "")
			for _, spec := range cfg.Modals {
				if spec.ID != id {
					continue
				}
				context := ochazuke.Context{Path: []string{"app"}}
				if spec.Input != "" {
					context = ochazuke.Context{Path: r.InputPaths[spec.Input], InputID: spec.Input}
				}
				err := r.Push(&ochazuke.Frame{ID: "modal:" + id, Context: context, State: map[string]any{}})
				return r.Context(), err
			}
			return nil, fmt.Errorf("unknown configured modal %q", id)
		},
	}
}
func registerActions(r *ochazuke.Runtime, cfg Config) error {
	handlers := handlerRegistry(cfg)
	requiredInputs := map[string][]string{"save": {"title", "notes"}, "save-editor": {"title", "notes"}, "summarize": {"notes"}}
	for _, spec := range cfg.Actions {
		if handlers[spec.Handler] == nil {
			return fmt.Errorf("action %s.%s: unknown handler %q", spec.Verb, spec.Noun, spec.Handler)
		}
		if strings.TrimSpace(spec.Verb) == "" || strings.TrimSpace(spec.Noun) == "" {
			return fmt.Errorf("action verb and noun are required")
		}
		for _, id := range requiredInputs[spec.Handler] {
			if r.Inputs[id] == nil {
				return fmt.Errorf("handler %q requires input %q", spec.Handler, id)
			}
		}
		for name, parameter := range spec.Parameters {
			if name == "" || (parameter.Type != "string" && parameter.Type != "integer" && parameter.Type != "boolean") {
				return fmt.Errorf("action %s.%s: invalid parameter %q type %q", spec.Verb, spec.Noun, name, parameter.Type)
			}
		}
		if spec.Layer != "" {
			continue
		}
		operation := ochazuke.Operation{Verb: spec.Verb, Noun: spec.Noun, Description: spec.Description, Parameters: spec.Parameters, Result: spec.Result, Run: handlers[spec.Handler]}
		if _, exists := r.Operations[operation.Key()]; exists {
			return fmt.Errorf("duplicate operation %q", operation.Key())
		}
		r.Register(operation)
	}
	for _, spec := range cfg.Actions {
		if spec.Layer == "" {
			continue
		}
		key := spec.Verb + "." + spec.Noun
		layer := r.Layers[spec.Layer]
		if layer == nil {
			return fmt.Errorf("action %s: unknown layer %q", key, spec.Layer)
		}
		if _, exists := r.Operations[key]; !exists {
			return fmt.Errorf("layer action %s has no registered operation", key)
		}
		if layer.Actions[key] != nil {
			return fmt.Errorf("duplicate layer action %s in %q", key, spec.Layer)
		}
		layer.Actions[key] = handlers[spec.Handler]
	}
	return nil
}
