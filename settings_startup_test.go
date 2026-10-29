package ochazuke

import (
	"strings"
	"testing"
)

// Every row of settings/ochazuke.json is declared when the runtime is
// made: an application sets forms.single-path at startup, before anything
// reads it and before the Model starts.
func TestFrameworkSettingSetAtStartup(t *testing.T) {
	r := NewRuntime()
	r.Config().Dir = t.TempDir()
	if err := r.Settings().Set(SinglePathSetting, SinglePathForm); err != nil {
		t.Fatalf("set before first use: %v", err)
	}
	if v, _ := r.settingText(SinglePathSetting); v != SinglePathForm {
		t.Fatalf("read back %q", v)
	}
	for _, one := range startupSettings() {
		if r.Settings().find(one.Name) == nil {
			t.Errorf("%s not declared at startup", one.Name)
		}
	}
	// terminal.json's rows wait for EnsureTerminalLeader.
	if r.Settings().find(TerminalLeaderSetting) != nil {
		t.Fatal("terminal-leader declared without terminals")
	}
	r.EnsureTerminalLeader()
	if r.Settings().find(TerminalLeaderSetting) == nil {
		t.Fatal("EnsureTerminalLeader did not declare terminal-leader")
	}
}

// A user's settings.json naming framework settings loads with no warning,
// whether it is read before the Model starts or by Model.Init; a setting
// declared after the file was read (an extension's) withdraws its
// unknown-setting warning.
func TestFrameworkSettingsFileLoadsWithoutWarning(t *testing.T) {
	r := NewRuntime()
	dir := t.TempDir()
	r.Config().Dir = dir
	var withdrawn []string
	r.Config().Withdraw = func(p string) { withdrawn = append(withdrawn, p) }
	writeConfig(t, dir, SettingsFile, `{"forms.single-path": "alone", "modal-dim": 10, "focus-display": "noun", "ext.late": 3}`)
	if got := r.Settings().String(SinglePathSetting); got != SinglePathAlone {
		t.Fatalf("read before Init: %q", got)
	}
	if err := r.DeclareSetting(Setting{Name: "ext.late", Type: SettingInteger, Default: 1}); err != nil {
		t.Fatal(err)
	}
	if r.Settings().Int("ext.late") != 3 || len(withdrawn) != 1 || !strings.Contains(withdrawn[0], `"ext.late"`) {
		t.Fatalf("late setting %d, withdrawn %q", r.Settings().Int("ext.late"), withdrawn)
	}
	m := NewModel(r)
	m.Init()
	for _, n := range m.Notices {
		if strings.Contains(n, "unknown setting") {
			t.Fatalf("warned: %q", m.Notices)
		}
	}
	if r.Settings().Int(ModalDimSetting) != 10 || r.FocusDisplay() != FocusDisplayNoun {
		t.Fatalf("values %+v", r.Settings().List())
	}
}

// An application declaring a framework setting itself, before or after
// the runtime declared it, replaces the row with its own default; the
// file's value still wins, and a second declaration is refused.
func TestApplicationDeclaresFrameworkSetting(t *testing.T) {
	r := NewRuntime()
	if err := r.DeclareSetting(Setting{Name: SinglePathSetting, Type: SettingString, Default: SinglePathForm, Enum: []string{SinglePathChooser, SinglePathAlone, SinglePathForm}, Description: "app's"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := r.settingText(SinglePathSetting); v != SinglePathForm {
		t.Fatalf("app default not taken: %q", v)
	}
	if err := r.DeclareSetting(Setting{Name: SinglePathSetting, Type: SettingString, Default: SinglePathAlone}); err == nil {
		t.Fatal("second declaration accepted")
	}
	// The embedded check stays when the application gives none.
	if err := r.DeclareSetting(Setting{Name: ModalDimSetting, Type: SettingInteger, Default: 50}); err != nil {
		t.Fatal(err)
	}
	if err := r.Settings().Set(ModalDimSetting, 500); err == nil {
		t.Fatal("modal-dim 500 accepted: the embedded check was dropped")
	}
	// An old name of a framework setting declares it with the app's default.
	if err := r.DeclareSetting(Setting{Name: "terminal-leader-timeout", Type: SettingDuration, Default: "2s"}); err != nil {
		t.Fatal(err)
	}
	if got := r.sequenceTimeout(); got.String() != "2s" {
		t.Fatalf("sequence-timeout %v", got)
	}
}

// The Go fallbacks the dim reads use match the embedded defaults.
func TestDimDefaultsMatchEmbeddedRows(t *testing.T) {
	for name, want := range map[string]int{ModalDimSetting: DefaultModalDim, DimUnfocusedSetting: DefaultDimUnfocused} {
		if one, ok := frameworkSetting(name); !ok || one.Default != want {
			t.Errorf("%s: embedded %v, Go %d", name, one.Default, want)
		}
	}
}
