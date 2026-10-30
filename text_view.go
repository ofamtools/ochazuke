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
// NounBuilder.TextResult. The palette shows such an answer in the text
// view (palette.text-results); the CLI and the API return the value as
// they return any other.
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

// textView is the text view modal's state: its title and the first line
// its window shows, kept so the window moves only when the cursor would
// leave it.
type textView struct {
	title string
	top   int
}

// TextResult marks the last declared verb's result as text
// (Operation.Result ResultText): the palette opens what it answers in the
// text view, a read-only editor. A string is shown as it is; any other
// value as indented JSON.
func (b *NounBuilder) TextResult() *NounBuilder {
	if b.last == "" {
		panic("TextResult must follow Verb")
	}
	op := b.r.Operations[b.last]
	op.Result = ResultText
	b.r.Operations[b.last] = op
	return b
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

// showsText reports whether the palette shows a verb's answer in the text
// view: a text result unless palette.text-results is plain; with all, any
// string answer too.
func (r *Runtime) showsText(op Operation, value any) bool {
	switch r.Settings().String(PaletteTextResultsSetting) {
	case "plain":
		return false
	case "all":
		if _, text := value.(string); text {
			return true
		}
	}
	return op.Result == ResultText
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
	lines := strings.Count(string(in.Text), "\n") + 1
	rows := min(lines, max(1, height-1))
	row, _ := in.rowColumn(in.Cursor)
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
