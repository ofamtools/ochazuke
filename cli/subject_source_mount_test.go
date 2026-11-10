package cli

import (
	"encoding/json"
	"reflect"
	"testing"

	"ofam.dev/ochazuke"
)

func TestMountedSubjectSourceCatalogPaletteAndCLI(t *testing.T) {
	source := ochazuke.NewRuntime()
	source.Define("parcel").Verb("edit", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }).
		Params(map[string]ochazuke.Parameter{"id": {Type: "string", Noun: "parcel", Focus: true, From: "row:parcel"}})
	raw, err := Catalog(source, "source", "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	r := ochazuke.NewRuntime()
	r.Config().Dir = t.TempDir()
	var got []string
	mounted := MountNouns(r, doc.AppNouns(), HostMount{
		Local: func(n string) string { return "extension." + n },
		Proxy: func(string, CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				id := ochazuke.StringArg(c.Args, "id", "")
				if id == "" {
					return ochazuke.Failed(c.Need("id", "choose a parcel"))
				}
				got = append(got, id)
				return ochazuke.Done(nil)
			}
		},
	})
	if len(mounted.Warnings) != 0 {
		t.Fatal(mounted.Warnings)
	}
	op, ok := r.Operation("extension.parcel.edit")
	if !ok || op.Parameters["id"].From != "row:parcel" || op.Parameters["id"].Noun != "extension.parcel" || !op.Parameters["id"].Focus {
		t.Fatalf("mounted subject declaration %+v", op.Parameters)
	}
	r.Define("item")
	if _, err := r.AddList("items", []ochazuke.ListItem{{ID: "item-row", Noun: "item", Label: "Related parcel", Data: "foreign-handle", Props: ochazuke.Thing{"parcel": "p1"}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("item-row"); err != nil {
		t.Fatal(err)
	}
	ochazuke.NewPalette(r)
	for _, q := range []ochazuke.Request{
		{Noun: "palette", Verb: "open"},
		{Noun: "palette", Verb: "fill", Args: ochazuke.Arguments{"text": "extension.parcel edit"}},
		{Noun: "palette", Verb: "run"},
	} {
		if _, err := r.Execute(q); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(got, []string{"p1"}) || r.TopModal() != "" {
		t.Fatalf("palette got %v, modal %q", got, r.TopModal())
	}
	if _, _, err := oX(t, r, "extension.parcel", "edit"); err == nil || len(got) != 1 {
		t.Fatalf("targetless CLI borrowed focus: got %v, err %v", got, err)
	}
	if _, stderr, err := oX(t, r, "extension.parcel", "edit", "--target", "item-row"); err != nil {
		t.Fatalf("targeted CLI: %v\n%s", err, stderr)
	}
	if _, stderr, err := oX(t, r, "extension.parcel", "edit", "--id", "explicit"); err != nil {
		t.Fatalf("explicit CLI: %v\n%s", err, stderr)
	}
	if !reflect.DeepEqual(got, []string{"p1", "p1", "explicit"}) {
		t.Fatalf("CLI got %v", got)
	}
}
