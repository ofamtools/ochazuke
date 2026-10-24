package main

import (
	"strings"
	"testing"

	"ochazuke"
)

func TestPaletteConfigurationValidation(t *testing.T) {
	for _, test := range []struct {
		name, noun, source, verb, argument, message string
	}{
		{"unknown source", "page", "missing", "open", "id", "unknown source"},
		{"unknown noun", "missing", "pages", "open", "id", "unknown noun"},
		{"unknown default", "page", "pages", "missing", "id", "unknown default action"},
		{"unknown argument", "page", "pages", "open", "missing", "string instance argument"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := LoadConfig("")
			if err != nil {
				t.Fatal(err)
			}
			cfg.App.Palette = []PaletteConfig{{Noun: test.noun, Source: test.source, DefaultVerb: test.verb, Argument: test.argument}}
			_, err = Build(cfg)
			if err == nil || !strings.Contains(err.Error(), "app.json") || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want app.json diagnostic containing %q", err, test.message)
			}
		})
	}
}

func TestConfiguredPaletteSourcesDispatchRealOperations(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	m, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	call := func(verb, noun string, args ochazuke.Arguments) {
		t.Helper()
		if _, err := m.Runtime.Execute(ochazuke.Request{Verb: verb, Noun: noun, Args: args}); err != nil {
			t.Fatalf("%s.%s: %v", verb, noun, err)
		}
	}
	line := func(text string) error {
		t.Helper()
		call("set", "palette", ochazuke.Arguments{"text": text})
		_, err := m.Runtime.Execute(ochazuke.Request{Verb: "run", Noun: "palette"})
		return err
	}
	call("open", "palette", nil)
	if err := line("page paths"); err != nil {
		t.Fatal(err)
	}
	if m.Runtime.Page != "paths" || m.Palette.IsOpen() {
		t.Fatal("configured page source did not dispatch page.open")
	}
	call("open", "palette", nil)
	if err := line("input title"); err == nil {
		t.Fatal("input source exposed an input from an inactive page")
	}
	if err := line("input destination"); err != nil {
		t.Fatal(err)
	}
	if m.Runtime.Context().InputID != "destination" || m.Palette.IsOpen() {
		t.Fatal("configured input source did not dispatch input.focus")
	}
}
