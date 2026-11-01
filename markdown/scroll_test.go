package markdown

import (
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// ctrl+e and ctrl+y scroll a windowed document a line (the framework's
// view.scroll, Vim's CTRL-E): the cursor stays while it shows inside the
// scrolloff margin, else it moves to the nearest row inside.
func TestCtrlEScrollsTheDocumentWindow(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Long\n\n")
	for i := range 30 {
		b.WriteString("- [ ] item " + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "\n")
	}
	r, d := newDoc(t, Source{Load: func() (string, error) { return b.String(), nil }})
	mono := ochazuke.Appearance{Theme: "mono"}
	_ = d.View(40, 10, mono)
	if !d.CanScrollLines() {
		t.Fatal("a 32-line document in 9 lines does not scroll")
	}
	start := d.cursorLine()
	if err := press(t, r, "ctrl+e"); err != nil {
		t.Fatal(err)
	}
	_ = d.View(40, 10, mono)
	if d.offset != 1 || d.cursorLine() < 1+2 {
		t.Fatalf("ctrl+e: offset %d cursor line %d (from %d); want the window down a line and the cursor in its margin", d.offset, d.cursorLine(), start)
	}
	if err := press(t, r, "5", "ctrl+e"); err != nil {
		t.Fatal(err)
	}
	_ = d.View(40, 10, mono)
	at := d.cursorLine()
	if d.offset != 6 || at < 6+2 || at > 6+8-2 {
		t.Fatalf("5 ctrl+e: offset %d cursor line %d", d.offset, at)
	}
	if err := press(t, r, "ctrl+y"); err != nil {
		t.Fatal(err)
	}
	_ = d.View(40, 10, mono)
	if d.offset != 5 || d.cursorLine() != at {
		t.Fatalf("ctrl+y: offset %d cursor line %d, want 5 and %d (the cursor stays)", d.offset, d.cursorLine(), at)
	}
}

// The Doc answers the shared motions through its line list (the
// framework's CursorMotions): g g and G go to the first and last line,
// ctrl+d and ctrl+u half a window, and view.edge and view.page reach it
// from the palette or the CLI as well.
func TestSharedMotionsMoveTheDocument(t *testing.T) {
	var b strings.Builder
	b.WriteString("# Long\n\n")
	for i := range 30 {
		b.WriteString("- [ ] item " + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "\n")
	}
	r, d := newDoc(t, Source{Load: func() (string, error) { return b.String(), nil }})
	mono := ochazuke.Appearance{Theme: "mono"}
	_ = d.View(40, 10, mono)
	last := d.cursorLine()
	if err := press(t, r, "G"); err != nil {
		t.Fatal(err)
	}
	_ = d.View(40, 10, mono)
	if last = d.cursorLine(); last < 31 {
		t.Fatalf("G: cursor line %d, want the last item", last)
	}
	if err := press(t, r, "g", "g"); err != nil {
		t.Fatal(err)
	}
	_ = d.View(40, 10, mono)
	if d.cursorLine() != 0 || d.offset != 0 {
		t.Fatalf("g g: cursor line %d offset %d", d.cursorLine(), d.offset)
	}
	if err := press(t, r, "ctrl+d"); err != nil {
		t.Fatal(err)
	}
	_ = d.View(40, 10, mono)
	if at := d.cursorLine(); at < 3 || at > 6 {
		t.Fatalf("ctrl+d: cursor line %d, want about half the window down", at)
	}
	for _, s := range []struct {
		verb string
		args ochazuke.Arguments
		want func() bool
	}{
		{"edge", ochazuke.Arguments{"edge": "last"}, func() bool { return d.cursorLine() == last }},
		{"edge", ochazuke.Arguments{"edge": "first"}, func() bool { return d.cursorLine() == 0 }},
		{"page", ochazuke.Arguments{"delta": 1}, func() bool { return d.cursorLine() > 0 }},
	} {
		if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.view", Verb: s.verb, Args: s.args}); err != nil {
			t.Fatalf("view.%s %v: %v", s.verb, s.args, err)
		}
		_ = d.View(40, 10, mono)
		if !s.want() {
			t.Fatalf("view.%s %v: cursor line %d", s.verb, s.args, d.cursorLine())
		}
	}
}
