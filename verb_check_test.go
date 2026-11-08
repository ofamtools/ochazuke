package ochazuke

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/namecheck"
)

// The catalog knows every spelling of a noun and tells a verb the framework
// generated (task.set) from a noun's own verb of the same word. The purged
// verbs are gone from it: a reference to one is removed; a noun's own verb
// of the word stays.
func TestVerbCatalog(t *testing.T) {
	t.Parallel()
	r, _ := newDerivedRuntime(t)
	c := r.VerbCatalog()
	cp := &c
	for ref, std := range map[string]string{"task.set": "set", "task.edit": "edit", "task.title": "", "ochazuke.list.move": "", "list.move": "", "ochazuke.focus.to": ""} {
		if got, ok := c.Known[ref]; !ok || got != std {
			t.Errorf("Known[%s] = %q, %v; want %q", ref, got, ok, std)
		}
	}
	for _, ref := range []string{"task.rename", "task.patch", "task.move", "task.duplicate", "task.restore"} {
		if _, ok := c.Known[ref]; ok {
			t.Errorf("Known has the purged %s", ref)
		}
	}
	if c.Nouns["list"] != "ochazuke.list" || c.Nouns["ochazuke.list"] != "ochazuke.list" || c.Nouns["task"] != "task" {
		t.Fatalf("nouns %v", c.Nouns)
	}
	rm := r.RemovedVerbs()
	for ref, want := range map[string]string{
		"task.rename": namecheck.StateRemoved, "task.patch": namecheck.StateRemoved, "list.move": "", "task.set": "",
		"ochazuke.focus.set": namecheck.StateRemoved, "worker.rename": namecheck.StateRemoved, "pane.nonesuch": namecheck.StateUnknown,
	} {
		noun, verb := SplitKey(ref)
		if got, _ := cp.Classify(noun, verb, rm); got != want {
			t.Errorf("%s = %q, want %q", ref, got, want)
		}
	}
}

// bindings check names the rows of the user's files that use a purged
// verb and what to write instead: both bindings formats, an actions.json
// alias and an action's call. It warns of set's deprecated parameters.
func TestBindingsCheckReportsRemovedVerbs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, bindings string }{
		{"grouped", `{"bindings": [{"context": "app/normal", "bindings": {"R": "task.rename", "M": "task.move", "q": "task.set"}}, {"context": "app/normal", "bindings": [{"key": "V", "action": "task.set", "args": {"prop": "status", "value": "done"}}, {"key": "T", "action": "task.set", "args": {"prop": "archived", "toggle": true}}]}]}`},
		{"rows", `[{"noun": "app", "mode": "normal", "keys": "R", "request": {"noun": "task", "verb": "rename"}}, {"noun": "app", "mode": "normal", "keys": "M", "request": {"noun": "task", "verb": "move"}}, {"noun": "app", "mode": "normal", "keys": "q", "request": {"noun": "task", "verb": "set"}}, {"noun": "app", "mode": "normal", "keys": "V", "request": {"noun": "task", "verb": "set", "args": {"prop": "status", "value": "done"}}}, {"noun": "app", "mode": "normal", "keys": "T", "request": {"noun": "task", "verb": "set", "args": {"prop": "archived", "toggle": true}}}]`},
	} {
		r, _ := newDerivedRuntime(t)
		dir := t.TempDir()
		r.Config().Dir = dir
		writeConfig(t, dir, BindingsFile, tc.bindings)
		writeConfig(t, dir, ActionsFile, `[{"alias": "task.rn", "action": "task.rename"}, {"noun": "task", "verb": "retitle", "call": "task.rename {{id}}"}, {"alias": "task.mine", "action": "task.set"}, {"alias": "task.old", "action": "task.set", "args": {"changes": {"status": "done"}}}]`)
		got, err := r.Execute(Request{Noun: "ochazuke.binding", Verb: "check"})
		if err != nil {
			t.Fatal(err)
		}
		out := strings.Join(got.([]string), "\n")
		for _, want := range []string{
			`task.rename is a removed verb (since R1); use edit {field: "@label"}`,
			`task.move is a removed verb`, `@owner`,
			`actions.json: alias 1 (task.rn): task.rename is a removed verb`,
			`actions.json: action 2 (task.retitle) call: task.rename is a removed verb`,
			`warning: bindings.json:`, `V: task.set: parameter prop with value of set is deprecated (removed in R2); use props {prop: value}`,
			`warning: actions.json: alias 4 (task.old): task.set: parameter changes of set is deprecated (removed in R2); use props`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %q in:\n%s", tc.name, want, out)
			}
		}
		if strings.Contains(out, "warning: bindings.json: row 1 R") || strings.Contains(out, "task.set is a removed") || strings.Contains(out, "task.mine") || strings.Contains(out, " T: ") {
			t.Errorf("%s: a purged verb reads as pending, or a good row was reported:\n%s", tc.name, out)
		}
	}
}

// After the purge the same rows are errors that still say what to use. An
// application's own verb that a release removes is a row of its own.
func TestBindingsCheckRemovedVerbAfterPurge(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	r.Define("task").Props(derivedProps()...).Store((&derivedStore{key: "id"}).store())
	if err := r.AddRemovedVerbs(namecheck.Removed{Noun: "task", Verb: "bump", Replacement: "task.set {prop: priority}", Since: "v2"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	r.Config().Dir = dir
	writeConfig(t, dir, BindingsFile, `[{"noun": "app", "mode": "normal", "keys": "B", "request": {"noun": "task", "verb": "bump"}}]`)
	got, _ := r.Execute(Request{Noun: "ochazuke.binding", Verb: "check"})
	out := strings.Join(got.([]string), "\n")
	if !strings.Contains(out, "bindings.json: row 1 B: task.bump is a removed verb (since v2); use task.set {prop: priority}") || strings.Contains(out, "warning: bindings.json: row 1 B: task.bump") {
		t.Fatalf("check:\n%s", out)
	}
}

func TestAddRemovedVerbs(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	if err := r.AddRemovedVerbs(namecheck.Removed{Noun: "task", Verb: "bump"}); err == nil {
		t.Fatal("a row without a replacement was accepted")
	}
	if err := r.AddRemovedVerbs(namecheck.Removed{Noun: "task", Verb: "bump", Replacement: "task.set"}); err != nil {
		t.Fatal(err)
	}
	if n := len(r.RemovedVerbs()); n != len(frameworkRemovedVerbs())+1 || len(NewRuntime().RemovedVerbs()) != len(frameworkRemovedVerbs()) {
		t.Fatalf("removed %d", n)
	}
}

// The generated set is read off a probe noun, so it follows the standard
// table: the verbs a release purges leave it when they leave the table.
func TestGeneratedVerbsFollowTheTable(t *testing.T) {
	t.Parallel()
	got := generatedVerbs()
	for _, std := range StandardVerbs() {
		if !got[std] && std != "watch" {
			t.Errorf("standard verb %s is not generated: %v", std, got)
		}
	}
	if got["list.move"] || got["probe"] {
		t.Fatalf("generated %v", got)
	}
}

// A line the removed list answers heads no parameter: the slot under it
// is not the misread of its words (note read as another noun's verb note,
// associate as that verb's argument), however the line is read.
func TestRemovedLineHeadsNoParameter(t *testing.T) {
	r := NewRuntime()
	r.Define("note").Props(derivedProps()...).Store((&derivedStore{key: "id"}).store())
	r.Define("doc").Verb("note", func(Ctx) Result { return Done(nil) }, "Note a doc").Positional("id", "text")
	op := r.Operations.At(Key("doc.note"))
	op.Parameters = map[string]Parameter{"id": {Type: "string", Description: "The doc instance"}, "text": {Type: "string", Description: "The note text"}}
	r.Operations.Set(op)
	if err := r.AddRemovedVerbs(namecheck.Removed{Noun: "note", Verb: "associate", Replacement: "note.set {props: {project: ID}}", Since: "v2"}); err != nil {
		t.Fatal(err)
	}
	p := NewPalette(r)
	paletteRequest(t, r, "open", "palette", nil)
	for _, line := range []string{"note associate ", "note associate x "} {
		paletteRequest(t, r, "fill", "palette", Arguments{"text": line})
		if c := p.parse(line); c.Err == nil || !strings.Contains(c.Err.Error(), "removed") {
			t.Fatalf("%q: %v", line, c.Err)
		}
		if slot, ok := p.currentSlot(); ok {
			t.Fatalf("%q heads %s %s", line, slot.Op.Key.Short(), slot.Name)
		}
	}
}
