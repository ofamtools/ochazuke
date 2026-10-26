package ochazuke

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// configFixture is a runtime whose config directory is a temp dir, with
// app rows a and b on the test noun and a Model over it (not started).
func configFixture(t *testing.T) (*Runtime, *Model, *[]string, string) {
	t.Helper()
	r := NewRuntime()
	calls := recorder(t, r, "alpha", "beta", "gamma")
	r.Define("app").Mode("normal").
		Bind("a", Do("test.alpha")).
		Bind("b", Do("test.beta"))
	dir := t.TempDir()
	r.Config().Dir = dir
	m := NewModel(r)
	return r, m, calls, dir
}

func writeConfig(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func helpKeys(r *Runtime) map[string]string {
	out := map[string]string{}
	for _, row := range BindingHelp(r, r.Focus) {
		out[row.Key] = row.Operation
	}
	return out
}

// The directory: Dir, else $XDG_CONFIG_HOME/<app>, else ~/.config/<app>;
// a test binary has no app name, so no directory at all.
func TestConfigDirectoryFollowsXDGThenHome(t *testing.T) {
	r := NewRuntime()
	c := r.Config()
	if c.App != "" || c.Directory() != "" || c.Path(BindingsFile) != "" {
		t.Fatalf("a test binary has a config directory: %+v %q", c, c.Directory())
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	c.App = "demo"
	if got := c.Path(BindingsFile); got != filepath.Join(home, "xdg", "demo", "bindings.json") {
		t.Fatalf("xdg path %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	if got := c.Path(SettingsFile); got != filepath.Join(home, ".config", "demo", "settings.json") {
		t.Fatalf("home path %q", got)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	if got := c.HistoryPath(); got != filepath.Join(home, "state", "demo", HistoryFile) {
		t.Fatalf("xdg state path %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	if got := c.HistoryPath(); got != filepath.Join(home, ".local", "state", "demo", HistoryFile) {
		t.Fatalf("home state path %q", got)
	}
	c.Dir = "/elsewhere"
	if got := c.Path(BindingsFile); got != "/elsewhere/bindings.json" {
		t.Fatalf("dir path %q", got)
	}
	if got, err := r.Execute(Request{Noun: "bindings", Verb: "path"}); err != nil || got != "/elsewhere/bindings.json" {
		t.Fatalf("bindings.path %v %v", got, err)
	}
	c.Disable()
	if _, err := r.Execute(Request{Noun: "bindings", Verb: "path"}); err == nil {
		t.Fatal("bindings.path without a directory")
	}
}

// No file: the defaults stand and nothing is reported.
func TestMissingBindingsFileIsNothing(t *testing.T) {
	r, m, calls, _ := configFixture(t)
	m.Init()
	keytest.Press(m, "a")
	if len(m.Notices) != 0 || !reflect.DeepEqual(*calls, []string{"alpha"}) {
		t.Fatalf("notices %v calls %v", m.Notices, *calls)
	}
	if got, _ := r.Execute(Request{Noun: "bindings", Verb: "check"}); len(got.([]string)) != 0 {
		t.Fatalf("check %v", got)
	}
}

// Acceptance: a file that rebinds one key and unbinds another takes effect
// at start; ? and bindings.list show it; a malformed row is a notice and
// the rest of the file still applies.
func TestUserBindingsRebindUnbindAndReportBadRows(t *testing.T) {
	r, m, calls, dir := configFixture(t)
	writeConfig(t, dir, BindingsFile, `[
		{"noun": "app", "mode": "normal", "keys": "a", "request": {"noun": "test", "verb": "gamma"}},
		{"noun": "app", "mode": "normal", "keys": "b", "request": null},
		{"noun": "app", "mode": "normal", "keys": "z", "request": {"noun": "test", "verb": "nope"}},
		{"noun": 7}
	]`)
	m.Init()
	keytest.Press(m, "a")
	keytest.Press(m, "b")
	if !reflect.DeepEqual(*calls, []string{"gamma"}) {
		t.Fatalf("calls %v", *calls)
	}
	keys := helpKeys(r)
	if keys["a"] != "test.gamma" || keys["b"] != "" || keys["z"] != "" {
		t.Fatalf("help keys %v", keys)
	}
	listed, _ := r.Execute(Request{Noun: "bindings", Verb: "list"})
	found := false
	for _, b := range listed.([]Binding) {
		found = found || b.Keys == "a" && b.Request.Key() == "test.gamma"
	}
	if !found {
		t.Fatalf("bindings.list lacks the override: %+v", listed)
	}
	if len(m.Notices) != 2 || !strings.Contains(m.Notices[0], "row 4") || !strings.Contains(m.Notices[1], `row 3 z: unknown operation "test.nope"`) {
		t.Fatalf("notices %q", m.Notices)
	}
	check, _ := r.Execute(Request{Noun: "bindings", Verb: "check"})
	if len(check.([]string)) != 2 {
		t.Fatalf("check %v", check)
	}
}

// A file that does not parse installs nothing: the app starts with its
// defaults and a notice.
func TestMalformedBindingsFileKeepsDefaults(t *testing.T) {
	_, m, calls, dir := configFixture(t)
	writeConfig(t, dir, BindingsFile, `{"noun": "app"`)
	m.Init()
	keytest.Press(m, "a")
	keytest.Press(m, "b")
	if !reflect.DeepEqual(*calls, []string{"alpha", "beta"}) || len(m.Notices) != 1 || !strings.HasPrefix(m.Notices[0], "bindings.json: not a JSON array") {
		t.Fatalf("calls %v notices %q", *calls, m.Notices)
	}
}

// bindings.reload takes the old file's rows out (the defaults they
// replaced come back) and applies the new file; the old file's notices go.
func TestBindingsReloadRestoresDefaultsAndAppliesTheNewFile(t *testing.T) {
	r, m, calls, dir := configFixture(t)
	writeConfig(t, dir, BindingsFile, `[
		{"noun": "app", "mode": "normal", "keys": "a", "request": {"noun": "test", "verb": "gamma"}},
		{"noun": "app", "mode": "normal", "keys": "b", "request": null},
		{"noun": "app", "mode": "normal", "keys": "q q", "request": {"noun": "test", "verb": "alpha"}},
		{"noun": "app", "mode": "normal", "keys": "x", "request": {"noun": "test", "verb": "nope"}}
	]`)
	m.Init()
	if r.EnsureUserBindings() != nil {
		t.Fatal("EnsureUserBindings ran twice")
	}
	writeConfig(t, dir, BindingsFile, `[
		{"noun": "app", "mode": "normal", "keys": "b", "request": {"noun": "test", "verb": "gamma"}},
		{"noun": "app", "mode": "normal", "keys": "x", "request": {"noun": "test", "verb": "nope"}}
	]`)
	got, err := r.Execute(Request{Noun: "bindings", Verb: "reload"})
	if err != nil || len(got.([]string)) != 1 {
		t.Fatalf("reload %v %v", got, err)
	}
	keytest.Press(m, "a")
	keytest.Press(m, "b")
	if !reflect.DeepEqual(*calls, []string{"alpha", "gamma"}) {
		t.Fatalf("calls %v", *calls)
	}
	if len(m.Notices) != 1 || !strings.Contains(m.Notices[0], "row 2 x") {
		t.Fatalf("the old notice was not taken back: %q", m.Notices)
	}
	keys := helpKeys(r)
	if keys["q q"] != "" || keys["q esc"] != "" {
		t.Fatalf("the old sequence and its cancel survived: %v", keys)
	}
	if err := os.Remove(filepath.Join(dir, BindingsFile)); err != nil {
		t.Fatal(err)
	}
	r.ReloadUserBindings()
	if len(m.Notices) != 0 {
		t.Fatalf("notices after removing the file: %q", m.Notices)
	}
	keytest.Press(m, "b")
	if !slices.Equal((*calls)[2:], []string{"beta"}) {
		t.Fatalf("after removing the file: %v", *calls)
	}
}

// guardFixture is configFixture with a "flag" guard the test toggles and
// two default rows on a: test.alpha while the flag holds, test.beta
// otherwise. It returns the runtime, the model, the calls, the directory,
// the flag and the app normal table as it stands before any file.
func guardFixture(t *testing.T) (*Runtime, *Model, *[]string, string, *bool, []string) {
	t.Helper()
	r, m, calls, dir := configFixture(t)
	flag := new(bool)
	r.DefineGuard("flag", func(*Runtime, KeyEvent) bool { return *flag })
	r.Define("app").Mode("normal").
		Bind("a", Do("test.alpha")).When("flag").
		Bind("a", Do("test.beta"))
	return r, m, calls, dir, flag, appNormal(t, r)
}

// appNormal is the app noun's normal table, a canonical row per entry.
func appNormal(t *testing.T, r *Runtime) []string {
	t.Helper()
	var out []string
	for _, b := range r.Nouns["app"].Bindings["normal"] {
		out = append(out, canonical(t, b))
	}
	return out
}

// pressBoth presses key with the flag on, then off, and returns the calls
// the two presses made.
func pressBoth(m *Model, calls *[]string, flag *bool, key string) []string {
	start := len(*calls)
	*flag = true
	keytest.Press(m, key)
	*flag = false
	keytest.Press(m, key)
	return append([]string(nil), (*calls)[start:]...)
}

// The guard is part of a row's identity: a file row with when replaces
// only the guarded row on its keys, one without when only the unguarded
// row, an unbind removes only the row with its guard, a new guarded row
// goes before the unguarded one so it answers while its guard holds, and
// each reload puts the table back exactly as it was before.
func TestUserBindingsGuardIsPartOfTheRowIdentity(t *testing.T) {
	r, m, calls, dir, flag, defaults := guardFixture(t)
	m.Init()
	if got := pressBoth(m, calls, flag, "a"); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("defaults: %v", got)
	}
	steps := []struct {
		name string
		file string
		key  string
		want []string // flag on, flag off
	}{
		{"guarded row replaces the guarded default", `[{"noun": "app", "mode": "normal", "keys": "a", "when": "flag", "request": {"noun": "test", "verb": "gamma"}}]`, "a", []string{"gamma", "beta"}},
		{"unguarded row replaces the unguarded default", `[{"noun": "app", "mode": "normal", "keys": "a", "request": {"noun": "test", "verb": "gamma"}}]`, "a", []string{"alpha", "gamma"}},
		{"guarded unbind", `[{"noun": "app", "mode": "normal", "keys": "a", "when": "flag", "request": null}]`, "a", []string{"beta", "beta"}},
		{"unguarded unbind", `[{"noun": "app", "mode": "normal", "keys": "a", "request": null}]`, "a", []string{"alpha"}},
		{"new guarded row answers before the unguarded default", `[{"noun": "app", "mode": "normal", "keys": "b", "when": "flag", "request": {"noun": "test", "verb": "gamma"}}]`, "b", []string{"gamma", "beta"}},
		{"both rows from one file", `[
			{"noun": "app", "mode": "normal", "keys": "a", "request": {"noun": "test", "verb": "gamma"}},
			{"noun": "app", "mode": "normal", "keys": "a", "when": "flag", "request": {"noun": "test", "verb": "beta"}}
		]`, "a", []string{"beta", "gamma"}},
	}
	for _, step := range steps {
		writeConfig(t, dir, BindingsFile, step.file)
		if problems := r.ReloadUserBindings(); len(problems) != 0 {
			t.Fatalf("%s: %v", step.name, problems)
		}
		if got := pressBoth(m, calls, flag, step.key); !slices.Equal(got, step.want) {
			t.Fatalf("%s: calls %v, want %v", step.name, got, step.want)
		}
		writeConfig(t, dir, BindingsFile, `[]`)
		r.ReloadUserBindings()
		if got := appNormal(t, r); !slices.Equal(got, defaults) {
			t.Fatalf("%s: reload did not restore the defaults:\n%v\nwant\n%v", step.name, got, defaults)
		}
	}
}

// bindings.load installs a guarded and an unguarded row on the same keys
// side by side, and loading the table's own rows back changes nothing.
func TestBindingsLoadKeepsGuardedAndUnguardedRowsApart(t *testing.T) {
	r, m, calls, dir, flag, _ := guardFixture(t)
	m.Init()
	path := filepath.Join(dir, "more.json")
	writeConfig(t, dir, "more.json", `[
		{"noun": "app", "mode": "normal", "keys": "c", "request": {"noun": "test", "verb": "beta"}},
		{"noun": "app", "mode": "normal", "keys": "c", "when": "flag", "request": {"noun": "test", "verb": "gamma"}}
	]`)
	if _, err := r.Execute(Request{Noun: "bindings", Verb: "load", Args: Arguments{"path": path}}); err != nil {
		t.Fatal(err)
	}
	if got := pressBoth(m, calls, flag, "c"); !slices.Equal(got, []string{"gamma", "beta"}) {
		t.Fatalf("calls %v", got)
	}
	before := appNormal(t, r)
	if _, err := r.Execute(Request{Noun: "bindings", Verb: "load", Args: Arguments{"path": path}}); err != nil {
		t.Fatal(err)
	}
	if got := appNormal(t, r); !slices.Equal(got, before) {
		t.Fatalf("a second load changed the table:\n%v\nwant\n%v", got, before)
	}
}

// The two default ctrl+c rows (the capture row guarded by no-modal and the
// bubbling one) are unbound one at a time: an unbind without when leaves
// the capture row quitting; the guarded unbind takes it out too.
func TestCtrlCRowsUnbindOneAtATime(t *testing.T) {
	r, _, _, _ := configFixture(t)
	quits := func() []string {
		var out []string
		for _, b := range r.Nouns["app"].Bindings[""] {
			if b.Keys == "ctrl+c" {
				out = append(out, b.When)
			}
		}
		return out
	}
	if got := quits(); !slices.Equal(got, []string{"", "no-modal"}) {
		t.Fatalf("default ctrl+c rows: %q", got)
	}
	if _, err := r.LoadBindings([]byte(`[{"noun": "app", "keys": "ctrl+c", "request": null}]`)); err != nil {
		t.Fatal(err)
	}
	if got := quits(); !slices.Equal(got, []string{"no-modal"}) {
		t.Fatalf("after the unguarded unbind: %q", got)
	}
	if _, err := r.LoadBindings([]byte(`[{"noun": "app", "keys": "ctrl+c", "when": "no-modal", "request": null}]`)); err != nil {
		t.Fatal(err)
	}
	if got := quits(); len(got) != 0 {
		t.Fatalf("after both unbinds: %q", got)
	}
}
