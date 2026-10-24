package ochazuke

import (
	"errors"
	"strings"
)

type MacroKey struct {
	Key   string `json:"key"`
	Paste bool   `json:"paste,omitempty"`
}

// MacroState is the application-wide recording and replay state. Awaiting a
// register name is the macro_pending or replay_pending mode of the focused
// instance, not a field here.
type MacroState struct {
	Recording string     `json:"recording,omitempty"`
	Last      string     `json:"last,omitempty"`
	Keys      []MacroKey `json:"keys,omitempty"`
	Depth     int        `json:"depth"`
	Steps     int        `json:"steps"`
}

// Recording is the macro_recording shadow mode: while a register records and
// no replay is running, every key is appended, then dispatched again in the
// mode it shadowed; q stops only when that mode is a quiet normal. The rows
// are Func rows on the app noun because they re-dispatch.
func registerMacros(r *Runtime) {
	record := func(r *Runtime, ev KeyEvent) error {
		g := r.Registers()
		if len(g.Macro.Keys) >= 10000 {
			return errors.New("macro recording exceeds 10000 keys")
		}
		g.Macro.Keys = append(g.Macro.Keys, ev.Key)
		_, err := r.dispatchIn(ev.Focus, *ev.Mode.Payload.Shadow, ev.Key)
		return err
	}
	r.DefineMode("macro_recording", true)
	r.Define("app").Mode("macro_recording").
		Bind("q", Func(func(r *Runtime, ev KeyEvent) error {
			shadow := ev.Mode.Payload.Shadow
			if shadow.Name == "normal" && shadow.Payload.empty() {
				_, err := r.Execute(Request{Verb: "stop", Noun: "macro"})
				return err
			}
			return record(r, ev)
		})).
		Pattern(Any, Func(record)).
		Pattern(Paste, Func(record))
	add := func(verb, description string, params map[string]Parameter, fn Handler) {
		r.Register(Operation{Verb: verb, Noun: "macro", Description: description, Parameters: params, Result: "macro state", Run: fn})
	}
	add("start", "Record application keys into a named register", map[string]Parameter{"register": {Type: "string", Required: true}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		key, appendMode, err := registerName(StringArg(a, "register", ""))
		if err != nil {
			return nil, err
		}
		if key == "_" {
			return nil, errors.New("cannot record into black-hole register")
		}
		g := r.Registers()
		if g.Macro.Recording != "" {
			return nil, errors.New("macro recording already active")
		}
		g.Macro.Recording = key
		g.Macro.Keys = nil
		if appendMode {
			g.Macro.Keys = registerMacroKeys(g.Named[key])
		}
		return g.Macro, nil
	})
	add("stop", "Finish the current macro recording", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		g := r.Registers()
		if g.Macro.Recording == "" {
			return nil, errors.New("no macro recording")
		}
		keys := append([]MacroKey(nil), g.Macro.Keys...)
		var text strings.Builder
		for _, key := range keys {
			if key.Paste && strings.HasPrefix(key.Key, "[") {
				text.WriteString(strings.TrimSuffix(strings.TrimPrefix(key.Key, "["), "]"))
			} else {
				switch key.Key {
				case "esc":
					text.WriteByte('\x1b')
				case "enter":
					text.WriteByte('\n')
				case "tab":
					text.WriteByte('\t')
				case "space":
					text.WriteByte(' ')
				default:
					text.WriteString(key.Key)
				}
			}
		}
		g.Named[g.Macro.Recording] = Register{Yank: Yank{Text: text.String(), Shape: "char"}, Keys: keys}
		g.Macro.Recording = ""
		g.Macro.Keys = nil
		return g.Macro, nil
	})
	add("replay", "Replay a register through application dispatch; @ repeats last macro", map[string]Parameter{"register": {Type: "string", Required: true}, "steps": {Type: "integer"}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.replayMacro(StringArg(a, "register", ""), IntArg(a, "steps", 1))
		return r.Registers().Macro, err
	})
}

func (r *Runtime) replayMacro(name string, count int) (err error) {
	g := r.Registers()
	if name == "@" {
		name = g.Macro.Last
		if name == "" {
			return errors.New("no previous macro")
		}
	}
	key, _, err := registerName(name)
	if err != nil {
		return err
	}
	if count < 1 || count > 10000 {
		return errors.New("macro count must be between 1 and 10000")
	}
	if g.Macro.Depth >= 16 {
		return errors.New("macro recursion limit exceeded")
	}
	keys := registerMacroKeys(g.Named[key])
	if len(keys) == 0 {
		return errors.New("macro register is empty")
	}
	outer := g.Macro.Depth == 0
	frameCount := len(r.Frames)
	if outer {
		g.Macro.Steps = 0
	}
	g.Macro.Last = key
	g.Macro.Depth++
	defer func() {
		g.Macro.Depth--
		if outer {
			if err != nil {
				r.CancelPendingBinding()
				for len(r.Frames) > frameCount {
					if popErr := r.Pop(); popErr != nil {
						break
					}
				}
			}
			if inst := r.Instances[r.FocusID()]; inst != nil && (r.pendingModes[inst.State] || inst.Payload.Prefix != "") {
				if in := r.Input(inst.ID); in != nil {
					in.resetKeys()
				} else {
					r.leaveMode(inst)
				}
			}
		}
	}()
	for range count {
		for _, key := range keys {
			g.Macro.Steps++
			if g.Macro.Steps > 10000 {
				return errors.New("macro step limit exceeded")
			}
			if _, err = r.Dispatch(key); err != nil {
				return err
			}
			if r.Quit {
				return nil
			}
		}
	}
	return nil
}
func registerMacroKeys(register Register) []MacroKey {
	if len(register.Keys) > 0 {
		return append([]MacroKey(nil), register.Keys...)
	}
	keys := make([]MacroKey, 0, len(register.Yank.Text))
	for _, ch := range register.Yank.Text {
		key := string(ch)
		switch ch {
		case '\n':
			key = "enter"
		case '\t':
			key = "tab"
		case '\x1b':
			key = "esc"
		}
		keys = append(keys, MacroKey{Key: key})
	}
	return keys
}
