package ochazuke

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func paletteRequest(t *testing.T, r *Runtime, verb, noun string, args Arguments) any {
	t.Helper()
	result, err := r.Execute(Request{Verb: verb, Noun: noun, Args: args})
	if err != nil {
		t.Fatalf("%s.%s: %v", verb, noun, err)
	}
	return result
}

func paletteChoose(t *testing.T, r *Runtime, values ...string) {
	t.Helper()
	for _, value := range values {
		paletteRequest(t, r, "select", "palette", Arguments{"value": value})
	}
}

func paletteKeys(t *testing.T, p *Palette, keys ...string) {
	t.Helper()
	for _, key := range keys {
		handled, err := p.HandleKey(key)
		if !handled || err != nil {
			t.Fatalf("palette key %q: handled=%v error=%v", key, handled, err)
		}
	}
}

func paletteInputSnapshot(t *testing.T, in *Input) string {
	t.Helper()
	data, err := json.Marshal(in.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPaletteVerbAndNounPaths(t *testing.T) {
	for _, section := range []string{"verbs", "nouns"} {
		t.Run(section, func(t *testing.T) {
			r := NewRuntime()
			in := NewInput("draft", "before", false)
			if err := r.AddInput(in); err != nil {
				t.Fatal(err)
			}
			if err := r.Focus("draft"); err != nil {
				t.Fatal(err)
			}
			p := NewPalette(r)
			if err := p.Open(); err != nil {
				t.Fatal(err)
			}
			if section == "verbs" {
				paletteChoose(t, r, "verb:insert", "input")
			} else {
				paletteChoose(t, r, "noun:input", "insert")
			}
			paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "text", "value": "hello"})
			paletteRequest(t, r, "execute", "palette", nil)
			if got := string(in.Text); got != "hellobefore" {
				t.Fatalf("input = %q, want hellobefore", got)
			}
			if p.IsOpen() || r.Context().InputID != "draft" {
				t.Fatalf("origin was not restored: %+v", r.Context())
			}
		})
	}
}

func TestPaletteNestedSearch(t *testing.T) {
	for _, path := range []struct {
		name, query, choice, nested string
	}{
		{"verb", "insert", "verb:insert", "input"},
		{"noun", "input", "noun:input", "insert"},
	} {
		t.Run(path.name, func(t *testing.T) {
			r := NewRuntime()
			p := NewPalette(r)
			if err := p.Open(); err != nil {
				t.Fatal(err)
			}
			for _, key := range path.query {
				paletteKeys(t, p, string(key))
			}
			paletteChoose(t, r, path.choice)
			if !p.searching || p.query != "" || string(p.editor("search").Text) != "" {
				t.Fatalf("nested search: active=%v query=%q text=%q", p.searching, p.query, p.editor("search").Text)
			}
			for _, key := range path.nested {
				paletteKeys(t, p, string(key))
			}
			if p.query != path.nested {
				t.Fatalf("nested query = %q, want %q", p.query, path.nested)
			}
			paletteKeys(t, p, "enter")
			if p.stage != "arguments" || p.op.Key() != "insert.input" {
				t.Fatalf("selected stage=%q operation=%q", p.stage, p.op.Key())
			}
			paletteKeys(t, p, "hello")
			if p.drafts["text"] != "hello" {
				t.Fatalf("argument text = %q, want hello", p.drafts["text"])
			}
		})
	}
}

func TestPaletteKeyboardUndoUsesCapturedInput(t *testing.T) {
	r := NewRuntime()
	origin := NewInput("origin", "base", false)
	other := NewInput("other", "untouched", false)
	for _, in := range []*Input{origin, other} {
		if err := r.AddInput(in); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Focus("origin"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"1", "2", "3"} {
		if err := origin.Replace(len(origin.Text), len(origin.Text), text); err != nil {
			t.Fatal(err)
		}
	}
	before := paletteInputSnapshot(t, origin)
	otherBefore := paletteInputSnapshot(t, other)
	contextBefore := r.Context().Clone()
	p := NewPalette(r)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteKeys(t, p, "undo", "enter", "input", "enter")
	if got := paletteInputSnapshot(t, origin); got != before {
		t.Fatal("navigation changed originating input")
	}
	// Changing overlay focus must not retarget the selected command.
	if err := r.Focus("other"); err != nil {
		t.Fatal(err)
	}
	paletteKeys(t, p, "2", "enter", "enter")
	if got := string(origin.Text); got != "base1" {
		t.Fatalf("captured undo = %q, want base1", got)
	}
	if got := paletteInputSnapshot(t, other); got != otherBefore {
		t.Fatal("undo changed overlay-focused input")
	}
	if got := r.Context(); !reflect.DeepEqual(got, contextBefore) {
		t.Fatalf("context = %+v, want %+v", got, contextBefore)
	}
}

func TestPaletteOptionalUndoStepsCanBeOmitted(t *testing.T) {
	r := NewRuntime()
	in := NewInput("draft", "a", false)
	if err := r.AddInput(in); err != nil {
		t.Fatal(err)
	}
	if err := r.Focus("draft"); err != nil {
		t.Fatal(err)
	}
	if err := in.Replace(1, 1, "b"); err != nil {
		t.Fatal(err)
	}
	if err := in.Replace(2, 2, "c"); err != nil {
		t.Fatal(err)
	}
	p := NewPalette(r)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteChoose(t, r, "verb:undo", "input")
	paletteKeys(t, p, "tab", "enter")
	if got := string(in.Text); got != "ab" {
		t.Fatalf("default undo = %q, want ab", got)
	}
}

func TestPaletteTypedArgumentsAndOmission(t *testing.T) {
	for _, entered := range []bool{false, true} {
		name := "omitted"
		if entered {
			name = "entered"
		}
		t.Run(name, func(t *testing.T) {
			r := NewRuntime()
			var saved Arguments
			r.Register(Operation{Verb: "apply", Noun: "record", Description: "Apply typed record settings", Parameters: map[string]Parameter{
				"name":    {Type: "string", Required: true},
				"count":   {Type: "integer"},
				"enabled": {Type: "boolean"},
				"note":    {Type: "string"},
			}, Run: func(_ *Runtime, _ Context, a Arguments) (any, error) { saved = a; return a, nil }})
			p := NewPalette(r)
			if err := p.Open(); err != nil {
				t.Fatal(err)
			}
			paletteChoose(t, r, "noun:record", "apply")
			paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "name", "value": "Ada"})
			if entered {
				for key, value := range map[string]string{"count": "0", "enabled": "false", "note": ""} {
					paletteRequest(t, r, "set", "palette-argument", Arguments{"name": key, "value": value})
				}
			} else {
				paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "count", "value": "4"})
				paletteRequest(t, r, "clear", "palette-argument", Arguments{"name": "count"})
			}
			paletteRequest(t, r, "execute", "palette", nil)
			want := Arguments{"name": "Ada"}
			if entered {
				want = Arguments{"name": "Ada", "count": 0, "enabled": false, "note": ""}
			}
			if !reflect.DeepEqual(saved, want) {
				t.Fatalf("arguments = %#v, want %#v", saved, want)
			}
		})
	}
}

func TestPaletteArgumentErrorsPreserveDraftsAndCanRecover(t *testing.T) {
	r := NewRuntime()
	var saved Arguments
	r.Register(Operation{Verb: "save", Noun: "record", Description: "Save a typed record", Parameters: map[string]Parameter{
		"name": {Type: "string", Required: true}, "count": {Type: "integer"}, "enabled": {Type: "boolean"},
	}, Run: func(_ *Runtime, _ Context, a Arguments) (any, error) { saved = a; return a, nil }})
	p := NewPalette(r)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteChoose(t, r, "verb:save", "record")
	_, err := r.Execute(Request{Verb: "execute", Noun: "palette"})
	if err == nil || !strings.Contains(p.View(), "name is required") {
		t.Fatalf("missing required-field error: %v\n%s", err, p.View())
	}
	paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "name", "value": "kept"})
	paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "count", "value": "no"})
	_, err = r.Execute(Request{Verb: "execute", Noun: "palette"})
	if err == nil || !p.IsOpen() || !strings.Contains(p.View(), "count must be integer") || !strings.Contains(p.View(), "kept") {
		t.Fatalf("integer failure lost form: %v\n%s", err, p.View())
	}
	paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "count", "value": "2"})
	paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "enabled", "value": "maybe"})
	_, err = r.Execute(Request{Verb: "execute", Noun: "palette"})
	if err == nil || !strings.Contains(p.View(), "enabled must be boolean") {
		t.Fatalf("boolean error: %v\n%s", err, p.View())
	}
	paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "enabled", "value": "true"})
	paletteRequest(t, r, "execute", "palette", nil)
	if !reflect.DeepEqual(saved, Arguments{"name": "kept", "count": 2, "enabled": true}) {
		t.Fatalf("recovered arguments: %#v", saved)
	}
}

func TestPaletteHandlerFailureRestoresOverlayAndDraft(t *testing.T) {
	r := NewRuntime()
	var published string
	r.Register(Operation{Verb: "publish", Noun: "record", Description: "Publish a draft", Parameters: map[string]Parameter{"text": {Type: "string", Required: true}}, Run: func(_ *Runtime, _ Context, a Arguments) (any, error) {
		text := StringArg(a, "text", "")
		if text == "bad" {
			return nil, errors.New("publication rejected")
		}
		published = text
		return text, nil
	}})
	p := NewPalette(r)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteChoose(t, r, "verb:publish", "record")
	paletteKeys(t, p, "bad", "enter")
	handled, err := p.HandleKey("enter")
	if !handled || err == nil || !p.IsOpen() || len(r.Frames) != 2 {
		t.Fatalf("failed execution lost overlay: handled=%v err=%v frames=%d", handled, err, len(r.Frames))
	}
	if view := p.View(); !strings.Contains(view, "publication rejected") || !strings.Contains(view, "\"bad\"") {
		t.Fatalf("error/draft not visible:\n%s", view)
	}
	paletteKeys(t, p, "shift+tab", "ctrl+u", "good", "enter", "enter")
	if published != "good" || p.IsOpen() {
		t.Fatalf("retry: published=%q open=%v", published, p.IsOpen())
	}
}

func TestPaletteSearchNavigationAndCancellation(t *testing.T) {
	r := NewRuntime()
	in := NewInput("draft", "unchanged", false)
	if err := r.AddInput(in); err != nil {
		t.Fatal(err)
	}
	if err := r.Focus("draft"); err != nil {
		t.Fatal(err)
	}
	before := paletteInputSnapshot(t, in)
	contextBefore := r.Context()
	p := NewPalette(r)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteKeys(t, p, "j")
	if p.query != "j" {
		t.Fatal("search must accept j as text")
	}
	paletteKeys(t, p, "esc")
	if !p.searching || p.editor("search").Mode != "normal" {
		t.Fatal("first Escape must enter normal mode in the search input")
	}
	paletteKeys(t, p, "esc")
	if !p.IsOpen() || p.searching {
		t.Fatal("second Escape must leave search before closing")
	}
	paletteKeys(t, p, "esc")
	if p.IsOpen() {
		t.Fatal("root escape must cancel")
	}
	if got := paletteInputSnapshot(t, in); got != before {
		t.Fatal("cancellation modified input state")
	}
	if !reflect.DeepEqual(r.Context(), contextBefore) {
		t.Fatalf("cancellation changed context: %+v", r.Context())
	}
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteChoose(t, r, "verb:insert", "input")
	paletteKeys(t, p, "draft text", "backspace", "esc", "esc", "esc", "esc")
	if p.IsOpen() || paletteInputSnapshot(t, in) != before {
		t.Fatal("argument cancellation modified input")
	}
	if handled, err := p.HandleKey("j"); handled || err != nil {
		t.Fatalf("closed palette consumed key: %v %v", handled, err)
	}
}

func TestPaletteRecursiveExecutionIsVisibleAndCancelable(t *testing.T) {
	r := NewRuntime()
	p := NewPalette(r)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteChoose(t, r, "verb:execute")
	_, err := r.Execute(Request{Verb: "select", Noun: "palette", Args: Arguments{"value": "palette"}})
	if err == nil || !p.IsOpen() || !strings.Contains(p.View(), "recursive palette execution") {
		t.Fatalf("recursive execution: %v\n%s", err, p.View())
	}
	paletteKeys(t, p, "ctrl+c")
	if p.IsOpen() || len(r.Frames) != 1 {
		t.Fatal("recursive execution prevented cancellation")
	}
}

func TestPaletteSelectedFocusChangesOriginFrame(t *testing.T) {
	r := NewRuntime()
	for _, id := range []string{"first", "second"} {
		if err := r.AddInput(NewInput(id, id, false)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Focus("first"); err != nil {
		t.Fatal(err)
	}
	p := NewPalette(r)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteChoose(t, r, "verb:focus", "input")
	paletteRequest(t, r, "set", "palette-argument", Arguments{"name": "id", "value": "second"})
	paletteRequest(t, r, "execute", "palette", nil)
	if p.IsOpen() || len(r.Frames) != 1 || r.Context().InputID != "second" {
		t.Fatalf("focus was applied to discarded overlay: %+v", r.Context())
	}
}

func TestPaletteNormalNavigationAfterInitialSearch(t *testing.T) {
	r := NewRuntime()
	p := NewPalette(r)
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteKeys(t, p, "page", "esc", "j", "k", "enter")
	if p.first != "page" || p.section != "nouns" {
		t.Fatalf("normal navigation changed search text or selected the wrong noun: first=%q section=%q", p.first, p.section)
	}
	paletteKeys(t, p, "ctrl+c")
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	paletteKeys(t, p, "esc", "l", "enter")
	if p.first != "app" || p.section != "nouns" {
		t.Fatalf("l did not navigate to nouns: first=%q section=%q", p.first, p.section)
	}
}
