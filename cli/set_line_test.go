package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke"
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
	if _, errOut, err := f.run("", "settings", "apply", "modal-dim=40", "nohelp.pick-view", "--yes"); err != nil {
		t.Fatalf("apply: %v\n%s", err, errOut)
	}
	if got := savedSettings(t, dir); got["modal-dim"] != 40.0 || got["help.pick-view"] != false {
		t.Fatalf("saved %v", got)
	}
	if _, errOut, err := f.mounted(t, "settings", "apply", "dim-unfocused=30", "modal-dim=10", "--yes"); err != nil {
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
		out, errOut, err := run("", "set", "mode", "normal")
		if err != nil || !strings.Contains(out, `"normal"`) {
			t.Fatalf("mount %v set mode normal: %v\n%s\n%s", mount, err, out, errOut)
		}
		_, errOut, err = run("", "verbose", "set", "dim-unfocused?")
		if err != nil || !strings.Contains(errOut, "dim-unfocused=20 — default") {
			t.Fatalf("mount %v verbose set: %v\n%s", mount, err, errOut)
		}
		if _, _, err := run("", "set", "foc"); err == nil || !strings.Contains(err.Error(), `"foc" is ambiguous: noun focus (focus set), or line focus-display`) {
			t.Fatalf("mount %v set foc: %v", mount, err)
		}
	}
}
