package inspect

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func readAudit(t *testing.T) *Annotations {
	t.Helper()
	b, err := os.ReadFile("testdata/audit.md")
	if err != nil {
		t.Fatal(err)
	}
	a := AuditAnnotations(string(b))
	a.Source = "testdata/audit.md"
	return a
}

func annotated(a *Annotations, tool, noun string) *AnnotatedNoun {
	for _, n := range a.Nouns {
		if n.Tool == tool && n.Noun == noun {
			return n
		}
	}
	return nil
}

func TestAuditAnnotations(t *testing.T) {
	a := readAudit(t)
	if a.Date != "2026-10-27" || a.Title != "verb-audit.md" {
		t.Fatalf("date %q title %q", a.Date, a.Title)
	}
	if len(a.Tools) != 1 || a.Tools[0].Tool != "host" || a.Tools[0].Version != "v0.9.0" {
		t.Fatalf("tools %+v", a.Tools)
	}
	pane := annotated(a, "host", "pane")
	if pane == nil || len(pane.Existing) != 3 || pane.Existing[0].Keys != "enter" {
		t.Fatalf("pane %+v", pane)
	}
	// show, kill, rename, associate{project}; section 3 adds nothing new for
	// pane (pane.show repeats) and project.associate lands on project.
	if len(pane.Suggestions) != 4 || a.Stats["duplicates"] != 1 {
		t.Fatalf("pane suggestions %+v dup %d", pane.Suggestions, a.Stats["duplicates"])
	}
	if !pane.Suggestions[1].Maybe || !pane.Suggestions[2].Maybe || !reflect.DeepEqual(pane.Suggestions[2].Params, []string{"name"}) {
		t.Fatalf("maybe/params %+v", pane.Suggestions[1:3])
	}
	if g := pane.Suggestions[0].Group; g != "Acts on the thing" || pane.Suggestions[0].Note != "one pane as JSON." {
		t.Fatalf("group %q note %q", g, pane.Suggestions[0].Note)
	}
	if f := annotated(a, "host", "field"); f == nil || !f.NoneToAdd || len(f.Existing) != 2 || !f.Existing[0].TUI {
		t.Fatalf("field %+v", f)
	}
	a.Match(fixtureData(t))
	got := map[string]bool{}
	for _, s := range pane.Suggestions {
		got[s.Verb] = s.Present
	}
	if got["show"] || !got["kill"] || !got["associate"] {
		t.Fatalf("present %v", got)
	}
	proj := annotated(a, "host", "project")
	if proj == nil || len(proj.Suggestions) != 1 || proj.Suggestions[0].Present || !strings.HasPrefix(proj.Suggestions[0].Note, "reverse of pane.associate") {
		t.Fatalf("project %+v", proj)
	}
	if a.Live["present"] != 2 || a.Live["missing"] != 3 {
		t.Fatalf("live %v", a.Live)
	}
	var unparsed []string
	for _, s := range a.Sections {
		if !s.Parsed {
			unparsed = append(unparsed, s.Title)
		}
	}
	if !equal(unparsed, []string{"1. Summary", "4. Parameters that should be noun-typed"}) {
		t.Fatalf("unparsed %v", unparsed)
	}
	if len(a.Skipped) != 1 || a.Skipped[0].Text != "Intro prose that is not parsed." {
		t.Fatalf("skipped %+v", a.Skipped)
	}
}

// What AuditAnnotations reads, written as an annotations file, reads back
// the same, and so does a hand-written file in the documented shape.
func TestAnnotationsRoundTrip(t *testing.T) {
	a := readAudit(t)
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ReadAnnotations(b)
	if err != nil {
		t.Fatal(err)
	}
	back.byKey, a.byKey = nil, nil
	if !reflect.DeepEqual(a, back) {
		t.Fatalf("round trip differs:\n%+v\n%+v", a, back)
	}

	hand := []byte(`{"source": "notes.md", "date": "2026-10-01", "nouns": [
	  {"tool": "host", "noun": "pane", "suggestions": [
	    {"verb": "kill"}, {"verb": "kill"}, {"verb": "associate", "params": ["project|scratch", "story"]},
	    {"noun": "project", "verb": "open", "maybe": true, "note": "already"}]}]}`)
	h, err := ReadAnnotations(hand)
	if err != nil {
		t.Fatal(err)
	}
	if h.Stats["duplicates"] != 1 || h.Stats["suggestions"] != 3 || h.Nouns[0].Suggestions[0].Noun != "pane" {
		t.Fatalf("normalized %+v %+v", h.Stats, h.Nouns[0].Suggestions)
	}
	h.Match(fixtureData(t))
	s := h.Nouns[0].Suggestions
	if !s[0].Present || s[1].Present || !reflect.DeepEqual(s[1].MissingParams, []string{"story"}) || s[1].LiveID != "host:pane#associate" || !s[2].Present {
		t.Fatalf("matched %+v", s)
	}
	if h.Live["present"] != 2 || h.Live["partial"] != 1 {
		t.Fatalf("live %v", h.Live)
	}
	if _, err := ReadAnnotations([]byte(`{"nouns": 3}`)); err == nil {
		t.Fatal("a malformed file must fail")
	}
}

func TestWriteInlinesData(t *testing.T) {
	d := fixtureData(t)
	d.Annotations = readAudit(t)
	d.Annotations.Match(d)
	var buf bytes.Buffer
	if err := Write(d, &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if strings.Contains(html, dataMarker) || !strings.HasPrefix(html, "<!doctype html>") || !strings.HasSuffix(strings.TrimSpace(html), "</html>") {
		t.Fatal("the marker must be replaced inside the page")
	}
	i := strings.Index(html, `<script id="catalog-data" type="application/json">`)
	j := strings.Index(html[i:], "</script>")
	raw := html[i+len(`<script id="catalog-data" type="application/json">`) : i+j]
	var back Data
	if err := json.Unmarshal([]byte(raw), &back); err != nil {
		t.Fatal(err)
	}
	if back.Host != "host" || len(back.Verbs) != len(d.Verbs) || back.Annotations == nil || back.Annotations.Date != "2026-10-27" {
		t.Fatalf("inlined %+v", back.Host)
	}
}

func TestNamespaces(t *testing.T) {
	srcs := []Source{
		{Tool: ToolInfo{Name: "mtm"}, Catalog: &Catalog{Name: "mtm"}},
		{Tool: ToolInfo{Name: "o-ai"}, Catalog: &Catalog{Name: "o-ai"}},
		{Tool: ToolInfo{Name: "o-gone", Status: "failed"}},
	}
	got := Namespaces(srcs, "mtm", "git")
	if !equal(got["mtm"], []string{"ai", "git"}) || !equal(got["o-ai"], []string{"mtm"}) || got["o-gone"] != nil {
		t.Fatalf("namespaces %v", got)
	}
	if ns, p, rest := namespaceOf("ai.agent", got["mtm"]); ns != "ext:ai" || p != "ai" || rest != "agent" {
		t.Fatalf("dotted mirror %s %s %s", ns, p, rest)
	}
}

// A host that installed its actions lists them in its own catalog; the
// actions file marks that verb instead of adding a second one.
func TestInstalledActionMarked(t *testing.T) {
	host := loadCatalog(t, "host.json")
	host.Nouns[1].Verbs = append(host.Nouns[1].Verbs, CatVerb{Name: "open-log", Kind: "application"})
	actions := []UserAction{{Noun: "pane", Verb: "open-log", Command: []string{"open", "{{data.log}}"}}}
	d := Merge([]Source{{Tool: ToolInfo{Name: "host", Role: RoleHost}, Catalog: host}}, nil, actions, "host")
	n := 0
	for _, v := range d.Verbs {
		if v.Name == "open-log" {
			n++
			if !v.User || v.Owner != "user" || v.Command != "open {{data.log}}" || v.ID != "host:pane#open-log" {
				t.Fatalf("open-log %+v", v)
			}
		}
	}
	if n != 1 || !equal(d.Actions, []string{"host:pane#open-log"}) {
		t.Fatalf("%d open-log verbs, actions %v", n, d.Actions)
	}
}
