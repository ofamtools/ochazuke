package ochazuke

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// relSectionFixture is a related stack over project hub with relation
// sections: outward directories, incoming `about`.
func relSectionFixture(t *testing.T, key *string) (*Runtime, *Related, *MemRelations) {
	t.Helper()
	r, mem, _ := relRuntime(t)
	r.Instance("page")
	rel, err := r.AddRelated("page/details", RelatedSpec{Noun: "project", Key: func() string { return *key }}, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("page", "Page", func(r *Runtime, w, h int) Block { return Text(rel.View(w, h)) }, Focus(rel.FocusTarget())); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	m.Width, m.Height = 60, 30
	m.View()
	return r, rel, mem
}

func relSectionRun(t *testing.T, r *Runtime, verb string, args Arguments) {
	t.Helper()
	args["related"] = "page/details"
	mustRun(t, r, Request{Noun: RelatedNoun, Verb: verb, Args: args})
}

func relText(rel *Related) string { return ansi.Strip(rel.View(60, 24)) }

func TestRelationSectionsListAndWrite(t *testing.T) {
	t.Parallel()
	key := "hub"
	r, rel, mem := relSectionFixture(t, &key)
	text := relText(rel)
	for _, want := range []string{"Directories (0)", "Projects (about)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("stack lacks %q:\n%s", want, text)
		}
	}
	// associate relates (appends), so the kind's rules and the default hold.
	for _, d := range []string{"/a", "/b"} {
		relSectionRun(t, r, "associate", Arguments{"section": "directory.directories", "thing": d})
	}
	if got := relDirs(t, r, "hub"); !reflect.DeepEqual(got, []string{"/a", "/b"}) {
		t.Fatalf("directories %v", got)
	}
	if text := relText(rel); !strings.Contains(text, "Directories (2)") || !strings.Contains(text, "/b") {
		t.Fatalf("section after associate:\n%s", text)
	}
	// A set from elsewhere shows at the next read (the relation generation).
	mustRun(t, r, Request{Noun: "project", Verb: "set", Args: Arguments{"id": "hub", "prop": "directories+=/c"}})
	if text := relText(rel); !strings.Contains(text, "Directories (3)") {
		t.Fatalf("section after outside set:\n%s", text)
	}
	// unassociate unrelates the selected row, never deletes the thing.
	list := rel.List("directory.directories")
	if err := list.Select(list.Items()[len(list.Items())-1].ID); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus(list.FocusTarget()); err != nil {
		t.Fatal(err)
	}
	relSectionRun(t, r, "unassociate", Arguments{"section": "directory.directories"})
	if got := relDirs(t, r, "hub"); !reflect.DeepEqual(got, []string{"/a", "/b"}) {
		t.Fatalf("after unassociate %v", got)
	}
	if mem.Len() != 2 {
		t.Fatalf("rows %d", mem.Len())
	}
	// new is not offered where there is no prop to fill.
	if _, err := r.Execute(Request{Noun: RelatedNoun, Verb: "new", Args: Arguments{"section": "directory.directories", "related": "page/details"}}); err == nil {
		t.Fatal("new in an outward section")
	}
}

func TestRelationSectionIncomingAppends(t *testing.T) {
	t.Parallel()
	key := "ops"
	r, rel, _ := relSectionFixture(t, &key)
	// project.about points at project (single); a second kind of section.
	relSectionRun(t, r, "associate", Arguments{"section": "project.about", "thing": "hub"})
	if got := relGet(t, r, "hub", "about"); got != "ops" {
		t.Fatalf("hub about %v", got)
	}
	if text := relText(rel); !strings.Contains(text, "Projects (about) (1)") && !strings.Contains(text, "(1)") {
		t.Fatalf("incoming section:\n%s", text)
	}
	// new fills the relation-backed prop of the created thing.
	relSectionRun(t, r, "new", Arguments{"section": "project.about"})
	if text := relText(rel); !strings.Contains(text, "(2)") {
		t.Fatalf("a created thing is related:\n%s", text)
	}
}

func TestRelationSectionsSettingTurnsThemOff(t *testing.T) {
	t.Parallel()
	key := "hub"
	r, rel, _ := relSectionFixture(t, &key)
	if _, err := r.Settings().SetSession(RelationSectionsSetting, false); err != nil {
		t.Fatal(err)
	}
	if text := relText(rel); strings.Contains(text, "Directories") {
		t.Fatalf("relation sections drawn with the setting off:\n%s", text)
	}
	if _, err := r.Settings().SetSession(RelationSectionsSetting, true); err != nil {
		t.Fatal(err)
	}
	if text := relText(rel); !strings.Contains(text, "Directories") {
		t.Fatalf("relation sections gone after the setting is back on:\n%s", text)
	}
}
