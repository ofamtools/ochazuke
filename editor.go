package ochazuke

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

func isVisual(mode string) bool {
	return mode == "visual" || mode == "visual-line" || mode == "visual-block"
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
	if in.Mode == "visual-line" {
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
	if !isVisual(in.Mode) {
		return nil
	}
	if in.Mode == "visual-block" {
		top, bottom, left, right := in.blockBounds()
		return map[string]any{"shape": "block", "top": top, "bottom": bottom, "left": left, "right": right}
	}
	lo, hi := in.selectionRange()
	shape := "char"
	if in.Mode == "visual-line" {
		shape = "line"
	}
	return map[string]any{"shape": shape, "start": lo, "end": hi}
}
func (in *Input) selectionYank() Yank {
	if in.Mode == "visual-block" {
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
	if in.Mode == "visual-line" {
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
	mode := in.Mode
	lo, hi := in.selectionRange()
	var blockText string
	var block *blockChange
	cursor := lo
	if mode == "visual-block" {
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
	if mode == "visual-block" {
		if err := in.Replace(0, len(in.Text), blockText); err != nil {
			return err
		}
	} else {
		if mode == "visual-line" && !change && hi == len(in.Text) && lo > 0 {
			lo--
			cursor = lo
		}
		replacement := ""
		if mode == "visual-line" && change && hi < len(in.Text) {
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
	if literal || in.Mode == "insert" {
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
func registerEditorOperations(r *Runtime) {
	for _, mode := range []string{"visual", "visual-line", "visual-block"} {
		r.Modes[mode] = true
	}
	register := func(verb, noun, description string, params map[string]Parameter, fn Handler) {
		r.Register(Operation{Verb: verb, Noun: noun, Description: description, Parameters: params, Result: "editor state", Run: fn})
	}
	input := func(fn func(*Runtime, *Input, Arguments) error) Handler {
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
	register("select", "input", "Start or toggle character, line, or block visual selection", map[string]Parameter{"shape": {Type: "string"}}, input(func(r *Runtime, in *Input, a Arguments) error {
		mode := map[string]string{"char": "visual", "line": "visual-line", "block": "visual-block"}[StringArg(a, "shape", "char")]
		if mode == "" {
			return errors.New("unknown selection shape")
		}
		if in.Mode == mode {
			mode = "normal"
		}
		in.SetMode(mode)
		return nil
	}))
	register("operator", "input", "Yank, delete, or change a selection, whole lines, or an existing motion", map[string]Parameter{"operator": {Type: "string", Required: true}, "motion": {Type: "string"}, "steps": {Type: "integer"}, "line": {Type: "integer"}, "register": {Type: "string"}}, input(func(r *Runtime, in *Input, a Arguments) error {
		op := StringArg(a, "operator", "")
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
		oldCursor, oldMode := in.Cursor, in.Mode
		if !isVisual(in.Mode) {
			motion := StringArg(a, "motion", "line")
			changeWord := op == "c" && motion == "word-forward" && oldCursor < len(in.Text) && wordClass(in.Text[oldCursor]) != 0
			if changeWord {
				motion = "word-end"
			}
			if motion == "line" {
				in.SetMode("visual-line")
				for n := 1; n < steps; n++ {
					_, end := in.lineBounds(in.Cursor)
					if end == len(in.Text) {
						break
					}
					in.Cursor = end + 1
				}
			}
			if motion != "line" {
				if err := in.move(motion, steps, IntArg(a, "line", 0)); err != nil {
					return err
				}
				destination := in.Cursor
				if changeWord {
					destination = oldCursor
					class := wordClass(in.Text[destination])
					for destination+1 < len(in.Text) && wordClass(in.Text[destination+1]) == class {
						destination++
					}
					for n := 1; n < min(steps, len(in.Text)+1); n++ {
						destination = in.wordEnd(destination)
					}
				}
				// Operator endpoints are half-open: unlike a normal cursor,
				// w/l may reach the end of the final line.
				switch motion {
				case "right":
					_, end := in.lineBounds(oldCursor)
					destination = min(end, oldCursor+min(steps, len(in.Text)))
				case "word-forward", "word-next":
					destination = oldCursor
					for range min(steps, len(in.Text)+1) {
						next := in.wordForward(destination)
						if motion == "word-next" {
							next = in.readlineWordForward(destination)
						}
						if next == destination {
							break
						}
						destination = next
					}
				}
				in.Cursor = destination
				in.storeMode("visual")
				in.Anchor = oldCursor
				if motion == "up" || motion == "down" || motion == "first-line" || motion == "last-line" {
					in.storeMode("visual-line")
				} else if destination > oldCursor && motion != "word-end" && motion != "line-end" {
					in.Cursor--
				} else if destination < oldCursor {
					in.Anchor--
				}
				if destination == oldCursor && motion != "word-end" && motion != "line-end" {
					in.storeMode(oldMode)
					in.resetKeys()
					return nil
				}
			}
		}
		y := in.selectionYank()
		if op == "y" {
			if err := r.storeYank(y, StringArg(a, "register", ""), false); err != nil {
				return err
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
	}))
	register("yank", "input", "Yank visual selection or whole current lines", map[string]Parameter{"steps": {Type: "integer"}, "register": {Type: "string"}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		args := Arguments{"operator": "y", "steps": IntArg(a, "steps", 1), "register": StringArg(a, "register", "")}
		return r.ExecuteAt(c, Request{Verb: "operator", Noun: "app.input", Args: args})
	})
	register("paste", "input", "Paste a shared register preserving its shape; literal inserts at cursor", map[string]Parameter{"register": {Type: "string"}, "before": {Type: "boolean"}, "literal": {Type: "boolean"}, "steps": {Type: "integer"}}, input(func(r *Runtime, in *Input, a Arguments) error {
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
		if !in.Multiline && (y.Shape == "block" || y.Shape == "line" || strings.ContainsAny(y.Text, "\r\n")) {
			return errors.New("single-line input cannot paste multiline yank")
		}
		if in.Mode == "visual-block" && y.Shape == "char" {
			top, bottom, _, _ := in.blockBounds()
			if len(y.Text) > 0 && steps*(bottom-top+1) > 16*1024*1024/len(y.Text) {
				return errors.New("paste exceeds size limit")
			}
		}
		if isVisual(in.Mode) {
			// Compose replacement in an isolated input, then commit one atomic edit.
			copy := NewInput(in.ID, string(in.Text), in.Multiline)
			copy.Cursor = in.Cursor
			copy.Anchor = in.Anchor
			copy.Mode = in.Mode
			copy.goalColumn = in.goalColumn
			if in.Mode == "visual-line" {
				lo, hi := in.selectionRange()
				replacement := strings.Repeat(y.Text, steps)
				if y.Shape != "line" && hi < len(in.Text) {
					replacement += "\n"
				}
				copy.SetMode("normal")
				if err = copy.Replace(lo, hi, replacement); err != nil {
					return err
				}
				copy.finishCursor(lo)
			} else if in.Mode == "visual-block" && y.Shape == "char" && !strings.Contains(y.Text, "\n") {
				top, bottom, left, right := in.blockBounds()
				lines := strings.Split(string(in.Text), "\n")
				for row := top; row <= bottom; row++ {
					prefix, _ := splitCell(lines[row], left)
					suffix := cellSlice(lines[row], right, cellWidth(lines[row]))
					lines[row] = prefix + strings.Repeat(y.Text, steps) + suffix
				}
				copy.SetMode("normal")
				if err = copy.Replace(0, len(copy.Text), strings.Join(lines, "\n")); err != nil {
					return err
				}
				cursor := 0
				for row := range top {
					cursor += len([]rune(lines[row])) + 1
				}
				cursor += len([]rune(cellSlice(lines[top], 0, left)))
				copy.finishCursor(cursor)
			} else {
				if err = copy.removeSelection(false); err != nil {
					return err
				}
				if err = copy.pasteYank(y, true, literal, steps); err != nil {
					return err
				}
			}
			in.SetMode("normal")
			if err = in.Replace(0, len(in.Text), string(copy.Text)); err != nil {
				return err
			}
			in.finishCursor(copy.Cursor)
		} else {
			err = in.pasteYank(y, before, literal, steps)
		}
		if err == nil {
			r.Registers().Active = ""
			in.resetKeys()
		}
		return err
	}))
	register("kill", "input", "Kill preceding WORD/path component or line into shared yank history", map[string]Parameter{"motion": {Type: "string", Required: true}}, input(func(r *Runtime, in *Input, a Arguments) error {
		lo, hi, err := in.killRange(StringArg(a, "motion", ""))
		if err != nil {
			return err
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
	}))
	register("select", "register", "Select the register used by the next yank, delete, or paste", map[string]Parameter{"name": {Type: "string", Required: true}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		name := StringArg(a, "name", "")
		if _, _, err := registerName(name); err != nil {
			return nil, err
		}
		r.Registers().Active = name
		return name, nil
	})
	register("set", "register", "Set a text register; uppercase appends, underscore discards", map[string]Parameter{"name": {Type: "string", Required: true}, "text": {Type: "string", Required: true}, "shape": {Type: "string"}, "width": {Type: "integer"}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		shape := StringArg(a, "shape", "char")
		if shape != "char" && shape != "line" && shape != "block" {
			return nil, errors.New("unknown yank shape")
		}
		text := StringArg(a, "text", "")
		width := IntArg(a, "width", 0)
		if width < 0 {
			return nil, errors.New("width must be nonnegative")
		}
		if shape == "line" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		if shape == "block" {
			for _, line := range strings.Split(text, "\n") {
				width = max(width, cellWidth(line))
			}
		}
		err := r.storeYank(Yank{Text: text, Shape: shape, Width: width}, StringArg(a, "name", ""), false)
		return r.Registers, err
	})
	register("get", "register", "Inspect a shared register", map[string]Parameter{"name": {Type: "string"}}, func(r *Runtime, c Context, a Arguments) (any, error) {
		key, _, err := registerName(StringArg(a, "name", ""))
		return r.Registers().Named[key], err
	})
}
