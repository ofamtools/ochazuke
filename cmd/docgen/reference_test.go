package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/refdoc"
)

// committedReference is docs/reference from this package's directory.
const committedReference = "../../docs/reference"

// TestReference holds the committed reference to the framework runtime's
// catalog: it renders the pages in memory and compares them with
// docs/reference below their headers (the header only on its catalog
// fingerprint, so the commit and date it names may be older).
func TestReference(t *testing.T) {
	start := time.Now()
	m := referenceRuntime(t.TempDir()).ReferenceModel()
	diffs, err := refdoc.Check(m, referenceOptions(refdoc.Stamp{Tool: referenceTool}), committedReference)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("checked %d settings and %d tables in %v", len(m.Settings), len(m.Bindings), time.Since(start))
	if len(diffs) > 0 {
		var b strings.Builder
		for _, d := range diffs {
			b.WriteString(d.String() + "\n")
		}
		t.Fatalf("docs/reference no longer matches the catalog (%s):\n%s\nregenerate: %s (go run ./cmd/docgen -ref docs/reference), then commit docs/reference", m.Fingerprint(), b.String(), regenerateCommand)
	}
}

// The docs viewer on the framework runtime with docs.show-config off
// shows each committed page's body, byte for byte, whatever the user's
// files hold.
func TestDocsViewerMatchesTheCommittedReference(t *testing.T) {
	dir := t.TempDir()
	r := referenceRuntime(dir)
	config := r.Config().Directory()
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, ochazuke.BindingsFile), []byte(`[{"noun": "app", "mode": "normal", "keys": "?", "request": null}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	r.EnsureUserBindings()
	if err := r.Settings().Set("focus-display", "stack"); err != nil {
		t.Fatal(err)
	}
	if err := r.Settings().Set(ochazuke.DocsShowConfigSetting, false); err != nil {
		t.Fatal(err)
	}
	for _, topic := range refdoc.Topics {
		header, text, err := r.DocsPage(topic.Topic)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(header, " · catalog sha256:") {
			t.Errorf("%s header %q", topic.Topic, header)
		}
		data, err := os.ReadFile(filepath.Join(committedReference, topic.File))
		if err != nil {
			t.Fatal(err)
		}
		if want := refdoc.Body(string(data)); text != want {
			t.Errorf("%s with docs.show-config off differs from %s (regenerate: %s)", topic.Topic, topic.File, regenerateCommand)
		}
	}
	if err := r.Settings().Set(ochazuke.DocsShowConfigSetting, true); err != nil {
		t.Fatal(err)
	}
	_, text, _ := r.DocsPage("settings")
	if !strings.Contains(text, "> yours: `stack` (settings.json)") {
		t.Error("show-config on lacks the yours line for focus-display")
	}
	_, text, _ = r.DocsPage("bindings")
	if !strings.Contains(text, "> yours: `?` unbound (bindings.json)") {
		t.Error("show-config on lacks the yours line for the unbound ?")
	}
}
