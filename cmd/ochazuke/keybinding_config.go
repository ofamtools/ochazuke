package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"ochazuke"
)

type sandboxBinding struct {
	Keys   string `json:"keys"`
	Action string `json:"action"`
}

func validateSandboxBinding(binding sandboxBinding) error {
	if strings.TrimSpace(binding.Keys) == "" || strings.TrimSpace(binding.Action) == "" {
		return fmt.Errorf("keys and action must not be empty")
	}
	for _, value := range []string{binding.Keys, binding.Action} {
		if strings.ContainsFunc(value, unicode.IsControl) {
			return fmt.Errorf("use Tea key names, not control characters")
		}
	}
	for _, key := range strings.Fields(binding.Keys) {
		if key == "ctrl+c" || key == "f2" {
			return fmt.Errorf("ctrl+c and f2 are reserved sandbox controls")
		}
	}
	return nil
}

func (m *keybindingModel) bindings() []sandboxBinding {
	bindings := make([]sandboxBinding, 0, len(m.runtime.Layers["app"].Bindings))
	for _, b := range m.runtime.Layers["app"].Bindings {
		bindings = append(bindings, sandboxBinding{Keys: b.Keys, Action: b.Request.Verb})
	}
	return bindings
}

func (m *keybindingModel) importBindings(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var bindings []sandboxBinding
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bindings); err != nil {
		return err
	}
	if bindings == nil {
		return fmt.Errorf("expected a JSON array of bindings")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON array")
	}
	for i, binding := range bindings {
		if err := validateSandboxBinding(binding); err != nil {
			return fmt.Errorf("binding %d: %w", i+1, err)
		}
	}
	m.runtime = ochazuke.NewRuntime()
	for _, binding := range bindings {
		m.installBinding(binding)
	}
	m.selected = 0
	return nil
}

func (m *keybindingModel) exportBindings(path string, overwrite bool) error {
	data, err := json.MarshalIndent(m.bindings(), "", "  ")
	if err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if overwrite {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	file, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
