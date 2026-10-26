package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
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
type TerminalFrame struct {
	Lines  []string `json:"lines"`
	Cursor Pos      `json:"cursor"`
	Shape  string   `json:"shape,omitempty"`
	Hidden bool     `json:"hidden,omitempty"`
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
}

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
	spec := instanceSpec{parent: "app", noun: "terminal"}
	for _, opt := range opts {
		opt(&spec)
	}
	if r.Instances[spec.parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", spec.parent)
	}
	t := &Terminal{r: r, id: id, stream: stream, closeOnDetach: spec.closeOnDetach}
	inst := r.Instance(id, Parent(spec.parent), Kind(spec.noun), FocusOnClose(spec.closeFocus))
	inst.Component = t
	r.applyTerminalLeader()
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
		t.frame = frame
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
// reports the pane's cursor; compose the cursor only while the terminal is
// focused. The stream is told the area the first time it is seen and
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
	// The page keeps the pane's top line on screen when it follows the focus.
	block := Text(t.r.PlacedAt(t.id, strings.Join(rows, "\n"), 0, 1))
	if !t.frame.Hidden {
		block.Cursor = &Pos{X: t.frame.Cursor.X, Y: t.frame.Cursor.Y}
		block = block.Clip(width, height)
	}
	return block
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
// again sends itself, a bound key runs its command and any other key sends
// the leader and itself (Join of the prefix and the key), in that order; the
// mode is one-shot, so every row returns to terminal mode. Every row is a
// Request. The leader is ctrl+a until the terminal-leader setting (or
// SetTerminalLeader) moves it.
func registerTerminal(r *Runtime) {
	r.DefineMode("terminal", false)
	_ = r.DefinePassthrough("terminal")
	r.DefineMode("leader_pending", true)
	leader := Request{Verb: "send", Noun: "terminal", Args: Arguments{"keys": "ctrl+a"}}
	r.Define("terminal").Pin("terminal").Focusable().
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
				if _, err := r.ExecuteAt(c, Request{Verb: "send", Noun: "terminal", Args: Arguments{"id": t.id, "keys": keys}}); err != nil {
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
		}, "Return focus to the instance around the terminal; the pane keeps running (a CloseOnDetach terminal is removed, its stream released and the focus sent to its FocusOnClose target)").
		Mode("terminal").
		Bind("ctrl+a", Enter("leader_pending", Payload{Prefix: "ctrl+a", Request: &leader}).Hinted("leader: d detaches, ctrl+a sends ctrl+a")).
		Pattern(Paste, Do("terminal.paste").FromPayload("text", SourceText).Hide()).
		Pattern(Any, Do("terminal.send").FromKey("keys").Hide()).
		Mode("leader_pending").
		Bind("d", Do("terminal.detach")).
		Bind("ctrl+a", Do("terminal.send", Arguments{"keys": "ctrl+a"})).
		Pattern(Paste, Do("terminal.paste").FromPayload("keys", SourcePrefix).FromPayload("text", SourceText).Hide()).
		Pattern(Any, Do("terminal.send").Join("keys", SourcePrefix, SourceKey).Hide())
	// Required-parameter metadata for the rows above and for the CLI.
	for verb, params := range map[string]map[string]Parameter{
		"send":   {"id": {Type: "string", Description: "Terminal instance (defaults to the focused one)"}, "keys": {Type: "string", Required: true, Description: "Space-separated key names, forwarded in order"}},
		"paste":  {"id": {Type: "string", Description: "Terminal instance (defaults to the focused one)"}, "text": {Type: "string", Required: true}, "keys": {Type: "string", Description: "Space-separated key names forwarded before the text"}},
		"attach": {"id": {Type: "string", Description: "Terminal instance (defaults to the focused one)"}},
		"detach": {"id": {Type: "string", Description: "Terminal instance (defaults to the focused one)"}},
	} {
		op := r.Operations["terminal."+verb]
		op.Parameters, op.Result = params, "JSON value"
		r.Operations["terminal."+verb] = op
	}
}

// The terminal leader setting: the key that enters leader_pending.
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

// TerminalLeader is the key the terminal noun's leader row binds (the
// terminal-leader setting once applied), DefaultTerminalLeader when the
// noun has no leader row.
func (r *Runtime) TerminalLeader() string {
	if n := r.Nouns["terminal"]; n != nil {
		for _, b := range n.Bindings["terminal"] {
			if key, ok := leaderRow(b); ok {
				return key
			}
		}
	}
	return DefaultTerminalLeader
}

// leaderRow reports whether a row is a leader: it enters leader_pending on
// one key that is its own prefix. It returns the key.
func leaderRow(b Binding) (string, bool) {
	if b.Keys == "" || b.Unbind || b.Request.Key() != "mode.enter" || b.Request.Args["mode"] != "leader_pending" {
		return "", false
	}
	if prefix, _ := b.Request.Args["prefix"].(string); prefix != b.Keys {
		return "", false
	}
	return b.Keys, true
}

// SetTerminalLeader moves every leader to key: on every noun, each row
// that enters leader_pending on one key (the framework's terminal row and
// an application's own, such as a richer hint) is rebound to key with key
// as its prefix and its timeout request, and the leader_pending row that
// sends the old leader to the pane now sends key on key. Hints that name
// the old leader name the new one. A row already bound to key in those
// tables gives way. The rows stay data: this rewrites them as a binding
// file would. The terminal-leader setting calls it; a key that is not one
// ctrl+ or alt+ combination is refused.
func (r *Runtime) SetTerminalLeader(key string) error {
	if err := validLeader(key); err != nil {
		return fmt.Errorf("terminal leader %q: %w", key, err)
	}
	names := make([]string, 0, len(r.Nouns))
	for name := range r.Nouns {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		n := r.Nouns[name]
		olds := map[string]bool{}
		for _, mode := range slices.Sorted(maps.Keys(n.Bindings)) {
			n.Bindings[mode] = rekeyRows(n.Bindings[mode], key, func(b Binding) (string, bool) {
				old, ok := leaderRow(b)
				if ok && old != key {
					olds[old] = true
					return old, true
				}
				return "", false
			})
		}
		if len(olds) == 0 {
			continue
		}
		n.Bindings["leader_pending"] = rekeyRows(n.Bindings["leader_pending"], key, func(b Binding) (string, bool) {
			if olds[b.Keys] && b.Request.Key() == "terminal.send" && b.Request.Args["keys"] == b.Keys {
				return b.Keys, true
			}
			return "", false
		})
	}
	r.CancelPendingBinding()
	return nil
}

// rekeyRows rebinds the rows match picks (returning the old leader) to
// key and drops the unguarded rows of the table already on key.
func rekeyRows(rows []Binding, key string, match func(Binding) (string, bool)) []Binding {
	moved := map[int]bool{}
	for i, b := range rows {
		if old, ok := match(b); ok {
			rows[i] = releadRow(b, old, key)
			moved[i] = true
		}
	}
	if len(moved) == 0 {
		return rows
	}
	out := make([]Binding, 0, len(rows))
	for i, b := range rows {
		if !moved[i] && b.Keys == key && b.When == "" {
			continue
		}
		out = append(out, b)
	}
	return out
}

// releadRow is a leader row (or the row sending the leader) moved from old
// to key: its keys, its prefix, the key it or its timeout request sends,
// and its hint and help text.
func releadRow(b Binding, old, key string) Binding {
	b.Keys = key
	args := maps.Clone(b.Request.Args)
	if _, ok := args["prefix"]; ok {
		args["prefix"] = key
	}
	if args["keys"] == old {
		args["keys"] = key
	}
	switch q := args["request"].(type) {
	case Request:
		if q.Args["keys"] == old {
			q.Args = maps.Clone(q.Args)
			q.Args["keys"] = key
			args["request"] = q
		}
	case *Request:
		if q != nil && q.Args["keys"] == old {
			c := *q
			c.Args = maps.Clone(q.Args)
			c.Args["keys"] = key
			args["request"] = &c
		}
	case map[string]any:
		if a, ok := q["args"].(map[string]any); ok && a["keys"] == old {
			q, a = maps.Clone(q), maps.Clone(a)
			a["keys"] = key
			q["args"] = a
			args["request"] = q
		}
	}
	b.Request.Args = args
	b.Hint = strings.ReplaceAll(b.Hint, old, key)
	b.Help = strings.ReplaceAll(b.Help, old, key)
	return b
}

// EnsureTerminalLeader declares the terminal-leader setting unless the
// application declared one, and applies it: an application with terminal
// panes calls it at setup (cli.AddCommands does), so settings.json, the
// settings editor and the CLI's settings set know the setting before the
// first pane opens. An application without terminals does not, and its
// settings list stays free of it.
func (r *Runtime) EnsureTerminalLeader() {
	if r.Settings().find(TerminalLeaderSetting) == nil {
		_ = r.DeclareSetting(Setting{Name: TerminalLeaderSetting, Type: SettingString, Default: DefaultTerminalLeader, Description: "Terminal leader key: then d detaches, the leader again sends it to the pane", Validate: validLeader})
	}
	r.applyTerminalLeader()
}

// applyTerminalLeader moves the leader to the terminal-leader setting's
// value when the setting is declared (an application's declaration with
// a value that is not a leader key keeps the default), and follows it.
// EnsureConfigSettings and AddTerminal call it, so leader rows an
// application defines after Init move too.
func (r *Runtime) applyTerminalLeader() {
	c, s := r.Config(), r.Settings()
	if c == nil || s.find(TerminalLeaderSetting) == nil {
		return
	}
	if !c.leaderObserved {
		c.leaderObserved = true
		s.Observe(TerminalLeaderSetting, func(string, any) { r.applyTerminalLeader() })
	}
	key := s.String(TerminalLeaderSetting)
	if !leaderKey(key) {
		key = DefaultTerminalLeader
	}
	_ = r.SetTerminalLeader(key)
}
