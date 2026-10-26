package ochazuke

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The suggestion list of inline completion. One Completer component, on
// the __completion instance under app, follows the focused input: after
// every event (PendingCommand) it looks at the input's text and cursor,
// and while the input is in insert mode with the cursor in a trigger token
// that was typed (or opened with tab), it asks the token's search after a
// pause (Debounce), as a job off the loop. The list opens when the first
// search starts and is drawn under the token by the Model.
//
// While the list is open the input's instance names the component as its
// inner level (Instance.inner), so the completion noun's rows answer
// before the input's own and every outer level, and typing, which no
// completion row binds, still edits the input. The rows: ctrl+n/ctrl+p and
// down/up move the highlight, tab takes the highlighted suggestion (else
// the first), enter takes it only once one is highlighted (so enter typed
// after a token still submits or breaks the line), esc hides the list and
// leaves the mode alone. With the list closed, tab on a trigger token
// opens it at once (an editor row, guarded so tab is untouched elsewhere).
//
// Answers are numbered by request: one answering an older request than
// the answer on screen is dropped (out-of-order answers never replace
// newer ones), and the searches it could no longer beat are cancelled.

// completionID is the completion component's instance.
const completionID = "__completion"

// CompletionNoun is the noun whose rows answer while suggestions are open.
const CompletionNoun = "completion"

// DefaultCompletionDebounce is the pause after the last edit before an
// input's search is asked.
const DefaultCompletionDebounce = 150 * time.Millisecond

// completionRows is the most suggestions the list draws at once.
const completionRows = 8

// CompletionDebounceMsg is the pause timer of one request: when it is the
// latest (Completer.DebounceCurrent) the search is started.
type CompletionDebounceMsg struct {
	Seq uint64
}

// completionAnswer is what a search job delivers: the request it answers
// and the search's result. The search's error rides inside, so it shows in
// the list rather than as the key's error.
type completionAnswer struct {
	seq  uint64
	rows []Suggestion
	err  error
}

// Completer is the inline completion component: which input it follows,
// the token whose suggestions are wanted and the answer on screen.
type Completer struct {
	r *Runtime
	// Debounce is the pause before a search is asked
	// (DefaultCompletionDebounce when 0).
	Debounce time.Duration

	input    string // the focused input it follows
	seen     string // that input's text and cursor at the last look
	text     string // its text then, to tell an edit from a move
	tracking bool   // a token's suggestions are wanted
	token    CompletionToken
	search   CompletionSearch
	open     bool // the list is shown and its rows answer
	tickDue  bool // a pause timer is to be scheduled at the next drain

	seq      uint64 // the latest request
	launched uint64 // the latest request whose search started
	shown    uint64 // the request the answer on screen answers
	floor    uint64 // answers to requests at or below this are dropped (dismissed)
	jobs     map[uint64]uint64

	rows   []Suggestion
	err    error
	cursor int // the highlighted suggestion, -1 for none
}

func registerCompletion(r *Runtime) {
	inst := r.Instance(completionID, Kind(CompletionNoun))
	inst.Component = &Completer{r: r, cursor: -1, jobs: map[uint64]uint64{}}
	r.DefineGuard("completion-available", func(r *Runtime, ev KeyEvent) bool {
		in := r.Input(ev.Focus)
		if in == nil || in.Mode != "insert" {
			return false
		}
		_, _, ok := completionToken(in, r.CompletionTriggers(in.ID))
		return ok
	})
	r.DefineGuard("completion-rows", func(r *Runtime, ev KeyEvent) bool {
		c := r.Completer()
		return c.open && c.input == ev.Focus && len(c.rows) > 0
	})
	r.DefineGuard("completion-highlighted", func(r *Runtime, ev KeyEvent) bool {
		c := r.Completer()
		return c.open && c.input == ev.Focus && c.cursor >= 0 && c.cursor < len(c.rows)
	})
	on := func(fn func(*Completer, Context, Arguments) error) Handler {
		return func(r *Runtime, ctx Context, a Arguments) (any, error) {
			c := r.Completer()
			if err := fn(c, ctx, a); err != nil {
				return nil, err
			}
			return c.State(), nil
		}
	}
	r.Define(CompletionNoun).
		Verb("open", on(func(c *Completer, ctx Context, _ Arguments) error { return c.openAt(focusOf(ctx)) }), "Offer suggestions for the trigger token at the cursor now").
		When("completion-available").
		Verb("move", on(func(c *Completer, _ Context, a Arguments) error { c.move(IntArg(a, "delta", 0)); return nil }), "Move the suggestion highlight; from none, 1 lands on the first and -1 stays").Motion().
		Verb("accept", on(func(c *Completer, _ Context, _ Arguments) error { return c.accept() }), "Replace the token with the highlighted suggestion, else the first, keeping its trigger; one undoable edit").
		Verb("dismiss", on(func(c *Completer, _ Context, _ Arguments) error { c.dismiss(); return nil }), "Hide the suggestions; the input stays in its mode and typing reopens them").
		Mode("insert").
		Bind("esc", Do("completion.dismiss")).
		Bind("tab", Do("completion.accept").Guard("completion-rows")).
		Bind("enter", Do("completion.accept").Guard("completion-highlighted")).
		Bind("ctrl+n", Do("completion.move", Arguments{"delta": 1}).Guard("completion-rows")).
		Bind("down", Do("completion.move", Arguments{"delta": 1}).Guard("completion-rows")).
		Bind("ctrl+p", Do("completion.move", Arguments{"delta": -1}).Guard("completion-rows")).
		Bind("up", Do("completion.move", Arguments{"delta": -1}).Guard("completion-rows"))
	move := r.Operations["completion.move"]
	move.Parameters = map[string]Parameter{"delta": {Type: "integer", Required: true}}
	r.Operations["completion.move"] = move
}

// Completer is the inline completion component.
func (r *Runtime) Completer() *Completer {
	if inst := r.Instances[completionID]; inst != nil {
		if c, ok := inst.Component.(*Completer); ok {
			return c
		}
	}
	return nil
}

// Open reports whether the suggestion list is shown (its rows answer).
func (c *Completer) Open() bool { return c.open }

// Input is the input the list belongs to, "" when none is followed.
func (c *Completer) Input() string { return c.input }

// Token is the token whose suggestions are wanted, and whether there is one.
func (c *Completer) Token() (CompletionToken, bool) { return c.token, c.tracking }

// Suggestions are the answer on screen.
func (c *Completer) Suggestions() []Suggestion { return slices.Clone(c.rows) }

// Err is the search error on screen, nil for none.
func (c *Completer) Err() error { return c.err }

// Highlighted is the highlighted suggestion's index, -1 for none.
func (c *Completer) Highlighted() int { return c.cursor }

// Searching reports whether a search newer than the answer on screen runs.
func (c *Completer) Searching() bool { return c.launched > c.shown && c.launched > c.floor }

// DebounceTick is the pause timer in flight, for a host that delivers it
// at once (tests, o-story): false when no search is waiting on one.
func (c *Completer) DebounceTick() (CompletionDebounceMsg, bool) {
	msg := CompletionDebounceMsg{Seq: c.seq}
	return msg, c.DebounceCurrent(msg)
}

// DebounceCurrent reports whether a pause timer is for the latest request,
// whose search has not started yet.
func (c *Completer) DebounceCurrent(msg CompletionDebounceMsg) bool {
	return c.tracking && msg.Seq == c.seq && c.launched < c.seq
}

// State is what the completion verbs return.
func (c *Completer) State() map[string]any {
	out := map[string]any{"open": c.open, "input": c.input, "suggestions": len(c.rows), "highlighted": c.cursor, "searching": c.Searching()}
	if c.tracking {
		out["token"] = c.token
	}
	if c.err != nil {
		out["error"] = c.err.Error()
	}
	return out
}

func (c *Completer) debounce() time.Duration {
	if c.Debounce > 0 {
		return c.Debounce
	}
	return DefaultCompletionDebounce
}

// HandleMessage starts a search when its pause ends and takes a search's
// answer.
func (c *Completer) HandleMessage(msg tea.Msg) bool {
	switch v := msg.(type) {
	case CompletionDebounceMsg:
		if c.DebounceCurrent(v) {
			c.launch()
		}
		return true
	case Completion:
		if a, ok := v.Value.(completionAnswer); ok {
			delete(c.jobs, v.Job)
			c.answer(a)
		}
		return true
	}
	return false
}

// PendingCommand looks at the focused input and schedules the pause timer
// of a new request.
func (c *Completer) PendingCommand() tea.Cmd {
	c.observe()
	if !c.tickDue {
		return nil
	}
	c.tickDue = false
	seq := c.seq
	return tea.Tick(c.debounce(), func(time.Time) tea.Msg { return CompletionDebounceMsg{Seq: seq} })
}

// Close hides the list and cancels its searches.
func (c *Completer) Close() { c.stop() }

func snapshotOf(in *Input) string {
	return in.ID + "\x00" + strconv.Itoa(in.Cursor) + "\x00" + string(in.Text)
}

// observe follows the focused input: an edit that leaves the cursor in a
// trigger token asks for its suggestions; leaving the token, the input or
// insert mode closes the list; a move inside the token changes nothing.
func (c *Completer) observe() {
	r := c.r
	in := r.Input(r.Focus)
	triggers := []CompletionTrigger(nil)
	if in != nil {
		triggers = r.CompletionTriggers(in.ID)
	}
	if in == nil || len(triggers) == 0 || r.BaseMode() != "insert" {
		c.stop()
		c.input, c.seen, c.text = "", "", ""
		return
	}
	snap := snapshotOf(in)
	if snap == c.seen {
		return
	}
	if in.ID != c.input {
		c.stop()
		c.input, c.text = in.ID, string(in.Text)
	}
	edited := string(in.Text) != c.text
	c.seen, c.text = snap, string(in.Text)
	tok, search, ok := completionToken(in, triggers)
	switch {
	case !ok:
		c.stop()
	case !c.tracking && !edited:
		// Moving into a token does not open the list; tab does.
	case c.tracking && tok == c.token:
		// A move inside the token: the same query.
	default:
		c.want(tok, search)
	}
}

// want asks for a token's suggestions after the pause.
func (c *Completer) want(tok CompletionToken, search CompletionSearch) {
	c.tracking, c.token, c.search = true, tok, search
	c.seq++
	c.tickDue = true
}

// openAt asks for the suggestions of the token at an input's cursor at
// once, without the pause (completion.open).
func (c *Completer) openAt(id string) error {
	in := c.r.Input(id)
	if in == nil {
		return errors.New("no focused input")
	}
	tok, search, ok := completionToken(in, c.r.CompletionTriggers(id))
	if !ok {
		return errors.New("no completion trigger at the cursor")
	}
	if c.input != id {
		c.stop()
	}
	c.input, c.seen, c.text = id, snapshotOf(in), string(in.Text)
	c.want(tok, search)
	c.tickDue = false
	c.launch()
	return nil
}

// launch starts the latest request's search as a job and opens the list.
func (c *Completer) launch() {
	r := c.r
	seq, query, search := c.seq, c.token.Query, c.search
	work := func(ctx context.Context) (any, error) {
		rows, err := search(ctx, query)
		return completionAnswer{seq: seq, rows: rows, err: err}, nil
	}
	ticket, err := r.Launch(completionID, "completion.search", Pending(work).Label(c.token.Trigger+query), Ctx{Runtime: r, Context: r.contextOf(completionID), Instance: r.Instances[completionID]})
	if err != nil {
		c.rows, c.err = nil, err
	} else {
		c.jobs[ticket.Job] = seq
	}
	c.launched = seq
	c.show()
}

// answer shows a search's answer unless the answer on screen is newer or
// the list was dismissed since it was asked; a shown answer cancels the
// older searches still running, which could no longer show.
func (c *Completer) answer(a completionAnswer) {
	if !c.tracking || a.seq <= c.shown || a.seq <= c.floor {
		return
	}
	keep := ""
	if c.cursor >= 0 && c.cursor < len(c.rows) {
		keep = c.rows[c.cursor].Text
	}
	c.shown = a.seq
	c.rows, c.err, c.cursor = a.rows, a.err, -1
	if keep != "" {
		c.cursor = slices.IndexFunc(c.rows, func(s Suggestion) bool { return s.Text == keep })
	}
	for job, seq := range c.jobs {
		if seq < a.seq {
			c.cancel(job)
		}
	}
	c.show()
}

// cancel stops one search job.
func (c *Completer) cancel(job uint64) {
	if j := c.r.jobs[job]; j != nil && j.State == JobRunning {
		c.r.finish(j, JobCancelled, nil)
	}
	delete(c.jobs, job)
}

// show opens the list on the followed input: its rows become the input's
// inner level.
func (c *Completer) show() {
	c.open = true
	if inst := c.r.Instances[c.input]; inst != nil {
		inst.inner = completionID
	}
}

// stop closes the list and forgets the token: pending answers are
// dropped and running searches cancelled.
func (c *Completer) stop() {
	if inst := c.r.Instances[c.input]; inst != nil && inst.inner == completionID {
		inst.inner = ""
	}
	for job := range c.jobs {
		c.cancel(job)
	}
	c.open, c.tracking, c.tickDue = false, false, false
	c.floor = c.seq
	c.rows, c.err, c.cursor = nil, nil, -1
	c.token, c.search = CompletionToken{}, nil
}

// dismiss hides the list where the cursor is; typing in the token asks
// again.
func (c *Completer) dismiss() { c.stop() }

// move steps the highlight, clamped at both ends (no wrap): from none, 1
// lands on the first suggestion and -1 stays on none, as in a chooser.
func (c *Completer) move(delta int) {
	if len(c.rows) == 0 || delta == 0 {
		return
	}
	switch {
	case c.cursor < 0 && delta > 0:
		c.cursor = 0
	case c.cursor < 0:
	default:
		c.cursor = min(max(c.cursor+delta, 0), len(c.rows)-1)
	}
}

// accept replaces the token under the cursor with the highlighted
// suggestion (else the first), keeping the trigger, as its own undo step,
// and closes the list.
func (c *Completer) accept() error {
	in := c.r.Input(c.input)
	if in == nil || !c.open {
		return errors.New("no suggestions are open")
	}
	if len(c.rows) == 0 {
		return errors.New("no suggestion to accept")
	}
	pick := c.rows[max(0, c.cursor)]
	tok, _, ok := completionToken(in, c.r.CompletionTriggers(in.ID))
	if !ok {
		c.stop()
		return errors.New("the cursor left the completion token")
	}
	// One undoable edit: the typing before it and after it are other steps.
	in.endGroup()
	err := in.Replace(tok.Start, tok.End, tok.Trigger+pick.Text)
	in.endGroup()
	c.stop()
	c.seen, c.text = snapshotOf(in), string(in.Text)
	return err
}

// overlay draws the list under the token on a rendered frame (above it
// when there is no room below), keeping the frame's cursor.
func (c *Completer) overlay(frame Block, width, height int, s Styles) Block {
	in := c.r.Input(c.input)
	if !c.open || in == nil || c.r.Focus != c.input || frame.Cursor == nil || width < 4 || height < 3 {
		return frame
	}
	box := c.box(min(width, 50), s)
	lines := strings.Split(box, "\n")
	boxWidth := ansi.StringWidth(lines[0])
	cursor := min(max(0, in.Cursor), len(in.Text))
	start := min(c.token.Start+len([]rune(c.token.Trigger)), cursor)
	// The suggestions' text sits under the query it completes, the border
	// under the trigger's last cell.
	x := frame.Cursor.X - cellWidth(string(in.Text[start:cursor])) - 1
	x = max(0, min(x, width-boxWidth))
	y := frame.Cursor.Y + 1
	if y+len(lines) > height && frame.Cursor.Y-len(lines) >= 0 {
		y = frame.Cursor.Y - len(lines)
	}
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(fitSurface(frame.Text, width, height)),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	))
	return frame.WithText(fitSurface(canvas.Render(), width, height))
}

// box is the list as a bordered panel at most width cells wide: the
// suggestions with the highlighted one reversed, windowed around it; the
// error, "searching…" or "no matches" when there is nothing to choose.
func (c *Completer) box(width int, s Styles) string {
	inner := max(1, width-2)
	var lines []string
	switch {
	case c.err != nil:
		lines = append(lines, s.Error.Render(ansi.Truncate("! "+modalLabel(c.err.Error()), inner, "…")))
	case len(c.rows) == 0 && c.Searching():
		lines = append(lines, s.Muted.Render("searching…"))
	case len(c.rows) == 0:
		lines = append(lines, s.Muted.Render("no matches"))
	}
	offset := 0
	if c.cursor >= completionRows {
		offset = c.cursor - completionRows + 1
	}
	plain := func(row Suggestion) (string, string) {
		label := modalLabel(row.label())
		detail := ""
		if row.Detail != "" {
			detail = "  " + modalLabel(row.Detail)
		}
		return label, detail
	}
	want := 12
	for _, line := range lines {
		want = max(want, ansi.StringWidth(line))
	}
	for _, row := range c.rows {
		label, detail := plain(row)
		want = max(want, ansi.StringWidth(label+detail))
	}
	inner = min(inner, want)
	for i := offset; i < len(c.rows) && i < offset+completionRows; i++ {
		label, detail := plain(c.rows[i])
		text := ansi.Truncate(label+detail, inner, "…")
		pad := strings.Repeat(" ", max(0, inner-ansi.StringWidth(text)))
		if i == c.cursor {
			lines = append(lines, reverseVideo(text+pad))
			continue
		}
		if cut := ansi.StringWidth(label); detail != "" && cut < ansi.StringWidth(text) {
			text = ansi.Cut(text, 0, cut) + s.Muted.Render(ansi.Cut(text, cut, inner))
		}
		lines = append(lines, text+pad)
	}
	if more := len(c.rows) - offset - completionRows; more > 0 {
		lines = append(lines, s.Muted.Render("+"+strconv.Itoa(more)+" more"))
	}
	for i, line := range lines {
		lines[i] = modalCells(line, inner)
	}
	panel := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(s.Accent.GetForeground())
	return panel.Render(strings.Join(lines, "\n"))
}
