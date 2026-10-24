package ochazuke

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// choicesRuntime registers wall.paint, whose color parameter is param, and
// colors.list, a source returning []string.
func choicesRuntime(t *testing.T, param Parameter) (*Runtime, *Palette, *Arguments) {
	t.Helper()
	r := NewRuntime()
	saved := new(Arguments)
	r.Register(Operation{Verb: "list", Noun: "colors", Description: "List paint colors", Result: "string[]", Run: func(*Runtime, Context, Arguments) (any, error) {
		return []string{"cyan", "magenta"}, nil
	}})
	r.Register(Operation{Verb: "paint", Noun: "wall", Description: "Paint the wall", Parameters: map[string]Parameter{"color": param}, Run: func(_ *Runtime, _ Context, a Arguments) (any, error) {
		*saved = a
		return a, nil
	}})
	p := NewPalette(r)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteChoose(t, r, "noun:wall", "paint")
	return r, p, saved
}

func typeKeys(t *testing.T, p *Palette, text string) {
	t.Helper()
	for _, key := range text {
		paletteKeys(t, p, string(key))
	}
}

func TestPalettePickListFromEnum(t *testing.T) {
	_, p, saved := choicesRuntime(t, Parameter{Type: "string", Required: true, Enum: []string{"red", "green", "blue"}})
	view := p.View()
	for _, want := range []string{"color (string, required, pick)", "  * red", "    green", "    blue"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	typeKeys(t, p, "gr")
	if view := p.View(); !strings.Contains(view, "  * green") || strings.Contains(view, "red\n") || strings.Contains(view, "blue\n") {
		t.Fatalf("typing did not filter the pick list:\n%s", view)
	}
	paletteKeys(t, p, "enter")
	if p.drafts["color"] != "green" || p.field != 1 {
		t.Fatalf("enter did not pick: drafts=%v field=%d", p.drafts, p.field)
	}
	paletteKeys(t, p, "enter")
	if !reflect.DeepEqual(*saved, Arguments{"color": "green"}) {
		t.Fatalf("arguments = %#v", *saved)
	}
}

func TestPalettePickListFromSource(t *testing.T) {
	_, p, saved := choicesRuntime(t, Parameter{Type: "string", Required: true, Source: "colors.list"})
	if view := p.View(); !strings.Contains(view, "  * cyan") || !strings.Contains(view, "    magenta") {
		t.Fatalf("source choices not listed:\n%s", p.View())
	}
	paletteKeys(t, p, "ctrl+n")
	if view := p.View(); !strings.Contains(view, "  * magenta") {
		t.Fatalf("ctrl+n did not move the choice:\n%s", view)
	}
	paletteKeys(t, p, "ctrl+p", "ctrl+p", "enter", "enter")
	if !reflect.DeepEqual(*saved, Arguments{"color": "magenta"}) {
		t.Fatalf("arguments = %#v", *saved)
	}
}

func TestPalettePickListFiltering(t *testing.T) {
	_, p, _ := choicesRuntime(t, Parameter{Type: "string", Required: true, Enum: []string{"red", "green", "grey"}})
	typeKeys(t, p, "gre")
	paletteKeys(t, p, "ctrl+n")
	if view := p.View(); !strings.Contains(view, "    green") || !strings.Contains(view, "  * grey") || strings.Contains(view, "red\n") {
		t.Fatalf("filtered list:\n%s", view)
	}
	paletteKeys(t, p, "backspace", "backspace", "backspace", "z", "z")
	if view := p.View(); !strings.Contains(view, "No matching choices") {
		t.Fatalf("no-match list:\n%s", view)
	}
	if _, err := p.HandleKey("enter"); err == nil || !strings.Contains(err.Error(), `no choice matches "zz"`) {
		t.Fatalf("enter on no match: %v", err)
	}
	if p.field != 0 {
		t.Fatalf("a failed pick advanced the field to %d", p.field)
	}
}

func TestPalettePickListFree(t *testing.T) {
	_, p, saved := choicesRuntime(t, Parameter{Type: "string", Required: true, Enum: []string{"red"}, Free: true})
	if view := p.View(); !strings.Contains(view, "(string, required, pick or type)") {
		t.Fatalf("free label:\n%s", view)
	}
	typeKeys(t, p, "teal")
	paletteKeys(t, p, "enter", "enter")
	if !reflect.DeepEqual(*saved, Arguments{"color": "teal"}) {
		t.Fatalf("arguments = %#v", *saved)
	}
}

func TestPalettePickListRejectsDraftOutsideChoices(t *testing.T) {
	r, p, saved := choicesRuntime(t, Parameter{Type: "string", Required: true, Source: "colors.list"})
	paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "color", "value": "teal"})
	_, err := r.Execute(Request{Verb: "execute", Noun: "palette"})
	if err == nil || !p.IsOpen() || !strings.Contains(p.View(), "color must be one of cyan, magenta") {
		t.Fatalf("draft outside choices: %v\n%s", err, p.View())
	}
	paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "color", "value": "cyan"})
	paletteRequest(t, r, "execute", "palette", nil)
	if !reflect.DeepEqual(*saved, Arguments{"color": "cyan"}) {
		t.Fatalf("arguments = %#v", *saved)
	}
}

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
