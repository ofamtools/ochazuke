package namecheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func verbFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, text := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func verbCatalog() Catalog {
	return Catalog{
		Nouns:     map[string]string{"task": "task", "ochazuke.collection": "ochazuke.collection", "list": "ochazuke.collection", "ochazuke.focus": "ochazuke.focus", "focus": "ochazuke.focus", "pane": "pane"},
		Generated: map[string]bool{"set": true, "get": true, "rename": true, "restore": true, "move": true},
		Known: map[string]string{
			"task.set": "set", "task.get": "get", "task.rename": "rename", "task.status": "",
			"ochazuke.collection.move": "", "collection.move": "", "pane.restore": "",
			"ochazuke.focus.set": "", "focus.set": "", "ochazuke.focus.to": "", "focus.to": "",
		},
	}
}

func verbRemoved() []Removed {
	return []Removed{
		{Verb: "rename", Replacement: `edit {field: "@label"}`, Since: "R1"},
		{Verb: "move", Replacement: "edit {field: \"@owner\"}", Since: "R1"},
		{Verb: "restore", Replacement: "set", Since: "R1"},
		{Noun: "ochazuke.focus", Verb: "set", Replacement: "ochazuke.focus.to", Since: "R1"},
	}
}

func hitsByRef(rep VerbReport) map[string]string {
	m := map[string]string{}
	for _, h := range rep.Hits {
		m[h.Ref] = h.State
	}
	return m
}

func TestVerbsStates(t *testing.T) {
	root := verbFixture(t, map[string]string{
		"a.go": `package a
// Doc: task.set is fine; task.fiction in prose is not a finding.
var x = "task.rename"
var v = "task.nonesuch {id}"
var y = "collection.move and ochazuke.collection.move"
var z = ` + "`focus.set`" + `
var w = "pane.restore, other.move and task.status"
`,
		"b.json": `{"action": "task.rename", "file": "list.go", "host": "ofam.dev/x", "name": "task.alsoname"}` + "\n" + `{"noun": "task", "verb": "rename"} {"verb": "bogus", "noun": "task"}`,
		"c.md":   "Use `task.get` or `task.gone`. See bindings.json and path/task.rename. Sentence ends task.rename.\n\n```sh\n$ task.alsogone 1\n```\n",
	})
	rep, err := Verbs(VerbsConfig{Catalog: verbCatalog(), Removed: verbRemoved(), Root: root, Files: []string{"a.go", "b.json", "c.md"}, UnknownIn: []string{".go", ".md", ".json"}})
	if err != nil {
		t.Fatal(err)
	}
	got := hitsByRef(rep)
	want := map[string]string{
		"task.nonesuch": StateUnknown,
		"task.bogus":    StateUnknown, // a request written as noun and verb
		"task.gone":     StateUnknown, // a code span names it on purpose
		"task.alsogone": StateUnknown, // so does a fenced line
		"task.rename":   StatePending, // still registered: works now
		"focus.set":     StatePending,
		"other.move":    StatePending, // unknown noun; the framework still generates move
	}
	for ref, state := range want {
		if got[ref] != state {
			t.Errorf("%s = %q, want %q (all %v)", ref, got[ref], state, got)
		}
	}
	for _, fine := range []string{"task.fiction", "task.alsoname", "task.set", "task.get", "collection.move", "ochazuke.collection.move", "pane.restore", "task.status", "list.go", "bindings.json", "ofam.dev"} {
		if _, bad := got[fine]; bad {
			t.Errorf("%s was reported (%s)", fine, got[fine])
		}
	}
	if n := len(rep.Violations()); n != 4 {
		t.Errorf("violations %d, want 4: %v", n, rep.Violations())
	}
	if n := len(rep.Pending()); n != 6 { // a.go x3 (other.move), b.json x2, b.json x2, c.md x1 (path/task.rename is a path, not a reference)
		t.Errorf("pending %d: %v", n, rep.Pending())
	}
}

// Once the catalog lacks the verb, a reference is a violation that names the
// replacement; a noun's own verb of a removed name stays valid.
func TestVerbsRemovedAfterPurge(t *testing.T) {
	cat := verbCatalog()
	delete(cat.Known, "task.rename")
	delete(cat.Known, "ochazuke.focus.set")
	delete(cat.Known, "focus.set")
	delete(cat.Generated, "rename") // the purge took rename off the standard table
	root := verbFixture(t, map[string]string{"a.go": "package a\nvar x = \"task.rename\"\nvar y = \"focus.set collection.move\"\n"})
	rep, err := Verbs(VerbsConfig{Catalog: cat, Removed: verbRemoved(), Root: root, Files: []string{"a.go"}})
	if err != nil {
		t.Fatal(err)
	}
	got := hitsByRef(rep)
	if got["task.rename"] != StateRemoved || got["focus.set"] != StateRemoved || len(got) != 2 {
		t.Fatalf("hits %v", got)
	}
	msg := rep.Violations()[0].String()
	if !strings.Contains(msg, "a.go:2:") || !strings.Contains(msg, `use edit {field: "@label"}`) || !strings.Contains(msg, "since R1") {
		t.Fatalf("message %q", msg)
	}
	var fake fakeT
	VerbsCheck(&fake, VerbsConfig{Catalog: cat, Removed: verbRemoved(), Root: root, Files: []string{"a.go"}})
	if !fake.failed {
		t.Fatal("VerbsCheck passed a fixture naming task.rename")
	}
}

func TestVerbsChangelogHistoryIsExempt(t *testing.T) {
	cat := verbCatalog()
	delete(cat.Known, "task.rename")
	root := verbFixture(t, map[string]string{
		"CHANGELOG.md": "# Changelog\n\n## Unreleased\n\n- Removed task.rename in favour of edit.\n\n## v0.5.0 (2026-01-01)\n\n- Added task.rename.\n\n## v0.4.0\n\n- task.rename again.\n\n## 2025-12-31\n\n- task.rename once more.\n\n## Historical notes\n\n- task.rename in old notes.\n",
		"docs/x.md":    "task.rename\n",
	})
	rep, err := Verbs(VerbsConfig{Catalog: cat, Removed: verbRemoved(), Root: root, Files: []string{"CHANGELOG.md", "docs/x.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Hits) != 2 || rep.Hits[0].File != "CHANGELOG.md" || rep.Hits[0].Line != 5 || rep.Hits[1].File != "docs/x.md" {
		t.Fatalf("hits %v", rep.Hits)
	}
}

func TestVerbsSkipAllowAndExts(t *testing.T) {
	cat := verbCatalog()
	delete(cat.Known, "task.rename")
	root := verbFixture(t, map[string]string{
		"a.go":              "package a\nvar x = \"task.rename\"\n",
		"vendor/b.go":       "package b\nvar x = \"task.rename\"\n",
		"testdata/c.json":   `"task.rename"`,
		"x/testdata/e.json": `{"action": "task.rename"}`,
		"d.txt":             "task.rename",
	})
	all := []string{"a.go", "vendor/b.go", "testdata/c.json", "x/testdata/e.json", "d.txt"}
	rep, _ := Verbs(VerbsConfig{Catalog: cat, Removed: verbRemoved(), Root: root, Files: all, Skip: []string{"gen/"}})
	if len(rep.Hits) != 1 || rep.Hits[0].File != "a.go" {
		t.Fatalf("hits %v", rep.Hits)
	}
	rep, _ = Verbs(VerbsConfig{Catalog: cat, Removed: verbRemoved(), Root: root, Files: all, Skip: []string{"gen/"}, Allow: []string{"task.rename"}})
	if len(rep.Hits) != 0 {
		t.Fatalf("allowed: %v", rep.Hits)
	}
}

// Files default to what git tracks, so an untracked build output is not read.
func TestVerbsReadsTrackedFiles(t *testing.T) {
	cat := verbCatalog()
	delete(cat.Known, "task.rename")
	root := verbFixture(t, map[string]string{"in.go": "package a\nvar x = \"task.rename\"\n", "out.go": "package a\nvar x = \"task.rename\"\n"})
	for _, args := range [][]string{{"init", "-q"}, {"add", "in.go"}} {
		cmd := gitCmd(root, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}
	rep, err := Verbs(VerbsConfig{Catalog: cat, Removed: verbRemoved(), Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Hits) != 1 || rep.Hits[0].File != "in.go" {
		t.Fatalf("hits %v", rep.Hits)
	}
}

type fakeT struct {
	testing.TB
	failed bool
}

func (f *fakeT) Helper()               {}
func (f *fakeT) Logf(string, ...any)   {}
func (f *fakeT) Fatalf(string, ...any) { f.failed = true }

func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd
}
