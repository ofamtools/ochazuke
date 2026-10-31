package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"ofam.dev/ochazuke"
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
		if key == "ctrl+c" || key == "ctrl+g" {
			return fmt.Errorf("ctrl+c and ctrl+g are reserved sandbox controls")
		}
	}
	return nil
}

func (m *keybindingModel) bindings() []sandboxBinding {
	rows := m.runtime.Noun("app").Bindings[""]
	bindings := make([]sandboxBinding, 0, len(rows))
	for _, b := range rows {
		if b.Request.Noun == "sandbox" {
			bindings = append(bindings, sandboxBinding{Keys: b.Keys, Action: b.Request.Verb})
		}
	}
	return bindings
}

func (m *keybindingModel) importBindings(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data, err = ochazuke.StandardizeJSONC(data)
	if err != nil {
		return err
	}
	var bindings []sandboxBinding
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '{' {
		// The grouped bindings format, as exportBindings writes it.
		if bindings, err = groupedSandboxBindings(data); err != nil {
			return err
		}
	} else {
		decoder := json.NewDecoder(bytes.NewReader(data))
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
	}
	for i, binding := range bindings {
		if err := validateSandboxBinding(binding); err != nil {
			return fmt.Errorf("binding %d: %w", i+1, err)
		}
	}
	m.resetRuntime()
	for _, binding := range bindings {
		m.installBinding(binding)
	}
	m.selected = 0
	return nil
}

// resetRuntime replaces the runtime with an empty one, cancelling any pending
// sequence and keeping the configured binding timeout (it is not in the JSON).
func (m *keybindingModel) resetRuntime() {
	timeout := time.Duration(0)
	if m.runtime != nil {
		m.runtime.CancelPendingBinding()
		timeout = m.runtime.BindingTimeout
	}
	m.runtime = ochazuke.NewRuntime()
	if timeout > 0 {
		m.runtime.BindingTimeout = timeout
	}
}

func (m *keybindingModel) exportBindings(path string, overwrite bool) error {
	// The grouped bindings format (ochazuke.FormatBindings): one "app"
	// group, each key mapped to its sandbox.ACTION.
	data, err := ochazuke.FormatBindings(ochazuke.BindingsDoc{Groups: ochazuke.GroupBindings(m.sandboxRows())})
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
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// sandboxRows are the sandbox bindings as rows of the app noun's any-mode
// table running sandbox.ACTION.
func (m *keybindingModel) sandboxRows() []ochazuke.Binding {
	rows := []ochazuke.Binding{}
	for _, b := range m.bindings() {
		rows = append(rows, ochazuke.Binding{Noun: "app", Keys: b.Keys, Request: ochazuke.Request{Noun: "sandbox", Verb: b.Action}})
	}
	return rows
}

// groupedSandboxBindings reads a grouped bindings file of sandbox rows:
// keys on the app noun's any-mode table, each a sandbox.ACTION.
func groupedSandboxBindings(data []byte) ([]sandboxBinding, error) {
	doc, _, err := ochazuke.ParseBindings(data)
	if err != nil {
		return nil, err
	}
	bindings := []sandboxBinding{}
	for _, row := range doc.Rows() {
		if row.Noun != "app" || row.Mode != "" || row.Keys == "" || row.Unbind || row.Request.Noun != "sandbox" {
			return nil, fmt.Errorf("%s: the sandbox reads only \"app\" keys running sandbox.ACTION", row.Keys+row.Pattern)
		}
		bindings = append(bindings, sandboxBinding{Keys: row.Keys, Action: row.Request.Verb})
	}
	return bindings, nil
}
