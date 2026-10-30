package ochazuke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// ownersFixture declares application settings across sections, an
// extension owner and a group, beside the framework's rows.
func ownersFixture(t *testing.T) *Runtime {
	t.Helper()
	r := NewRuntime()
	r.Config().App = "demo"
	if err := r.DeclareSetting(
		Setting{Name: "width", Type: SettingInteger, Default: 3},
		Setting{Name: "panes.group", Type: SettingString, Default: "x"},
		Setting{Name: "schedule.gh.enabled", Type: SettingBoolean, Default: true, Section: "schedules", Group: "gh"},
		Setting{Name: "schedules.catch-up", Type: SettingBoolean, Default: false},
		Setting{Name: "schedule.slack.enabled", Type: SettingBoolean, Default: true, Section: "schedules", Group: "slack"},
		Setting{Name: "schedule.gh.interval", Type: SettingDuration, Default: "5m", Section: "schedules", Group: "gh"},
		Setting{Name: "ext.md.scope", Type: SettingString, Default: "all", Owner: "extensions", Section: "md"},
		Setting{Name: "ext.ai.scope", Type: SettingString, Default: "all", Owner: "extensions", Section: "ai"},
	); err != nil {
		t.Fatal(err)
	}
	return r
}

// A setting's owner is its Owner, else the framework's for the embedded
// rows and a framework feature's declarations, else the application's; an
// application changing a framework row's default keeps the framework's
// owner and section.
func TestSettingOwnerResolution(t *testing.T) {
	r := ownersFixture(t)
	s := r.Settings()
	for name, want := range map[string]string{
		"width": OwnerApp, "ext.md.scope": "extensions", "rename.select": OwnerFramework, "sequence-timeout": OwnerFramework,
	} {
		if got := s.OwnerOf(name); got != want {
			t.Errorf("%s owner %q, want %q", name, got, want)
		}
	}
	r.Builtin(func() { _ = r.DeclareSetting(Setting{Name: "feature.knob", Type: SettingBoolean, Default: true}) })
	if got := s.OwnerOf("feature.knob"); got != OwnerFramework {
		t.Fatalf("a Builtin declaration's owner %q", got)
	}
	if err := r.DeclareSetting(Setting{Name: "rename.select", Type: SettingBoolean, Default: false}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("rename.select")
	if got.Owner != OwnerFramework || got.Section != "lists" || got.Default != false {
		t.Fatalf("a replaced framework row %+v", got.Setting)
	}
	if err := r.DeclareSetting(Setting{Name: "repeat.empty", Type: SettingString, Default: "silent", Enum: []string{"info", "silent"}, Owner: "mine", Section: "keys"}); err != nil {
		t.Fatal(err)
	}
	if got := s.OwnerOf("repeat.empty"); got != "mine" {
		t.Fatalf("an application's own owner on a framework row %q", got)
	}
}

// Sections take the name before the first dot; tree, tree.zoom and
// tree.breadcrumb are one section.
func TestSectionNameFirstDot(t *testing.T) {
	for name, want := range map[string]string{
		"tree.zoom.root": "tree", "tree.breadcrumb": "tree", "width": SettingsGeneralSection, "ext.ai.poll-every": "ext",
	} {
		if got := (Setting{Name: name}).SectionName(); got != want {
			t.Errorf("%s section %q, want %q", name, got, want)
		}
	}
	if got := (Setting{Name: "a.b", Section: "x"}).SectionName(); got != "x" {
		t.Fatalf("Section wins: %q", got)
	}
}

// Owners draw app first, then the application's in order of declaration
// or first setting, then ochazuke last; an Order moves one. Sections draw
// declared first (General, then the owner's), the rest by first setting;
// grouped settings sit together.
func TestSettingOwnersAndSectionsOrder(t *testing.T) {
	r := ownersFixture(t)
	s := r.Settings()
	ids := func() []string {
		var out []string
		for _, o := range s.Owners() {
			out = append(out, o.ID)
		}
		return out
	}
	if got := ids(); !reflect.DeepEqual(got, []string{OwnerApp, "extensions", OwnerFramework}) {
		t.Fatalf("owners %v", got)
	}
	owners := s.Owners()
	if owners[0].Title != "demo" || owners[1].Title != "extensions" || owners[2].Title != "ochazuke" || owners[0].Settings != 6 || owners[0].Sections != 3 {
		t.Fatalf("owner info %+v", owners)
	}
	r.Config().App = OwnerFramework
	if got := s.Owners()[0].Title; got != "ochazuke (app)" {
		t.Fatalf("an application named ochazuke: %q", got)
	}
	r.Config().App = "demo"
	if err := r.DeclareSettingOwner(SettingOwner{ID: "extensions", Title: "Extensions", Order: 200}); err != nil {
		t.Fatal(err)
	}
	if got := ids(); !reflect.DeepEqual(got, []string{OwnerApp, OwnerFramework, "extensions"}) || s.Owners()[2].Title != "Extensions" {
		t.Fatalf("owners with an order %v", got)
	}
	app, err := s.Sections(OwnerApp)
	if err != nil {
		t.Fatal(err)
	}
	var secs []string
	for _, sec := range app {
		secs = append(secs, sec.ID+":"+sec.Title)
	}
	if want := []string{"general:General", "panes:panes", "schedules:schedules"}; !reflect.DeepEqual(secs, want) {
		t.Fatalf("app sections %v", secs)
	}
	if want := []string{"schedules.catch-up", "schedule.gh.enabled", "schedule.gh.interval", "schedule.slack.enabled"}; !reflect.DeepEqual(app[2].Settings, want) {
		t.Fatalf("grouped settings %v", app[2].Settings)
	}
	if err := r.DeclareSettingSection(SettingSection{Owner: OwnerApp, ID: "schedules", Title: "Schedules", Description: "When jobs run"}); err != nil {
		t.Fatal(err)
	}
	app, _ = s.Sections(OwnerApp)
	if app[1].ID != "schedules" || app[1].Title != "Schedules" || app[1].Description != "When jobs run" {
		t.Fatalf("a declared section moves before the undeclared: %+v", app)
	}
	fw, _ := s.Sections(OwnerFramework)
	if fw[0].ID != "appearance" || fw[0].Title != "Appearance" {
		t.Fatalf("framework sections start %+v", fw[0])
	}
	for _, sec := range fw {
		if sec.Title == sec.ID {
			t.Errorf("framework section %q has no declared title", sec.ID)
		}
	}
	if _, err := s.Sections("nobody"); err == nil {
		t.Fatal("an unknown owner")
	}
	if err := r.DeclareSettingOwner(SettingOwner{ID: "two words"}); err == nil {
		t.Fatal("an owner id of two words")
	}
}

// Every framework row names a declared section of the framework, so the
// ochazuke tab never shows an untitled section.
func TestFrameworkRowsHaveDeclaredSections(t *testing.T) {
	_, sections := frameworkSettingLayout()
	for _, one := range frameworkSettings() {
		if one.Owner != OwnerFramework {
			t.Errorf("%s owner %q", one.Name, one.Owner)
		}
		if !slices.ContainsFunc(sections, func(d SettingSection) bool {
			return d.ID == one.SectionName() && (d.Owner == OwnerFramework || d.Owner == "")
		}) {
			t.Errorf("%s section %q is not declared in sections.json", one.Name, one.SectionName())
		}
	}
}

// settings list --json, settings.owners, settings.sections and the
// setting things carry owner, section and group.
func TestSettingLayoutVerbsAndOutput(t *testing.T) {
	r := ownersFixture(t)
	list, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "list"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(list)
	var rows []map[string]any
	_ = json.Unmarshal(data, &rows)
	by := map[string]map[string]any{}
	for _, row := range rows {
		by[row["name"].(string)] = row
	}
	if row := by["schedule.gh.enabled"]; row["owner"] != OwnerApp || row["section"] != "schedules" || row["group"] != "gh" {
		t.Fatalf("list row %v", row)
	}
	if row := by["tree.zoom"]; row["owner"] != OwnerFramework || row["section"] != "lists" {
		t.Fatalf("framework list row %v", row)
	}
	owners, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "owners"})
	if err != nil || len(owners.([]SettingOwnerInfo)) != 3 {
		t.Fatalf("settings.owners %v %v", owners, err)
	}
	secs, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "sections", Args: Arguments{"owner": "extensions"}})
	if err != nil {
		t.Fatal(err)
	}
	got := secs.([]SettingSectionInfo)
	if len(got) != 2 || got[0].ID != "md" || got[0].Owner != "extensions" || !reflect.DeepEqual(got[0].Settings, []string{"ext.md.scope"}) {
		t.Fatalf("settings.sections extensions %+v", got)
	}
	thing, err := settingThing(r, "schedule.gh.interval")
	if err != nil || thing["owner"] != OwnerApp || thing["section"] != "schedules" || thing["group"] != "gh" {
		t.Fatalf("setting thing %v %v", thing, err)
	}
}

// config export's settings.reference.json carries each setting's owner,
// section and group.
func TestConfigExportCarriesOwnerAndSection(t *testing.T) {
	r := ownersFixture(t)
	r.Config().Dir = t.TempDir()
	out := t.TempDir()
	if _, err := r.Execute(Request{Noun: "ochazuke.config", Verb: "export", Args: Arguments{"dir": out}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, SettingsReferenceFile))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := DecodeJSONC(data, &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row["name"] == "schedule.gh.enabled" {
			if row["owner"] != OwnerApp || row["section"] != "schedules" || row["group"] != "gh" {
				t.Fatalf("export row %v", row)
			}
			return
		}
	}
	t.Fatal("no schedule.gh.enabled row in the export")
}
