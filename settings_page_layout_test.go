package ochazuke

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/keytest"
)

// layoutFixture is the settings page fixture with sections across two
// owners of the application's.
func layoutFixture(t *testing.T) (*Runtime, *Model) {
	t.Helper()
	r, m, _ := settingsPageFixture(t)
	if err := r.DeclareSetting(
		Setting{Name: "panes.group", Type: SettingString, Default: "a", Enum: []string{"a", "b"}},
		Setting{Name: "panes.width", Type: SettingInteger, Default: 3},
		Setting{Name: "ext.ai.model", Type: SettingString, Default: "large", Owner: "extensions", Section: "ai"},
	); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "ctrl+o")
	m.View()
	return r, m
}

// settings.section steps within the shown tab, wrapping, or with across
// over every tab; settings.tab steps the owners and reopens a tab on the
// section it last showed.
func TestSettingsSectionAndTabSteps(t *testing.T) {
	r, _ := layoutFixture(t)
	run := func(verb string, a Arguments) string {
		t.Helper()
		if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: verb, Args: a}); err != nil {
			t.Fatal(err)
		}
		return r.Settings().page.section
	}
	if got := run("section", Arguments{"step": 1}); got != "app/panes" {
		t.Fatalf("section +1 %q", got)
	}
	if got := run("section", Arguments{"step": 1}); got != "app/general" {
		t.Fatalf("section +1 wraps inside the tab: %q", got)
	}
	if got := run("section", Arguments{"step": -1, "across": true}); got != "ochazuke/settings" {
		t.Fatalf("section -1 across wraps to the last tab's last section: %q", got)
	}
	if got := run("section", Arguments{"step": 1, "across": true}); got != "app/general" {
		t.Fatalf("section +1 across wraps to the first: %q", got)
	}
	run("section", Arguments{"name": "panes"})
	if got := run("tab", Arguments{"step": 1}); got != "extensions/ai" {
		t.Fatalf("tab +1 %q", got)
	}
	if got := run("tab", Arguments{"name": "app"}); got != "app/panes" {
		t.Fatalf("tab app reopens on its last section: %q", got)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "tab", Args: Arguments{"name": "nobody"}}); err == nil {
		t.Fatal("an unknown tab")
	}
	if r.Focus != settingRowPrefix+"panes.group" {
		t.Fatalf("focus %q", r.Focus)
	}
}

// The section verbs act on every setting of a section, their tab twins on
// every setting of an owner: reset is one undo step, copy yanks :set lines,
// write and revert move session values.
func TestSettingsSectionAndTabVerbs(t *testing.T) {
	r, _ := layoutFixture(t)
	s := r.Settings()
	_ = s.Set("panes.group", "b")
	_ = s.Set("panes.width", 5)
	got, err := r.Execute(Request{Noun: settingsSectionNoun, Verb: "copy", Args: Arguments{"section": "panes"}})
	if err != nil || !reflect.DeepEqual(got, []string{"set panes.group=b", "set panes.width=5"}) {
		t.Fatalf("copy %v %v", got, err)
	}
	if y, err := r.ReadYank(""); err != nil || y.Text != "set panes.group=b\nset panes.width=5" || y.Shape != "line" {
		t.Fatalf("yank %+v %v", y, err)
	}
	undo := len(s.page.undo)
	got, err = r.Execute(Request{Noun: settingsSectionNoun, Verb: "reset", Args: Arguments{"section": "panes", "scope": ScopeFile}})
	if err != nil || !reflect.DeepEqual(got, []string{"panes.group", "panes.width"}) || s.String("panes.group") != "a" || s.Int("panes.width") != 3 {
		t.Fatalf("reset %v %v", got, err)
	}
	if len(s.page.undo) != undo+1 {
		t.Fatalf("reset is %d undo steps", len(s.page.undo)-undo)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "undo"}); err != nil || s.String("panes.group") != "b" || s.Int("panes.width") != 5 {
		t.Fatalf("undo of a section reset: %v %q %d", err, s.String("panes.group"), s.Int("panes.width"))
	}
	if _, err := s.SetSession("ext.ai.model", "small"); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Execute(Request{Noun: settingsTabNoun, Verb: "revert", Args: Arguments{"owner": "extensions"}}); err != nil || !reflect.DeepEqual(got, []string{"ext.ai.model"}) || s.String("ext.ai.model") != "large" {
		t.Fatalf("tab revert %v %v", got, err)
	}
	_, _ = s.SetSession("ext.ai.model", "small")
	if got, err := r.Execute(Request{Noun: settingsTabNoun, Verb: "write", Args: Arguments{"owner": "extensions"}}); err != nil || !reflect.DeepEqual(got, []string{"ext.ai.model"}) || s.persisted(*s.find("ext.ai.model")) != "small" {
		t.Fatalf("tab write %v %v", got, err)
	}
	if _, err := r.Execute(Request{Noun: settingsSectionNoun, Verb: "open", Args: Arguments{"section": "ai"}}); err != nil || r.Focus != settingRowPrefix+"ext.ai.model" {
		t.Fatalf("open %q %v", r.Focus, err)
	}
}

// settings.layout tabs draws a tab per section and no section list;
// sections draws no tabs and every owner's sections; the tab, the section
// and the narrow selector carry the session mark.
func TestSettingsLayoutValues(t *testing.T) {
	r, m := layoutFixture(t)
	frame := func() string { return ansi.Strip(m.View().Content) }
	if f := frame(); !strings.Contains(f, "› app  extensions  ochazuke") || !strings.Contains(f, "Sections") {
		t.Fatalf("tabs+sections:\n%s", f)
	}
	_ = r.Settings().Set(SettingsLayoutSetting, SettingsLayoutTabs)
	if f := frame(); !strings.Contains(f, "› General  panes  ai  Appearance") || strings.Contains(f, "Sections") {
		t.Fatalf("tabs:\n%s", f)
	}
	_ = r.Settings().Set(SettingsLayoutSetting, SettingsLayoutSections)
	if f := frame(); strings.Contains(f, "› app") || !strings.Contains(f, "extensions") || !strings.Contains(f, "Appearance") {
		t.Fatalf("sections:\n%s", f)
	}
	_ = r.Settings().Set(SettingsLayoutSetting, SettingsLayoutTabsSections)
	_, _ = r.Settings().SetSession("panes.width", 9)
	if f := frame(); !strings.Contains(f, "› app ◦") || !strings.Contains(f, "panes ◦") {
		t.Fatalf("session marks:\n%s", f)
	}
	m.Width = 60
	if f := frame(); !strings.Contains(f, "Section ‹ General  1/2 ›") || !r.Settings().page.narrow {
		t.Fatalf("narrow:\n%s", f)
	}
	_ = r.Settings().Set(SettingsNarrowWidthSetting, 0)
	if f := frame(); strings.Contains(f, "Section ‹") || r.Settings().page.narrow {
		t.Fatalf("narrow-width 0 never folds:\n%s", f)
	}
	_ = r.Settings().Set(SettingsMarkSessionSetting, false)
	if f := frame(); strings.Contains(f, "◦") {
		t.Fatalf("mark-session off:\n%s", f)
	}
}

// A spill into a SpillToSelection list lands on its selected row; into
// any other list, on its edge row.
func TestSpillToSelection(t *testing.T) {
	r := NewRuntime()
	r.Define("item")
	r.Instance("p", Parent("app"), Kind("ochazuke.page"))
	rows := func(prefix string) []ListItem {
		return []ListItem{{ID: prefix + "1", Noun: "item"}, {ID: prefix + "2", Noun: "item"}, {ID: prefix + "3", Noun: "item"}}
	}
	nav, _ := r.AddList("nav", rows("n"), Parent("p"), SpillToSelection())
	if _, err := r.AddList("body", rows("b"), Parent("p")); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterPage(&Page{ID: "p", Title: "P", Focus: "nav", Render: func(r *Runtime, w, h int) Block {
		return Text(r.List("nav").View(w, 3) + "\n" + r.List("body").View(w, 3))
	}}); err != nil {
		t.Fatal(err)
	}
	_ = r.OpenPage("p")
	_ = nav.Select("n2")
	_ = r.SetFocus("b1")
	if _, err := r.Execute(Request{Noun: "ochazuke.list", Verb: "move", Args: Arguments{"id": "body", "delta": -1}}); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "n2" {
		t.Fatalf("spill into a SpillToSelection list landed on %q", r.Focus)
	}
}

// settings.search-values: the section list's filter matches a section by
// its settings' descriptions and choices across every tab, the settings
// lists follow it (a section shown by its title alone shows all its
// settings), a settings list the user filtered keeps its own filter, and
// clearing restores the shown tab. Off, a section matches by its title.
func TestSettingsSearchValues(t *testing.T) {
	r, m := layoutFixture(t)
	if err := r.DeclareSetting(Setting{Name: "panes.mode", Type: SettingString, Default: "tile", Enum: []string{"tile", "zebrafish"}, Description: "How panes lay out"}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeclareSettingSection(SettingSection{ID: "panes", Title: "Window layout"}); err != nil {
		t.Fatal(err)
	}
	refreshSettingsPage(r)
	m.View()
	sections := r.List(settingsSectionsID)
	if err := sections.Focus(); err != nil {
		t.Fatal(err)
	}
	ids := func(l *List) []string {
		var out []string
		for _, it := range l.items {
			out = append(out, it.ID)
		}
		return out
	}
	sections.SetFilter("zebrafish")
	if got := ids(sections); !reflect.DeepEqual(got, []string{settingSectionPrefix + "app/panes"}) {
		t.Fatalf("an enum choice finds its section: %v", got)
	}
	panes := r.List(settingsListID("app/panes"))
	if got := ids(panes); !reflect.DeepEqual(got, []string{settingRowPrefix + "panes.mode"}) {
		t.Fatalf("the settings pane shows the matches: %v", got)
	}
	// A section matching by its title alone shows every setting.
	sections.SetFilter("window layout")
	if got := ids(sections); !reflect.DeepEqual(got, []string{settingSectionPrefix + "app/panes"}) || panes.Filter() != "" || panes.Shown() != 3 {
		t.Fatalf("panes by title: filter %q, %d shown", panes.Filter(), panes.Shown())
	}
	// Every tab: the extensions owner's section matches by its setting.
	sections.SetFilter("ext.ai")
	if got := ids(sections); !reflect.DeepEqual(got, []string{settingSectionPrefix + "extensions/ai"}) || sections.items[0].Section == "" {
		t.Fatalf("the filter spans every tab, under owner headings: %v", sections.items)
	}
	// A settings list the user filtered keeps its own filter.
	general := r.List(settingsListID("app/general"))
	general.SetFilter("own")
	sections.SetFilter("")
	if general.Filter() != "own" || panes.Filter() != "" {
		t.Fatalf("clearing: general %q, panes %q", general.Filter(), panes.Filter())
	}
	for _, it := range sections.items {
		if !strings.HasPrefix(it.ID, settingSectionPrefix+"app/") && !strings.HasPrefix(it.ID, settingSectionPrefix+"extensions/") {
			t.Fatalf("cleared, the list holds the shown tab's sections only: %v", ids(sections))
		}
	}
	general.SetFilter("")
	// The settings list matches descriptions too.
	panes.SetFilter("lay out")
	if got := ids(panes); !reflect.DeepEqual(got, []string{settingRowPrefix + "panes.mode"}) {
		t.Fatalf("a settings list matches by description: %v", got)
	}
	panes.SetFilter("")
	if err := r.Settings().Set(SettingsSearchValuesSetting, false); err != nil {
		t.Fatal(err)
	}
	refreshSettingsPage(r)
	sections.SetFilter("zebrafish")
	if sections.Shown() != 0 || panes.Filter() != "" {
		t.Fatalf("off: %v, panes %q", ids(sections), panes.Filter())
	}
	if got := ansi.Strip(sections.emptyLine()); got != "no sections match 'zebrafish'" {
		t.Fatalf("off empty line %q", got)
	}
}
