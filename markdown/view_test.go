package markdown

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

func viewerFixture(t *testing.T) (*ochazuke.Runtime, *ochazuke.Model) {
	t.Helper()
	r := ochazuke.NewRuntime()
	m := ochazuke.NewModel(r)
	r.Define("record").Focusable()
	r.Instance("origin", ochazuke.Kind("record"))
	if err := r.SetFocus("origin"); err != nil {
		t.Fatal(err)
	}
	return r, m
}

// Linking the package registers the viewer for Markdown and preformatted
// text; OpenText opens it: a modal over the focus holding a read-only
// Doc with the focus on its first element; edits stay out of its keys,
// y a copies the text as given, q closes it back on the origin.
func TestOpenTextShowsMarkdownInTheViewer(t *testing.T) {
	r, m := viewerFixture(t)
	if r.TextViewer(ochazuke.TextFormatMarkdown) == nil || r.TextViewer(ochazuke.TextFormatPreformatted) == nil {
		t.Fatal("the package did not register its viewer")
	}
	text := "# Guide\n\n- [ ] one\n\n```sh\nrun it\n```\n"
	if err := r.OpenText(ochazuke.TextDoc{Format: ochazuke.TextFormatMarkdown, Title: "record.guide", Text: text}); err != nil {
		t.Fatal(err)
	}
	d := Viewer(r)
	if r.TopModal() != ViewID || d == nil || d.Editable() || r.Focus != d.FocusTarget() {
		t.Fatalf("modal %q doc %v focus %q", r.TopModal(), d, r.Focus)
	}
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"record.guide", "Guide", "run it", "esc modal.close", "y a copy all"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame lacks %q:\n%s", want, frame)
		}
	}
	for _, edit := range []string{"markdown.insert", "markdown.toggle", "markdown.delete", "markdown.undo", "```"} {
		if strings.Contains(frame, edit) {
			t.Fatalf("frame shows %q:\n%s", edit, frame)
		}
	}
	// x on the task is not the toggle here: nothing is written or refused.
	if err := press(t, r, "j", "x"); err != nil {
		t.Fatalf("x on a read-only task: %v", err)
	}
	if !strings.Contains(d.Text(), "- [ ] one") {
		t.Fatalf("the text changed: %q", d.Text())
	}
	if err := press(t, r, "y", "a"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ReadYank(""); got.Text != text {
		t.Fatalf("y a yanked %q", got.Text)
	}
	if err := press(t, r, "q"); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != "" || r.Focus != "origin" || r.Instances[viewDocID] != nil {
		t.Fatalf("q left modal %q focus %q", r.TopModal(), r.Focus)
	}
}

// Opened again while it is the top modal, the viewer shows the new text in
// its place; esc closes it back on the origin.
func TestOpenViewReplacesItself(t *testing.T) {
	r, _ := viewerFixture(t)
	for _, text := range []string{"# One\n", "# Two\n"} {
		if err := OpenView(r, ochazuke.TextDoc{Format: ochazuke.TextFormatMarkdown, Title: "t", Label: "topic", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	if d := Viewer(r); d == nil || d.Text() != "# Two\n" || len(r.Modals()) != 1 {
		t.Fatalf("doc %v modals %v", d, r.Modals())
	}
	if err := press(t, r, "esc"); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != "" || r.Focus != "origin" {
		t.Fatalf("esc left modal %q focus %q", r.TopModal(), r.Focus)
	}
}

// A preformatted text draws as written: every paragraph in a fence of its
// own, Markdown characters kept, a fence longer than any backtick run.
func TestPreformattedKeepsTheText(t *testing.T) {
	help := "Usage: x [flags] <cmd> [NAME]\r\n\n\n  --glob *.md   match *.md or __init__\n  --tick ```   three ticks\n"
	got := Preformatted(help)
	want := "```\nUsage: x [flags] <cmd> [NAME]\n```\n\n````\n  --glob *.md   match *.md or __init__\n  --tick ```   three ticks\n````\n"
	if got != want {
		t.Fatalf("Preformatted =\n%s\nwant\n%s", got, want)
	}
	if Preformatted("\n \n") != "" {
		t.Fatal("blank text is not empty")
	}
	drawn := ansi.Strip(Block(got, 70, ochazuke.Appearance{Theme: "mono"}).Text)
	for _, line := range []string{"Usage: x [flags] <cmd> [NAME]", "  --glob *.md   match *.md or __init__", "  --tick ```   three ticks"} {
		if !strings.Contains(drawn, line) {
			t.Fatalf("drawn lacks %q:\n%s", line, drawn)
		}
	}
	r, m := viewerFixture(t)
	if err := r.OpenText(ochazuke.TextDoc{Format: ochazuke.TextFormatPreformatted, Title: "x help", Text: help}); err != nil {
		t.Fatal(err)
	}
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "  --glob *.md   match *.md or __init__") {
		t.Fatalf("viewer lacks the help line:\n%s", frame)
	}
	if err := press(t, r, "y", "a"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ReadYank(""); got.Text != help {
		t.Fatalf("y a yanked %q, want the help as given", got.Text)
	}
}

// help.markdown off: OpenText shows the text plain in the text view.
func TestOpenTextOffIsPlain(t *testing.T) {
	r, _ := viewerFixture(t)
	if err := r.Settings().Set(ochazuke.HelpMarkdownSetting, false); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenText(ochazuke.TextDoc{Format: ochazuke.TextFormatMarkdown, Title: "t", Text: "# One"}); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != ochazuke.TextViewID {
		t.Fatalf("modal %q", r.TopModal())
	}
}

// An editable Doc keeps its edit verbs; a read-only one is refused them by
// the markdown-writable guard (their rows leave the hints).
func TestWritableGuard(t *testing.T) {
	text := "- [ ] a\n"
	r, _ := newDoc(t, Source{Load: func() (string, error) { return text, nil }, Save: func(s string) error { text = s; return nil }})
	q := ochazuke.Request{Noun: DocNoun, Verb: "toggle"}
	if !r.Available(q, r.Focus, r.Mode) {
		t.Fatal("toggle unavailable in an editable doc")
	}
	ro, _ := newDoc(t, Source{Load: func() (string, error) { return "- [ ] a\n", nil }})
	if ro.Available(q, ro.Focus, ro.Mode) {
		t.Fatal("toggle available in a read-only doc")
	}
	if !ro.Available(ochazuke.Request{Noun: DocNoun, Verb: "yank"}, ro.Focus, ro.Mode) {
		t.Fatal("yank unavailable in a read-only doc")
	}
}
