package ochazuke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke/refdoc"
)

// Every row of the standard verb table says in words what it needs, so
// the reference's standard-verbs page never prints an empty Needs.
func TestStandardNeedsCoverEveryRow(t *testing.T) {
	for _, v := range standardVerbs {
		if standardNeeds[v.Verb] == "" {
			t.Errorf("standardNeeds has no entry for %s", v.Verb)
		}
	}
	for verb := range standardNeeds {
		if standardRow(verb) == nil {
			t.Errorf("standardNeeds names %s, which is not a row", verb)
		}
	}
}

// The reference model carries the standard verb table and each noun with
// props: its kind, its props and its verbs, each marked with the standard
// verb it was generated from.
func TestReferenceModelHasPropsKindAndStandard(t *testing.T) {
	r, _ := newTaskRuntime(t)
	r.Define("task").Verb("launch", func(Ctx) Result { return Done(nil) })
	m := r.ReferenceModel()
	if got := len(m.StandardVerbs); got != len(standardVerbs) || m.StandardVerbs[0].Verb != "get" || m.StandardVerbs[0].Needs != "Store.Get" {
		t.Fatalf("standard verbs %+v", m.StandardVerbs)
	}
	i := slices.IndexFunc(m.Nouns, func(n refdoc.Noun) bool { return n.Name == "task" })
	if i < 0 {
		t.Fatalf("no task in %+v", m.Nouns)
	}
	task := m.Nouns[i]
	if task.Kind != Collection || len(task.Props) != len(taskTestProps()) || !task.Props[0].Key || task.Props[1].Role != RoleLabel || task.Props[1].Fallback != "summary" {
		t.Fatalf("task %+v", task)
	}
	verbs := map[string]string{}
	for _, v := range task.Verbs {
		verbs[v.Name] = v.Standard
	}
	if verbs["get"] != "get" || verbs["list"] != "list" || verbs["launch"] != "" {
		t.Fatalf("verbs %v", verbs)
	}
	for _, n := range m.Nouns {
		if len(r.Props(n.Name)) == 0 {
			t.Fatalf("%s has no props but is listed", n.Name)
		}
	}
	pages := refdoc.Render(m, refdoc.Options{})
	if page := pages[refdoc.StandardVerbsFile]; !strings.Contains(page, "### `task`") || !strings.Contains(page, "Its own verbs: `launch`.") {
		t.Fatalf("standard-verbs page:\n%s", page)
	}
}

// config.export writes props.reference.json: the standard verbs and every
// noun with props, valid JSON, the same data as the reference model.
func TestConfigExportWritesThePropsReference(t *testing.T) {
	r, _ := newTaskRuntime(t)
	r.Config().Dir = t.TempDir()
	out := filepath.Join(t.TempDir(), "out")
	if _, err := r.ExportConfig(out, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, PropsReferenceFile))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		StandardVerbs []refdoc.StandardVerb `json:"standardVerbs"`
		Nouns         []refdoc.Noun         `json:"nouns"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if len(got.StandardVerbs) != len(standardVerbs) || len(got.Nouns) != 1 || got.Nouns[0].Name != "task" || got.Nouns[0].Kind != Collection {
		t.Fatalf("props reference %s", data)
	}
	readme, err := os.ReadFile(filepath.Join(out, ReferenceReadme))
	if err != nil || !strings.Contains(string(readme), "`"+PropsReferenceFile+"`") {
		t.Fatalf("the README names %s: %v", PropsReferenceFile, err)
	}
	// A runtime without props still writes the file, with no nouns.
	plain := NewRuntime()
	plain.Config().Dir = t.TempDir()
	if _, err := plain.ExportConfig(out, false); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(out, PropsReferenceFile))
	if err := json.Unmarshal(data, &got); err != nil || len(got.Nouns) != 0 {
		t.Fatalf("no props: %v %s", err, data)
	}
}
