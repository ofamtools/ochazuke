package input

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

// CopyMode wraps a read-only input.Model over ANSI-stripped plain text while
// preserving the original styled ANSI lines for styled rendering and yanking.
type CopyMode struct {
	model       *Model
	styledLines []string
	plainLines  []string
	lineStarts  []int // rune offset of each line in model.text

	searching   bool
	searchBack  bool
	searchQuery string
	lastQuery   string
	lastBack    bool

	yankPlain  string
	yankStyled string
	hasYank    bool
}

// NewCopyMode creates a read-only CopyMode over styledLines.
func NewCopyMode(styledLines []string) *CopyMode {
	styled := slices.Clone(styledLines)
	plain := make([]string, len(styled))
	starts := make([]int, len(styled))
	offset := 0
	for i, ln := range styled {
		clean := stripANSI(ln)
		plain[i] = clean
		starts[i] = offset
		offset += utf8.RuneCountInString(clean) + 1
	}
	m := New(strings.Join(plain, "\n"), true)
	return &CopyMode{
		model:       m,
		styledLines: styled,
		plainLines:  plain,
		lineStarts:  starts,
	}
}

// SetWordScope sets the word boundary scope on the underlying model.
func (cm *CopyMode) SetWordScope(scope string) { cm.model.SetWordScope(scope) }

// Mode returns the current Vim mode (normal, visual, or visual-line).
func (cm *CopyMode) Mode() Mode { return cm.model.Mode() }

// Searching reports whether an interactive search prompt (/ or ?) is active.
func (cm *CopyMode) Searching() bool { return cm.searching }

// SearchQuery returns the active search prompt query (or last executed query).
func (cm *CopyMode) SearchQuery() string {
	if cm.searching {
		return cm.searchQuery
	}
	return cm.lastQuery
}

// Value returns the plain ANSI-stripped text snapshot.
func (cm *CopyMode) Value() string { return cm.model.Value() }

// YankPlain returns the plain text from the most recent yank.
func (cm *CopyMode) YankPlain() string { return cm.yankPlain }

// YankStyled returns the styled ANSI text from the most recent yank,
// preserving SGR styling across the yanked rune span.
func (cm *CopyMode) YankStyled() string { return cm.yankStyled }

// CursorLineCol returns the 0-based (line, col) rune position of the cursor.
func (cm *CopyMode) CursorLineCol() (line, col int) {
	return cm.offsetToLineCol(cm.model.cursor)
}

func (cm *CopyMode) offsetToLineCol(off int) (line, col int) {
	if len(cm.lineStarts) == 0 {
		return 0, 0
	}
	off = max(0, min(off, len(cm.model.text)))
	// Binary search the last lineStart <= off.
	lo, hi := 0, len(cm.lineStarts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if cm.lineStarts[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo, off - cm.lineStarts[lo]
}

// Search jumps the cursor to the next occurrence of query (wrapping around).
func (cm *CopyMode) Search(query string, backward bool) bool {
	if query == "" {
		return false
	}
	cm.lastQuery = query
	cm.lastBack = backward
	return cm.jumpSearch(query, backward)
}

func (cm *CopyMode) jumpSearch(query string, backward bool) bool {
	if query == "" || len(cm.model.text) == 0 {
		return false
	}
	q := []rune(query)
	text := cm.model.text
	n := len(text)
	qn := len(q)
	if qn > n {
		return false
	}
	matchAt := func(pos int) bool {
		for j := 0; j < qn; j++ {
			if text[pos+j] != q[j] {
				return false
			}
		}
		return true
	}
	if !backward {
		start := cm.model.cursor + 1
		for i := start; i+qn <= n; i++ {
			if matchAt(i) {
				cm.model.cursor = cm.model.normalPos(i)
				return true
			}
		}
		for i := 0; i < min(start, n-qn+1); i++ {
			if matchAt(i) {
				cm.model.cursor = cm.model.normalPos(i)
				return true
			}
		}
		return false
	}
	start := min(cm.model.cursor-1, n-qn)
	for i := start; i >= 0; i-- {
		if matchAt(i) {
			cm.model.cursor = cm.model.normalPos(i)
			return true
		}
	}
	for i := n - qn; i > start && i >= 0; i-- {
		if matchAt(i) {
			cm.model.cursor = cm.model.normalPos(i)
			return true
		}
	}
	return false
}

// HandleKey processes a Vim navigation, visual, search, or yank key while
// refusing mutating edits cleanly.
func (cm *CopyMode) HandleKey(key string) bool {
	return cm.handleKeyText(key, "")
}

func (cm *CopyMode) handleKeyText(key, text string) bool {
	if cm.searching {
		switch key {
		case "esc":
			cm.searching = false
			cm.searchQuery = ""
			return true
		case "enter":
			q := cm.searchQuery
			back := cm.searchBack
			cm.searching = false
			cm.searchQuery = ""
			if q != "" {
				cm.lastQuery = q
				cm.lastBack = back
				cm.jumpSearch(q, back)
			}
			return true
		case "backspace":
			rs := []rune(cm.searchQuery)
			if len(rs) > 0 {
				cm.searchQuery = string(rs[:len(rs)-1])
			}
			return true
		case "space":
			cm.searchQuery += " "
			return true
		default:
			if text != "" {
				cm.searchQuery += text
				return true
			}
			if utf8.RuneCountInString(key) == 1 {
				cm.searchQuery += key
				return true
			}
			return false
		}
	}

	p := cm.model.pending
	if p.prefix == "" && p.op == "" {
		switch key {
		case "/", "?":
			cm.searching = true
			cm.searchBack = key == "?"
			cm.searchQuery = ""
			return true
		case "n":
			if cm.lastQuery == "" {
				return true
			}
			cm.jumpSearch(cm.lastQuery, cm.lastBack)
			return true
		case "N":
			if cm.lastQuery == "" {
				return true
			}
			cm.jumpSearch(cm.lastQuery, !cm.lastBack)
			return true
		}
	}

	if !cm.allowKey(key) {
		return false
	}

	cm.model.hasLastYank = false
	handled := cm.model.HandleKey(key)
	if cm.model.hasLastYank {
		cm.captureYank(cm.model.lastYank)
		cm.model.hasLastYank = false
	}
	return handled
}

func (cm *CopyMode) allowKey(key string) bool {
	p := cm.model.pending
	if key == "esc" {
		return true
	}
	if p.prefix == `"` {
		return true
	}
	if p.prefix == "g" {
		return key == "g"
	}
	if p.prefix == "i" || p.prefix == "a" {
		return isObject(p.prefix + key)
	}
	if len(key) == 1 && key[0] >= '0' && key[0] <= '9' {
		return true
	}
	if p.op == "y" {
		return key == "y" || key == "g" || key == `"` || key == "i" || key == "a" || isMotion(key) || isObject(key)
	}
	if cm.model.isVisual() {
		if key == "i" || key == "a" || key == "y" || key == "v" || key == "V" || key == "g" || key == `"` || isMotion(key) || isObject(key) {
			return true
		}
		return false
	}
	switch key {
	case "v", "V", "y", "g", `"`:
		return true
	}
	return isMotion(key)
}

// HandleCommand processes resolved commands while refusing mutating edits.
func (cm *CopyMode) HandleCommand(cmd core.Command) bool {
	switch cmd.ID {
	case CmdKey:
		return cm.handleKeyText(cmd.Arg("key"), cmd.Arg("text"))
	case CmdEscape:
		return cm.HandleKey("esc")
	case CmdYank:
		cm.model.hasLastYank = false
		ok := cm.model.HandleCommand(cmd)
		if cm.model.hasLastYank {
			cm.captureYank(cm.model.lastYank)
			cm.model.hasLastYank = false
		}
		return ok
	}
	if key, ok := commandKeys[cmd.ID]; ok && isMotion(key) {
		cm.model.hasLastYank = false
		handled := cm.model.HandleCommand(cmd)
		if cm.model.hasLastYank {
			cm.captureYank(cm.model.lastYank)
			cm.model.hasLastYank = false
		}
		return handled
	}
	return false
}

func (cm *CopyMode) captureYank(r span) {
	cm.hasYank = true
	if r.lo < 0 {
		r.lo = 0
	}
	if r.hi > len(cm.model.text) {
		r.hi = len(cm.model.text)
	}
	if r.hi < r.lo {
		r.hi = r.lo
	}
	cm.yankPlain = string(cm.model.text[r.lo:r.hi])
	cm.yankStyled = cm.sliceStyled(r)
}

func (cm *CopyMode) sliceStyled(r span) string {
	if len(cm.styledLines) == 0 {
		return ""
	}
	startLine, startCol := cm.offsetToLineCol(r.lo)
	endLine, endCol := cm.offsetToLineCol(r.hi)
	if r.linewise {
		lines := make([]string, 0, endLine-startLine+1)
		for l := startLine; l <= endLine && l < len(cm.styledLines); l++ {
			lines = append(lines, cm.styledLines[l])
		}
		return strings.Join(lines, "\n")
	}
	if startLine == endLine {
		if startLine >= len(cm.styledLines) {
			return ""
		}
		return sliceANSIRunes(cm.styledLines[startLine], startCol, endCol)
	}
	var parts []string
	if startLine < len(cm.styledLines) {
		n := utf8.RuneCountInString(cm.plainLines[startLine])
		parts = append(parts, sliceANSIRunes(cm.styledLines[startLine], startCol, n))
	}
	for l := startLine + 1; l < endLine && l < len(cm.styledLines); l++ {
		parts = append(parts, cm.styledLines[l])
	}
	if endLine < len(cm.styledLines) {
		parts = append(parts, sliceANSIRunes(cm.styledLines[endLine], 0, endCol))
	}
	return strings.Join(parts, "\n")
}

// View renders the styled terminal snapshot as a ui.KindRaw node with cursor.
func (cm *CopyMode) View(id string) ui.ViewNode {
	line, col := cm.CursorLineCol()
	display := cm.styledLines
	if cm.model.isVisual() && len(cm.styledLines) > 0 {
		r := cm.model.visualRange()
		display = cm.highlightVisual(r)
	}
	ansi := strings.Join(display, "\n")
	h := fnv.New64a()
	fmt.Fprintf(h, "%s|%d|%d|%t|%s|%d|", cm.Mode(), line, col, cm.searching, cm.searchQuery, cm.model.anchor)
	h.Write([]byte(ansi))
	return ui.ViewNode{
		ID:        id,
		Kind:      ui.KindRaw,
		Scope:     "copymode",
		Focusable: true,
		Props: map[string]any{
			"ansi":   ansi,
			"cursor": map[string]any{"x": col, "y": line, "shape": "block"},
		},
		Facts: map[string]any{
			"mode":        string(cm.Mode()),
			"searching":   cm.searching,
			"searchQuery": cm.searchQuery,
			"line":        line,
			"col":         col,
			"hasYank":     cm.hasYank,
		},
		CacheKey: max(h.Sum64(), 1),
	}
}

func (cm *CopyMode) highlightVisual(r span) []string {
	out := slices.Clone(cm.styledLines)
	startLine, startCol := cm.offsetToLineCol(r.lo)
	endLine, endCol := cm.offsetToLineCol(r.hi)
	for l := startLine; l <= endLine && l < len(out); l++ {
		plainLen := utf8.RuneCountInString(cm.plainLines[l])
		lo, hi := 0, plainLen
		if !r.linewise {
			if l == startLine {
				lo = startCol
			}
			if l == endLine {
				hi = endCol
			}
		}
		if lo >= hi {
			continue
		}
		before := sliceANSIRunes(out[l], 0, lo)
		mid := sliceANSIRunes(out[l], lo, hi)
		after := sliceANSIRunes(out[l], hi, plainLen)
		out[l] = before + "\x1b[7m" + mid + "\x1b[27m" + after
	}
	return out
}

// ansiSeqLen returns the byte length of an ANSI escape sequence starting at s[i],
// or 0 if s[i] does not begin one.
func ansiSeqLen(s string, i int) int {
	if i >= len(s) || s[i] != '\x1b' {
		return 0
	}
	if i+1 >= len(s) {
		return 1
	}
	switch s[i+1] {
	case '[': // CSI: ESC [ params intermediates final (@-~)
		j := i + 2
		for j < len(s) {
			b := s[j]
			if b >= 0x40 && b <= 0x7e {
				return j - i + 1
			}
			j++
		}
		return len(s) - i
	case ']': // OSC: ESC ] ... (BEL or ST)
		j := i + 2
		for j < len(s) {
			if s[j] == '\x07' {
				return j - i + 1
			}
			if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
				return j - i + 2
			}
			j++
		}
		return len(s) - i
	default:
		return 2
	}
}

func isSGR(seq string) bool {
	return len(seq) >= 3 && seq[0] == '\x1b' && seq[1] == '[' && seq[len(seq)-1] == 'm'
}

func isSGRReset(seq string) bool {
	return seq == "\x1b[0m" || seq == "\x1b[m"
}

func stripANSI(s string) string {
	if !strings.ContainsRune(s, '\x1b') {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); {
		if n := ansiSeqLen(s, i); n > 0 {
			i += n
			continue
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		sb.WriteRune(r)
		i += sz
	}
	return sb.String()
}

// sliceANSIRunes extracts the visible rune range [lo, hi) from an ANSI-styled
// line, re-emitting any active SGR sequences at the start and resetting at the
// end if styling remains open.
func sliceANSIRunes(s string, lo, hi int) string {
	if lo >= hi || s == "" {
		return ""
	}
	var sb strings.Builder
	var active []string
	started := false
	runeIdx := 0
	for i := 0; i < len(s); {
		if n := ansiSeqLen(s, i); n > 0 {
			seq := s[i : i+n]
			if isSGR(seq) {
				if isSGRReset(seq) {
					active = active[:0]
				} else {
					active = append(active, seq)
				}
			}
			if started {
				sb.WriteString(seq)
			}
			i += n
			continue
		}
		r, sz := utf8.DecodeRuneInString(s[i:])
		if runeIdx >= hi {
			break
		}
		if runeIdx >= lo {
			if !started {
				for _, seq := range active {
					sb.WriteString(seq)
				}
				started = true
			}
			sb.WriteRune(r)
		}
		runeIdx++
		i += sz
	}
	if started && len(active) > 0 {
		sb.WriteString("\x1b[0m")
	}
	return sb.String()
}
