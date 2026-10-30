package ochazuke

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Under a modal the page's highlights are kept, dimmed, by default
// (modal-highlights keep: a grid's cursor and a list's selection
// stay visible). plain draws the backdrop without reverse video or
// underline, so a wide modal leaves no sliver of a list's cursor bar
// beside it (found by o-json's edit modal). Either way the frame keeps its
// size and text.
func TestModalBackdropHighlights(t *testing.T) {
	frame := func(setting string) string {
		t.Helper()
		r := NewRuntime()
		r.Config().Disable()
		m := NewModel(r)
		m.Appearance.Theme = "mono"
		m.Width, m.Height = 60, 12
		r.Define("row").Verb("drop", func(Ctx) Result { return Done(nil) }).Confirm("Drop {{label}}?")
		if _, err := r.AddPage("p", "Page", func(r *Runtime, w, h int) Block { return Text(r.List("l").View(w, h)) }); err != nil {
			t.Fatal(err)
		}
		if _, err := r.AddList("l", []ListItem{{ID: "a", Label: strings.Repeat("alpha ", 8), Noun: "row"}, {ID: "b", Label: "beta", Noun: "row"}}, Parent("p")); err != nil {
			t.Fatal(err)
		}
		if err := r.OpenPage("p"); err != nil {
			t.Fatal(err)
		}
		if err := r.SetFocus("a"); err != nil {
			t.Fatal(err)
		}
		if setting != "" {
			if err := r.Settings().Set(ModalBackdropHighlightsSetting, setting); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := r.Execute(Request{Noun: "row", Verb: "drop"}); err != nil || r.TopModal() == "" {
			t.Fatalf("no confirm: %v", err)
		}
		return m.View().Content
	}
	keep, plain := frame(""), frame("plain")
	if !strings.Contains(keep, "\x1b[7") && !strings.Contains(keep, ";7m") && !strings.Contains(keep, ";7;") {
		t.Fatalf("keep (the default) lost the highlight beneath the modal:\n%q", keep)
	}
	for _, sgr := range []string{"\x1b[7m", ";7m", ";7;", "[7;"} {
		if strings.Contains(plain, sgr) {
			t.Fatalf("plain left reverse video (%q) beneath the modal:\n%q", sgr, plain)
		}
	}
	if ansi.Strip(keep) != ansi.Strip(plain) {
		t.Fatalf("plain changed the frame's text:\n%s\n---\n%s", ansi.Strip(keep), ansi.Strip(plain))
	}
}
