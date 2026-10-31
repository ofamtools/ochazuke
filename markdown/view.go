package markdown

import (
	"cmp"
	"errors"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
)

// The Markdown viewer: help and docs text in a modal (a verb's Markdown
// or preformatted answer from the palette, the docs viewer), drawn by a
// read-only Doc. Linking this package registers it for both formats in
// every runtime (ochazuke.RegisterTextViewer); the setting help.markdown
// turns it off, and the text shows plain in the text view as before.
const (
	// ViewID is the viewer's modal: the instance id and, canonical as
	// ochazuke.markdown-view, its noun.
	ViewID = "markdown-view"
	// ViewNoun is the viewer modal's noun (q closes it, y a copies the
	// whole text).
	ViewNoun = "ochazuke.markdown-view"
	// viewDocID is the read-only Doc the viewer holds.
	viewDocID = "__markdown-view/doc"
)

func init() {
	ochazuke.RegisterTextViewer(ochazuke.TextFormatMarkdown, OpenView)
	ochazuke.RegisterTextViewer(ochazuke.TextFormatPreformatted, OpenView)
}

// viewer is the viewer modal's component: what it shows and the Doc that
// draws it. It draws the modal (ochazuke.AppearanceModalBody).
type viewer struct {
	doc   *Doc
	title string
	label string
	text  string // the text as given (Preformatted's input, not its fences)
	// fitFor is the modal width fitWidth was measured at (fit).
	fitFor, fitWidth int
}

// OpenView shows doc in the Markdown viewer: a modal over the focus
// holding a read-only Doc (New with a Source that has no Save), the
// focus on its first element. A preformatted text is drawn through
// Preformatted, never read as Markdown. The Doc's keys work there
// (j/k by element and link, }/{, ] h, g O, enter on a link, y y, y l);
// q or esc closes it back to the focus it opened over and y a copies the
// whole text as given. Opened while the viewer is the top modal, it shows
// the new text in its place (ReplaceOpen).
func OpenView(r *ochazuke.Runtime, doc ochazuke.TextDoc) error {
	Register(r)
	text := doc.Text
	if doc.Format == ochazuke.TextFormatPreformatted {
		text = Preformatted(text)
	}
	inst, err := r.OpenModal(ViewID, ochazuke.Payload{Prompt: doc.Title}, ochazuke.ReplaceOpen())
	if err != nil {
		return err
	}
	d, err := New(r, viewDocID, ViewID, Source{Load: func() (string, error) { return text, nil }}, WrapAll())
	if err != nil {
		_ = r.Pop()
		return err
	}
	inst.Component = &viewer{doc: d, title: doc.Title, label: doc.Label, text: doc.Text}
	if len(d.Elements()) > 0 {
		return d.Focus()
	}
	return nil
}

// Viewer is the Doc the open Markdown viewer shows, or nil.
func Viewer(r *ochazuke.Runtime) *Doc {
	if v := viewerOf(r); v != nil {
		return v.doc
	}
	return nil
}

func viewerOf(r *ochazuke.Runtime) *viewer {
	if inst := r.Instances[ViewID]; inst != nil {
		if v, ok := inst.Component.(*viewer); ok {
			return v
		}
	}
	return nil
}

// ModalTitle is the title the text was opened with.
func (v *viewer) ModalTitle() string { return v.title }

// ModalBlock draws in the default appearance (a host without one).
func (v *viewer) ModalBlock(s ochazuke.Styles, width, height int) ochazuke.Block {
	return v.ModalBlockIn(ochazuke.Appearance{Theme: "adaptive", Dark: true}, s, width, height)
}

// ModalBlockIn draws the label on a muted line, when there is one, then
// the document windowed to the rest of the height (its last line the
// position marker once it is longer). The document is laid out as narrow
// as its widest drawn line (a short help keeps a small modal; the lines
// break where they broke at the full width), and a line wider than the
// modal (a table with more columns than fit) is cut, never wrapped.
func (v *viewer) ModalBlockIn(a ochazuke.Appearance, s ochazuke.Styles, width, height int) ochazuke.Block {
	if width <= 0 || height <= 0 {
		return ochazuke.Text("")
	}
	var top string
	if v.label != "" {
		top = ansi.Truncate(v.label, width, "…")
		if height == 1 {
			return ochazuke.Text(s.Muted.Render(top))
		}
		height--
	}
	body := v.doc.Block(v.fit(width, height, a), height, a)
	lines := strings.Split(body.Text, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) > width {
			lines[i] = ansi.Truncate(line, width, "…")
		}
	}
	body.Text = strings.Join(lines, "\n")
	if top == "" {
		return body
	}
	out := ochazuke.Block{Text: s.Muted.Render(top) + "\n" + body.Text, Cursor: body.Cursor}
	if out.Cursor != nil {
		out.Cursor.Y++
	}
	return out
}

// fit is the width the document is laid out at in a modal width cells
// wide: the narrowest of a few widths from its widest drawn line at that
// width up that draws the same number of lines (a code block keeps a
// right margin, so its widest line alone would wrap it), else the whole
// width; measured once per width (the text never changes).
func (v *viewer) fit(width, height int, a ochazuke.Appearance) int {
	if v.fitFor == width && v.fitWidth > 0 {
		return v.fitWidth
	}
	widest := func(at int) (int, int) {
		v.doc.Block(at, height, a)
		w := 0
		for _, line := range v.doc.lay.lines {
			w = max(w, ansi.StringWidth(strings.TrimRight(ansi.Strip(line), " ")))
		}
		return w, len(v.doc.lay.lines)
	}
	w, lines := widest(width)
	fit := width
	for at := max(1, w+1); at < width && at <= w+7; at += 2 {
		if _, n := widest(at); n == lines {
			fit = at
			break
		}
	}
	v.fitFor, v.fitWidth = width, fit
	return fit
}

// Preformatted is text laid out for a terminal (a --help page) as
// Markdown that draws it as written: each run of lines between blank
// lines in a code fence of its own (longer than any backtick run in it),
// so spacing, asterisks, brackets and underscores stay as they are and
// j/k step a paragraph at a time. Runs of blank lines draw as one.
func Preformatted(text string) string {
	text = strings.ReplaceAll(strings.TrimRight(text, "\n"), "\r\n", "\n")
	var out, block []string
	flush := func() {
		if len(block) == 0 {
			return
		}
		fence := strings.Repeat("`", max(3, longestRun(strings.Join(block, "\n"), '`')+1))
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, fence)
		out = append(out, block...)
		out = append(out, fence)
		block = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		block = append(block, strings.TrimRight(line, " \t"))
	}
	flush()
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// longestRun is the longest run of c in s.
func longestRun(s string, c byte) int {
	best, run := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			run++
			best = max(best, run)
		} else {
			run = 0
		}
	}
	return best
}

// registerView defines the viewer's noun and its verbs (keys in
// bindings.json, context ochazuke.markdown-view/normal).
func registerView(r *ochazuke.Runtime) {
	r.Define(ViewNoun).
		Verb("copy", func(c ochazuke.Ctx) ochazuke.Result {
			v := viewerOf(c.Runtime)
			if v == nil {
				return ochazuke.Failed(errors.New("no Markdown viewer open"))
			}
			if err := c.Runtime.StoreYank(ochazuke.Yank{Text: v.text, Shape: "line"}, ochazuke.StringArg(c.Args, "register", ""), false); err != nil {
				return ochazuke.Failed(err)
			}
			c.Info("Copied " + cmp.Or(v.label, v.title, "the text"))
			return ochazuke.Done(v.text)
		}, "Copy the viewer's whole text, as it was given (raw Markdown, or the preformatted text)").Fetch()
	if op, ok := r.Operation(ViewNoun + ".copy"); ok {
		op.Parameters = map[string]ochazuke.Parameter{"register": {Type: "string", Description: "The register to yank into (default the unnamed one)"}}
		r.Operations[op.Key()] = op
	}
}
