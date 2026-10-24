package ochazuke

import (
	"errors"
	"slices"
	"strings"
)

type EditNode struct {
	Text     string `json:"text"`
	Cursor   int    `json:"cursor"`
	Parent   int    `json:"parent"`
	Children []int  `json:"children"`
}

type Input struct {
	ID          string
	Text        []rune
	Cursor      int
	Mode        string // the editing mode: normal, insert or a visual mode; pending modes live on the instance
	Multiline   bool
	Kind        string
	PathFilter  string
	Anchor      int
	History     []EditNode
	Head        int
	groupOpen   bool
	groupNode   int
	goalColumn  int
	blockChange *blockChange
	pin         string    // the noun's pinned mode (prompt pins insert)
	inst        *Instance // set by AddInput; holds the full mode and pending payload
}

func NewInput(id, text string, multiline bool) *Input {
	in := &Input{ID: id, Text: []rune(text), Mode: "normal", Multiline: multiline, groupNode: -1, goalColumn: -1}
	in.History = []EditNode{{Text: text, Parent: -1}}
	return in
}

// Count is the pending Vim count, read from the instance payload.
func (in *Input) Count() int {
	if in.inst == nil {
		return 0
	}
	return in.inst.Payload.Count
}

// Pending describes the pending editor mode the way the old grammar did:
// "d" for an operator, "g" for a g prefix, "dg" for both, "register" while a
// register name is awaited, "" otherwise.
func (in *Input) Pending() string {
	if in.inst == nil {
		return ""
	}
	switch in.inst.State {
	case "operator_pending":
		return in.inst.Payload.Op
	case "operator_g_pending":
		return in.inst.Payload.Op + "g"
	case "g_pending":
		return "g"
	case "register_pending":
		return "register"
	}
	return ""
}

func (in *Input) Snapshot() map[string]any {
	row, column := 0, 0
	for i, ch := range in.Text {
		if i >= in.Cursor {
			break
		}
		if ch == '\n' {
			row++
			column = 0
		} else {
			column++
		}
	}
	return map[string]any{
		"id": in.ID, "text": string(in.Text), "cursor": in.Cursor, "mode": in.Mode,
		"multiline": in.Multiline, "pinned": in.pin, "history": in.History, "head": in.Head,
		"count": in.Count(), "pending": in.Pending(), "kind": in.Kind, "pathFilter": in.PathFilter,
		"anchor": in.Anchor, "selection": in.selectionSnapshot(),
		"insertGroup": in.groupOpen, "row": row, "column": column,
	}
}

// SetMode is the mode-transition hook for both editor operations and set.mode.
// Direct assignment to Mode cannot delimit insert sessions.
func (in *Input) SetMode(mode string) {
	if in.pin != "" {
		mode = in.pin
	}
	if mode == in.Mode {
		in.resetKeys()
		return
	}
	old := in.Mode
	if old == "insert" {
		in.finishBlockChange()
	}
	edited := in.groupNode == in.Head
	in.endGroup()
	in.storeMode(mode)
	if isVisual(mode) && !isVisual(old) {
		in.Anchor = in.Cursor
	}
	in.resetKeys()
	in.goalColumn = -1
	if mode == "insert" {
		in.History[in.Head].Cursor = in.Cursor
		in.groupOpen = true
	} else if old == "insert" && mode == "normal" {
		start, _ := in.lineBounds(in.Cursor)
		if in.Cursor > start {
			in.Cursor--
		}
	}
	in.clampCursor()
	if edited {
		in.History[in.Head].Cursor = in.Cursor
	}
}

// storeMode writes the editing mode to the input's instance, the source of
// truth, clearing any pending mode, and to the Mode mirror the editor reads.
func (in *Input) storeMode(mode string) {
	in.Mode = mode
	if in.inst != nil {
		in.inst.State, in.inst.Payload = mode, Payload{}
	}
}

func (in *Input) Replace(start, end int, text string) error {
	if start < 0 || end < start || end > len(in.Text) {
		return errors.New("invalid edit range")
	}
	if !in.Multiline && strings.ContainsAny(text, "\r\n") {
		return errors.New("single-line input cannot contain newlines")
	}
	inserted := []rune(text)
	if slices.Equal(in.Text[start:end], inserted) {
		return nil
	}
	if in.Mode == "insert" && !in.groupOpen {
		in.groupOpen = true
		in.groupNode = -1
		in.History[in.Head].Cursor = in.Cursor
	}
	next := slices.Replace(in.Text, start, end, inserted...)
	grouped := in.groupOpen && in.groupNode == in.Head
	if !grouped && !in.groupOpen {
		in.History[in.Head].Cursor = in.Cursor
	}
	in.Text = next
	in.Cursor = start + len(inserted)
	in.goalColumn = -1
	in.clampCursor()
	if grouped {
		in.History[in.Head].Text = string(next)
		in.History[in.Head].Cursor = in.Cursor
	} else {
		node := EditNode{Text: string(next), Cursor: in.Cursor, Parent: in.Head}
		in.History[in.Head].Children = append(in.History[in.Head].Children, len(in.History))
		in.History = append(in.History, node)
		in.Head = len(in.History) - 1
		if in.groupOpen {
			in.groupNode = in.Head
		}
	}
	return nil
}

func (in *Input) Undo(steps int) error {
	if steps < 1 {
		return errors.New("steps must be positive")
	}
	in.endGroup()
	in.blockChange = nil
	if isVisual(in.Mode) {
		in.storeMode("normal")
	}
	in.resetKeys()
	for ; steps > 0; steps-- {
		parent := in.History[in.Head].Parent
		if parent < 0 {
			break
		}
		in.Head = parent
	}
	in.restore()
	return nil
}

func (in *Input) Redo(steps, branch int) error {
	if steps < 1 {
		return errors.New("steps must be positive")
	}
	if branch < -1 {
		return errors.New("branch must be nonnegative or omitted")
	}
	if branch >= len(in.History[in.Head].Children) && branch >= 0 {
		return errors.New("unknown undo branch")
	}
	in.endGroup()
	in.blockChange = nil
	if isVisual(in.Mode) {
		in.storeMode("normal")
	}
	in.resetKeys()
	for ; steps > 0; steps-- {
		children := in.History[in.Head].Children
		if len(children) == 0 {
			break
		}
		pick := len(children) - 1
		if branch >= 0 {
			pick = branch
			branch = -1
		}
		in.Head = children[pick]
	}
	in.restore()
	return nil
}

func (in *Input) restore() {
	node := in.History[in.Head]
	in.Text = []rune(node.Text)
	in.Cursor = node.Cursor
	in.goalColumn = -1
	in.clampCursor()
}

func (in *Input) endGroup() {
	if in.groupNode == in.Head {
		in.History[in.Head].Cursor = in.Cursor
	}
	in.groupOpen = false
	in.groupNode = -1
}

// resetKeys drops a pending count, operator or prefix: the instance returns
// to the editing mode Input.Mode names.
func (in *Input) resetKeys() {
	if in.inst != nil && (in.inst.State != in.Mode || !in.inst.Payload.empty()) {
		in.inst.State, in.inst.Payload = in.Mode, Payload{}
	}
}

func RegisterInputOperations(r *Runtime) {
	r.Modes["normal"] = true
	r.Modes["insert"] = true
	for _, spec := range []struct {
		verb, desc string
		params     map[string]Parameter
		fn         func(*Input, Arguments) error
	}{
		{"insert", "Insert literal text at the rune cursor", map[string]Parameter{"text": {Type: "string", Required: true}}, func(in *Input, a Arguments) error {
			in.resetKeys()
			return in.Replace(in.Cursor, in.Cursor, StringArg(a, "text", ""))
		}},
		{"replace", "Replace a half-open rune range", map[string]Parameter{"start": {Type: "integer", Required: true}, "end": {Type: "integer", Required: true}, "text": {Type: "string", Required: true}}, func(in *Input, a Arguments) error {
			in.resetKeys()
			return in.Replace(IntArg(a, "start", 0), IntArg(a, "end", 0), StringArg(a, "text", ""))
		}},
		{"undo", "Undo changes in this input", map[string]Parameter{"steps": {Type: "integer"}}, func(in *Input, a Arguments) error { return in.Undo(IntArg(a, "steps", 1)) }},
		{"redo", "Redo changes; branch is a zero-based child index for the first step", map[string]Parameter{"steps": {Type: "integer"}, "branch": {Type: "integer"}}, func(in *Input, a Arguments) error { return in.Redo(IntArg(a, "steps", 1), IntArg(a, "branch", -1)) }},
		{"move", "Move by left/right/up/down, Vim word-forward/backward/end, Readline word-next/previous, line-start/end, first-line/last-line", map[string]Parameter{"motion": {Type: "string", Required: true}, "steps": {Type: "integer"}, "line": {Type: "integer"}}, func(in *Input, a Arguments) error {
			return in.move(StringArg(a, "motion", ""), IntArg(a, "steps", 1), IntArg(a, "line", 0))
		}},
		{"begin", "Enter insert mode at insert/append/first/end/below/above", map[string]Parameter{"position": {Type: "string"}}, func(in *Input, a Arguments) error { return in.begin(StringArg(a, "position", "insert")) }},
		{"delete", "Delete forward/backward runes without crossing a normal-mode line", map[string]Parameter{"direction": {Type: "string"}, "steps": {Type: "integer"}}, func(in *Input, a Arguments) error {
			direction, steps := StringArg(a, "direction", "forward"), IntArg(a, "steps", 1)
			start, end := in.Cursor, in.Cursor
			normal := in.Mode == "normal"
			if normal && steps > 0 {
				boundedSteps := min(steps, len(in.Text))
				lower, upper := in.lineBounds(in.Cursor)
				if direction == "backward" {
					start = max(lower, start-boundedSteps)
				} else {
					end = min(upper, end+boundedSteps)
				}
			}
			killed := ""
			if normal {
				killed = string(in.Text[start:end])
			}
			if err := in.deleteRunes(direction, steps); err != nil {
				return err
			}
			if normal && killed != "" {
				return r.storeYank(Yank{Text: killed, Shape: "char"}, "", true)
			}
			return nil
		}},
	} {
		fn := spec.fn
		r.Register(Operation{Verb: spec.verb, Noun: "input", Description: spec.desc, Parameters: spec.params, Result: "input state", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			in := r.Inputs[c.InputID]
			if in == nil {
				return nil, errors.New("no focused input")
			}
			if err := fn(in, a); err != nil {
				return nil, err
			}
			return in.Snapshot(), nil
		}})
	}
	r.Register(Operation{Verb: "cancel", Noun: "input", Description: "Cancel the pending count, operator, prefix or register of the focused instance; a visual selection returns to normal", Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		r.ActiveRegister = ""
		inst := r.Instances[focusOf(c)]
		if in := r.Inputs[c.InputID]; in != nil {
			if isVisual(in.Mode) {
				in.SetMode("normal")
			} else {
				in.resetKeys()
			}
			r.modeGeneration++
			return in.Mode, nil
		}
		if r.pendingModes[inst.State] || inst.Payload.Prefix != "" {
			r.leaveMode(inst)
		}
		return r.modeState(inst.ID).Name, nil
	}})
	registerEditorOperations(r)
}
