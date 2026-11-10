package ochazuke

import (
	"slices"
	"testing"
)

// A noun's free verbs come from its props' types: a url prop gets open and
// yank-path (not reveal or open-with, which need a file path), a reference
// prop gets follow, which opens the referenced thing's index on its row.
func TestFreeVerbsFromPropTypes(t *testing.T) {
	r := NewRuntime()
	r.Config().Disable()
	projects := NewMemDocTable(r, "project")
	r.Define("project").Props(PropSpec{Name: "id", Key: true, CreateOnly: true}, PropSpec{Name: "title", Role: RoleLabel}).Store(DocStore(projects, "project"))
	_ = projects.Put("p1", Thing{"id": "p1", "title": "Home"})
	links := NewMemDocTable(r, "bookmark")
	r.Define("bookmark").Props(
		PropSpec{Name: "id", Key: true, CreateOnly: true},
		PropSpec{Name: "url", Parameter: Parameter{Type: PropURL}},
		PropSpec{Name: "project", Parameter: Parameter{Noun: "project"}},
	).Store(DocStore(links, "bookmark"))
	_ = links.Put("b1", Thing{"id": "b1", "url": "https://example.com", "project": "p1"})
	verbs := r.Operations.ActionNames(r.Nouns["bookmark"])
	for _, v := range []string{"open", "yank-path", "follow"} {
		if !slices.Contains(verbs, v) {
			t.Fatalf("bookmark lacks %s: %v", v, verbs)
		}
	}
	for _, v := range []string{"reveal", "open-with"} {
		if slices.Contains(verbs, v) {
			t.Fatalf("bookmark.%s needs a file path prop: %v", v, verbs)
		}
	}
	mustRun(t, r, Request{Noun: "bookmark", Verb: "yank-path", Args: Arguments{"id": "b1"}})
	if y, _ := r.ReadYank("\""); y.Text != "https://example.com" {
		t.Fatalf("yank %q", y.Text)
	}
	if _, err := r.AddPage("work", "Work", func(*Runtime, int, int) Block { return Text("") }); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r, Request{Noun: "bookmark", Verb: "follow", Args: Arguments{"id": "b1"}})
	if r.Page != IndexPageID("project") {
		t.Fatalf("follow opens the project's index: %s", r.Page)
	}
	if item, ok := r.List(IndexPageID("project") + "/list").Selected(); !ok || item.Label != "Home" {
		t.Fatalf("follow selects the thing: %+v", item)
	}
}
