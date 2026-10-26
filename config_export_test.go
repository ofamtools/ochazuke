package ochazuke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// config.path prints the config, state and default reference directories;
// a sandbox has none and says so.
func TestConfigPathPrintsTheConfigAndStateDirectories(t *testing.T) {
	r := NewRuntime()
	if _, err := r.Execute(Request{Verb: "path", Noun: "config"}); err == nil {
		t.Fatal("config.path answered without any directory")
	}
	dir, state := t.TempDir(), t.TempDir()
	r.Config().Dir, r.Config().StateDir = dir, state
	got, err := r.Execute(Request{Verb: "path", Noun: "config"})
	if err != nil {
		t.Fatal(err)
	}
	want := ConfigPaths{Config: dir, State: state, Reference: filepath.Join(dir, ReferenceDir)}
	if got != want {
		t.Fatalf("config.path = %+v, want %+v", got, want)
	}
}

// exportFixture is a runtime with a temp config directory, three declared
// settings (one set in settings.json) and app rows, sequences included.
func exportFixture(t *testing.T) (*Runtime, string) {
	t.Helper()
	r := NewRuntime()
	recorder(t, r, "alpha", "beta")
	r.Define("app").Mode("normal").
		Bind("a", Do("test.alpha")).
		Bind("g b", Do("test.beta"))
	dir := t.TempDir()
	r.Config().Dir = dir
	if err := r.DeclareSetting(
		Setting{Name: "pager", Type: SettingString, Default: "less", Description: "The pager", Enum: []string{"less", "more"}},
		Setting{Name: "lines", Type: SettingInteger, Default: 20},
		Setting{Name: "delay", Type: SettingDuration, Default: "400ms", Description: "Delay"},
	); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, SettingsFile, `{"pager": "more"}`)
	return r, dir
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, data)
	}
}

// config.export writes into <config dir>/reference by default and lists
// the files it wrote.
func TestConfigExportWritesTheReferenceDirectory(t *testing.T) {
	r, dir := exportFixture(t)
	got, err := r.Execute(Request{Verb: "export", Noun: "config"})
	if err != nil {
		t.Fatal(err)
	}
	export := got.(ConfigExport)
	ref := filepath.Join(dir, ReferenceDir)
	if export.Dir != ref || export.Config != dir {
		t.Fatalf("export %+v", export)
	}
	for _, name := range []string{SettingsFile, SettingsReferenceFile, BindingsReferenceFile} {
		if !slices.Contains(export.Files, filepath.Join(ref, name)) {
			t.Fatalf("%s not in %v", name, export.Files)
		}
	}
	for _, f := range export.Files {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("listed %s: %v", f, err)
		}
	}
	entries, _ := os.ReadDir(ref)
	if len(entries) != len(export.Files) {
		t.Fatalf("directory holds %d files, export lists %d (temp files left?)", len(entries), len(export.Files))
	}
}

// Every declared setting appears in both settings files: settings.json at
// its current value (valid to copy in as is), the reference with its
// type, default, choices and description.
func TestConfigExportListsEverySetting(t *testing.T) {
	r, dir := exportFixture(t)
	out := filepath.Join(t.TempDir(), "out")
	if _, err := r.ExportConfig(out, false); err != nil {
		t.Fatal(err)
	}
	var current map[string]any
	readJSON(t, filepath.Join(out, SettingsFile), &current)
	var reference []SettingValue
	readJSON(t, filepath.Join(out, SettingsReferenceFile), &reference)
	declared := r.Settings().Declared()
	if len(current) != len(declared) || len(reference) != len(declared) {
		t.Fatalf("%d declared, settings.json %v, reference %d", len(declared), current, len(reference))
	}
	for i, one := range declared {
		ref := reference[i]
		value, _ := r.Settings().Get(one.Name)
		if ref.Name != one.Name || ref.Type != one.Type || ref.Description != one.Description || !slices.Equal(ref.Enum, one.Enum) {
			t.Fatalf("reference %d %+v, declared %+v", i, ref, one)
		}
		if fmt.Sprint(ref.Default) != fmt.Sprint(fileForm(one.Default)) || fmt.Sprint(ref.Value) != fmt.Sprint(value.Value) {
			t.Fatalf("%s default %v value %v, want %v %v", one.Name, ref.Default, ref.Value, fileForm(one.Default), value.Value)
		}
		if fmt.Sprint(current[one.Name]) != fmt.Sprint(value.Value) {
			t.Fatalf("settings.json %s = %v, want %v", one.Name, current[one.Name], value.Value)
		}
	}
	if current["pager"] != "more" || current["delay"] != "400ms" {
		t.Fatalf("settings.json %v", current)
	}
	// The framework's own settings are declared before the export.
	if _, ok := current[ThemeSetting]; !ok {
		t.Fatalf("theme missing from %v", current)
	}
	// The exported settings.json reads back without a problem.
	other := NewRuntime()
	other.Config().Dir = out
	for _, one := range declared {
		if err := other.DeclareSetting(one); err != nil {
			t.Fatal(err)
		}
	}
	if problems := other.Settings().Problems(); len(problems) > 0 {
		t.Fatalf("exported settings.json has problems: %v", problems)
	}
	if other.Settings().String("pager") != "more" {
		t.Fatal("exported settings.json lost the set value")
	}
	_ = dir
}

// The bindings reference is every stored row, and every row loads back
// through bindings.load's validation.
func TestConfigExportBindingsRoundTripThroughBindingsLoad(t *testing.T) {
	r, _ := exportFixture(t)
	out := t.TempDir()
	if _, err := r.ExportConfig(out, false); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(out, BindingsReferenceFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []Binding
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(r.allBindings()) {
		t.Fatalf("%d rows, the runtime stores %d", len(rows), len(r.allBindings()))
	}
	keys := map[string]bool{}
	for _, row := range rows {
		keys[row.Noun+"/"+row.Mode+"/"+row.Keys] = true
	}
	for _, want := range []string{"app/normal/a", "app/normal/g b", "editor/insert/ctrl+a"} {
		if !keys[want] {
			t.Fatalf("row %s missing from the reference", want)
		}
	}
	if fileRows, problems := r.readUserBindings(path); len(problems) > 0 || len(fileRows) != len(rows) {
		t.Fatalf("bindings.json reader: %d rows, problems %v", len(fileRows), problems)
	}
	if _, err := r.Execute(Request{Verb: "load", Noun: "bindings", Args: Arguments{"path": path}}); err != nil {
		t.Fatalf("bindings.load of the reference: %v", err)
	}
	if _, err := r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
	// Loaded back, each row stands as it was, except where one table has
	// two rows on the same keys (a guarded row and its fallback): a loaded
	// row replaces every row on its keys, so the last one stands.
	table := func(b Binding) string { return b.Noun + "/" + b.Instance + "/" + b.Mode + "/" + b.Keys + b.Pattern }
	shared := map[string]int{}
	for _, row := range rows {
		shared[table(row)]++
	}
	stored := map[string]bool{}
	for _, row := range r.allBindings() {
		stored[canonical(t, row)] = true
	}
	for _, row := range rows {
		if shared[table(row)] == 1 && !stored[canonical(t, row)] {
			t.Fatalf("row %s changed on load", canonical(t, row))
		}
	}
}

// canonical is a row's JSON with object keys sorted.
func canonical(t *testing.T, row Binding) string {
	t.Helper()
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(v)
	return string(data)
}

// The live files are never written without force: exporting into the
// config directory itself is refused and writes nothing; force writes.
func TestConfigExportNeverOverwritesTheLiveFilesWithoutForce(t *testing.T) {
	r, dir := exportFixture(t)
	live := filepath.Join(dir, SettingsFile)
	before, _ := os.ReadFile(live)
	if _, err := r.Execute(Request{Verb: "export", Noun: "config", Args: Arguments{"dir": dir}}); err == nil || !strings.Contains(err.Error(), "live settings.json") {
		t.Fatalf("export into the config directory: %v", err)
	}
	if after, _ := os.ReadFile(live); !bytes.Equal(before, after) {
		t.Fatal("the live settings.json changed")
	}
	if _, err := os.Stat(filepath.Join(dir, SettingsReferenceFile)); err == nil {
		t.Fatal("a refused export wrote a file")
	}
	// Through a symbolic link to the config directory too.
	link := filepath.Join(t.TempDir(), "cfg")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ExportConfig(link, false); err == nil {
		t.Fatal("export through a link to the config directory was not refused")
	}
	if _, err := r.Execute(Request{Verb: "export", Noun: "config", Args: Arguments{"dir": dir, "force": true}}); err != nil {
		t.Fatal(err)
	}
	var current map[string]any
	readJSON(t, live, &current)
	if current["lines"] != float64(20) {
		t.Fatalf("forced export left settings.json %v", current)
	}
}

// Each file is replaced by a rename, never rewritten in place: a hard
// link to the old file keeps the old content, and no temp file is left.
func TestConfigExportWritesAtomically(t *testing.T) {
	r, _ := exportFixture(t)
	out := t.TempDir()
	path := filepath.Join(out, SettingsReferenceFile)
	writeConfig(t, out, SettingsReferenceFile, "old")
	keep := filepath.Join(t.TempDir(), "keep")
	if err := os.Link(path, keep); err != nil {
		t.Skip("no hard links here:", err)
	}
	if _, err := r.ExportConfig(out, false); err != nil {
		t.Fatal(err)
	}
	if old, _ := os.ReadFile(keep); string(old) != "old" {
		t.Fatalf("the old file was written in place: %q", old)
	}
	if now, _ := os.ReadFile(path); !json.Valid(now) {
		t.Fatalf("new file %q", now)
	}
	entries, _ := os.ReadDir(out)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Fatalf("temp file %s left", e.Name())
		}
	}
}

// Without a config directory the export needs a directory.
func TestConfigExportWithoutAConfigDirectory(t *testing.T) {
	r := NewRuntime()
	if _, err := r.ExportConfig("", false); err == nil {
		t.Fatal("export with no directory at all")
	}
	out := t.TempDir()
	got, err := r.ExportConfig(out, false)
	if err != nil || got.Dir != out || got.Config != "" {
		t.Fatalf("export %+v %v", got, err)
	}
}

// The example files are valid as they are: copied in as actions.json and
// themes.json they install with no notice, and between them they use
// every field an action and a scheme take.
func TestConfigExportExamplesInstallWithoutNotices(t *testing.T) {
	r, _ := exportFixture(t)
	out := t.TempDir()
	if _, err := r.ExportConfig(out, false); err != nil {
		t.Fatal(err)
	}
	actions, _ := os.ReadFile(filepath.Join(out, ActionsExampleFile))
	themes, _ := os.ReadFile(filepath.Join(out, ThemesExampleFile))

	live := NewRuntime()
	live.Config().Dir = t.TempDir()
	var notices []string
	live.Config().Report = func(p string) { notices = append(notices, p) }
	writeConfig(t, live.Config().Dir, ActionsFile, string(actions))
	writeConfig(t, live.Config().Dir, ThemesFile, string(themes))
	if problems := live.EnsureUserActions(); len(problems) > 0 {
		t.Fatalf("actions example: %v", problems)
	}
	if problems := live.ReloadThemes(); len(problems) > 0 {
		t.Fatalf("themes example: %v", problems)
	}
	if len(notices) > 0 {
		t.Fatalf("notices: %v", notices)
	}
	installed := live.UserActions()
	if len(installed) != 2 || live.Operations["app.example-run"].Noun == "" || live.Operations["app.example-call"].Noun == "" {
		t.Fatalf("installed %+v", installed)
	}
	if names := live.Config().themeNames(); !slices.Contains(names, "example") || !slices.Contains(names, "example-dark") {
		t.Fatalf("theme names %v", names)
	}

	// Every Action field and every Parameter field appears in the file.
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(actions, &entries); err != nil {
		t.Fatal(err)
	}
	seen, params := map[string]bool{}, map[string]bool{}
	for _, e := range entries {
		for k := range e {
			seen[k] = true
		}
		var ps map[string]map[string]any
		_ = json.Unmarshal(e["parameters"], &ps)
		for _, p := range ps {
			for k := range p {
				params[k] = true
			}
		}
	}
	for _, f := range jsonFields(Action{}) {
		if !seen[f] {
			t.Errorf("actions example lacks %q", f)
		}
	}
	for _, f := range jsonFields(Parameter{}) {
		if !params[f] {
			t.Errorf("actions example lacks parameter field %q", f)
		}
	}
	var schemes map[string]map[string]any
	if err := json.Unmarshal(themes, &schemes); err != nil {
		t.Fatal(err)
	}
	for _, role := range append([]string{"background"}, jsonFields(Scheme{})...) {
		for name, s := range schemes {
			if _, ok := s[role]; !ok {
				t.Errorf("scheme %s lacks %q", name, role)
			}
		}
	}
}

// jsonFields are a struct's JSON field names.
func jsonFields(v any) []string {
	var out []string
	rt := reflect.TypeOf(v)
	for i := range rt.NumField() {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}
