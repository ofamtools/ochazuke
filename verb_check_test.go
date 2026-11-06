package ochazuke

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke/namecheck"
)

// The catalog knows every spelling of a noun and tells a verb the framework
// generated (task.rename) from a noun's own verb of the same word.
func TestVerbCatalog(t *testing.T) {
	t.Parallel()
	r, _ := newDerivedRuntime(t)
	c := r.VerbCatalog()
	cp := &c
	for ref, std := range map[string]string{"task.rename": "rename", "task.patch": "patch", "task.set": "set", "task.title": "", "ochazuke.list.move": "", "list.move": "", "ochazuke.focus.set": ""} {
		if got, ok := c.Known[ref]; !ok || got != std {
			t.Errorf("Known[%s] = %q, %v; want %q", ref, got, ok, std)
		}
	}
	if c.Nouns["list"] != "ochazuke.list" || c.Nouns["ochazuke.list"] != "ochazuke.list" || c.Nouns["task"] != "task" {
		t.Fatalf("nouns %v", c.Nouns)
	}
	// The thing verbs are pending (registered, going); a noun's own verb stays.
	rm := r.RemovedVerbs()
	for ref, want := range map[string]string{
		"task.rename": namecheck.StatePending, "task.patch": namecheck.StatePending, "list.move": "",
		"ochazuke.focus.set": namecheck.StatePending, "worker.rename": namecheck.StatePending, "pane.nonesuch": namecheck.StateUnknown,
	} {
		noun, verb := SplitKey(ref)
		if got, _ := cp.Classify(noun, verb, rm); got != want {
			t.Errorf("%s = %q, want %q", ref, got, want)
		}
	}
}

// bindings check names the rows of the user's files that use a removed verb
// and what to write instead: both bindings formats, an actions.json alias
// and an action's call.
func TestBindingsCheckReportsRemovedVerbs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, bindings string }{
		{"grouped", `{"bindings": [{"context": "app/normal", "bindings": {"R": "task.rename", "M": "task.move", "q": "task.set"}}]}`},
		{"rows", `[{"noun": "app", "mode": "normal", "keys": "R", "request": {"noun": "task", "verb": "rename"}}, {"noun": "app", "mode": "normal", "keys": "M", "request": {"noun": "task", "verb": "move"}}, {"noun": "app", "mode": "normal", "keys": "q", "request": {"noun": "task", "verb": "set"}}]`},
	} {
		r, _ := newDerivedRuntime(t)
		dir := t.TempDir()
		r.Config().Dir = dir
		writeConfig(t, dir, BindingsFile, tc.bindings)
		writeConfig(t, dir, ActionsFile, `[{"alias": "task.rn", "action": "task.rename"}, {"noun": "task", "verb": "retitle", "call": "task.rename {{id}}"}, {"alias": "task.mine", "action": "task.set"}]`)
		got, err := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "check"})
		if err != nil {
			t.Fatal(err)
		}
		out := strings.Join(got.([]string), "\n")
		for _, want := range []string{
			`warning: bindings.json:`, `task.rename is a removed verb (since R1); use edit {field: "@label"}`,
			`task.move is a removed verb`, `@owner`,
			`actions.json: alias 1 (task.rn): task.rename is a removed verb`,
			`actions.json: action 2 (task.retitle) call: task.rename is a removed verb`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %q in:\n%s", tc.name, want, out)
			}
		}
		if strings.Contains(out, "task.set is a removed") || strings.Contains(out, "task.mine") {
			t.Errorf("%s: reported a good row:\n%s", tc.name, out)
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
	got, _ := r.Execute(Request{Noun: "ochazuke.bindings", Verb: "check"})
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
