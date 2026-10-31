package demo

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ofam.dev/ochazuke/cli"
	"ofam.dev/oden"
)

// TestCatalogInspect writes the demo's catalog inspector page, with a few
// annotations so the Gaps view draws too. OCHAZUKE_INSPECT_OUT names the
// page to keep (for cli/inspect/smoke.mjs); otherwise it is a temp file.
func TestCatalogInspect(t *testing.T) {
	out := os.Getenv("OCHAZUKE_INSPECT_OUT")
	if out == "" {
		out = filepath.Join(t.TempDir(), "demo.html")
	}
	notes := filepath.Join(t.TempDir(), "notes.json")
	if err := os.WriteFile(notes, []byte(`{"source": "examples/demo", "title": "demo notes", "date": "2026-10-27", "nouns": [
	  {"tool": "demo", "noun": "task", "existing": [{"verb": "edit"}], "suggestions": [
	    {"verb": "edit", "group": "Acts on the thing", "note": "already there"},
	    {"verb": "rename", "params": ["name"], "group": "Acts on the thing", "note": "tasks have titles"},
	    {"verb": "star", "maybe": true, "note": "a favourite marker"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	var stdout, stderr bytes.Buffer
	app := oden.New("demo", []string{"catalog", "inspect", "--out", out, "--annotations", notes},
		oden.WithStdio(strings.NewReader(""), &stdout, &stderr), oden.WithPath(t.TempDir()))
	cli.Mount(app, cli.New(m.Runtime, "demo", strings.NewReader(""), &stdout, &stderr))
	if err := app.Run(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"id":"demo:task"`) || !strings.Contains(string(b), `"host":"demo"`) {
		t.Fatalf("%s lacks the demo's nouns", out)
	}
	t.Logf("wrote %s (%d KB); %s", out, len(b)/1024, strings.TrimSpace(stderr.String()))
}
