package ochazuke

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The key echo shows the last few keys the application received, as a
// screencast does: for recordings, demos, screen sharing and bug reports.
// It reads what the dispatcher did with each key (the mode before and
// after it, and the requests it ran, from the action history), never the
// terminal's input, so the keys read in ochazuke's names (ctrl+w, esc,
// space) and group as they were typed:
//
//   - a pending sequence is one entry: 5j, g g, ctrl+w >, d d, " a; while
//     it waits it ends in … (g…);
//   - the same key pressed again counts up: j ×3;
//   - keys typed into an input (an Operation.Typing request) collapse into
//     the text, "hello" (keys.echo-typing text) or typed 5 (count);
//     nothing at all shows while the focus is a secret input
//     (Input.Secret);
//   - a palette line shows as typed, :page open docs, in place of the
//     keys that typed it, once it runs (keys.echo-commands off: :…);
//   - keys a passthrough mode forwards to a pane (a terminal's) show only
//     with keys.echo-terminal; the leader and what follows it always do.
//
// keys.echo places it (off, the default; footer: the right end of the
// footer; badge: beside the mode badge on the title row), keys.echo-width
// is the cells it reserves there, whether or not it holds keys, so the
// line never reflows as keys come and go, keys.echo-count the entries it
// keeps and keys.echo-linger how long one stays (0s: until newer ones push
// it out). The Model's clock times it, so a story's fake clock moves it.

// The key echo settings.
const (
	KeyEchoSetting         = "keys.echo"
	KeyEchoWidthSetting    = "keys.echo-width"
	KeyEchoCountSetting    = "keys.echo-count"
	KeyEchoLingerSetting   = "keys.echo-linger"
	KeyEchoTypingSetting   = "keys.echo-typing"
	KeyEchoCommandsSetting = "keys.echo-commands"
	KeyEchoTerminalSetting = "keys.echo-terminal"
)

// The keys.echo placements, in the order key.echo cycles them.
const (
	KeyEchoOff    = "off"
	KeyEchoFooter = "footer"
	KeyEchoBadge  = "badge"
)

// KeyEchoPlacements are keys.echo's values, in cycle order.
var KeyEchoPlacements = []string{KeyEchoOff, KeyEchoFooter, KeyEchoBadge}

// The keys.echo-typing values.
const (
	KeyEchoTypingText  = "text"
	KeyEchoTypingCount = "count"
)

// keyEchoMinWidth is the narrowest echo worth drawing; keyEchoSep joins
// its entries.
const (
	keyEchoMinWidth = 8
	keyEchoSep      = "  "
)

// KeyEcho is the keys.echo value, off when it is undeclared or holds
// anything else.
func (r *Runtime) KeyEcho() string {
	if v := r.Settings().String(KeyEchoSetting); slices.Contains(KeyEchoPlacements, v) {
		return v
	}
	return KeyEchoOff
}

// KeyEchoCell is the key echo as the shell draws it (Header.Keys,
// Footer.Keys): the entries, oldest first, and the cells it reserves
// (keys.echo-width), which the shell holds to at most half the line.
type KeyEchoCell struct {
	Labels []string
	Width  int
}

// cells is the width the echo takes in a line width cells wide: its own,
// at most half the line; 0 (not drawn) when that leaves too little.
func (c *KeyEchoCell) cells(width int) int {
	if c == nil {
		return 0
	}
	w := min(c.Width, width/2)
	if w < keyEchoMinWidth {
		return 0
	}
	return w
}

// echoEntry is one entry of the echo: a key, a sequence, a run of typed
// text or a palette command.
type echoEntry struct {
	keys    []string // a key or a sequence: the keys as typed
	joins   []string // what goes before each key after the first ("" after a count digit, else " ")
	repeat  int      // the same lone key pressed again: ×repeat
	text    string   // a run of typed text
	typed   int      // the keys in that run
	input   string   // the instance the text went into
	command string   // a palette line, as the palette writes it
	open    bool     // a sequence still waiting for its next key
	counted bool     // the last key of the sequence went into a count
	before  string   // the focus the entry's first key was pressed on
	after   string   // the focus after its last key
	at      time.Time
}

// keyEcho is the Model's memory of the keys it echoes.
type keyEcho struct {
	entries []echoEntry
	tickAt  time.Time // the linger timer in flight, zero when none
}

// echoKey is what observe needs about one key beyond the runtime's state
// after it: the state before it and the history entries it added.
type echoKey struct {
	key    MacroKey
	mode   ModeState
	focus  string
	ran    []HistoryEntry
	nowSeq uint64
	line   string // the palette's line as the key found it: what a command entry shows
}

// observe adds one key the dispatcher handled. The runtime is in the state
// the key left it in.
func (e *keyEcho) observe(r *Runtime, p *Palette, k echoKey, now time.Time) {
	if in := r.Input(k.focus); in != nil && in.Secret {
		e.closeOpen()
		return // a secret input shows nothing, not even how much was typed
	}
	pendingBefore := r.pendingModes[k.mode.Name] || k.mode.Payload.Prefix != ""
	pendingAfter := r.pending()
	// A digit that changed a count joins the key after it (5j); a key that
	// only set a count's default (c's operator count of 1) does not (c c).
	digit := len(k.key.Key) == 1 && unicode.IsDigit(rune(k.key.Key[0]))
	counted := digit && (k.mode.Payload.Count != r.Mode.Payload.Count || k.mode.Payload.OpCount != r.Mode.Payload.OpCount)
	typing, command := false, ""
	hasCommand := false
	for _, h := range k.ran {
		if op, ok := r.Operations[h.Request.Key()]; ok && op.Typing && strings.HasPrefix(h.Context.Source, "key:") {
			typing = true
		}
		if h.Context.Source == "palette" && p != nil && !p.actions[h.Request.Key()] {
			command, hasCommand = strings.TrimSpace(k.line), true
			if command == "" {
				command = p.commandText(h.Request)
			}
		}
	}
	if hasCommand {
		e.command(r, command, k, now)
		return
	}
	if last := e.last(); last != nil && last.open && pendingBefore {
		// The next key of a sequence: the leader's, a count's, g g.
		join := " "
		if last.counted {
			join = ""
		}
		last.keys = append(last.keys, k.key.Key)
		last.joins = append(last.joins, join)
		last.open, last.counted, last.after, last.at = pendingAfter, counted && pendingAfter, r.Focus, now
		return
	}
	e.closeOpen()
	if !pendingBefore && !pendingAfter && typing && r.IsPassthrough(r.baseOfMode(k.mode)) && !r.Settings().Bool(KeyEchoTerminalSetting) {
		return // a key forwarded to a pane: keys.echo-terminal shows them
	}
	if text, ok := typedText(k.key); ok && typing && !pendingAfter {
		if last := e.last(); last != nil && last.input == k.focus && last.typed > 0 {
			last.text += text
			last.typed++
			last.after, last.at = r.Focus, now
			return
		}
		e.push(echoEntry{text: text, typed: 1, input: k.focus, before: k.focus, after: r.Focus, at: now})
		return
	}
	name := k.key.Key
	if k.key.Paste {
		name = "paste"
	}
	if last := e.last(); !pendingAfter && last != nil && len(last.keys) == 1 && !last.open && last.keys[0] == name && last.command == "" {
		last.repeat = max(last.repeat, 1) + 1
		last.after, last.at = r.Focus, now
		return
	}
	e.push(echoEntry{keys: []string{name}, open: pendingAfter, counted: counted && pendingAfter, before: k.focus, after: r.Focus, at: now})
}

// command replaces the keys that opened and typed a palette line with the
// command it ran: the entries pressed inside the palette, and the key that
// opened it, give way to one entry.
func (e *keyEcho) command(r *Runtime, line string, k echoKey, now time.Time) {
	inside := func(id string) bool { return slices.Contains(r.Ancestry(id), paletteID) }
	for len(e.entries) > 0 && inside(e.entries[len(e.entries)-1].before) {
		e.entries = e.entries[:len(e.entries)-1]
	}
	if n := len(e.entries); n > 0 && inside(e.entries[n-1].after) {
		e.entries = e.entries[:n-1]
	}
	e.push(echoEntry{command: line, before: k.focus, after: r.Focus, at: now})
}

// typedText is the text a key types: the character, a space, the pasted
// text; false for a key that is not text (esc, ctrl+w, enter).
func typedText(k MacroKey) (string, bool) {
	switch {
	case k.Paste:
		return pasteText(k.Key), true
	case k.Key == "space":
		return " ", true
	case utf8.RuneCountInString(k.Key) == 1:
		return k.Key, true
	}
	return "", false
}

// baseOfMode is the base mode a mode state reads, as BaseMode does for
// the live mode: a pending mode's return, else its name.
func (r *Runtime) baseOfMode(m ModeState) string {
	if (r.pendingModes[m.Name] || m.Payload.Prefix != "") && m.Payload.Return != "" {
		return m.Payload.Return
	}
	return m.Name
}

func (e *keyEcho) last() *echoEntry {
	if len(e.entries) == 0 {
		return nil
	}
	return &e.entries[len(e.entries)-1]
}

func (e *keyEcho) push(entry echoEntry) { e.entries = append(e.entries, entry) }

// closeOpen ends a sequence that stopped waiting (a timeout, a secret
// input took the focus).
func (e *keyEcho) closeOpen() {
	if last := e.last(); last != nil {
		last.open = false
	}
}

// prune drops the entries past keys.echo-count and those older than
// keys.echo-linger; a sequence still waiting stays.
func (e *keyEcho) prune(r *Runtime, now time.Time) {
	if n := max(1, r.Settings().Int(KeyEchoCountSetting)); len(e.entries) > n {
		e.entries = slices.Delete(e.entries, 0, len(e.entries)-n)
	}
	linger := r.Settings().Duration(KeyEchoLingerSetting)
	if linger <= 0 {
		return
	}
	pending := r.pending()
	e.entries = slices.DeleteFunc(e.entries, func(x echoEntry) bool {
		return !(x.open && pending) && !now.Before(x.at.Add(linger))
	})
}

// lingerCommand schedules the linger timer (a JobLingerMsg, the Model's
// one linger timer) for the oldest entry, unless one is in flight for it.
func (e *keyEcho) lingerCommand(r *Runtime, now time.Time) tea.Cmd {
	linger := r.Settings().Duration(KeyEchoLingerSetting)
	if linger <= 0 || len(e.entries) == 0 {
		return nil
	}
	next := e.entries[0].at.Add(linger)
	for _, x := range e.entries[1:] {
		if at := x.at.Add(linger); at.Before(next) {
			next = at
		}
	}
	if !e.tickAt.IsZero() && !next.Before(e.tickAt) {
		return nil
	}
	if !next.After(now) {
		next = now.Add(time.Millisecond)
	}
	e.tickAt = next
	return tea.Tick(next.Sub(now), func(time.Time) tea.Msg { return JobLingerMsg{At: next} })
}

// fired clears the timer in flight when its message arrives.
func (e *keyEcho) fired(msg JobLingerMsg) {
	if msg.filter == nil && msg.At.Equal(e.tickAt) {
		e.tickAt = time.Time{}
	}
}

// labels are the entries as the echo draws them, oldest first.
func (e *keyEcho) labels(r *Runtime) []string {
	out := make([]string, 0, len(e.entries))
	pending := r.pending()
	for _, x := range e.entries {
		out = append(out, x.label(r, pending))
	}
	return out
}

// label is one entry as drawn.
func (x echoEntry) label(r *Runtime, pending bool) string {
	switch {
	case x.command != "":
		if !r.Settings().Bool(KeyEchoCommandsSetting) {
			return ":…"
		}
		return ":" + x.command
	case x.typed > 0:
		if r.Settings().String(KeyEchoTypingSetting) == KeyEchoTypingCount {
			return fmt.Sprintf("typed %d", x.typed)
		}
		return `"` + safeLine(x.text) + `"`
	}
	var b strings.Builder
	for i, key := range x.keys {
		if i > 0 {
			b.WriteString(x.joins[i-1])
		}
		b.WriteString(key)
	}
	if x.repeat > 1 {
		fmt.Fprintf(&b, " ×%d", x.repeat)
	}
	if x.open && pending {
		b.WriteString("…")
	}
	return safeLine(b.String())
}

// KeyEchoLabels are the entries the key echo shows now, oldest first: none
// while keys.echo is off.
func (m *Model) KeyEchoLabels() []string {
	if m.Runtime.KeyEcho() == KeyEchoOff {
		return nil
	}
	return m.echo.labels(m.Runtime)
}

// echoCell draws the echo's entries into exactly width cells, right
// aligned, the newest at the right edge in the accent and the older ones
// muted; entries that do not fit are dropped from the oldest, with … in
// their place, and a newest entry wider than the cell is cut with ….
func (l Layout) echoCell(labels []string, width int) string {
	if width <= 0 {
		return ""
	}
	s := l.Styles
	n, used := 0, 0
	for i := len(labels) - 1; i >= 0; i-- {
		w := ansi.StringWidth(modalLabel(labels[i]))
		if n > 0 {
			w += ansi.StringWidth(keyEchoSep)
		}
		// Room for the … that says older entries were dropped.
		lead := 0
		if i > 0 {
			lead = ansi.StringWidth("…" + keyEchoSep)
		}
		if used+w+lead > width && n > 0 {
			break
		}
		n, used = n+1, used+w
	}
	shown := labels[len(labels)-n:]
	var parts []string
	if n < len(labels) && n > 0 {
		parts = append(parts, s.Muted.Render("…"))
	}
	for i, label := range shown {
		text := modalLabel(label)
		if i == len(shown)-1 {
			parts = append(parts, s.Accent.Render(text))
		} else {
			parts = append(parts, s.Muted.Render(text))
		}
	}
	out := strings.Join(parts, keyEchoSep)
	if ansi.StringWidth(out) > width {
		out = ansi.Truncate(out, width-1, "") + s.Muted.Render("…")
		if ansi.StringWidth(out) > width {
			out = ansi.Truncate(out, width, "")
		}
	}
	return strings.Repeat(" ", max(0, width-ansi.StringWidth(out))) + out
}

// registerKeyEcho registers key.echo, the verb that sets or cycles the
// keys.echo setting (as focus.display does focus-display).
func registerKeyEcho(r *Runtime) {
	r.Register(Operation{Verb: "echo", Noun: "ochazuke.key", Description: "Show the keys being pressed (the keys.echo setting): off, footer or badge; with no placement, the next in that order", Parameters: map[string]Parameter{"placement": {Type: "string", Enum: slices.Clone(KeyEchoPlacements), Description: "off, footer (the right end of the footer) or badge (beside the mode badge) (default: the next after the current)"}}, Result: "the keys.echo value", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		placement := StringArg(a, "placement", "")
		if placement == "" {
			i := slices.Index(KeyEchoPlacements, r.KeyEcho())
			placement = KeyEchoPlacements[(i+1)%len(KeyEchoPlacements)]
		}
		if err := r.Settings().Set(KeyEchoSetting, placement); err != nil {
			return nil, err
		}
		return placement, nil
	}})
}

// historySince is the history entries recorded after seq, oldest first:
// what one key ran.
func (r *Runtime) historySince(seq uint64) []HistoryEntry {
	var out []HistoryEntry
	for _, ring := range [][]HistoryEntry{r.history.entries, r.history.fetches} {
		for i := len(ring) - 1; i >= 0 && ring[i].Seq > seq; i-- {
			out = append(out, ring[i])
		}
	}
	slices.SortFunc(out, func(a, b HistoryEntry) int { return cmp.Compare(a.Seq, b.Seq) })
	return out
}
