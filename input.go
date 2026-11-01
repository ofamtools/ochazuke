package ochazuke

import (
	"errors"
	"slices"
	"strings"
)

// EditNode is one state in an input's undo tree: its text, cursor, parent and children.
type EditNode struct {
	Text     string `json:"text"`
	Cursor   int    `json:"cursor"`
	Parent   int    `json:"parent"`
	Children []int  `json:"children"`
}

// Input is a single- or multi-line modal text field with an undo tree.
type Input struct {
	ID         string
	Text       []rune
	Cursor     int
	Mode       string // the editing mode: normal, insert or a visual mode; pending modes live on the instance
	Multiline  bool
	Kind       string
	PathFilter string
	// Secret marks an input whose text is never shown outside it: the key
	// echo (keys.echo) shows nothing at all while it has the focus, not
	// even how much was typed. A record editor field over a Secret prop
	// sets it (RecordField.Secret).
	Secret bool
	// Completions are the input's completion triggers (Completions, the
	// AddInput option); none falls back to its noun's.
	Completions []CompletionTrigger
	// ReadOnly makes the input a viewer: the cursor, motions, visual
	// selections and yanks work, every edit is a silent no-op (Replace
	// leaves the text, insert mode is not entered, a delete or change
	// operator neither deletes nor yanks), and the editing verbs are
	// unavailable at it (guard input-writable), so their rows stand aside
	// and leave the hints. Like Vim's 'nomodifiable', without E21: nothing
	// is reported. Set it before or after AddInput (or the ReadOnly option).
	ReadOnly bool
	// Wrap soft-wraps the input's lines where InputBlock draws them: a
	// line wider than the block continues on the next row, broken after
	// the last space that fits (a word wider than the row breaks where it
	// must). Only the drawing changes; the text holds no added line
	// breaks. Off, a long line scrolls sideways around the cursor. See
	// input_wrap.go.
	Wrap bool
	// WrapIndent is how many cells a wrapped line's continuation rows are
	// indented (nil: the line's leading spaces, Vim's 'breakindent').
	WrapIndent  func(line string) int
	Anchor      int
	History     []EditNode
	Head        int
	groupOpen   bool
	groupNode   int
	goalColumn  int
	blockChange *blockChange
	pin         string // the noun's pinned mode (prompt pins insert)
	// typeOver is the open select range [start, end) (SelectText): in
	// insert mode the next text typed replaces it; nil when none is open.
	typeOver *[2]int
	r        *Runtime // set by AddInput; while the input is focused its mode is r.Mode
}

// NewInput returns an input in normal mode holding text, with text as its undo root.
func NewInput(id, text string, multiline bool) *Input {
	in := &Input{ID: id, Text: []rune(text), Mode: "normal", Multiline: multiline, groupNode: -1, goalColumn: -1}
	in.History = []EditNode{{Text: text, Parent: -1}}
	return in
}

// focused reports whether the input is the runtime's focus, which puts it
// in the global mode.
func (in *Input) focused() bool { return in.r != nil && in.r.Focus == in.ID }

// Count is the pending Vim count, read from the global mode payload while
// the input is focused.
func (in *Input) Count() int {
	if !in.focused() {
		return 0
	}
	return in.r.Mode.Payload.Count
}

// Pending describes the pending editor mode the way the old grammar did:
// "d" for an operator, "g" for a g prefix, "dg" for both, "register" while a
// register name is awaited, "" otherwise.
func (in *Input) Pending() string {
	if !in.focused() {
		return ""
	}
	switch in.r.Mode.Name {
	case "operator_pending":
		return in.r.Mode.Payload.Op
	case "operator_g_pending":
		return in.r.Mode.Payload.Op + "g"
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
		"anchor": in.Anchor, "selection": in.selectionSnapshot(), "typeOver": in.typeOverSnapshot(),
		"insertGroup": in.groupOpen, "row": row, "column": column, "readOnly": in.ReadOnly,
	}
}

// SetMode is the mode-transition hook for both editor operations and set.mode.
// Direct assignment to Mode cannot delimit insert sessions.
func (in *Input) SetMode(mode string) { in.transition(mode, true) }

// transition changes the editing mode. Leaving insert for normal steps the
// cursor back one rune, as esc does in Vim, unless the change comes from a
// focus move (stepBack false), which is not an esc.
func (in *Input) transition(mode string, stepBack bool) {
	if in.pin != "" {
		mode = in.pin
	}
	if in.ReadOnly && modeIs(in.r, mode, "insert") {
		mode = in.Mode // a viewer never types: the mode stays
	}
	if mode == in.Mode {
		in.resetKeys()
		return
	}
	in.typeOver = nil
	old := in.Mode
	wasInsert, isInsert := modeIs(in.r, old, "insert"), modeIs(in.r, mode, "insert")
	if wasInsert {
		in.finishBlockChange()
	}
	edited := in.groupNode == in.Head
	in.endGroup()
	in.storeMode(mode)
	if editorShape(in.r, mode) != "" && editorShape(in.r, old) == "" {
		in.Anchor = in.Cursor
	}
	in.resetKeys()
	in.goalColumn = -1
	if isInsert {
		in.History[in.Head].Cursor = in.Cursor
		in.groupOpen = true
	} else if wasInsert && modeIs(in.r, mode, "normal") && stepBack {
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

// storeMode writes the editing mode the editor reads and, while the input is
// focused, the global mode it mirrors, clearing any pending mode.
func (in *Input) storeMode(mode string) {
	in.Mode = mode
	if in.focused() {
		in.r.setMode(ModeState{Name: mode})
	}
}

// SetText puts text in the input from the program, read-only or not: a
// viewer refreshed with what it shows (a jq panel following the query).
// The text becomes the undo tree's root (nothing to undo back to); the
// cursor and a visual selection's anchor keep their offsets, clamped into
// the new text as a motion would leave them (in normal mode not past the
// line's last character); an open select range and an insert session end.
// A single-line input keeps the text up to its first line break. The same
// text again changes nothing, so the undo history stays.
func (in *Input) SetText(text string) {
	if !in.Multiline {
		text, _, _ = strings.Cut(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	}
	if string(in.Text) == text && len(in.History) > 0 {
		return
	}
	in.Text = []rune(text)
	in.History, in.Head = []EditNode{{Text: text, Parent: -1}}, 0
	in.groupOpen, in.groupNode, in.goalColumn = false, -1, -1
	in.typeOver, in.blockChange = nil, nil
	anchor := in.Cursor
	in.Cursor = in.Anchor
	in.clampCursor()
	in.Cursor, in.Anchor = anchor, in.Cursor
	in.clampCursor()
	in.History[0].Cursor = in.Cursor
}

func (in *Input) Replace(start, end int, text string) error {
	if start < 0 || end < start || end > len(in.Text) {
		return errors.New("invalid edit range")
	}
	if !in.Multiline && strings.ContainsAny(text, "\r\n") {
		return errors.New("single-line input cannot contain newlines")
	}
	if in.ReadOnly {
		return nil // a viewer's text never changes (Input.ReadOnly)
	}
	inserted := []rune(text)
	in.typeOver = nil
	if slices.Equal(in.Text[start:end], inserted) {
		return nil
	}
	if !in.groupOpen && modeIs(in.r, in.Mode, "insert") {
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
	if in.shape() != "" {
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
	if in.shape() != "" {
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
	in.typeOver = nil
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

// resetKeys drops a pending count, operator or prefix: the global mode
// returns to the editing mode Input.Mode names.
func (in *Input) resetKeys() {
	if in.focused() && (in.r.Mode.Name != in.Mode || !in.r.Mode.Payload.empty()) {
		in.r.setMode(ModeState{Name: in.Mode})
	}
}

// SelectText opens a select range over the runes [start, end) in insert
// mode, as Vim's select mode does over a new name: the first text typed
// (input.insert, a paste) replaces the range and typing continues in
// insert; backspace or delete removes it; a motion, a mode change (esc),
// an undo or any other edit closes it and keeps the text. The range is
// drawn in the visual-selection style. It is a state of the input, not a
// mode: the person is inserting, and the mode stays insert. An empty range
// selects nothing and only enters insert at end.
func (in *Input) SelectText(start, end int) error {
	if start < 0 || end < start || end > len(in.Text) {
		return errors.New("invalid select range")
	}
	if in.ReadOnly {
		return nil // nothing to type over in a viewer
	}
	in.SetMode("insert")
	if in.Mode != "insert" {
		return errors.New("input is pinned to " + in.Mode)
	}
	in.Cursor = end
	in.goalColumn = -1
	in.typeOver = nil
	if end > start {
		in.typeOver = &[2]int{start, end}
	}
	return nil
}

// SelectAll opens a select range over the whole text (SelectText).
func (in *Input) SelectAll() error { return in.SelectText(0, len(in.Text)) }

// SelectedText is the open select range (SelectText), if any.
func (in *Input) SelectedText() (start, end int, ok bool) {
	if in.typeOver == nil {
		return in.Cursor, in.Cursor, false
	}
	return in.typeOver[0], in.typeOver[1], true
}

// Selection is the visual selection's rune range [start, end) while the
// input is in a character or line visual mode (v, V): the runes under
// the anchor and the cursor are both in it, a line selection covers whole
// lines and its last newline. ok is false outside a visual mode and in a
// block selection (visual-block), which is not one range (its snapshot names the
// rows and columns). Hosts read it instead of recomputing it from Anchor
// and Cursor.
func (in *Input) Selection() (start, end int, ok bool) {
	if shape := in.shape(); shape == "" || shape == SelectionBlock {
		return in.Cursor, in.Cursor, false
	}
	start, end = in.selectionRange()
	return start, end, true
}

// typeOverRange is where typed text goes: the open select range, else the
// cursor.
func (in *Input) typeOverRange() (int, int) {
	start, end, _ := in.SelectedText()
	return start, end
}

func (in *Input) typeOverSnapshot() any {
	start, end, ok := in.SelectedText()
	if !ok {
		return nil
	}
	return map[string]any{"start": start, "end": end}
}

func RegisterInputOperations(r *Runtime) {
	r.Modes["normal"] = true
	r.Modes["insert"] = true
	for _, spec := range []struct {
		verb, desc string
		params     map[string]Parameter
		fn         func(*Input, Arguments) error
		edit       bool // changes the text: a no-op at a read-only input
	}{
		{"insert", "Insert literal text at the rune cursor; it replaces an open select range", map[string]Parameter{"text": {Type: "string", Required: true}}, func(in *Input, a Arguments) error {
			in.resetKeys()
			start, end := in.typeOverRange()
			return in.Replace(start, end, StringArg(a, "text", ""))
		}, true},
		{"replace", "Replace a half-open rune range", map[string]Parameter{"start": {Type: "integer", Required: true}, "end": {Type: "integer", Required: true}, "text": {Type: "string", Required: true}}, func(in *Input, a Arguments) error {
			in.resetKeys()
			return in.Replace(IntArg(a, "start", 0), IntArg(a, "end", 0), StringArg(a, "text", ""))
		}, true},
		{"undo", "Undo changes in this input", map[string]Parameter{"steps": {Type: "integer"}}, func(in *Input, a Arguments) error { return in.Undo(IntArg(a, "steps", 1)) }, true},
		{"redo", "Redo changes; branch is a zero-based child index for the first step", map[string]Parameter{"steps": {Type: "integer"}, "branch": {Type: "integer"}}, func(in *Input, a Arguments) error { return in.Redo(IntArg(a, "steps", 1), IntArg(a, "branch", -1)) }, true},
		{"move", "Move by left/right/up/down, Vim word-forward/backward/end, Readline word-next/previous, line-start/end, first-line/last-line", map[string]Parameter{"motion": {Type: "string", Required: true}, "steps": {Type: "integer"}, "line": {Type: "integer"}}, func(in *Input, a Arguments) error {
			return in.move(StringArg(a, "motion", ""), IntArg(a, "steps", 1), IntArg(a, "line", 0))
		}, false},
		{"begin", "Enter insert mode at insert/append/first/end/below/above", map[string]Parameter{"position": {Type: "string"}}, func(in *Input, a Arguments) error { return in.begin(StringArg(a, "position", "insert")) }, true},
		{"delete", "Delete forward/backward runes without crossing a normal-mode line; deletes an open select range", map[string]Parameter{"direction": {Type: "string"}, "steps": {Type: "integer"}}, func(in *Input, a Arguments) error {
			direction, steps := StringArg(a, "direction", "forward"), IntArg(a, "steps", 1)
			if lo, hi, ok := in.SelectedText(); ok {
				in.resetKeys()
				return in.Replace(lo, hi, "")
			}
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
		}, true},
	} {
		fn, edit := spec.fn, spec.edit
		r.Register(Operation{Verb: spec.verb, Noun: "ochazuke.input", Description: spec.desc, Parameters: spec.params, Result: "input state", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			in := r.Input(c.InputID)
			if in == nil {
				return nil, errors.New("no focused input")
			}
			if edit && in.ReadOnly {
				in.resetKeys() // a viewer's edit is a silent no-op
				return in.Snapshot(), nil
			}
			if err := fn(in, a); err != nil {
				return nil, err
			}
			return in.Snapshot(), nil
		}})
	}
	r.Register(Operation{Verb: "cancel", Noun: "ochazuke.input", Description: "Cancel the pending count, operator, prefix or register of the focused instance; a visual selection returns to normal", Result: "mode", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		r.Registers().Active = ""
		if in := r.Input(c.InputID); in != nil {
			if in.shape() != "" {
				in.SetMode("normal")
			} else {
				in.resetKeys()
			}
			r.modeGeneration++
			return in.Mode, nil
		}
		if r.pending() {
			r.leaveMode()
		}
		return r.Mode.Name, nil
	}})
	registerEditorOperations(r)
}
