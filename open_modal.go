package ochazuke

import (
	"errors"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// OpenModalSetting turns the open modal (ochazuke.open.show) on (the
// default) or off.
const OpenModalSetting = "open.modal"

// OpenModalID is the open modal's record editor.
const OpenModalID = "__open-form"

// OpenNoun holds the verbs of the opener table.
const OpenNoun = "ochazuke.open"

// openResult is what open.resolve and open.run answer.
type openResult struct {
	Text   string `json:"text"`
	Kind   string `json:"kind"`   // what the text was read as
	Action string `json:"action"` // the OpenAction constant
	Does   string `json:"does"`   // what it does, in a line
}

func openResultOf(t OpenTarget, a OpenAction) openResult {
	return openResult{Text: t.Text, Kind: t.Kind, Action: a.Kind, Does: a.String()}
}

// registerOpen registers the open verbs: show (the modal), run (open text
// now) and resolve (what open would do). None has a default key.
func registerOpen(r *Runtime) {
	text := func(required bool) map[string]Parameter {
		return map[string]Parameter{"text": {Type: "string", Required: required, Rest: true, Description: "A link, a path, a file, an image or a noun:id: whatever should open"}}
	}
	r.Register(Operation{Key: KeyOf(OpenNoun, "resolve"), Description: "Say what opening the text would do, without doing it (the opener table: open.openers, open.unmatched)", Parameters: text(true), Result: "open result", HistoryKind: HistoryFetch, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		t, act, _ := r.ResolveOpen(StringArg(a, "text", ""))
		return openResultOf(t, act), nil
	}})
	r.Register(Operation{Key: KeyOf(OpenNoun, "run"), Description: "Open the text through the opener table: a link, a path, a file, an image or a noun:id; unmatched text follows open.unmatched", Parameters: text(true), Result: "open result", HistoryKind: HistoryAction, Agent: AgentConfirm, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		t, _, _ := r.ResolveOpen(StringArg(a, "text", ""))
		act, err := r.Open(StringArg(a, "text", ""))
		return openResultOf(t, act), err
	}})
	r.Register(Operation{Key: KeyOf(OpenNoun, "show"), Description: "Open the open modal: type or paste anything, see what it would do, enter opens it (setting open.modal)", Parameters: text(false), Result: "open result", HistoryKind: HistoryAction, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if !r.Settings().Bool(OpenModalSetting) {
			return nil, errors.New("the open modal is off (setting open.modal); open.run opens text directly")
		}
		return nil, r.openModal(StringArg(a, "text", ""))
	}})
	r.Define(OpenNoun)
}

// openModal opens the one-field form over the focus: the text is free
// (line breaks stay), and the line under it says what enter would do.
func (r *Runtime) openModal(prefill string) error {
	if r.Instances[OpenModalID] != nil {
		return errors.New("the open modal is already open")
	}
	var editor *RecordEditor
	spec := RecordSpec{
		Title:  "Open",
		Fields: []RecordField{{Name: "text", Label: "open", Placeholder: "a link, a path, a file, noun:id", Parameter: Parameter{Type: "string", Required: true}}},
		Load:   func() (Record, error) { return Record{"text": prefill}, nil },
		Insert: true,
		Submit: func(rec Record) Result {
			if _, err := r.Open(rec["text"]); err != nil {
				return Failed(err)
			}
			return Done(nil)
		},
		Render: func(s Styles, width, height int) Block {
			b := editor.Block(width, max(1, height-1))
			_, act, _ := r.ResolveOpen(editor.Values()["text"])
			line := "→ " + act.String()
			if act.Unmatched {
				line += " · no opener row matches"
			}
			b.Text += "\n" + s.Muted.Render(ansi.Truncate(strings.ReplaceAll(line, "\n", "⏎"), width, "…"))
			return b
		},
	}
	var err error
	editor, err = r.OpenRecordEditor(OpenModalID, spec)
	return err
}
