package cli

import (
	"encoding/json"
	"testing"

	"ofam.dev/ochazuke"
)

// TestCatalogCarriesParameterNoun: a parameter naming a noun publishes the
// noun, and the noun's list verb as its source when the runtime reads the
// choices from it, so a host lists the same options; a noun with no list
// verb publishes the noun alone; the runtime's own Parameter is untouched.
func TestCatalogCarriesParameterNoun(t *testing.T) {
	r := ochazuke.NewRuntime()
	done := func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	r.Define("skill").Verb("list", done, "List skills")
	r.Define("widget")
	r.Define("task").Verb("launch", done, "Launch")
	op := r.Operations["task.launch"]
	op.Parameters = map[string]ochazuke.Parameter{
		"skill":  {Type: "string", Noun: "skill"},
		"widget": {Type: "string", Noun: "widget"},
	}
	r.Operations["task.launch"] = op
	data, err := Catalog(r, "tool", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var params map[string]ochazuke.Parameter
	for _, n := range doc.Nouns {
		for _, v := range n.Verbs {
			if n.Name == "task" && v.Name == "launch" {
				params = v.Params
			}
		}
	}
	if p := params["skill"]; p.Noun != "skill" || p.Source != "skill.list" {
		t.Fatalf("skill param = %+v; want noun skill, source skill.list", p)
	}
	if p := params["widget"]; p.Noun != "widget" || p.Source != "" {
		t.Fatalf("widget param = %+v; want noun widget, no source", p)
	}
	if r.Operations["task.launch"].Parameters["skill"].Source != "" {
		t.Fatal("publishing the catalog changed the runtime's parameter")
	}
}
