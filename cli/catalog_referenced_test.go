package cli

import (
	"encoding/json"
	"reflect"
	"testing"

	"ofam.dev/ochazuke"
)

// The catalog publishes, for a noun that other nouns' props name, the
// props that do (referencedBy, registration order); a noun nothing names
// has none.
func TestCatalogPublishesReferencedBy(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	done := func(ochazuke.Ctx) ochazuke.Result { return ochazuke.Done(nil) }
	r.Define("project").Props(ochazuke.PropSpec{Name: "id", Key: true}).Verb("open", done, "Open a project")
	r.Define("task").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "project", Role: ochazuke.RoleOwner, Parameter: ochazuke.Parameter{Noun: "project"}}).Verb("run", done, "Run a task")
	r.Define("note").Props(ochazuke.PropSpec{Name: "id", Key: true}, ochazuke.PropSpec{Name: "project", Parameter: ochazuke.Parameter{Noun: "project"}}).Verb("pin", done, "Pin a note")
	ochazuke.NewModel(r)
	out, err := Catalog(r, "tool", "1")
	if err != nil {
		t.Fatal(err)
	}
	var doc CatalogDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	var got []string
	project, _ := doc.noun("project")
	for _, f := range project.ReferencedBy {
		got = append(got, f.ID())
	}
	if want := []string{"task.project", "note.project"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("project referencedBy %v, want %v", got, want)
	}
	if task, _ := doc.noun("task"); len(task.ReferencedBy) != 0 {
		t.Fatalf("task referencedBy %v", task.ReferencedBy)
	}
}
