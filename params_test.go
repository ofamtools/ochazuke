package ochazuke

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParameterChoiceValidation(t *testing.T) {
	r := NewRuntime()
	failing := errors.New("offline")
	r.Register(Operation{Verb: "list", Noun: "colors", Run: func(*Runtime, Context, Arguments) (any, error) {
		return []map[string]any{{"id": "c1", "label": "Cyan"}, {"id": "m1"}}, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "broken", Run: func(*Runtime, Context, Arguments) (any, error) { return nil, failing }})
	ok := func(*Runtime, Context, Arguments) (any, error) { return "ok", nil }
	r.Register(Operation{Verb: "enum", Noun: "check", Parameters: map[string]Parameter{"v": {Type: "string", Enum: []string{"a", "b"}}}, Run: ok})
	r.Register(Operation{Verb: "free", Noun: "check", Parameters: map[string]Parameter{"v": {Type: "string", Enum: []string{"a"}, Free: true}}, Run: ok})
	r.Register(Operation{Verb: "source", Noun: "check", Parameters: map[string]Parameter{"v": {Type: "string", Source: "colors.list"}}, Run: ok})
	r.Register(Operation{Verb: "broken", Noun: "check", Parameters: map[string]Parameter{"v": {Type: "string", Source: "broken.list"}}, Run: ok})
	for _, tc := range []struct {
		verb, value, err string
	}{
		{"enum", "a", ""},
		{"enum", "c", "v must be one of a, b"},
		{"free", "z", ""},
		{"source", "c1", ""},
		{"source", "Cyan", "v must be one of c1, m1"},
		{"broken", "anything", ""},
	} {
		_, err := r.Execute(Request{Verb: tc.verb, Noun: "check", Args: Arguments{"v": tc.value}})
		if (tc.err == "" && err != nil) || (tc.err != "" && (err == nil || err.Error() != tc.err)) {
			t.Fatalf("%s %q: error %v, want %q", tc.verb, tc.value, err, tc.err)
		}
	}
	if _, err := r.Execute(Request{Verb: "enum", Noun: "check", Args: Arguments{"v": 3}}); err == nil || err.Error() != "v must be string" {
		t.Fatalf("type check changed: %v", err)
	}
	data, err := json.Marshal(r.Catalog())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"enum":["a","b"]`, `"source":"colors.list"`, `"free":true`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("catalog missing %s", want)
		}
	}
}

func TestParseChoicesShapes(t *testing.T) {
	want := []Choice{{ID: "a", Label: "a"}, {ID: "b", Label: "Bee"}}
	for _, result := range []any{
		[]any{"a", map[string]any{"id": "b", "label": "Bee"}},
		[]map[string]any{{"id": "a"}, {"id": "b", "label": "Bee"}},
		[]map[string]string{{"id": "a"}, {"id": "b", "label": "Bee"}},
		[]PaletteInstance{{ID: "a"}, {ID: "b", Label: "Bee"}},
	} {
		got, err := ParseChoices(result)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%T: %v %v", result, got, err)
		}
	}
	if got, err := ParseChoices([]string{"x"}); err != nil || !reflect.DeepEqual(got, []Choice{{ID: "x", Label: "x"}}) {
		t.Fatalf("[]string: %v %v", got, err)
	}
	for _, bad := range []any{"x", []any{1}, []any{map[string]any{"label": "no id"}}} {
		if _, err := ParseChoices(bad); err == nil {
			t.Fatalf("%#v parsed", bad)
		}
	}
}
