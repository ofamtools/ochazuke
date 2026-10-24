package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"ochazuke"
	"ochazuke/internal/keytest"
)

func TestBindingSetupRoundTrip(t *testing.T) {
	m := &keybindingModel{width: 100, height: 24, runtime: ochazuke.NewRuntime()}
	typeText := func(s string) { keytest.Type(m, s) }
	key := func(k string) { keytest.Press(m, k) }
	typeText("a")
	typeText("ctrl+s")
	key("enter")
	typeText("save")
	key("enter")
	if got := m.bindings(); !reflect.DeepEqual(got, []sandboxBinding{{Keys: "ctrl+s", Action: "save"}}) {
		t.Fatalf("created bindings: %+v", got)
	}
	key("enter")
	key("ctrl+s")
	if !m.sandbox || m.results[0] != "ACTION: save" {
		t.Fatalf("sandbox did not execute save: %+v", m.results[0])
	}
	key("ctrl+g")
	if m.sandbox {
		t.Fatal("ctrl+g did not return to setup")
	}
	typeText("e")
	key("tab")
	key("ctrl+u")
	typeText("updated")
	key("enter")
	path := filepath.Join(t.TempDir(), "bindings.json")
	m.path = path
	typeText("o")
	key("enter")
	if m.form != "" {
		t.Fatalf("export failed: %s", m.notice)
	}
	typeText("d")
	if len(m.bindings()) != 0 {
		t.Fatal("delete did not remove binding")
	}
	typeText("i")
	key("enter")
	if got := m.bindings(); !reflect.DeepEqual(got, []sandboxBinding{{Keys: "ctrl+s", Action: "updated"}}) {
		t.Fatalf("imported: %+v", got)
	}
	typeText("o")
	key("enter")
	if m.form != "overwrite" {
		t.Fatalf("expected overwrite confirmation, got %q", m.form)
	}
	typeText("n")
	key("enter")
	key("ctrl+s")
	if m.results[1] != "ACTION: updated" {
		t.Fatalf("imported action: %q", m.results[1])
	}
}

func TestSetupTimeoutEditPreservedAndInvalidatesStaleTimer(t *testing.T) {
	m := &keybindingModel{width: 100, height: 24, runtime: ochazuke.NewRuntime(), sandbox: true}
	for _, spec := range []string{"g=single", "g g=double"} {
		if err := m.addBinding(spec); err != nil {
			t.Fatal(err)
		}
	}
	m.runtime.BindingTimeout = 5 * time.Millisecond
	cmd := keytest.Type(m, "g")
	stale := cmd().(ochazuke.BindingTimeoutMsg)
	keytest.Press(m, "ctrl+g")
	typeKeys := func(s string) { keytest.Type(m, s) }
	typeKeys("t")
	if m.form != "timeout" || !strings.Contains(m.View().Content, "400ms, 1s") {
		t.Fatalf("timeout editor not shown: form=%q", m.form)
	}
	keytest.Press(m, "ctrl+u")
	typeKeys("0s")
	keytest.Press(m, "enter")
	if m.form != "timeout" || m.runtime.BindingTimeout != 5*time.Millisecond {
		t.Fatalf("invalid timeout accepted: %q %v", m.notice, m.runtime.BindingTimeout)
	}
	keytest.Press(m, "ctrl+u")
	typeKeys("1s")
	keytest.Press(m, "enter")
	if m.form != "" || m.runtime.BindingTimeout != time.Second || !strings.Contains(m.View().Content, "Timeout: 1s") {
		t.Fatalf("timeout not applied: %q %v", m.notice, m.runtime.BindingTimeout)
	}
	// Edit rebuilds the runtime but keeps the timeout and drops stale timers.
	typeKeys("d")
	if m.runtime.BindingTimeout != time.Second {
		t.Fatal("timeout lost when the runtime was rebuilt")
	}
	m.sandbox = true
	count, results := m.count, m.results
	m.Update(stale)
	if m.count != count || m.results != results {
		t.Fatal("stale timer changed state after setup change")
	}
}

func TestBindingConfigValidationIsTransactional(t *testing.T) {
	m := &keybindingModel{runtime: ochazuke.NewRuntime()}
	if err := m.addBinding("x=original"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "bindings.json")
	for _, data := range []string{
		`[{"keys":"z","action":"new"},{"keys":"ctrl+g","action":"bad"}]`,
		`[{"keys":"x","action":"new","typo":true}]`,
		`[] []`, `null`, `{"keys":"x"}`,
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := m.importBindings(path); err == nil {
			t.Fatalf("accepted invalid config: %s", data)
		}
		if got := m.bindings(); !reflect.DeepEqual(got, []sandboxBinding{{Keys: "x", Action: "original"}}) {
			t.Fatalf("failed import mutated bindings: %+v", got)
		}
	}
	if err := m.exportBindings(path, false); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
	if err := m.exportBindings(path, true); err != nil {
		t.Fatal(err)
	}
	if err := m.importBindings(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`[]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.importBindings(path); err != nil {
		t.Fatal(err)
	}
	if len(m.bindings()) != 0 {
		t.Fatal("empty config should clear bindings")
	}
}

func TestBindingFileBrowserSelectsDirectoryAndFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0700); err != nil {
		t.Fatal(err)
	}
	m := &keybindingModel{width: 100, height: 24, runtime: ochazuke.NewRuntime()}
	if err := m.addBinding("x=selected"); err != nil {
		t.Fatal(err)
	}
	m.openForm("export", filepath.Join(dir, "bindings.json"))
	browse := func() {
		t.Helper()
		cmd := keytest.Press(m, "ctrl+o")
		if cmd == nil {
			t.Fatal("browser did not load directory")
		}
		m.Update(cmd())
		keytest.Press(m, "ctrl+s")
	}
	browse()
	want := filepath.Join(out, "bindings.json")
	if m.form != "export" || m.fields[0].Value() != want {
		t.Fatalf("directory selection: form=%q path=%q", m.form, m.fields[0].Value())
	}
	keytest.Press(m, "enter")
	m.openForm("import", want)
	browse()
	if m.form != "import" || m.fields[0].Value() != want {
		t.Fatalf("file selection: form=%q path=%q", m.form, m.fields[0].Value())
	}
	keytest.Press(m, "enter")
	keytest.Press(m, "enter")
	keytest.Type(m, "x")
	if m.results[0] != "ACTION: selected" {
		t.Fatalf("selected file's action: %q", m.results[0])
	}
}
