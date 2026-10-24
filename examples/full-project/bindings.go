package main

import (
	"fmt"

	"ochazuke"
)

// registerBindings installs the configured rows. The file is the framework's
// row shape; validation beyond the runtime's own covers the example's
// handlers (a configured modal must exist for open-modal rows).
func registerBindings(r *ochazuke.Runtime, cfg Config) error {
	seen := map[string]bool{}
	for _, row := range cfg.Bindings {
		key := row.Noun + "\x00" + row.Instance + "\x00" + row.Mode + "\x00" + row.Keys + row.Pattern
		if seen[key] {
			return fmt.Errorf("duplicate binding %q on %s%s mode %q", row.Keys+row.Pattern, row.Noun, row.Instance, row.Mode)
		}
		seen[key] = true
		q := row.Request
		if q.Target != "" && q.Target != "focused-input" && len(r.Ancestry(q.Target)) == 0 {
			return fmt.Errorf("binding %q: unknown target %q", row.Keys, q.Target)
		}
		id := ochazuke.StringArg(q.Args, "id", "")
		switch q.Key() {
		case "input.focus":
			if r.Input(id) == nil {
				return fmt.Errorf("binding %q: unknown input %q", row.Keys, id)
			}
		case "page.open":
			if r.PageOf(id) == nil {
				return fmt.Errorf("binding %q: unknown page %q", row.Keys, id)
			}
		case "modal.push":
			input := ochazuke.StringArg(q.Args, "input", "")
			if r.Input(input) == nil {
				return fmt.Errorf("binding %q: unknown modal input %q", row.Keys, input)
			}
		case "mode.set":
			mode := ochazuke.StringArg(q.Args, "mode", "")
			if !r.Modes[mode] {
				return fmt.Errorf("binding %q: unknown mode %q", row.Keys, mode)
			}
		}
		for _, action := range cfg.Actions {
			if action.Verb != q.Verb || action.Noun != q.Noun || action.Handler != "open-modal" {
				continue
			}
			found := false
			for _, modal := range cfg.Modals {
				if modal.ID == id {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("binding %q: unknown configured modal %q", row.Keys, id)
			}
		}
		if err := r.Bind(row); err != nil {
			return fmt.Errorf("binding %q: %w", row.Keys+row.Pattern, err)
		}
	}
	return nil
}
