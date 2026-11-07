package ochazuke

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// editorShape is the selection shape mode holds in an input: char, line
// or block, the shapes the editor draws and edits ("" for any other mode,
// a component's own shape included). The shape is the mode's property
// (DefineSelection), never its name.
func editorShape(r *Runtime, mode string) string {
	switch shape := r.SelectionShape(mode); shape {
	case SelectionChar, SelectionLine, SelectionBlock:
		return shape
	}
	return ""
}

// shape is the input's selection shape, "" outside a visual mode.
func (in *Input) shape() string { return editorShape(in.r, in.Mode) }

// cursorOnChar reports whether the input's cursor sits on a character in
// its mode: a CursorOnChar mode (normal) or a selection mode (visual), so
// motions stop at a line's last character.
func (in *Input) cursorOnChar() bool {
	return modeSpecOf(in.r, in.Mode).CursorOnChar || in.shape() != ""
}

func cellWidth(s string) int              { return ansi.StringWidth(safeText(s)) }
func runeCells(ch rune) int               { return cellWidth(string(ch)) }
func padCells(s string, width int) string { return s + strings.Repeat(" ", max(0, width-cellWidth(s))) }

// cellSlice preserves grapheme clusters; a rectangle slicing a wide cluster
// uses spaces for its intersected cells rather than corrupting the glyph.
func cellSlice(s string, left, right int) string {
	var out strings.Builder
	col := 0
	clusters := uniseg.NewGraphemes(s)
	for clusters.Next() {
		text := clusters.Str()
		w := cellWidth(text)
		if w == 0 {
			if col > left && col <= right {
				out.WriteString(text)
			}
			continue
		}
		lo, hi := max(col, left), min(col+w, right)
		if hi > lo {
			if lo == col && hi == col+w {
				out.WriteString(text)
			} else {
				out.WriteString(strings.Repeat(" ", hi-lo))
			}
		}
		col += w
	}
	return out.String()
}

func runeOffsetAtColumn(text []rune, column, limit int) int {
	clusters := uniseg.NewGraphemes(safeText(string(text)))
	offset, cells := 0, 0
	for clusters.Next() {
		w, count := cellWidth(clusters.Str()), utf8.RuneCountInString(clusters.Str())
		if cells+w > column || offset+count > limit {
			break
		}
		offset += count
		cells += w
	}
	return offset
}
func splitCell(s string, col int) (string, string) {
	width := cellWidth(s)
	return padCells(cellSlice(s, 0, col), col), cellSlice(s, col, width)
}
func (in *Input) rowColumn(cursor int) (int, int) {
	cursor = max(0, min(cursor, len(in.Text)))
	start, _ := in.lineBounds(cursor)
	return strings.Count(string(in.Text[:start]), "\n"), cellWidth(string(in.Text[start:cursor]))
}
func (in *Input) blockBounds() (int, int, int, int) {
	ar, ac := in.rowColumn(in.Anchor)
	cr, cc := in.rowColumn(in.Cursor)
	actualColumn := cc
	if in.goalColumn >= 0 {
		cc = in.goalColumn
	}
	aw, cw := 1, 1
	if in.Anchor < len(in.Text) && in.Text[in.Anchor] != '\n' {
		aw = max(1, runeCells(in.Text[in.Anchor]))
	}
	if cc == actualColumn && in.Cursor < len(in.Text) && in.Text[in.Cursor] != '\n' {
		cw = max(1, runeCells(in.Text[in.Cursor]))
	}
	return min(ar, cr), max(ar, cr), min(ac, cc), max(ac+aw, cc+cw)
}
func (in *Input) selectionRange() (int, int) {
	lo, hi := min(in.Anchor, in.Cursor), max(in.Anchor, in.Cursor)
	lo = max(0, min(lo, len(in.Text)))
	hi = max(0, min(hi, len(in.Text)))
	if in.shape() == SelectionLine {
		lo, _ = in.lineBounds(lo)
		_, hi = in.lineBounds(hi)
		if hi < len(in.Text) {
			hi++
		}
	} else if hi < len(in.Text) {
		hi++
	}
	return lo, hi
}
func (in *Input) selectionSnapshot() any {
	if in.shape() == "" {
		return nil
	}
	if in.shape() == SelectionBlock {
		top, bottom, left, right := in.blockBounds()
		return map[string]any{"shape": "block", "top": top, "bottom": bottom, "left": left, "right": right}
	}
	lo, hi := in.selectionRange()
	shape := "char"
	if in.shape() == SelectionLine {
		shape = "line"
	}
	return map[string]any{"shape": shape, "start": lo, "end": hi}
}
func (in *Input) selectionYank() Yank {
	if in.shape() == SelectionBlock {
		top, bottom, left, right := in.blockBounds()
		lines := strings.Split(string(in.Text), "\n")
		rows := make([]string, 0, bottom-top+1)
		for row := top; row <= bottom; row++ {
			rows = append(rows, padCells(cellSlice(lines[row], left, right), right-left))
		}
		return Yank{Text: strings.Join(rows, "\n"), Shape: "block", Width: right - left}
	}
	lo, hi := in.selectionRange()
	y := Yank{Text: string(in.Text[lo:hi]), Shape: "char"}
	if in.shape() == SelectionLine {
		y.Shape = "line"
		if !strings.HasSuffix(y.Text, "\n") {
			y.Text += "\n"
		}
	}
	return y
}
func (in *Input) finishCursor(cursor int) {
	in.Cursor = cursor
	in.clampCursor()
	in.History[in.Head].Cursor = in.Cursor
}
func (in *Input) removeSelection(change bool) error {
	shape := in.shape()
	lo, hi := in.selectionRange()
	var blockText string
	var block *blockChange
	cursor := lo
	if shape == SelectionBlock {
		top, bottom, left, right := in.blockBounds()
		lines := strings.Split(string(in.Text), "\n")
		for row := top; row <= bottom; row++ {
			// Do not pad ragged lines merely because a rectangle extends beyond them.
			before := cellSlice(lines[row], 0, left)
			after := cellSlice(lines[row], right, cellWidth(lines[row]))
			lines[row] = before + after
		}
		if change {
			prefix, suffix := splitCell(lines[top], left)
			lines[top] = prefix + suffix
			block = &blockChange{Top: top, Bottom: bottom, Column: left, Prefix: prefix, Suffix: suffix}
		}
		cursor = 0
		for row := range top {
			cursor += len([]rune(lines[row])) + 1
		}
		cursor += len([]rune(cellSlice(lines[top], 0, left)))
		blockText = strings.Join(lines, "\n")
	}
	in.SetMode("normal")
	if change {
		in.SetMode("insert")
	}
	if shape == SelectionBlock {
		if err := in.Replace(0, len(in.Text), blockText); err != nil {
			return err
		}
	} else {
		if shape == SelectionLine && !change && hi == len(in.Text) && lo > 0 {
			lo--
			cursor = lo
		}
		replacement := ""
		if shape == SelectionLine && change && hi < len(in.Text) {
			replacement = "\n"
		}
		if err := in.Replace(lo, hi, replacement); err != nil {
			return err
		}
	}
	in.finishCursor(cursor)
	in.blockChange = block
	return nil
}
func (in *Input) pasteYank(y Yank, before, literal bool, steps int) error {
	if steps < 1 {
		return errors.New("steps must be positive")
	}
	if y.Text == "" {
		return nil
	}
	if steps > 1000000 || len(y.Text) > 0 && steps > 16*1024*1024/len(y.Text) {
		return errors.New("paste exceeds size limit")
	}
	if !in.Multiline && (y.Shape == "block" || y.Shape == "line" || strings.ContainsAny(y.Text, "\r\n")) {
		return errors.New("single-line input cannot paste multiline yank")
	}
	if literal || typingMode(in.r, in.Mode) {
		return in.Replace(in.Cursor, in.Cursor, strings.Repeat(y.Text, steps))
	}
	start, end := in.lineBounds(in.Cursor)
	switch y.Shape {
	case "line":
		text := strings.Repeat(y.Text, steps)
		at := start
		if !before {
			at = end
			if end < len(in.Text) {
				at++
			} else {
				text = "\n" + strings.TrimSuffix(text, "\n")
			}
		}
		cursor := at
		if at == end && end == len(in.Text) && !before {
			cursor++
		}
		if err := in.Replace(at, at, text); err != nil {
			return err
		}
		in.finishCursor(cursor)
	case "block":
		row, col := in.rowColumn(in.Cursor)
		if !before && in.Cursor < end {
			col += max(1, runeCells(in.Text[in.Cursor]))
		}
		lines := strings.Split(string(in.Text), "\n")
		rows := strings.Split(y.Text, "\n")
		for len(lines) < row+len(rows) {
			lines = append(lines, "")
		}
		cursor := 0
		for i := range row {
			cursor += len([]rune(lines[i])) + 1
		}
		for i, chunk := range rows {
			left, right := splitCell(lines[row+i], col)
			lines[row+i] = left + strings.Repeat(padCells(chunk, y.Width), steps) + right
			if i == 0 {
				cursor += len([]rune(left))
			}
		}
		if err := in.Replace(0, len(in.Text), strings.Join(lines, "\n")); err != nil {
			return err
		}
		in.finishCursor(cursor)
	default:
		at := in.Cursor
		if !before && at < end {
			at++
		}
		text := strings.Repeat(y.Text, steps)
		if err := in.Replace(at, at, text); err != nil {
			return err
		}
		in.finishCursor(at + len([]rune(text)) - 1)
	}
	return nil
}

// onInput lifts an edit of the focused input into a verb handler that
// answers the input's snapshot.
func onInput(fn func(*Runtime, *Input, Arguments) error) Handler {
	return func(r *Runtime, c Context, a Arguments) (any, error) {
		in := r.Input(c.InputID)
		if in == nil {
			return nil, errors.New("no focused input")
		}
		if err := fn(r, in, a); err != nil {
			return nil, err
		}
		return in.Snapshot(), nil
	}
}

// editorOps is the table registerEditorOperations registers.
func editorOps() []verbSpec {
	return []verbSpec{
		{"select", "input", "Start or toggle character, line, or block visual selection", "editor state", map[string]Parameter{"shape": {Type: "string"}}, onInput(editorSelect), "", InputFocusGuard, Operation{}},
		{"operator", "input", "Yank, delete, or change a selection, whole lines, or an existing motion", "editor state", map[string]Parameter{"operator": {Type: "string", Required: true}, "motion": {Type: "string"}, "steps": {Type: "integer"}, "line": {Type: "integer"}, "register": {Type: "string"}}, onInput(editorOperator), "", InputFocusGuard, Operation{Repeatable: true, HistoryKind: HistoryChange}},
		{"yank", "input", "Yank visual selection or whole current lines", "editor state", map[string]Parameter{"steps": {Type: "integer"}, "register": {Type: "string"}}, editorYank, "", InputFocusGuard, Operation{HistoryKind: HistoryChange}},
		{"paste", "input", "Paste a shared register preserving its shape; literal inserts at cursor", "editor state", map[string]Parameter{"register": {Type: "string"}, "before": {Type: "boolean"}, "literal": {Type: "boolean"}, "steps": {Type: "integer"}}, onInput(editorPaste), "", InputFocusGuard, Operation{Repeatable: true, HistoryKind: HistoryChange}},
		{"kill", "input", "Kill preceding WORD/path component, readline word before or after, or line into shared yank history; kills an open select range", "editor state", map[string]Parameter{"motion": {Type: "string", Required: true, Enum: []string{"word-backward", "word-previous", "word-next", "line-start", "line-end"}}}, onInput(editorKill), "", InputFocusGuard, Operation{Repeatable: true, HistoryKind: HistoryChange}},
		{"select", "register", "Select the register used by the next yank, delete, or paste", "editor state", map[string]Parameter{"name": {Type: "string", Required: true}}, editorSelectRegister, "", "", Operation{}},
	}
}

func registerEditorOperations(r *Runtime) {
	r.registerVerbSpecs(editorOps())
	registerReadOnlyInputs(r)
}

func editorSelect(r *Runtime, in *Input, a Arguments) error {
	mode := ""
	if shape := StringArg(a, "shape", SelectionChar); editorShape(r, r.SelectionMode(shape)) == shape {
		mode = r.SelectionMode(shape)
	}
	if mode == "" {
		return errors.New("unknown selection shape")
	}
	if in.Mode == mode {
		mode = "normal"
	}
	in.SetMode(mode)
	return nil
}

func editorOperator(r *Runtime, in *Input, a Arguments) error {
	op := StringArg(a, "operator", "")
	if in.ReadOnly && editorOperatorChanges(op) {
		// A viewer neither deletes nor yanks what it would have deleted.
		if in.shape() != "" {
			in.SetMode("normal")
		}
		in.resetKeys()
		return nil
	}
	if op != "y" && op != "d" && op != "c" {
		return errors.New("operator must be y, d, or c")
	}
	if _, _, err := registerName(StringArg(a, "register", r.Registers().Active)); err != nil {
		return err
	}
	steps := IntArg(a, "steps", 1)
	if steps < 1 {
		return errors.New("steps must be positive")
	}
	oldCursor := in.Cursor
	if in.shape() == "" {
		if done, err := in.operatorSelect(a, op, steps); done || err != nil {
			return err
		}
	}
	y := in.selectionYank()
	if op == "y" {
		if err := r.storeYank(y, StringArg(a, "register", ""), false); err != nil {
			return err
		}
		if v, _ := r.settingValue(YankReportSetting); v == true {
			r.UI().Info(yankReport(y))
		}
		in.SetMode("normal")
		in.finishCursor(min(in.Anchor, in.Cursor))
		return nil
	}
	previousHead := in.Head
	if err := in.removeSelection(op == "c"); err != nil {
		return err
	}
	if previousHead != in.Head {
		in.History[previousHead].Cursor = oldCursor
	}
	return r.storeYank(y, StringArg(a, "register", ""), true)
}

// operatorSelect selects what an operator without a selection acts on:
// the span of a motion (whole lines for a linewise one; "line", the
// default, is dd's). done is true when the motion went nowhere and the
// operator has nothing to do.
func (in *Input) operatorSelect(a Arguments, op string, steps int) (done bool, err error) {
	oldCursor, oldMode := in.Cursor, in.Mode
	m, ok := motions[StringArg(a, "motion", "line")]
	if !ok {
		return false, errors.New("unknown input motion")
	}
	if op == "c" && m.Change != nil && oldCursor < len(in.Text) && !unicode.IsSpace(in.Text[oldCursor]) {
		m = *m.Change
	}
	line := IntArg(a, "line", 0)
	if line < 0 {
		return false, errors.New("line must be positive or omitted")
	}
	in.resetKeys()
	args := in.motionArgs(m, min(steps, len(in.Text)+1), line)
	args.Operator = true
	destination, moved := m.To(in, args)
	if !moved {
		in.storeMode(oldMode)
		in.resetKeys()
		return true, nil
	}
	if (m.Inclusive || m.Linewise) && args.OnChar {
		destination = ClampOnChar(in.Text, destination)
	}
	in.Cursor = destination
	in.storeMode("visual")
	in.Anchor = oldCursor
	switch {
	case m.Linewise:
		in.storeMode("visual-line")
	case m.Inclusive:
	case destination > oldCursor:
		in.Cursor--
	case destination < oldCursor:
		in.Anchor--
	}
	if destination == oldCursor && !m.Inclusive {
		in.storeMode(oldMode)
		in.resetKeys()
		return true, nil
	}
	return false, nil
}

func editorYank(r *Runtime, c Context, a Arguments) (any, error) {
	args := Arguments{"operator": "y", "steps": IntArg(a, "steps", 1), "register": StringArg(a, "register", "")}
	return r.ExecuteAt(c, Request{Verb: "operator", Noun: "ochazuke.input", Args: args})
}

func editorPaste(r *Runtime, in *Input, a Arguments) error {
	if in.ReadOnly {
		r.Registers().Active = ""
		in.resetKeys()
		return nil
	}
	y, err := r.readYank(StringArg(a, "register", ""))
	if err != nil {
		return err
	}
	before, _ := a["before"].(bool)
	literal, _ := a["literal"].(bool)
	steps := IntArg(a, "steps", 1)
	if steps < 1 {
		return errors.New("steps must be positive")
	}
	if steps > 1000000 || len(y.Text) > 0 && steps > 16*1024*1024/len(y.Text) {
		return errors.New("paste exceeds size limit")
	}
	if y.Text == "" {
		return nil
	}
	if err := in.checkPaste(y, steps); err != nil {
		return err
	}
	if in.shape() != "" {
		err = in.pasteOverSelection(y, literal, steps)
	} else {
		// A paste replaces an open select range, as typing does.
		if lo, hi, ok := in.SelectedText(); ok {
			if err = in.Replace(lo, hi, ""); err != nil {
				return err
			}
			before = true
		}
		err = in.pasteYank(y, before, literal, steps)
	}
	if err == nil {
		r.Registers().Active = ""
		in.resetKeys()
	}
	return err
}

// checkPaste refuses a yank this input cannot take.
func (in *Input) checkPaste(y Yank, steps int) error {
	if !in.Multiline && (y.Shape == "block" || y.Shape == "line" || strings.ContainsAny(y.Text, "\r\n")) {
		return errors.New("single-line input cannot paste multiline yank")
	}
	if in.shape() == SelectionBlock && y.Shape == "char" {
		top, bottom, _, _ := in.blockBounds()
		if len(y.Text) > 0 && steps*(bottom-top+1) > 16*1024*1024/len(y.Text) {
			return errors.New("paste exceeds size limit")
		}
	}
	return nil
}

// pasteOverSelection replaces the selection with the yank: the
// replacement is composed in an isolated input, then committed as one
// atomic edit.
func (in *Input) pasteOverSelection(y Yank, literal bool, steps int) error {
	copy := NewInput(in.ID, string(in.Text), in.Multiline)
	copy.Cursor = in.Cursor
	copy.Anchor = in.Anchor
	copy.Mode = in.Mode
	copy.goalColumn = in.goalColumn
	var err error
	switch {
	case in.shape() == SelectionLine:
		err = in.pasteOverLines(copy, y, steps)
	case in.shape() == SelectionBlock && y.Shape == "char" && !strings.Contains(y.Text, "\n"):
		err = in.pasteOverBlock(copy, y, steps)
	default:
		if err = copy.removeSelection(false); err == nil {
			err = copy.pasteYank(y, true, literal, steps)
		}
	}
	if err != nil {
		return err
	}
	in.SetMode("normal")
	if err = in.Replace(0, len(in.Text), string(copy.Text)); err != nil {
		return err
	}
	in.finishCursor(copy.Cursor)
	return nil
}

func (in *Input) pasteOverLines(copy *Input, y Yank, steps int) error {
	lo, hi := in.selectionRange()
	replacement := strings.Repeat(y.Text, steps)
	if y.Shape != "line" && hi < len(in.Text) {
		replacement += "\n"
	}
	copy.SetMode("normal")
	if err := copy.Replace(lo, hi, replacement); err != nil {
		return err
	}
	copy.finishCursor(lo)
	return nil
}

func (in *Input) pasteOverBlock(copy *Input, y Yank, steps int) error {
	top, bottom, left, right := in.blockBounds()
	lines := strings.Split(string(in.Text), "\n")
	for row := top; row <= bottom; row++ {
		prefix, _ := splitCell(lines[row], left)
		suffix := cellSlice(lines[row], right, cellWidth(lines[row]))
		lines[row] = prefix + strings.Repeat(y.Text, steps) + suffix
	}
	copy.SetMode("normal")
	if err := copy.Replace(0, len(copy.Text), strings.Join(lines, "\n")); err != nil {
		return err
	}
	cursor := 0
	for row := range top {
		cursor += len([]rune(lines[row])) + 1
	}
	cursor += len([]rune(cellSlice(lines[top], 0, left)))
	copy.finishCursor(cursor)
	return nil
}

func editorKill(r *Runtime, in *Input, a Arguments) error {
	if in.ReadOnly {
		in.resetKeys()
		return nil
	}
	lo, hi, err := in.killRange(StringArg(a, "motion", ""))
	if err != nil {
		return err
	}
	// An open select range (SelectText) is what a kill removes, as
	// backspace and delete remove it (UX-RULES R9.6).
	if slo, shi, ok := in.SelectedText(); ok {
		lo, hi = slo, shi
	}
	in.resetKeys()
	if lo == hi {
		return nil
	}
	y := Yank{Text: string(in.Text[lo:hi]), Shape: "char"}
	if err = in.Replace(lo, hi, ""); err != nil {
		return err
	}
	return r.storeYank(y, "", true)
}

func editorSelectRegister(r *Runtime, c Context, a Arguments) (any, error) {
	name := StringArg(a, "name", "")
	if _, _, err := registerName(name); err != nil {
		return nil, err
	}
	r.Registers().Active = name
	return name, nil
}
