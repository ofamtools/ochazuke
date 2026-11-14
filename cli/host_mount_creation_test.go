package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

func TestMountKeepsCustomCreationWithDependentChoices(t *testing.T) {
	producer := ochazuke.NewRuntime()
	producer.Config().Disable()
	records := map[string]map[string]ochazuke.Thing{"specimen": {}, "shipment": {}}
	for noun, things := range records {
		producer.Define(noun).Props(
			ochazuke.PropSpec{Name: "id", Key: true, ReadOnly: true},
			ochazuke.PropSpec{Name: "path", ReadOnly: true, Parameter: ochazuke.Parameter{Type: "path", Path: "dir"}},
			ochazuke.PropSpec{Name: "origin", ReadOnly: true},
		).Store(ochazuke.Store{
			Facts: true,
			Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
				return things[id], nil
			},
			List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
				out := make([]ochazuke.Thing, 0, len(things))
				for _, thing := range things {
					out = append(out, thing)
				}
				return out, nil
			},
		})
	}
	producer.Define("specimen").Verb("names", func(ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Done([]string{"seed"})
	}).Params(map[string]ochazuke.Parameter{"scope": {Type: "path", Path: "dir"}})
	params := map[string]ochazuke.Parameter{
		"name":  {Type: "string", Required: true},
		"scope": {Type: "path", Path: "dir", Required: true},
		"from":  {Type: "string", Source: "specimen.names", Accept: ochazuke.AcceptKeep, SourceArgs: map[string]string{"scope": "scope"}},
	}
	create := func(noun string) ochazuke.AppHandler {
		return func(c ochazuke.Ctx) ochazuke.Result {
			id := ochazuke.StringArg(c.Args, "name", "")
			thing := ochazuke.Thing{"id": id, "path": c.Args["scope"], "origin": c.Args["from"]}
			records[noun][id] = thing
			return ochazuke.Done(thing)
		}
	}
	producer.Define("specimen").Verb("create", create("specimen")).Params(params).Positional("name", "from").Creates()
	producer.Define("shipment").Verb("add", create("shipment")).Params(params).Positional("name", "from").Creates()
	data, err := Catalog(producer, "lab", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, disableSource := range []bool{false, true} {
		t.Run(fmt.Sprint("disable-source=", disableSource), func(t *testing.T) {
			host := ochazuke.NewRuntime()
			host.Config().Disable()
			mounted := MountNouns(host, doc.AppNouns(), HostMount{
				Local: func(noun string) string { return "lab." + noun },
				Param: func(p ochazuke.Parameter) ochazuke.Parameter {
					if disableSource && p.Source != "" {
						p.Source, p.Accept = "", ochazuke.AcceptKeep
					}
					return p
				},
				Proxy: func(noun string, v CatalogVerb) any {
					return func(c ochazuke.Ctx) ochazuke.Result {
						value, err := producer.Execute(ochazuke.Request{Noun: noun, Verb: v.Name, Args: c.Args})
						if err != nil {
							return ochazuke.Failed(err)
						}
						return ochazuke.Done(value)
					}
				},
			})
			if len(mounted.Warnings) != 0 {
				t.Fatal(mounted.Warnings)
			}
			for noun, verb := range map[string]string{"specimen": "create", "shipment": "add"} {
				op, ok := host.Operation("lab." + noun + "." + verb)
				if !ok {
					t.Fatalf("custom %s.%s was removed", noun, verb)
				}
				wantSource := "lab.specimen.names"
				if disableSource {
					wantSource = ""
				}
				if op.Standard != "" || !op.Creates || op.Parameters["from"].Source != wantSource || !reflect.DeepEqual(op.Parameters["from"].SourceArgs, map[string]string{"scope": "scope"}) || op.Parameters["scope"].Type != "path" || op.Parameters["scope"].Path != "dir" || !reflect.DeepEqual(op.Positional, []string{"name", "from"}) {
					t.Fatalf("custom operation metadata: %+v", op)
				}
				scope := t.TempDir()
				if _, err := host.Execute(ochazuke.Request{Noun: "lab." + noun, Verb: verb, Args: ochazuke.Arguments{"name": "new", "scope": scope, "from": "seed"}}); err != nil {
					t.Fatalf("create %s: %v", noun, err)
				}
				var stdout, stderr bytes.Buffer
				shell := New(host, "host", strings.NewReader(""), &stdout, &stderr)
				if err := shell.Run(t.Context(), []string{"lab." + noun, "list"}); err != nil {
					t.Fatalf("list created %s: %v %s", noun, err, stderr.String())
				}
				var listed ochazuke.ListAnswer
				if err := json.Unmarshal(stdout.Bytes(), &listed); err != nil {
					t.Fatalf("decode list: %v %s", err, stdout.String())
				}
				if len(listed.Items) != 1 || listed.Items[0]["id"] != "new" || listed.Items[0]["path"] != scope || listed.Items[0]["origin"] != "seed" {
					t.Fatalf("host did not list the created %s: %+v", noun, listed)
				}
			}
		})
	}
}
