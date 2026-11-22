package form

import (
	"reflect"
	"testing"

	"ofam.dev/ochazuke/core"
)

func TestChoiceResolverDependentFields(t *testing.T) {
	models := map[string][]string{
		"claude": {"opus", "sonnet"},
		"codex":  {"gpt-5", "o3"},
	}
	specs := []core.ParamSpec{
		{Name: "harness", Type: core.ParamString, Enum: []string{"claude", "codex"}, Default: "claude"},
		{Name: "model", Type: core.ParamString, Required: true}, // no static Enum
		{Name: "note", Type: core.ParamString},
	}
	m := New("Agent", specs, map[string]any{"model": "opus"})
	if KindFor(specs[1]) != KindText || m.KindAt(1) != KindText {
		t.Fatal("without a resolver the field stays static")
	}
	m.ChoiceResolver = func(s core.ParamSpec, values map[string]any) []string {
		if s.Name != "model" {
			return nil
		}
		h, _ := values["harness"].(string)
		return models[h]
	}
	if got := m.EffectiveEnum(1); !reflect.DeepEqual(got, models["claude"]) {
		t.Fatalf("claude choices %v", got)
	}
	if got := m.EffectiveEnum(0); !reflect.DeepEqual(got, specs[0].Enum) {
		t.Fatalf("static fallback %v", got)
	}
	if m.KindAt(1) != KindEnum || !m.Validate() {
		t.Fatalf("kind %s errors %v", m.KindAt(1), m.Errors)
	}
	before := m.View("f").CacheKey

	// Changing the parent updates the child's choices and re-validates it.
	m.CycleEnum(1) // harness -> codex
	if m.Values["harness"] != "codex" {
		t.Fatalf("harness %v", m.Values["harness"])
	}
	if m.Errors["model"] == "" {
		t.Fatalf("stale model not flagged: %v", m.Errors)
	}
	node := m.View("f")
	fields := node.Props["fields"].([]core.ParamSpec)
	if !reflect.DeepEqual(fields[1].Enum, models["codex"]) || node.CacheKey == before {
		t.Fatalf("view fields %v (cache changed %v)", fields[1].Enum, node.CacheKey != before)
	}
	if _, ok := m.Submit(); ok {
		t.Fatal("submitted a model the harness does not offer")
	}

	m.ActiveIdx = 1
	m.CycleEnum(1)
	if m.Values["model"] != "gpt-5" || m.Errors["model"] != "" {
		t.Fatalf("model %v errors %v", m.Values["model"], m.Errors)
	}
	vals, ok := m.Submit()
	if !ok || vals["harness"] != "codex" || vals["model"] != "gpt-5" {
		t.Fatalf("submit %v %v", vals, ok)
	}
}
