package ochazuke

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

func settingsFixture(t *testing.T) (*Runtime, *Model, string) {
	t.Helper()
	r := NewRuntime()
	dir := t.TempDir()
	r.Config().Dir = dir
	if err := r.DeclareSetting(
		Setting{Name: "scrolloff", Type: SettingInteger, Default: 2, Description: "Rows kept above and below the focus"},
		Setting{Name: "theme", Type: SettingString, Default: "dark", Description: "Colour theme", Enum: []string{"dark", "light", "mono"}},
		Setting{Name: "wrap", Type: SettingBoolean, Default: false, Description: "Wrap long lines"},
		Setting{Name: "debounce", Type: SettingDuration, Default: "400ms", Description: "Wait before writing"},
		Setting{Name: "ratio", Type: SettingNumber, Default: 0.5, Description: "Split ratio"},
	); err != nil {
		t.Fatal(err)
	}
	return r, NewModel(r), dir
}

func readSettingsFile(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, SettingsFile))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDeclareSettingChecksNameTypeDefaultAndEnum(t *testing.T) {
	r := NewRuntime()
	for _, bad := range []Setting{
		{Name: "", Type: SettingString, Default: ""},
		{Name: "two words", Type: SettingString, Default: ""},
		{Name: "n", Type: "color", Default: "red"},
		{Name: "n", Type: SettingInteger, Default: "two"},
		{Name: "n", Type: SettingString, Default: "x", Enum: []string{"a"}},
		{Name: "n", Type: SettingInteger, Default: 1, Enum: []string{"1"}},
	} {
		if err := r.DeclareSetting(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if err := r.DeclareSetting(Setting{Name: "n", Type: SettingInteger, Default: 1}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeclareSetting(Setting{Name: "n", Type: SettingInteger, Default: 1}); err == nil {
		t.Fatal("declared twice")
	}
}

// Defaults until set; set validates, writes settings.json atomically and
// tells observers; the typed getters read the new value; reset goes back.
func TestSettingsSetValidatesPersistsAndNotifies(t *testing.T) {
	r, _, dir := settingsFixture(t)
	s := r.Settings()
	if s.Int("scrolloff") != 2 || s.String("theme") != "dark" || s.Bool("wrap") || s.Duration("debounce") != 400*time.Millisecond || s.Float("ratio") != 0.5 {
		t.Fatalf("defaults: %+v", s.List())
	}
	var seen []string
	s.Observe("", func(name string, v any) {
		seen = append(seen, name+"="+strings.TrimSpace(strings.Trim(settingText(v), `"`)))
	})
	var themes []any
	s.Observe("theme", func(_ string, v any) { themes = append(themes, v) })
	for _, step := range [][2]string{{"scrolloff", "5"}, {"theme", "light"}, {"wrap", "true"}, {"debounce", "1s"}, {"ratio", "0.25"}} {
		if _, err := r.Execute(Request{Noun: "settings", Verb: "set", Args: Arguments{"name": step[0], "value": step[1]}}); err != nil {
			t.Fatalf("set %v: %v", step, err)
		}
	}
	if s.Int("scrolloff") != 5 || s.String("theme") != "light" || !s.Bool("wrap") || s.Duration("debounce") != time.Second || s.Float("ratio") != 0.25 {
		t.Fatalf("after set: %+v", s.List())
	}
	want := map[string]any{"scrolloff": 5.0, "theme": "light", "wrap": true, "debounce": "1s", "ratio": 0.25}
	if got := readSettingsFile(t, dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("file %v", got)
	}
	if !reflect.DeepEqual(seen, []string{"scrolloff=5", "theme=light", "wrap=true", "debounce=1s", "ratio=0.25"}) || !reflect.DeepEqual(themes, []any{"light"}) {
		t.Fatalf("observers %v %v", seen, themes)
	}
	for _, bad := range [][2]string{{"scrolloff", "five"}, {"theme", "neon"}, {"wrap", "maybe"}, {"debounce", "soon"}, {"nope", "1"}} {
		if _, err := r.Execute(Request{Noun: "settings", Verb: "set", Args: Arguments{"name": bad[0], "value": bad[1]}}); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	if got := readSettingsFile(t, dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("a refused set changed the file: %v", got)
	}
	got, err := r.Execute(Request{Noun: "settings", Verb: "reset", Args: Arguments{"name": "theme"}})
	if err != nil || got.(SettingValue).Value != "dark" || got.(SettingValue).Set || s.String("theme") != "dark" {
		t.Fatalf("reset %+v %v", got, err)
	}
	if _, ok := readSettingsFile(t, dir)["theme"]; ok || !reflect.DeepEqual(themes, []any{"light", "dark"}) {
		t.Fatalf("reset left the key or did not notify: %v", themes)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temp files left: %v", entries)
	}
	list, _ := r.Execute(Request{Noun: "settings", Verb: "list"})
	if rows := list.([]SettingValue); len(rows) != 5 || rows[3].Value != "1s" || rows[3].Default != "400ms" || !rows[3].Set {
		t.Fatalf("list %+v", list)
	}
}

func settingText(v any) string { data, _ := json.Marshal(fileForm(v)); return string(data) }

// Unknown and invalid keys are reported and kept; a file that does not
// parse is reported and never written over.
func TestSettingsFileKeepsUnknownKeysAndRefusesToClobberABrokenFile(t *testing.T) {
	r, m, dir := settingsFixture(t)
	writeConfig(t, dir, SettingsFile, `{"scrolloff": "lots", "future": {"x": 1}, "wrap": true}`)
	m.Init()
	s := r.Settings()
	if s.Int("scrolloff") != 2 || !s.Bool("wrap") {
		t.Fatalf("values %+v", s.List())
	}
	if len(m.Notices) != 2 || !strings.Contains(m.Notices[0], `unknown setting "future" (kept)`) || !strings.Contains(m.Notices[1], "scrolloff: must be an integer") {
		t.Fatalf("notices %q", m.Notices)
	}
	if err := s.Set("scrolloff", 4); err != nil {
		t.Fatal(err)
	}
	got := readSettingsFile(t, dir)
	if got["scrolloff"] != 4.0 || got["future"] == nil || got["wrap"] != true {
		t.Fatalf("file %v", got)
	}
	if len(m.Notices) != 1 {
		t.Fatalf("the fixed value's notice stayed: %q", m.Notices)
	}
	writeConfig(t, dir, SettingsFile, `{"scrolloff": `)
	s.Reload()
	if s.Int("scrolloff") != 2 || len(m.Notices) != 1 || !strings.Contains(m.Notices[0], "not a JSON object") {
		t.Fatalf("broken file: %d %q", s.Int("scrolloff"), m.Notices)
	}
	if err := s.Set("wrap", false); err == nil {
		t.Fatal("wrote over a broken file")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, SettingsFile)); string(data) != `{"scrolloff": ` {
		t.Fatalf("file %q", data)
	}
}

// A value another process wrote (the CLI) reaches the running Model on
// its next poll, and its observers run.
func TestSettingsPollPicksUpAnotherProcessWrite(t *testing.T) {
	r, m, dir := settingsFixture(t)
	if m.Init() == nil {
		t.Fatal("no init command")
	}
	var seen []any
	r.Settings().Observe("scrolloff", func(_ string, v any) { seen = append(seen, v) })
	other := NewRuntime()
	other.Config().Dir = dir
	if err := other.DeclareSetting(Setting{Name: "scrolloff", Type: SettingInteger, Default: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Execute(Request{Noun: "settings", Verb: "set", Args: Arguments{"name": "scrolloff", "value": "7"}}); err != nil {
		t.Fatal(err)
	}
	_, cmd := m.Update(SettingsPollMsg{})
	if cmd == nil || r.Settings().Int("scrolloff") != 7 || !reflect.DeepEqual(seen, []any{7}) {
		t.Fatalf("poll: %d %v", r.Settings().Int("scrolloff"), seen)
	}
	if r.Settings().ReloadIfChanged() {
		t.Fatal("reloaded an unchanged file")
	}
}

// No config directory: values last for the session, nothing is polled.
func TestSettingsWithoutADirectory(t *testing.T) {
	r := NewRuntime()
	if err := r.DeclareSetting(Setting{Name: "n", Type: SettingInteger, Default: 3}); err != nil {
		t.Fatal(err)
	}
	m := NewModel(r)
	if r.Settings().Int("n") != 3 || r.Settings().Set("n", 4) != nil || r.Settings().Int("n") != 4 || m.settingsPoll() != nil {
		t.Fatal("a runtime without a directory kept no value or polled")
	}
}

// settingsPageFixture is settingsFixture with a page of its own (a list
// of two rows, the second focused) and ctrl+o bound to settings.edit.
func settingsPageFixture(t *testing.T) (*Runtime, *Model, string) {
	t.Helper()
	r, m, dir := settingsFixture(t)
	r.Define("item")
	r.Instance("main", Parent("app"), Kind("page"))
	if _, err := r.AddList("rows", []ListItem{{ID: "a", Label: "a", Noun: "item"}, {ID: "b", Label: "b", Noun: "item"}}, Parent("main")); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterPage(&Page{ID: "main", Title: "Main", Focus: "rows", Render: func(r *Runtime, w, h int) Block { return Text(r.List("rows").View(w, h)) }}); err != nil {
		t.Fatal(err)
	}
	r.Define("app").Mode("normal").Bind("ctrl+o", Do("settings.edit"))
	m.Width, m.Height = 100, 24
	m.Init()
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("b"); err != nil {
		t.Fatal(err)
	}
	return r, m, dir
}

// The Settings page edits in place: each key saves at once, no save key;
// a typed value that does not validate keeps the prompt open with the
// error; q goes back to the page it was opened from, focus kept.
func TestSettingsPageSavesEachChange(t *testing.T) {
	r, m, dir := settingsPageFixture(t)
	keytest.Press(m, "ctrl+o")
	if r.Page != SettingsPageID || r.Focus != settingRowPrefix+"scrolloff" || r.TopModal() != "" {
		t.Fatalf("page %q focus %q modal %q status %q", r.Page, r.Focus, r.TopModal(), m.Status)
	}
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"General (5)", "scrolloff", "‹ 2 ›", "Rows kept above and below the focus", "Default is 2.", "a whole number"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame lacks %q:\n%s", want, frame)
		}
	}
	keytest.Press(m, "l")
	keytest.Press(m, "l")
	if r.Settings().Int("scrolloff") != 4 || readSettingsFile(t, dir)["scrolloff"] != 4.0 {
		t.Fatalf("l l: %d", r.Settings().Int("scrolloff"))
	}
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "scrolloff •") || !strings.Contains(frame, "Set in settings.json.") {
		t.Fatalf("no dot or source after a change:\n%s", frame)
	}
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	if r.Settings().String("theme") != "light" {
		t.Fatalf("enter on theme: %q", r.Settings().String("theme"))
	}
	keytest.Press(m, "h")
	keytest.Press(m, "h")
	if r.Settings().String("theme") != "mono" {
		t.Fatalf("h h wraps: %q", r.Settings().String("theme"))
	}
	keytest.Press(m, "l")
	keytest.Press(m, "j")
	keytest.Press(m, "space")
	if !r.Settings().Bool("wrap") {
		t.Fatal("space on wrap")
	}
	keytest.Press(m, "j")
	if strings.Contains(BindingHints(r, r.Focus), "setting.cycle") {
		t.Fatalf("a duration row offers cycle: %s", BindingHints(r, r.Focus))
	}
	keytest.Press(m, "enter")
	if r.TopModal() != settingPromptID || string(r.Input(settingValueID).Text) != "400ms" {
		t.Fatalf("prompt %q %q", r.TopModal(), r.Focus)
	}
	keytest.Press(m, "ctrl+u")
	keytest.Type(m, "soon")
	keytest.Press(m, "enter")
	if r.TopModal() != settingPromptID || !strings.Contains(m.Status, "must be a duration") {
		t.Fatalf("bad value: modal %q status %q", r.TopModal(), m.Status)
	}
	keytest.Press(m, "ctrl+u")
	keytest.Type(m, "2s")
	keytest.Press(m, "enter")
	if r.TopModal() != "" || r.Focus != settingRowPrefix+"debounce" || r.Settings().Duration("debounce") != 2*time.Second {
		t.Fatalf("commit: modal %q focus %q %v", r.TopModal(), r.Focus, r.Settings().Duration("debounce"))
	}
	keytest.Press(m, "r")
	if r.Settings().Duration("debounce") != 400*time.Millisecond {
		t.Fatal("r did not reset")
	}
	keytest.Press(m, "enter")
	keytest.Press(m, "esc")
	if r.TopModal() != "" || r.Page != SettingsPageID {
		t.Fatalf("esc in the prompt: modal %q page %q", r.TopModal(), r.Page)
	}
	keytest.Press(m, "q")
	if r.Page != "main" || r.Focus != "b" || r.PageOf(SettingsPageID) != nil {
		t.Fatalf("q: page %q focus %q, settings page kept %v", r.Page, r.Focus, r.PageOf(SettingsPageID) != nil)
	}
	want := map[string]any{"scrolloff": 4.0, "theme": "dark", "wrap": true}
	if got := readSettingsFile(t, dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("file %v", got)
	}
}

// Sections come from Section, else the name before its last dot; the
// settings without one come first under General.
func TestSettingSections(t *testing.T) {
	r := NewRuntime()
	if err := r.DeclareSetting(
		Setting{Name: "ext.ai.poll-every", Type: SettingDuration, Default: "30s"},
		Setting{Name: "width", Type: SettingInteger, Default: 3},
		Setting{Name: "colour", Type: SettingString, Default: "x", Section: "look"},
		Setting{Name: "ext.ai.model", Type: SettingString, Default: "m"},
	); err != nil {
		t.Fatal(err)
	}
	if got := settingSections(r.Settings()); !reflect.DeepEqual(got, []string{"", "ext.ai", "look"}) {
		t.Fatalf("sections %q", got)
	}
}

// esc and q leave the page; settings.edit on the page stays; a permanent
// page (AddSettingsPage) stays registered after it is left.
func TestSettingsPageLeaving(t *testing.T) {
	r, m, _ := settingsPageFixture(t)
	keytest.Press(m, "ctrl+o")
	keytest.Press(m, "ctrl+o")
	if r.Page != SettingsPageID {
		t.Fatalf("settings.edit on the page left it: %q", r.Page)
	}
	keytest.Press(m, "esc")
	if r.Page != "main" || r.Focus != "b" || r.PageOf(SettingsPageID) != nil {
		t.Fatalf("esc: page %q focus %q", r.Page, r.Focus)
	}
	if _, err := r.AddSettingsPage(); err != nil {
		t.Fatal(err)
	}
	keytest.Press(m, "ctrl+o")
	keytest.Press(m, "q")
	if r.Page != "main" || r.PageOf(SettingsPageID) == nil {
		t.Fatalf("permanent page: page %q, registered %v", r.Page, r.PageOf(SettingsPageID) != nil)
	}
}

// A list setting holds strings: settings.json keeps it as a JSON array,
// settings set takes one as a word, a value of another shape is refused,
// the getter hands out a copy, and observers see each change once.
func TestListSetting(t *testing.T) {
	r := NewRuntime()
	dir := t.TempDir()
	r.Config().Dir = dir
	for _, bad := range []Setting{
		{Name: "l", Type: SettingList, Default: "a"},
		{Name: "l", Type: SettingList, Default: []any{"a", 1}},
		{Name: "l", Type: SettingList, Default: []string{}, Enum: []string{"a"}},
	} {
		if err := r.DeclareSetting(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if err := r.DeclareSetting(
		Setting{Name: "paths", Type: SettingList, Default: []any{"~/proj"}},
		Setting{Name: "ignore", Type: SettingList, Default: []string{}},
	); err != nil {
		t.Fatal(err)
	}
	s := r.Settings()
	if got := s.Strings("paths"); !reflect.DeepEqual(got, []string{"~/proj"}) {
		t.Fatalf("default %q", got)
	}
	s.Strings("paths")[0] = "changed"
	if s.Strings("paths")[0] != "~/proj" {
		t.Fatal("the getter handed out the stored slice")
	}
	if got, _ := s.Get("ignore"); got.Value == nil || len(got.Value.([]string)) != 0 {
		t.Fatalf("empty list reads %#v", got.Value)
	}
	var seen [][]string
	s.Observe("paths", func(_ string, v any) { seen = append(seen, v.([]string)) })
	if _, err := r.Execute(Request{Noun: "settings", Verb: "set", Args: Arguments{"name": "paths", "value": `["~/proj", "~/work"]`}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("paths", []string{"~/proj", "~/work"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, [][]string{{"~/proj", "~/work"}}) {
		t.Fatalf("observers %q", seen)
	}
	if got := readSettingsFile(t, dir)["paths"]; !reflect.DeepEqual(got, []any{"~/proj", "~/work"}) {
		t.Fatalf("file %#v", got)
	}
	for _, bad := range []string{"~/proj", `["a", 2]`, `{"a": 1}`, "null"} {
		if err := s.Set("paths", bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	got, err := r.Execute(Request{Noun: "settings", Verb: "reset", Args: Arguments{"name": "paths"}})
	if err != nil || !reflect.DeepEqual(got.(SettingValue).Value, []string{"~/proj"}) || len(seen) != 2 {
		t.Fatalf("reset %+v %v %q", got, err, seen)
	}
	writeConfig(t, dir, SettingsFile, `{"ignore": ["*.tmp"], "paths": "oops"}`)
	s.Reload()
	if !reflect.DeepEqual(s.Strings("ignore"), []string{"*.tmp"}) || !reflect.DeepEqual(s.Strings("paths"), []string{"~/proj"}) {
		t.Fatalf("reload %q %q %q", s.Strings("ignore"), s.Strings("paths"), s.Problems())
	}
}

// The description says why the default applies when settings.json holds a
// value that does not validate; undo on the page puts back what the file
// held, even that invalid value.
func TestSettingsPageSourceAndUndo(t *testing.T) {
	r, m, dir := settingsPageFixture(t)
	writeConfig(t, dir, SettingsFile, `{"scrolloff": "lots"}`)
	r.Settings().Reload()
	keytest.Press(m, "ctrl+o")
	frame := ansi.Strip(m.View().Content)
	if !strings.Contains(frame, "settings.json holds an invalid value (must be an integer), so the default") {
		t.Fatalf("source:\n%s", frame)
	}
	keytest.Press(m, "l")
	if r.Settings().Int("scrolloff") != 3 {
		t.Fatalf("l: %d", r.Settings().Int("scrolloff"))
	}
	keytest.Press(m, "u")
	if got := readSettingsFile(t, dir)["scrolloff"]; got != "lots" || r.Settings().Int("scrolloff") != 2 {
		t.Fatalf("undo wrote %v, reads %d", got, r.Settings().Int("scrolloff"))
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "holds an invalid value") {
		t.Fatal("undo did not bring back the invalid value's problem")
	}
	if _, err := r.Execute(Request{Noun: "settings", Verb: "undo"}); err == nil {
		t.Fatal("undo with nothing to undo succeeded")
	}
	if _, err := r.Execute(Request{Noun: "settings", Verb: "section", Args: Arguments{"name": "nope"}}); err == nil {
		t.Fatal("an unknown section was shown")
	}
}
