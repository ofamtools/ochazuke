package ochazuke

import (
	"testing"

	"ochazuke/internal/keytest"
)

func TestDemoPageKeysDoNotStealInsertEditing(t *testing.T) {
	m := NewDemo()
	keytest.Press(m, "ctrl+j")
	if m.Runtime.ActivePage != "playground" {
		t.Fatalf("Ctrl-J page = %q", m.Runtime.ActivePage)
	}
	keytest.Press(m, "alt+1")
	if m.Runtime.ActivePage != "playground" {
		t.Fatal("Alt-1 still switches pages")
	}
	keytest.Type(m, "iabc")
	keytest.Press(m, "ctrl+k")
	if got := string(m.Runtime.Inputs["directory"].Text); got != "abc" {
		t.Fatalf("insert Ctrl-K text = %q, want abc", got)
	}
	if m.Runtime.ActivePage != "playground" {
		t.Fatal("insert Ctrl-K switched page")
	}
	keytest.Press(m, "esc")
	keytest.Press(m, "ctrl+k")
	if m.Runtime.ActivePage != "overview" {
		t.Fatalf("normal Ctrl-K page = %q", m.Runtime.ActivePage)
	}
	keytest.Type(m, ":open")
	if !m.Palette.IsActive() || m.Palette.query != "open" {
		t.Fatalf("colon search: active=%v query=%q", m.Palette.IsActive(), m.Palette.query)
	}
}
