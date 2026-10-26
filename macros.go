package ochazuke

import (
	"errors"
	"strings"
)

// MacroKey is one recorded key of a macro; Paste marks pasted text.
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
// no replay is running, every key runs macro.record, which appends the key
// and dispatches it again in the mode it shadowed; q stops only when that
// mode is a quiet normal (the shadow-idle guard) and is recorded otherwise.
// The rows are Requests on the app noun like every other row.
func registerMacros(r *Runtime) {
	r.DefineMode("macro_recording", true)
	r.Define("app").Mode("macro_recording").
		Bind("q", Do("macro.stop")).When("shadow-idle").
		Pattern(Any, Do("macro.record").FromKey("key")).
		Pattern(Paste, Do("macro.record", Arguments{"paste": true}).FromKey("key"))
	keyParams := map[string]Parameter{
		"key":   {Type: "string", Required: true, Description: "Key name as bindings spell it; for a paste, the bracketed text"},
		"paste": {Type: "boolean", Description: "The key is pasted text"},
	}
	add := func(verb, description string, params map[string]Parameter, fn Handler) {
		r.Register(Operation{Verb: verb, Noun: "macro", Description: description, Parameters: params, Result: "macro state", Run: fn})
	}
	add("append", "Append one key to the macro being recorded", keyParams, func(r *Runtime, c Context, a Arguments) (any, error) {
		g := r.Registers()
		if g.Macro.Recording == "" {
			return nil, errors.New("no macro recording")
		}
		if len(g.Macro.Keys) >= 10000 {
			return nil, errors.New("macro recording exceeds 10000 keys")
		}
		paste, _ := a["paste"].(bool)
		g.Macro.Keys = append(g.Macro.Keys, MacroKey{Key: StringArg(a, "key", ""), Paste: paste})
		return g.Macro, nil
	})
	add("record", "Record one key: append it to the recording, then dispatch it in the shadowed mode (macro.append, then key.dispatch)", keyParams, func(r *Runtime, c Context, a Arguments) (any, error) {
		if _, err := r.ExecuteAt(c, Request{Verb: "append", Noun: "macro", Args: a}); err != nil {
			return nil, err
		}
		args := Arguments{"mode": "shadow"}
		for k, v := range a {
			args[k] = v
		}
		if _, err := r.ExecuteAt(c, Request{Verb: "dispatch", Noun: "key", Args: args}); err != nil {
			return nil, err
		}
		return r.Registers().Macro, nil
	})
	add("start", "Record application keys into a named register", map[string]Parameter{"register": {Type: "string", Required: true}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		key, appendMode, err := registerName(StringArg(a, "register", ""))
		if err != nil {
			return nil, err
		}
		if isBlackHole(key) {
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
			if key.Paste && pasteKey(pasteText(key.Key)) == key.Key {
				text.WriteString(pasteText(key.Key))
			} else {
				text.WriteString(keyText(key.Key))
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
	modalCount := len(r.modals)
	if outer {
		g.Macro.Steps = 0
	}
	g.Macro.Last = key
	g.Macro.Depth++
	defer r.forceSource("macro:" + key)()
	defer func() {
		g.Macro.Depth--
		if outer {
			if err != nil {
				r.CancelPendingBinding()
				for len(r.modals) > modalCount {
					if popErr := r.Pop(); popErr != nil {
						break
					}
				}
			}
			if r.pending() {
				if in := r.Input(r.Focus); in != nil {
					in.resetKeys()
				} else {
					r.leaveMode()
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
		keys = append(keys, MacroKey{Key: textKey(ch)})
	}
	return keys
}
