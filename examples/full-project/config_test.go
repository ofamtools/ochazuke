package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func copyConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source, err := fs.Sub(embeddedConfig, "config")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dir, source); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestExternalConfigurationChangesApplication(t *testing.T) {
	dir := copyConfig(t)
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.App.Title = "External workspace"
	data, err := json.Marshal(cfg.App)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	model, err := Build(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if model.Title != "External workspace" {
		t.Fatalf("title = %q, want external configuration title", model.Title)
	}
}

func TestConfigurationRejectsInvalidFiles(t *testing.T) {
	for _, test := range []struct {
		name, data, message string
	}{
		{"unknown field", `{"titel":"Typo"}`, `unknown field "titel"`},
		{"trailing value", `{} {}`, "exactly one JSON value"},
		{"null", `null`, "not null"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := copyConfig(t)
			if err := os.WriteFile(filepath.Join(dir, "app.json"), []byte(test.data), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(dir)
			if err == nil || !strings.Contains(err.Error(), "app.json") || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want app.json diagnostic containing %q", err, test.message)
			}
		})
	}
	t.Run("missing file does not use embedded fallback", func(t *testing.T) {
		dir := copyConfig(t)
		if err := os.Remove(filepath.Join(dir, "bindings.json")); err != nil {
			t.Fatal(err)
		}
		_, err := LoadConfig(dir)
		if err == nil || !strings.Contains(err.Error(), "bindings.json") {
			t.Fatalf("error = %v, want missing bindings.json diagnostic", err)
		}
	})
}
