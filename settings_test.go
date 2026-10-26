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

// The editor edits in place: each key saves at once, no save key; a typed
// value that does not validate keeps the prompt open with the error.
func TestSettingsEditorSavesEachChange(t *testing.T) {
	r, m, dir := settingsFixture(t)
	r.Define("app").Mode("normal").Bind("ctrl+o", Do("settings.edit"))
	m.Width, m.Height = 100, 24
	m.Init()
	keytest.Press(m, "ctrl+o")
	if r.TopModal() != settingsEditorID || r.Focus != settingRowPrefix+"scrolloff" {
		t.Fatalf("modal %q focus %q status %q", r.TopModal(), r.Focus, m.Status)
	}
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"Settings", "Setting", "Value", "scrolloff", "Rows kept above and below the focus", "dark, light, mono"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame lacks %q:\n%s", want, frame)
		}
	}
	keytest.Press(m, "l")
	keytest.Press(m, "l")
	if r.Settings().Int("scrolloff") != 4 || readSettingsFile(t, dir)["scrolloff"] != 4.0 {
		t.Fatalf("l l: %d", r.Settings().Int("scrolloff"))
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
	if r.TopModal() != settingsEditorID || r.Focus != settingRowPrefix+"debounce" || r.Settings().Duration("debounce") != 2*time.Second {
		t.Fatalf("commit: modal %q focus %q %v", r.TopModal(), r.Focus, r.Settings().Duration("debounce"))
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "2s *") {
		t.Fatalf("the row does not show the set value:\n%s", ansi.Strip(m.View().Content))
	}
	keytest.Press(m, "r")
	if r.Settings().Duration("debounce") != 400*time.Millisecond {
		t.Fatal("r did not reset")
	}
	keytest.Press(m, "enter")
	keytest.Press(m, "esc")
	keytest.Press(m, "esc")
	keytest.Press(m, "esc")
	if r.TopModal() != "" {
		t.Fatalf("esc left %q open", r.TopModal())
	}
	want := map[string]any{"scrolloff": 4.0, "theme": "dark", "wrap": true}
	if got := readSettingsFile(t, dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("file %v", got)
	}
}
