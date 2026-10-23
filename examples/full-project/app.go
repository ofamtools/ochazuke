package main

import (
	"fmt"
	"strings"

	"ochazuke"
)

// Build composes the framework without coupling its runtime to the file format.
func Build(cfg Config) (*ochazuke.Model, error) {
	r := ochazuke.NewRuntime()
	reserved := map[string]bool{"palette": true, "picker": true, "yank-history": true}
	for _, spec := range cfg.Layers {
		if strings.TrimSpace(spec.ID) == "" || r.Layers[spec.ID] != nil || reserved[spec.ID] {
			return nil, fmt.Errorf("layer %q is empty, duplicate, or reserved", spec.ID)
		}
		r.Layers[spec.ID] = &ochazuke.Layer{ID: spec.ID, Noun: spec.Noun, Actions: map[string]ochazuke.Handler{}}
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
		if err := r.AddInput(input, spec.Path); err != nil {
			return nil, fmt.Errorf("input %q: %w", spec.ID, err)
		}
	}
	m := ochazuke.NewModel(r)
	m.Title = cfg.App.Title
	picker := ochazuke.NewPicker(r)
	m.Pickers = append(m.Pickers, picker)
	m.ModalViews["picker"] = ochazuke.ModalView{Title: "Choose a path", Render: func(_ *ochazuke.Runtime, _ *ochazuke.Frame, w, h int) string { return picker.View(w, h) }}
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
	if err := registerBindings(r, cfg); err != nil {
		return nil, err
	}
	if err := r.OpenPage(cfg.App.StartPage); err != nil {
		return nil, err
	}
	return m, nil
}
