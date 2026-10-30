package ochazuke

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// sessionFixture is settingsFixture with a session started, as a running
// Model's Init does.
func sessionFixture(t *testing.T) (*Runtime, *Model, string) {
	t.Helper()
	r, m, dir := settingsFixture(t)
	r.Settings().StartSession()
	return r, m, dir
}

// readJSONCSettings is readSettingsFile for a file with comments.
func readJSONCSettings(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, SettingsFile))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := DecodeJSONC(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func writeSettingsFile(t *testing.T, dir, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Session over file over default; a session write leaves the file alone;
// observers fire once per change of the effective value.
func TestSessionLayersOverTheFile(t *testing.T) {
	r, _, dir := sessionFixture(t)
	s := r.Settings()
	writeSettingsFile(t, dir, `{"theme": "light"}`)
	s.Reload()
	var seen []any
	s.Observe("theme", func(_ string, v any) { seen = append(seen, v) })
	if layer, err := s.SetSession("theme", "mono"); err != nil || layer != OriginSession {
		t.Fatalf("set session: %q %v", layer, err)
	}
	if s.String("theme") != "mono" || readSettingsFile(t, dir)["theme"] != "light" {
		t.Fatalf("effective %q file %v", s.String("theme"), readSettingsFile(t, dir))
	}
	got, _ := s.Get("theme")
	if !got.Session || got.Saved != "light" || got.Value != "mono" || !got.Set {
		t.Fatalf("get: %+v", got)
	}
	if _, err := s.SetSession("theme", "mono"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetSession("theme", "blue"); err == nil {
		t.Fatal("a session value skipped the enum")
	}
	if err := s.Revert("theme"); err != nil || s.String("theme") != "light" {
		t.Fatalf("revert: %q %v", s.String("theme"), err)
	}
	if !reflect.DeepEqual(seen, []any{"mono", "light"}) {
		t.Fatalf("observers %v", seen)
	}
	if _, ok := s.Session("theme"); ok {
		t.Fatal("revert kept the session value")
	}
}

// Without a session (a CLI process, a script) or with settings.autosave,
// a session write goes to the file: the writes-through rule.
func TestSessionWritesThroughWithoutASessionOrWithAutosave(t *testing.T) {
	r, _, dir := settingsFixture(t)
	s := r.Settings()
	if layer, err := s.SetSession("scrolloff", 7); err != nil || layer != OriginFile || readSettingsFile(t, dir)["scrolloff"] != 7.0 {
		t.Fatalf("no session: %q %v", layer, err)
	}
	s.StartSession()
	if err := s.Set(SettingsAutosaveSetting, true); err != nil {
		t.Fatal(err)
	}
	if layer, err := s.SetSession("scrolloff", 8); err != nil || layer != OriginFile || readSettingsFile(t, dir)["scrolloff"] != 8.0 {
		t.Fatalf("autosave: %q %v", layer, err)
	}
	if layer, err := s.ResetSession("scrolloff"); err != nil || layer != OriginFile || readSettingsFile(t, dir)["scrolloff"] != nil {
		t.Fatalf("autosave reset: %q %v %v", layer, err, readSettingsFile(t, dir))
	}
	if len(s.Diff()) != 0 {
		t.Fatalf("diff %+v", s.Diff())
	}
}

// Writing the file (Set, Reset, the page's undo) drops the setting's
// session value, so what was written shows.
func TestFileWriteDropsTheSessionValue(t *testing.T) {
	r, _, _ := sessionFixture(t)
	s := r.Settings()
	_, _ = s.SetSession("scrolloff", 9)
	if err := s.Set("scrolloff", 4); err != nil || s.Int("scrolloff") != 4 {
		t.Fatalf("set: %d %v", s.Int("scrolloff"), err)
	}
	if _, ok := s.Session("scrolloff"); ok {
		t.Fatal("Set kept the session value")
	}
	_, _ = s.SetSession("wrap", true)
	if err := s.Reset("wrap"); err != nil || s.Bool("wrap") {
		t.Fatalf("reset of a setting only the session held: %v %v", s.Bool("wrap"), err)
	}
}

// A file change the poll reads leaves the session value on top, marks the
// setting changed on disk and shows in the diff's saved value.
func TestPollKeepsTheSessionValueAndMarksTheDisk(t *testing.T) {
	r, m, dir := settingsFixture(t)
	m.Init()
	s := r.Settings()
	if !s.HasSession() {
		t.Fatal("Init started no session")
	}
	_, _ = s.SetSession("theme", "mono")
	other := NewRuntime()
	other.Config().Dir = dir
	_ = other.DeclareSetting(Setting{Name: "theme", Type: SettingString, Default: "dark", Enum: []string{"dark", "light", "mono"}})
	if err := other.Settings().Set("theme", "light"); err != nil {
		t.Fatal(err)
	}
	m.Update(SettingsPollMsg{})
	if s.String("theme") != "mono" {
		t.Fatalf("the poll replaced the session value: %q", s.String("theme"))
	}
	diff := s.Diff()
	if len(diff) != 1 || diff[0].Saved != "light" || diff[0].SavedIn != OriginFile || diff[0].Session != "mono" || diff[0].Origin.Disk.IsZero() {
		t.Fatalf("diff %+v", diff)
	}
	o, _ := s.Origin("theme")
	if o.Layer != OriginSession || o.Source != "api" || !strings.Contains(o.String(), "settings.json changed on disk at") {
		t.Fatalf("origin %+v %q", o, o.String())
	}
}

// Write moves session values into the file (a reset removes the key);
// ResetAll follows the writes-through rule.
func TestSessionWriteRevertAndResetAll(t *testing.T) {
	r, _, dir := sessionFixture(t)
	s := r.Settings()
	writeSettingsFile(t, dir, "{\n  // mine\n  \"ratio\": 0.25\n}\n")
	s.Reload()
	_, _ = s.SetSession("scrolloff", 5)
	_, _ = s.ResetSession("ratio")
	if s.Float("ratio") != 0.5 || readJSONCSettings(t, dir)["ratio"] != 0.25 {
		t.Fatalf("session reset touched the file: %v", readJSONCSettings(t, dir))
	}
	names, err := s.WriteAll()
	if err != nil || !reflect.DeepEqual(names, []string{"scrolloff", "ratio"}) {
		t.Fatalf("write all: %v %v", names, err)
	}
	file := readJSONCSettings(t, dir)
	if file["scrolloff"] != 5.0 || file["ratio"] != nil || len(s.Diff()) != 0 {
		t.Fatalf("file %v diff %+v", file, s.Diff())
	}
	_, _ = s.SetSession("wrap", true)
	layer, names, err := s.ResetAll()
	if err != nil || layer != OriginSession || !reflect.DeepEqual(names, []string{"scrolloff", "wrap"}) {
		t.Fatalf("reset all: %q %v %v", layer, names, err)
	}
	if s.Int("scrolloff") != 2 || s.Bool("wrap") || readJSONCSettings(t, dir)["scrolloff"] != 5.0 {
		t.Fatalf("session reset all: %d %v %v", s.Int("scrolloff"), s.Bool("wrap"), readJSONCSettings(t, dir))
	}
	if got := s.RevertAll(); !reflect.DeepEqual(got, []string{"scrolloff"}) || s.Int("scrolloff") != 5 {
		t.Fatalf("revert all: %v %d", got, s.Int("scrolloff"))
	}
	_ = s.Set(SettingsAutosaveSetting, true)
	_, _ = s.SetSession("theme", "light")
	if layer, _, err := s.ResetAll(); err != nil || layer != OriginFile || len(readJSONCSettings(t, dir)) != 0 || s.Int("scrolloff") != 2 {
		t.Fatalf("file reset all: %q %v %v", layer, err, readJSONCSettings(t, dir))
	}
}

// Origin names each layer: the embedded file of a framework default, the
// application's declaring file and line, settings.json's path and line,
// and a session value's request source and name.
func TestSettingOrigins(t *testing.T) {
	r, _, dir := sessionFixture(t)
	s := r.Settings()
	if o, _ := s.Origin("modal-dim"); o.Layer != OriginDefault || o.Path != "settings/ochazuke.json" || o.String() != "default, settings/ochazuke.json (ochazuke)" {
		t.Fatalf("framework default %+v %q", o, o.String())
	}
	if o, _ := s.Origin("scrolloff"); o.By != AliasApp || !strings.Contains(o.Path, "settings_test.go:") || !strings.HasPrefix(o.String(), "default, declared by the application at ") {
		t.Fatalf("app default %+v", o)
	}
	writeSettingsFile(t, dir, "{\n  \"wrap\": false,\n  \"scrolloff\": 3\n}\n")
	s.Reload()
	// Written behind the runtime's back, so it changed on disk too.
	if o, _ := s.Origin("scrolloff"); o.Layer != OriginFile || o.Line != 3 || !strings.HasPrefix(o.String(), "set in "+filepath.Join(dir, SettingsFile)+" line 3; settings.json changed on disk at ") {
		t.Fatalf("file %+v %q", o, o.String())
	}
	restore := r.Sourced("palette")
	_, err := r.Execute(Request{Noun: "ochazuke.setting", Verb: "save", Args: Arguments{"name": "scrolloff"}})
	restore()
	if err != nil {
		t.Fatal(err)
	}
	restore = r.Sourced("key:x")
	defer restore()
	_, _ = s.SetSession("scrolloff", 6)
	if o, _ := s.Origin("scrolloff"); o.Layer != OriginSession || o.Source != "key:x" || o.At.IsZero() || !strings.HasPrefix(o.String(), "set this session (key:x, ") {
		t.Fatalf("session %+v %q", o, o.String())
	}
}

// The verbs: setting.revert and setting.write (alias save) on one
// setting, by name or the focused row; settings.write (save, mksettings),
// settings.revert, settings.diff and settings.reset-all (asks first).
func TestSessionVerbs(t *testing.T) {
	r, _, dir := sessionFixture(t)
	s := r.Settings()
	_, _ = s.SetSession("scrolloff", 5)
	_, _ = s.SetSession("theme", "light")
	diff, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "diff"})
	if rows, _ := diff.([]SettingDiff); err != nil || len(rows) != 2 || rows[0].Name != "scrolloff" || rows[0].Saved != 2 || rows[0].Session != 5 || rows[0].SavedIn != OriginDefault {
		t.Fatalf("diff %+v %v", diff, err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.setting", Verb: "revert", Args: Arguments{"name": "theme"}}); err != nil || s.String("theme") != "dark" {
		t.Fatalf("setting revert: %v", err)
	}
	if got, err := r.Execute(Request{Verb: "mksettings"}); err != nil || !reflect.DeepEqual(got, []string{"scrolloff"}) || readSettingsFile(t, dir)["scrolloff"] != 5.0 {
		t.Fatalf("mksettings: %v %v", got, err)
	}
	_, _ = s.SetSession("wrap", true)
	// settings.revert asks first, naming what it drops.
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "revert"}); err != nil || r.TopModal() != "confirm" || r.Instances["confirm"].Payload.Prompt != "Revert 1 session value (wrap) to the saved ones?" {
		t.Fatalf("settings revert did not ask: %q %v", r.TopModal(), err)
	}
	if got, err := r.Execute(Request{Noun: "ochazuke.confirm", Verb: "accept"}); err != nil || !reflect.DeepEqual(got, []string{"wrap"}) {
		t.Fatalf("settings revert: %v %v", got, err)
	}
	_, _ = s.SetSession("wrap", true)
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "save"}); err != nil || readSettingsFile(t, dir)["wrap"] != true {
		t.Fatalf("settings save: %v", err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.setting", Verb: "write"}); err == nil {
		t.Fatal("setting write with no name and no focused row")
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "reset-all"}); err != nil || r.TopModal() != "confirm" {
		t.Fatalf("reset-all did not ask: %q %v", r.TopModal(), err)
	}
	if p := r.Instances[r.TopModal()].Payload.Prompt; p != "Reset all 2 changed settings to their defaults for this session?" {
		t.Fatalf("prompt %q", p)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.confirm", Verb: "accept"}); err != nil || s.Int("scrolloff") != 2 || s.Bool("wrap") {
		t.Fatalf("accept: %v", err)
	}
	for _, alias := range []string{"ochazuke.setting.save", "ochazuke.settings.save", "ochazuke.settings.mksettings"} {
		if a, ok := r.ActionAlias(alias); !ok || a.Source != AliasFramework {
			t.Fatalf("alias %s: %+v %v", alias, a, ok)
		}
	}
}

// From the API (a script, not the CLI) settings set writes without
// asking, as before; only a CLI process asks (cli tests).
func TestSettingsSetFromTheAPIDoesNotAsk(t *testing.T) {
	r, _, dir := settingsFixture(t)
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "set", Args: Arguments{"name": "scrolloff", "value": "4"}}); err != nil || r.TopModal() != "" || readSettingsFile(t, dir)["scrolloff"] != 4.0 {
		t.Fatalf("api set: %q %v", r.TopModal(), err)
	}
}

// The Settings page draws a session value bold with the saved value
// beside it while the two differ, behind settings.mark-session.
func TestSettingsPageMarksSessionValues(t *testing.T) {
	r, _, _ := settingsPageFixture(t)
	s := r.Settings()
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "edit"}); err != nil {
		t.Fatal(err)
	}
	row := func() ListItem {
		for _, it := range r.List(settingsListID(SettingsGeneralSection)).Items() {
			if it.ID == settingRowPrefix+"theme" {
				return it
			}
		}
		t.Fatal("no theme row")
		return ListItem{}
	}
	_, _ = s.SetSession("theme", "light")
	got := row()
	if got.Cells[1] != "‹ light ›  (saved: dark)" || !reflect.DeepEqual(got.Tones, []ToneSpan{SpanTone(1, 0, 9, ToneSession)}) {
		t.Fatalf("marked row %q %+v", got.Cells, got.Tones)
	}
	_ = s.Set(SettingsMarkSessionSetting, false)
	if got := row(); got.Cells[1] != "‹ light ›" || got.Tones != nil {
		t.Fatalf("mark-session off: %q", got.Cells)
	}
	_ = s.Set(SettingsMarkSessionSetting, true)
	_, _ = s.SetSession("theme", "dark")
	if got := row(); got.Cells[1] != "‹ dark ›" {
		t.Fatalf("equal to the saved value: %q", got.Cells)
	}
	_, _ = s.SetSession("theme", "mono")
	_ = r.SetFocus(settingRowPrefix + "theme")
	_, text := settingDetail(s, "theme", -1)
	if !strings.Contains(text, "Set this session (api, ") || !strings.Contains(text, "the default applies beneath it. setting.write saves it, setting.revert drops it.") {
		t.Fatalf("detail %q", text)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.setting", Verb: "write"}); err != nil || row().Cells[1] != "‹ mono ›" {
		t.Fatalf("write from the focused row: %v %q", err, row().Cells)
	}
}
