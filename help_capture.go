package ochazuke

import (
	"errors"
	"strings"
)

// HelpBindSequencesSetting lets the help's interactive bind capture a
// sequence of keys (enter ends it, backspace drops the last key, ctrl+v
// takes the next key as it comes). Off, b binds the first key pressed at
// once, as it did before sequences.
const HelpBindSequencesSetting = "help.bind-sequences"

// The two modes of the capture: keys come through the any row; after
// ctrl+v the next key is taken as a key to bind, whatever it is.
const (
	helpCaptureMode = "help_bind_pending"
	helpLiteralMode = "help_bind_literal"
)

// capturing reports whether the interactive bind is waiting for keys: its
// state is held and the mode it entered is still the mode (leaving by any
// other way, a focus change or a closed modal, ends it).
func (h *helpState) capturing() bool {
	return h.capture != nil && h.captureGen == h.r.modeGeneration
}

// captureHeader is the line the help shows while capturing: what is being
// bound and the keys so far. The keys to finish, drop or cancel come from
// the capture mode's rows, as every hint does.
func (h *helpState) captureHeader() string {
	if !h.capturing() {
		return ""
	}
	row, ok := h.r.selectedHelpRow()
	if !ok {
		return ""
	}
	text := "bind " + row.Request + ": " + strings.Join(h.capture, " ") + " _"
	if h.literal {
		text += "  (the next key is taken as it is)"
	}
	return text
}

// captureTo moves the capture to a mode and marks it as the mode the keys
// belong to: a change of the mode's payload in place, so the engine does
// not leave the pending mode after the key that ran.
func (h *helpState) captureTo(mode string, literal bool) {
	h.r.changeMode(ModeState{Name: mode, Payload: Payload{Return: h.r.Mode.Payload.Return}}, modePayloadUpdate)
	h.captureGen = h.r.modeGeneration
	h.literal = literal
}

// endCapture drops the held keys and leaves the capture mode.
func (h *helpState) endCapture() {
	h.capture = nil
	if h.r.modes[h.r.Mode.Name].Pending {
		h.r.leaveMode()
	}
}

// bindCaptured binds the keys held to the selected verb: leaves the mode
// first, so a question about a taken key opens over the help as it does
// from every route (binding.bind's key-taken check).
func (h *helpState) bindCaptured(row HelpRow) (any, error) {
	keys := strings.Join(h.capture, " ")
	h.endCapture()
	if keys == "" {
		return nil, errors.New("no key pressed: nothing to bind")
	}
	return h.r.Execute(Request{Noun: "ochazuke.binding", Verb: "bind", Args: Arguments{"key": keys, "action": row.Request}})
}

func registerHelpCapture(r *Runtime) {
	selected := func(r *Runtime) (*helpState, HelpRow, error) {
		row, ok := r.selectedHelpRow()
		h := r.helpOpen()
		if h == nil || !ok || !row.Unbound {
			return nil, HelpRow{}, errors.New("select an unbound verb in the help")
		}
		return h, row, nil
	}
	r.Register(Operation{Key: KeyOf(helpID, "bind-start"), Staying: true, Description: "Wait for the keys to bind to the selected unbound verb: a key that starts no sequence is bound at once; a sequence goes on (g, then z), enter binds the keys so far, esc cancels, backspace drops the last key, ctrl+v takes the next key as it comes. With manual only enter ends it", Parameters: map[string]Parameter{
		"manual": {Type: "boolean", Description: "only enter ends the capture (a key that is bound alone can begin a sequence)"},
	}, Result: "mode", Run: func(r *Runtime, _ Context, a Arguments) (any, error) {
		h, _, err := selected(r)
		if err != nil {
			return nil, err
		}
		h.capture, h.manual = []string{}, a["manual"] == true
		r.enterMode(helpCaptureMode, Payload{})
		h.captureGen = r.modeGeneration
		return r.Mode, nil
	}})
	r.Register(Operation{Key: KeyOf(helpID, "bind-key"), Staying: true, Description: "Add the key just pressed to the keys being bound to the selected unbound verb, and bind them when no sequence goes on from them (a key that already runs something asks first)", Parameters: map[string]Parameter{
		"key":     {Type: "string", Required: true, Description: "the key, as the bindings file spells it"},
		"literal": {Type: "boolean", Description: "the key was taken after ctrl+v: it is added whatever it is"},
	}, Result: "binding", Run: func(r *Runtime, _ Context, a Arguments) (any, error) {
		h, row, err := selected(r)
		if err != nil {
			return nil, err
		}
		if h.capture == nil { // the verb alone, from the CLI or the palette: one key
			h.capture, h.manual = []string{}, false
		}
		h.capture = append(h.capture, StringArg(a, "key", ""))
		more := r.helpFlag(HelpBindSequencesSetting, true)
		if more && (h.manual || r.bindContinues(strings.Join(h.capture, " "))) {
			h.captureTo(helpCaptureMode, false)
			return strings.Join(h.capture, " "), nil
		}
		return h.bindCaptured(row)
	}})
	r.Register(Operation{Key: KeyOf(helpID, "bind-finish"), Staying: true, Description: "Bind the keys captured so far to the selected unbound verb (a taken key or a sequence it cuts short asks first)", Result: "binding", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		h, row, err := selected(r)
		if err != nil {
			return nil, err
		}
		return h.bindCaptured(row)
	}})
	r.Register(Operation{Key: KeyOf(helpID, "bind-drop"), Staying: true, Description: "Drop the last key of the keys being captured for the bind", Result: "mode", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		h, _, err := selected(r)
		if err != nil {
			return nil, err
		}
		if n := len(h.capture); n > 0 {
			h.capture = h.capture[:n-1]
		}
		h.captureTo(helpCaptureMode, false)
		return strings.Join(h.capture, " "), nil
	}})
	r.Register(Operation{Key: KeyOf(helpID, "bind-literal"), Staying: true, Description: "Take the next key as a key to bind, whatever it is (enter, esc and backspace included)", Result: "mode", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		h, _, err := selected(r)
		if err != nil {
			return nil, err
		}
		h.captureTo(helpLiteralMode, true)
		return r.Mode, nil
	}})
	r.Register(Operation{Key: KeyOf(helpID, "bind-cancel"), Staying: true, Description: "Leave the bind without binding anything", Result: "mode", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		if h := r.helpOpen(); h != nil {
			h.endCapture()
		} else if r.pending() {
			r.leaveMode()
		}
		return r.Mode, nil
	}})
	r.DefineGuard("help-bind-sequence-row", func(r *Runtime, _ KeyEvent) bool {
		row, ok := r.selectedHelpRow()
		return ok && row.Unbound && r.helpFlag(HelpBindSequencesSetting, true)
	})
}
