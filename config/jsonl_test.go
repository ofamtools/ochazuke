package config

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"ofam.dev/ochazuke/core"
)

func TestBindingsJSONLRoundTrip(t *testing.T) {
	bindings := []core.Binding{
		{Scope: "table", Mode: "normal", Keys: []string{"j"}, Command: "motion.down", Args: map[string]any{"text": "two\nlines", "count": 2.0}},
		{Scope: "table", Mode: "normal", Keys: []string{"j"}, Unbind: true},
		{Mode: "normal", Keys: []string{"space"}, Layer: &core.Layer{Name: "leader", Modes: []string{"leader", "normal"}, Sticky: true}},
	}
	exported, err := ExportBindingsJSONL(bindings)
	if err != nil {
		t.Fatal(err)
	}
	if lines := bytes.Count(exported, []byte("\n")); lines != len(bindings) {
		t.Fatalf("exported %d lines for %d bindings", lines, len(bindings))
	}
	loaded, err := LoadBindingsJSONL(exported)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, bindings) {
		t.Fatalf("bindings changed during round trip: %+v", loaded)
	}
	encodedAgain, err := ExportBindingsJSONL(loaded)
	if err != nil || !bytes.Equal(exported, encodedAgain) {
		t.Fatalf("binding export changed: %q, %v", encodedAgain, err)
	}
}

func TestSettingsJSONLRoundTrip(t *testing.T) {
	settings, err := LoadSettings([]byte(`{"z":"two\nlines","n":null,"a":{"z":true,"a":2},"":["one","two"]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	values := settings.Merged()
	exported, err := ExportSettingsJSONL(values)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"key":"","value":["one","two"]}
{"key":"a","value":{"a":2,"z":true}}
{"key":"n","value":null}
{"key":"z","value":"two\nlines"}
`
	if string(exported) != want {
		t.Fatalf("exported settings:\n%s\nwant:\n%s", exported, want)
	}
	loaded, err := LoadSettingsJSONL(exported)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, values) {
		t.Fatalf("settings changed during round trip: %+v", loaded)
	}
	encodedAgain, err := ExportSettingsJSONL(loaded)
	if err != nil || !bytes.Equal(exported, encodedAgain) {
		t.Fatalf("settings export changed: %q, %v", encodedAgain, err)
	}
}

func TestSettingsJSONLLargeRecord(t *testing.T) {
	large := strings.Repeat("残", 64*1024)
	data := []byte("{\"key\":\"large\",\"value\":null}\r\n\r\n{\"key\":\"large\",\"value\":\"" + large + "\"}")
	loaded, err := LoadSettingsJSONL(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := loaded["large"].(string); got != large {
		t.Fatalf("second value for large has %d bytes, want %d", len(got), len(large))
	}
}

func TestJSONLMalformedRecords(t *testing.T) {
	t.Run("bindings", func(t *testing.T) {
		for _, row := range []string{`null`, `[]`, `{`, `{"keys":"j"}`, `{} {}`} {
			loaded, err := LoadBindingsJSONL([]byte("{\"keys\":[\"j\"]}\n\n" + row))
			if loaded != nil || err == nil || !strings.Contains(err.Error(), "bindings JSONL line 3") {
				t.Errorf("row %q: bindings = %v, error = %v", row, loaded, err)
			}
		}
		exported, err := ExportBindingsJSONL([]core.Binding{{}, {Args: map[string]any{"unsupported": make(chan int)}}})
		if exported != nil || err == nil || !strings.Contains(err.Error(), "bindings JSONL line 2") {
			t.Errorf("unsupported argument: output = %q, error = %v", exported, err)
		}
	})
	t.Run("settings", func(t *testing.T) {
		for _, row := range []string{`null`, `{`, `{"value":1}`, `{"key":null,"value":1}`, `{"key":1,"value":true}`, `{"key":"x"}`} {
			loaded, err := LoadSettingsJSONL([]byte("{\"key\":\"a\",\"value\":true}\n" + row))
			if loaded != nil || err == nil || !strings.Contains(err.Error(), "settings JSONL line 2") {
				t.Errorf("row %q: settings = %v, error = %v", row, loaded, err)
			}
		}
		exported, err := ExportSettingsJSONL(map[string]any{"unsupported": make(chan int)})
		if exported != nil || err == nil || !strings.Contains(err.Error(), `settings JSONL key "unsupported"`) {
			t.Errorf("unsupported value: output = %q, error = %v", exported, err)
		}
	})
}
