package ochazuke

import (
	"slices"
	"sync"
)

// Text formats: how a text shown in a modal is written, and so how it is
// drawn. A verb declares its answer's format through Operation.Result
// (NounBuilder.TextResult, MarkdownResult, PreformattedResult); a caller
// names it in TextDoc.Format. The CLI and the API return the text as it
// is, whatever its format.
const (
	// TextFormatPlain is text to read, select and yank as written: the
	// text view (OpenTextView), a read-only editor.
	TextFormatPlain = ResultText
	// TextFormatMarkdown is Markdown: headings, lists, code fences, links.
	TextFormatMarkdown = ResultMarkdown
	// TextFormatPreformatted is text laid out for a terminal (a --help
	// page): drawn as it is written, never read as Markdown.
	TextFormatPreformatted = ResultPreformatted
)

// ResultMarkdown is the Operation.Result of a verb whose answer is
// Markdown (NounBuilder.MarkdownResult).
const ResultMarkdown = "markdown"

// ResultPreformatted is the Operation.Result of a verb whose answer is
// preformatted text, such as a command's --help (NounBuilder.PreformattedResult).
const ResultPreformatted = "preformatted"

// HelpMarkdownSetting turns the formatted text viewers on (default true):
// Markdown and preformatted text in a modal (a palette text result, the
// docs viewer) open in the viewer registered for their format
// (RegisterTextViewer: package markdown's); off, they open as plain text
// as before.
const HelpMarkdownSetting = "help.markdown"

// TextDoc is a text to show in a modal: its format (TextFormat*), the
// modal's title, an optional muted label line above the text (the docs
// viewer's topic) and the text.
type TextDoc struct {
	Format string
	Title  string
	Label  string
	Text   string
}

// TextViewer opens doc in a modal over the focus. Opened while its own
// modal is the top one, it shows doc in that modal's place (ReplaceOpen).
type TextViewer func(r *Runtime, doc TextDoc) error

var (
	textViewersMu sync.RWMutex
	textViewers   = map[string]TextViewer{}
)

// RegisterTextViewer registers the viewer for a text format in every
// runtime of the process (package markdown registers Markdown and
// preformatted text when it is linked in, so only its importers carry
// Glamour). A nil viewer removes the format's.
func RegisterTextViewer(format string, view TextViewer) {
	textViewersMu.Lock()
	defer textViewersMu.Unlock()
	if view == nil {
		delete(textViewers, format)
		return
	}
	textViewers[format] = view
}

// TextViewer is the viewer a text of format opens in: the registered
// viewer while help.markdown is on, nil for plain text, with the setting
// off or with no viewer for the format (the caller shows the text plain).
func (r *Runtime) TextViewer(format string) TextViewer {
	if format == "" || format == TextFormatPlain {
		return nil
	}
	if v, ok := r.Settings().Value(HelpMarkdownSetting); ok {
		if on, _ := v.(bool); !on {
			return nil
		}
	}
	textViewersMu.RLock()
	defer textViewersMu.RUnlock()
	return textViewers[format]
}

// OpenText shows doc in a modal: in its format's viewer (TextViewer),
// else in the text view as plain text.
func (r *Runtime) OpenText(doc TextDoc) error {
	if view := r.TextViewer(doc.Format); view != nil {
		return view(r, doc)
	}
	_, err := r.OpenTextView(doc.Title, doc.Text)
	return err
}

// textResults are the Operation.Result values that are a text format.
var textResults = []string{ResultText, ResultMarkdown, ResultPreformatted}

// IsTextResult reports whether an Operation.Result names a text format:
// the palette shows such an answer in a modal (palette.text-results).
func IsTextResult(result string) bool { return slices.Contains(textResults, result) }

// MarkdownResult marks the last declared verb's answer as Markdown
// (Operation.Result ResultMarkdown): the palette opens it in the Markdown
// viewer (help.markdown), else in the text view, as TextResult does.
func (b *NounBuilder) MarkdownResult() *NounBuilder {
	return b.result("MarkdownResult", ResultMarkdown)
}

// PreformattedResult marks the last declared verb's answer as
// preformatted text, such as a command's --help (Operation.Result
// ResultPreformatted): the palette opens it in the viewer for the format
// (help.markdown), drawn as written, else in the text view.
func (b *NounBuilder) PreformattedResult() *NounBuilder {
	return b.result("PreformattedResult", ResultPreformatted)
}

// result sets the last declared verb's Operation.Result.
func (b *NounBuilder) result(name, result string) *NounBuilder {
	if b.last == "" {
		panic(name + " must follow Verb")
	}
	op := b.r.Operations[b.last]
	op.Result = result
	b.r.Operations[b.last] = op
	return b
}
