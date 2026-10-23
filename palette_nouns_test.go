package ochazuke

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPalettePageInstancesAreLiveAndSearchable(t *testing.T) {
	r := NewRuntime()
	p := NewPalette(r)
	for i := range 60 {
		page := pageTestRegister(t, r, fmt.Sprintf("page-%02d", i))
		page.Title = fmt.Sprintf("Workspace %02d", i)
	}
	paletteRequest(t, r, "open", "page", Arguments{"id": "page-00"})
	paletteRequest(t, r, "open", "palette", nil)
	paletteChoose(t, r, "noun:page")
	instances, verbs := 0, 0
	for _, entry := range p.entries() {
		switch entry.Kind {
		case "instance":
			instances++
		case "verb":
			verbs++
		}
	}
	if instances != 60 || verbs != 3 {
		t.Fatalf("noun listing has %d instances and %d verbs, want 60 and 3", instances, verbs)
	}
	pageTestRegister(t, r, "late-page").Title = "Newly registered workspace"
	for _, query := range []string{"Workspace 59", "page-59", "Newly registered"} {
		paletteRequest(t, r, "search", "palette", Arguments{"query": query})
		entries := p.entries()
		wantID := "page-59"
		if query == "Newly registered" {
			wantID = "late-page"
		}
		if len(entries) != 1 || entries[0].ID != wantID {
			t.Fatalf("search %q = %#v, want instance %q", query, entries, wantID)
		}
		if !strings.Contains(p.View(), entries[0].Label) || !strings.Contains(p.View(), entries[0].ID) {
			t.Fatalf("view hides instance label or ID: %s", p.View())
		}
	}
	r.Pages["late-page"].Title = "Retitled runtime page"
	paletteRequest(t, r, "search", "palette", Arguments{"query": "Retitled"})
	state := p.state()
	entries := state["entries"].([]PaletteEntry)
	if len(entries) != 1 || entries[0].Value != "instance:late-page" || entries[0].Label != "Retitled runtime page" {
		t.Fatalf("published choices = %#v", entries)
	}
	paletteChoose(t, r, entries[0].Value)
	if p.IsOpen() || r.ActivePage != "late-page" || len(r.Frames) != 1 {
		t.Fatalf("instance did not open page and close palette: page=%s frames=%d", r.ActivePage, len(r.Frames))
	}
}

func TestPaletteNounVerbsAndInstancesDoNotCollide(t *testing.T) {
	r := NewRuntime()
	for _, id := range []string{"home", "open", "verb:open", "instance:open"} {
		pageTestRegister(t, r, id)
	}
	p := NewPalette(r)
	paletteRequest(t, r, "open", "page", Arguments{"id": "home"})
	paletteRequest(t, r, "open", "palette", nil)
	paletteChoose(t, r, "noun:page", "verb:open")
	if p.stage != "arguments" || p.op.Key() != "open.page" {
		t.Fatalf("verb did not retain its ordinary argument form: %#v", p.state())
	}
	paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "id", "value": "open"})
	paletteRequest(t, r, "execute", "palette", nil)
	if r.ActivePage != "open" {
		t.Fatalf("verb form opened %q", r.ActivePage)
	}
	for _, id := range []string{"verb:open", "instance:open"} {
		paletteRequest(t, r, "open", "palette", nil)
		paletteChoose(t, r, "noun:page", "instance:"+id)
		if r.ActivePage != id || p.IsOpen() {
			t.Fatalf("instance %q collided with a verb", id)
		}
	}
}

func TestPaletteInstanceUsesConfiguredDefaultAndCapturedOrigin(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "home")
	pageTestRegister(t, r, "target")
	paletteRequest(t, r, "open", "page", Arguments{"id": "home"})
	paletteRequest(t, r, "focus", "input", Arguments{"id": "home-input"})
	origin := r.Context()
	var gotContext Context
	var gotArgs Arguments
	r.Register(Operation{Verb: "inspect", Noun: "page", Parameters: map[string]Parameter{"pageID": {Type: "string", Required: true}}, Run: func(_ *Runtime, _ Context, _ Arguments) (any, error) {
		t.Fatal("instance bypassed layered dispatch")
		return nil, nil
	}})
	r.Layers["home-editor"].Actions = map[string]Handler{"inspect.page": func(_ *Runtime, context Context, args Arguments) (any, error) {
		gotContext, gotArgs = context, args
		return nil, nil
	}}
	p := NewPalette(r)
	if err := p.RegisterNoun("page", PaletteNoun{Source: PagesPaletteSource, DefaultVerb: "inspect", Argument: "pageID"}); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteChoose(t, r, "noun:page", "instance:target")
	if !reflect.DeepEqual(gotContext, origin) || !reflect.DeepEqual(gotArgs, Arguments{"pageID": "target"}) || r.ActivePage != "home" || p.IsOpen() {
		t.Fatalf("configured dispatch: context=%#v args=%#v page=%s open=%v", gotContext, gotArgs, r.ActivePage, p.IsOpen())
	}
}

func TestPaletteInstanceFailurePreservesModalAndOrigin(t *testing.T) {
	r := NewRuntime()
	pageTestRegister(t, r, "home")
	pageTestRegister(t, r, "target")
	paletteRequest(t, r, "open", "page", Arguments{"id": "home"})
	paletteRequest(t, r, "focus", "input", Arguments{"id": "home-input"})
	modal := &Frame{ID: "dialog", Context: r.Context(), State: map[string]any{"draft": "keep"}}
	if err := r.Push(modal); err != nil {
		t.Fatal(err)
	}
	origin := r.Context()
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	paletteChoose(t, r, "noun:page")
	_, err := r.Execute(Request{Verb: "select", Noun: "palette", Args: Arguments{"value": "instance:target"}})
	if err == nil || !strings.Contains(err.Error(), "modal") || !p.IsOpen() || len(r.Frames) != 3 || r.Frames[1] != modal || modal.State["draft"] != "keep" || r.ActivePage != "home" || !reflect.DeepEqual(p.origin, origin) {
		t.Fatalf("failed instance lost modal/origin: err=%v state=%#v", err, p.state())
	}
	paletteRequest(t, r, "close", "palette", nil)
	if !reflect.DeepEqual(r.Context(), origin) || len(r.Frames) != 2 {
		t.Fatal("closing failed palette lost modal focus")
	}
	paletteRequest(t, r, "pop", "modal", nil)
	paletteRequest(t, r, "open", "palette", nil)
	paletteChoose(t, r, "noun:page", "instance:target")
	if p.IsOpen() || r.ActivePage != "target" {
		t.Fatal("page instance failed after modal closed")
	}
}

func TestPaletteInstanceUsesRuntimeValidationAndRestoresOnError(t *testing.T) {
	r := NewRuntime()
	var captured Context
	origin := r.Context()
	var sourceContext Context
	r.Register(Operation{Verb: "use", Noun: "thing", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}}, Run: func(_ *Runtime, c Context, _ Arguments) (any, error) {
		captured = c
		return nil, errors.New("thing rejected")
	}})
	p := NewPalette(r)
	if err := p.RegisterNoun("thing", PaletteNoun{Source: func(_ *Runtime, c Context) []PaletteInstance {
		sourceContext = c
		return []PaletteInstance{{ID: "x", Label: "Example"}}
	}, DefaultVerb: "use", Argument: "id"}); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteChoose(t, r, "noun:thing")
	_, err := r.Execute(Request{Verb: "select", Noun: "palette", Args: Arguments{"value": "instance:x"}})
	if err == nil || err.Error() != "thing rejected" || !p.IsOpen() || !reflect.DeepEqual(captured, origin) || !reflect.DeepEqual(sourceContext, origin) || p.stage != "second" {
		t.Fatalf("failure dispatch/restoration: %v %#v", err, p.state())
	}
	op := r.Operations["use.thing"]
	op.Parameters["id"] = Parameter{Type: "integer", Required: true}
	captured = Context{}
	_, err = r.Execute(Request{Verb: "select", Noun: "palette", Args: Arguments{"value": "instance:x"}})
	if err == nil || !strings.Contains(err.Error(), "integer") || !p.IsOpen() || len(captured.Path) != 0 {
		t.Fatalf("instance bypassed live validation: %v", err)
	}
}

func TestPaletteNounRegistrationValidation(t *testing.T) {
	r := NewRuntime()
	r.Register(Operation{Verb: "use", Noun: "thing", Parameters: map[string]Parameter{"count": {Type: "integer", Required: true}, "id": {Type: "string", Required: true}}, Run: func(*Runtime, Context, Arguments) (any, error) { return nil, nil }})
	p := NewPalette(r)
	for _, test := range []struct {
		name, noun, verb, argument, message string
		source                              PaletteSource
	}{
		{"unknown noun", "missing", "open", "id", "unknown noun", PagesPaletteSource},
		{"nil source", "page", "open", "id", "source is required", nil},
		{"unknown action", "page", "missing", "id", "unknown default action", PagesPaletteSource},
		{"unknown argument", "page", "open", "missing", "string instance argument", PagesPaletteSource},
		{"non-string argument", "thing", "use", "count", "string instance argument", PagesPaletteSource},
		{"additional required argument", "thing", "use", "id", "also requires argument", PagesPaletteSource},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := p.RegisterNoun(test.noun, PaletteNoun{Source: test.source, DefaultVerb: test.verb, Argument: test.argument})
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}
