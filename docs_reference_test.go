package ochazuke

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke/refdoc"
)

// docJSON is a bindings doc's groups as JSON, for comparing two docs.
func docJSON(t *testing.T, doc BindingsDoc) string {
	t.Helper()
	data, err := FormatBindings(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// With no user file the default doc is the stored one.
func TestDefaultBindingsDocWithoutAUserFileIsBindingsDoc(t *testing.T) {
	r, m, _, _ := configFixture(t)
	m.Init()
	if got, want := docJSON(t, r.DefaultBindingsDoc()), docJSON(t, r.BindingsDoc()); got != want {
		t.Fatalf("defaults differ from the stored tables with no bindings.json:\n%s\nwant\n%s", got, want)
	}
}

// A user row that replaces a default, one that unbinds a default and one
// that adds a key: the default doc has the defaults back and no user row,
// the stored doc keeps the user's.
func TestDefaultBindingsDocUndoesTheUserFile(t *testing.T) {
	r, m, _, dir := configFixture(t)
	before := docJSON(t, r.BindingsDoc())
	writeConfig(t, dir, BindingsFile, `[
		{"noun": "app", "mode": "normal", "keys": "?", "request": {"noun": "test", "verb": "gamma"}},
		{"noun": "app", "mode": "normal", "keys": "a", "request": null},
		{"noun": "app", "mode": "normal", "keys": "w", "request": {"noun": "test", "verb": "gamma"}}
	]`)
	m.Init()
	if problems := r.Config().BindingProblems(); len(problems) > 0 {
		t.Fatalf("bindings.json problems: %v", problems)
	}
	live := docJSON(t, r.BindingsDoc())
	if live == before || !strings.Contains(live, "test.gamma") {
		t.Fatalf("the user's rows are not installed:\n%s", live)
	}
	got := docJSON(t, r.DefaultBindingsDoc())
	if got != before {
		t.Fatalf("defaults with a user file:\n%s\nwant the tables before it:\n%s", got, before)
	}
	for _, want := range []string{`{"key": "?", "action": "ochazuke.bindings.help"}`, `{"key": "a", "action": "test.alpha"}`} {
		if !strings.Contains(got, want) {
			t.Errorf("default %s is not back", want)
		}
	}
	if strings.Contains(got, "test.gamma") {
		t.Errorf("a user row is in the defaults:\n%s", got)
	}
}

// The reference model: settings by name with the user's value marked,
// the user's rows as yours lines on their table, none of which reaches
// the fingerprint; with show-config off the page is the default render.
func TestReferenceModelMarksTheUserConfig(t *testing.T) {
	plain, pm, _, _ := configFixture(t)
	pm.Init()
	clean := plain.ReferenceModel()
	r, m, _, dir := configFixture(t)
	writeConfig(t, dir, BindingsFile, `[
		{"noun": "app", "mode": "normal", "keys": "?", "request": {"noun": "test", "verb": "gamma"}},
		{"noun": "app", "mode": "normal", "keys": "a", "request": null}
	]`)
	writeConfig(t, dir, SettingsFile, `{"focus-display": "stack"}`)
	m.Init()
	got := r.ReferenceModel()
	if got.Fingerprint() != clean.Fingerprint() {
		t.Fatalf("the user's config changed the fingerprint: %s, clean %s", got.Fingerprint(), clean.Fingerprint())
	}
	var focus *refdoc.Setting
	for i, s := range got.Settings {
		if i > 0 && got.Settings[i-1].Name > s.Name {
			t.Fatalf("settings out of order: %s before %s", got.Settings[i-1].Name, s.Name)
		}
		if s.Name == FocusDisplaySetting {
			focus = &got.Settings[i]
		}
	}
	if focus == nil || focus.Default != "off" || strings.Join(focus.Enum, ",") != "off,noun,stack" || focus.Yours != "stack" || !focus.Framework {
		t.Fatalf("focus-display = %+v", focus)
	}
	var yours []string
	for _, g := range got.Bindings {
		if g.Context == "app/normal" {
			yours = g.Yours
		}
	}
	if len(yours) != 2 || !strings.Contains(yours[0], "`?` → `test.gamma` (replaces the default)") || !strings.Contains(yours[1], "`a` unbound") {
		t.Fatalf("app/normal yours = %q", yours)
	}
	off := refdoc.Render(got, refdoc.Options{})
	want := refdoc.Render(clean, refdoc.Options{})
	for name, text := range want {
		if off[name] != text {
			t.Errorf("%s with show-config off differs from the default render", name)
		}
	}
	on := refdoc.Render(got, refdoc.Options{ShowConfig: true})
	if !strings.Contains(on[refdoc.SettingsFile], "> yours: `stack` (settings.json)") || !strings.Contains(on[refdoc.BindingsFile], "> yours: `?` → `test.gamma`") {
		t.Errorf("show-config pages lack the yours lines")
	}
	// Only yours lines (each after a blank line) are added.
	var extra []string
	wantLines := map[string]int{}
	for _, l := range strings.Split(want[refdoc.SettingsFile]+want[refdoc.BindingsFile], "\n") {
		wantLines[l]++
	}
	for _, l := range strings.Split(on[refdoc.SettingsFile]+on[refdoc.BindingsFile], "\n") {
		if wantLines[l] > 0 {
			wantLines[l]--
		} else if l != "" && !strings.HasPrefix(l, "> yours: ") {
			extra = append(extra, l)
		}
	}
	if len(extra) > 0 {
		t.Errorf("show-config adds lines other than yours: %q", extra)
	}
}

// docs.open renders a topic into the viewer modal headed by the build;
// help and h are its bare aliases; an unknown topic is refused.
func TestDocsOpenShowsATopic(t *testing.T) {
	r, m, _, _ := configFixture(t)
	m.Init()
	if _, err := r.Execute(Request{Noun: "ochazuke.docs", Verb: "open", Args: Arguments{"topic": "settings"}}); err != nil {
		t.Fatal(err)
	}
	inst := r.Instances[docsViewerID]
	if inst == nil || r.TopModal() != docsViewerID {
		t.Fatalf("no docs viewer; modal %q", r.TopModal())
	}
	v := inst.Component.(*docsViewer)
	if !strings.Contains(v.ModalTitle(), " · catalog sha256:") || !strings.HasPrefix(v.text, "# Settings") {
		t.Fatalf("title %q text %.40q", v.ModalTitle(), v.text)
	}
	if !strings.Contains(v.text, "### `focus-display`") {
		t.Errorf("the settings page has no focus-display")
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"help", "h"} {
		a, ok := r.ActionAlias(name)
		if !ok || a.Action != "ochazuke.docs.open" {
			t.Errorf("alias %s = %+v, %v", name, a, ok)
		}
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.docs", Verb: "open", Args: Arguments{"topic": "nope"}}); err == nil {
		t.Error("an unknown topic opened")
	}
	if problems := r.ActionAliasProblems(); len(problems) > 0 {
		t.Errorf("alias problems: %v", problems)
	}
	data, _ := json.Marshal(r.Settings().List())
	if !strings.Contains(string(data), DocsShowConfigSetting) {
		t.Errorf("%s is not declared", DocsShowConfigSetting)
	}
}
