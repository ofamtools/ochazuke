package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

func TestCatalogSideDeclarePublish(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	command := []string{"{bin}", "view", "--repo", "{dir}"}
	first := CatalogSide{Name: "diff", Title: "Side diff", Key: "g", Command: command}
	if err := DeclareSide(r, first); err != nil {
		t.Fatal(err)
	}
	command[0] = "changed"
	second := CatalogSide{Name: "vim", Title: "Side vim", Launch: "vim-session"}
	if err := DeclareSide(r, second); err != nil {
		t.Fatal(err)
	}
	data, err := Catalog(r, "extension", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	first.Command = []string{"{bin}", "view", "--repo", "{dir}"}
	first.Launch = "command"
	if !reflect.DeepEqual(doc.Sides, []CatalogSide{first, second}) {
		t.Fatalf("published sides = %+v", doc.Sides)
	}
	if err := doc.ValidateSides(); err != nil {
		t.Fatal(err)
	}
	if len(r.PageList()) != 0 || len(r.Jobs()) != 0 {
		t.Fatal("declarations must not add pages or launch jobs")
	}
	again, err := Catalog(r, "extension", "v1")
	if err != nil || string(again) != string(data) {
		t.Fatalf("catalog is not deterministic: %v", err)
	}
	other := ochazuke.NewRuntime()
	other.Config().Disable()
	data, err = Catalog(other, "other", "v1")
	if err != nil || strings.Contains(string(data), `"sides"`) {
		t.Fatalf("sides leaked between runtimes: %v", err)
	}
}

func TestCatalogSideValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		side CatalogSide
		want string
	}{
		{"empty name", CatalogSide{Command: []string{"tool"}}, "name must be nonempty"},
		{"space name", CatalogSide{Name: " "}, "name must contain"},
		{"slash", CatalogSide{Name: "bad/name"}, "name must contain"},
		{"uppercase", CatalogSide{Name: "Diff"}, "name must contain"},
		{"underscore", CatalogSide{Name: "side_diff"}, "name must contain"},
		{"missing command", CatalogSide{Name: "diff"}, "command must be nonempty"},
		{"command launcher", CatalogSide{Name: "diff", Launch: "command"}, "command must be nonempty"},
		{"empty executable", CatalogSide{Name: "diff", Command: []string{" "}}, "executable must be nonempty"},
		{"unknown placeholder", CatalogSide{Name: "diff", Command: []string{"{unknown}"}}, "unknown side placeholder"},
		{"host placeholder", CatalogSide{Name: "diff", Launch: "custom", Command: []string{"{unknown}"}}, "unknown side placeholder"},
		{"unclosed placeholder", CatalogSide{Name: "diff", Command: []string{"tool", "{dir"}}, "unmatched {"},
		{"unopened placeholder", CatalogSide{Name: "diff", Command: []string{"tool", "dir}"}}, "unmatched }"},
		{"key sequence", CatalogSide{Name: "diff", Launch: "custom", Key: "g g"}, "single key name"},
		{"key whitespace", CatalogSide{Name: "diff", Launch: "custom", Key: "\tg"}, "single key name"},
		{"key control", CatalogSide{Name: "diff", Launch: "custom", Key: "\x1b"}, "single key name"},
		{"unknown key", CatalogSide{Name: "diff", Launch: "custom", Key: "nonsense"}, "unknown key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := ochazuke.NewRuntime()
			r.Config().Disable()
			for _, err := range []error{DeclareSide(r, tc.side), (CatalogDoc{Sides: []CatalogSide{tc.side}}).ValidateSides()} {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want %q", err, tc.want)
				}
			}
			if r.Instances[sidesID] != nil {
				t.Fatal("invalid declaration changed runtime")
			}
		})
	}
	for _, key := range []string{"", "g", "G", "ctrl+g", "shift+tab", "+", "é"} {
		side := CatalogSide{Name: "3d-diff", Key: key, Launch: "custom"}
		if err := (CatalogDoc{Sides: []CatalogSide{side}}).ValidateSides(); err != nil {
			t.Errorf("key %q: %v", key, err)
		}
	}
}

func TestCatalogSideDuplicate(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	side := CatalogSide{Name: "diff", Command: []string{"tool"}}
	if err := DeclareSide(r, side); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{DeclareSide(r, side), (CatalogDoc{Sides: []CatalogSide{side, side}}).ValidateSides()} {
		if err == nil || err.Error() != "side diff is declared twice" {
			t.Fatalf("duplicate = %v", err)
		}
	}
	if len(declaredSides(r)) != 1 {
		t.Fatal("duplicate declaration changed runtime")
	}
}

func TestCatalogSideDeclarationIDInUse(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	inst := r.Instance(sidesID)
	inst.Data = "existing data"
	err := DeclareSide(r, CatalogSide{Name: "diff", Command: []string{"tool"}})
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("instance collision = %v", err)
	}
	if inst.Data != "existing data" {
		t.Fatal("declaration overwrote an existing instance")
	}
}

func TestExpandSideCommand(t *testing.T) {
	t.Parallel()
	vars := map[string]string{
		"dir": "/tmp/a b", "pane": "pane-1", "session": "session-1",
		"project": "", "shell": "/bin/sh", "bin": "/tmp/o-git",
	}
	for _, tc := range []struct {
		name string
		cmd  []string
		want []string
	}{
		{"every placeholder", []string{"{dir}", "{pane}", "{session}", "{project}", "{shell}", "{bin}"}, []string{"/tmp/a b", "pane-1", "session-1", "", "/bin/sh", "/tmp/o-git"}},
		{"embedded repeated", []string{"--dir={dir}/{pane}/{dir}"}, []string{"--dir=/tmp/a b/pane-1//tmp/a b"}},
		{"no shell splitting", []string{"{bin}", "{dir}", "'a b'", "$HOME; echo x", ""}, []string{"/tmp/o-git", "/tmp/a b", "'a b'", "$HOME; echo x", ""}},
		{"nil", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := append([]string(nil), tc.cmd...)
			got, err := ExpandSideCommand(tc.cmd, vars)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("expanded = %q, %v, want %q", got, err, tc.want)
			}
			if !reflect.DeepEqual(tc.cmd, before) {
				t.Fatal("expansion mutated argv")
			}
		})
	}
	for _, arg := range []string{"{unknown}", "{}", "{{dir}}", "{dir", "dir}"} {
		if got, err := ExpandSideCommand([]string{arg}, vars); err == nil || got != nil {
			t.Errorf("%q = %q, %v; want error", arg, got, err)
		}
	}
	if _, err := ExpandSideCommand([]string{"{made-up}"}, map[string]string{"made-up": "x"}); err == nil {
		t.Fatal("host vars must not extend the placeholder vocabulary")
	}
	if _, err := ExpandSideCommand([]string{"{dir}"}, nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing value = %v", err)
	}
	got, err := ExpandSideCommand([]string{"{dir}"}, map[string]string{"dir": "{bin}; $(echo x)"})
	if err != nil || got[0] != "{bin}; $(echo x)" {
		t.Fatalf("replacement expanded recursively: %q, %v", got, err)
	}
}

func TestCatalogInspectSides(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	side := CatalogSide{Name: "diff", Title: "Side diff", Key: "g", Command: []string{"{bin}", "view", "{dir}"}}
	if err := DeclareSide(f.r, side); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "page.html")
	if _, stderr, err := f.tool(t.TempDir(), "catalog", "inspect", "--out", out); err != nil {
		t.Fatalf("%v: %s", err, stderr)
	}
	d := pageData(t, out)
	for _, tool := range d.Tools {
		if tool.Name != "tool" {
			continue
		}
		if len(tool.Sides) != 1 || tool.Sides[0].Name != side.Name || tool.Sides[0].Launch != "command" || !reflect.DeepEqual(tool.Sides[0].Command, side.Command) {
			t.Fatalf("inspector lost side: %+v", tool.Sides)
		}
		html, err := os.ReadFile(out)
		if err != nil || !strings.Contains(string(html), "<th>sides</th>") {
			t.Fatalf("inspector lacks side display: %v", err)
		}
		return
	}
	t.Fatal("inspector lost tool")
}
