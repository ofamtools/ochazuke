package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/cli/inspect"
	"github.com/unixsuperhero/oden"
)

func TestCatalogAgentMetadata(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Define("task").About("Work to do").Verb("delete", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "Delete work").Destructive().Agent("never").When("owned").Example(ochazuke.Arguments{"id": "t1"}, "Delete this task")
	r.AddVerbAvailability("task.delete", "ready")
	data, err := Catalog(r, "test", "v1", About("Tracks work"), AgentRules("Stay in your lane"))
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	doc.Nouns = doc.AppNouns()
	doc.Bindings = nil
	doc.Pages = nil
	doc.Aliases = nil
	golden, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/agent-catalog.golden")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(golden, '\n'), want) {
		t.Fatalf("catalog differs:\n%s", golden)
	}
	host, mounted, _ := mountHost(t, doc)
	if len(mounted.Warnings) != 0 {
		t.Fatal(mounted.Warnings)
	}
	again, err := Catalog(host, "host", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var got CatalogDoc
	json.Unmarshal(again, &got)
	for _, n := range got.Nouns {
		if n.Name == "ext-task" {
			if n.About != doc.Nouns[0].About || !reflect.DeepEqual(n.Verbs, doc.Nouns[0].Verbs) {
				t.Fatalf("mounted metadata changed: %+v", n)
			}
		}
	}
	var out bytes.Buffer
	sh := New(r, "test", nil, &out, nil, About("Tracks work"), AgentRules("Stay in your lane"))
	app := oden.New("test", []string{"catalog"})
	Mount(app, sh)
	if err := sh.catalogCommand(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"agentRules": "Stay in your lane"`)) {
		t.Fatal(out.String())
	}
	var input inspect.Catalog
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	merged := inspect.Merge([]inspect.Source{{Tool: inspect.ToolInfo{Name: "test"}, Catalog: &input}}, nil, nil, "test")
	if merged.Tools[0].About != doc.About {
		t.Fatal("inspector lost app about")
	}
	found := false
	for _, v := range merged.Verbs {
		if v.Name == "delete" && v.Agent == "never" {
			found = v.Destructive && len(v.Guards) == 2 && len(v.Examples) == 1
		}
	}
	if !found {
		t.Fatal("inspector lost verb metadata")
	}
}
