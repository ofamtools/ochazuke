// Package input provides a modal, rune-indexed text editor for ui.KindInput.
package input

import (
	"fmt"
	"hash/fnv"
	"strings"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

// Mode is the input's local editing mode.
type Mode string

const (
	ModeNormal     Mode = "normal"
	ModeInsert     Mode = "insert"
	ModeVisual     Mode = "visual"
	ModeVisualLine Mode = "visual-line"
)

// Command IDs for bindings. CmdKey accepts the same key and text that core
// forwards to a widget. CmdInsert inserts literal text during an insert session.
const (
	CmdKey         = "input.key"
	CmdInsert      = "input.insert"
	CmdBackspace   = "input.backspace"
	CmdNewline     = "input.newline"
	CmdEscape      = "input.escape"
	CmdDelete      = "input.delete"
	CmdChange      = "input.change"
	CmdYank        = "input.yank"
	CmdPasteAfter  = "input.paste.after"
	CmdPasteBefore = "input.paste.before"
	CmdUndo        = "input.undo"
	CmdRedo        = "input.redo"
	CmdRepeat      = "input.repeat"
)

type snapshot struct {
	value  string
	cursor int
}

type register struct {
	text     string // linewise text excludes the final line terminator
	linewise bool
}

type pending struct {
	op, prefix     string
	opCount, count int
	reg            byte
	sur, surTarget string // surround stage ("ys", "ys:", "ds", "cs", "cs:", "S") and ys target
	surFrom        rune   // cs delimiter being replaced
}

// Model owns the buffer, cursor, registers and undo history for one input.
// Its zero value is an empty single-line input in normal mode.
type Model struct {
	WordScope string // "", "default", "code", "path", "json", "csv", "markdown"

	text           []rune
	initial        string
	multiline      bool
	cursor, anchor int
	column         int // preferred rune column for consecutive vertical motions
	mode           Mode
	pending        pending
	registers      map[byte]register
	undo, redo     []snapshot
	last           edit
	session        *insertSession
	kill           string
	lastYank       span
	hasLastYank    bool
}

// SetWordScope sets the scope used for word boundary motions and Readline word operations.
func (m *Model) SetWordScope(scope string) { m.WordScope = scope }

// New creates an input in normal mode at the first rune. Single-line inputs
// turn pasted and initial line breaks into spaces.
func New(value string, multiline bool) *Model {
	m := &Model{multiline: multiline, mode: ModeNormal, column: -1, registers: make(map[byte]register)}
	m.initial = m.normalize(value)
	m.text = []rune(m.initial)
	return m
}

func (m *Model) normalize(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	if !m.multiline {
		s = strings.ReplaceAll(s, "\n", " ")
	}
	return s
}

// Value returns the current text.
func (m *Model) Value() string { return string(m.text) }

// Cursor returns the absolute rune offset. In insert mode it is the insertion point.
func (m *Model) Cursor() int { return m.cursor }

// Mode returns the current editing mode.
func (m *Model) Mode() Mode {
	if m.mode == "" {
		return ModeNormal
	}
	return m.mode
}

// View builds a focused-input candidate using the renderer's rune cursor contract.
func (m *Model) View(id string) ui.ViewNode {
	value := m.Value()
	dirty := value != m.initial
	hash := fnv.New64a()
	fmt.Fprintf(hash, "%s/%d/%t/%t/", m.Mode(), m.cursor, m.multiline, dirty)
	hash.Write([]byte(value))
	return ui.ViewNode{ID: id, Kind: ui.KindInput, Scope: "input", Focusable: true,
		Props:    map[string]any{"value": value, "cursor": m.cursor, "multiline": m.multiline},
		Facts:    map[string]any{"mode": string(m.Mode()), "dirty": dirty, "multiline": m.multiline},
		CacheKey: max(hash.Sum64(), 1),
	}
}

var commandKeys = map[string]string{
	"motion.left": "h", "motion.down": "j", "motion.up": "k", "motion.right": "l",
	"motion.word.next": "w", "motion.word.prev": "b", "motion.word.end": "e",
	"motion.top": "gg", "motion.bottom": "G", "motion.start": "0", "motion.end": "$",
	"motion.block.next": "}", "motion.block.prev": "{",
	CmdBackspace: "backspace", CmdNewline: "enter", CmdEscape: "esc",
	CmdDelete: "d", CmdChange: "c", CmdYank: "y", CmdPasteAfter: "p", CmdPasteBefore: "P",
	CmdUndo: "u", CmdRedo: "ctrl+r", CmdRepeat: ".",
}

// HandleCommand applies a resolved motion or input command. For example,
// input.delete followed by motion.word.next is the same operation as dw.
// To keep Escape local when a host also binds it to close a modal, bind
// "esc" to CmdEscape at scope "input" and let the enclosing close binding
// use the default bubble phase. The keymap resolves the inner scope first.
func (m *Model) HandleCommand(cmd core.Command) bool {
	switch cmd.ID {
	case CmdKey:
		return m.handle(cmd.Arg("key"), cmd.Arg("text"), cmd.Count)
	case CmdInsert:
		if m.mode == ModeInsert {
			m.typeIn(insertion{text: cmd.Arg("text")})
		}
		return true
	case CmdSurroundAdd, CmdSurroundDelete, CmdSurroundChange:
		return m.surroundCommand(cmd)
	}
	if m.mode == ModeInsert && IsReadlineCommand(cmd.ID) {
		return m.handleReadline(cmd.ID)
	}
	key, ok := commandKeys[cmd.ID]
	if !ok {
		return false
	}
	if m.mode == ModeInsert && key != "esc" && key != "enter" && key != "backspace" {
		return false
	}
	count := 0
	if cmd.Count > 0 {
		count = cmd.Times()
	}
	return m.handle(key, "", count)
}

// HandleKey applies a key name such as "i", "esc" or "ctrl+r".
func (m *Model) HandleKey(key string) bool { return m.handle(key, "", 0) }

// HandleInput accepts the Key and Text of a core.PassToWidgetEffect.
// A paste's text is inserted literally, including embedded newlines in multiline inputs.
func (m *Model) HandleInput(key, text string) bool { return m.handle(key, text, 0) }

func (m *Model) handle(key, text string, count int) bool {
	if m.mode == "" {
		m.mode, m.column = ModeNormal, -1
		m.registers = make(map[byte]register)
	}
	if key == "esc" {
		if m.mode == ModeInsert {
			m.finishInsert()
		}
		m.mode, m.pending = ModeNormal, pending{}
		return true
	}
	if m.mode == ModeInsert {
		if rl, ok := readlineKeyCommands[key]; ok {
			return m.handleReadline(rl)
		}
		switch key {
		case "backspace":
			m.typeIn(insertion{backspace: true})
		case "enter":
			if m.multiline {
				m.typeIn(insertion{text: "\n"})
			}
		default:
			if text == "" {
				switch {
				case key == "space":
					text = " "
				case utf8.RuneCountInString(key) == 1:
					text = key
				default:
					return false
				}
			}
			m.typeIn(insertion{text: text})
		}
		return true
	}
	p := &m.pending
	if count > 0 {
		p.count = count
	}
	if handled, ok := m.surroundKey(key); ok {
		return handled
	}
	if p.prefix == `"` {
		p.prefix = ""
		if key == `"` {
			p.reg = 0
		} else if len(key) == 1 && key[0] >= 'a' && key[0] <= 'z' {
			p.reg = key[0]
		} else {
			m.pending = pending{}
		}
		return true
	}
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' && (key != "0" || p.count > 0) {
		digit := int(key[0] - '0')
		if p.count <= (maxInt-digit)/10 {
			p.count = p.count*10 + digit
		}
		return true
	}
	if p.prefix != "" {
		key = p.prefix + key
		p.prefix = ""
	} else if key == "g" || key == `"` || (p.op != "" || m.isVisual()) && (key == "i" || key == "a") {
		p.prefix = key
		return true
	}
	op := key == "d" || key == "c" || key == "y"
	if op && p.op == "" && !m.isVisual() {
		p.op, p.opCount, p.count = key, p.count, 0
		return true
	}

	active := *p
	*p = pending{}
	count = multipliedCount(active.opCount, active.count)
	action := edit{key: key, count: count, reg: active.reg}
	switch {
	case active.op != "":
		action.key, action.target = active.op, key
		m.apply(action)
	case op && m.isVisual():
		action.selection = m.visualShape()
		m.apply(action)
	case m.isVisual() && isObject(key):
		if r, ok := m.object(key, count); ok && (r.hi > r.lo || r.linewise) {
			m.anchor, m.cursor = r.lo, max(r.lo, r.hi-1)
			if r.linewise {
				m.mode = ModeVisualLine
			}
		}
	case isMotion(key):
		return m.move(key, count)
	case key == "v" || key == "V":
		mode := ModeVisual
		if key == "V" {
			mode = ModeVisualLine
		}
		if m.mode == mode {
			m.mode = ModeNormal
		} else {
			if !m.isVisual() {
				m.anchor = m.cursor
			}
			m.mode = mode
		}
	case key == "u" || key == "ctrl+r":
		m.history(key == "u", max(count, 1))
	case key == ".":
		if m.last.key != "" {
			repeated := m.last
			if count > 0 {
				repeated.count = count
			}
			m.apply(repeated)
			if m.mode == ModeInsert {
				m.finishInsert()
			}
		}
	case key == "p" || key == "P":
		if m.isVisual() {
			action.selection = m.visualShape()
		}
		m.apply(action)
	case key == "i" || key == "a" || key == "I" || key == "A" || key == "o" || key == "O":
		m.apply(action)
	default:
		return false
	}
	return true
}

const maxInt = int(^uint(0) >> 1)

func multipliedCount(a, b int) int {
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	return min(a, maxInt/b) * b
}

func (m *Model) isVisual() bool { return m.mode == ModeVisual || m.mode == ModeVisualLine }
