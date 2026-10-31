package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
)

func TestPropFilterShellAndCatalog(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Define("note").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "by"}).Store(ochazuke.Store{List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
		return []ochazuke.Thing{{"id": "a", "by": "agent"}, {"id": "b", "by": "owner"}}, nil
	}}).Filters(ochazuke.SavedFilter{Name: "mine", Where: map[string]any{"by": "owner"}})
	var out, stderr bytes.Buffer
	shell := New(r, "test", strings.NewReader(""), &out, &stderr)
	if err := shell.Run(context.Background(), []string{"note", "list", "--filter", "mine", "--where", "by=owner"}); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != "b" {
		t.Fatal(out.String())
	}
	raw, err := Catalog(r, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	n, ok := doc.noun("note")
	if !ok || len(n.Filters) != 1 {
		t.Fatalf("catalog filter lost: %+v", n)
	}
	host := ochazuke.NewRuntime()
	mounted := MountNouns(host, []CatalogNoun{n}, HostMount{Local: func(name string) string { return "ext." + name }, Proxy: func(string, CatalogVerb) any { return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) } }})
	if len(mounted.Warnings) > 0 {
		t.Fatal(mounted.Warnings)
	}
	filters, err := host.SavedFilters("", "ext.note")
	if err != nil || len(filters) != 1 || filters[0].Noun != "ext.note" {
		t.Fatalf("filters %+v err %v", filters, err)
	}
	out.Reset()
	if err := shell.Run(context.Background(), []string{"bindings", "check"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `"severity":"error"`) {
		t.Fatal(out.String())
	}
}

func TestPropFilterCatalogPagePreservesForeignID(t *testing.T) {
	doc := pageDoc("stack", false)
	for i := range doc.Nouns {
		if doc.Nouns[i].Name == "skill" {
			doc.Nouns[i].Props = []ochazuke.PropSpec{{Name: "key", Key: true}, {Name: "origin"}}
			doc.Nouns[i].Filters = []ochazuke.SavedFilter{{Name: "mine", Where: map[string]any{"origin": "claude"}}}
		}
	}
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	mounted := MountNouns(r, doc.Nouns, HostMount{Local: func(name string) string { return name }, Proxy: func(string, CatalogVerb) any { return func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) } }})
	if len(mounted.Warnings) > 0 {
		t.Fatal(mounted.Warnings)
	}
	m := ochazuke.NewModel(r)
	views, err := MountPages(m, doc, PageConfig{Fetch: answers(pageAnswers)})
	if err != nil || len(views) != 1 {
		t.Fatalf("views %d: %v", len(views), err)
	}
	v := views[0]
	if err := v.SetRows("ai/skills", []Row{{ID: "s1", Label: "Architect", Data: map[string]any{"origin": "claude"}}, {ID: "s2", Label: "Review", Data: map[string]any{"origin": "ai-tools"}}}); err != nil {
		t.Fatal(err)
	}
	l := m.Runtime.List("ai/skills")
	if err := l.ApplyFilter("mine"); err != nil {
		t.Fatal(err)
	}
	if l.Shown() != 1 {
		t.Fatal("catalog row record not used by prop filter")
	}
	row, _ := l.Selected()
	if row.ID != "ai/skills/s1" || row.Data != "s1" || row.Props["key"] != "s1" {
		t.Fatalf("foreign key lost: %+v", row)
	}
	if err := l.SetPropFilters([]ochazuke.PropFilter{{Prop: "key", Value: "s1"}}); err != nil {
		t.Fatal(err)
	}
	if l.Shown() != 1 {
		t.Fatal("key prop must use foreign id, not instance id")
	}
}

func TestPropFilterShellSavedManagementAndOrder(t *testing.T) {
	r := ochazuke.NewRuntime()
	r.Config().Dir = t.TempDir()
	r.Define("note").Props(ochazuke.PropSpec{Name: "by"}).Filters(ochazuke.SavedFilter{Name: "all"})
	l, err := r.AddList("notes", []ochazuke.ListItem{{ID: "a", Noun: "note", Data: ochazuke.Thing{"by": "owner"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.SetPropFilters([]ochazuke.PropFilter{{Prop: "by", Value: "owner"}}); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	shell := New(r, "test", strings.NewReader(""), &out, &stderr)
	for _, args := range [][]string{
		{"list", "filter-save", "--id", "notes", "--name", "local"},
		{"list", "filters", "--id", "notes"},
		{"list", "filter-saved-delete", "--id", "notes", "--name", "local"},
	} {
		out.Reset()
		if err := shell.Run(context.Background(), args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if args[1] == "filters" && !strings.Contains(out.String(), `"name":"local"`) {
			t.Fatal(out.String())
		}
	}
	raw, err := json.Marshal(CatalogNoun{Name: "note", Props: []ochazuke.PropSpec{{Name: "by"}}, Filters: []ochazuke.SavedFilter{{Name: "all"}}, Verbs: []CatalogVerb{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(raw), `"props"`) > strings.Index(string(raw), `"filters"`) || !strings.HasPrefix(string(raw), `{"name":`) {
		t.Fatal(string(raw))
	}
}
