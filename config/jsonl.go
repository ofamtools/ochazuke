package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"ofam.dev/ochazuke/core"
)

// ExportBindingsJSONL writes one compact JSON object per binding, preserving
// declaration order for keymap priority ties.
func ExportBindingsJSONL(bindings []core.Binding) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	for i, binding := range bindings {
		if err := enc.Encode(binding); err != nil {
			return nil, fmt.Errorf("bindings JSONL line %d: %w", i+1, err)
		}
	}
	return out.Bytes(), nil
}

// LoadBindingsJSONL decodes binding objects in declaration order.
func LoadBindingsJSONL(data []byte) ([]core.Binding, error) {
	var bindings []core.Binding
	err := decodeJSONLLines(data, "bindings", func(line []byte) error {
		var binding core.Binding
		if err := json.Unmarshal(line, &binding); err != nil {
			return err
		}
		bindings = append(bindings, binding)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return bindings, nil
}

// ExportSettingsJSONL writes one compact key/value object per supplied setting,
// in lexical key order.
func ExportSettingsJSONL(values map[string]any) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	for _, key := range slices.Sorted(maps.Keys(values)) {
		row := struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}{Key: key, Value: values[key]}
		if err := enc.Encode(row); err != nil {
			return nil, fmt.Errorf("settings JSONL key %q: %w", key, err)
		}
	}
	return out.Bytes(), nil
}

// LoadSettingsJSONL decodes key/value objects. Later rows replace earlier rows
// with the same key, as in LoadSettings.
func LoadSettingsJSONL(data []byte) (map[string]any, error) {
	values := make(map[string]any)
	err := decodeJSONLLines(data, "settings", func(line []byte) error {
		var row struct {
			Key   *string         `json:"key"`
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			return err
		}
		if row.Key == nil || row.Value == nil {
			return fmt.Errorf("expected a string key and a value")
		}
		var value any
		if err := json.Unmarshal(row.Value, &value); err != nil {
			return fmt.Errorf("key %q: %w", *row.Key, err)
		}
		values[*row.Key] = value
		return nil
	})
	if err != nil {
		return nil, err
	}
	return values, nil
}

// decodeJSONLLines accepts blank lines and a final line without a newline.
// Splitting input bytes avoids imposing a fixed size limit on a config record.
func decodeJSONLLines(data []byte, name string, decode func([]byte) error) error {
	lineNumber := 0
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		lineNumber++
		line = bytes.Trim(line, " \t\r")
		if len(line) == 0 {
			continue
		}
		if line[0] != '{' {
			return fmt.Errorf("%s JSONL line %d: expected an object", name, lineNumber)
		}
		if err := decode(line); err != nil {
			return fmt.Errorf("%s JSONL line %d: %w", name, lineNumber, err)
		}
	}
	return nil
}
