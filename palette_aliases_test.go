package ochazuke

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// extRuntime is a host with two extensions' nouns mounted under their
// namespaces (json.doc, md.doc, json.schema) beside an own noun docsets,
// and a palette.
func extRuntime(t *testing.T) (*Runtime, *Palette, *[]string, *RecordingSink) {
	t.Helper()
	r := NewRuntime()
	var ran []string
	for _, noun := range []string{"json.doc", "md.doc", "docsets", "json.schema"} {
		noun := noun
		r.Register(Operation{Noun: noun, Verb: "rows", Description: "List the rows", Run: func(*Runtime, Context, Arguments) (any, error) {
			ran = append(ran, noun+".rows")
			return nil, nil
		}})
	}
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	p := NewPalette(r)
	return r, p, &ran, sink
}

func parsed(p *Palette, line string) string {
	c := p.parse(line)
	if c.Err != nil {
		return "error: " + c.Err.Error()
	}
	return c.Op.Key()
}

// Exact beats prefix: a bare alias (doc) names its noun even though it is
// a prefix of another noun (docs), and so does any other alias and the
// canonical name; namespace words join into the noun, as on the CLI.
func TestPaletteNounAliasesAndNamespaceWords(t *testing.T) {
	r, p, _, _ := extRuntime(t)
	if err := r.Alias("doc", "json.doc"); err != nil {
		t.Fatal(err)
	}
	if err := r.Alias("json-doc", "json.doc"); err != nil {
		t.Fatal(err)
	}
	for line, want := range map[string]string{
		"doc rows":         "json.doc.rows",
		"json-doc rows":    "json.doc.rows",
		"json.doc rows":    "json.doc.rows",
		"json doc rows":    "json.doc.rows",
		"JSON Doc rows":    "json.doc.rows",
		"rows doc":         "json.doc.rows",
		"rows json doc":    "json.doc.rows",
		"rows json-doc":    "json.doc.rows",
		"docsets rows":     "docsets.rows",
		"md doc rows":      "md.doc.rows",
		"md.doc rows":      "md.doc.rows",
		"json schema rows": "json.schema.rows",
		"schema rows":      "json.schema.rows", // the one namespaced noun ending in schema
	} {
		if got := parsed(p, line); got != want {
			t.Errorf("%q: %s, want %s", line, got, want)
		}
	}
	// The noun is drawn by its shortest live alias.
	if got := r.DisplayNoun("json.doc"); got != "doc" {
		t.Errorf("DisplayNoun(json.doc) = %q, want doc", got)
	}
	if got := r.DisplayNoun("md.doc"); got != "md.doc" {
		t.Errorf("DisplayNoun(md.doc) = %q, want md.doc", got)
	}
}

// With no alias claiming a word, every namespaced noun ending in it is
// offered by its full name and none is picked.
func TestPaletteAmbiguousShortNameOffersEveryNamespace(t *testing.T) {
	r, p, _, _ := extRuntime(t)
	c := p.parse("doc rows")
	if c.Err == nil || !strings.Contains(c.Err.Error(), "json.doc, md.doc") {
		t.Fatalf("doc rows: %v (op %s), want an ambiguity naming json.doc, md.doc", c.Err, c.Op.Key())
	}
	if got := c.Words[0].Candidates; !slices.Equal(got, []string{"json.doc", "md.doc"}) {
		t.Errorf("candidates %v", got)
	}
	// Off: the word reads as a prefix alone (docs, docsets).
	if err := r.Settings().Set(PaletteNounTailsSetting, "false"); err != nil {
		t.Fatal(err)
	}
	if got := parsed(p, "doc rows"); strings.Contains(got, "json.doc") || strings.Contains(got, "md.doc") {
		t.Errorf("noun-tails off: doc rows is %s, want the prefix reading alone (docs, docsets)", got)
	}
}

// A namespace's words complete: after json  the namespace's nouns' next
// words; a typed prefix offers the namespaces themselves.
func TestPaletteNamespaceCompletion(t *testing.T) {
	_, p, _, _ := extRuntime(t)
	paletteRequest(t, p.r, "open", "palette", nil)
	typeLine(t, p, "json ")
	got := completionValues(p)
	for _, want := range []string{"noun:doc", "noun:schema"} {
		if !slices.Contains(got, want) {
			t.Errorf("after 'json ': %v lacks %s", got, want)
		}
	}
	typeLine(t, p, "doc ")
	if got := completionValues(p); !slices.Contains(got, "verb:rows") {
		t.Errorf("after 'json doc ': %v lacks verb:rows", got)
	}
	paletteRequest(t, p.r, "set", "palette", Arguments{"text": "js"})
	if got := completionValues(p); !slices.Contains(got, "noun:json") {
		t.Errorf("js: %v lacks the namespace json", got)
	}
}

// A deprecated alias is accepted with one notice, never completed and
// never drawn; Unalias takes a name away.
func TestPaletteDeprecatedNounAlias(t *testing.T) {
	r, p, ran, sink := extRuntime(t)
	if err := r.Alias("doc", "json.doc"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddNounAlias(NounAlias{Alias: "jdoc", Noun: "json.doc", Since: "v0.57.0", RemoveAfter: "v0.58.0"}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddNounAlias(NounAlias{Alias: "j", Noun: "json.doc", Since: "v0.57.0"}); err != nil {
		t.Fatal(err)
	}
	if got := r.DisplayNoun("json.doc"); got != "doc" {
		t.Errorf("a deprecated alias is never drawn: DisplayNoun = %q", got)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "jdoc rows")
	paletteRequest(t, r, "open", "palette", nil)
	paletteLine(t, r, "jdoc rows")
	if !slices.Equal(*ran, []string{"json.doc.rows", "json.doc.rows"}) {
		t.Errorf("ran %v", *ran)
	}
	var notices []string
	for _, n := range sink.Notices() {
		if strings.Contains(n, "jdoc") {
			notices = append(notices, n)
		}
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "jdoc is an old name of doc (renamed in v0.57.0; removed after v0.58.0)") {
		t.Errorf("notices %q, want one naming the noun", notices)
	}
	paletteRequest(t, r, "open", "palette", nil)
	paletteRequest(t, r, "set", "palette", Arguments{"text": "jd"})
	for _, e := range p.completions() {
		if strings.Contains(e.Value, "jdoc") {
			t.Errorf("a deprecated alias is completed: %+v", e)
		}
	}
	// The API spelling posts it too (once per alias).
	paletteRequest(t, r, "rows", "j", nil)
	if got := sink.Notices(); !slices.ContainsFunc(got, func(n string) bool { return strings.HasPrefix(n, "j is an old name") }) {
		t.Errorf("notices %q lack the API's", got)
	}
	if err := r.Unalias("doc"); err != nil {
		t.Fatal(err)
	}
	if err := r.Unalias("doc"); err == nil {
		t.Error("Unalias of a removed alias: no error")
	}
	if got := r.DisplayNoun("json.doc"); got != "json.doc" {
		t.Errorf("after Unalias: DisplayNoun = %q", got)
	}
	if err := r.Alias("jd", "json.doc"); err != nil {
		t.Fatal(err)
	}
	if got := parsed(p, "jd rows"); got != "json.doc.rows" {
		t.Errorf("a new alias after Unalias: %s", got)
	}
}

// nouns.display picks the name a noun is drawn by.
func TestNounsDisplaySetting(t *testing.T) {
	r, _, _, _ := extRuntime(t)
	if err := r.Alias("doc", "json.doc"); err != nil {
		t.Fatal(err)
	}
	for value, want := range map[string][2]string{
		NounsDisplayShortest:  {"doc", "list"},
		NounsDisplayFramework: {"json.doc", "list"},
		NounsDisplayCanonical: {"json.doc", "ochazuke.list"},
	} {
		if err := r.Settings().Set(NounsDisplaySetting, value); err != nil {
			t.Fatal(err)
		}
		if got := [2]string{r.DisplayNoun("json.doc"), r.DisplayNoun("ochazuke.list")}; got != want {
			t.Errorf("%s: %v, want %v", value, got, want)
		}
	}
}

// bindings check names a row spelled by a deprecated noun alias and the
// noun to rename it to; the row still loads.
func TestBindingsCheckNotesADeprecatedNounAlias(t *testing.T) {
	r, _, ran, _ := extRuntime(t)
	if err := r.Alias("doc", "json.doc"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddNounAlias(NounAlias{Alias: "json-doc", Noun: "json.doc", Since: "v0.57.0"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	r.Config().Dir = dir
	if err := os.WriteFile(filepath.Join(dir, BindingsFile), []byte(`[{"noun": "app", "mode": "normal", "keys": "R", "request": {"noun": "json-doc", "verb": "rows"}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if problems := r.EnsureUserBindings(); len(problems) != 0 {
		t.Fatalf("load: %q", problems)
	}
	want := "bindings.json: row 1 R: json-doc resolves to json.doc; json-doc is an old name of doc (renamed in v0.57.0): rename the row's noun to doc"
	if check := r.CheckUserBindings(); !slices.Contains(check, want) {
		t.Errorf("bindings check lacks %q:\n%s", want, strings.Join(check, "\n"))
	}
	if _, err := r.Dispatch(MacroKey{Key: "R"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*ran, []string{"json.doc.rows"}) {
		t.Errorf("ran %v", *ran)
	}
}

// page open offers the pages' titles and takes one (quoted when it holds
// a space); the id still runs typed whole, and palette.choice-words id
// offers the ids as before.
func TestPalettePageOpenOffersTitles(t *testing.T) {
	r := NewRuntime()
	p := NewPalette(r)
	if _, err := r.AddPage("ext/json", "JSON", func(*Runtime, int, int) Block { return Block{} }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("ext/pulls", "Pull Requests", func(*Runtime, int, int) Block { return Block{} }); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "open", "palette", nil)
	typeLine(t, p, "page open ")
	got := completionValues(p)
	if !slices.Contains(got, "choice:JSON") || !slices.Contains(got, "choice:Pull Requests") || slices.Contains(got, "choice:ext/json") {
		t.Fatalf("page open offers %v, want the titles", got)
	}
	for line, want := range map[string]string{
		`page open JSON`:            "ext/json",
		`page open js`:              "ext/json",
		`page open "Pull Requests"`: "ext/pulls",
		`page open Pu`:              "ext/pulls",
		`page open ext/json`:        "ext/json",
		`page JSON`:                 "ext/json",
	} {
		c := p.parse(line)
		if c.Err != nil || c.Args["id"] != want {
			t.Errorf("%q: %v %v, want id %s", line, c.Args, c.Err, want)
		}
	}
	// Taking a title with a space quotes it.
	paletteRequest(t, r, "set", "palette", Arguments{"text": `page open "Pull`})
	for _, e := range p.completions() {
		if e.ID == "ext/pulls" {
			if err := p.accept(e); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := p.Text(); got != `page open "Pull Requests" ` {
		t.Errorf("line %q", got)
	}
	if err := r.Settings().Set(PaletteChoiceWordsSetting, "id"); err != nil {
		t.Fatal(err)
	}
	paletteRequest(t, r, "set", "palette", Arguments{"text": "page open "})
	if got := completionValues(p); !slices.Contains(got, "choice:ext/json") {
		t.Errorf("choice-words id offers %v, want the ids", got)
	}
}
