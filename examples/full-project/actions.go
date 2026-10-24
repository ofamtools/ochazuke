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
				if input := r.Input(id); input != nil {
					values[id] = string(input.Text)
				}
			}
			state := pageState(r.Page)
			state["saved"] = values
			state["lastAction"] = label
			return label, nil
		}
	}
	// activeList is the list page that is open, or an error naming the need.
	activeList := func(r *ochazuke.Runtime) (PageConfig, *ochazuke.List, error) {
		for _, page := range cfg.Pages {
			if page.ID == r.Page && page.Renderer == "list" {
				return page, r.List(page.Focus), nil
			}
		}
		return PageConfig{}, nil, fmt.Errorf("open a list page first")
	}
	// removeRow drops one row from the page's sample data and the live list.
	removeRow := func(r *ochazuke.Runtime, page PageConfig, list *ochazuke.List, id string) error {
		rows := slices.Clone(itemsByPage[page.ID])
		for i, row := range rows {
			if row.ID == id {
				rows = slices.Delete(rows, i, i+1)
				break
			}
		}
		if err := list.SetItems(rows); err != nil {
			return err
		}
		itemsByPage[page.ID] = rows
		return nil
	}
	// rowAction is a verb of one row noun. The row is named by args ("row"),
	// else by the context (a request targeted at a row instance, or a key
	// pressed on the focused row), else it is the selected row. A row of
	// another noun is refused. The result is recorded in the page frame, as
	// the other handlers do.
	rowAction := func(verb, noun string, apply func(*ochazuke.Runtime, PageConfig, *ochazuke.List, ochazuke.ListItem) error) ochazuke.Handler {
		return func(r *ochazuke.Runtime, c ochazuke.Context, args ochazuke.Arguments) (any, error) {
			page, list, err := activeList(r)
			if err != nil {
				return nil, err
			}
			listID := page.Focus
			id := ochazuke.StringArg(args, "row", "")
			if leaf := c.Path[len(c.Path)-1]; id == "" && c.InputID == "" && r.Instances[leaf] != nil && r.Instances[leaf].Parent == listID {
				id = leaf
			}
			row, ok := list.Selected()
			if id != "" {
				ok = false
				for _, item := range list.Snapshot()["items"].([]ochazuke.ListItem) {
					if item.ID == id {
						row, ok = item, true
					}
				}
			}
			if !ok {
				return nil, fmt.Errorf("%s.%s: no such row", noun, verb)
			}
			if row.Noun != noun {
				return nil, fmt.Errorf("%s.%s: row %q is a %s", noun, verb, row.ID, row.Noun)
			}
			if apply != nil {
				if err := apply(r, page, list, row); err != nil {
					return nil, err
				}
			}
			result := map[string]any{"verb": verb, "noun": noun, "row": row.ID}
			pageState(page.ID)["lastAction"] = verb + " " + row.ID
			return result, nil
		}
	}
	return map[string]ochazuke.Handler{
		"save":        save("workspace saved"),
		"save-editor": save("editor saved"),
		"refresh-items": func(r *ochazuke.Runtime, _ ochazuke.Context, args ochazuke.Arguments) (any, error) {
			page, list, err := activeList(r)
			if err != nil {
				return nil, fmt.Errorf("open a list page before refreshing items")
			}
			if remove, _ := args["remove"].(bool); remove {
				if selected, ok := list.Selected(); ok {
					if err := removeRow(r, page, list, selected.ID); err != nil {
						return nil, err
					}
				}
				return list.Snapshot(), nil
			}
			rows := slices.Clone(itemsByPage[page.ID])
			if len(rows) > 1 {
				first := rows[0]
				copy(rows, rows[1:])
				rows[len(rows)-1] = first
			}
			if err := list.SetItems(rows); err != nil {
				return nil, err
			}
			itemsByPage[page.ID] = rows
			return list.Snapshot(), nil
		},
		"fetch-row":    rowAction("fetch", "repo", nil),
		"open-row":     rowAction("open", "worktree", nil),
		"checkout-row": rowAction("checkout", "branch", nil),
		"remove-row": rowAction("remove", "worktree", func(r *ochazuke.Runtime, page PageConfig, list *ochazuke.List, row ochazuke.ListItem) error {
			return removeRow(r, page, list, row.ID)
		}),
		"cycle-input": func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
			if len(r.Modals()) != 0 {
				return nil, fmt.Errorf("close modals before changing page fields")
			}
			for _, page := range cfg.Pages {
				if page.ID != r.Page {
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
				err := r.SetFocus(page.Inputs[index])
				return r.Context(), err
			}
			return nil, fmt.Errorf("no active configured page")
		},
		"summarize": func(r *ochazuke.Runtime, _ ochazuke.Context, _ ochazuke.Arguments) (any, error) {
			notes := r.Input("notes")
			if notes == nil {
				return nil, fmt.Errorf("summarize requires the notes input")
			}
			result := fmt.Sprintf("%d words", len(strings.Fields(string(notes.Text))))
			pageState(r.Page)["summary"] = result
			return result, nil
		},
		"inspect": func(r *ochazuke.Runtime, c ochazuke.Context, _ ochazuke.Arguments) (any, error) {
			inspection := map[string]any{"page": r.Page, "path": c.Path, "input": c.InputID, "mode": r.Mode.Name}
			pageState(r.Page)["inspection"] = inspection
			return inspection, nil
		},
		"greet": func(r *ochazuke.Runtime, _ ochazuke.Context, a ochazuke.Arguments) (any, error) {
			count := ochazuke.IntArg(a, "repeat", 1)
			if count < 1 || count > 20 {
				return nil, fmt.Errorf("repeat must be between 1 and 20")
			}
			greeting := "Hello, " + ochazuke.StringArg(a, "name", "") + "!"
			result := strings.TrimSuffix(strings.Repeat(greeting+" ", count), " ")
			pageState(r.Page)["greeting"] = result
			return result, nil
		},
		"open-modal": func(r *ochazuke.Runtime, c ochazuke.Context, a ochazuke.Arguments) (any, error) {
			id := ochazuke.StringArg(a, "id", "")
			for _, spec := range cfg.Modals {
				if spec.ID != id {
					continue
				}
				focus := "app"
				if spec.Input != "" {
					focus = spec.Input
				}
				// The opening focus lets a renderer describe what was focused.
				modalOrigins["modal:"+id] = focusOf(c)
				err := r.Push("modal:"+id, focus)
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
			return fmt.Errorf("action %s.%s: unknown handler %q", spec.Noun, spec.Verb, spec.Handler)
		}
		if strings.TrimSpace(spec.Verb) == "" || strings.TrimSpace(spec.Noun) == "" {
			return fmt.Errorf("action verb and noun are required")
		}
		for _, id := range requiredInputs[spec.Handler] {
			if r.Input(id) == nil {
				return fmt.Errorf("handler %q requires input %q", spec.Handler, id)
			}
		}
		for name, parameter := range spec.Parameters {
			if name == "" || (parameter.Type != "string" && parameter.Type != "integer" && parameter.Type != "boolean") {
				return fmt.Errorf("action %s.%s: invalid parameter %q type %q", spec.Noun, spec.Verb, name, parameter.Type)
			}
		}
		if spec.On != "" {
			continue
		}
		operation := ochazuke.Operation{Verb: spec.Verb, Noun: spec.Noun, Description: spec.Description, Parameters: spec.Parameters, Result: spec.Result, Confirm: spec.Confirm, Run: handlers[spec.Handler]}
		if _, exists := r.Operations[operation.Key()]; exists {
			return fmt.Errorf("duplicate operation %q", operation.Key())
		}
		r.Register(operation)
	}
	for _, spec := range cfg.Actions {
		if spec.On == "" {
			continue
		}
		key := spec.Noun + "." + spec.Verb
		noun := r.Noun(spec.On)
		if noun == nil {
			return fmt.Errorf("action %s: unknown noun %q", key, spec.On)
		}
		if _, exists := r.Operations[key]; !exists {
			return fmt.Errorf("noun action %s has no registered operation", key)
		}
		if noun.Actions[key] != nil {
			return fmt.Errorf("duplicate noun action %s on %q", key, spec.On)
		}
		noun.Actions[key] = handlers[spec.Handler]
	}
	return nil
}

// focusOf is the instance a handler context addresses.
func focusOf(c ochazuke.Context) string {
	if c.InputID != "" {
		return c.InputID
	}
	if len(c.Path) == 0 {
		return ""
	}
	return c.Path[len(c.Path)-1]
}
