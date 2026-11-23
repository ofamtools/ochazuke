// Package commandline is a pure Vim-style bottom command bar: a prefix
// (":", "/", "?", "!", "|", "r!"), a single-line editor with register paste,
// per-prefix history, tab completion, context variable expansion and
// submission parsing. It performs no I/O; hosts supply registers and
// completions through callbacks.
package commandline

import (
	"fmt"
	"hash/fnv"
	"strings"
	"unicode"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/input"
)

// Prefix selects what the command line means.
type Prefix string

// Prefixes.
const (
	PrefixCommand    Prefix = ":"
	PrefixSearch     Prefix = "/"
	PrefixSearchBack Prefix = "?"
	PrefixShell      Prefix = "!"
	PrefixPipe       Prefix = "|"
	PrefixReadShell  Prefix = "r!"
)

// Command IDs handled by Model.HandleCommand. Motion IDs reuse the shared
// motion.* vocabulary.
const (
	CmdInsert       = "input.insert" // Args["text"]
	CmdBackspace    = "input.backspace"
	CmdDelete       = "input.delete.char"
	CmdDeleteWord   = "commandline.delete.word"
	CmdClear        = "commandline.clear"
	CmdRegister     = "commandline.register" // ctrl+r
	CmdHistoryPrev  = "commandline.history.prev"
	CmdHistoryNext  = "commandline.history.next"
	CmdCompleteNext = "commandline.complete.next"
	CmdCompletePrev = "commandline.complete.prev"
	CmdSubmit       = "commandline.submit"
	CmdCancel       = "commandline.cancel"
)

// Submission is a parsed committed command line.
type Submission struct {
	Prefix   Prefix
	Raw      string   // text as typed
	Expanded string   // Raw with variables expanded (shell-safe for shell prefixes)
	Command  string   // ":" only: first word of Expanded
	Args     []string // ":" only: remaining words of Expanded
	PipeCmd  string   // "|" only: shell command fed the subject
	ShellCmd string   // "!" and "r!": shell command to run
}

// Model is the command-line state.
type Model struct {
	Prefix  Prefix
	Text    []rune
	CursorX int // rune offset
	kill    string

	PendingRegister bool
	RegisterLookup  func(reg rune) (string, bool)

	History map[Prefix][]string
	histIdx int // -1 = not browsing
	draft   string

	Completer func(prefix Prefix, text string) []string
	comp      []string
	compIdx   int // -1 = none selected
	compBase  string

	// Vars feed ExpandVars on Submit.
	Vars map[string]string
}

// New returns an open command line with prefix p.
func New(p Prefix) *Model {
	return &Model{Prefix: p, History: map[Prefix][]string{}, histIdx: -1, compIdx: -1}
}

// Open resets the line for prefix p, keeping history and callbacks.
func (m *Model) Open(p Prefix) {
	m.Prefix = p
	m.setText("")
	m.PendingRegister = false
	m.histIdx = -1
	m.draft = ""
	m.resetComp()
}

// Value returns the current text.
func (m *Model) Value() string { return string(m.Text) }

func (m *Model) setText(s string) {
	m.Text = []rune(s)
	m.CursorX = len(m.Text)
}

func (m *Model) resetComp() { m.comp, m.compIdx, m.compBase = nil, -1, "" }

// edited marks the text as user-edited: it ends history browsing and completion.
func (m *Model) edited() {
	m.histIdx = -1
	m.resetComp()
}

// Insert inserts s at the cursor.
func (m *Model) Insert(s string) {
	if s == "" {
		return
	}
	rs := []rune(s)
	out := make([]rune, 0, len(m.Text)+len(rs))
	out = append(out, m.Text[:m.CursorX]...)
	out = append(out, rs...)
	out = append(out, m.Text[m.CursorX:]...)
	m.Text = out
	m.CursorX += len(rs)
	m.edited()
}

// InsertRune inserts r, or, when a register paste is pending, looks r up as a
// register name and inserts its text instead.
func (m *Model) InsertRune(r rune) {
	if m.PendingRegister {
		m.PendingRegister = false
		if m.RegisterLookup != nil {
			if s, ok := m.RegisterLookup(r); ok {
				m.Insert(strings.ReplaceAll(s, "\n", " "))
			}
		}
		return
	}
	m.Insert(string(r))
}

// Left, Right, Home and End move the cursor.
func (m *Model) Left()  { m.CursorX = max(0, m.CursorX-1) }
func (m *Model) Right() { m.CursorX = min(len(m.Text), m.CursorX+1) }
func (m *Model) Home()  { m.CursorX = 0 }
func (m *Model) End()   { m.CursorX = len(m.Text) }

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

// WordNext moves to the start of the next word.
func (m *Model) WordNext() {
	i, n := m.CursorX, len(m.Text)
	if i < n && isWord(m.Text[i]) {
		for i < n && isWord(m.Text[i]) {
			i++
		}
	} else if i < n && !unicode.IsSpace(m.Text[i]) {
		for i < n && !isWord(m.Text[i]) && !unicode.IsSpace(m.Text[i]) {
			i++
		}
	}
	for i < n && unicode.IsSpace(m.Text[i]) {
		i++
	}
	m.CursorX = i
}

// WordPrev moves to the start of the previous word.
func (m *Model) WordPrev() {
	i := m.CursorX
	for i > 0 && unicode.IsSpace(m.Text[i-1]) {
		i--
	}
	if i > 0 && isWord(m.Text[i-1]) {
		for i > 0 && isWord(m.Text[i-1]) {
			i--
		}
	} else {
		for i > 0 && !isWord(m.Text[i-1]) && !unicode.IsSpace(m.Text[i-1]) {
			i--
		}
	}
	m.CursorX = i
}

func (m *Model) cut(from, to int) {
	m.Text = append(m.Text[:from:from], m.Text[to:]...)
	m.CursorX = from
	m.edited()
}

// Backspace deletes the rune before the cursor.
func (m *Model) Backspace() {
	if m.CursorX > 0 {
		m.cut(m.CursorX-1, m.CursorX)
	}
}

// Delete deletes the rune under the cursor.
func (m *Model) Delete() {
	if m.CursorX < len(m.Text) {
		m.cut(m.CursorX, m.CursorX+1)
	}
}

// DeleteWord deletes the word before the cursor (ctrl+w).
func (m *Model) DeleteWord() {
	end := m.CursorX
	m.WordPrev()
	if m.CursorX < end {
		m.kill = string(m.Text[m.CursorX:end])
	}
	m.cut(m.CursorX, end)
}

// DeleteWordForward deletes the word after the cursor (alt+d).
func (m *Model) DeleteWordForward() {
	start := m.CursorX
	m.WordNext()
	end := m.CursorX
	m.CursorX = start
	if start < end {
		m.kill = string(m.Text[start:end])
		m.cut(start, end)
	}
}

// Clear deletes everything before the cursor (ctrl+u).
func (m *Model) Clear() {
	if m.CursorX > 0 {
		m.kill = string(m.Text[:m.CursorX])
	}
	m.cut(0, m.CursorX)
}

// KillToEnd deletes everything from the cursor to the end of the line (ctrl+k).
func (m *Model) KillToEnd() {
	if m.CursorX < len(m.Text) {
		m.kill = string(m.Text[m.CursorX:])
		m.cut(m.CursorX, len(m.Text))
	}
}

// Transpose swaps the two characters before the cursor (ctrl+t).
func (m *Model) Transpose() {
	if len(m.Text) < 2 || m.CursorX == 0 {
		return
	}
	pos := m.CursorX
	if pos == len(m.Text) {
		pos--
	}
	m.Text[pos-1], m.Text[pos] = m.Text[pos], m.Text[pos-1]
	m.CursorX = min(pos+1, len(m.Text))
	m.edited()
}

// YankKill inserts the last killed text at the cursor (ctrl+y).
func (m *Model) YankKill() {
	if m.kill != "" {
		m.Insert(m.kill)
	}
}

// StartRegister arms register paste (ctrl+r); the next rune names the register.
func (m *Model) StartRegister() { m.PendingRegister = true }

// HistoryPrev recalls the previous older entry for the current prefix.
func (m *Model) HistoryPrev() {
	h := m.History[m.Prefix]
	if len(h) == 0 {
		return
	}
	if m.histIdx == -1 {
		m.draft = m.Value()
		m.histIdx = len(h) - 1
	} else if m.histIdx > 0 {
		m.histIdx--
	}
	m.setText(h[m.histIdx])
	m.resetComp()
}

// HistoryNext recalls the next newer entry, ending on the saved draft.
func (m *Model) HistoryNext() {
	h := m.History[m.Prefix]
	if m.histIdx == -1 {
		return
	}
	m.histIdx++
	if m.histIdx >= len(h) {
		m.histIdx = -1
		m.setText(m.draft)
	} else {
		m.setText(h[m.histIdx])
	}
	m.resetComp()
}

// Record appends a committed line to the prefix's history unless empty or equal
// to the previous entry.
func (m *Model) Record(p Prefix, s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	if m.History == nil {
		m.History = map[Prefix][]string{}
	}
	h := m.History[p]
	if len(h) > 0 && h[len(h)-1] == s {
		return
	}
	m.History[p] = append(h, s)
}

func (m *Model) complete(step int) {
	if m.Completer == nil {
		return
	}
	if m.comp == nil {
		m.compBase = m.Value()
		m.comp = m.Completer(m.Prefix, m.compBase)
		m.compIdx = -1
	}
	if len(m.comp) == 0 {
		m.comp = nil
		return
	}
	n := len(m.comp)
	switch {
	case m.compIdx == -1 && step > 0:
		m.compIdx = 0
	case m.compIdx == -1:
		m.compIdx = n - 1
	default:
		m.compIdx = ((m.compIdx+step)%n + n) % n
	}
	m.setText(m.comp[m.compIdx])
}

// CompleteNext cycles forward through completions of the text as first tabbed.
func (m *Model) CompleteNext() { m.complete(1) }

// CompletePrev cycles backward.
func (m *Model) CompletePrev() { m.complete(-1) }

// Hint returns the completion that the next CompleteNext would insert, or "".
func (m *Model) Hint() string {
	if m.comp != nil {
		return m.comp[(m.compIdx+1)%len(m.comp)]
	}
	if m.Completer == nil || m.Value() == "" {
		return ""
	}
	if c := m.Completer(m.Prefix, m.Value()); len(c) > 0 {
		return c[0]
	}
	return ""
}

// Submit parses the current line (using Vars), records it in history and
// returns the submission. The line is left unchanged; call Open to reset.
func (m *Model) Submit() Submission {
	raw := m.Value()
	m.Record(m.Prefix, raw)
	m.histIdx = -1
	m.resetComp()
	return ParseSubmission(m.Prefix, raw, m.Vars)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ExpandVars expands $NAME and ${NAME} from vars verbatim (raw). Unknown names
// and a lone "$" are left untouched; "$$" yields a literal "$". The well-known
// names are FILE, DIR, REF, ID and TITLE, but any key in vars works.
func ExpandVars(raw string, vars map[string]string) string { return expand(raw, vars, false) }

// ExpandVarsShell is ExpandVars with each substituted value single-quoted for
// a POSIX shell.
func ExpandVarsShell(raw string, vars map[string]string) string { return expand(raw, vars, true) }

func expand(raw string, vars map[string]string, quote bool) string {
	rs := []rune(raw)
	var b strings.Builder
	sub := func(name string, orig string) {
		v, ok := vars[name]
		if !ok {
			b.WriteString(orig)
			return
		}
		if quote {
			v = shellQuote(v)
		}
		b.WriteString(v)
	}
	for i := 0; i < len(rs); {
		if rs[i] != '$' || i+1 >= len(rs) {
			b.WriteRune(rs[i])
			i++
			continue
		}
		switch c := rs[i+1]; {
		case c == '$':
			b.WriteRune('$')
			i += 2
		case c == '{':
			j := i + 2
			for j < len(rs) && rs[j] != '}' {
				j++
			}
			if j >= len(rs) {
				b.WriteString(string(rs[i:]))
				i = len(rs)
				break
			}
			sub(string(rs[i+2:j]), string(rs[i:j+1]))
			i = j + 1
		case isWord(c) && !unicode.IsDigit(c):
			j := i + 1
			for j < len(rs) && isWord(rs[j]) {
				j++
			}
			sub(string(rs[i+1:j]), string(rs[i:j]))
			i = j
		default:
			b.WriteRune('$')
			i++
		}
	}
	return b.String()
}

// ParseSubmission classifies a committed line. Shell-bearing prefixes
// ("!", "|", "r!") expand variables shell-safely; ":" and searches expand raw.
func ParseSubmission(p Prefix, text string, vars map[string]string) Submission {
	s := Submission{Prefix: p, Raw: text}
	switch p {
	case PrefixShell, PrefixPipe, PrefixReadShell:
		s.Expanded = strings.TrimSpace(ExpandVarsShell(text, vars))
		if p == PrefixPipe {
			s.PipeCmd = s.Expanded
		} else {
			s.ShellCmd = s.Expanded
		}
	case PrefixSearch, PrefixSearchBack:
		s.Expanded = text
	default:
		s.Expanded = strings.TrimSpace(ExpandVars(text, vars))
		f := strings.Fields(s.Expanded)
		if len(f) > 0 {
			s.Command, s.Args = f[0], f[1:]
		}
	}
	return s
}

// HandleCommand applies a command-line command. handled reports whether cmd
// was understood; the Submission is non-zero only for CmdSubmit.
func (m *Model) HandleCommand(cmd core.Command) (Submission, bool) {
	switch cmd.ID {
	case CmdInsert:
		t := cmd.Arg("text")
		if m.PendingRegister {
			for _, r := range t {
				m.InsertRune(r)
				break
			}
		} else {
			m.Insert(t)
		}
	case CmdBackspace:
		m.PendingRegister = false
		m.Backspace()
	case CmdDelete:
		m.Delete()
	case CmdDeleteWord, input.CmdKillWordBack:
		m.DeleteWord()
	case input.CmdKillWordForward:
		m.DeleteWordForward()
	case CmdClear, input.CmdKillLineBack:
		m.Clear()
	case input.CmdKillLineForward:
		m.KillToEnd()
	case input.CmdTranspose:
		m.Transpose()
	case input.CmdYankKill:
		m.YankKill()
	case CmdRegister:
		m.StartRegister()
	case "motion.left", input.CmdCharLeft:
		m.Left()
	case "motion.right", input.CmdCharRight:
		m.Right()
	case "motion.start", "motion.home", input.CmdLineStart:
		m.Home()
	case "motion.end", input.CmdLineEnd:
		m.End()
	case "motion.word.next", input.CmdWordRight:
		m.WordNext()
	case "motion.word.prev", input.CmdWordLeft:
		m.WordPrev()
	case CmdHistoryPrev, "motion.up":
		m.HistoryPrev()
	case CmdHistoryNext, "motion.down":
		m.HistoryNext()
	case CmdCompleteNext:
		m.CompleteNext()
	case CmdCompletePrev:
		m.CompletePrev()
	case CmdCancel:
		m.PendingRegister = false
		m.Open(m.Prefix)
	case CmdSubmit:
		return m.Submit(), true
	default:
		return Submission{}, false
	}
	return Submission{}, true
}

// View builds the focusable input node. The prompt (prefix, plus a "\"" while
// a register paste is pending) is the node title; the renderer draws the
// hardware cursor at Props["cursor"].
func (m *Model) View(id string) ui.ViewNode {
	prompt := string(m.Prefix)
	if m.PendingRegister {
		prompt += `"`
	}
	value, hint := m.Value(), m.Hint()
	h := fnv.New64a()
	fmt.Fprintf(h, "%s/%d/%t/%s/%s", m.Prefix, m.CursorX, m.PendingRegister, hint, value)
	props := map[string]any{"value": value, "cursor": m.CursorX}
	if hint != "" {
		props["hint"] = hint
	}
	return ui.ViewNode{ID: id, Kind: ui.KindInput, Scope: "commandline", Focusable: true,
		Title: prompt, Props: props,
		Facts: map[string]any{
			"prefix": string(m.Prefix), "pendingRegister": m.PendingRegister,
			"empty": len(m.Text) == 0, "completing": m.comp != nil, "browsingHistory": m.histIdx != -1,
		},
		CacheKey: max(h.Sum64(), 1)}
}
