package ochazuke

import (
	"slices"
	"strings"
	"testing"
)

// configReloadModel is a Model over a temp config directory with a declared
// pager setting (its observed values in the returned slice), a worktree
// noun for actions to land on, and a count of every problem reported.
func configReloadModel(t *testing.T) (*Model, string, *[]any, map[string]int) {
	t.Helper()
	r := NewRuntime()
	r.Define("worktree")
	dir := t.TempDir()
	r.Config().Dir = dir
	if err := r.DeclareSetting(Setting{Name: "pager", Type: SettingString, Default: "less", Enum: []string{"less", "more"}}); err != nil {
		t.Fatal(err)
	}
	var seen []any
	r.Settings().Observe("pager", func(_ string, v any) { seen = append(seen, v) })
	m := NewModel(r)
	reported := map[string]int{}
	prev := r.Config().Report
	r.Config().Report = func(p string) {
		reported[p]++
		prev(p)
	}
	m.Init()
	return m, dir, &seen, reported
}

func reloadConfig(t *testing.T, r *Runtime) []string {
	t.Helper()
	got, err := r.Execute(Request{Noun: "config", Verb: "reload"})
	if err != nil {
		t.Fatal(err)
	}
	return got.([]string)
}

// config.reload reads all four files again: a scheme themes.json gained is
// listed and selected by settings.json, a setting's observer sees its new
// value, an action verb is installed and a bindings.json row naming it
// answers, applied once.
func TestConfigReloadReadsEveryFile(t *testing.T) {
	m, dir, seen, _ := configReloadModel(t)
	r := m.Runtime
	cmd := fakeCommand(t)
	writeConfig(t, dir, ThemesFile, `{"kombu": {"accent": "#C9A227"}}`)
	writeConfig(t, dir, SettingsFile, `{"theme": "kombu", "pager": "more"}`)
	writeConfig(t, dir, ActionsFile, `[{"noun": "worktree", "verb": "open", "command": ["`+strings.Join(cmd, `","`)+`", "echo", "x"]}]`)
	writeConfig(t, dir, BindingsFile, `[{"noun": "worktree", "mode": "normal", "keys": "o", "request": {"noun": "worktree", "verb": "open"}}]`)
	if got := reloadConfig(t, r); len(got) != 0 {
		t.Fatalf("problems %q", got)
	}
	if !slices.Contains(m.Appearance.ThemeNames(), "kombu") || m.Appearance.Theme != "kombu" {
		t.Fatalf("theme %q names %v", m.Appearance.Theme, m.Appearance.ThemeNames())
	}
	if v, _ := r.Settings().Value("pager"); v != "more" || !slices.Equal(*seen, []any{"more"}) {
		t.Fatalf("pager %v observed %v", v, *seen)
	}
	if op, ok := r.Operations["worktree.open"]; !ok || op.Kind != KindApplication {
		t.Fatalf("action not installed: %+v", op)
	}
	if n := len(r.Config().applied); n != 1 {
		t.Fatalf("user rows applied %d times", n)
	}
	rows := 0
	for _, b := range r.Nouns["worktree"].Bindings["normal"] {
		if b.Keys == "o" {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("worktree o rows %d", rows)
	}
	// A second reload leaves exactly the same state.
	if got := reloadConfig(t, r); len(got) != 0 || len(r.Config().applied) != 1 || len(r.UserActions()) != 1 {
		t.Fatalf("second reload %q applied %d actions %d", got, len(r.Config().applied), len(r.UserActions()))
	}
}

// A bad file in each of the four is a problem naming that file, reported
// once, and the rest still reload; fixing them withdraws every notice.
func TestConfigReloadReportsEachBadFile(t *testing.T) {
	m, dir, seen, reported := configReloadModel(t)
	r := m.Runtime
	writeConfig(t, dir, ThemesFile, `{"kombu": {"accent": "teal"}, "ok": {"accent": "#C9A227"}}`)
	writeConfig(t, dir, SettingsFile, `{"pager": "more", "bogus": 1}`)
	writeConfig(t, dir, ActionsFile, `[{"noun": "nowhere", "verb": "x", "command": ["true"]}]`)
	writeConfig(t, dir, BindingsFile, `[{"noun": "app", "mode": "normal", "keys": "Z", "request": {"noun": "app", "verb": "missing"}}]`)
	got := reloadConfig(t, r)
	for i, file := range []string{ThemesFile, SettingsFile, ActionsFile, BindingsFile} {
		if i >= len(got) || !strings.HasPrefix(got[i], file+": ") {
			t.Fatalf("problem %d should name %s: %q", i, file, got)
		}
	}
	if len(got) != 4 {
		t.Fatalf("problems %q", got)
	}
	for _, p := range got {
		if reported[p] != 1 {
			t.Errorf("%q reported %d times", p, reported[p])
		}
	}
	if !slices.Contains(m.Appearance.ThemeNames(), "ok") || !slices.Equal(*seen, []any{"more"}) {
		t.Fatalf("good parts did not reload: names %v pager %v", m.Appearance.ThemeNames(), *seen)
	}
	if len(m.Notices) != 4 {
		t.Fatalf("notices %q", m.Notices)
	}
	for _, name := range []string{ThemesFile, SettingsFile, ActionsFile, BindingsFile} {
		writeConfig(t, dir, name, `[]`)
	}
	writeConfig(t, dir, ThemesFile, `{}`)
	writeConfig(t, dir, SettingsFile, `{}`)
	if got := reloadConfig(t, r); len(got) != 0 || len(m.Notices) != 0 {
		t.Fatalf("after fixing: problems %q notices %q", got, m.Notices)
	}
}

// An application's hook runs after the framework's files, in registration
// order; its problems carry its name (once), are notices, and go at the
// next reload.
func TestConfigReloadRunsApplicationHooks(t *testing.T) {
	m, _, _, _ := configReloadModel(t)
	r := m.Runtime
	var order []string
	bad := true
	r.OnConfigReload("directories.json", func() []string {
		order = append(order, "directories")
		if bad {
			return []string{"entry 2: no path (skipped)", "directories.json: not sorted"}
		}
		return nil
	})
	r.OnConfigReload("extensions", func() []string { order = append(order, "extensions"); return nil })
	got := reloadConfig(t, r)
	want := []string{"directories.json: entry 2: no path (skipped)", "directories.json: not sorted"}
	if !slices.Equal(got, want) || !slices.Equal(order, []string{"directories", "extensions"}) {
		t.Fatalf("problems %q order %v", got, order)
	}
	if len(m.Notices) != 2 {
		t.Fatalf("notices %q", m.Notices)
	}
	bad = false
	if got := reloadConfig(t, r); len(got) != 0 || len(m.Notices) != 0 {
		t.Fatalf("after fixing: %q notices %q", got, m.Notices)
	}
}

// Without a config directory config.reload says so, as the path verbs do;
// it is a builtin fetch with no default key.
func TestConfigReloadNeedsTheConfigDirectory(t *testing.T) {
	r := NewRuntime()
	if _, err := r.Execute(Request{Noun: "config", Verb: "reload"}); err == nil || !strings.Contains(err.Error(), "no config directory") {
		t.Fatalf("err %v", err)
	}
	op := r.Operations["config.reload"]
	if !op.Builtin || op.HistoryKindOf() != HistoryFetch || op.Result != "problem[]" {
		t.Fatalf("operation %+v", op)
	}
}
