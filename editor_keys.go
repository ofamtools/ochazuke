package ochazuke

func editorMotion(key string) string {
	return map[string]string{"h": "left", "left": "left", "j": "down", "down": "down", "k": "up", "up": "up", "l": "right", "right": "right", "w": "word-forward", "b": "word-backward", "e": "word-end", "0": "line-start", "home": "line-start", "$": "line-end", "end": "line-end", "G": "last-line"}[key]
}
func handleNormalEditorKey(r *Runtime, in *Input, key string) (bool, error) {
	command := func(verb string, args Arguments) (bool, error) {
		_, err := r.Execute(Request{Verb: verb, Noun: "input", Target: "focused-input", Args: args})
		return true, err
	}
	prefix := func(key string) (bool, error) { return command("prefix", Arguments{"key": key}) }
	if key == "esc" {
		if _, err := command("cancel", nil); err != nil {
			return true, err
		}
		if isVisual(in.Mode) {
			_, err := r.Execute(Request{Verb: "set", Noun: "mode", Args: Arguments{"mode": "normal"}})
			return true, err
		}
		return true, nil
	}
	if in.Pending == "register" {
		_, err := r.Execute(Request{Verb: "select", Noun: "register", Args: Arguments{"name": key}})
		if err == nil {
			_, err = r.Execute(Request{Verb: "prefix", Noun: "input", Args: Arguments{"key": "register-done"}})
		} else {
			_, _ = command("cancel", nil)
		}
		return true, err
	}
	if key == "\"" {
		return prefix("register")
	}
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' && (key != "0" || in.Count > 0) {
		return prefix(key)
	}
	steps := max(1, in.Count)
	pending := in.Pending
	if pending == "g" {
		if key == "g" {
			return command("move", Arguments{"motion": "first-line", "line": steps})
		}
		return command("cancel", nil)
	}
	if pending != "" {
		op := pending[:1]
		if op == "y" || op == "d" || op == "c" {
			steps *= max(1, in.operatorCount)
			motion := editorMotion(key)
			line := 0
			if key == op && len(pending) == 1 {
				motion = "line"
			}
			if key == "g" {
				if len(pending) == 1 {
					return prefix("g")
				}
				motion = "first-line"
				line = steps
			} else if len(pending) > 1 {
				return command("cancel", nil)
			}
			if motion == "last-line" {
				line = in.Count
				if in.Count == 0 && in.operatorCount > 0 {
					line = in.operatorCount
				}
			}
			if motion != "" {
				return command("operator", Arguments{"operator": op, "motion": motion, "steps": steps, "line": line})
			}
			return command("cancel", nil)
		}
	}
	switch key {
	case "v", "V", "ctrl+v":
		shape := map[string]string{"v": "char", "V": "line", "ctrl+v": "block"}[key]
		return command("select", Arguments{"shape": shape})
	case "y", "d", "c":
		if isVisual(in.Mode) {
			return command("operator", Arguments{"operator": key})
		}
		return prefix(key)
	case "p", "P":
		return command("paste", Arguments{"before": key == "P", "steps": steps})
	case "g":
		return prefix(key)
	case "i", "a", "I", "A", "o", "O":
		if isVisual(in.Mode) {
			return command("cancel", nil)
		}
		position := map[string]string{"i": "insert", "a": "append", "I": "first", "A": "end", "o": "below", "O": "above"}[key]
		return command("begin", Arguments{"position": position})
	case "x", "delete":
		if isVisual(in.Mode) {
			return command("operator", Arguments{"operator": "d"})
		}
		return command("delete", Arguments{"steps": steps})
	case "u":
		return command("undo", Arguments{"steps": steps})
	case "ctrl+r":
		return command("redo", Arguments{"steps": steps})
	}
	if motion := editorMotion(key); motion != "" {
		args := Arguments{"motion": motion, "steps": steps}
		if key == "G" {
			args["line"] = in.Count
		}
		return command("move", args)
	}
	if in.Count != 0 || in.Pending != "" {
		return command("cancel", nil)
	}
	return false, nil
}
