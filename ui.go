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

// UI message kinds.
const (
	UINotice      = "notice"
	UIClearNotice = "clear"
	UIStatus      = "status"
)

// UIMessage is one thing a handler told the user: a notice (Kind notice,
// ID and Text), the clearing of one (Kind clear, ID) or the status line
// (Kind status, Text; "" clears it). The startup ring, a call's
// Response.Messages and RecordingSink hold these.
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
	case UIStatus:
		s.Status(msg.Text)
	}
}

// UISink is how a host shows what handlers tell the user: the Model puts
// notices in the footer and the status in the status line, the CLI shell
// prints them on stderr, a test records them. Handlers never reach the
// sink: they post through Ctx (ctx.Notify, ctx.ClearNotice, ctx.Status)
// or Runtime.UI, and the host decides how it looks.
type UISink interface {
	// Notify shows a persistent notice until ClearNotice(id) or the host
	// clears it.
	Notify(id NoticeID, text string)
	// ClearNotice takes back a notice; an unknown id is ignored.
	ClearNotice(id NoticeID)
	// Status replaces the one-line status; "" clears it.
	Status(text string)
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
	inst := r.Instance(uiID, Kind("ui"))
	inst.Component = &UI{}
}

// UI returns the UI component: Notify, ClearNotice and Status for code
// without a Ctx (setup, daemon events), Attach for a host.
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

// Status sets the one-line status; "" clears it.
func (u *UI) Status(text string) {
	if u == nil {
		return
	}
	u.post(UIMessage{Kind: UIStatus, Text: text})
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

// Status sets the one-line status through the runtime's UI; "" clears it.
func (c Ctx) Status(text string) { c.Runtime.UI().Status(text) }

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

// Status records a status.
func (s *RecordingSink) Status(text string) { s.record(UIMessage{Kind: UIStatus, Text: text}) }

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

// LastStatus is the last status recorded, "" for none.
func (s *RecordingSink) LastStatus() string {
	msgs := s.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Kind == UIStatus {
			return msgs[i].Text
		}
	}
	return ""
}
