package ochazuke

import (
	"errors"
	"unicode"
)

func (in *Input) lineBounds(cursor int) (int, int) { return TextLineBounds(in.Text, cursor) }

// Runes is the input's text (TextBuffer).
func (in *Input) Runes() []rune { return in.Text }

// ScreenLines is the lines the input's last draw showed (TextBuffer): the
// window InputBlock recorded on its placement (Placement.Screen); none
// before a draw, which reads as the whole text.
func (in *Input) ScreenLines() (first, count int) {
	s := in.r.drawnScreen(in.ID)
	return s.First, s.Count
}

func (in *Input) clampCursor() {
	in.Cursor = max(0, min(in.Cursor, len(in.Text)))
	if in.cursorOnChar() {
		start, end := in.lineBounds(in.Cursor)
		if end > start && in.Cursor == end {
			in.Cursor--
		}
	}
}

// move runs the motion named name from the cursor: steps times, or to
// line for G and gg.
func (in *Input) move(name string, steps, line int) error {
	m, ok := motions[name]
	if !ok {
		return errors.New("unknown input motion")
	}
	return in.moveBy(m, steps, line)
}

// moveBy moves the cursor by m. A motion closes an open select range and
// keeps its text; a motion with a SelectEdge lands on its edge first.
func (in *Input) moveBy(m Motion, steps, line int) error {
	if steps < 1 {
		return errors.New("steps must be positive")
	}
	if line < 0 {
		return errors.New("line must be positive or omitted")
	}
	in.resetKeys()
	if lo, hi, ok := in.SelectedText(); ok {
		in.typeOver = nil
		if m.SelectEdge != 0 {
			in.Cursor = hi
			if m.SelectEdge < 0 {
				in.Cursor = lo
			}
			steps--
		}
		if steps == 0 {
			in.clampCursor()
			return nil
		}
	}
	in.clampCursor()
	if pos, ok := m.To(in, in.motionArgs(m, min(steps, len(in.Text)+1), line)); ok {
		in.Cursor = pos
	}
	in.clampCursor()
	return nil
}

// motionArgs are m's arguments from the cursor. They set the goal column
// a KeepsGoal motion keeps and clear it for the others.
func (in *Input) motionArgs(m Motion, steps, line int) MotionArgs {
	if !m.KeepsGoal {
		in.goalColumn = -1
	} else if in.goalColumn < 0 {
		start, _ := in.lineBounds(in.Cursor)
		in.goalColumn = cellWidth(string(in.Text[start:in.Cursor]))
	}
	return MotionArgs{Pos: in.Cursor, Steps: steps, Line: line, Goal: in.goalColumn, OnChar: in.cursorOnChar()}
}

func (in *Input) begin(position string) error {
	switch position {
	case "insert", "append", "first", "end", "below", "above":
	default:
		return errors.New("unknown insertion position")
	}
	if (position == "below" || position == "above") && !in.Multiline {
		// An input never refuses: opening a line grows it, or, with the
		// setting off, does nothing (no error).
		if !in.newlinesGrow() {
			return nil
		}
		in.Multiline = true
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
	if modeSpecOf(in.r, in.Mode).CursorOnChar {
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

// killRange is the span input.kill removes for its motion argument.
func (in *Input) killRange(name string) (int, int, error) {
	m, ok := killMotions[name]
	if !ok {
		return 0, 0, errors.New("unknown kill motion")
	}
	to, _ := m.To(in, MotionArgs{Pos: in.Cursor, Steps: 1})
	return min(in.Cursor, to), max(in.Cursor, to), nil
}
