package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cli/inspect"
)

// pageData reads the data a written inspector page inlines.
func pageData(t *testing.T, path string) inspect.Data {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const open = `<script id="catalog-data" type="application/json">`
	html := string(b)
	i := strings.Index(html, open)
	j := strings.Index(html[i:], "</script>")
	if i < 0 || j < 0 {
		t.Fatalf("%s has no inlined data", path)
	}
	var d inspect.Data
	if err := json.Unmarshal([]byte(html[i+len(open):i+j]), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func nounIDs(d inspect.Data) map[string]*inspect.Noun {
	out := map[string]*inspect.Noun{}
	for _, n := range d.Nouns {
		out[n.ID] = n
	}
	return out
}

func TestCatalogInspectWritesTheAppsNouns(t *testing.T) {
	f := newFixture(t)
	// A user row on the widget keymap is an effective row the page shows.
	if err := f.r.Bind(ochazuke.Binding{Noun: "widget", Mode: "normal", Keys: "p", Request: ochazuke.Request{Noun: "widget", Verb: "show"}}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "sub", "page.html")
	stdout, stderr, err := f.tool(t.TempDir(), "catalog", "inspect", "--out", out)
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	if strings.TrimSpace(stdout) != out {
		t.Fatalf("stdout %q, want the path", stdout)
	}
	d := pageData(t, out)
	nouns := nounIDs(d)
	if d.Host != "tool" || nouns["tool:widget"] == nil || nouns["tool:widget"].Owner != "tool" {
		t.Fatalf("host %q widget %+v", d.Host, nouns["tool:widget"])
	}
	if n := nouns["ochazuke:ochazuke.settings"]; n == nil || n.Owner != "ochazuke" {
		t.Fatalf("framework noun ochazuke.settings = %+v", n)
	}
	var host *inspect.ToolInfo
	for i := range d.Tools {
		if d.Tools[i].Name == "tool" {
			host = &d.Tools[i]
		}
	}
	if host == nil || host.Role != inspect.RoleHost || host.Version != "1.2.3" || !strings.Contains(host.BindingSource, "effective") {
		t.Fatalf("host tool %+v", host)
	}
	found := false
	for _, k := range d.Keys {
		if k.Tool == "tool" && k.Keys == "p" && k.VerbID == "tool:widget#show" {
			found = true
		}
	}
	if !found {
		t.Fatal("the effective row p widget.show is missing")
	}
	if d.Annotations != nil {
		t.Fatal("no annotations were given")
	}
	if !strings.Contains(stderr, "nouns ") {
		t.Fatalf("stderr %q has no totals", stderr)
	}
}

func TestCatalogInspectOtherSourcesAndAnnotations(t *testing.T) {
	f := newFixture(t)
	old, err := os.ReadFile("inspect/testdata/old.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	saved := filepath.Join(dir, "old.json")
	if err := os.WriteFile(saved, old, 0o644); err != nil {
		t.Fatal(err)
	}
	host, err := os.ReadFile("inspect/testdata/host.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "host.json"), host, 0o644); err != nil {
		t.Fatal(err)
	}
	path := bins(t, map[string]string{
		"o-host":   "cat " + filepath.Join(dir, "host.json"),
		"o-broken": "echo nope >&2; exit 3",
		"o-twin":   "cat " + saved, // the same catalog name as the saved file: read once
	})
	notes := filepath.Join(dir, "notes.json")
	if err := os.WriteFile(notes, []byte(`{"date": "2026-10-01", "nouns": [
	  {"tool": "tool", "noun": "widget", "suggestions": [{"verb": "paint", "params": ["color"]}, {"verb": "polish"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "page.html")
	data := filepath.Join(dir, "data.json")
	_, stderr, err := f.tool(t.TempDir(), "catalog", "inspect", "--out", out, "--json", data,
		"--catalog", saved, "--bin", filepath.Join(path, "o-host"), "--bin", filepath.Join(path, "o-broken"),
		"--bin", filepath.Join(path, "o-twin"), "--annotations", notes)
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	d := pageData(t, out)
	roles := map[string]string{}
	for _, tl := range d.Tools {
		roles[tl.Name] = tl.Role + "/" + tl.Status
	}
	if roles["host"] != "tool/ok" || roles["o-ext"] != "saved/ok" || roles["o-broken"] != "tool/failed" || roles["ochazuke"] != "framework/merged" || len(d.Tools) != 5 {
		t.Fatalf("tools %v", roles)
	}
	if !strings.Contains(stderr, "same catalog as o-ext, skipped") {
		t.Fatalf("stderr %q", stderr)
	}
	nouns := nounIDs(d)
	// host.json's ext-agent mirrors o-ext's agent: o-ext's short name is ext.
	if m := nouns["host:ext-agent"]; m == nil || m.MirrorOf != "o-ext:agent" {
		t.Fatalf("ext-agent %+v", m)
	}
	a := d.Annotations
	if a == nil || a.Source != notes || a.Date != "2026-10-01" || a.Live["present"] != 1 || a.Live["missing"] != 1 {
		t.Fatalf("annotations %+v", a)
	}
	if s := a.Nouns[0].Suggestions[0]; !s.Present || s.Noun != "widget" || s.LiveID != "tool:widget#paint" {
		t.Fatalf("paint %+v", s)
	}
	var fromJSON inspect.Data
	if b, err := os.ReadFile(data); err != nil || json.Unmarshal(b, &fromJSON) != nil || len(fromJSON.Verbs) != len(d.Verbs) {
		t.Fatalf("--json: %v", err)
	}
}

func TestCatalogInspectAudit(t *testing.T) {
	f := newFixture(t)
	out := filepath.Join(t.TempDir(), "page.html")
	if _, stderr, err := f.tool(t.TempDir(), "catalog", "inspect", "--out", out, "--audit", "inspect/testdata/audit.md"); err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	a := pageData(t, out).Annotations
	if a == nil || a.Title != "verb-audit.md" || !filepath.IsAbs(a.Source) || a.Stats["suggestions"] != 5 {
		t.Fatalf("audit %+v", a)
	}
	if _, _, err := f.tool(t.TempDir(), "catalog", "inspect", "--audit", "a.md", "--annotations", "b.json"); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("both: %v", err)
	}
}

func TestCatalogInspectDefaultPathAndOpen(t *testing.T) {
	f := newFixture(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	var opened string
	was := openPage
	openPage = func(path string) error { opened = path; return nil }
	defer func() { openPage = was }()
	stdout, stderr, err := f.tool(t.TempDir(), "catalog", "inspect", "--open")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	want := filepath.Join(tmp, InspectFile) // no state directory in a test binary
	if strings.TrimSpace(stdout) != want || opened != want {
		t.Fatalf("stdout %q opened %q, want %s", stdout, opened, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.tool(t.TempDir(), "catalog", "inspekt"); err == nil {
		t.Fatal("catalog with another word must fail")
	}
	if _, _, err := f.tool(t.TempDir(), "catalog", "inspect", "extra"); err == nil {
		t.Fatal("catalog inspect takes no bare words")
	}
	if out, _, err := f.tool(t.TempDir(), "catalog"); err != nil || !strings.HasPrefix(out, "{") {
		t.Fatalf("catalog still prints JSON: %v %q", err, out[:min(len(out), 40)])
	}
}
