package input

import (
	"slices"
	"strings"
)

type shape struct {
	size     int // number of runes or lines
	linewise bool
}

type insertion struct {
	text      string
	backspace bool
}

// An edit stores the gesture, so repeat resolves its target at the new cursor.
type edit struct {
	key, target string
	count       int
	reg         byte
	selection   shape
	typed       []insertion
	from, to    rune // surround delimiters
}

type insertSession struct {
	before snapshot
	action edit
}

func (m *Model) snapshot() snapshot { return snapshot{m.Value(), m.cursor} }

func (m *Model) save(before snapshot, action edit) {
	if before.value == m.Value() {
		return
	}
	m.undo = append(m.undo, before)
	m.redo = nil
	m.last = action
}

func (m *Model) history(undo bool, count int) {
	from, to := &m.undo, &m.redo
	if !undo {
		from, to = to, from
	}
	for range min(count, len(*from)) {
		last := len(*from) - 1
		s := (*from)[last]
		*to = append(*to, m.snapshot())
		*from = (*from)[:last]
		m.text, m.cursor = []rune(s.value), s.cursor
	}
	m.mode, m.column = ModeNormal, -1
}

func (m *Model) visualRange() span {
	if m.mode == ModeVisualLine {
		return m.lineRange(m.anchor, m.cursor)
	}
	return span{min(m.anchor, m.cursor), min(max(m.anchor, m.cursor)+1, len(m.text)), false}
}

func (m *Model) visualShape() shape {
	r := m.visualRange()
	if r.linewise {
		return shape{1 + strings.Count(string(m.text[r.lo:r.hi]), "\n"), true}
	}
	return shape{r.hi - r.lo, false}
}

func (m *Model) selectionRange(s shape) span {
	if m.isVisual() {
		return m.visualRange()
	}
	if s.linewise {
		return m.lineRange(m.cursor, m.linesFrom(m.cursor, 1, s.size-1))
	}
	return span{m.cursor, m.cursor + min(s.size, len(m.text)-m.cursor), false}
}

func (m *Model) apply(a edit) {
	before := m.snapshot()
	m.column = -1
	switch a.key {
	case "i", "a", "I", "A", "o", "O":
		if (a.key == "o" || a.key == "O") && !m.multiline {
			return
		}
		m.prepareInsert(a.key)
	case "d", "c", "y":
		var r span
		if m.isVisual() || a.selection.size > 0 {
			r = m.selectionRange(a.selection)
		} else {
			var ok bool
			r, ok = m.motionRange(a.key, a.target, a.count)
			if !ok {
				return
			}
		}
		if r.lo != r.hi || r.linewise {
			m.registers[a.reg] = register{string(m.text[r.lo:r.hi]), r.linewise}
			m.registers[0] = m.registers[a.reg]
		} else if a.key != "c" {
			m.mode = ModeNormal
			return
		}
		m.mode = ModeNormal
		if a.key == "y" {
			m.lastYank = r
			m.hasLastYank = true
			m.cursor = m.normalPos(r.lo)
			return
		}
		m.remove(r, a.key == "c")
		if a.key == "d" {
			m.cursor = m.normalPos(m.cursor)
			m.save(before, a)
			return
		}
	case "p", "P":
		reg, ok := m.registers[a.reg]
		if !ok {
			return
		}
		if !m.multiline {
			reg.linewise = false
		}
		overSelection := m.isVisual() || a.selection.size > 0
		if overSelection {
			r := m.selectionRange(a.selection)
			m.registers[0] = register{string(m.text[r.lo:r.hi]), r.linewise}
			// Replacing selected whole lines retains a line to receive the paste.
			m.remove(r, r.linewise)
			if r.linewise && reg.linewise {
				reg.text = strings.TrimSuffix(strings.Repeat(reg.text+"\n", max(a.count, 1)), "\n")
				m.splice(m.cursor, m.cursor, reg.text)
				m.mode = ModeNormal
				m.save(before, a)
				return
			}
		}
		m.paste(reg, a.key == "P" || overSelection, max(a.count, 1))
		m.mode = ModeNormal
		m.save(before, a)
		return
	case surroundAdd, surroundDelete, surroundChange, surroundVisual:
		m.applySurround(before, a)
		return
	default:
		return
	}

	m.mode = ModeInsert
	events := a.typed
	a.typed = nil
	m.session = &insertSession{before: before, action: a}
	for _, ev := range events {
		m.typeIn(ev)
	}
}

func (m *Model) splice(lo, hi int, text string) {
	m.text = slices.Replace(m.text, lo, hi, []rune(text)...)
}

func (m *Model) remove(r span, replacing bool) {
	lo, hi := r.lo, r.hi
	if r.linewise && !replacing {
		switch {
		case hi < len(m.text):
			hi++
		case lo > 0:
			lo--
		}
	}
	m.splice(lo, hi, "")
	m.cursor = lo
	if r.linewise && !replacing {
		m.cursor = m.firstNonblank(m.lineStart(lo))
	}
}

func (m *Model) paste(reg register, before bool, count int) {
	pos := m.cursor
	if reg.linewise {
		text := strings.TrimSuffix(strings.Repeat(reg.text+"\n", count), "\n")
		if before {
			pos = m.lineStart(pos)
			m.splice(pos, pos, text+"\n")
		} else {
			pos = m.lineEnd(pos)
			m.splice(pos, pos, "\n"+text)
			pos++
		}
		m.cursor = m.firstNonblank(pos)
		return
	}
	text := strings.Repeat(reg.text, count)
	if !before && pos < m.lineEnd(pos) {
		pos++
	}
	m.splice(pos, pos, text)
	m.cursor = m.normalPos(pos + max(0, len([]rune(text))-1))
}

func (m *Model) prepareInsert(key string) {
	switch key {
	case "a":
		if m.cursor < m.lineEnd(m.cursor) {
			m.cursor++
		}
	case "I":
		m.cursor = m.indentEnd(m.lineStart(m.cursor))
	case "A":
		m.cursor = m.lineEnd(m.cursor)
	case "O":
		m.cursor = m.lineStart(m.cursor)
		m.splice(m.cursor, m.cursor, "\n")
	case "o":
		m.cursor = m.lineEnd(m.cursor)
		m.splice(m.cursor, m.cursor, "\n")
		m.cursor++
	}
}

func (m *Model) insert(ev insertion) {
	if ev.backspace {
		if m.cursor > 0 {
			m.splice(m.cursor-1, m.cursor, "")
			m.cursor--
		}
		return
	}
	runes := []rune(m.normalize(ev.text))
	m.text = slices.Insert(m.text, m.cursor, runes...)
	m.cursor += len(runes)
}

func (m *Model) typeIn(ev insertion) {
	m.session.action.typed = append(m.session.action.typed, ev)
	m.insert(ev)
}

func (m *Model) finishInsert() {
	s := m.session
	if s.action.key != "c" && (len(s.action.typed) > 0 || s.action.key == "o" || s.action.key == "O") {
		for i := 1; i < s.action.count; i++ {
			if s.action.key == "o" || s.action.key == "O" {
				m.prepareInsert("o")
			}
			for _, ev := range s.action.typed {
				m.insert(ev)
			}
		}
	}
	if m.cursor > m.lineStart(m.cursor) {
		m.cursor--
	}
	m.mode, m.session = ModeNormal, nil
	m.save(s.before, s.action)
}
