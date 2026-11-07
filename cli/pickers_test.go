package cli

import (
	"encoding/json"
	"slices"
	"testing"

	"ofam.dev/ochazuke"
)

// An extension's declared pickers travel in its catalog (the framework's
// never do); the host mounts them under its own nouns, prefixed, and
// picker.list and the pickers picker show them.
func TestCatalogPickersRoundTrip(t *testing.T) {
	ext := ochazuke.NewRuntime()
	ext.Config().Disable()
	ext.Define("run").NoPicker().
		Verb("recent", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done([]string{"r1", "r2"}) }, "Recent runs").
		Verb("show", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "Show a run")
	if err := ext.AddPicker(ochazuke.PickerDef{Name: "runs", Title: "Runs", Noun: "run", Source: "run.recent", Pick: "run.show", Match: "substring"}); err != nil {
		t.Fatal(err)
	}
	data, err := Catalog(ext, "o-cmds", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Pickers) != 1 || doc.Pickers[0].Name != "runs" || doc.Pickers[0].Source != "run.recent" {
		t.Fatalf("catalog pickers = %+v", doc.Pickers)
	}
	if i := slices.IndexFunc(doc.Nouns, func(n CatalogNoun) bool { return n.Name == "run" }); i < 0 || !doc.Nouns[i].NoPicker {
		t.Fatalf("catalog noun run lost NoPicker")
	}

	host := ochazuke.NewRuntime()
	host.Config().Disable()
	host.Define("ext-run").
		Verb("recent", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done([]string{"r1", "r2"}) }).
		Verb("show", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) })
	if err := MountCatalogPickers(host, doc, "o-cmds", func(n string) string { return "ext-" + n }); err != nil {
		t.Fatal(err)
	}
	def, ok := host.PickerDef("o-cmds-runs")
	if !ok || def.Noun != "ext-run" || def.Source != "ext-run.recent" || def.Pick != "ext-run.show" {
		t.Fatalf("host picker = %+v %v", def, ok)
	}
	v, err := host.Execute(ochazuke.Request{Noun: ochazuke.PickerNoun, Verb: "list"})
	if err != nil {
		t.Fatal(err)
	}
	choices, err := ochazuke.ParseChoices(v)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(choices, func(c ochazuke.Choice) bool { return c.ID == "o-cmds-runs" }) {
		t.Fatalf("picker.list %v", choices)
	}
	c, err := host.OpenNamedPicker(ochazuke.PickersPickerName, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(c.Matches(), func(row ochazuke.ChooserRow) bool { return row.ID == "o-cmds-runs" }) {
		t.Fatal("the pickers picker lacks the mounted picker")
	}
}
