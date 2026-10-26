package cli

import (
	"encoding/json"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// TestCatalogCarriesFree: a free sourced parameter (mtm's --model: choices
// from a source, any value accepted) publishes "free": true, and a closed
// sourced parameter writes no free key, so a host can tell them apart.
func TestCatalogCarriesFree(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Define("harness").Verb("models", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "List models")
	r.Define("task").Verb("launch", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "Launch")
	op := r.Operations["task.launch"]
	op.Parameters = map[string]ochazuke.Parameter{
		"model":   {Type: "string", Source: "harness.models", Free: true},
		"harness": {Type: "string", Source: "harness.models"},
	}
	r.Operations["task.launch"] = op
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
	if model["free"] != true || model["source"] != "harness.models" {
		t.Fatalf("free sourced param = %v; want free true", model)
	}
	if _, ok := harness["free"]; ok || harness["source"] != "harness.models" {
		t.Fatalf("closed sourced param = %v; want no free key", harness)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, n := range doc.Nouns {
		for _, v := range n.Verbs {
			if n.Name == "task" && (!v.Params["model"].Free || v.Params["harness"].Free) {
				t.Fatalf("decoded params %+v", v.Params)
			}
		}
	}
}
