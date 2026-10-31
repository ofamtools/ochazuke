package ochazuke

import (
	"testing"
)

// fakeTextViewer registers a viewer for format for the test and records
// what it was asked to show.
func fakeTextViewer(t *testing.T, format string) *[]TextDoc {
	t.Helper()
	var got []TextDoc
	restore := swapTextViewer(t, format, func(_ *Runtime, doc TextDoc) error {
		got = append(got, doc)
		return nil
	})
	t.Cleanup(restore)
	return &got
}

// swapTextViewer registers view for format (nil removes it) and returns
// what puts the process's viewer back (package markdown's, linked into
// this test binary by the external tests).
func swapTextViewer(t *testing.T, format string, view TextViewer) func() {
	t.Helper()
	textViewersMu.RLock()
	was := textViewers[format]
	textViewersMu.RUnlock()
	RegisterTextViewer(format, view)
	return func() { RegisterTextViewer(format, was) }
}

// MarkdownResult and PreformattedResult set the verb's Result to a text
// format; IsTextResult knows the three and nothing else.
func TestMarkdownResultDeclaresAFormat(t *testing.T) {
	r := NewRuntime()
	r.Define("record").
		Verb("guide", func(Ctx) Result { return Done("# Guide") }, "a guide").MarkdownResult().
		Verb("usage", func(Ctx) Result { return Done("Usage: x") }, "a --help page").PreformattedResult()
	if op, _ := r.Operation("record.guide"); op.Result != ResultMarkdown || textFormatOf(op) != TextFormatMarkdown {
		t.Fatalf("guide result %q", op.Result)
	}
	if op, _ := r.Operation("record.usage"); op.Result != ResultPreformatted || textFormatOf(op) != TextFormatPreformatted {
		t.Fatalf("usage result %q", op.Result)
	}
	for result, want := range map[string]bool{"text": true, "markdown": true, "preformatted": true, "": false, "appearance": false, "docs page": false} {
		if IsTextResult(result) != want {
			t.Fatalf("IsTextResult(%q) = %v", result, !want)
		}
	}
	if textFormatOf(Operation{Result: "rows"}) != TextFormatPlain {
		t.Fatal("a non-text result is not plain")
	}
}

// From the palette, a Markdown answer goes to the format's viewer with the
// verb's key as the title and the raw text; with help.markdown off, or no
// viewer registered, it opens in the text view as before.
func TestPaletteMarkdownResultOpensItsViewer(t *testing.T) {
	r, _ := textResultFixture(t)
	r.Define("record").
		Verb("guide", func(Ctx) Result { return Done("# Guide\n\n- one") }, "a guide").MarkdownResult()

	// No viewer registered: the text view, the Markdown as written.
	restore := swapTextViewer(t, TextFormatMarkdown, nil)
	runPaletteResults(t, r, "guide record")
	if in := r.Input(textViewInputID); r.TopModal() != TextViewID || in == nil || string(in.Text) != "# Guide\n\n- one" {
		t.Fatalf("no viewer: modal %q", r.TopModal())
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	restore()

	got := fakeTextViewer(t, TextFormatMarkdown)
	runPaletteResults(t, r, "guide record")
	if len(*got) != 1 || (*got)[0] != (TextDoc{Format: TextFormatMarkdown, Title: "record.guide", Text: "# Guide\n\n- one"}) {
		t.Fatalf("viewer got %+v", *got)
	}
	if r.TopModal() != "" {
		t.Fatalf("the text view opened too: %q", r.TopModal())
	}

	// A plain text result never goes to a viewer.
	runPaletteResults(t, r, "program record")
	if r.TopModal() != TextViewID || len(*got) != 1 {
		t.Fatalf("plain text: modal %q viewer %d", r.TopModal(), len(*got))
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}

	// help.markdown off: the text view again.
	if err := r.Settings().Set(HelpMarkdownSetting, false); err != nil {
		t.Fatal(err)
	}
	if r.TextViewer(TextFormatMarkdown) != nil {
		t.Fatal("help.markdown off still offers the viewer")
	}
	runPaletteResults(t, r, "guide record")
	if r.TopModal() != TextViewID || len(*got) != 1 {
		t.Fatalf("off: modal %q viewer %d", r.TopModal(), len(*got))
	}
}

// docs open hands its page to the Markdown viewer, the build as the title
// and the topic as the label; with help.markdown off it opens the plain
// docs viewer as before.
func TestDocsOpenUsesTheMarkdownViewer(t *testing.T) {
	r := NewRuntime()
	NewModel(r)
	got := fakeTextViewer(t, TextFormatMarkdown)
	if _, err := r.Execute(Request{Noun: "ochazuke.docs", Verb: "open", Args: Arguments{"topic": "settings"}}); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 || (*got)[0].Label != "settings" || (*got)[0].Format != TextFormatMarkdown || (*got)[0].Title == "" || (*got)[0].Text == "" {
		t.Fatalf("viewer got %+v", *got)
	}
	if r.TopModal() == docsViewerID {
		t.Fatal("the plain docs viewer opened too")
	}
	if err := r.Settings().Set(HelpMarkdownSetting, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Noun: "ochazuke.docs", Verb: "open", Args: Arguments{"topic": "settings"}}); err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != docsViewerID || len(*got) != 1 {
		t.Fatalf("off: modal %q viewer %d", r.TopModal(), len(*got))
	}
}
