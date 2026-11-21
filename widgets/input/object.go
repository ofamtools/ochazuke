package input

import (
	"strings"
	"unicode"
)

func isObject(key string) bool {
	return len(key) == 2 && (key[0] == 'i' || key[0] == 'a') && strings.ContainsRune("wp\"'()[]{}", rune(key[1]))
}

func (m *Model) object(key string, count int) (span, bool) {
	around := key[0] == 'a'
	n := max(1, count)
	switch key[1] {
	case 'w':
		if m.cursor >= len(m.text) {
			return span{}, false
		}
		lo := m.cursor
		class := wordClass(m.text[lo], false)
		for lo > 0 && m.text[lo-1] != '\n' && wordClass(m.text[lo-1], false) == class {
			lo--
		}
		hi := m.cursor
		for hi < len(m.text) && m.text[hi] != '\n' && wordClass(m.text[hi], false) == class {
			hi++
		}
		// An inner word counts both runs of blanks and runs of word
		// characters. An around-word includes the separating blanks.
		for i := 1; i < n || (around && class == 0 && i == n); i++ {
			if around {
				for hi < len(m.text) && unicode.IsSpace(m.text[hi]) {
					hi++
				}
			}
			if hi == len(m.text) {
				return span{}, false
			}
			next := wordClass(m.text[hi], false)
			for hi < len(m.text) && wordClass(m.text[hi], false) == next {
				hi++
			}
		}
		if around && class != 0 {
			lo, hi = m.aroundBlanks(lo, hi)
		}
		return span{lo, hi, false}, hi > lo
	case 'p':
		start := m.lineStart(m.cursor)
		blank := m.blank(start)
		lo, hi := start, start
		for lo > 0 {
			prev := m.lineStart(lo - 1)
			if m.blank(prev) != blank {
				break
			}
			lo = prev
		}
		for {
			next := m.linesFrom(hi, 1, 1)
			if next == hi {
				break
			}
			if b := m.blank(next); b != blank {
				if n == 1 {
					break
				}
				n--
				blank = b
			}
			hi = next
		}
		if around {
			for {
				next := m.linesFrom(hi, 1, 1)
				if next == hi || !m.blank(next) {
					break
				}
				hi = next
			}
		}
		return m.lineRange(lo, hi), true
	case '"', '\'':
		return m.quoteObject(rune(key[1]), around, n)
	default:
		return m.pairObject(rune(key[1]), around, n)
	}
}

func enclosed(lo, hi int, around bool) span {
	if around {
		return span{lo, hi + 1, false}
	}
	return span{lo + 1, hi, false}
}

func (m *Model) quoteObject(quote rune, around bool, count int) (span, bool) {
	open := -1
	escaped := false
	for i := m.lineStart(m.cursor); i < len(m.text) && m.text[i] != '\n'; i++ {
		r := m.text[i]
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r != quote {
			continue
		}
		if open < 0 {
			open = i
			continue
		}
		if m.cursor <= i {
			r := enclosed(open, i, around || count > 1)
			if around {
				r.lo, r.hi = m.aroundBlanks(r.lo, r.hi)
			}
			return r, true
		}
		open = -1
	}
	return span{}, false
}

func (m *Model) pairObject(delimiter rune, around bool, count int) (span, bool) {
	index := strings.IndexRune(opens, delimiter)
	if index < 0 {
		index = strings.IndexRune(closes, delimiter)
	}
	open, close := rune(opens[index]), rune(closes[index])
	// If no opening delimiter encloses the cursor, Vim looks ahead
	// to the next opening delimiter of the requested type.
	var stack []int
	following := -1
	hasEnclosing := false
	for i, r := range m.text {
		if r == open {
			stack = append(stack, i)
			if following < 0 && i > m.cursor {
				following = i
			}
		}
		if r == close && len(stack) > 0 {
			left := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if left <= m.cursor && i >= m.cursor {
				hasEnclosing = true
				count--
				if count == 0 {
					return enclosed(left, i, around), true
				}
			}
		}
	}
	if !hasEnclosing && following >= 0 && count == 1 {
		if right, ok := m.partner(following); ok {
			return enclosed(following, right, around), true
		}
	}
	return span{}, false
}

// aroundBlanks prefers trailing whitespace and falls back to leading whitespace.
func (m *Model) aroundBlanks(lo, hi int) (int, int) {
	end := hi
	for hi < len(m.text) && m.text[hi] != '\n' && unicode.IsSpace(m.text[hi]) {
		hi++
	}
	if hi == end {
		for lo > 0 && m.text[lo-1] != '\n' && unicode.IsSpace(m.text[lo-1]) {
			lo--
		}
	}
	return lo, hi
}
