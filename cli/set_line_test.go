package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// setFixture is the widget fixture with a settings.json folder.
func setFixture(t *testing.T) (*fixture, string) {
	t.Helper()
	f := newFixture(t)
	dir := t.TempDir()
	f.r.Config().Dir = dir
	return f, dir
}

func savedSettings(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ochazuke.SettingsFile))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// A rest parameter takes the remaining words joined by spaces: no quotes
// needed for a :set line, from the shell and through Mount.
func TestCLIRestParameterJoinsTheWords(t *testing.T) {
	f, dir := setFixture(t)
	if _, errOut, err := f.run("", "setting", "apply", "modal-dim=40", "nohelp.pick-view", "--yes"); err != nil {
		t.Fatalf("apply: %v\n%s", err, errOut)
	}
	if got := savedSettings(t, dir); got["modal-dim"] != 40.0 || got["help.pick-view"] != false {
		t.Fatalf("saved %v", got)
	}
	if _, errOut, err := f.mounted(t, "setting", "apply", "dim-unfocused=30", "modal-dim=10", "--yes"); err != nil {
		t.Fatalf("mounted apply: %v\n%s", err, errOut)
	}
	if got := savedSettings(t, dir); got["modal-dim"] != 10.0 || got["dim-unfocused"] != 30.0 {
		t.Fatalf("saved %v", got)
	}
}

// bin set reads as the palette does: set NOUN runs NOUN set, set opt is
// the :set line, and verbose before it adds where values were set.
func TestCLIBareSet(t *testing.T) {
	for _, mount := range []bool{false, true} {
		f, dir := setFixture(t)
		run := f.run
		if mount {
			run = func(_ string, args ...string) (string, string, error) { return f.mounted(t, args...) }
		}
		if _, errOut, err := run("", "set", "modal-dim=20", "--yes"); err != nil {
			t.Fatalf("mount %v set: %v\n%s", mount, err, errOut)
		}
		if got := savedSettings(t, dir); got["modal-dim"] != 20.0 {
			t.Fatalf("mount %v saved %v", mount, got)
		}
		// set NOUN runs a noun's named set: set setting NAME VALUE.
		if _, errOut, err := run("", "set", "setting", "modal-dim", "30", "--yes"); err != nil || savedSettings(t, dir)["modal-dim"] != 30.0 {
			t.Fatalf("mount %v set setting modal-dim 30: %v %v\n%s", mount, err, savedSettings(t, dir), errOut)
		}
		_, errOut, err := run("", "verbose", "set", "dim-unfocused?")
		if err != nil || !strings.Contains(errOut, "dim-unfocused=20 — default") {
			t.Fatalf("mount %v verbose set: %v\n%s", mount, err, errOut)
		}
	}
}

// settings review from a CLI process has no session to review: it says
// so (exit 0) and points at settings diff.
func TestCLISettingsReviewHasNoSession(t *testing.T) {
	f, _ := setFixture(t)
	out, errOut, err := f.run("", "setting", "review")
	if err != nil || !strings.Contains(out, "no session to review") || !strings.Contains(out, "setting diff") {
		t.Fatalf("review: %q %q %v", out, errOut, err)
	}
}

func TestCLIExplicitShadowVerb(t *testing.T) {
	for _, mount := range []bool{false, true} {
		f, dir := setFixture(t)
		if err := f.r.AddActionAlias(ochazuke.ActionAlias{
			Alias: "adjust", Action: "ochazuke.setting.apply", ShadowVerb: "set",
		}); err != nil {
			t.Fatal(err)
		}
		run := f.run
		if mount {
			run = func(_ string, args ...string) (string, string, error) { return f.mounted(t, args...) }
		}
		if _, errOut, err := run("", "adjust", "setting", "modal-dim", "25", "--yes"); err != nil || savedSettings(t, dir)["modal-dim"] != 25.0 {
			t.Fatalf("mount %v adjust setting: %v\n%s", mount, err, errOut)
		}
		if _, errOut, err := run("", "adjust", "modal-dim=35", "--yes"); err != nil {
			t.Fatalf("mount %v adjust setting: %v\n%s", mount, err, errOut)
		}
		if got := savedSettings(t, dir); got["modal-dim"] != 35.0 {
			t.Fatalf("mount %v saved %v", mount, got)
		}
	}
}
