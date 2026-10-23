package main

import (
	"encoding/json"
	"fmt"

	"ochazuke"
)

func registerBindings(r *ochazuke.Runtime, cfg Config) error {
	seen := map[string]bool{}
	for _, spec := range cfg.Bindings {
		key := spec.Layer + "\x00" + spec.Mode + "\x00" + spec.Keys
		if seen[key] {
			return fmt.Errorf("duplicate binding %q in layer %q mode %q", spec.Keys, spec.Layer, spec.Mode)
		}
		seen[key] = true
		q := spec.Request
		if q.Target != "" && q.Target != "focused-input" && r.Inputs[q.Target] == nil {
			return fmt.Errorf("binding %q: unknown target %q", spec.Keys, q.Target)
		}
		id := ochazuke.StringArg(q.Args, "id", "")
		switch q.Key() {
		case "focus.input":
			if r.Inputs[id] == nil {
				return fmt.Errorf("binding %q: unknown input %q", spec.Keys, id)
			}
		case "open.page":
			if r.Pages[id] == nil {
				return fmt.Errorf("binding %q: unknown page %q", spec.Keys, id)
			}
		case "push.modal":
			input := ochazuke.StringArg(q.Args, "input", "")
			if r.Inputs[input] == nil {
				return fmt.Errorf("binding %q: unknown modal input %q", spec.Keys, input)
			}
		case "set.mode":
			mode := ochazuke.StringArg(q.Args, "mode", "")
			if !r.Modes[mode] {
				return fmt.Errorf("binding %q: unknown mode %q", spec.Keys, mode)
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
				return fmt.Errorf("binding %q: unknown configured modal %q", spec.Keys, id)
			}
		}
		data, err := json.Marshal(q)
		if err != nil {
			return err
		}
		_, err = r.Execute(ochazuke.Request{Verb: "set", Noun: "binding", Args: ochazuke.Arguments{"layer": spec.Layer, "mode": spec.Mode, "keys": spec.Keys, "request": string(data)}})
		if err != nil {
			return fmt.Errorf("binding %q: %w", spec.Keys, err)
		}
	}
	return nil
}
