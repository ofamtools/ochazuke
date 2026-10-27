package ochazuke

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
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

// A parameter naming a noun takes its choices from the noun: its list verb
// when that is a read with no required parameter, else its live instances
// in registration order; Enum comes first and an explicit Source wins.
func TestParameterNounChoices(t *testing.T) {
	r := NewRuntime()
	r.Register(Operation{Verb: "list", Noun: "color", Run: func(*Runtime, Context, Arguments) (any, error) {
		return []map[string]any{{"id": "c1", "label": "Cyan"}, {"key": "m1", "title": "Magenta"}}, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "shade", Run: func(*Runtime, Context, Arguments) (any, error) {
		return []string{"dark", "light"}, nil
	}})
	r.Register(Operation{Verb: "list", Noun: "guarded", Parameters: map[string]Parameter{"scope": {Type: "string", Required: true}}, Run: func(*Runtime, Context, Arguments) (any, error) {
		return []string{"never"}, nil
	}})
	r.Define("widget")
	r.Define("guarded")
	r.Instance("w1", Kind("widget")).Label = "First"
	r.Instance("w2", Kind("widget"))
	r.Instance("g1", Kind("guarded"))
	r.Define("task")
	for _, id := range []string{"todo", "done"} {
		if _, err := r.AddList(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.List("todo").SetItems([]ListItem{{ID: "t1", Label: "Write", Noun: "task", Data: "ext-1"}, {ID: "t2", Label: "Hidden", Noun: "task"}, {ID: "w9", Noun: "widget"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.List("done").SetItems([]ListItem{{ID: "t3", Noun: "task"}}); err != nil {
		t.Fatal(err)
	}
	r.List("todo").SetFilter("write")
	if shown := r.List("todo").Shown(); shown != 1 {
		t.Fatalf("filter shows %v", shown)
	}
	c := r.Context()
	for _, tc := range []struct {
		name   string
		param  Parameter
		source string
		want   []Choice
	}{
		{"list verb", Parameter{Type: "string", Noun: "color"}, "color.list", []Choice{{ID: "c1", Label: "Cyan"}, {ID: "m1", Label: "Magenta"}}},
		{"instances", Parameter{Type: "string", Noun: "widget"}, "", []Choice{{ID: "w1", Label: "First"}, {ID: "w2", Label: "w2"}, {ID: "w9", Label: "w9"}}},
		{"list rows", Parameter{Type: "string", Noun: "task"}, "", []Choice{{ID: "t1", Label: "Write"}, {ID: "t2", Label: "Hidden"}, {ID: "t3", Label: "t3"}}},
		{"source wins", Parameter{Type: "string", Noun: "color", Source: "shade.list"}, "shade.list", []Choice{{ID: "dark", Label: "dark"}, {ID: "light", Label: "light"}}},
		{"enum first", Parameter{Type: "string", Noun: "widget", Enum: []string{"none"}}, "", []Choice{{ID: "none", Label: "none"}, {ID: "w1", Label: "First"}, {ID: "w2", Label: "w2"}, {ID: "w9", Label: "w9"}}},
		{"list verb needs an argument", Parameter{Type: "string", Noun: "guarded"}, "", []Choice{{ID: "g1", Label: "g1"}}},
		{"unknown noun", Parameter{Type: "string", Noun: "nothing"}, "", nil},
	} {
		if got := r.ChoicesSource(tc.param); got != tc.source {
			t.Fatalf("%s: source %q, want %q", tc.name, got, tc.source)
		}
		got, err := r.Choices(c, tc.param)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: Choices = %v %v, want %v", tc.name, got, err, tc.want)
		}
		var loaded []Choice
		if r.loadChoices(c, tc.param, "app", func(choices []Choice, err error) {
			if err != nil {
				t.Fatalf("%s: loadChoices: %v", tc.name, err)
			}
			loaded = choices
		}) {
			t.Fatalf("%s: loadChoices went off the loop", tc.name)
		}
		if !reflect.DeepEqual(loaded, tc.want) {
			t.Fatalf("%s: loadChoices = %v, want %v", tc.name, loaded, tc.want)
		}
	}
	confirmed := NewRuntime()
	confirmed.Register(Operation{Verb: "list", Noun: "risky", Confirm: "Sure?", Run: func(*Runtime, Context, Arguments) (any, error) { return []string{"x"}, nil }})
	if got := confirmed.ChoicesSource(Parameter{Noun: "risky"}); got != "" {
		t.Fatalf("a confirmed list verb is a choices source: %q", got)
	}
}

// A Noun whose list verb is an application verb returning Pending loads off
// the loop through the same path as a Source.
func TestParameterNounChoicesLoadOffTheLoop(t *testing.T) {
	r := NewRuntime()
	r.Define("remote").Verb("list", func(Ctx) Result {
		return Pending(func(context.Context) (any, error) { return []string{"far"}, nil })
	})
	var got []Choice
	if !r.loadChoices(r.Context(), Parameter{Type: "string", Noun: "remote"}, "app", func(choices []Choice, err error) {
		if err != nil {
			t.Fatal(err)
		}
		got = choices
	}) {
		t.Fatal("a pending list verb loaded on the loop")
	}
	settleJob(t, r)
	if !reflect.DeepEqual(got, []Choice{{ID: "far", Label: "far"}}) {
		t.Fatalf("loaded %v", got)
	}
}

func TestParseChoicesFallbackFields(t *testing.T) {
	got, err := ParseChoices([]any{
		map[string]any{"key": "app.quit", "label": "Quit"},
		map[string]any{"name": "theme"},
		map[string]any{"id": "alpha", "title": "Alpha"},
		map[string]string{"name": "n", "title": "N"},
	})
	want := []Choice{{ID: "app.quit", Label: "Quit"}, {ID: "theme", Label: "theme"}, {ID: "alpha", Label: "Alpha"}, {ID: "n", Label: "N"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v %v, want %v", got, err, want)
	}
	ops, err := ParseChoices([]Operation{{Noun: "app", Verb: "quit"}})
	if err != nil || !reflect.DeepEqual(ops, []Choice{{ID: "app.quit", Label: "app.quit"}}) {
		t.Fatalf("operations: %v %v", ops, err)
	}
}

// The framework's own noun parameters list what their list verbs return.
func TestFrameworkNounParameters(t *testing.T) {
	r := NewRuntime()
	if _, err := r.AddPage("alpha", "Alpha", func(*Runtime, int, int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	c := r.Context()
	pages, err := r.Choices(c, r.Operations["page.open"].Parameters["id"])
	if err != nil || !slices.Contains(pages, Choice{ID: "alpha", Label: "Alpha"}) {
		t.Fatalf("page.open id: %v %v", pages, err)
	}
	modes, err := r.Choices(c, r.Operations["mode.set"].Parameters["mode"])
	if err != nil || !slices.ContainsFunc(modes, func(ch Choice) bool { return ch.ID == "insert" }) {
		t.Fatalf("mode.set mode: %v %v", modes, err)
	}
	ops, err := r.Choices(c, r.Operations["confirm.set"].Parameters["operation"])
	if err != nil || !slices.ContainsFunc(ops, func(ch Choice) bool { return ch.ID == "page.open" }) {
		t.Fatalf("confirm.set operation: %v", err)
	}
}
