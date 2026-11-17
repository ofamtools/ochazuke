package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cli"
)

type bellParams struct {
	Times int    `json:"times" param:"required" desc:"How many rings"`
	Tone  string `json:"tone"`
}

// One noun file through the loader: its props (a polymorphic owner), store
// source, verbs with named handlers (a typed one supplying the
// parameters), guard, key rows and settings land on the runtime, and every
// problem (an unknown key, a missing handler, a restated parameter list
// that differs, an unused handler) is reported while the rest loads.
func TestLoadNounFile(t *testing.T) {
	r := ochazuke.NewRuntime()
	things := []ochazuke.Thing{{"id": "b1", "name": "front", "house": "h1"}}
	r.AddSource("bell.store", func() ochazuke.Store {
		return ochazuke.Store{Kind: ochazuke.KindCollection, List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) { return things, nil }}
	})
	var rang bellParams
	ochazuke.AddTypedHandler(r, "bell.ring", func(_ ochazuke.Ctx, p bellParams) ochazuke.Result { rang = p; return ochazuke.Done(nil) })
	r.AddHandler("bell.mute", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("muted") })
	r.AddHandler("bell.unused", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) })
	r.DefineGuard("bell-ok", func(*ochazuke.Runtime, ochazuke.KeyEvent) bool { return true })
	file := `{"$schema": "` + cli.NounSchemaURL + `", "name": "bell", "about": "A bell", "noEditKey": true, "parent": "house",
	  "props": [{"name": "id", "type": "string", "key": true}, {"name": "name", "type": "string", "role": "label"},
	            {"name": "house", "type": "string", "ref": ["house"]}, {"name": "owner", "ref": ["project", "task"], "role": "owner", "inherit": "house"}],
	  "store": {"source": "bell.store"},
	  "verbs": [
	    {"verb": "ring", "handler": "bell.ring", "description": "Ring it", "parameters": {"times": {"type": "string"}}},
	    {"verb": "mute", "handler": "bell.mute", "description": "Mute it", "when": "bell-ok", "agent": "allow"},
	    {"verb": "gone", "handler": "bell.nowhere"}
	  ],
	  "keys": [{"context": "bell/normal", "bindings": [{"key": "b", "action": "bell.ring", "args": {"times": 2}, "hint": "ring"}]}],
	  "settings": [{"name": "bell.loud", "type": "boolean", "default": false, "description": "Ring loudly"}],
	  "colour": "brass"}`
	problems := cli.LoadNouns(r, fstest.MapFS{"nouns/bell.json": {Data: []byte(file)}}, "nouns")
	kinds := map[string]string{}
	for _, p := range problems {
		kinds[p.Kind+" "+p.Row] = p.Text
	}
	for _, want := range []string{"unknown-key /colour", "missing-name bell.gone", "params bell.ring"} {
		if _, ok := kinds[want]; !ok {
			t.Errorf("no problem %q in %v", want, problems)
		}
	}
	n := r.Nouns["bell"]
	if n == nil || n.About != "A bell" || !n.NoEditKey || n.Parent != "house" || n.Store == nil {
		t.Fatalf("noun = %+v", n)
	}
	props := r.Props("bell")
	owner := props[slices.IndexFunc(props, func(p ochazuke.PropSpec) bool { return p.Name == "owner" })]
	if owner.Role != ochazuke.RoleOwner || !slices.Equal(owner.Ref, []string{"project", "task"}) || owner.Inherit != "house" {
		t.Errorf("owner prop = %+v", owner)
	}
	ring, ok := r.Operations.Find("bell.ring")
	if !ok || ring.Description != "Ring it" || ring.Parameters["times"].Type != "integer" || !ring.Parameters["times"].Required {
		t.Fatalf("ring = %+v", ring)
	}
	if _, ok := r.Operations.Find("bell.gone"); ok {
		t.Error("a verb whose handler is missing was declared")
	}
	mute, _ := r.Operations.Find("bell.mute")
	if mute.Agent != ochazuke.AgentAllow || r.VerbAvailability("bell.mute")[0] != "bell-ok" {
		t.Errorf("mute = %+v, guards %v", mute, r.VerbAvailability("bell.mute"))
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "bell", Verb: "ring", Args: ochazuke.Arguments{"times": 3, "tone": "low"}}); err != nil || rang != (bellParams{3, "low"}) {
		t.Errorf("ring ran with %+v, %v", rang, err)
	}
	// The file's row loads ahead of the generated key block, as a row bound
	// with the noun would.
	rows := r.Nouns["bell"].Bindings["normal"]
	own := slices.IndexFunc(rows, func(b ochazuke.Binding) bool { return b.Keys == "b" && b.Hint == "ring" })
	generated := slices.IndexFunc(rows, func(b ochazuke.Binding) bool { return b.Generated })
	if own < 0 || generated >= 0 && generated < own {
		t.Errorf("key row at %d, first generated row at %d", own, generated)
	}
	if _, err := r.Settings().Get("bell.loud"); err != nil {
		t.Error("the setting did not load")
	}
	unused, _ := r.Named("", "bell.unused")
	if len(unused.Rows) != 0 {
		t.Errorf("unused handler rows %v", unused.Rows)
	}
	h, _ := r.Named(ochazuke.NamedHandler, "bell.ring")
	if h.ParamType != "cli_test.bellParams" || !slices.Equal(h.Rows, []string{"bell.ring"}) || !strings.HasPrefix(h.At(), "ofam.dev/ochazuke/cli/nounfile_test.go:") {
		t.Errorf("ring handler = %+v (%s)", h, h.At())
	}
	got, err := r.Execute(ochazuke.Request{Noun: "ochazuke.handler", Verb: "get", Args: ochazuke.Arguments{"name": "bell.store"}})
	if err != nil || got.(map[string]any)["kind"] != ochazuke.NamedSource {
		t.Errorf("handler get = %v, %v", got, err)
	}
	if _, ok := r.SchemaOf(cli.NounFileKind); !ok {
		t.Error("the noun schema is not registered")
	}
}

// The published noun schema is the generated one (OCHAZUKE_SCHEMA_UPDATE=1
// writes it).
func TestNounSchemaIsCurrent(t *testing.T) {
	data, err := json.MarshalIndent(cli.NounFileSchema().Schema, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join("..", "docs", "schemas", "noun.v1.json")
	if os.Getenv("OCHAZUKE_SCHEMA_UPDATE") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(old, data) {
		t.Fatalf("%s is stale: OCHAZUKE_SCHEMA_UPDATE=1 go test -run TestNounSchemaIsCurrent ./cli (%v)", path, err)
	}
}
