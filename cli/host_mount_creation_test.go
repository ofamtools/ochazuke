package cli

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"ofam.dev/ochazuke"
)

func TestMountKeepsCustomCreationWithDependentChoices(t *testing.T) {
	producer := ochazuke.NewRuntime()
	producer.Config().Disable()
	producer.Define("specimen").Props(
		ochazuke.PropSpec{Name: "id", Key: true, ReadOnly: true},
		ochazuke.PropSpec{Name: "path", ReadOnly: true, Parameter: ochazuke.Parameter{Type: "path", Path: "dir"}},
	).Store(ochazuke.Store{Facts: true})
	producer.Define("specimen").Verb("names", func(ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Done([]string{"seed"})
	}).Params(map[string]ochazuke.Parameter{"scope": {Type: "path", Path: "dir"}})
	params := map[string]ochazuke.Parameter{
		"name":  {Type: "string", Required: true},
		"scope": {Type: "path", Path: "dir", Required: true},
		"from":  {Type: "string", Source: "specimen.names", Free: true, SourceArgs: map[string]string{"scope": "scope"}},
	}
	create := func(c ochazuke.Ctx) ochazuke.Result {
		return ochazuke.Done(fmt.Sprintf("%s:%s:%s", c.Args["scope"], c.Args["name"], c.Args["from"]))
	}
	producer.Define("specimen").Verb("create", create).Params(params).Positional("name", "from").Creates()
	producer.Define("shipment").Verb("add", create).Params(params).Positional("name", "from").Creates()
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
						p.Source, p.Free = "", true
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
				value, err := host.Execute(ochazuke.Request{Noun: "lab." + noun, Verb: verb, Args: ochazuke.Arguments{"name": "new", "scope": scope, "from": "seed"}})
				if err != nil || value != scope+":new:seed" {
					t.Fatalf("original application handler returned %#v %v", value, err)
				}
			}
		})
	}
}
