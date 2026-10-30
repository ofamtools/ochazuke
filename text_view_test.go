package ochazuke

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func textResultFixture(t *testing.T) (*Runtime, *Model) {
	t.Helper()
	r := NewRuntime()
	r.Define("record").Focusable()
	r.Instance("origin", Kind("record"))
	if err := r.SetFocus("origin"); err != nil {
		t.Fatal(err)
	}
	r.Define("record").
		Verb("program", func(Ctx) Result { return Done(".a | .b") }, "a jq program").TextResult().
		Verb("mapping", func(Ctx) Result {
			return Done(map[string]any{"a": []any{1, 2}, "b": "x"})
		}, "a mapping").TextResult().
		Verb("later", func(Ctx) Result {
			return Pending(func(context.Context) (any, error) { return "one\ntwo\nthree", nil })
		}, "a text answered off the loop").TextResult().
		Verb("path", func(Ctx) Result { return Done("/tmp/x") }, "an undeclared string")
	return r, NewModel(r)
}

// A verb declared TextResult opens its answer from the palette in the text
// view: a one-line read-only input for one line, a multiline one for
// JSON (indented); v, y yank it; esc or q closes back to the origin.
func TestPaletteTextResultOpensReadOnlyEditor(t *testing.T) {
	r, m := textResultFixture(t)
	if op, _ := r.Operation("record.program"); op.Result != ResultText {
		t.Fatalf("TextResult left Result %q", op.Result)
	}
	runPaletteResults(t, r, "program record")
	in := r.Input(textViewInputID)
	if r.TopModal() != TextViewID || in == nil || r.Focus != in.ID {
		t.Fatalf("modal %q focus %q", r.TopModal(), r.Focus)
	}
	if !in.ReadOnly || in.Multiline || string(in.Text) != ".a | .b" || r.Mode.Name != "normal" {
		t.Fatalf("input %+v mode %q", in, r.Mode.Name)
	}
	frame := ansi.Strip(m.View().Content)
	for _, want := range []string{"Text", "record.program", ".a | .b", "q modal.pop"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("frame lacks %q:\n%s", want, frame)
		}
	}
	editorTestKeys(t, r, "w", "w", "v", "$", "y")
	if got := r.Registers().Named["\""].Yank.Text; got != "| .b" {
		t.Fatalf("yank %q", got)
	}
	editorTestKeys(t, r, "i", "x", "d", "d")
	if string(in.Text) != ".a | .b" {
		t.Fatalf("text edited to %q", string(in.Text))
	}
	editorTestKeys(t, r, "q")
	if r.TopModal() != "" || r.Focus != "origin" {
		t.Fatalf("q left modal %q focus %q", r.TopModal(), r.Focus)
	}

	runPaletteResults(t, r, "mapping record")
	in = r.Input(textViewInputID)
	want := "{\n  \"a\": [\n    1,\n    2\n  ],\n  \"b\": \"x\"\n}"
	if in == nil || !in.Multiline || string(in.Text) != want {
		t.Fatalf("mapping text %q", string(in.Text))
	}
	editorTestKeys(t, r, "j", "V", "j", "y")
	if got := r.Registers().Named["\""].Yank; got.Text != "  \"a\": [\n    1,\n" || got.Shape != "line" {
		t.Fatalf("V j y yanked %+v", got)
	}
	editorTestKeys(t, r, "esc")
	if r.TopModal() != "" || r.Focus != "origin" {
		t.Fatalf("esc left modal %q focus %q", r.TopModal(), r.Focus)
	}
}

// A pending text result opens the view when it lands.
func TestPaletteTextResultPending(t *testing.T) {
	r, _ := textResultFixture(t)
	runPaletteResults(t, r, "later record")
	if r.TopModal() != "" {
		t.Fatalf("opened before the answer: %q", r.TopModal())
	}
	for _, msg := range runCmd(r.PendingCommands()) {
		if _, err := r.Deliver(msg); err != nil {
			t.Fatal(err)
		}
	}
	if in := r.Input(textViewInputID); r.TopModal() != TextViewID || in == nil || string(in.Text) != "one\ntwo\nthree" || !in.Multiline {
		t.Fatalf("modal %q input %+v", r.TopModal(), in)
	}
}

// palette.text-results: plain keeps the old behaviour (a string answer
// shows nothing); all opens an undeclared string answer too.
func TestPaletteTextResultsSetting(t *testing.T) {
	r, _ := textResultFixture(t)
	runPaletteResults(t, r, "path record")
	if r.TopModal() != "" {
		t.Fatalf("an undeclared string opened %q under editor", r.TopModal())
	}
	if err := r.Settings().Set(PaletteTextResultsSetting, "plain"); err != nil {
		t.Fatal(err)
	}
	runPaletteResults(t, r, "program record")
	if r.TopModal() != "" {
		t.Fatalf("plain opened %q", r.TopModal())
	}
	if err := r.Settings().Set(PaletteTextResultsSetting, "all"); err != nil {
		t.Fatal(err)
	}
	runPaletteResults(t, r, "path record")
	if in := r.Input(textViewInputID); r.TopModal() != TextViewID || in == nil || string(in.Text) != "/tmp/x" {
		t.Fatalf("all: modal %q", r.TopModal())
	}
}

// The CLI and the API get the value itself, never the view.
func TestTextResultKeepsTheValue(t *testing.T) {
	r, _ := textResultFixture(t)
	value, err := r.Execute(Request{Noun: "record", Verb: "program"})
	if err != nil || value != ".a | .b" || r.TopModal() != "" {
		t.Fatalf("value %#v err %v modal %q", value, err, r.TopModal())
	}
}

// A text longer than the modal scrolls inside a window of fixed height:
// the frame's line count stays while the cursor moves through it (R4.9),
// and the header says which lines show.
func TestTextViewScrollsAtAFixedSize(t *testing.T) {
	r, m := textResultFixture(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = "line " + strings.Repeat("x", i%7)
	}
	if _, err := r.OpenTextView("long", strings.Join(lines, "\n")); err != nil {
		t.Fatal(err)
	}
	height := func() int { return strings.Count(ansi.Strip(m.View().Content), "\n") }
	width := func() int {
		for _, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			if strings.Contains(line, "╭") && strings.Contains(line, "╮") {
				return strings.Index(line, "╮") - strings.Index(line, "╭")
			}
		}
		t.Fatal("no modal border")
		return 0
	}
	firstWidth := width()
	first := height()
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "long  1–") {
		t.Fatalf("no window marker:\n%s", frame)
	}
	for range 30 {
		editorTestKeys(t, r, "j")
		if got := height(); got != first {
			t.Fatalf("frame height %d, was %d", got, first)
		}
		if got := width(); got != firstWidth {
			t.Fatalf("modal width %d, was %d", got, firstWidth)
		}
	}
	editorTestKeys(t, r, "G")
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "/ 60") || !strings.Contains(frame, "– 60 / 60") && !strings.Contains(frame, "–60 / 60") {
		t.Fatalf("G did not reach the end:\n%s", frame)
	}
	editorTestKeys(t, r, "k")
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "–60 / 60") {
		t.Fatalf("k at the bottom scrolled the window:\n%s", frame)
	}
}
