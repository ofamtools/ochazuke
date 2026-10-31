package cli

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/grid"
	"github.com/unixsuperhero/ochazuke/markdown"
)

func TestExtensionNamespace(t *testing.T) {
	for bin, want := range map[string]string{
		"o-ai":                 "ai",
		"o-qbt":                "qbt",
		"/usr/local/bin/o-git": "git",
		"O-CSV":                "csv",
		"mtm-mermaid":          "mtm-mermaid",
		"nabe":                 "nabe",
		"o-o-x":                "o-x",
	} {
		if got, err := ExtensionNamespace(bin); err != nil || got != want {
			t.Errorf("ExtensionNamespace(%q) = %q, %v; want %q", bin, got, err, want)
		}
	}
	for _, bin := range []string{"", "o-", "ochazuke", "o-ochazuke", "app", "o-app", "o-settings", "list", "o-9x", "o-a.b", "o-a_b"} {
		if got, err := ExtensionNamespace(bin); err == nil {
			t.Errorf("ExtensionNamespace(%q) = %q, want an error", bin, got)
		}
	}
}

// Every framework noun's short name is refused, including those of the
// framework packages a host adds (commands, page groups, grid, markdown).
func TestExtensionNamespaceRefusesFrameworkNames(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	m := ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	grid.Register(r)
	markdown.Register(r)
	if _, err := AddCommands(m, &fakeCommands{}, CommandsConfig{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtensionNamespace("o-page-group"); err == nil {
		t.Error("page-group (GroupNoun, a MountPage groups rail) is accepted")
	}
	for _, a := range r.Aliases() {
		if got, err := ExtensionNamespace("o-" + a.Alias); err == nil {
			t.Errorf("framework short name %q (%s) is accepted as %q", a.Alias, a.Noun, got)
		}
	}
}

// Include lazy framework constructors, not just NewRuntime/NewModel: apps
// compare both runtime registrations and catalog metadata for these verbs.
func TestFrameworkCatalogDeclaresHistory(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	m := ochazuke.NewModel(r)
	ochazuke.NewPicker(r)
	grid.Register(r)
	markdown.Register(r)
	if _, err := AddCommands(m, &fakeCommands{}, CommandsConfig{}); err != nil {
		t.Fatal(err)
	}
	New(r, "tool", nil, io.Discard, io.Discard)
	data, err := Catalog(r, "tool", "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, noun := range doc.Nouns {
		for _, verb := range noun.Verbs {
			key := noun.Name + "." + verb.Name
			op, ok := r.Operations[key]
			if !ok {
				t.Errorf("%s is in the catalog without a registration", key)
				continue
			}
			if !verb.Builtin {
				continue
			}
			seen[key] = true
			if !op.Builtin || op.HistoryKind == "" || op.HistoryKind != verb.History {
				t.Errorf("%s: catalog history %q, declared %q, builtin %t", key, verb.History, op.HistoryKind, op.Builtin)
			}
		}
	}
	for key, op := range r.Operations {
		if op.Builtin && !seen[key] {
			t.Errorf("framework verb %s missing from catalog", key)
		}
	}
	for _, key := range []string{"ochazuke.catalog.describe", "ochazuke.catalog.list", "ochazuke.mcp.tools", "ochazuke.agent.rights"} {
		if op, ok := r.Operations[key]; !ok || op.HistoryKind != ochazuke.HistoryFetch {
			t.Errorf("%s: want explicitly registered fetch, got %+v", key, op)
		}
	}
}
