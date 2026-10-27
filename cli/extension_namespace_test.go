package cli

import (
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
