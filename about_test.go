package ochazuke

import (
	"errors"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/keytest"
)

// fakeAboutEnv is a fixed build: a main module with vcs stamps, ochazuke
// replaced by a local directory, oden brought in by a go.work, monogatari
// at a tag, and a module outside the family prefix.
func fakeAboutEnv(opened *[]string) AboutEnv {
	start := time.Date(2026, 10, 31, 9, 0, 0, 0, time.Local)
	return AboutEnv{
		BuildInfo: func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{
				GoVersion: "go1.26.0",
				Main:      debug.Module{Path: "github.com/unixsuperhero/matomeru", Version: "(devel)"},
				Deps: []*debug.Module{
					{Path: "charm.land/bubbletea/v2", Version: "v2.1.0"},
					{Path: "github.com/unixsuperhero/ochazuke", Version: "v0.74.0", Replace: &debug.Module{Path: "../ochazuke"}},
					{Path: "github.com/unixsuperhero/oden", Version: "(devel)"},
					{Path: "github.com/unixsuperhero/monogatari", Version: "v0.9.0"},
				},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "0123456789abcdef0123"},
					{Key: "vcs.time", Value: "2026-10-30T12:00:00Z"},
					{Key: "vcs.modified", Value: "true"},
				},
			}, true
		},
		Executable: func() (string, error) { return "/opt/bin/mtm", nil },
		PID:        4242,
		Started:    start,
		Now:        func() time.Time { return start.Add(90 * time.Minute) },
		Open: func(path string) error {
			if opened != nil {
				*opened = append(*opened, path)
			}
			return nil
		},
	}
}

func aboutRows(t *testing.T, secs []AboutSection, title string) []AboutRow {
	t.Helper()
	for _, s := range secs {
		if s.Title == title {
			return s.Rows
		}
	}
	t.Fatalf("no section %q in %v", title, secs)
	return nil
}

// about.show answers Build, the application's sections, Dependencies (the
// family prefix only, each replace or go.work said) and Runtime.
func TestAboutSectionsFromFakeBuild(t *testing.T) {
	r := NewRuntime()
	r.Config().Dir = t.TempDir()
	r.SetAboutEnv(fakeAboutEnv(nil))
	r.AddAboutSection("Data", func() []AboutRow {
		return []AboutRow{{Label: "database", Value: "/tmp/x/mtm.db", Kind: AboutPath}}
	})
	got, err := r.Execute(Request{Noun: "ochazuke.about", Verb: "show"})
	if err != nil {
		t.Fatal(err)
	}
	secs := got.([]AboutSection)
	var titles []string
	for _, s := range secs {
		titles = append(titles, s.Title)
	}
	if want := []string{"Build", "Data", "Dependencies", "Runtime"}; !reflect.DeepEqual(titles, want) {
		t.Fatalf("titles %v", titles)
	}
	build := []AboutRow{
		{Label: "module", Value: "github.com/unixsuperhero/matomeru"},
		{Label: "version", Value: "devel"},
		{Label: "commit", Value: "0123456789ab"},
		{Label: "commit time", Value: "2026-10-30T12:00:00Z"},
		{Label: "modified", Value: "yes", Note: "built from a tree with uncommitted changes"},
		{Label: "go", Value: "go1.26.0"},
		{Label: "binary", Value: "/opt/bin/mtm", Kind: AboutPath},
	}
	if got := aboutRows(t, secs, "Build"); !reflect.DeepEqual(got, build) {
		t.Fatalf("build %#v", got)
	}
	deps := []AboutRow{
		{Label: "ochazuke", Value: "../ochazuke", Kind: AboutPath, Note: "local replace of v0.74.0"},
		{Label: "oden", Value: "devel", Note: "local build (go.work)"},
		{Label: "monogatari", Value: "v0.9.0"},
	}
	if got := aboutRows(t, secs, "Dependencies"); !reflect.DeepEqual(got, deps) {
		t.Fatalf("deps %#v", got)
	}
	run := aboutRows(t, secs, "Runtime")
	if run[0] != (AboutRow{Label: "pid", Value: "4242"}) || run[2] != (AboutRow{Label: "uptime", Value: "1h30m0s"}) {
		t.Fatalf("runtime %#v", run)
	}
	one, err := r.Execute(Request{Noun: "ochazuke.about", Verb: "show", Args: Arguments{"section": "data"}})
	if err != nil || len(one.([]AboutSection)) != 1 {
		t.Fatalf("show section data %v %v", one, err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.about", Verb: "show", Args: Arguments{"section": "nope"}}); err == nil || !strings.Contains(err.Error(), "Build, Data, Dependencies, Runtime") {
		t.Fatalf("unknown section %v", err)
	}
	// An empty about.dep-prefixes lists no dependencies.
	if err := r.Settings().Set(AboutDepPrefixesSetting, []string{}); err != nil {
		t.Fatal(err)
	}
	for _, s := range r.About() {
		if s.Title == "Dependencies" {
			t.Fatal("dependencies with no prefixes")
		}
	}
}

// An application's rows run when the page opens, refreshes or answers,
// never at startup; a section that panics reads one error row.
func TestAboutRowsEvaluateLazily(t *testing.T) {
	r, m, _ := settingsPageFixture(t)
	m.Height = 40
	r.SetAboutEnv(fakeAboutEnv(nil))
	calls := 0
	r.AddAboutSection("Data", func() []AboutRow {
		calls++
		return []AboutRow{{Label: "calls", Value: strings.Repeat("x", calls)}}
	})
	r.AddAboutSection("Broken", func() []AboutRow { panic("daemon gone") })
	m.View()
	if calls != 0 {
		t.Fatalf("rows ran at startup: %d", calls)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.about", Verb: "open"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || r.Page != AboutPageID {
		t.Fatalf("calls %d page %q", calls, r.Page)
	}
	keytest.Press(m, "r")
	if calls != 2 {
		t.Fatalf("refresh calls %d", calls)
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Build", "Data", "Broken", "daemon gone", "Dependencies", "Runtime", "github.com/unixsuperhero/matomeru"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	keytest.Press(m, "q")
	if r.Page != "main" || r.PageOf(AboutPageID) != nil || r.Focus != "b" {
		t.Fatalf("q: page %q about %v focus %q", r.Page, r.PageOf(AboutPageID), r.Focus)
	}
}

// about.copy yanks the report; on the page y yanks a row's value, enter
// opens a path row with the opener, and a row that is not a path refuses.
func TestAboutCopyAndRowVerbs(t *testing.T) {
	r, m, _ := settingsPageFixture(t)
	var opened []string
	r.SetAboutEnv(fakeAboutEnv(&opened))
	text, err := r.Execute(Request{Noun: "ochazuke.about", Verb: "copy"})
	if err != nil {
		t.Fatal(err)
	}
	y, _ := r.ReadYank("")
	if y.Text != text || !strings.Contains(y.Text, "Build\n  module       github.com/unixsuperhero/matomeru\n") || !strings.Contains(y.Text, "  ochazuke    ../ochazuke  (local replace of v0.74.0)") {
		t.Fatalf("copy %q", y.Text)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.about", Verb: "open"}); err != nil {
		t.Fatal(err)
	}
	if r.Focus != aboutRowID("Build", "module") {
		t.Fatalf("focus %q", r.Focus)
	}
	keytest.Press(m, "y")
	if y, _ := r.ReadYank(""); y.Text != "github.com/unixsuperhero/matomeru" {
		t.Fatalf("row yank %q", y.Text)
	}
	if _, err := r.Execute(Request{Noun: aboutRowNoun, Verb: "open"}); err == nil || !strings.Contains(err.Error(), "not a path") {
		t.Fatalf("open a version %v", err)
	}
	if err := r.SetFocus(aboutRowID("Build", "binary")); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "enter")
	if !reflect.DeepEqual(opened, []string{"/opt/bin/mtm"}) {
		t.Fatalf("opened %v (status %q)", opened, m.Status)
	}
	got, err := r.Execute(Request{Noun: aboutRowNoun, Verb: "show"})
	if ref, ok := got.(aboutRowRef); err != nil || !ok || ref.Section != "Build" || ref.Kind != AboutPath {
		t.Fatalf("row show %#v %v", got, err)
	}
	r.about().env.Open = func(string) error { return errors.New("no opener") }
	if _, err := r.Execute(Request{Noun: aboutRowNoun, Verb: "open"}); err == nil {
		t.Fatal("an opener error is the verb's")
	}
}

// With about.tab the About page is the Settings page's last tab: ] from
// the last owner opens it, ] again comes back to the first owner, [ from
// the first owner goes to it; q on About goes back to the page Settings
// was opened from. Off, ] stays among the owners and About draws no tabs.
func TestAboutTabOnSettingsPage(t *testing.T) {
	r, m := layoutFixture(t)
	r.SetAboutEnv(fakeAboutEnv(nil))
	if titles, active := r.SettingsTabTitles(AboutPageID); active != len(titles)-1 || titles[active] != "About" {
		t.Fatalf("tabs %v %d", titles, active)
	}
	keytest.Press(m, "]")
	keytest.Press(m, "]")
	if r.Page != SettingsPageID {
		t.Fatalf("page %q", r.Page)
	}
	keytest.Press(m, "]")
	if r.Page != AboutPageID || r.PageOf(SettingsPageID) != nil {
		t.Fatalf("] from the last owner: page %q settings %v", r.Page, r.PageOf(SettingsPageID) != nil)
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "About") || !strings.Contains(view, "ochazuke") || !strings.Contains(view, "]/[ settings.tab") {
		t.Fatalf("about view:\n%s", view)
	}
	keytest.Press(m, "]")
	if r.Page != SettingsPageID || !strings.HasPrefix(r.Settings().page.section, "app/") || r.PageOf(AboutPageID) != nil {
		t.Fatalf("] from About: page %q section %q", r.Page, r.Settings().page.section)
	}
	keytest.Press(m, "[")
	if r.Page != AboutPageID {
		t.Fatalf("[ from the first owner: page %q", r.Page)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "tab", Args: Arguments{"name": "extensions"}}); err != nil || r.Page != SettingsPageID || r.Settings().page.section != "extensions/ai" {
		t.Fatalf("tab extensions from About: %v page %q section %q", err, r.Page, r.Settings().page.section)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "tab", Args: Arguments{"name": "about"}}); err != nil || r.Page != AboutPageID {
		t.Fatalf("tab about: %v page %q", err, r.Page)
	}
	keytest.Press(m, "q")
	if r.Page != "main" || r.Focus != "b" {
		t.Fatalf("q on About: page %q focus %q", r.Page, r.Focus)
	}

	if err := r.Settings().Set(AboutTabSetting, false); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ctrl+o", "]", "]", "]"} {
		keytest.Press(m, k)
	}
	if r.Page != SettingsPageID {
		t.Fatalf("about.tab off: page %q", r.Page)
	}
	if titles, _ := r.SettingsTabTitles(AboutPageID); len(titles) != 3 {
		t.Fatalf("about.tab off tabs %v", titles)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.about", Verb: "open"}); err != nil {
		t.Fatal(err)
	}
	view = ansi.Strip(m.View().Content)
	if strings.Contains(view, "extensions") || strings.Contains(view, "settings.tab") {
		t.Fatalf("about.tab off draws the tabs:\n%s", view)
	}
}

// Every about verb is read-only: agents may run them, and history files
// them as fetches, changes (a yank) or actions.
func TestAboutVerbsAgentsAndHistory(t *testing.T) {
	r := NewRuntime()
	kinds := map[string]string{
		"ochazuke.about.open": HistoryAction, "ochazuke.about.close": HistoryAction, "ochazuke.about.refresh": HistoryFetch,
		"ochazuke.about.show": HistoryFetch, "ochazuke.about.copy": HistoryChange,
		"ochazuke.about-row.show": HistoryFetch, "ochazuke.about-row.copy": HistoryChange, "ochazuke.about-row.open": HistoryAction,
	}
	for key, kind := range kinds {
		op, ok := r.Operations[key]
		if !ok {
			t.Fatalf("no %s", key)
		}
		if op.HistoryKindOf() != kind {
			t.Fatalf("%s history %q", key, op.HistoryKindOf())
		}
		if right := r.AgentRight(op, true); right.Right != AgentAllow {
			t.Fatalf("%s agent %v", key, right)
		}
	}
}
