package input

import (
	"unicode"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
)

// Surround command IDs. CmdSurroundAdd takes Args "target" (a motion or
// text object such as "iw", "ip", "$", or "s" for the line) and "char";
// CmdSurroundDelete takes "char"; CmdSurroundChange takes "from" and "to".
// Command.Count applies as in the keys (2ysiw", ds2( ).
const (
	CmdSurroundAdd    = "input.surround.add"
	CmdSurroundDelete = "input.surround.delete"
	CmdSurroundChange = "input.surround.change"
)

// edit keys for surround gestures; they never collide with typed keys
// because apply only sees them through surroundKey and the methods below.
const (
	surroundAdd    = "ys"
	surroundDelete = "ds"
	surroundChange = "cs"
	surroundVisual = "S"
)

// SurroundPair returns the opening and closing delimiters for r. Either
// side of a bracket pair selects the pair: '(' and ')' both give "(" ")".
// The vim-surround aliases b, B, r and a stand for (), {}, [] and <>.
// Quotes, backticks and other punctuation surround with the same rune on
// both sides. Letters, digits and blanks are not delimiters.
func SurroundPair(r rune) (open, close rune, ok bool) {
	switch r {
	case '(', ')', 'b':
		return '(', ')', true
	case '[', ']', 'r':
		return '[', ']', true
	case '{', '}', 'B':
		return '{', '}', true
	case '<', '>', 'a':
		return '<', '>', true
	}
	if unicode.IsPunct(r) || unicode.IsSymbol(r) {
		return r, r, true
	}
	return 0, 0, false
}

func surroundRune(key string) (rune, bool) {
	if utf8.RuneCountInString(key) != 1 {
		return 0, false
	}
	r, _ := utf8.DecodeRuneInString(key)
	_, _, ok := SurroundPair(r)
	return r, ok
}

// surroundKey drives the ys/ds/cs/visual-S key stages. ok is false when
// the key is not part of a surround gesture and handle should continue.
func (m *Model) surroundKey(key string) (handled, ok bool) {
	p := &m.pending
	cancel := func() (bool, bool) { m.pending = pending{}; return true, true }
	switch p.sur {
	case "":
		if p.prefix != "" {
			return false, false
		}
		if key == "s" && !m.isVisual() && (p.op == "y" || p.op == "d" || p.op == "c") {
			p.sur, p.op = p.op+"s", ""
			return true, true
		}
		if key == "S" && m.isVisual() {
			p.sur = surroundVisual
			return true, true
		}
		return false, false
	case surroundAdd:
		if p.surTarget == "" && len(key) == 1 && key[0] >= '0' && key[0] <= '9' && (key != "0" || p.count > 0) {
			if d := int(key[0] - '0'); p.count <= (maxInt-d)/10 {
				p.count = p.count*10 + d
			}
			return true, true
		}
		if p.surTarget == "" && (key == "i" || key == "a" || key == "g") {
			p.surTarget = key
			return true, true
		}
		target := p.surTarget + key
		if target != "s" && !isObject(target) && !isMotion(target) {
			return cancel()
		}
		p.sur, p.surTarget = surroundAdd+":", target
		return true, true
	case surroundChange:
		r, valid := surroundRune(key)
		if !valid {
			return cancel()
		}
		p.sur, p.surFrom = surroundChange+":", r
		return true, true
	}
	r, valid := surroundRune(key)
	if !valid {
		return cancel()
	}
	active := *p
	m.pending = pending{}
	a := edit{target: active.surTarget, count: multipliedCount(active.opCount, active.count)}
	switch active.sur {
	case surroundAdd + ":":
		a.key, a.to = surroundAdd, r
	case surroundDelete:
		a.key, a.from = surroundDelete, r
	case surroundChange + ":":
		a.key, a.from, a.to = surroundChange, active.surFrom, r
	case surroundVisual:
		a.key, a.to, a.selection = surroundVisual, r, m.visualShape()
	default:
		return true, true
	}
	m.apply(a)
	return true, true
}

func (m *Model) surroundCommand(cmd core.Command) bool {
	if m.mode == ModeInsert {
		return false
	}
	count := 0
	if cmd.Count > 0 {
		count = cmd.Times()
	}
	first := func(name string) rune { r, _ := utf8.DecodeRuneInString(cmd.Arg(name)); return r }
	switch cmd.ID {
	case CmdSurroundAdd:
		m.Surround(cmd.Arg("target"), first("char"), count)
	case CmdSurroundDelete:
		m.DeleteSurround(first("char"), count)
	case CmdSurroundChange:
		m.ChangeSurround(first("from"), first("to"), count)
	}
	return true
}

// Surround wraps the range of target (a motion, a text object, or "s" for
// the current line without its indentation) in the pair for delim, like
// ys{target}{delim}. In visual mode it wraps the selection and target is
// ignored, like S{delim}. It reports whether the text changed; the change
// is one undo step and repeats with ".".
func (m *Model) Surround(target string, delim rune, count int) bool {
	a := edit{key: surroundAdd, target: target, count: count, to: delim}
	if m.isVisual() {
		a.key, a.selection = surroundVisual, m.visualShape()
	}
	return m.applyChanged(a)
}

// DeleteSurround removes the count-th pair of delim enclosing the cursor,
// like ds{delim}.
func (m *Model) DeleteSurround(delim rune, count int) bool {
	return m.applyChanged(edit{key: surroundDelete, count: count, from: delim})
}

// ChangeSurround replaces the count-th pair of from enclosing the cursor
// with the pair for to, like cs{from}{to}.
func (m *Model) ChangeSurround(from, to rune, count int) bool {
	return m.applyChanged(edit{key: surroundChange, count: count, from: from, to: to})
}

func (m *Model) applyChanged(a edit) bool {
	if m.mode == "" {
		m.mode, m.column = ModeNormal, -1
		m.registers = make(map[byte]register)
	}
	if m.mode == ModeInsert {
		return false
	}
	before := m.Value()
	m.pending = pending{}
	m.apply(a)
	return m.Value() != before
}

func (m *Model) applySurround(before snapshot, a edit) {
	defer func() { m.mode = ModeNormal }()
	switch a.key {
	case surroundAdd, surroundVisual:
		open, close, ok := SurroundPair(a.to)
		if !ok {
			return
		}
		var r span
		switch {
		case a.key == surroundVisual:
			r = m.selectionRange(a.selection)
		case a.target == "s":
			start := m.lineStart(m.cursor)
			r = span{m.indentEnd(start), m.lineEnd(m.linesFrom(start, 1, max(a.count, 1)-1)), false}
		default:
			r, ok = m.motionRange("y", a.target, a.count)
			if !ok {
				return
			}
		}
		if !r.linewise || !m.multiline {
			for r.hi > r.lo && unicode.IsSpace(m.text[r.hi-1]) {
				r.hi--
			}
			if r.hi <= r.lo {
				return
			}
			m.splice(r.hi, r.hi, string(close))
			m.splice(r.lo, r.lo, string(open))
		} else {
			m.splice(r.hi, r.hi, "\n"+string(close))
			m.splice(r.lo, r.lo, string(open)+"\n")
		}
		m.cursor = r.lo
	case surroundDelete, surroundChange:
		lo, hi, ok := m.surrounding(a.from, a.count)
		if !ok {
			return
		}
		if a.key == surroundDelete {
			m.splice(hi, hi+1, "")
			m.splice(lo, lo+1, "")
		} else {
			open, close, ok := SurroundPair(a.to)
			if !ok {
				return
			}
			m.splice(hi, hi+1, string(close))
			m.splice(lo, lo+1, string(open))
		}
		m.cursor = lo
	}
	m.cursor = m.normalPos(m.cursor)
	m.save(before, a)
}

// surrounding returns the positions of the opening and closing delimiters
// of the count-th pair for delim enclosing the cursor. Symmetric
// delimiters (quotes) pair on the cursor's line, honoring backslashes.
func (m *Model) surrounding(delim rune, count int) (lo, hi int, ok bool) {
	open, close, ok := SurroundPair(delim)
	if !ok {
		return 0, 0, false
	}
	if open == close {
		r, ok := m.quoteObject(open, false, 1)
		return r.lo - 1, r.hi, ok
	}
	count = max(count, 1)
	var stack []int
	for i, r := range m.text {
		switch r {
		case open:
			stack = append(stack, i)
		case close:
			if len(stack) == 0 {
				continue
			}
			left := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if left <= m.cursor && i >= m.cursor {
				if count--; count == 0 {
					return left, i, true
				}
			}
		}
	}
	return 0, 0, false
}
