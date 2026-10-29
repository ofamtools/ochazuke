package ochazuke

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docFixture: doc.write {force} records its args and the name it ran by.
func docFixture(t *testing.T) (*Runtime, *[]string) {
	t.Helper()
	r := NewRuntime()
	var runs []string
	r.Define("doc").Verb("write", func(c Ctx) Result {
		runs = append(runs, c.Invoked.Name+" force="+jsonText(c.Args["force"]))
		return Done(nil)
	}, "Write the doc")
	op := r.Operations["doc.write"]
	op.Parameters = map[string]Parameter{"force": {Type: "boolean"}}
	r.Operations["doc.write"] = op
	r.Instance("d1", Kind("doc"))
	if err := r.SetFocus("d1"); err != nil {
		t.Fatal(err)
	}
	return r, &runs
}

// An alias carries preset args; a bare one (no noun) takes its action's
// noun and is reached by its one word; a typed argument wins.
func TestAliasPresetArgsAndBareNames(t *testing.T) {
	r, runs := docFixture(t)
	if err := r.AddActionAlias(ActionAlias{Alias: "w!", Action: "doc.write", Args: Arguments{"force": true}}); err != nil {
		t.Fatal(err)
	}
	a, ok := r.ActionAlias("w!")
	if !ok || !a.Bare || a.Alias != "doc.w!" || a.Name() != "w!" {
		t.Fatalf("bare alias %+v %v", a, ok)
	}
	for _, q := range []Request{{Verb: "w!"}, {Noun: "w!"}, {Noun: "doc", Verb: "w!"}, Do("w!").Request} {
		if _, err := r.Execute(q); err != nil {
			t.Fatalf("%+v: %v", q, err)
		}
	}
	if _, err := r.Execute(Request{Verb: "w!", Args: Arguments{"force": false}}); err != nil {
		t.Fatal(err)
	}
	want := "doc.w! force=true;doc.w! force=true;doc.w! force=true;doc.w! force=true;doc.w! force=false"
	if got := strings.Join(*runs, ";"); got != want {
		t.Fatalf("runs %s", got)
	}
	if op, inv, ok := r.ResolveAction("w!"); !ok || op.Key() != "doc.write" || !inv.Alias {
		t.Fatalf("ResolveAction: %v %+v", ok, inv)
	}
	if err := r.AddActionAlias(ActionAlias{Alias: "w2", Action: "doc.write", Args: Arguments{"nope": 1}}); err == nil || !strings.Contains(err.Error(), "unknown argument") {
		t.Fatalf("bad preset args: %v", err)
	}
	if err := r.AddActionAlias(ActionAlias{Alias: "a.b!", Action: "doc.write"}); err == nil {
		t.Fatal("a dotted name with no noun is refused")
	}
}

// aliases.shadow: a bare alias named like a verb is refused by default,
// loaded with allow, and then the palette's word runs the alias.
func TestAliasShadowSetting(t *testing.T) {
	r, runs := docFixture(t)
	r.Define("other").Verb("save", func(Ctx) Result { return Done(nil) })
	if err := r.AddActionAlias(ActionAlias{Alias: "save", Action: "doc.write", Args: Arguments{"force": true}}); err == nil || !strings.Contains(err.Error(), "would shadow the verb other.save") {
		t.Fatalf("refuse: %v", err)
	}
	r.EnsureConfigSettings()
	if err := r.Settings().Set(AliasShadowSetting, "allow"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddActionAlias(ActionAlias{Alias: "save", Action: "doc.write", Args: Arguments{"force": true}}); err != nil {
		t.Fatalf("allow: %v", err)
	}
	NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "save")
	if got := strings.Join(*runs, ";"); got != "doc.save force=true" {
		t.Fatalf("palette save: %s", got)
	}
}

// The palette: a bare alias typed whole runs with its preset args; a
// dotted alias's args are preset too (noun verb words).
func TestPaletteAliasArgs(t *testing.T) {
	r, runs := docFixture(t)
	for _, a := range []ActionAlias{
		{Alias: "w!", Action: "doc.write", Args: Arguments{"force": true}},
		{Alias: "doc.overwrite", Action: "doc.write", Args: Arguments{"force": true}},
	} {
		if err := r.AddActionAlias(a); err != nil {
			t.Fatal(err)
		}
	}
	NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "w!")
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "doc overwrite")
	if got := strings.Join(*runs, ";"); got != "doc.w! force=true;doc.overwrite force=true" {
		t.Fatalf("runs %s", got)
	}
}

// actions.json alias rows take args and bare names; a binding row names a
// bare alias by its full name.
func TestUserAliasRowsWithArgs(t *testing.T) {
	r, runs := docFixture(t)
	dir := t.TempDir()
	r.Config().Dir = dir
	if err := os.WriteFile(filepath.Join(dir, ActionsFile), []byte(`[{"alias": "w!", "action": "doc.write", "args": {"force": true}}, {"alias": "x", "action": "doc.write", "args": {"force": "yes"}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	problems := r.EnsureUserActions()
	if len(problems) != 1 || !strings.Contains(problems[0], "alias 2 (x)") {
		t.Fatalf("problems %q", problems)
	}
	if _, err := r.LoadBindings([]byte(`[{"noun": "doc", "keys": "W", "request": {"noun": "doc", "verb": "w!"}}]`)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Dispatch(MacroKey{Key: "W"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*runs, ";"); got != "doc.w! force=true" {
		t.Fatalf("runs %s", got)
	}
}

// A preset is part of a request, not a whole one: an alias whose action
// has a required parameter the presets leave out loads, and the missing
// one is asked for when it runs (the args form for a key or the palette,
// "NAME is required" for the API); given, it runs. An unknown or
// wrongly typed preset is still refused at load.
func TestAliasPartialPresetArgs(t *testing.T) {
	r := NewRuntime()
	var runs []string
	r.Define("doc").Verb("write", func(c Ctx) Result {
		runs = append(runs, c.Invoked.Name+" path="+jsonText(c.Args["path"])+" force="+jsonText(c.Args["force"]))
		return Done(nil)
	}, "Write the doc")
	op := r.Operations["doc.write"]
	op.Parameters = map[string]Parameter{"path": {Type: "string", Required: true}, "force": {Type: "boolean"}}
	r.Operations["doc.write"] = op
	r.Instance("d1", Kind("doc"))
	if err := r.SetFocus("d1"); err != nil {
		t.Fatal(err)
	}
	r.EnsureConfigSettings()
	if err := r.AddActionAlias(ActionAlias{Alias: "w!", Action: "doc.write", Args: Arguments{"force": true}}); err != nil {
		t.Fatalf("a partial preset loads: %v", err)
	}
	for _, bad := range []Arguments{{"nope": true}, {"force": "yes"}} {
		if err := r.AddActionAlias(ActionAlias{Alias: "w2", Action: "doc.write", Args: bad}); err == nil {
			t.Fatalf("preset %v loaded", bad)
		}
	}
	if _, err := r.Execute(Request{Verb: "w!"}); err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("API without path: %v", err)
	}
	if _, err := r.Execute(Request{Verb: "w!", Args: Arguments{"path": "a.txt"}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runs, ";"); got != `doc.w! path="a.txt" force=true` {
		t.Fatalf("runs %s", got)
	}
	// A key on the alias opens the args form for path; filled, it runs
	// with the preset.
	if _, err := r.LoadBindings([]byte(`[{"noun": "doc", "keys": "W", "request": {"noun": "doc", "verb": "w!"}}]`)); err != nil {
		t.Fatal(err)
	}
	formKeys(t, r, "W")
	if r.TopModal() != ArgsFormID || r.Focus != ArgsFormID+"/path" {
		t.Fatalf("key without path: top %q focus %q", r.TopModal(), r.Focus)
	}
	typeKeys(t, r, "b.txt")
	formKeys(t, r, "enter")
	if got := strings.Join(runs, ";"); got != `doc.w! path="a.txt" force=true;doc.w! path="b.txt" force=true` {
		t.Fatalf("runs %s (top %q)", got, r.TopModal())
	}
	// The palette: the one word opens the form; a typed path runs.
	NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "w!")
	if r.TopModal() != ArgsFormID || r.Focus != ArgsFormID+"/path" {
		t.Fatalf("palette w!: top %q focus %q", r.TopModal(), r.Focus)
	}
	formKeys(t, r, "esc", "esc")
	paletteLine(t, r, "w! c.txt")
	if got := runs[len(runs)-1]; got != `doc.w! path="c.txt" force=true` {
		t.Fatalf("palette w! c.txt ran %s", got)
	}
}
