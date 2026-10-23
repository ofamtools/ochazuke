package ochazuke

import (
	"errors"
	"strings"
)

type MacroKey struct {
	Key   string `json:"key"`
	Paste bool   `json:"paste,omitempty"`
}
type MacroState struct {
	Recording string     `json:"recording,omitempty"`
	Pending   string     `json:"pending,omitempty"`
	Last      string     `json:"last,omitempty"`
	Keys      []MacroKey `json:"keys,omitempty"`
	Depth     int        `json:"depth"`
	Steps     int        `json:"steps"`
	Count     int        `json:"count,omitempty"`
}

func registerMacros(r *Runtime) {
	add := func(verb, description string, params map[string]Parameter, fn Handler) {
		r.Register(Operation{Verb: verb, Noun: "macro", Description: description, Parameters: params, Result: "macro state", Run: fn})
	}
	add("prefix", "Accumulate an application macro count or await a recording/replay register", map[string]Parameter{"key": {Type: "string", Required: true}, "steps": {Type: "integer"}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		key := StringArg(a, "key", "")
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
			if r.Macro.Count > 100000 {
				return nil, errors.New("macro count exceeds 1000000")
			}
			r.Macro.Count = r.Macro.Count*10 + int(key[0]-'0')
			return r.Macro, nil
		}
		if key != "q" && key != "@" && key != "" {
			return nil, errors.New("macro prefix must be q, @, or a digit")
		}
		r.Macro.Pending = key
		r.Macro.Count = IntArg(a, "steps", 0)
		if in := r.Inputs[c.InputID]; in != nil {
			in.resetKeys()
		}
		return r.Macro, nil
	})
	add("start", "Record application keys into a named register", map[string]Parameter{"register": {Type: "string", Required: true}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		key, appendMode, err := registerName(StringArg(a, "register", ""))
		if err != nil {
			return nil, err
		}
		if key == "_" {
			return nil, errors.New("cannot record into black-hole register")
		}
		if r.Macro.Recording != "" {
			return nil, errors.New("macro recording already active")
		}
		r.Macro.Recording = key
		r.Macro.Pending = ""
		r.Macro.Count = 0
		r.Macro.Keys = nil
		if appendMode {
			r.Macro.Keys = registerMacroKeys(r.Registers[key])
		}
		return r.Macro, nil
	})
	add("stop", "Finish the current macro recording", nil, func(r *Runtime, c Context, a Arguments) (any, error) {
		if r.Macro.Recording == "" {
			return nil, errors.New("no macro recording")
		}
		keys := append([]MacroKey(nil), r.Macro.Keys...)
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
		r.Registers[r.Macro.Recording] = Register{Yank: Yank{Text: text.String(), Shape: "char"}, Keys: keys}
		r.Macro.Recording = ""
		r.Macro.Keys = nil
		r.Macro.Pending = ""
		r.Macro.Count = 0
		return r.Macro, nil
	})
	add("replay", "Replay a register through application dispatch; @ repeats last macro", map[string]Parameter{"register": {Type: "string", Required: true}, "steps": {Type: "integer"}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.replayMacro(StringArg(a, "register", ""), IntArg(a, "steps", 1))
		return r.Macro, err
	})
}

func (r *Runtime) replayMacro(name string, count int) (err error) {
	r.Macro.Pending = ""
	r.Macro.Count = 0
	if name == "@" {
		name = r.Macro.Last
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
	if r.dispatchKey == nil {
		return errors.New("macro replay requires a model dispatcher")
	}
	if r.Macro.Depth >= 16 {
		return errors.New("macro recursion limit exceeded")
	}
	keys := registerMacroKeys(r.Registers[key])
	if len(keys) == 0 {
		return errors.New("macro register is empty")
	}
	outer := r.Macro.Depth == 0
	frameCount := len(r.Frames)
	if outer {
		r.Macro.Steps = 0
	}
	r.Macro.Last = key
	r.Macro.Depth++
	defer func() {
		r.Macro.Depth--
		if outer {
			if err != nil {
				for len(r.Frames) > frameCount {
					if popErr := r.Pop(); popErr != nil {
						break
					}
				}
			}
			r.Macro.Pending = ""
			r.Pending = ""
			if in := r.Inputs[r.Context().InputID]; in != nil {
				in.resetKeys()
			}
		}
	}()
	for range count {
		for _, key := range keys {
			r.Macro.Steps++
			if r.Macro.Steps > 10000 {
				return errors.New("macro step limit exceeded")
			}
			if err = r.dispatchKey(key); err != nil {
				return err
			}
			if r.Quit {
				return nil
			}
		}
	}
	return nil
}
func (r *Runtime) handleMacroKey(key string) (bool, error) {
	if r.Mode(r.Context()) != "normal" {
		return false, nil
	}
	execute := func(verb string, args Arguments) (bool, error) {
		_, err := r.Execute(Request{Verb: verb, Noun: "macro", Args: args})
		return true, err
	}
	if r.Macro.Pending != "" {
		if key == "esc" {
			return execute("prefix", Arguments{"key": ""})
		}
		if r.Macro.Pending == "q" {
			return execute("start", Arguments{"register": key})
		}
		return execute("replay", Arguments{"register": key, "steps": max(1, r.Macro.Count)})
	}
	in := r.Inputs[r.Context().InputID]
	if in == nil && len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		return execute("prefix", Arguments{"key": key})
	}
	if in != nil && in.Pending != "" {
		return false, nil
	}
	if key == "q" && r.Macro.Recording != "" {
		return execute("stop", nil)
	}
	if key == "q" || key == "@" {
		steps := max(1, r.Macro.Count)
		if in != nil {
			steps = max(1, in.Count)
		}
		return execute("prefix", Arguments{"key": key, "steps": steps})
	}
	if in == nil && r.Macro.Count > 0 {
		return execute("prefix", Arguments{"key": ""})
	}
	return false, nil
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
