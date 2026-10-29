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
