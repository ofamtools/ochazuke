package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/oden"
)

// aliasFixture is the widget fixture with display (a permanent alias of
// show), base.add (an old name of paint) and gadget.show (show reachable
// under another noun).
func aliasFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.r.Define("gadget")
	for _, a := range []ochazuke.ActionAlias{
		{Alias: "widget.display", Action: "widget.show"},
		{Alias: "widget.base.add", Action: "widget.paint", Since: "v0.27.0", RemoveAfter: "v0.28.0"},
		{Alias: "gadget.show", Action: "widget.show"},
	} {
		if err := f.r.AddActionAlias(a); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *fixture) mounted(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	app := oden.New("tool", args, oden.WithBuiltins(), oden.WithStdio(strings.NewReader(""), &out, &errOut), oden.WithPath(t.TempDir()))
	Mount(app, New(f.r, "tool", strings.NewReader(""), &out, &errOut))
	err := app.Run(context.Background())
	return out.String(), errOut.String(), err
}

func TestCLIAliasesRunTheCanonicalVerb(t *testing.T) {
	f := aliasFixture(t)
	help, _, err := f.mounted(t, "widget", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(help, "show, display") || !strings.Contains(help, "paint, base.add") || strings.Contains(help, "\n  display ") {
		t.Fatalf("widget help lists the aliases beside their verbs once:\n%s", help)
	}
	gadget, _, err := f.mounted(t, "gadget", "--help")
	if err != nil || !strings.Contains(gadget, "alias of widget.show") {
		t.Fatalf("gadget help: %v\n%s", err, gadget)
	}
	for _, args := range [][]string{
		{"widget", "display", "--target", "w1"},
		{"gadget", "show", "--target", "w2"},
	} {
		out, errOut, err := f.mounted(t, args...)
		if err != nil || !strings.Contains(out, `"target"`) {
			t.Fatalf("%v: %v\n%s\n%s", args, err, out, errOut)
		}
	}
	if len(f.calls) != 2 || f.calls[0].Target != "w1" || f.calls[1].Target != "w2" {
		t.Fatalf("calls %+v", f.calls)
	}
	out, errOut, err := f.run("", "widget", "base.add", "--target", "w1", "--color", "red")
	if err != nil || !strings.Contains(errOut, "notice: widget.base.add is an old name of widget.paint (renamed in v0.27.0; removed after v0.28.0)") {
		t.Fatalf("old name: %v\n%s\n%s", err, out, errOut)
	}
	// A write with its required slot left out fails naming it.
	if _, _, err := f.run("", "widget", "base.add", "--target", "w1"); err == nil || !strings.Contains(err.Error(), "color is required") {
		t.Fatalf("missing slot: %v", err)
	}
	if len(f.calls) != 3 {
		t.Fatalf("a refused write ran: %+v", f.calls)
	}
}

func TestCatalogPublishesAliases(t *testing.T) {
	f := aliasFixture(t)
	data, err := Catalog(f.r, "tool", "v1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	var show CatalogVerb
	for _, n := range doc.Nouns {
		if n.Name == "gadget" {
			t.Fatalf("an alias's noun listed with verbs: %+v", n)
		}
		for _, v := range n.Verbs {
			if n.Name == "widget" && v.Name == "show" {
				show = v
			}
			if n.Name == "widget" && (v.Name == "display" || v.Name == "base.add") {
				t.Fatalf("alias listed as a verb: %s.%s", n.Name, v.Name)
			}
		}
	}
	if len(show.Aliases) != 2 || show.Aliases[0].Alias != "gadget.show" || show.Aliases[1].Alias != "widget.display" {
		t.Fatalf("show aliases %+v", show.Aliases)
	}
}

// A bare alias with preset args runs from the CLI by its one word, and
// a flag of the same name wins over the preset.
func TestCLIBareAliasWithArgs(t *testing.T) {
	f := newFixture(t)
	if err := f.r.AddActionAlias(ochazuke.ActionAlias{Alias: "red!", Action: "widget.paint", Args: ochazuke.Arguments{"color": "red"}}); err != nil {
		t.Fatal(err)
	}
	out, errOut, err := f.mounted(t, "red!", "--target", "w1")
	if err != nil || !strings.Contains(out, `"color": "red"`) {
		t.Fatalf("red!: %v\n%s\n%s", err, out, errOut)
	}
	out, _, err = f.mounted(t, "red!", "--target", "w1", "--color", "blue")
	if err != nil || !strings.Contains(out, `"color": "blue"`) {
		t.Fatalf("red! --color blue: %v\n%s", err, out)
	}
}

// An alias whose presets leave a required parameter out loads; the CLI
// asks for it as for the verb (NAME is required), and given, it runs.
func TestCLIAliasPartialPresets(t *testing.T) {
	f := newFixture(t)
	if err := f.r.AddActionAlias(ochazuke.ActionAlias{Alias: "two!", Action: "widget.paint", Args: ochazuke.Arguments{"coats": 2}}); err != nil {
		t.Fatalf("partial preset: %v", err)
	}
	if _, _, err := f.mounted(t, "two!", "--target", "w1"); err == nil || !strings.Contains(err.Error(), "color is required") {
		t.Fatalf("two! without color: %v", err)
	}
	out, errOut, err := f.mounted(t, "two!", "--target", "w1", "--color", "red")
	if err != nil || !strings.Contains(out, `"color": "red"`) || !strings.Contains(out, `"coats": 2`) {
		t.Fatalf("two! --color red: %v\n%s\n%s", err, out, errOut)
	}
}
