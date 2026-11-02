package markdown

import (
	"testing"

	"ofam.dev/ochazuke"
)

// / in the Markdown viewer finds the next drawn line holding the query
// (search.case smart), n and N go on either way with a count, wrapping;
// n does not reach the note verb (the viewer is read-only).
func TestViewerSlashFinds(t *testing.T) {
	r, m := viewerFixture(t)
	m.Width, m.Height = 80, 30
	text := "# Guide\n\nalpha line\n\n- first item\n- second Needle item\n\nmiddle\n\nlast needle here\n"
	if err := r.OpenText(ochazuke.TextDoc{Format: ochazuke.TextFormatMarkdown, Title: "guide", Text: text}); err != nil {
		t.Fatal(err)
	}
	d := Viewer(r)
	m.View()
	line := func() string {
		e, _ := d.Cursor()
		if e < 0 {
			return ""
		}
		el := d.Elements()[e]
		return d.Text()[el.Start:el.End]
	}
	if err := press(t, r, "/", "n", "e", "e", "d", "l", "e", "enter"); err != nil {
		t.Fatal(err)
	}
	if got := line(); got != "- second Needle item" {
		t.Fatalf("/needle landed on %q", got)
	}
	if err := press(t, r, "n"); err != nil {
		t.Fatal(err)
	}
	if got := line(); got != "last needle here" {
		t.Fatalf("n landed on %q", got)
	}
	if err := press(t, r, "N"); err != nil {
		t.Fatal(err)
	}
	if got := line(); got != "- second Needle item" {
		t.Fatalf("N landed on %q", got)
	}
	if err := press(t, r, "2", "n"); err != nil {
		t.Fatal(err)
	}
	if got := line(); got != "- second Needle item" {
		t.Fatalf("2n (wrapping) landed on %q", got)
	}
	if err := press(t, r, "/", "N", "enter"); err != nil {
		t.Fatal(err)
	}
	if got := line(); got != "- second Needle item" {
		t.Fatalf("/N (smart case) landed on %q", got)
	}
	if r.TopModal() != ViewID || d.Text() != text {
		t.Fatalf("modal %q, text changed %v", r.TopModal(), d.Text() != text)
	}
}
