package input

import (
	"slices"
	"strings"
	"unicode"

	"ofam.dev/ochazuke/core"
)

// Readline command IDs understood by input.Model (in insert mode), input.Line,
// form.Model, picker.Model, pathpicker.Model and commandline.Model.
const (
	CmdLineStart       = "input.line.start"        // ctrl+a, home
	CmdLineEnd         = "input.line.end"          // ctrl+e, end
	CmdCharLeft        = "input.char.left"         // ctrl+b, left
	CmdCharRight       = "input.char.right"        // ctrl+f, right
	CmdWordLeft        = "input.word.left"         // alt+b, ctrl+left
	CmdWordRight       = "input.word.right"        // alt+f, ctrl+right
	CmdDeleteChar      = "input.delete.char"       // ctrl+d, delete
	CmdKillWordBack    = "input.kill.word.back"    // ctrl+w, alt+backspace
	CmdKillWordForward = "input.kill.word.forward" // alt+d
	CmdKillLineBack    = "input.kill.line.back"    // ctrl+u
	CmdKillLineForward = "input.kill.line.forward" // ctrl+k
	CmdTranspose       = "input.transpose"         // ctrl+t
	CmdYankKill        = "input.yank.kill"         // ctrl+y
)

var readlineCommandIDs = []string{
	CmdLineStart,
	CmdLineEnd,
	CmdCharLeft,
	CmdCharRight,
	CmdWordLeft,
	CmdWordRight,
	CmdBackspace,
	CmdDeleteChar,
	CmdKillWordBack,
	CmdKillWordForward,
	CmdKillLineBack,
	CmdKillLineForward,
	CmdTranspose,
	CmdYankKill,
}

// ReadlineCommandIDs returns every Readline editing and cursor-motion command ID.
func ReadlineCommandIDs() []string { return slices.Clone(readlineCommandIDs) }

// IsReadlineCommand reports whether id is one of the standard Readline input commands.
func IsReadlineCommand(id string) bool { return slices.Contains(readlineCommandIDs, id) }

// ReadlineSpecs returns the CommandSpecs for all Readline input actions so hosts
// can register them in their command registry and expose them in inspectors and
// binding editors.
func ReadlineSpecs() []core.CommandSpec {
	tags := []string{"input", "readline"}
	return []core.CommandSpec{
		{ID: CmdLineStart, Description: "Move cursor to the start of the line", Tags: tags},
		{ID: CmdLineEnd, Description: "Move cursor to the end of the line", Tags: tags},
		{ID: CmdCharLeft, Description: "Move cursor back one character", Tags: tags},
		{ID: CmdCharRight, Description: "Move cursor forward one character", Tags: tags},
		{ID: CmdWordLeft, Description: "Move cursor back one word", Tags: tags},
		{ID: CmdWordRight, Description: "Move cursor forward one word", Tags: tags},
		{ID: CmdBackspace, Description: "Delete the character before the cursor", Tags: tags},
		{ID: CmdDeleteChar, Description: "Delete the character under the cursor", Tags: tags},
		{ID: CmdKillWordBack, Description: "Delete the word before the cursor into the kill ring", Tags: tags},
		{ID: CmdKillWordForward, Description: "Delete the word after the cursor into the kill ring", Tags: tags},
		{ID: CmdKillLineBack, Description: "Delete from the cursor to the start of the line", Tags: tags},
		{ID: CmdKillLineForward, Description: "Delete from the cursor to the end of the line", Tags: tags},
		{ID: CmdTranspose, Description: "Transpose the two characters before the cursor", Tags: tags},
		{ID: CmdYankKill, Description: "Insert the last killed text at the cursor", Tags: tags},
	}
}

// ReadlineBindings returns default Readline key bindings for mode and scope.
// Secondary aliases (arrow keys, home/end, delete, ctrl+h) are marked Hidden so
// footers stay uncluttered while remaining inspectable and rebindable.
func ReadlineBindings(mode, scope string) []core.Binding {
	b := func(key, cmd, hint string, hidden bool) core.Binding {
		return core.Binding{Mode: mode, Scope: scope, Keys: []string{key}, Command: cmd, Hint: hint, Hidden: hidden}
	}
	return []core.Binding{
		b("ctrl+a", CmdLineStart, "line start", false),
		b("home", CmdLineStart, "line start", true),
		b("ctrl+e", CmdLineEnd, "line end", false),
		b("end", CmdLineEnd, "line end", true),
		b("ctrl+b", CmdCharLeft, "char left", true),
		b("left", CmdCharLeft, "char left", true),
		b("ctrl+f", CmdCharRight, "char right", true),
		b("right", CmdCharRight, "char right", true),
		b("alt+b", CmdWordLeft, "word left", false),
		b("ctrl+left", CmdWordLeft, "word left", true),
		b("alt+f", CmdWordRight, "word right", false),
		b("ctrl+right", CmdWordRight, "word right", true),
		b("backspace", CmdBackspace, "erase", true),
		b("ctrl+h", CmdBackspace, "erase", true),
		b("ctrl+d", CmdDeleteChar, "delete char", true),
		b("delete", CmdDeleteChar, "delete char", true),
		b("ctrl+w", CmdKillWordBack, "delete word", false),
		b("alt+backspace", CmdKillWordBack, "delete word", true),
		b("alt+d", CmdKillWordForward, "delete word forward", true),
		b("ctrl+u", CmdKillLineBack, "clear to start", false),
		b("ctrl+k", CmdKillLineForward, "clear to end", false),
		b("ctrl+t", CmdTranspose, "transpose chars", true),
		b("ctrl+y", CmdYankKill, "yank killed", true),
	}
}

var readlineKeyCommands = map[string]string{
	"ctrl+a":        CmdLineStart,
	"home":          CmdLineStart,
	"ctrl+e":        CmdLineEnd,
	"end":           CmdLineEnd,
	"ctrl+b":        CmdCharLeft,
	"left":          CmdCharLeft,
	"ctrl+f":        CmdCharRight,
	"right":         CmdCharRight,
	"alt+b":         CmdWordLeft,
	"ctrl+left":     CmdWordLeft,
	"alt+f":         CmdWordRight,
	"ctrl+right":    CmdWordRight,
	"ctrl+h":        CmdBackspace,
	"ctrl+d":        CmdDeleteChar,
	"delete":        CmdDeleteChar,
	"ctrl+w":        CmdKillWordBack,
	"alt+backspace": CmdKillWordBack,
	"alt+d":         CmdKillWordForward,
	"ctrl+u":        CmdKillLineBack,
	"ctrl+k":        CmdKillLineForward,
	"ctrl+t":        CmdTranspose,
	"ctrl+y":        CmdYankKill,
}

func readlineWordPrev(rs []rune, lo, pos int, scope string) int {
	i := pos
	for i > lo && unicode.IsSpace(rs[i-1]) {
		i--
	}
	if i > lo {
		cls := wordClassScoped(rs[i-1], false, scope)
		for i > lo && wordClassScoped(rs[i-1], false, scope) == cls {
			i--
		}
	}
	return i
}

func readlineWordNext(rs []rune, pos, hi int, scope string) int {
	i := pos
	for i < hi && unicode.IsSpace(rs[i]) {
		i++
	}
	if i < hi {
		cls := wordClassScoped(rs[i], false, scope)
		for i < hi && wordClassScoped(rs[i], false, scope) == cls {
			i++
		}
	}
	return i
}

// Line is a rune-indexed single-line editor with a cursor and kill buffer,
// implementing the standard Readline commands for forms, pickers and filters.
type Line struct {
	Text      []rune
	Cursor    int
	Kill      string
	WordScope string // "", "default", "code", "path", "json", "csv", "markdown"
}

// SetWordScope sets the scope used for Readline word motions and kills.
func (l *Line) SetWordScope(scope string) { l.WordScope = scope }

// NewLine creates a Line holding s with the cursor at the end.
func NewLine(s string) Line {
	rs := []rune(s)
	return Line{Text: rs, Cursor: len(rs)}
}

// Value returns the current text.
func (l *Line) Value() string { return string(l.Text) }

// Set replaces the text and moves the cursor to the end.
func (l *Line) Set(s string) {
	l.Text = []rune(s)
	l.Cursor = len(l.Text)
}

// Sync aligns the Line with external text s: if s differs from Value(), Set(s)
// is called; otherwise the cursor is clamped into [0, len(Text)].
func (l *Line) Sync(s string) {
	if string(l.Text) != s {
		l.Set(s)
		return
	}
	l.Cursor = min(max(l.Cursor, 0), len(l.Text))
}

// Insert inserts s (normalizing CRLF and CR to LF) at the cursor.
func (l *Line) Insert(s string) bool {
	if s == "" {
		return false
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	rs := []rune(s)
	l.Cursor = min(max(l.Cursor, 0), len(l.Text))
	l.Text = slices.Insert(l.Text, l.Cursor, rs...)
	l.Cursor += len(rs)
	return true
}

func (l *Line) lineBounds() (lo, hi int) {
	lo = l.Cursor
	for lo > 0 && l.Text[lo-1] != '\n' {
		lo--
	}
	hi = l.Cursor
	for hi < len(l.Text) && l.Text[hi] != '\n' {
		hi++
	}
	return lo, hi
}

func (l *Line) cut(from, to int, saveKill bool) bool {
	if from < 0 {
		from = 0
	}
	if to > len(l.Text) {
		to = len(l.Text)
	}
	if from >= to {
		return false
	}
	if saveKill {
		l.Kill = string(l.Text[from:to])
	}
	l.Text = slices.Delete(l.Text, from, to)
	l.Cursor = from
	return true
}

// HandleCommand applies CmdInsert, CmdNewline, or any Readline command to the line.
// changed reports whether Text was modified; handled reports whether cmd.ID
// was recognized.
func (l *Line) HandleCommand(cmd core.Command) (changed, handled bool) {
	l.Cursor = min(max(l.Cursor, 0), len(l.Text))
	lo, hi := l.lineBounds()
	switch cmd.ID {
	case CmdInsert:
		return l.Insert(cmd.Arg("text")), true
	case CmdNewline:
		return l.Insert("\n"), true
	case CmdLineStart:
		l.Cursor = lo
		return false, true
	case CmdLineEnd:
		l.Cursor = hi
		return false, true
	case CmdCharLeft:
		if l.Cursor > 0 {
			l.Cursor--
		}
		return false, true
	case CmdCharRight:
		if l.Cursor < len(l.Text) {
			l.Cursor++
		}
		return false, true
	case CmdWordLeft:
		l.Cursor = readlineWordPrev(l.Text, lo, l.Cursor, l.WordScope)
		return false, true
	case CmdWordRight:
		l.Cursor = readlineWordNext(l.Text, l.Cursor, hi, l.WordScope)
		return false, true
	case CmdBackspace:
		return l.cut(l.Cursor-1, l.Cursor, false), true
	case CmdDeleteChar:
		return l.cut(l.Cursor, l.Cursor+1, false), true
	case CmdKillWordBack:
		from := readlineWordPrev(l.Text, lo, l.Cursor, l.WordScope)
		return l.cut(from, l.Cursor, true), true
	case CmdKillWordForward:
		to := readlineWordNext(l.Text, l.Cursor, hi, l.WordScope)
		return l.cut(l.Cursor, to, true), true
	case CmdKillLineBack:
		return l.cut(lo, l.Cursor, true), true
	case CmdKillLineForward:
		return l.cut(l.Cursor, hi, true), true
	case CmdTranspose:
		if hi-lo < 2 || l.Cursor <= lo {
			return false, true
		}
		pos := l.Cursor
		if pos == hi {
			pos--
		}
		l.Text[pos-1], l.Text[pos] = l.Text[pos], l.Text[pos-1]
		l.Cursor = min(pos+1, hi)
		return true, true
	case CmdYankKill:
		return l.Insert(l.Kill), true
	default:
		return false, false
	}
}

// handleReadline applies a Readline command while input.Model is in insert mode.
// Line-bounded commands stay within the current line in multiline buffers.
func (m *Model) handleReadline(id string) bool {
	lo := m.lineStart(m.cursor)
	hi := m.lineEnd(m.cursor)
	cut := func(from, to int, saveKill bool) {
		if from >= to {
			return
		}
		if saveKill {
			m.kill = string(m.text[from:to])
			m.registers[0] = register{text: m.kill}
		}
		m.splice(from, to, "")
		m.cursor = from
	}
	switch id {
	case CmdLineStart:
		m.cursor = lo
	case CmdLineEnd:
		m.cursor = hi
	case CmdCharLeft:
		if m.cursor > 0 {
			m.cursor--
		}
	case CmdCharRight:
		if m.cursor < len(m.text) {
			m.cursor++
		}
	case CmdWordLeft:
		m.cursor = readlineWordPrev(m.text, lo, m.cursor, m.WordScope)
	case CmdWordRight:
		m.cursor = readlineWordNext(m.text, m.cursor, hi, m.WordScope)
	case CmdBackspace:
		m.typeIn(insertion{backspace: true})
	case CmdDeleteChar:
		if m.cursor < len(m.text) {
			cut(m.cursor, m.cursor+1, false)
		}
	case CmdKillWordBack:
		from := readlineWordPrev(m.text, lo, m.cursor, m.WordScope)
		cut(from, m.cursor, true)
	case CmdKillWordForward:
		to := readlineWordNext(m.text, m.cursor, hi, m.WordScope)
		cut(m.cursor, to, true)
	case CmdKillLineBack:
		cut(lo, m.cursor, true)
	case CmdKillLineForward:
		cut(m.cursor, hi, true)
	case CmdTranspose:
		if hi-lo < 2 || m.cursor <= lo {
			return true
		}
		pos := m.cursor
		if pos == hi {
			pos--
		}
		m.text[pos-1], m.text[pos] = m.text[pos], m.text[pos-1]
		m.cursor = min(pos+1, hi)
	case CmdYankKill:
		if m.kill != "" {
			m.typeIn(insertion{text: m.kill})
		}
	default:
		return false
	}
	return true
}
