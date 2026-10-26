package demo

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// The Library page: J/K scroll the selected entry's detail while the
// entries list keeps the focus, 10J takes a count, j to another entry
// starts its detail at the top, and the footer reads J/K detail.
func TestLibraryDetailScrollsWithTheListFocused(t *testing.T) {
	m := New()
	m.Width, m.Height = 100, 24
	m.Appearance.Theme = "mono"
	if err := m.Runtime.OpenPage("library"); err != nil {
		t.Fatal(err)
	}
	m.View()
	focus := m.Runtime.Focus
	for _, key := range []string{"J", "J", "1", "0", "J"} {
		keytest.Press(m, key)
	}
	frame := ansi.Strip(m.View().Content)
	if m.Runtime.Focus != focus || !strings.Contains(frame, "Detail · 13–") || !strings.Contains(frame, "J/K detail") {
		t.Fatalf("focus %q (want %q), frame:\n%s", m.Runtime.Focus, focus, frame)
	}
	if testing.Verbose() {
		t.Log("\n" + frame)
	}
	keytest.Press(m, "j")
	if frame = ansi.Strip(m.View().Content); !strings.Contains(frame, "Detail · 1–") || !strings.Contains(frame, "LISTS") {
		t.Fatalf("a new entry starts at the top:\n%s", frame)
	}
	if _, err := m.Runtime.Execute(ochazuke.Request{Noun: "detail", Verb: "scroll", Args: ochazuke.Arguments{"lines": -5}}); err != nil {
		t.Fatal(err)
	}
}
