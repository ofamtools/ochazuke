package cli

import (
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

// An extension's noun mounted as json.doc with a bare alias doc and a
// deprecated json-doc reads the same on the CLI (Mount and Shell) as in
// the palette: doc rows, json doc rows, json.doc rows and json-doc rows
// (with one notice) all run json.doc.rows; help lists json.doc with alias doc, never json-doc.
func TestCLINounAliasesMatchThePalette(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	f := newFixture(t)
	f.r.Define("json.doc").Verb("rows", func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done("json rows") }, "The documents' rows")
	if err := f.r.Alias("doc", "json.doc"); err != nil {
		t.Fatal(err)
	}
	if err := f.r.AddNounAlias(ochazuke.NounAlias{Alias: "json-doc", Noun: "json.doc", Since: "v0.57.0"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, args := range [][]string{{"doc", "rows"}, {"json", "doc", "rows"}, {"json.doc", "rows"}, {"json-doc", "rows"}} {
		out, errOut, err := f.tool(dir, args...)
		if err != nil || strings.TrimSpace(out) != `"json rows"` {
			t.Errorf("tool %s = %q (%v)\n%s", strings.Join(args, " "), out, err, errOut)
		}
		if deprecated := args[0] == "json-doc"; deprecated != strings.Contains(errOut, "notice: json-doc is an old name of doc (renamed in v0.57.0)") {
			t.Errorf("tool %s: notice %v, stderr %q", strings.Join(args, " "), deprecated, errOut)
		}
		out, errOut, err = f.run("", args...)
		if err != nil || strings.TrimSpace(out) != `"json rows"` {
			t.Errorf("shell %s = %q (%v)\n%s", strings.Join(args, " "), out, err, errOut)
		}
	}
	help, _, err := f.tool(dir, "help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(help, "\n  json.doc, doc ") || strings.Contains(help, "json-doc") {
		t.Errorf("help lists json.doc with alias doc and not json-doc:\n%s", help)
	}
	// Unalias takes the name away from the CLI too.
	if err := f.r.Unalias("doc"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.run("", "doc", "rows"); err == nil {
		t.Error("doc rows still runs after Unalias")
	}
}
