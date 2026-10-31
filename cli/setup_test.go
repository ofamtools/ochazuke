package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/setup"
	"github.com/unixsuperhero/oden"
)

type setupFixture struct {
	r         *ochazuke.Runtime
	home, bin string
}

func newSetupFixture(t *testing.T) *setupFixture {
	t.Helper()
	home := t.TempDir()
	home, _ = filepath.EvalSymlinks(home)
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		t.Setenv(key, filepath.Join(home, key))
	}
	r := ochazuke.NewRuntime()
	r.Config().Dir = filepath.Join(home, ".config", "tool")
	r.Config().StateDir = filepath.Join(home, ".state")
	r.Config().NoHistoryFile = true
	bin := filepath.Join(home, "tool")
	if err := os.WriteFile(bin, []byte("binary"), 0700); err != nil {
		t.Fatal(err)
	}
	items := []setup.Item{{ID: "rules", Name: "Agent rules", Kind: setup.Block, Path: "~/AGENTS.md", Content: "Use tool.\n"}, {ID: "shell", Name: "Shell", Kind: setup.Block, Path: "~/.zshrc", Content: `eval "$(tool shell-init zsh)"`, OptIn: true, Group: "shell"}}
	if err := DeclareSetup(r, items, setup.Options{Tool: "tool", Bin: bin, Home: home}); err != nil {
		t.Fatal(err)
	}
	return &setupFixture{r: r, home: home, bin: bin}
}
func (f *setupFixture) run(t *testing.T, input string, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	app := oden.New("tool", args, oden.WithStdio(strings.NewReader(input), &out, &errOut), oden.WithPath(t.TempDir()))
	Mount(app, New(f.r, "tool", strings.NewReader(input), &out, &errOut))
	err := app.Run(context.Background())
	return out.String(), errOut.String(), err
}

func TestSetupMountStatusAndJSON(t *testing.T) {
	f := newSetupFixture(t)
	out, stderr, err := f.run(t, "", "setup")
	if err != nil || !strings.Contains(out, "rules: missing") || !strings.Contains(out, "shell: manual — eval") || !strings.HasSuffix(out, "run: tool setup install\n") {
		t.Fatalf("%v\n%s\n%s", err, out, stderr)
	}
	out, _, err = f.run(t, "", "setup", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []setup.Status
	if err = json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 2 || rows[0].ID != "rules" || rows[1].State != setup.Manual {
		t.Fatalf("%v %s", err, out)
	}
	// The schema and ordering stay stable; normalize only the fixture home.
	got := strings.ReplaceAll(out, f.home, "$HOME")
	want := `[
  {
    "id": "rules",
    "name": "Agent rules",
    "kind": "block",
    "path": "$HOME/AGENTS.md",
    "content": "Use tool.\n",
    "match": {},
    "state": "missing",
    "next": "tool setup install rules",
    "cells": [
      "rules",
      "block",
      "missing",
      "$HOME/AGENTS.md",
      "tool setup install rules"
    ]
  },
  {
    "id": "shell",
    "name": "Shell",
    "kind": "block",
    "path": "$HOME/.zshrc",
    "content": "eval \"$(tool shell-init zsh)\"",
    "match": {},
    "group": "shell",
    "opt_in": true,
    "state": "manual",
    "next": "eval \"$(tool shell-init zsh)\"",
    "cells": [
      "shell",
      "block",
      "manual",
      "$HOME/.zshrc",
      "eval \"$(tool shell-init zsh)\""
    ]
  }
]
`
	if got != want {
		t.Fatalf("setup JSON changed:\n%s", got)
	}
}

func TestSetupMountVerbsAndConfirmation(t *testing.T) {
	f := newSetupFixture(t)
	for _, args := range [][]string{{"setup", "list"}, {"setup", "show", "rules"}, {"setup", "plan"}, {"setup", "copy", "rules"}, {"setup", "--help"}} {
		out, stderr, err := f.run(t, "", args...)
		if err != nil || out == "" {
			t.Fatalf("%v: %v %s %s", args, err, out, stderr)
		}
	}
	if out, _, err := f.run(t, "", "setup", "copy", "rules"); err != nil || out != "Use tool.\n\n" {
		t.Fatalf("copy: %q %v", out, err)
	}
	if _, _, err := f.run(t, "n\n", "setup", "install"); err == nil {
		t.Fatal("declined install succeeded")
	}
	if _, err := os.Stat(filepath.Join(f.home, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("declined install wrote a file")
	}
	if out, stderr, err := f.run(t, "", "setup", "install", "--yes"); err != nil || !strings.Contains(out, "rules: installed") {
		t.Fatalf("install: %v %s %s", err, out, stderr)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatal("bare install edited shell rc")
	}
	if out, stderr, err := f.run(t, "", "setup", "install", "shell", "--yes", "--group", "shell"); err != nil || !strings.Contains(out, "shell: installed") {
		t.Fatalf("named opt-in: %v %s %s", err, out, stderr)
	}
	if _, _, err := f.run(t, "", "setup", "uninstall", "rules", "shell", "--yes"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.home, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("uninstall did not remove new rules file")
	}
}

func TestSetupMountHomeUserItemsAndSkip(t *testing.T) {
	f := newSetupFixture(t)
	home := t.TempDir()
	home, _ = filepath.EvalSymlinks(home)
	dir := filepath.Join(home, ".config", "tool")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "setup.json"), []byte(`[{"id":"user","name":"User rules","kind":"block","path":"~/USER.md","content":"User content"}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Settings().Set(SetupSkipSetting, `["rules"]`); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := f.run(t, "", "setup", "install", "--home", home, "--yes", "--json")
	if err != nil {
		t.Fatalf("%v %s %s", err, out, stderr)
	}
	if !strings.Contains(out, `"state": "skipped"`) || !strings.Contains(out, `"id": "user"`) {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(home, "USER.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.home, "USER.md")); !os.IsNotExist(err) {
		t.Fatal("home override leaked")
	}
}

func TestSetupRegistryHistoryAndNoKeys(t *testing.T) {
	f := newSetupFixture(t)
	noun := f.r.Noun("setup")
	if noun == nil || len(noun.Bindings) != 0 {
		t.Fatal("setup has keys")
	}
	for _, v := range setupVerbs {
		op := f.r.Operations[f.r.CanonicalKey("setup."+v.name)]
		want := ochazuke.HistoryFetch
		if v.name == "install" || v.name == "uninstall" {
			want = ochazuke.HistoryChange
		}
		if op.HistoryKind != want || !op.Builtin {
			t.Fatalf("%s history %q builtin %v", v.name, op.HistoryKind, op.Builtin)
		}
	}
	sh := New(f.r, "tool", nil, &bytes.Buffer{}, &bytes.Buffer{})
	if err := sh.Run(context.Background(), []string{"setup", "show", "rules"}); err != nil {
		t.Fatal("registry shell", err)
	}
	for _, args := range [][]string{{"setup", "list", "--format", "table"}, {"setup", "install", "--help"}} {
		out, stderr, err := f.run(t, "", args...)
		if err != nil {
			t.Fatalf("%v: %v %s %s", args, err, out, stderr)
		}
		if args[1] == "list" && (!strings.Contains(out, "Item") || !strings.Contains(out, "Next step") || strings.Contains(out, "opt_in")) {
			t.Fatalf("table ignores declared columns: %s", out)
		}
		if args[1] == "install" && !strings.Contains(out, "--group") {
			t.Fatalf("setup help omitted parameters: %s", out)
		}
	}
}

func TestSetupAbsentAndAppCommandWins(t *testing.T) {
	for _, declared := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "declared"}[declared], func(t *testing.T) {
			r := ochazuke.NewRuntime()
			r.Config().Disable()
			if declared {
				r = newSetupFixture(t).r
			}
			var out bytes.Buffer
			app := oden.New("tool", []string{"setup"}, oden.WithStdio(strings.NewReader(""), &out, &out), oden.WithPath(t.TempDir()))
			if declared {
				app.Add(oden.Command{Name: "setup", Run: func(context.Context, []string) error { _, err := out.WriteString("application setup"); return err }})
			}
			Mount(app, New(r, "tool", nil, &out, &out))
			entry := app.Catalog().Lookup("setup")
			if !declared {
				if entry != nil {
					t.Fatal("mounted without declarations")
				}
				return
			}
			if err := app.Run(context.Background()); err != nil || out.String() != "application setup" {
				t.Fatalf("%v %s", err, out.String())
			}
		})
	}
}

func TestSetupApplicationNounKeepsShortName(t *testing.T) {
	f := newSetupFixture(t)
	f.r.DefineOwn("setup").Verb("ping", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("application setup") })
	out, stderr, err := f.run(t, "", "setup", "ping")
	if err != nil || !strings.Contains(out, "application setup") {
		t.Fatalf("app noun lost its name: %v %s %s", err, out, stderr)
	}
	out, stderr, err = f.run(t, "", "ochazuke", "setup", "status", "--json")
	if err != nil || !strings.Contains(out, `"id": "rules"`) {
		t.Fatalf("framework setup became unreachable: %v %s %s", err, out, stderr)
	}
}
