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
