package ochazuke

import (
	"slices"
	"unicode"
)

// TextBuffer is the text a motion moves over: an Input, or an
// application's own buffer (a terminal's copy mode). A position is a rune
// offset into Runes, from 0 to len(Runes()); lines are split at '\n'.
// The interface is kept small so any text holder can implement it: the
// motions read the text and the lines on screen, nothing else, and never
// write.
type TextBuffer interface {
	// Runes is the buffer's text. The motions only read it.
	Runes() []rune
	// ScreenLines is the first line on screen (0-based) and how many
	// lines show; a count <= 0 means the whole buffer shows. H, M and L
	// (screen-top, screen-middle, screen-bottom) move within it.
	ScreenLines() (first, count int)
}

// MotionArgs is where a motion starts and how it was asked for.
type MotionArgs struct {
	// Pos is the starting position (a rune offset).
	Pos int
	// Steps is the count, 1 or more; motions that take none ignore it.
	Steps int
	// Line is an explicit line number (1-based) for G and gg; 0 is none.
	Line int
	// Goal is the goal column in cells for a KeepsGoal motion.
	Goal int
	// OnChar is a cursor that rests on characters (Vim's normal and visual
	// modes): horizontal motions stay on the line. Off (insert mode) the
	// cursor may stand past a line's last character and flows across
	// line breaks.
	OnChar bool
	// Operator asks for an operator's end rather than a cursor's place: an
	// exclusive motion may then end one past a line's last character.
	Operator bool
}

// Motion is one row of the motion table: how a named motion moves and how
// an operator reads its span. Its name is the motion argument of the
// move and operator verbs (input.move {motion: "word-forward"}).
type Motion struct {
	// Linewise makes an operator act on whole lines (Vim's linewise
	// motions: j, k, G, gg, H, M, L).
	Linewise bool
	// Inclusive makes an operator's span include the character the motion
	// ends on (e, E, $, %); an inclusive or linewise-inclusive motion
	// acts even when it does not move (dd, d$ at the line's end). An
	// exclusive motion that does not move leaves nothing to act on.
	Inclusive bool
	// KeepsGoal keeps the goal column across the motion (j, k): the cursor
	// returns to it on a line long enough.
	KeepsGoal bool
	// SelectEdge is where the motion leaves an open select range
	// (Input.SelectText): -1 on its start, 1 on its end, which counts as
	// one step, as a text field's arrows do; 0 moves from the cursor.
	SelectEdge int
	// Change is the motion the c operator uses in this one's place when
	// the cursor is on a non-blank: Vim's cw and cW change to the end of
	// the word (`:help cw`).
	Change *Motion
	// To is where the motion lands from a.Pos in b, and false when it
	// cannot move at all (% with no bracket): the cursor stays and an
	// operator does nothing. A position past a line's last character is
	// the caller's to clamp when its cursor rests on characters.
	To func(b TextBuffer, a MotionArgs) (int, bool)
}

// MotionTable maps motion names to motions. Being the declaration of the
// motions, a MotionTable literal is not a name table to namecheck.
type MotionTable map[string]Motion

// motions are the text motions every Input answers, by name. Grid and list
// motions are cell motions and live with their components.
var motions = MotionTable{
	"left":               {SelectEdge: -1, To: moveLeft},
	"right":              {SelectEdge: 1, To: moveRight},
	"up":                 {Linewise: true, KeepsGoal: true, To: lineUp},
	"down":               {Linewise: true, KeepsGoal: true, To: lineDown},
	"line":               {Linewise: true, Inclusive: true, To: wholeLines},
	"word-forward":       {To: repeated(wordForwardBy(wordClass)), Change: &Motion{Inclusive: true, To: changeWordEnd(wordClass)}},
	"word-backward":      {To: repeated(wordBackwardBy(wordClass))},
	"word-end":           {Inclusive: true, To: repeated(wordEndBy(wordClass))},
	"WORD-forward":       {To: repeated(wordForwardBy(blankClass)), Change: &Motion{Inclusive: true, To: changeWordEnd(blankClass)}},
	"WORD-backward":      {To: repeated(wordBackwardBy(blankClass))},
	"WORD-end":           {Inclusive: true, To: repeated(wordEndBy(blankClass))},
	"word-next":          {To: repeated(readlineForward)},
	"word-previous":      {To: repeated(readlineBackward)},
	"line-start":         {To: lineStart},
	"line-end":           {Inclusive: true, To: lineEnd},
	"first-line":         {Linewise: true, To: toFirstLine},
	"last-line":          {Linewise: true, To: toLastLine},
	"paragraph-forward":  {To: paragraphForward},
	"paragraph-backward": {To: paragraphBackward},
	"screen-top":         {Linewise: true, Inclusive: true, To: screenTop},
	"screen-middle":      {Linewise: true, Inclusive: true, To: screenMiddle},
	"screen-bottom":      {Linewise: true, Inclusive: true, To: screenBottom},
	"match":              {Inclusive: true, To: matchBracket},
}

// killMotions are the spans input.kill removes, by its motion argument:
// readline's and the shell's kills, which differ from the Vim motions of
// the same names (ctrl+w kills a WORD or a path component).
var killMotions = MotionTable{
	"word-backward": {To: pathComponentBackward},
	"word-previous": {To: repeated(readlineBackward)},
	"word-next":     {To: repeated(readlineForward)},
	"line-start":    {To: lineStart},
	"line-end":      {To: killLineEnd},
}

// TextMotion is the motion named name, for an application's own
// TextBuffer (its copy mode): move with To, then clamp and read the span
// by the row's flags as Input does.
func TextMotion(name string) (Motion, bool) {
	m, ok := motions[name]
	return m, ok
}

// MotionNames are the names of the text motions, sorted.
func MotionNames() []string {
	names := make([]string, 0, len(motions))
	for name := range motions {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// TextLineBounds is the line holding pos in text: its first position and
// the position of its line break (or the text's end).
func TextLineBounds(text []rune, pos int) (start, end int) {
	pos = max(0, min(pos, len(text)))
	start, end = pos, pos
	for start > 0 && text[start-1] != '\n' {
		start--
	}
	for end < len(text) && text[end] != '\n' {
		end++
	}
	return start, end
}

// ClampOnChar is pos moved back off a line's end onto its last character,
// where a cursor that rests on characters stands; an empty line keeps it.
func ClampOnChar(text []rune, pos int) int {
	pos = max(0, min(pos, len(text)))
	start, end := TextLineBounds(text, pos)
	if end > start && pos == end {
		return pos - 1
	}
	return pos
}

// lineNumber is the 0-based line holding pos.
func lineNumber(text []rune, pos int) int {
	n := 0
	for _, ch := range text[:max(0, min(pos, len(text)))] {
		if ch == '\n' {
			n++
		}
	}
	return n
}

// lineCount is how many lines text holds (an empty text has one).
func lineCount(text []rune) int { return lineNumber(text, len(text)) + 1 }

// lineStartAt is the first position of 0-based line n, clamped to the
// first and last lines.
func lineStartAt(text []rune, n int) int {
	pos := 0
	for ; n > 0; n-- {
		_, end := TextLineBounds(text, pos)
		if end == len(text) {
			break
		}
		pos = end + 1
	}
	return pos
}

// firstNonBlank is the first non-blank of the line starting at start, or
// its end.
func firstNonBlank(text []rune, start int) int {
	_, end := TextLineBounds(text, start)
	for start < end && unicode.IsSpace(text[start]) {
		start++
	}
	return start
}

func moveLeft(b TextBuffer, a MotionArgs) (int, bool) {
	lower := 0
	if a.OnChar {
		lower, _ = TextLineBounds(b.Runes(), a.Pos)
	}
	return max(lower, a.Pos-a.Steps), true
}

func moveRight(b TextBuffer, a MotionArgs) (int, bool) {
	text := b.Runes()
	upper := len(text)
	if a.OnChar || a.Operator {
		_, upper = TextLineBounds(text, a.Pos)
	}
	return min(upper, a.Pos+a.Steps), true
}

// lineUp and lineDown land on the goal column of the line steps away, or
// the nearest line there is.
func lineUp(b TextBuffer, a MotionArgs) (int, bool)   { return lineBy(b, a, -1), true }
func lineDown(b TextBuffer, a MotionArgs) (int, bool) { return lineBy(b, a, 1), true }

func lineBy(b TextBuffer, a MotionArgs, dir int) int {
	text := b.Runes()
	start, end := TextLineBounds(text, a.Pos)
	for steps := a.Steps; steps > 0; steps-- {
		if dir < 0 {
			if start == 0 {
				break
			}
			start, end = TextLineBounds(text, start-1)
		} else {
			if end == len(text) {
				break
			}
			start, end = TextLineBounds(text, end+1)
		}
	}
	return start + runeOffsetAtColumn(text[start:end], a.Goal, end-start)
}

// wholeLines is the motion of dd and yy: the line steps-1 below, the
// span reaching through it.
func wholeLines(b TextBuffer, a MotionArgs) (int, bool) {
	text := b.Runes()
	pos := a.Pos
	for n := 1; n < a.Steps; n++ {
		_, end := TextLineBounds(text, pos)
		if end == len(text) {
			break
		}
		pos = end + 1
	}
	return pos, true
}

// repeated runs a one-step motion a.Steps times, stopping where it stops
// moving.
func repeated(step func(text []rune, pos int) int) func(TextBuffer, MotionArgs) (int, bool) {
	return func(b TextBuffer, a MotionArgs) (int, bool) {
		text := b.Runes()
		pos := a.Pos
		for range min(a.Steps, len(text)+1) {
			next := step(text, pos)
			if next == pos {
				break
			}
			pos = next
		}
		return pos, true
	}
}

// wordClass is Vim's word classes: 0 blank, 1 a keyword character, 2
// other punctuation.
func wordClass(ch rune) int {
	if unicode.IsSpace(ch) {
		return 0
	}
	if unicode.IsLetter(ch) || unicode.IsDigit(ch) || unicode.IsMark(ch) || ch == '_' {
		return 1
	}
	return 2
}

// blankClass is a WORD's classes: 0 blank, 1 anything else.
func blankClass(ch rune) int {
	if unicode.IsSpace(ch) {
		return 0
	}
	return 1
}

// wordForwardBy is w (W with blankClass): the start of the next word.
func wordForwardBy(class func(rune) int) func([]rune, int) int {
	return func(text []rune, pos int) int {
		if pos >= len(text) {
			return len(text)
		}
		c := class(text[pos])
		for pos < len(text) && class(text[pos]) == c {
			pos++
		}
		for pos < len(text) && class(text[pos]) == 0 {
			pos++
		}
		return pos
	}
}

// wordBackwardBy is b (B): the start of this or the previous word.
func wordBackwardBy(class func(rune) int) func([]rune, int) int {
	return func(text []rune, pos int) int {
		if pos == 0 {
			return 0
		}
		pos--
		for pos > 0 && class(text[pos]) == 0 {
			pos--
		}
		c := class(text[pos])
		for pos > 0 && class(text[pos-1]) == c {
			pos--
		}
		return pos
	}
}

// wordEndBy is e (E): the end of this or the next word.
func wordEndBy(class func(rune) int) func([]rune, int) int {
	return func(text []rune, pos int) int {
		if pos >= len(text)-1 {
			return max(0, len(text)-1)
		}
		pos++
		for pos < len(text)-1 && class(text[pos]) == 0 {
			pos++
		}
		c := class(text[pos])
		for pos < len(text)-1 && class(text[pos+1]) == c {
			pos++
		}
		return pos
	}
}

// changeWordEnd is what cw (cW) changes to: the end of the word under the
// cursor, then steps-1 word ends on.
func changeWordEnd(class func(rune) int) func(TextBuffer, MotionArgs) (int, bool) {
	end := wordEndBy(class)
	return func(b TextBuffer, a MotionArgs) (int, bool) {
		text := b.Runes()
		pos := a.Pos
		if pos >= len(text) {
			return pos, true
		}
		c := class(text[pos])
		for pos+1 < len(text) && class(text[pos+1]) == c {
			pos++
		}
		for n := 1; n < min(a.Steps, len(text)+1); n++ {
			pos = end(text, pos)
		}
		return pos, true
	}
}

// readlineForward is alt+f: on to the end of the next word.
func readlineForward(text []rune, pos int) int {
	for pos < len(text) && wordClass(text[pos]) != 1 {
		pos++
	}
	for pos < len(text) && wordClass(text[pos]) == 1 {
		pos++
	}
	return pos
}

// readlineBackward is alt+b: back to the start of the word before.
func readlineBackward(text []rune, pos int) int {
	for pos > 0 && wordClass(text[pos-1]) != 1 {
		pos--
	}
	for pos > 0 && wordClass(text[pos-1]) == 1 {
		pos--
	}
	return pos
}

// pathComponentBackward is the shell's ctrl+w: back over blanks, then a
// WORD or a path component (a slash is a boundary; punctuation inside a
// filename is not).
func pathComponentBackward(b TextBuffer, a MotionArgs) (int, bool) {
	text := b.Runes()
	pos := a.Pos
	for pos > 0 && unicode.IsSpace(text[pos-1]) {
		pos--
	}
	if pos > 0 && text[pos-1] == '/' {
		pos--
	}
	for pos > 0 && !unicode.IsSpace(text[pos-1]) && text[pos-1] != '/' {
		pos--
	}
	return pos, true
}

func lineStart(b TextBuffer, a MotionArgs) (int, bool) {
	start, _ := TextLineBounds(b.Runes(), a.Pos)
	return start, true
}

// lineEnd is $: the end of the line steps-1 below.
func lineEnd(b TextBuffer, a MotionArgs) (int, bool) {
	text := b.Runes()
	_, end := TextLineBounds(text, a.Pos)
	for steps := a.Steps; steps > 1 && end < len(text); steps-- {
		_, end = TextLineBounds(text, end+1)
	}
	return end, true
}

// killLineEnd is ctrl+k: to the line's end, or the line break itself when
// the cursor is already there.
func killLineEnd(b TextBuffer, a MotionArgs) (int, bool) {
	text := b.Runes()
	_, end := TextLineBounds(text, a.Pos)
	if a.Pos == end && end < len(text) {
		end++
	}
	return end, true
}

// toFirstLine is gg (line 1, or the counted line); toLastLine is G (the last
// line, or the counted one). Both land on the line's start.
func toFirstLine(b TextBuffer, a MotionArgs) (int, bool) {
	return lineStartAt(b.Runes(), max(1, a.Line)-1), true
}

func toLastLine(b TextBuffer, a MotionArgs) (int, bool) {
	text := b.Runes()
	if a.Line == 0 {
		start, _ := TextLineBounds(text, len(text))
		return start, true
	}
	return lineStartAt(text, a.Line-1), true
}

// emptyLine reports whether the line starting at start has no characters:
// Vim's paragraph boundary (a line of blanks is not one).
func emptyLine(text []rune, start int) bool { return start == len(text) || text[start] == '\n' }

// paragraphForward is }: the next empty line after a paragraph, or the end
// of the last line. As an operator's end from inside a line, it stops at
// the end of the line before the empty one (Vim's `:help exclusive`).
func paragraphForward(b TextBuffer, a MotionArgs) (int, bool) {
	text := b.Runes()
	start, _ := TextLineBounds(text, a.Pos)
	for range a.Steps {
		_, end := TextLineBounds(text, start)
		for end < len(text) && emptyLine(text, start) {
			start, end = end+1, endOf(text, end+1)
		}
		for end < len(text) && !emptyLine(text, start) {
			start, end = end+1, endOf(text, end+1)
		}
		if !emptyLine(text, start) {
			start = end
			break
		}
	}
	if a.Operator && start > 0 && text[start-1] == '\n' && a.Pos > firstNonBlank(text, lineStartOf(text, a.Pos)) {
		start--
	}
	return start, true
}

// paragraphBackward is {: the previous empty line before a paragraph, or
// the start of the first line.
func paragraphBackward(b TextBuffer, a MotionArgs) (int, bool) {
	text := b.Runes()
	start, _ := TextLineBounds(text, a.Pos)
	for range a.Steps {
		for start > 0 && emptyLine(text, start) {
			start = lineStartOf(text, start-1)
		}
		for start > 0 && !emptyLine(text, start) {
			start = lineStartOf(text, start-1)
		}
		if start == 0 {
			break
		}
	}
	return start, true
}

func endOf(text []rune, pos int) int {
	_, end := TextLineBounds(text, pos)
	return end
}

func lineStartOf(text []rune, pos int) int {
	start, _ := TextLineBounds(text, pos)
	return start
}

// screenSpan is the first and last line on screen, clamped to the text.
func screenSpan(b TextBuffer) (first, last int) {
	n := lineCount(b.Runes())
	first, count := b.ScreenLines()
	if count <= 0 {
		return 0, n - 1
	}
	first = max(0, min(first, n-1))
	return first, min(first+count, n) - 1
}

// screenTop is H: the first non-blank of the steps-th line on screen.
func screenTop(b TextBuffer, a MotionArgs) (int, bool) {
	first, last := screenSpan(b)
	return firstNonBlank(b.Runes(), lineStartAt(b.Runes(), min(last, first+a.Steps-1))), true
}

// screenMiddle is M: the first non-blank of the middle line on screen.
func screenMiddle(b TextBuffer, a MotionArgs) (int, bool) {
	first, last := screenSpan(b)
	return firstNonBlank(b.Runes(), lineStartAt(b.Runes(), first+(last-first)/2)), true
}

// screenBottom is L: the first non-blank of the steps-th line from the
// bottom of the screen.
func screenBottom(b TextBuffer, a MotionArgs) (int, bool) {
	first, last := screenSpan(b)
	return firstNonBlank(b.Runes(), lineStartAt(b.Runes(), max(first, last-a.Steps+1))), true
}

// bracketPairs are the brackets % matches.
var bracketPairs = map[rune]rune{'(': ')', '[': ']', '{': '}', ')': '(', ']': '[', '}': '{'}

// matchBracket is %: the first bracket at or after the cursor on its line,
// then the one that matches it, across lines (nesting counts brackets of
// the same kind). No bracket, or none matching, fails.
func matchBracket(b TextBuffer, a MotionArgs) (int, bool) {
	text := b.Runes()
	_, end := TextLineBounds(text, a.Pos)
	pos := max(0, a.Pos)
	for pos < end && bracketPairs[text[pos]] == 0 {
		pos++
	}
	if pos >= end {
		return a.Pos, false
	}
	open, want := text[pos], bracketPairs[text[pos]]
	dir := 1
	if open == ')' || open == ']' || open == '}' {
		dir = -1
	}
	depth := 0
	for q := pos; q >= 0 && q < len(text); q += dir {
		switch text[q] {
		case open:
			depth++
		case want:
			depth--
			if depth == 0 {
				return q, true
			}
		}
	}
	return a.Pos, false
}
