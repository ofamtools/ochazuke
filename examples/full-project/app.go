package main

import (
	"fmt"
	"strings"

	"ofam.dev/ochazuke"
)

// Build composes the framework without coupling its runtime to the file format.
func Build(cfg Config) (*ochazuke.Model, error) {
	r := ochazuke.NewRuntime()
	workspaceState = map[string]map[string]any{}
	reservedIDs := map[string]bool{"app": true, "palette": true, "picker": true}
	reservedNouns := map[string]bool{"editor": true, "prompt": true, "modal": true, "list": true, "palette": true, "picker": true, "yank-history": true}
	for _, spec := range cfg.Instances {
		if strings.TrimSpace(spec.Noun) == "" || reservedIDs[spec.ID] || reservedNouns[spec.Noun] {
			return nil, fmt.Errorf("instance %q / noun %q is empty or reserved", spec.ID, spec.Noun)
		}
		if spec.ID == "" {
			r.Define(spec.Noun).Verbs(spec.Verbs...)
			continue
		}
		if r.Instances[spec.ID] != nil {
			return nil, fmt.Errorf("instance %q is duplicate", spec.ID)
		}
		opts := []ochazuke.InstanceOption{ochazuke.Kind(spec.Noun)}
		if spec.Parent != "" {
			if r.Instances[spec.Parent] == nil {
				return nil, fmt.Errorf("instance %q: unknown parent %q", spec.ID, spec.Parent)
			}
			opts = append(opts, ochazuke.Parent(spec.Parent))
		}
		r.Instance(spec.ID, opts...)
		r.Define(spec.Noun).Verbs(spec.Verbs...)
	}
	for _, name := range cfg.App.Modes {
		if _, err := r.Execute(ochazuke.Request{Verb: "register", Noun: "mode", Args: ochazuke.Arguments{"name": name}}); err != nil {
			return nil, err
		}
	}
	for _, spec := range cfg.Inputs {
		if spec.ID == "picker-path" || strings.HasPrefix(spec.ID, "__palette/") {
			return nil, fmt.Errorf("input %q is reserved", spec.ID)
		}
		input := ochazuke.NewInput(spec.ID, spec.Value, spec.Multiline)
		input.Kind, input.PathFilter = spec.Kind, spec.PathFilter
		opts := []ochazuke.InstanceOption{}
		if spec.Parent != "" {
			opts = append(opts, ochazuke.Parent(spec.Parent))
		}
		if spec.Noun != "" {
			opts = append(opts, ochazuke.Kind(spec.Noun))
		}
		if err := r.AddInput(input, opts...); err != nil {
			return nil, fmt.Errorf("input %q: %w", spec.ID, err)
		}
	}
	m := ochazuke.NewModel(r)
	m.Title = cfg.App.Title
	m.Sidebar = true
	picker := ochazuke.NewPicker(r)
	m.ModalViews["picker"] = ochazuke.ModalView{Title: "Choose a path", Render: func(_ *ochazuke.Runtime, _ *ochazuke.Instance, w, h int) ochazuke.Block {
		return picker.ViewBlock(w, h)
	}}
	appearance := ochazuke.Arguments{}
	if cfg.App.Appearance.Theme != "" {
		appearance["theme"] = cfg.App.Appearance.Theme
	}
	if cfg.App.Appearance.Density != "" {
		appearance["density"] = cfg.App.Appearance.Density
	}
	if _, err := r.Execute(ochazuke.Request{Verb: "set", Noun: "appearance", Args: appearance}); err != nil {
		return nil, err
	}
	if err := registerPages(m, cfg.Pages); err != nil {
		return nil, err
	}
	if err := registerModals(m, cfg.Modals); err != nil {
		return nil, err
	}
	if err := registerActions(r, cfg); err != nil {
		return nil, err
	}
	if err := registerPalette(m, cfg.App.Palette); err != nil {
		return nil, err
	}
	if err := registerBindings(r, cfg); err != nil {
		return nil, err
	}
	if err := r.OpenPage(cfg.App.StartPage); err != nil {
		return nil, err
	}
	return m, nil
}

// Sources stay Go functions; JSON only selects a source and its explicit action.
// A source is built for the configured noun: "rows" lists the list rows that
// name that noun, so a palette noun can be a kind of row.
func registerPalette(m *ochazuke.Model, configs []PaletteConfig) error {
	sources := map[string]func(noun string) ochazuke.PaletteSource{
		"pages": func(string) ochazuke.PaletteSource { return ochazuke.PagesPaletteSource },
		"inputs": func(string) ochazuke.PaletteSource {
			return func(r *ochazuke.Runtime, origin ochazuke.Context) []ochazuke.PaletteInstance {
				var instances []ochazuke.PaletteInstance
				for _, in := range r.Inputs() {
					id := in.ID
					path := r.InstancePath(id)
					if len(origin.Path) > 1 && (len(path) < 2 || path[1] != origin.Path[1]) {
						continue
					}
					if !strings.HasPrefix(id, "__palette/") && id != "picker-path" {
						instances = append(instances, ochazuke.PaletteInstance{ID: id, Label: id})
					}
				}
				return instances
			}
		},
		"rows": func(noun string) ochazuke.PaletteSource {
			return func(r *ochazuke.Runtime, _ ochazuke.Context) []ochazuke.PaletteInstance {
				var instances []ochazuke.PaletteInstance
				for _, list := range r.Lists() {
					for _, item := range list.Snapshot()["items"].([]ochazuke.ListItem) {
						if item.Noun == noun {
							instances = append(instances, ochazuke.PaletteInstance{ID: item.ID, Label: item.Label})
						}
					}
				}
				return instances
			}
		},
	}
	seen := map[string]bool{}
	for _, config := range configs {
		source, ok := sources[config.Source]
		if !ok {
			return fmt.Errorf("app.json palette noun %q: unknown source %q", config.Noun, config.Source)
		}
		if seen[config.Noun] {
			return fmt.Errorf("app.json palette: duplicate noun %q", config.Noun)
		}
		seen[config.Noun] = true
		if err := m.Palette.RegisterNoun(config.Noun, ochazuke.PaletteNoun{Source: source(config.Noun), DefaultVerb: config.DefaultVerb, Argument: config.Argument}); err != nil {
			return fmt.Errorf("app.json: %w", err)
		}
	}
	return nil
}
