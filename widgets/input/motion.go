package input

import (
	"strings"
	"unicode"
)

// span is a half-open rune range. A linewise span ends immediately before
// the last line's terminating newline (or at EOF).
type span struct {
	lo, hi   int
	linewise bool
}

func (m *Model) lineStart(pos int) int {
	for pos > 0 && m.text[pos-1] != '\n' {
		pos--
	}
	return pos
}

func (m *Model) lineEnd(pos int) int {
	for pos < len(m.text) && m.text[pos] != '\n' {
		pos++
	}
	return pos
}

func (m *Model) normalPos(pos int) int {
	pos = min(max(pos, 0), len(m.text))
	if (pos == len(m.text) || m.text[pos] == '\n') && pos > 0 && m.text[pos-1] != '\n' {
		return pos - 1
	}
	return pos
}

func (m *Model) indentEnd(start int) int {
	pos := start
	for pos < len(m.text) && m.text[pos] != '\n' && unicode.IsSpace(m.text[pos]) {
		pos++
	}
	return pos
}

func (m *Model) firstNonblank(start int) int { return m.normalPos(m.indentEnd(start)) }

// stepLine returns a neighboring line's start, or the current start at a boundary.
func (m *Model) stepLine(start, dir int) int {
	if dir < 0 {
		return m.lineStart(max(start-1, 0))
	}
	return min(m.lineEnd(start)+1, len(m.text))
}

func (m *Model) linesFrom(pos, dir, n int) int {
	start := m.lineStart(pos)
	for range n {
		next := m.lineStart(m.stepLine(start, dir))
		if next == start {
			break
		}
		start = next
	}
	return start
}

func (m *Model) lineRange(a, b int) span {
	return span{m.lineStart(min(a, b)), m.lineEnd(max(a, b)), true}
}

func wordClass(r rune, big bool) int {
	return wordClassScoped(r, big, "")
}

func wordClassScoped(r rune, big bool, scope string) int {
	if unicode.IsSpace(r) {
		return 0
	}
	if big {
		return 1
	}
	isIdent := unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) || r == '_'
	switch scope {
	case "path":
		if r == '/' || r == '\\' {
			return 3
		}
		if isIdent {
			return 1
		}
		return 2
	case "code":
		if isIdent {
			return 1
		}
		if strings.ContainsRune("()[]{}", r) {
			return 3
		}
		return 2
	case "json":
		if idx := strings.IndexRune(`{}[],:"`, r); idx >= 0 {
			return 10 + idx
		}
		if isIdent || r == '-' || r == '.' {
			return 1
		}
		return 2
	case "csv":
		if r == ',' {
			return 3
		}
		if isIdent {
			return 1
		}
		return 2
	case "markdown":
		if idx := strings.IndexRune("#*[]()", r); idx >= 0 {
			return 10 + idx
		}
		if isIdent || r == '-' {
			return 1
		}
		return 2
	default:
		if isIdent {
			return 1
		}
		return 2
	}
}

func (m *Model) wordClass(r rune, big bool) int {
	return wordClassScoped(r, big, m.WordScope)
}

func (m *Model) wordNext(pos int, big bool) int {
	if pos == len(m.text) {
		return pos
	}
	class := m.wordClass(m.text[pos], big)
	start := pos
	for pos < len(m.text) && class != 0 && m.wordClass(m.text[pos], big) == class {
		pos++
	}
	for pos < len(m.text) && unicode.IsSpace(m.text[pos]) {
		if pos > start && m.text[pos] == '\n' && m.text[pos-1] == '\n' {
			break
		}
		pos++
	}
	return pos
}

func (m *Model) wordPrev(pos int, big bool) int {
	for pos > 0 && unicode.IsSpace(m.text[pos-1]) {
		pos--
		if m.text[pos] == '\n' && (pos == 0 || m.text[pos-1] == '\n') {
			return pos
		}
	}
	if pos > 0 {
		class := m.wordClass(m.text[pos-1], big)
		for pos > 0 && m.wordClass(m.text[pos-1], big) == class {
			pos--
		}
	}
	return pos
}

// wordEnd includes the current rune in the search when changing a word (cw).
func (m *Model) wordEnd(pos int, big, includeCurrent bool) int {
	if !includeCurrent {
		pos = min(pos+1, len(m.text))
	}
	for pos < len(m.text) && unicode.IsSpace(m.text[pos]) {
		pos++
	}
	if pos == len(m.text) {
		return pos
	}
	class := m.wordClass(m.text[pos], big)
	for pos+1 < len(m.text) && m.wordClass(m.text[pos+1], big) == class {
		pos++
	}
	return pos
}

func (m *Model) blank(start int) bool {
	for i := start; i < len(m.text) && m.text[i] != '\n'; i++ {
		if !unicode.IsSpace(m.text[i]) {
			return false
		}
	}
	return true
}

func (m *Model) paragraph(pos, dir int) int {
	start := m.lineStart(pos)
	seenText := start < len(m.text) && m.text[start] != '\n'
	for {
		next := m.linesFrom(start, dir, 1)
		if next == start {
			if dir < 0 {
				return 0
			}
			return len(m.text)
		}
		start = next
		blank := start == len(m.text) || m.text[start] == '\n'
		if seenText && blank {
			return start
		}
		if !blank {
			seenText = true
		}
	}
}

const opens = "([{"
const closes = ")]}"

func (m *Model) partner(pos int) (int, bool) {
	if pos >= len(m.text) {
		return pos, false
	}
	r := m.text[pos]
	index := strings.IndexRune(opens, r)
	dir := 1
	partners := closes
	if index < 0 {
		index = strings.IndexRune(closes, r)
		dir, partners = -1, opens
	}
	if index < 0 {
		return pos, false
	}
	mate := rune(partners[index])
	depth := 1
	for i := pos + dir; i >= 0 && i < len(m.text); i += dir {
		switch m.text[i] {
		case r:
			depth++
		case mate:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return pos, false
}

func isFindMotion(key string) (kind string, target rune, ok bool) {
	if len(key) < 2 {
		return "", 0, false
	}
	switch key[0] {
	case 'f', 'F', 't', 'T':
		rs := []rune(key[1:])
		if len(rs) == 1 {
			return key[:1], rs[0], true
		}
	}
	return "", 0, false
}

func isMotion(key string) bool {
	switch key {
	case "h", "j", "k", "l", "w", "W", "b", "B", "e", "E", "0", "^", "$", "gg", "G", "{", "}", "(", ")", "%", ";", ",":
		return true
	}
	_, _, ok := isFindMotion(key)
	return ok
}

func (m *Model) findCharOnLine(pos int, kind string, target rune, count int) (int, bool) {
	start, end := m.lineStart(pos), m.lineEnd(pos)
	n := max(count, 1)
	cur := pos
	switch kind {
	case "f", "t":
		for step := 0; step < n; step++ {
			from := cur + 1
			if kind == "t" && step > 0 {
				from = cur + 2
			}
			found := -1
			for i := from; i < end; i++ {
				if m.text[i] == target {
					found = i
					break
				}
			}
			if found < 0 {
				return pos, false
			}
			if kind == "t" {
				cur = found - 1
			} else {
				cur = found
			}
		}
		return cur, true
	case "F", "T":
		for step := 0; step < n; step++ {
			from := cur - 1
			if kind == "T" && step > 0 {
				from = cur - 2
			}
			found := -1
			for i := from; i >= start; i-- {
				if m.text[i] == target {
					found = i
					break
				}
			}
			if found < 0 {
				return pos, false
			}
			if kind == "T" {
				cur = found + 1
			} else {
				cur = found
			}
		}
		return cur, true
	}
	return pos, false
}

func invertFindKind(kind string) string {
	switch kind {
	case "f":
		return "F"
	case "F":
		return "f"
	case "t":
		return "T"
	case "T":
		return "t"
	}
	return kind
}

// unitMove implements ( and ) with WordScope-aware boundaries.
func (m *Model) unitMove(pos, dir int) int {
	n := len(m.text)
	if n == 0 {
		return 0
	}
	isDelim := func(i int) bool {
		r := m.text[i]
		switch m.WordScope {
		case "csv":
			return r == ',' || r == '\n'
		case "json":
			return r == ',' || r == '{' || r == '}' || r == '[' || r == ']' || r == '\n'
		default:
			if r == '\n' && i+1 < n && m.text[i+1] == '\n' {
				return true
			}
			return (r == '.' || r == '!' || r == '?') && (i+1 == n || unicode.IsSpace(m.text[i+1]))
		}
	}
	if dir > 0 {
		i := pos
		for i < n && !isDelim(i) {
			i++
		}
		for i < n && (isDelim(i) || unicode.IsSpace(m.text[i])) {
			i++
		}
		return min(i, n)
	}
	i := max(pos-1, 0)
	for i > 0 && (isDelim(i) || unicode.IsSpace(m.text[i])) {
		i--
	}
	for i > 0 && !isDelim(i-1) {
		i--
	}
	for i < n && unicode.IsSpace(m.text[i]) {
		i++
	}
	return i
}

// destination may return the insertion point at EOL/EOF for exclusive
// operators. Cursor movement clamps it onto a character separately.
func (m *Model) destination(key string, count int, operating bool) (int, bool) {
	n := max(count, 1)
	pos := m.cursor
	if kind, target, ok := isFindMotion(key); ok {
		m.lastFindKind, m.lastFindChar = kind, target
		return m.findCharOnLine(pos, kind, target, n)
	}
	if key == ";" || key == "," {
		if m.lastFindKind == "" {
			return pos, false
		}
		kind := m.lastFindKind
		if key == "," {
			kind = invertFindKind(kind)
		}
		return m.findCharOnLine(pos, kind, m.lastFindChar, n)
	}
	switch key {
	case "h":
		return max(m.lineStart(pos), pos-min(pos, n)), true
	case "l":
		end := m.lineEnd(pos)
		return pos + min(end-pos, n), true
	case "0":
		return m.lineStart(pos), true
	case "^":
		return m.firstNonblank(m.lineStart(pos)), true
	case "$":
		return m.normalPos(m.lineEnd(m.linesFrom(pos, 1, n-1))), true
	case "j", "k":
		col := m.column
		if col < 0 || operating {
			col = pos - m.lineStart(pos)
		}
		dir := 1
		if key == "k" {
			dir = -1
		}
		start := m.linesFrom(pos, dir, n)
		return m.normalPos(start + min(col, m.lineEnd(start)-start)), true
	case "gg", "G":
		start := m.lineStart(len(m.text))
		if count > 0 || key == "gg" {
			start = m.linesFrom(0, 1, n-1)
		}
		return m.firstNonblank(start), true
	case "%":
		if count > 0 {
			if count > 100 {
				return pos, false
			}
			lines := 1
			for _, r := range m.text {
				if r == '\n' {
					lines++
				}
			}
			line := (lines/100)*count + ((lines%100)*count+99)/100
			return m.firstNonblank(m.linesFrom(0, 1, line-1)), true
		}
		for i, end := pos, m.lineEnd(pos); i < end; i++ {
			if strings.ContainsRune(opens+closes, m.text[i]) {
				return m.partner(i)
			}
		}
		return pos, false
	}
	for i := 0; i < n; i++ {
		prev := pos
		switch key {
		case "w", "W":
			pos = m.wordNext(pos, key == "W")
		case "b", "B":
			pos = m.wordPrev(pos, key == "B")
		case "e", "E":
			pos = m.wordEnd(pos, key == "E", false)
		case "{":
			pos = m.paragraph(pos, -1)
		case "}":
			pos = m.paragraph(pos, 1)
		case "(":
			pos = m.unitMove(pos, -1)
		case ")":
			pos = m.unitMove(pos, 1)
		default:
			return pos, false
		}
		if pos == prev {
			break
		}
	}
	return pos, true
}

func (m *Model) move(key string, count int) bool {
	if key == "j" || key == "k" {
		if m.column < 0 {
			m.column = m.cursor - m.lineStart(m.cursor)
		}
	} else if key == "$" {
		m.column = maxInt
	} else {
		m.column = -1
	}
	pos, ok := m.destination(key, count, false)
	if ok {
		m.cursor = m.normalPos(pos)
	}
	return true
}

func (m *Model) motionRange(op, key string, count int) (span, bool) {
	if op == key {
		return m.lineRange(m.cursor, m.linesFrom(m.cursor, 1, max(count, 1)-1)), true
	}
	if isObject(key) {
		return m.object(key, count)
	}
	if !isMotion(key) {
		return span{}, false
	}
	if op == "c" && (key == "w" || key == "W") && m.cursor < len(m.text) && !unicode.IsSpace(m.text[m.cursor]) {
		end := m.wordEnd(m.cursor, key == "W", true)
		for i := 1; i < count && end < len(m.text); i++ {
			end = m.wordEnd(end, key == "W", false)
		}
		return span{m.cursor, min(end+1, len(m.text)), false}, true
	}
	pos, ok := m.destination(key, count, true)
	if !ok {
		return span{}, false
	}
	if kind, _, ok := isFindMotion(key); ok {
		if kind == "f" || kind == "t" {
			return span{min(m.cursor, pos), min(max(m.cursor, pos)+1, len(m.text)), false}, true
		}
		return span{min(m.cursor, pos), max(m.cursor, pos), false}, true
	}
	if key == ";" || key == "," {
		eff := m.lastFindKind
		if key == "," {
			eff = invertFindKind(eff)
		}
		if eff == "f" || eff == "t" {
			return span{min(m.cursor, pos), min(max(m.cursor, pos)+1, len(m.text)), false}, true
		}
		return span{min(m.cursor, pos), max(m.cursor, pos), false}, true
	}
	switch key {
	case "j", "k":
		if m.lineStart(m.cursor) == m.lineStart(pos) {
			return span{}, false
		}
		return m.lineRange(m.cursor, pos), true
	case "gg", "G":
		return m.lineRange(m.cursor, pos), true
	case "$":
		return span{m.cursor, m.lineEnd(pos), false}, true
	case "%":
		if count > 0 {
			return m.lineRange(m.cursor, pos), true
		}
		fallthrough
	case "e", "E":
		return span{min(m.cursor, pos), min(max(m.cursor, pos)+1, len(m.text)), false}, true
	case "{", "}":
		// An exclusive motion to column zero becomes linewise when
		// it started in the indentation. The destination line stays.
		lo, hi := min(m.cursor, pos), max(m.cursor, pos)
		if lo < hi && lo <= m.indentEnd(m.lineStart(lo)) && (hi == len(m.text) || hi == m.lineStart(hi)) {
			return m.lineRange(lo, hi-1), true
		}
	case "w", "W":
		// When the last step skips a line break, dw stops at that line's end.
		last := m.cursor
		for i := 1; i < count && last < len(m.text); i++ {
			last = m.wordNext(last, key == "W")
		}
		if end := m.lineEnd(last); last < end && pos > end {
			pos = end
		}
	}
	return span{min(m.cursor, pos), max(m.cursor, pos), false}, true
}
