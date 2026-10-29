package ochazuke

import (
	"errors"
	"unicode"
)

func (in *Input) lineBounds(cursor int) (int, int) {
	cursor = max(0, min(cursor, len(in.Text)))
	start, end := cursor, cursor
	for start > 0 && in.Text[start-1] != '\n' {
		start--
	}
	for end < len(in.Text) && in.Text[end] != '\n' {
		end++
	}
	return start, end
}

func (in *Input) clampCursor() {
	in.Cursor = max(0, min(in.Cursor, len(in.Text)))
	if in.Mode == "normal" || isVisual(in.Mode) {
		start, end := in.lineBounds(in.Cursor)
		if end > start && in.Cursor == end {
			in.Cursor--
		}
	}
}

func wordClass(ch rune) int {
	if unicode.IsSpace(ch) {
		return 0
	}
	if unicode.IsLetter(ch) || unicode.IsDigit(ch) || unicode.IsMark(ch) || ch == '_' {
		return 1
	}
	return 2
}

func (in *Input) wordForward(cursor int) int {
	if cursor >= len(in.Text) {
		return len(in.Text)
	}
	class := wordClass(in.Text[cursor])
	for cursor < len(in.Text) && wordClass(in.Text[cursor]) == class {
		cursor++
	}
	for cursor < len(in.Text) && wordClass(in.Text[cursor]) == 0 {
		cursor++
	}
	return cursor
}

func (in *Input) wordBackward(cursor int) int {
	if cursor == 0 {
		return 0
	}
	cursor--
	for cursor > 0 && wordClass(in.Text[cursor]) == 0 {
		cursor--
	}
	class := wordClass(in.Text[cursor])
	for cursor > 0 && wordClass(in.Text[cursor-1]) == class {
		cursor--
	}
	return cursor
}

func (in *Input) wordEnd(cursor int) int {
	if cursor >= len(in.Text)-1 {
		return max(0, len(in.Text)-1)
	}
	cursor++
	for cursor < len(in.Text)-1 && wordClass(in.Text[cursor]) == 0 {
		cursor++
	}
	class := wordClass(in.Text[cursor])
	for cursor < len(in.Text)-1 && wordClass(in.Text[cursor+1]) == class {
		cursor++
	}
	return cursor
}

func (in *Input) readlineWordForward(cursor int) int {
	for cursor < len(in.Text) && wordClass(in.Text[cursor]) != 1 {
		cursor++
	}
	for cursor < len(in.Text) && wordClass(in.Text[cursor]) == 1 {
		cursor++
	}
	return cursor
}

func (in *Input) readlineWordBackward(cursor int) int {
	for cursor > 0 && wordClass(in.Text[cursor-1]) != 1 {
		cursor--
	}
	for cursor > 0 && wordClass(in.Text[cursor-1]) == 1 {
		cursor--
	}
	return cursor
}

func (in *Input) move(motion string, steps, line int) error {
	if steps < 1 {
		return errors.New("steps must be positive")
	}
	if line < 0 {
		return errors.New("line must be positive or omitted")
	}
	switch motion {
	case "left", "right", "up", "down", "word-forward", "word-backward", "word-end", "word-next", "word-previous", "line-start", "line-end", "first-line", "last-line":
	default:
		return errors.New("unknown input motion")
	}
	in.resetKeys()
	// A motion closes an open select range and keeps its text; left and
	// right land on its edges, as a text field's arrows do.
	if lo, hi, ok := in.SelectedText(); ok {
		in.typeOver = nil
		switch motion {
		case "left":
			in.Cursor = lo
			steps--
		case "right":
			in.Cursor = hi
			steps--
		}
		if steps == 0 {
			in.clampCursor()
			return nil
		}
	}
	in.clampCursor()
	start, end := in.lineBounds(in.Cursor)
	steps = min(steps, len(in.Text)+1)
	if motion != "up" && motion != "down" {
		in.goalColumn = -1
	}
	switch motion {
	case "left":
		lower := 0
		if in.Mode == "normal" || isVisual(in.Mode) {
			lower = start
		}
		in.Cursor = max(lower, in.Cursor-steps)
	case "right":
		upper := len(in.Text)
		if in.Mode == "normal" || isVisual(in.Mode) {
			upper = max(start, end-1)
		}
		in.Cursor = min(upper, in.Cursor+steps)
	case "up", "down":
		if in.goalColumn < 0 {
			in.goalColumn = cellWidth(string(in.Text[start:in.Cursor]))
		}
		for ; steps > 0; steps-- {
			if motion == "up" {
				if start == 0 {
					break
				}
				start, end = in.lineBounds(start - 1)
			} else {
				if end == len(in.Text) {
					break
				}
				start, end = in.lineBounds(end + 1)
			}
		}
		limit := end
		if in.Mode == "normal" || isVisual(in.Mode) {
			limit = max(start, end-1)
		}
		in.Cursor = start + runeOffsetAtColumn(in.Text[start:end], in.goalColumn, limit-start)
	case "word-forward", "word-backward", "word-end", "word-next", "word-previous":
		for ; steps > 0; steps-- {
			old := in.Cursor
			switch motion {
			case "word-forward":
				in.Cursor = in.wordForward(in.Cursor)
			case "word-backward":
				in.Cursor = in.wordBackward(in.Cursor)
			case "word-end":
				in.Cursor = in.wordEnd(in.Cursor)
			case "word-next":
				in.Cursor = in.readlineWordForward(in.Cursor)
			case "word-previous":
				in.Cursor = in.readlineWordBackward(in.Cursor)
			}
			if old == in.Cursor {
				break
			}
		}
	case "line-start":
		in.Cursor = start
	case "line-end":
		for ; steps > 1 && end < len(in.Text); steps-- {
			start, end = in.lineBounds(end + 1)
		}
		in.Cursor = end
	case "first-line", "last-line":
		if line == 0 && motion == "last-line" {
			in.Cursor = len(in.Text)
			in.Cursor, _ = in.lineBounds(in.Cursor)
		} else {
			line = max(1, line)
			in.Cursor = 0
			for ; line > 1; line-- {
				_, end = in.lineBounds(in.Cursor)
				if end == len(in.Text) {
					break
				}
				in.Cursor = end + 1
			}
		}
	}
	in.clampCursor()
	return nil
}

func (in *Input) begin(position string) error {
	switch position {
	case "insert", "append", "first", "end", "below", "above":
	default:
		return errors.New("unknown insertion position")
	}
	if (position == "below" || position == "above") && !in.Multiline {
		return errors.New("single-line input cannot open a new line")
	}
	in.resetKeys()
	in.SetMode("insert")
	// An explicit begin after undo starts a new insert group even if the mode stayed insert.
	if !in.groupOpen {
		in.groupOpen = true
		in.groupNode = -1
	}
	start, end := in.lineBounds(in.Cursor)
	switch position {
	case "append":
		if in.Cursor < end {
			in.Cursor++
		}
	case "first":
		in.Cursor = start
		for in.Cursor < end && unicode.IsSpace(in.Text[in.Cursor]) {
			in.Cursor++
		}
	case "end":
		in.Cursor = end
	case "below":
		if err := in.Replace(end, end, "\n"); err != nil {
			return err
		}
	case "above":
		if err := in.Replace(start, start, "\n"); err != nil {
			return err
		}
		in.Cursor = start
	}
	in.goalColumn = -1
	in.clampCursor()
	return nil
}

func (in *Input) deleteRunes(direction string, steps int) error {
	if steps < 1 {
		return errors.New("steps must be positive")
	}
	if direction != "forward" && direction != "backward" {
		return errors.New("unknown delete direction")
	}
	in.resetKeys()
	start, end := in.Cursor, in.Cursor
	lower, upper := 0, len(in.Text)
	if in.Mode == "normal" {
		lower, upper = in.lineBounds(in.Cursor)
	}
	steps = min(steps, len(in.Text))
	if direction == "backward" {
		start = max(lower, start-steps)
	} else {
		end = min(upper, end+steps)
	}
	return in.Replace(start, end, "")
}

func (in *Input) killRange(motion string) (int, int, error) {
	start, end := in.Cursor, in.Cursor
	lineStart, lineEnd := in.lineBounds(in.Cursor)
	switch motion {
	case "word-backward":
		for start > 0 && unicode.IsSpace(in.Text[start-1]) {
			start--
		}
		// A slash is a component boundary; punctuation inside a filename is not.
		if start > 0 && in.Text[start-1] == '/' {
			start--
		}
		for start > 0 && !unicode.IsSpace(in.Text[start-1]) && in.Text[start-1] != '/' {
			start--
		}
	case "line-start":
		start = lineStart
	case "line-end":
		end = lineEnd
		if start == end && end < len(in.Text) && in.Multiline {
			end++
		}
	default:
		return 0, 0, errors.New("unknown kill motion")
	}
	return start, end, nil
}
