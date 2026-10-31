package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TerminalStream is the application's connection to one pane: a daemon
// client, never a PTY the framework owns. The framework reads frames from
// it, forwards keys and pasted text to it and tells it the pane's size.
// Next blocks until the pane changes and returns its next frame; it returns
// ctx's error when the terminal instance closes or newer work supersedes
// the read. Keys are named as bindings spell them ("ctrl+a", "d", "esc").
type TerminalStream interface {
	Next(ctx context.Context) (TerminalFrame, error)
	Send(keys ...string) error
	Paste(text string) error
	Resize(cols, rows int) error
}

// TerminalReleaser is a TerminalStream that holds something to give back
// when its terminal closes: a daemon size lease, a connection. Terminal.Close
// calls Release once, so Pop, SetItems, RemoveInstance and a CloseOnDetach
// detach all release the stream; its error is kept as the terminal's Err.
type TerminalReleaser interface {
	Release() error
}

// TerminalFrame is one rendering of a pane: its rows of trusted ANSI, the
// cursor cell, the cursor shape the pane reports ("block", "bar",
// "underline"; "" for the default block) and whether the cursor is hidden.
// Status is the stream's own state beside its cells ("running", "exited 0")
// for streams whose application draws that state; the framework does not
// draw it. A read whose cells are unchanged is not redrawn unless its
// Status changed, so a stream reports here any change an application shows
// outside the cells (an exit, a reconnect), even while the pane is hidden.
type TerminalFrame struct {
	Lines  []string `json:"lines"`
	Cursor Pos      `json:"cursor"`
	Shape  string   `json:"shape,omitempty"`
	Hidden bool     `json:"hidden,omitempty"`
	Status string   `json:"status,omitempty"`
}

// Terminal is the component of a terminal instance: it holds the last frame
// the stream delivered, renders it into the area a page gives it and reports
// the pane's cursor. Frames arrive through the delivery contract as
// Completions owned by the instance in the "read" slot, one read at a time,
// so a frame that arrives after Close, or after a newer read started, is
// stale and dropped before it reaches the component.
type Terminal struct {
	r      *Runtime
	id     string
	stream TerminalStream
	frame  TerminalFrame
	cols   int
	rows   int
	err    string
	closed bool
	// closeOnDetach removes the instance on terminal.detach (CloseOnDetach).
	closeOnDetach bool
	// overlay is drawn over the frame by ViewBlock (SetOverlay).
	overlay *TerminalOverlay
}

// TerminalOverlay is what an application draws over a terminal's frame
// without touching the frame: Dim fades the pane's cells (the modal-dim
// backdrop style; faint in mono and NO_COLOR) and each mark replaces the
// cells it covers. Hints over a live pane (labels on its URLs and paths)
// are an overlay plus an application mode; see SetOverlay.
type TerminalOverlay struct {
	Dim   bool          `json:"dim,omitempty"`
	Marks []OverlayMark `json:"marks,omitempty"`
}

// OverlayMark is plain text drawn from one cell of the frame (Row and Col,
// 0-based, in the pane's cells) to the right, cut at the frame's edge; a
// wide rune it half covers becomes a space. Style draws the text; the
// empty style draws it reverse and bold, so a mark stands out in mono
// too. Marks are drawn in order, a later one over an earlier one.
type OverlayMark struct {
	Row   int            `json:"row"`
	Col   int            `json:"col"`
	Text  string         `json:"text"`
	Style lipgloss.Style `json:"-"`
}

// SetOverlay draws o over the pane from the next frame on; nil clears it.
// The overlay stays until the application clears or replaces it: a new
// frame from the stream does not clear it, and neither do a focus change,
// a mode change or a detach, so an application that pairs it with a mode
// clears it where it leaves the mode (and in its own focus or close
// handling). It changes only what ViewBlock draws: Frame, the stream and
// every other reader of the pane see the pane's own cells.
func (t *Terminal) SetOverlay(o *TerminalOverlay) { t.overlay = o }

// Overlay is the overlay SetOverlay set, nil for none.
func (t *Terminal) Overlay() *TerminalOverlay { return t.overlay }

// AddTerminal registers a terminal instance of the terminal noun fed by
// stream, in terminal mode: every key forwards to the pane except the
// leader. Options: Parent(id) (default app), Kind(noun) for a noun that
// supplies its own tables and CloseOnDetach to remove the instance (and
// release the stream) on detach. The first frame read is scheduled at once.
func (r *Runtime) AddTerminal(id string, stream TerminalStream, opts ...InstanceOption) (*Terminal, error) {
	if id == "" || r.Instances[id] != nil {
		return nil, errors.New("terminal ID must be unique and nonempty")
	}
	if stream == nil {
		return nil, errors.New("terminal stream is required")
	}
	spec := instanceSpec{parent: "app", noun: "ochazuke.terminal"}
	for _, opt := range opts {
		opt(&spec)
	}
	if r.Instances[spec.parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", spec.parent)
	}
	t := &Terminal{r: r, id: id, stream: stream, closeOnDetach: spec.closeOnDetach}
	inst := r.Instance(id, Parent(spec.parent), Kind(spec.noun), FocusOnClose(spec.closeFocus))
	inst.Component = t
	t.read()
	return t, nil
}

// Terminal returns the terminal an instance fronts, or nil.
func (r *Runtime) Terminal(id string) *Terminal {
	if inst := r.Instances[id]; inst != nil {
		if t, ok := inst.Component.(*Terminal); ok {
			return t
		}
	}
	return nil
}

// ID is the terminal's instance id.
func (t *Terminal) ID() string { return t.id }

// Frame is the last frame the stream delivered.
func (t *Terminal) Frame() TerminalFrame { return t.frame }

// Size is the area the pane was last told to fill.
func (t *Terminal) Size() (cols, rows int) { return t.cols, t.rows }

// Err is the stream's last error, "" when it is healthy.
func (t *Terminal) Err() string { return t.err }

// Focused reports whether keys go to this pane.
func (t *Terminal) Focused() bool { return t.r.Focus == t.id }

// read schedules the next frame read as work owned by the instance.
func (t *Terminal) read() {
	if t.closed {
		return
	}
	stream := t.stream
	t.r.Launch(t.id, "read", Pending(func(ctx context.Context) (any, error) { return stream.Next(ctx) }).Replacing("read"), Ctx{})
}

// HandleMessage stores a frame delivered to this terminal and schedules the
// next read. A stream error ends reading; a cancelled read is silent.
func (t *Terminal) HandleMessage(msg tea.Msg) bool {
	done, ok := msg.(Completion)
	if !ok || done.Owner != t.id {
		return false
	}
	if done.Err != nil {
		if !errors.Is(done.Err, context.Canceled) {
			t.err = done.Err.Error()
		}
		return true
	}
	if frame, ok := done.Value.(TerminalFrame); ok {
		t.acceptFrame(frame)
	}
	t.read()
	return true
}

// PendingCommand is nil: reads are scheduled through Runtime.Start.
func (t *Terminal) PendingCommand() tea.Cmd { return nil }

// Close stops reading and releases a TerminalReleaser stream, once; the
// runtime has already cancelled the read in flight.
func (t *Terminal) Close() {
	if t.closed {
		return
	}
	t.closed = true
	if rel, ok := t.stream.(TerminalReleaser); ok {
		if err := rel.Release(); err != nil {
			t.err = err.Error()
		}
	}
}

// View renders the pane without a cursor; see ViewBlock.
func (t *Terminal) View(width, height int) string { return t.ViewBlock(width, height).Text }

// ViewBlock renders the pane's cells into exactly width x height cells and
// reports the pane's cursor only while focused. The stream is told the area
// the first time it is seen and
// whenever it changes, so the pane is sized to the detail area it fills.
func (t *Terminal) ViewBlock(width, height int) Block {
	t.r.Placed(t.id)
	if width <= 0 || height <= 0 {
		return Block{}
	}
	if width != t.cols || height != t.rows {
		t.cols, t.rows = width, height
		if err := t.stream.Resize(width, height); err != nil {
			t.err = err.Error()
		}
	}
	rows := make([]string, height)
	for i := range rows {
		line := ""
		if i < len(t.frame.Lines) {
			line = t.frame.Lines[i]
		}
		rows[i] = modalCells(line, width)
	}
	if t.overlay != nil {
		t.drawOverlay(rows, width)
	}
	// The page keeps the pane's top line on screen when it follows the focus.
	block := Text(t.r.PlacedAt(t.id, strings.Join(rows, "\n"), 0, 1))
	if t.Focused() && !t.frame.Hidden {
		block.Cursor = &Pos{X: t.frame.Cursor.X, Y: t.frame.Cursor.Y}
		block = block.Clip(width, height)
	}
	return block
}

// drawOverlay fades rows (each exactly width cells) when the overlay dims
// and splices its marks into them.
func (t *Terminal) drawOverlay(rows []string, width int) {
	if t.overlay.Dim {
		fade := lipgloss.NewStyle().Faint(true)
		if styles := t.r.Settings().page.styles; styles != nil {
			if s := styles(); s.Backdrop.Render("x") != "x" {
				fade = s.Backdrop
			}
		}
		for i := range rows {
			rows[i] = dim(rows[i], fade)
		}
	}
	for _, mark := range t.overlay.Marks {
		if mark.Row < 0 || mark.Row >= len(rows) {
			continue
		}
		rows[mark.Row] = overlayCells(rows[mark.Row], width, mark)
	}
}

// overlayCells is a line of exactly width cells with a mark drawn over the
// cells from its column: the cells before keep their styles, the text is
// cut at the edge, and the cells after start with every sequence that came
// before them so their styles carry on. A wide rune the mark cuts through
// becomes a space on that side.
func overlayCells(line string, width int, mark OverlayMark) string {
	text := ansi.Truncate(modalLabel(mark.Text), width-mark.Col, "")
	w := ansi.StringWidth(text)
	if mark.Col < 0 || w == 0 {
		return line
	}
	style := mark.Style
	if style.Render("x") == "x" {
		style = lipgloss.NewStyle().Reverse(true).Bold(true)
	}
	left := ansi.Truncate(line, mark.Col, "")
	left += strings.Repeat(" ", mark.Col-ansi.StringWidth(left))
	end := mark.Col + w
	rest := ansi.TruncateLeft(line, end, "")
	for n := end; ansi.StringWidth(rest) > width-end; n++ {
		// A wide rune straddling end is kept whole by TruncateLeft.
		rest = ansi.TruncateLeft(line, n+1, "")
	}
	rest = strings.Repeat(" ", width-end-ansi.StringWidth(rest)) + rest
	return left + "\x1b[m" + style.Render(text) + "\x1b[m" + rest
}

// cursorShape maps the pane's reported shape onto the terminal cursor.
func (t *Terminal) cursorShape() tea.CursorShape {
	switch t.frame.Shape {
	case "bar":
		return tea.CursorBar
	case "underline":
		return tea.CursorUnderline
	}
	return tea.CursorBlock
}

// terminalOf resolves the terminal an operation addresses: args.id, else the
// nearest terminal on the context's ancestry.
func (r *Runtime) terminalOf(c Context, args Arguments) (*Terminal, error) {
	if id := StringArg(args, "id", ""); id != "" {
		if t := r.Terminal(id); t != nil {
			return t, nil
		}
		return nil, fmt.Errorf("unknown terminal %q", id)
	}
	for _, id := range r.Ancestry(focusOf(c)) {
		if t := r.Terminal(id); t != nil {
			return t, nil
		}
	}
	return nil, errors.New("no focused terminal")
}

// registerTerminal defines the terminal noun: the terminal mode, where every
// key forwards to the pane and the leader (ctrl+a) enters leader_pending
// with the leader as its prefix and "send the leader alone" as the request
// its timer runs, and the leader_pending table, where d detaches, the leader
// again sends itself and a bound key runs its command; the
// mode is one-shot, so every row returns to terminal mode. Every row is a
// Request. The leader rows name the terminal-leader setting
// ({terminal-leader}), so they follow it. A key no leader_pending row
// binds is sequence-unknown's: pass (the default) sends the leader and
// then the key, swallow drops both. The modes and rows are
// bindings/terminal.json.
func registerTerminal(r *Runtime) {
	r.Define("ochazuke.terminal").Pin("terminal").Focusable().
		Verb("send", func(r *Runtime, c Context, a Arguments) (any, error) {
			t, err := r.terminalOf(c, a)
			if err != nil {
				return nil, err
			}
			keys := strings.Fields(StringArg(a, "keys", ""))
			if len(keys) == 0 {
				return nil, errors.New("keys is required")
			}
			return keys, t.stream.Send(keys...)
		}, "Forward key names, in order, to the pane").
		Verb("paste", func(r *Runtime, c Context, a Arguments) (any, error) {
			t, err := r.terminalOf(c, a)
			if err != nil {
				return nil, err
			}
			if keys := StringArg(a, "keys", ""); keys != "" {
				if _, err := r.ExecuteAt(c, Request{Verb: "send", Noun: "ochazuke.terminal", Args: Arguments{"id": t.id, "keys": keys}}); err != nil {
					return nil, err
				}
			}
			text := StringArg(a, "text", "")
			return text, t.stream.Paste(text)
		}, "Paste text into the pane, after forwarding any keys given (the leader, under it)").
		Verb("attach", func(r *Runtime, c Context, a Arguments) (any, error) {
			t, err := r.terminalOf(c, a)
			if err != nil {
				return nil, err
			}
			err = r.SetFocus(t.id)
			return r.Context(), err
		}, "Focus a terminal: keys go to the pane until the leader detaches").
		Verb("detach", func(r *Runtime, c Context, a Arguments) (any, error) {
			t, err := r.terminalOf(c, a)
			if err != nil {
				return nil, err
			}
			back := r.Instances[t.id].Parent
			if t.closeOnDetach {
				if to := r.closeFocusOf(t.id, r.subtreeSet(t.id)); to != "" {
					back = to
				}
			}
			if err = r.SetFocus(back); err == nil && t.closeOnDetach {
				err = r.RemoveInstance(t.id)
			}
			return r.Context(), err
		}, "Return focus to the instance around the terminal; the pane keeps running (a CloseOnDetach terminal is removed, its stream released and the focus sent to its FocusOnClose target)")
	// Required-parameter metadata for the rows (bindings/terminal.json) and
	// for the CLI.
	for verb, params := range map[string]map[string]Parameter{
		"send":   {"id": {Type: "string", Description: "Terminal instance (defaults to the focused one)"}, "keys": {Type: "string", Required: true, Description: "Space-separated key names, forwarded in order"}},
		"paste":  {"id": {Type: "string", Description: "Terminal instance (defaults to the focused one)"}, "text": {Type: "string", Required: true}, "keys": {Type: "string", Description: "Space-separated key names forwarded before the text"}},
		"attach": {"id": {Type: "string", Description: "Terminal instance (defaults to the focused one)"}},
		"detach": {"id": {Type: "string", Description: "Terminal instance (defaults to the focused one)"}},
	} {
		op := r.Operations["ochazuke.terminal."+verb]
		op.Parameters, op.Result = params, "JSON value"
		r.Operations["ochazuke.terminal."+verb] = op
	}
}

// The terminal leader setting: the key that enters leader_pending. Its
// default is in settings/ochazuke.json; DefaultTerminalLeader is the
// fallback when the setting holds a key that is not a leader.
const (
	TerminalLeaderSetting = "terminal-leader"
	DefaultTerminalLeader = "ctrl+a"
)

// validLeader is the terminal-leader setting's check.
func validLeader(v any) error {
	if key, _ := v.(string); !leaderKey(key) {
		return errors.New(`must be one ctrl+ or alt+ key, such as "ctrl+b"`)
	}
	return nil
}

// TerminalLeader is the key that enters leader_pending: the
// terminal-leader setting (its embedded default before it is declared),
// DefaultTerminalLeader when that is not one ctrl+ or alt+ key. The
// rows that bind it name the setting ({terminal-leader}), so they follow
// it (binding_settings.go).
func (r *Runtime) TerminalLeader() string {
	if key, ok := r.settingText(TerminalLeaderSetting); ok && leaderKey(key) {
		return key
	}
	return DefaultTerminalLeader
}

// SetTerminalLeader sets the terminal-leader setting to key (declaring
// it first when the application has not), so every row naming the
// setting moves to key. A key that is not one ctrl+ or alt+ combination
// is refused.
func (r *Runtime) SetTerminalLeader(key string) error {
	if err := validLeader(key); err != nil {
		return fmt.Errorf("terminal leader %q: %w", key, err)
	}
	r.declareFrameworkSettings(TerminalLeaderSetting)
	return r.Settings().Set(TerminalLeaderSetting, key)
}

// EnsureTerminalLeader declares the terminal-leader setting unless the
// application declared it: an application with terminal panes calls it
// at setup (cli.AddCommands does), so settings.json, the Settings page
// and the CLI's settings set know the setting before the first pane
// opens. An application without terminals does not, and its settings
// list stays free of it. How long the leader waits is sequence-timeout,
// as for every other sequence.
func (r *Runtime) EnsureTerminalLeader() {
	r.declareFrameworkSettings(TerminalLeaderSetting)
}
