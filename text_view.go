package ochazuke

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ResultText is the Operation.Result of a verb whose answer is text to
// read and copy (a jq program, a JSON mapping), set by
// NounBuilder.TextResult. The palette and (with keys.text-results) key
// bindings show it in the text view (palette.text-results); the CLI and
// API return the value unchanged.
const ResultText = "text"

// TextViewID is the text view's modal: the instance id and, canonical as
// ochazuke.text-view, its noun.
const TextViewID = "text-view"

// TextViewNoun is the text view modal's noun (q closes it).
const TextViewNoun = "ochazuke.text-view"

// textViewInputID is the read-only input the text view holds.
const textViewInputID = "__text-view/text"

// PaletteTextResultsSetting says how the palette shows a text result:
// editor, all or plain (settings/ochazuke.json).
const PaletteTextResultsSetting = "palette.text-results"

// DataResultsSetting shows the data answers of verbs run from the help and
// the palette that no other presenter shows.
const DataResultsSetting = "results.show-data"

// KeyTextResultsSetting enables declared text results from key bindings.
const KeyTextResultsSetting = "keys.text-results"

// KeyTableResultsSetting enables declared rows answers (ResultRows) from key
// bindings: an application verb whose answer is a list of objects opens it
// in the results table the palette uses.
const KeyTableResultsSetting = "keys.table-results"

// ResultRows is the Operation.Result of a verb whose answer is a list of
// objects: the palette (and, with keys.table-results, a key) shows it as a
// table. A mounted verb declares it in its catalog (result "rows"); it is
// also what a standard list verb answers.
const ResultRows = "rows"

// RowsResult marks the last declared verb's answer as a list of objects
// (Operation.Result ResultRows): the palette shows it as a table, and a key
// does too (keys.table-results).
func (b *NounBuilder) RowsResult() *NounBuilder { return b.result("RowsResult", ResultRows) }

// TextAnswer carries a text answer's display title across CLI/catalog calls.
// Only declared text results interpret this shape as presentation metadata.
type TextAnswer struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

func textAnswer(value any) (TextAnswer, bool) {
	switch v := value.(type) {
	case TextAnswer:
		return v, true
	case map[string]any:
		title, titled := v["title"].(string)
		text, written := v["text"].(string)
		return TextAnswer{Title: title, Text: text}, titled && written
	}
	return TextAnswer{}, false
}

// textView is the text view modal's state: its title and the first line
// its window shows, kept so the window moves only when the cursor would
// leave it.
type textView struct {
	title string
	top   int
}

// TextResult marks the last declared verb's result as text
// (Operation.Result ResultText): UI dispatch opens the answer in the text
// view, a read-only editor. A string is shown as it is; any other value as
// indented JSON.
func (b *NounBuilder) TextResult() *NounBuilder { return b.result("TextResult", ResultText) }

// PresentResult presents a completed answer according to the operation's
// result format and palette.text-results policy. It reports whether the
// answer has a text presenter; structured answers remain the caller's.
// CLI/API execution does not call it.
func (r *Runtime) PresentResult(q Request, value any) (bool, error) {
	op := r.Operations.At(Key(requestOperation(r.CanonicalRequest(q))))
	if !r.showsText(op, value) || value == nil {
		return false, nil
	}
	doc := TextDoc{Format: textFormatOf(op), Title: q.Key(), Text: resultText(value)}
	if answer, ok := textAnswer(value); ok && r.Settings().Bool("text-view.result-title") {
		doc.Title = answer.Title
	}
	return true, r.OpenText(doc)
}

// keyResultPresenter remembers the interaction that asked. A late answer
// never covers a different page, selection, mode or newly opened modal.
func (r *Runtime) keyResultPresenter(q Request) func(any) error {
	op, _ := r.Operation(q.Key())
	text := r.Settings().Bool(KeyTextResultsSetting) && IsTextResult(op.Result) && r.Settings().String(PaletteTextResultsSetting) != "plain"
	table := op.Result == ResultRows && op.Kind == KindApplication && op.Standard == "" && r.Settings().Bool(KeyTableResultsSetting)
	if !text && !table {
		return nil
	}
	return r.resultPresenter(q, func(value any) (bool, error) {
		if text && r.Settings().Bool(KeyTextResultsSetting) {
			return r.PresentResult(q, value)
		}
		if table && r.Settings().Bool(KeyTableResultsSetting) {
			return r.presentRows(q, value)
		}
		return false, nil
	})
}

// presentRows opens a rows answer (ResultRows) in the results table. An
// answer that is not a list of objects, or an empty one, is left to the
// caller (shown is false); a modal over the interaction is never covered.
func (r *Runtime) presentRows(q Request, value any) (bool, error) {
	value = AnswerRows(value)
	if value == nil || r.TopModal() != "" {
		return false, nil
	}
	before := r.Instances[r.TopModal()]
	if err := r.showResults(q.Key(), value); err != nil {
		return false, err
	}
	return r.Instances[r.TopModal()] != before, nil
}

// resultPresenter is shared by keys and the palette. Presentation acts for
// the interaction that asked, not the job's (possibly underlying) owner.
func (r *Runtime) resultPresenter(q Request, show func(any) (bool, error)) func(any) error {
	focus, mode, page := r.Focus, r.BaseMode(), r.Instances[r.Page]
	under := r.Instances[r.TopModal()]
	input := r.UI().passthroughInput
	present := func(value any, late bool) (bool, error) {
		if r.Focus != focus || r.BaseMode() != mode || r.Instances[r.Page] != page || r.Instances[r.TopModal()] != under || r.UI().passthroughInput != input {
			// Only a late answer is reported: a synchronous one that finds the
			// interaction changed was moved by its own verb (page.open), which
			// has said what it has to.
			if late && value != nil {
				r.UI().Info("answer not opened: " + r.DisplayKey(q.Key()) + " (interaction changed)\n\n" + resultText(value))
			}
			return false, nil
		}
		defer r.actingFor(focus)()
		return show(value)
	}
	return func(value any) error {
		if ticket, pending := value.(Ticket); pending {
			if job := r.jobs[ticket.Job]; job != nil {
				then := job.then
				job.then = func(c Ctx, value any, err error) error {
					if then != nil {
						if callbackErr := then(c, value, err); callbackErr != nil {
							return callbackErr
						}
					}
					if err != nil || r.Instances[ticket.Owner] == nil {
						return nil
					}
					shown, presentErr := present(value, true)
					if shown && presentErr == nil && job.presented != nil {
						job.presented(c)
					}
					return presentErr
				}
			}
			return nil
		}
		_, err := present(value, false)
		return err
	}
}

func registerTextView(r *Runtime) {
	r.Define(TextViewNoun)
}

// OpenTextView opens text in the text view: a modal over the focus
// holding a read-only input (Input.ReadOnly), one line when the text is
// one line, else many, with the cursor at the start in normal mode. The
// editor's motions, visual modes and yanks work there; q or esc closes it
// back to the focus it opened over. title heads it (the palette's is the
// verb's name). Opened while the text view is the top modal, it shows the
// new text in its place, from the top, over the focus the first opened
// over (ReplaceOpen, R4.8).
func (r *Runtime) OpenTextView(title, text string) (*Input, error) {
	text = strings.TrimRight(text, "\n")
	in := NewInput(textViewInputID, text, strings.Contains(text, "\n"))
	in.ReadOnly = true
	in.Wrap = r.Settings().Bool("text-view.wrap")
	inst, err := r.OpenModal(TextViewID, Payload{Prompt: title}, WithInput(in), ReplaceOpen())
	if err != nil {
		return nil, err
	}
	inst.Data = &textView{title: title}
	return in, nil
}

// resultText is a verb's answer as the text view shows it: a string as
// it is, JSON indented, nil as nothing.
func resultText(value any) string {
	if answer, ok := textAnswer(value); ok {
		return answer.Text
	}
	var data []byte
	switch value := value.(type) {
	case nil:
		return ""
	case string:
		return value
	case json.RawMessage:
		data = value
	case []byte:
		return string(value)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		data = encoded
	}
	var out bytes.Buffer
	if err := json.Indent(&out, data, "", "  "); err != nil {
		return string(data)
	}
	return out.String()
}

// showsText reports whether the palette shows a verb's answer in a text
// modal: a text result (IsTextResult: text, Markdown, preformatted) unless
// palette.text-results is plain; with all, any string answer too.
func (r *Runtime) showsText(op Operation, value any) bool {
	switch r.Settings().String(PaletteTextResultsSetting) {
	case "plain":
		return false
	case "all":
		if _, text := value.(string); text {
			return true
		}
	}
	return IsTextResult(op.Result)
}

// textFormatOf is the format the palette shows a verb's answer in: the
// verb's own when its result is a text format, else plain.
func textFormatOf(op Operation) string {
	if IsTextResult(op.Result) {
		return op.Result
	}
	return TextFormatPlain
}

// textViewRender draws the text view: the title and, once the text is
// longer than the window, the lines shown out of how many; then the text,
// in a window as tall as the text up to the space there is, so the modal
// keeps its size while the cursor scrolls it (R4.9).
func textViewRender(r *Runtime, inst *Instance, width, height int) Block {
	in := r.Input(textViewInputID)
	if in == nil || inst == nil || width <= 0 || height <= 0 {
		return Text("")
	}
	view, _ := inst.Data.(*textView)
	if view == nil {
		view = &textView{title: inst.Payload.Prompt}
		inst.Data = view
	}
	in.Wrap = r.Settings().Bool("text-view.wrap")
	lines := InputRows(in, width)
	rows := min(lines, max(1, height-1))
	row, _ := in.rowColumn(in.Cursor)
	breaks, _ := in.wrapBreaks(width, in.Cursor)
	for at := range breaks {
		if at <= in.Cursor {
			row++
		}
	}
	view.top = min(max(view.top, row-rows+1), row, lines-rows)
	view.top = max(0, view.top)
	header := view.title
	if rows < lines {
		header += fmt.Sprintf("  %d–%d / %d", view.top+1, view.top+rows, lines)
	}
	if height == 1 {
		return Text(header)
	}
	body := inputBlockFrom(in, width, rows, view.top)
	if !in.Wrap {
		shown := strings.Split(body.Text, "\n")
		source := strings.Split(string(in.Text), "\n")
		for i := range shown {
			if view.top+i < len(source) && cellWidth(source[view.top+i]) >= width {
				shown[i] = ansi.Truncate(shown[i], max(0, width-1), "") + "›"
			}
		}
		body.Text = strings.Join(shown, "\n")
	}
	// As wide as the whole text's widest line (and a cell for the cursor
	// past its end), not the lines in view, so scrolling never resizes it.
	wide := cellWidth(header)
	for _, line := range strings.Split(string(in.Text), "\n") {
		wide = max(wide, cellWidth(line)+1)
	}
	wide = min(wide, width)
	out := strings.Split(header+"\n"+body.Text, "\n")
	for i, line := range out {
		if gap := wide - ansi.StringWidth(line); gap > 0 {
			out[i] = line + strings.Repeat(" ", gap)
		}
	}
	return Block{Text: strings.Join(out, "\n"), Cursor: body.Cursor}.Offset(0, 1)
}

// presentData shows a data answer that nothing else presented, for a verb
// run from the help or the palette: one line in the status line, a longer
// one in the text view (results.show-data). A pending answer, a text
// answer (its own presenter), an empty one and the answer of a framework
// or standard verb are left alone.
func (r *Runtime) presentData(q Request, value any) error {
	if value == nil || !r.Settings().Bool(DataResultsSetting) {
		return nil
	}
	if _, pending := value.(Ticket); pending {
		return nil
	}
	// Only an application verb's own answer: the framework's interaction
	// verbs answer with where they moved, and the standard verbs have their
	// own views and notices.
	op, ok := r.Operation(q.Key())
	if !ok || IsTextResult(op.Result) || op.Kind != KindApplication || op.Standard != "" || op.Motion {
		return nil
	}
	if op.Summary != "" {
		// The verb names its human line: that, never the JSON.
		if line := summaryLine(value, op.Summary); line != "" {
			r.UI().Info(line)
		}
		return nil
	}
	if compact, err := json.Marshal(value); err == nil && len(compact) <= 160 && string(compact) != "null" && string(compact) != "[]" && string(compact) != "{}" {
		r.UI().Notify(r.DisplayKey(q.Key()) + ": " + string(compact))
		return nil
	}
	text := resultText(value)
	if strings.TrimSpace(text) == "" || text == "[]" || text == "{}" {
		return nil
	}
	return r.OpenText(TextDoc{Title: q.Key(), Text: text})
}

// summaryLine is the text an answer holds in its field (Operation.Summary),
// "" when it holds none: the answer as a Go map, or any value whose JSON is
// an object.
func summaryLine(value any, field string) string {
	m, ok := value.(map[string]any)
	if !ok {
		data, err := json.Marshal(value)
		if err != nil || json.Unmarshal(data, &m) != nil {
			return ""
		}
	}
	s, _ := m[field].(string)
	return s
}
