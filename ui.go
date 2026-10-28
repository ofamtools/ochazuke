package ochazuke

import (
	"slices"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// uiID is the single UI instance under app.
const uiID = "__ui"

// uiRing bounds the messages the UI keeps while no sink has attached yet.
const uiRing = 32

// NoticeID names one notice so the code that posted it can clear it. The
// UI assigns it, whatever sink shows the notice (or none), so a handler
// can always clear what it posted.
type NoticeID uint64

// UI message kinds. The status line is one line with two tones: UIError
// is the error tone (Ctx.Error; the TUI draws "Error: TEXT" in the error
// style, the CLI prints "error: TEXT") and UIInfo the neutral one
// (Ctx.Info: plain text before the hints, "status: TEXT" on the CLI).
// Either replaces the other. The kind strings are wire values
// (Response.Messages), so UIError keeps its old wire value "status".
const (
	UINotice      = "notice"
	UIClearNotice = "clear"
	UIError       = "status"
	UIInfo        = "info"
)

// UIMessage is one thing a handler told the user: a notice (Kind notice,
// ID and Text), the clearing of one (Kind clear, ID), or the status line in
// its error tone (Kind status, Text) or its neutral tone (Kind info, Text);
// "" clears the line. The startup ring, a call's Response.Messages and
// RecordingSink hold these.
type UIMessage struct {
	Kind string   `json:"kind"`
	ID   NoticeID `json:"id,omitempty"`
	Text string   `json:"text,omitempty"`
}

// Apply hands the message to s.
func (msg UIMessage) Apply(s UISink) {
	switch msg.Kind {
	case UINotice:
		s.Notify(msg.ID, msg.Text)
	case UIClearNotice:
		s.ClearNotice(msg.ID)
	case UIError:
		s.Status(msg.Text)
	case UIInfo:
		s.Info(msg.Text)
	}
}

// Line is the message as a host prints it on a line of its own (the CLI's
// stderr): "notice: TEXT", "error: TEXT" for the error line, "status:
// TEXT" for the neutral one; "" for a clear or an empty status.
func (msg UIMessage) Line() string {
	if msg.Text == "" {
		return ""
	}
	switch msg.Kind {
	case UINotice:
		return "notice: " + msg.Text
	case UIError:
		return "error: " + msg.Text
	case UIInfo:
		return "status: " + msg.Text
	}
	return ""
}

// UISink is how a host shows what handlers tell the user: the Model puts
// notices in the footer and the status in the status line, the CLI shell
// prints them on stderr, a test records them. Handlers never reach the
// sink: they post through Ctx (ctx.Notify, ctx.ClearNotice, ctx.Error,
// ctx.Info) or Runtime.UI, and the host decides how it looks. Every sink
// shows both tones of the status line, so a plain message is never drawn
// as an error for want of a method. A sink that wraps another forwards
// every message (UIMessage.Apply does it).
type UISink interface {
	// Notify shows a persistent notice until ClearNotice(id) or the host
	// clears it.
	Notify(id NoticeID, text string)
	// ClearNotice takes back a notice; an unknown id is ignored.
	ClearNotice(id NoticeID)
	// Status replaces the one-line status with an error ("" clears it):
	// what Ctx.Error posts (UIError). Only the error tone reaches it.
	Status(text string)
	// Info replaces the one-line status with plain text ("" clears it):
	// what Ctx.Info posts (UIInfo).
	Info(text string)
}

// UI is the component that routes handler messages to the host's sink. It
// is one instance under app (Runtime.UI), never a Runtime field. With no
// sink it discards, except that until the first sink attaches it keeps
// the last 32 messages (the startup ring) and replays them, in order, into
// that sink; after the first attach nothing is kept, so memory stays
// bounded whether or not a sink ever attaches.
//
// Like the rest of Runtime it is called from the update loop; work off the
// loop posts from its Then (which gets a Ctx).
type UI struct {
	sink     UISink
	attached bool // a sink attached once: the ring is gone for good
	ring     []UIMessage
	next     NoticeID
	captures []*[]UIMessage // HandleCall's open captures, innermost last
}

func registerUI(r *Runtime) {
	inst := r.Instance(uiID, Kind("ochazuke.ui"))
	inst.Component = &UI{}
}

// UI returns the UI component: Notify, ClearNotice, Error and Info
// for code without a Ctx (setup, daemon events), Attach for a host.
func (r *Runtime) UI() *UI {
	if r == nil {
		return nil
	}
	ui, _ := r.Instances[uiID].Component.(*UI)
	return ui
}

// HandleMessage: the UI does no off-loop work.
func (u *UI) HandleMessage(tea.Msg) bool { return false }

// PendingCommand: nothing is ever scheduled.
func (u *UI) PendingCommand() tea.Cmd { return nil }

// Close keeps everything: the UI lives as long as the runtime.
func (u *UI) Close() {}

// Attach makes s the sink every later message goes to and returns the one
// it replaces (nil for none). The first attach of the runtime's life
// replays the startup ring into s and drops it. Attach(nil) detaches:
// messages are discarded from then on.
func (u *UI) Attach(s UISink) UISink {
	if u == nil {
		return nil
	}
	previous := u.sink
	u.sink = s
	var replay []UIMessage
	if s != nil && !u.attached {
		u.attached = true
		replay, u.ring = u.ring, nil
	}
	for _, msg := range replay {
		msg.Apply(s)
	}
	return previous
}

// Sink is the attached sink, nil when messages are discarded.
func (u *UI) Sink() UISink {
	if u == nil {
		return nil
	}
	return u.sink
}

// Notify posts a persistent notice and returns its id for ClearNotice.
func (u *UI) Notify(text string) NoticeID {
	if u == nil {
		return 0
	}
	u.next++
	id := u.next
	u.post(UIMessage{Kind: UINotice, ID: id, Text: text})
	return id
}

// ClearNotice takes back the notice Notify returned id for.
func (u *UI) ClearNotice(id NoticeID) {
	if u == nil || id == 0 {
		return
	}
	u.post(UIMessage{Kind: UIClearNotice, ID: id})
}

// Error sets the status line to an error, drawn "Error: TEXT" in the
// error style until the next key; "" clears it. Use it for what failed and
// was not returned as a Failed result (a background refresh, a daemon
// event).
func (u *UI) Error(text string) {
	if u == nil {
		return
	}
	u.post(UIMessage{Kind: UIError, Text: text})
}

// Info sets the status line to a plain message ("copied 3 rows", "queued"),
// drawn without the error style before the hints until the next key; ""
// clears it.
func (u *UI) Info(text string) {
	if u == nil {
		return
	}
	u.post(UIMessage{Kind: UIInfo, Text: text})
}

func (u *UI) post(msg UIMessage) {
	for _, c := range u.captures {
		*c = append(*c, msg)
	}
	switch {
	case u.sink != nil:
		msg.Apply(u.sink)
	case !u.attached:
		if len(u.ring) == uiRing {
			u.ring = append(u.ring[:0], u.ring[1:]...)
		}
		u.ring = append(u.ring, msg)
	}
}

// capture records every message posted until the returned stop runs,
// which returns them (HandleCall's Response.Messages). The sink still gets
// them.
func (u *UI) capture() (stop func() []UIMessage) {
	if u == nil {
		return func() []UIMessage { return nil }
	}
	got := new([]UIMessage)
	u.captures = append(u.captures, got)
	return func() []UIMessage {
		u.captures = slices.DeleteFunc(u.captures, func(c *[]UIMessage) bool { return c == got })
		return *got
	}
}

// Notify posts a persistent notice through the runtime's UI (the footer in
// the TUI, a notice: line on the CLI) and returns its id.
func (c Ctx) Notify(text string) NoticeID { return c.Runtime.UI().Notify(text) }

// ClearNotice takes back a notice Notify posted.
func (c Ctx) ClearNotice(id NoticeID) { c.Runtime.UI().ClearNotice(id) }

// Error sets the status line to an error through the runtime's UI (the
// TUI's "Error: TEXT" until the next key, an error: line on the CLI); ""
// clears it. A verb that fails returns Failed instead; Error is for what
// fails beside the result (a job's Then, a refresh).
func (c Ctx) Error(text string) { c.Runtime.UI().Error(text) }

// Info sets the status line to a plain message through the runtime's UI
// (before the hints until the next key, a status: line on the CLI); ""
// clears it. Notify is the notice that stays.
func (c Ctx) Info(text string) { c.Runtime.UI().Info(text) }

// RecordingSink is a UISink that keeps every message, for tests. Attach it
// with r.UI().Attach(&ochazuke.RecordingSink{}).
type RecordingSink struct {
	mu       sync.Mutex
	messages []UIMessage
}

// Notify records a notice.
func (s *RecordingSink) Notify(id NoticeID, text string) {
	s.record(UIMessage{Kind: UINotice, ID: id, Text: text})
}

// ClearNotice records a clear.
func (s *RecordingSink) ClearNotice(id NoticeID) { s.record(UIMessage{Kind: UIClearNotice, ID: id}) }

// Status records an error status (UIError).
func (s *RecordingSink) Status(text string) { s.record(UIMessage{Kind: UIError, Text: text}) }

// Info records a plain status.
func (s *RecordingSink) Info(text string) { s.record(UIMessage{Kind: UIInfo, Text: text}) }

func (s *RecordingSink) record(msg UIMessage) {
	s.mu.Lock()
	s.messages = append(s.messages, msg)
	s.mu.Unlock()
}

// Messages is a copy of what the sink recorded, in order.
func (s *RecordingSink) Messages() []UIMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]UIMessage(nil), s.messages...)
}

// Notices is the text of the notices still standing (posted and not
// cleared), oldest first.
func (s *RecordingSink) Notices() []string {
	var ids []NoticeID
	text := map[NoticeID]string{}
	for _, msg := range s.Messages() {
		switch msg.Kind {
		case UINotice:
			ids = append(ids, msg.ID)
			text[msg.ID] = msg.Text
		case UIClearNotice:
			delete(text, msg.ID)
		}
	}
	var out []string
	for _, id := range ids {
		if t, ok := text[id]; ok {
			out = append(out, t)
		}
	}
	return out
}

// LastError is the last error status recorded (Error), "" for none.
func (s *RecordingSink) LastError() string { return s.last(UIError) }

// LastInfo is the last plain status recorded (Info), "" for none.
func (s *RecordingSink) LastInfo() string { return s.last(UIInfo) }

func (s *RecordingSink) last(kind string) string {
	msgs := s.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Kind == kind {
			return msgs[i].Text
		}
	}
	return ""
}
