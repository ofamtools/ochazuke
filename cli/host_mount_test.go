package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/oden"
)

// hostTestCatalog is an extension's catalog: a task noun with props and a
// store (so it has generated verbs), a hand-written verb, a preset alias
// (done = set status done), a bare alias, and a project noun the task
// references.
func hostTestCatalog(t *testing.T) CatalogDoc {
	t.Helper()
	r := ochazuke.NewRuntime()
	things := map[string]ochazuke.Thing{"t1": {"id": "t1", "title": "Water", "status": "todo", "project": "home"}}
	r.Define("project").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "name", Role: ochazuke.RoleLabel}).
		Store(ochazuke.Store{List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			return []ochazuke.Thing{{"id": "home", "name": "Home"}}, nil
		}})
	r.Define("task").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "status", Parameter: ochazuke.Parameter{Enum: []string{"todo", "done"}}, When: "extension-only-guard"},
		ochazuke.PropSpec{Name: "project", Role: ochazuke.RoleOwner, Parameter: ochazuke.Parameter{Noun: "project"}},
	).Store(ochazuke.Store{
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) { return things[id], nil },
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			return []ochazuke.Thing{things["t1"]}, nil
		},
		Patch: func(_ ochazuke.Ctx, id string, c ochazuke.Thing) (ochazuke.Thing, error) { return things[id], nil },
	}).Verb("launch", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }, "Launch a task").Positional()
	if err := r.AddActionAlias(ochazuke.ActionAlias{Alias: "task.done", Action: "task.set", Args: ochazuke.Arguments{"prop": "status", "value": "done"}}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddActionAlias(ochazuke.ActionAlias{Alias: "go!", Action: "task.launch"}); err != nil {
		t.Fatal(err)
	}
	data, err := Catalog(r, "ext", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

type hostCall struct {
	noun, verb string
	args       ochazuke.Arguments
}

// mountHost mounts the catalog's app nouns as ext-NOUN in a fresh host
// runtime that already has a task noun of its own; calls records what the
// proxies were asked to run.
func mountHost(t *testing.T, doc CatalogDoc) (*ochazuke.Runtime, MountedNouns, *[]hostCall) {
	t.Helper()
	r := ochazuke.NewRuntime()
	r.Define("project").Verb("open", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) })
	var calls []hostCall
	got := MountNouns(r, doc.AppNouns(), HostMount{
		Local: func(noun string) string { return "ext-" + noun },
		Proxy: func(noun string, v CatalogVerb) any {
			return func(c ochazuke.Ctx) ochazuke.Result {
				calls = append(calls, hostCall{noun, v.Name, c.Args})
				return ochazuke.Done(map[string]any{"ran": noun + "." + v.Name})
			}
		},
	})
	return r, got, &calls
}

// A mounted noun gets its props and its generated verbs, marked standard,
// with no host code per noun: the host names references by its own
// names and drops the extension's guards.
func TestMountNounsPropsAndStandardVerbs(t *testing.T) {
	doc := hostTestCatalog(t)
	r, got, calls := mountHost(t, doc)
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings: %v", got.Warnings)
	}
	if got.Nouns["task"] != "ext-task" || got.Nouns["project"] != "ext-project" {
		t.Fatalf("nouns %v", got.Nouns)
	}
	props := r.Props("ext-task")
	if len(props) != 4 || !props[0].Key || props[1].Role != ochazuke.RoleLabel {
		t.Fatalf("props %+v", props)
	}
	if p, _ := r.PropOf("ext-task", "project"); p.Noun != "ext-project" || p.Role != ochazuke.RoleOwner {
		t.Fatalf("a reference names the host's noun: %+v", p)
	}
	if p, _ := r.PropOf("ext-task", "status"); p.When != "" {
		t.Fatalf("the extension's guard is the extension's: %+v", p)
	}
	for verb, standard := range map[string]string{"get": "get", "list": "list", "set": "set", "rename": "rename", "move": "move", "launch": ""} {
		op, ok := r.Operations["ext-task."+verb]
		if !ok || op.Standard != standard {
			t.Fatalf("ext-task.%s: %v %q, want standard %q", verb, ok, op.Standard, standard)
		}
	}
	if op := r.Operations["ext-task.set"]; !op.Many || op.HistoryKind != ochazuke.HistoryChange {
		t.Fatalf("set keeps the catalog's flags: %+v", op)
	}
	if p := r.Operations["ext-task.move"].Parameters["to"]; p.Noun != "ext-project" || p.Source != "ext-project.list" {
		t.Fatalf("move's to names the host's project: %+v", p)
	}
	// A prop-shaped request runs the forwarded set, as on the extension.
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-task", Prop: "status", Value: "done", Args: ochazuke.Arguments{"id": "t1"}}); err != nil {
		t.Fatal(err)
	}
	if last := (*calls)[len(*calls)-1]; last.verb != "set" || last.args["prop"] != "status" || last.args["value"] != "done" {
		t.Fatalf("prop request forwarded as %+v", last)
	}
	// The host's own nouns are untouched.
	if _, ok := r.Operations["project.open"]; !ok {
		t.Fatal("the host's project lost its verb")
	}
}

// The catalog's aliases are registered again under the host names: patch
// (standard), a preset alias, and a bare alias under its noun.
func TestMountNounsReregistersAliases(t *testing.T) {
	r, got, calls := mountHost(t, hostTestCatalog(t))
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings: %v", got.Warnings)
	}
	for alias, action := range map[string]string{"ext-task.patch": "ext-task.set", "ext-task.done": "ext-task.set", "ext-task.go!": "ext-task.launch"} {
		a, ok := r.ActionAlias(alias)
		if !ok || a.Action != action {
			t.Fatalf("%s: %+v %v", alias, a, ok)
		}
	}
	if a, _ := r.ActionAlias("ext-task.patch"); a.Source != ochazuke.AliasStandard {
		t.Fatalf("patch stays a standard alias: %+v", a)
	}
	if _, ok := r.ActionAlias("go!"); ok {
		t.Fatal("an extension's bare alias must not take a global name in the host")
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "ext-task", Verb: "done", Args: ochazuke.Arguments{"id": "t1"}}); err != nil {
		t.Fatal(err)
	}
	if last := (*calls)[len(*calls)-1]; last.verb != "set" || last.args["value"] != "done" || last.args["prop"] != "status" {
		t.Fatalf("done runs set with its presets: %+v", last)
	}
}

// A bad verb, prop or alias is skipped with a warning; the rest mounts and
// nothing panics. A noun whose host name is taken is skipped too.
func TestMountNounsSkipsBadEntriesWithAWarning(t *testing.T) {
	doc := hostTestCatalog(t)
	nouns := doc.AppNouns()
	for i := range nouns {
		if nouns[i].Name != "task" {
			continue
		}
		nouns[i].Verbs = append(nouns[i].Verbs,
			CatalogVerb{Name: "title", Description: "clashes with the prop title"},
			CatalogVerb{Name: "odd", History: "sometimes"},
			CatalogVerb{Name: "pos", Positional: []string{"missing"}},
			CatalogVerb{Name: "ghost", Aliases: []ochazuke.ActionAlias{{Alias: "nowhere.ghost", Noun: "nowhere", Verb: "ghost"}}})
		nouns[i].Props = append(nouns[i].Props, ochazuke.PropSpec{Name: "bad name"})
	}
	nouns = append(nouns, CatalogNoun{Name: "taken", Verbs: []CatalogVerb{{Name: "x"}}})
	r := ochazuke.NewRuntime()
	r.Define("ext-taken").Verb("y", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) })
	got := MountNouns(r, nouns, HostMount{
		Local: func(noun string) string { return "ext-" + noun },
		Proxy: func(string, CatalogVerb) any {
			return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
		},
	})
	text := strings.Join(got.Warnings, "\n")
	for _, want := range []string{"task title skipped", "task prop bad name skipped", `task odd: declares history "sometimes"`, "task pos: positional missing names no parameter", "alias nowhere.ghost skipped", "noun taken skipped"} {
		if !strings.Contains(text, want) {
			t.Errorf("warnings lack %q:\n%s", want, text)
		}
	}
	if _, ok := r.Operations["ext-task.title"]; ok {
		t.Fatal("the clashing verb was mounted")
	}
	for _, key := range []string{"ext-task.set", "ext-task.odd", "ext-task.pos", "ext-task.ghost", "ext-taken.y"} {
		if _, ok := r.Operations[key]; !ok {
			t.Fatalf("%s should be mounted", key)
		}
	}
	if _, ok := r.Operations["ext-taken.x"]; ok {
		t.Fatal("a taken noun's verbs were mounted into the host's noun")
	}
}

// helpOut runs Mount on args against r and returns what it printed.
func helpOut(t *testing.T, r *ochazuke.Runtime, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	app := oden.New("tool", args, oden.WithStdio(strings.NewReader(""), &out, &errOut), oden.WithPath(t.TempDir()))
	Mount(app, New(r, "tool", strings.NewReader(""), &out, io.Discard))
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, errOut.String())
	}
	return out.String()
}

// A noun's help lists its standard verbs on one line (owner decision 24):
// the named verbs keep their entries and details; cli.standard-verbs list
// gives each standard verb its own entry again. Every one still runs.
func TestHelpListsStandardVerbsOnOneLine(t *testing.T) {
	r, _, calls := mountHost(t, hostTestCatalog(t))
	help := helpOut(t, r, "ext-task", "--help")
	line := ""
	for _, l := range strings.Split(help, "\n") {
		if strings.HasPrefix(l, "Standard verbs") {
			line = l
		}
	}
	for _, verb := range []string{"get", "list", "set (patch)", "rename", "move", "watch"} {
		if !strings.Contains(line, " "+verb) {
			t.Fatalf("the standard verbs line lacks %s: %q\n%s", verb, line, help)
		}
	}
	if !strings.Contains(help, "  launch, go!") || !strings.Contains(help, "  done ") {
		t.Fatalf("named verbs and preset aliases keep their entries:\n%s", help)
	}
	if strings.Contains(help, "  rename ") || strings.Contains(help, "  get ") || strings.Contains(help, "--where <string>") {
		t.Fatalf("standard verbs are not listed one by one:\n%s", help)
	}
	if sub := helpOut(t, r, "ext-task", "set", "--help"); !strings.Contains(sub, "--prop <string>") {
		t.Fatalf("a standard verb's own help is whole:\n%s", sub)
	}
	helpOut(t, r, "ext-task", "rename", "t1", "Fern")
	if last := (*calls)[len(*calls)-1]; last.verb != "rename" {
		t.Fatalf("a standard verb still runs from the CLI: %+v", last)
	}
	var stdout, top bytes.Buffer
	app := oden.New("tool", nil, oden.WithStdio(strings.NewReader(""), &stdout, &top), oden.WithPath(t.TempDir()))
	Mount(app, New(r, "tool", strings.NewReader(""), &stdout, io.Discard))
	_ = app.Run(context.Background()) // no command word: the listing, exit 1
	seen := false
	for _, l := range strings.Split(top.String(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "ext-task ") {
			seen = true
			if !strings.Contains(l, "launch done · std get list watch set rename move") {
				t.Fatalf("the noun's summary names its own verbs first: %q", l)
			}
		}
	}
	if !seen {
		t.Fatalf("no ext-task line:\n%s", top.String())
	}
	if err := r.Settings().Set(ochazuke.CLIStandardVerbsSetting, ochazuke.CLIStandardVerbsList); err != nil {
		t.Fatal(err)
	}
	listed := helpOut(t, r, "ext-task", "--help")
	if strings.Contains(listed, "Standard verbs") || !strings.Contains(listed, "  rename ") {
		t.Fatalf("cli.standard-verbs list lists each verb:\n%s", listed)
	}
	if !slices.Contains(strings.Fields(listed), "--prop") && !strings.Contains(listed, "--prop <string>") {
		t.Fatalf("list keeps each verb's details:\n%s", listed)
	}
}
