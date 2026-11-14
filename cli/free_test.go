package cli

import (
	"encoding/json"
	"testing"

	"ofam.dev/ochazuke"
)

// TestCatalogCarriesFree: a free sourced parameter (mtm's --model: choices
// from a source, any value accepted) publishes "accept": "keep" and, for
// one release, "free": true; a closed sourced parameter writes neither,
// so a host can tell them apart; a catalog's free: true still reads.
func TestCatalogCarriesFree(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Define("harness").Verb("models", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "List models")
	r.Define("task").Verb("launch", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "Launch")
	op := r.Operations.At(ochazuke.Key("task.launch"))
	op.Parameters = map[string]ochazuke.Parameter{
		"model":   {Type: "string", Source: "harness.models", Accept: ochazuke.AcceptKeep},
		"harness": {Type: "string", Source: "harness.models"},
	}
	r.Operations.Set(op)
	data, err := Catalog(r, "tool", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Nouns []struct {
			Name  string `json:"name"`
			Verbs []struct {
				Name   string                     `json:"name"`
				Params map[string]json.RawMessage `json:"params"`
			} `json:"verbs"`
		} `json:"nouns"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	var params map[string]json.RawMessage
	for _, n := range raw.Nouns {
		for _, v := range n.Verbs {
			if n.Name == "task" && v.Name == "launch" {
				params = v.Params
			}
		}
	}
	var model, harness map[string]any
	if err := json.Unmarshal(params["model"], &model); err != nil {
		t.Fatalf("model param: %v (%s)", err, data)
	}
	if err := json.Unmarshal(params["harness"], &harness); err != nil {
		t.Fatalf("harness param: %v", err)
	}
	if model["accept"] != "keep" || model["free"] != true || model["source"] != "harness.models" {
		t.Fatalf("free sourced param = %v; want accept keep and, for one release, free", model)
	}
	_, accepts := harness["accept"]
	if _, free := harness["free"]; accepts || free || harness["source"] != "harness.models" {
		t.Fatalf("closed sourced param = %v; want no accept or free key", harness)
	}
	// A catalog written before accept says free: true; it still reads as
	// a parameter that takes any value.
	var old ochazuke.Parameter
	if err := json.Unmarshal([]byte(`{"type":"string","source":"harness.models","free":true}`), &old); err != nil || !old.TakesAny() {
		t.Fatalf("old free param %+v %v", old, err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, n := range doc.Nouns {
		for _, v := range n.Verbs {
			if n.Name == "task" && (!v.Params["model"].TakesAny() || v.Params["harness"].TakesAny()) {
				t.Fatalf("decoded params %+v", v.Params)
			}
		}
	}
}
