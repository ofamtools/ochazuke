package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/oden"
)

// mountedHelp runs a demo tool (one noun of its own, the framework's nouns
// registered) with args and returns what it printed.
func mountedHelp(t *testing.T, args ...string) string {
	t.Helper()
	f := newFixture(t)
	f.r.Define("ochazuke.binding")
	var out bytes.Buffer
	app := oden.New("tool", args, oden.WithBuiltins(), oden.WithStdio(strings.NewReader(""), &out, io.Discard), oden.WithPath(t.TempDir()))
	Mount(app, New(f.r, "tool", strings.NewReader(""), &out, io.Discard))
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, &out)
	}
	return out.String()
}

// Default help lists the tool's own commands and nouns, the generic CLI
// commands and one line for the framework namespace: no framework noun
// rows, no internal rows, no shadow lines, brief notes.
func TestDefaultHelpListsOwnCommandsOnly(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		got := mountedHelp(t, args...)
		for _, want := range []string{"widget", "completion", "version", "which", "Namespaces:", "ochazuke., ochazuke", "tool help --all, or tool ochazuke", "tool help --all' for every command"} {
			if !strings.Contains(got, want) {
				t.Errorf("%v: help lacks %q:\n%s", args, want, got)
			}
		}
		for _, bad := range []string{"ochazuke.binding", "ochazuke.about", "__", "shadows", "Each call uses a fresh local runtime"} {
			if strings.Contains(got, bad) {
				t.Errorf("%v: default help has %q:\n%s", args, bad, got)
			}
		}
		if n := strings.Count(got, "\n"); n > 45 {
			t.Errorf("%v: default help is %d lines:\n%s", args, n, got)
		}
	}
}

// help --all prints everything: the framework's nouns, internal ones, the
// shadow lines and the full notes.
func TestHelpAllListsFrameworkNouns(t *testing.T) {
	for _, args := range [][]string{{"help", "--all"}, {"--help", "--all"}, {"-h", "--all"}} {
		got := mountedHelp(t, args...)
		for _, want := range []string{"ochazuke.binding", "ochazuke.__picker-state", "Each call uses a fresh local runtime", "widget"} {
			if !strings.Contains(got, want) {
				t.Errorf("%v: --all lacks %q:\n%s", args, want, got)
			}
		}
		if strings.Contains(got, "Namespaces:") {
			t.Errorf("%v: --all is the flat listing", args)
		}
	}
}

// The namespace's own help lists its nouns, internal ones left out.
func TestNamespaceHelpListsItsNouns(t *testing.T) {
	for _, args := range [][]string{{"ochazuke"}, {"ochazuke."}} {
		got := mountedHelp(t, args...)
		if !strings.Contains(got, "binding") || strings.Contains(got, "__picker-state") {
			t.Errorf("%v:\n%s", args, got)
		}
	}
}

// helpNames reduces help to its command-name column: the lines of the
// listing, without the summaries and sources that move with the code.
func helpNames(help string) string {
	var out []string
	for _, l := range strings.Split(help, "\n") {
		if strings.HasPrefix(l, "  ") && !strings.HasPrefix(l, "   ") {
			l, _, _ = strings.Cut(strings.TrimSpace(l), "  ")
		}
		if strings.HasPrefix(l, "   ") && strings.TrimSpace(l) != "" {
			continue // a shadow line: it carries source locations
		}
		out = append(out, strings.TrimRight(l, " "))
	}
	return strings.Join(out, "\n")
}

func TestHelpGoldens(t *testing.T) {
	for name, args := range map[string][]string{"help-default": {"help"}, "help-all": {"help", "--all"}} {
		got := helpNames(mountedHelp(t, args...))
		golden := filepath.Join("testdata", name+".golden")
		if *update {
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(golden)
		if err != nil || string(want) != got {
			t.Errorf("%s differs (go test ./cli -run TestHelpGoldens -update):\n%s", golden, got)
		}
	}
}

// A noun with no verbs has nothing to run from the CLI: --all lists it,
// default help does not.
func TestVerblessNounIsNotADefaultHelpRow(t *testing.T) {
	f := newFixture(t)
	f.r.Define("blankpage")
	run := func(args ...string) string {
		var out bytes.Buffer
		app := oden.New("tool", args, oden.WithBuiltins(), oden.WithStdio(strings.NewReader(""), &out, io.Discard), oden.WithPath(t.TempDir()))
		Mount(app, New(f.r, "tool", strings.NewReader(""), &out, io.Discard))
		if err := app.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := run("help"); strings.Contains(got, "blankpage") || !strings.Contains(got, "widget") || !strings.Contains(got, "  app ") {
		t.Errorf("default help:\n%s", got)
	}
	if got := run("help", "--all"); !strings.Contains(got, "blankpage") {
		t.Errorf("--all:\n%s", got)
	}
}
