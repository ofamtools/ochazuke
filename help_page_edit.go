package ochazuke

import (
	"encoding/json"
	"os"
	"strings"
)

// Editing from the binding help page (stage 2): the page's keys for the
// binding edit verbs (binding_edit.go). r, a and C capture a key or
// sequence in a pass-through mode (help_page_capture: every key is taken;
// enter applies, esc leaves, backspace drops the last key, ctrl+v takes
// the next key as it is), and the detail lines show what the edit would
// do from the walked focus (HelpWalk.Preview) after every key, so enter
// applies an edit already seen. d d and U apply at once and leave the
// preview of what they did; e opens binding.rebind's form on the row; u
// is history-entry.undo; W shows your bindings.json as written and its
// diff against the file when the page opened. Setting help.edit (default
// on) off leaves the page read-only: the keys stand aside (guard
// help-edits) and the binding verbs stay on the palette and the CLI.

// HelpEditSetting turns the help page's editing keys on (the default) or
// off.
const HelpEditSetting = "help.edit"

// The capture's modes: every key comes through the any row; after ctrl+v
// the next key is taken as a key, whatever it is.
const (
	helpPageCaptureMode = "help_page_capture"
	helpPageLiteralMode = "help_page_literal"
)

// HelpPageEdit is an edit the page is capturing keys for: what it is, the
// keys so far, how to plan it on some keys (for the preview) and the
// request that applies it.
type HelpPageEdit struct {
	What    string   `json:"what"`
	Keys    []string `json:"keys"`
	Literal bool     `json:"literal,omitempty"`
	// Gen is the mode generation the capture is held at; Plan plans the
	// edit on some keys and Apply is the request that makes it.
	Gen   uint64                        `json:"-"`
	Plan  func(keys string) BindingPlan `json:"-"`
	Apply func(keys string) Request     `json:"-"`
}

// capturing reports whether the page is waiting for keys: an edit is held
// and the mode it entered is still the mode (leaving by any other way ends
// it).
func (p *HelpPage) capturing() bool {
	return p.Edit != nil && p.Edit.Gen == p.r.modeGeneration
}

// startCapture holds an edit and enters the capture mode.
func (p *HelpPage) startCapture(e *HelpPageEdit) {
	p.Edit = e
	p.r.enterMode(helpPageCaptureMode, Payload{})
	e.Gen = p.r.modeGeneration
	p.showCapture()
}

// captureTo moves the capture to a mode in place, so the engine does not
// leave the pending mode after the key that ran.
func (p *HelpPage) captureTo(mode string, literal bool) {
	r := p.r
	r.changeMode(ModeState{Name: mode, Payload: Payload{Return: r.Mode.Payload.Return}}, modePayloadUpdate)
	p.Edit.Gen, p.Edit.Literal = r.modeGeneration, literal
	p.showCapture()
}

// endCapture drops the held edit and leaves the capture mode.
func (p *HelpPage) endCapture() {
	p.Edit = nil
	if p.r.modes[p.r.Mode.Name].Pending {
		p.r.leaveMode()
	}
}

// showCapture puts the edit and the preview of its keys so far in the
// detail lines.
func (p *HelpPage) showCapture() {
	e := p.Edit
	keys := strings.Join(e.Keys, " ")
	head := e.What + ": " + keys + " _"
	if e.Literal {
		head += "  (the next key is taken as it is)"
	}
	p.Detail = []string{head}
	if keys == "" {
		p.Detail = append(p.Detail, "Press the key or sequence; enter applies, esc leaves, backspace drops the last key, ctrl+v takes the next key as it is.")
		return
	}
	p.Detail = append(p.Detail, p.preview(e.Plan(keys)).Texts()...)
}

// preview is what a plan does from the walked focus.
func (p *HelpPage) preview(plan BindingPlan) BindingPreview {
	return p.Walk.Preview(plan, p.r.rowGuardText, p.r.displayRequest)
}

// selectedRow is the selected line's row, said on the status line when
// the line is not a row.
func (p *HelpPage) selectedRow() (HelpPageRow, bool) {
	line, ok := p.selectedLine()
	if !ok || line.Kind != HelpLineRow {
		p.r.UI().Info("Select a row first.")
		return HelpPageRow{}, false
	}
	return *line.Row, true
}

// rowArgs are the arguments naming a row to the binding verbs: its table,
// mode, key and guard.
func rowArgs(b Binding) Arguments {
	a := Arguments{"key": planKey(b), "mode": b.Mode, "when": b.When}
	if b.Instance != "" {
		a["instance"] = b.Instance
	} else {
		a["noun"] = b.Noun
	}
	return a
}

// bindingRequest is a binding verb's request.
func bindingRequest(verb string, a Arguments) Request {
	return Request{Noun: "ochazuke.binding", Verb: verb, Args: a}
}

// addAt is where another key for a row goes, and the request it runs:
// the row's table (an instance row's noun, as binding.bind writes noun
// tables) and request; for a verb with no key, the verb's noun in the
// walk's mode.
func (p *HelpPage) addAt(v HelpPageRow) (Binding, Request) {
	if v.Unbound {
		return Binding{Noun: v.Noun, Mode: p.Walk.Mode}, Request{Noun: v.Noun, Verb: v.Verb}
	}
	at := Binding{Noun: v.Row.Noun, Mode: v.Row.Mode}
	if inst := p.r.Instances[v.Row.Instance]; v.Row.Instance != "" && inst != nil {
		at.Noun = inst.Noun
	}
	return at, v.Row.Request
}

// startAdd captures a key for another row of the selected row's request
// (binding.bind; on a verb with no key, its first).
func (p *HelpPage) startAdd(Arguments) error {
	v, ok := p.selectedRow()
	if !ok {
		return nil
	}
	at, q := p.addAt(v)
	r := p.r
	p.startCapture(&HelpPageEdit{What: "add a key to " + r.displayRequest(q),
		Plan: func(keys string) BindingPlan { return r.bindingEditorNow().PlanAdd(at, q, keys) },
		Apply: func(keys string) Request {
			a := Arguments{"key": keys, "action": q.Key(), "noun": at.Noun, "mode": at.Mode, "replace": true}
			if len(q.Args) > 0 {
				a["args"] = map[string]any(cloneArgs(q.Args))
			}
			return bindingRequest("bind", a)
		}})
	return nil
}

// startRebind captures the new key for the selected row (binding.rebind);
// a verb with no key takes its first (as a).
func (p *HelpPage) startRebind(a Arguments) error {
	v, ok := p.selectedRow()
	if !ok {
		return nil
	}
	if v.Unbound {
		return p.startAdd(a)
	}
	r, row := p.r, v.Row.withoutAliases()
	p.startCapture(&HelpPageEdit{What: "rebind " + r.displayRequest(row.Request) + " " + planKey(row),
		Plan: func(keys string) BindingPlan {
			return r.bindingEditorNow().PlanRebind(row, BindingRevision{Keys: keys, SetKeys: true})
		},
		Apply: func(keys string) Request {
			a := rowArgs(row)
			a["to"] = keys
			return bindingRequest("rebind", a)
		}})
	return nil
}

// startUnify captures one key for the selected row's verb on every noun
// that has it (binding.unify), in the walk's mode.
func (p *HelpPage) startUnify(Arguments) error {
	v, ok := p.selectedRow()
	if !ok {
		return nil
	}
	r, verb, mode := p.r, v.Verb, p.Walk.Mode
	p.startCapture(&HelpPageEdit{What: "one key for " + verb + " on every noun that has it",
		Plan: func(keys string) BindingPlan {
			return r.bindingEditorNow().PlanUnify(verb, keys, r.unifyRows(verb, mode, nil), false)
		},
		Apply: func(keys string) Request {
			return bindingRequest("unify", Arguments{"verb": verb, "key": keys, "mode": mode})
		}})
	return nil
}

// applyNow runs an edit on the selected row at once (d d, U), the detail
// lines left saying what it did from the walked focus.
func (p *HelpPage) applyNow(verb string, plan func(BindingEditor, Binding) BindingPlan) error {
	v, ok := p.selectedRow()
	if !ok {
		return nil
	}
	if v.Unbound {
		p.r.UI().Info(v.Operation + " has no key here; a gives it one.")
		return nil
	}
	row := v.Row.withoutAliases()
	planned := plan(p.r.bindingEditorNow(), row)
	lines := p.preview(planned).Texts()
	if _, err := p.r.Execute(bindingRequest(verb, rowArgs(row))); err != nil {
		return err
	}
	head := planned.Name
	if planned.How != "" {
		head += " (" + planned.How + ")"
	}
	p.Detail = append([]string{head}, lines...)
	return nil
}

// editFields opens binding.rebind's form on the selected row, holding its
// key, arguments, hint, hidden and guard.
func (p *HelpPage) editFields(Arguments) error {
	v, ok := p.selectedRow()
	if !ok {
		return nil
	}
	if v.Unbound {
		p.r.UI().Info(v.Operation + " has no key here; a gives it one.")
		return nil
	}
	row := v.Row.withoutAliases()
	a := rowArgs(row)
	a["to"], a["hint"], a["guard"], a["hint-hidden"] = planKey(row), row.Hint, row.When, row.Hidden
	if len(row.Request.Args) > 0 {
		data, _ := json.Marshal(row.Request.Args)
		a["args"] = string(data)
	}
	_, err := p.r.OpenArgsForm(bindingRequest("rebind", a), func(q Request) error {
		_, err := p.r.ExecuteAt(p.r.Context(), q)
		return err
	})
	return err
}

// showFile opens your bindings.json as written, after its diff against the
// file when the page opened.
func (p *HelpPage) showFile(Arguments) error {
	r := p.r
	path := r.Config().Path(BindingsFile)
	now, doc, err := r.readUserBindingsDoc()
	if err != nil {
		return err
	}
	start, _, _ := ParseBindings([]byte(p.FileStart))
	lines := []string{"Changes since the page opened:"}
	lines = append(lines, DiffBindings(start, doc).Lines()...)
	text := strings.TrimSpace(string(now))
	if text == "" {
		text = "(no rows: the file is empty or not there)"
	}
	lines = append(lines, "", path+":", text)
	_, err = r.OpenTextView(BindingsFile, strings.Join(lines, "\n"))
	return err
}

// captureKey adds a key to the capture (the any row); off the capture it
// says so.
func (p *HelpPage) captureKey(a Arguments) error {
	if !p.capturing() {
		p.r.UI().Info("No key is being captured; r, a or C start one.")
		return nil
	}
	p.Edit.Keys = append(p.Edit.Keys, StringArg(a, "key", ""))
	p.captureTo(helpPageCaptureMode, false)
	return nil
}

// captureDrop drops the last captured key.
func (p *HelpPage) captureDrop(Arguments) error {
	if !p.capturing() {
		return nil
	}
	if n := len(p.Edit.Keys); n > 0 {
		p.Edit.Keys = p.Edit.Keys[:n-1]
	}
	p.captureTo(helpPageCaptureMode, false)
	return nil
}

// captureLiteral takes the next key as it is.
func (p *HelpPage) captureLiteral(Arguments) error {
	if p.capturing() {
		p.captureTo(helpPageLiteralMode, true)
	}
	return nil
}

// captureApply applies the edit on the keys captured (its preview stays in
// the detail lines); with no key it leaves.
func (p *HelpPage) captureApply(Arguments) error {
	if !p.capturing() {
		return nil
	}
	e := p.Edit
	keys := strings.Join(e.Keys, " ")
	p.endCapture()
	if keys == "" {
		p.Detail = nil
		p.r.UI().Info("No key pressed: nothing changed.")
		return nil
	}
	lines := p.preview(e.Plan(keys)).Texts()
	if _, err := p.r.Execute(e.Apply(keys)); err != nil {
		return err
	}
	p.Detail = append([]string{e.What + ": " + keys}, lines...)
	return nil
}

// captureCancel leaves the capture with nothing changed.
func (p *HelpPage) captureCancel(Arguments) error {
	if p.capturing() {
		p.endCapture()
		p.Detail = nil
		p.r.UI().Info("Left without changing anything.")
	} else if p.r.pending() {
		p.r.leaveMode()
	}
	return nil
}

// helpPageEditVerbs are the page's editing verbs: their keys stand aside
// with help.edit off (guard help-edits); the capture's own verbs are rows
// of the capture modes only.
func helpPageEditVerbs() []helpPageVerb {
	return []helpPageVerb{
		{"rebind", "Rebind the selected row: press the new key or sequence, see what it replaces or shadows from this focus, enter applies (binding.rebind)", nil, (*HelpPage).startRebind, ""},
		{"add-key", "Give the selected row's verb another key (a verb with no key its first): press it, see the preview, enter applies (binding.bind)", nil, (*HelpPage).startAdd, ""},
		{"remove", "Take the selected row off its key: your own row is deleted, a default gets an unbind row (binding.remove)", nil, func(p *HelpPage, _ Arguments) error {
			return p.applyNow("remove", BindingEditor.PlanRemove)
		}, ""},
		{"reset", "Put the default back on the selected row's key, dropping your rows over it (binding.reset)", nil, func(p *HelpPage, _ Arguments) error {
			return p.applyNow("reset", BindingEditor.PlanReset)
		}, ""},
		{"edit-fields", "Edit the selected row's fields in a form: key, arguments, hint, hidden, guard, table, mode (binding.rebind)", nil, (*HelpPage).editFields, ""},
		{"unify", "One key for the selected row's verb on every noun that has it: press it, see the preview, enter applies (binding.unify)", nil, (*HelpPage).startUnify, ""},
		{"show-file", "Show your bindings.json as written, after its changes since the page opened", nil, (*HelpPage).showFile, ""},
		{"undo", "Undo the last change (history-entry.undo): an edit of your bindings.json writes the file's old text back", nil, func(p *HelpPage, _ Arguments) error {
			err := p.r.undoHistory(0)
			p.Detail = nil
			return err
		}, ""},
	}
}

// helpPageCaptureVerbs are the capture modes' verbs.
func helpPageCaptureVerbs() []helpPageVerb {
	return []helpPageVerb{
		{"capture-key", "Add the key just pressed to the keys being captured for the edit", map[string]Parameter{"key": {Type: "string", Required: true, Description: "the key, as the bindings file spells it"}}, (*HelpPage).captureKey, ""},
		{"capture-drop", "Drop the last key captured for the edit", nil, (*HelpPage).captureDrop, ""},
		{"capture-literal", "Take the next key as a key for the edit, whatever it is (enter, esc and backspace included)", nil, (*HelpPage).captureLiteral, ""},
		{"capture-apply", "Apply the edit on the keys captured", nil, (*HelpPage).captureApply, ExitsPending},
		{"capture-cancel", "Leave the capture without changing anything", nil, (*HelpPage).captureCancel, ExitsPending},
	}
}

// registerHelpPageEdits adds the guard and marks the editing verbs.
func registerHelpPageEdits(r *Runtime) {
	r.InvalidateBindings()
	r.DefineGuard("help-edits", func(r *Runtime, _ KeyEvent) bool { return r.helpFlag(HelpEditSetting, true) })
	n := r.Noun(helpPageNoun)
	if n.Guards == nil {
		n.Guards = map[string]string{}
	}
	for _, v := range helpPageEditVerbs() {
		n.Guards[v.name] = "help-edits"
	}
}

// fileText is the user's bindings.json as it stands, "" when there is none.
func (r *Runtime) fileText() string {
	path := r.Config().Path(BindingsFile)
	if path == "" {
		return ""
	}
	data, _ := os.ReadFile(path)
	return string(data)
}
