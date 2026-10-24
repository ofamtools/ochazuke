package ochazuke

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// fakeStream is a line-buffer pane: frames are pushed by the test, keys and
// pastes are recorded, and Next waits for a frame or the context.
type fakeStream struct {
	frames  chan TerminalFrame
	sent    [][]string
	pasted  []string
	resizes [][2]int
	closed  bool
}

func newFakeStream() *fakeStream { return &fakeStream{frames: make(chan TerminalFrame, 8)} }

func (f *fakeStream) Next(ctx context.Context) (TerminalFrame, error) {
	select {
	case frame := <-f.frames:
		return frame, nil
	case <-ctx.Done():
		return TerminalFrame{}, ctx.Err()
	}
}
func (f *fakeStream) Send(keys ...string) error {
	if f.closed {
		return errors.New("pane closed")
	}
	f.sent = append(f.sent, keys)
	return nil
}
func (f *fakeStream) Paste(text string) error { f.pasted = append(f.pasted, text); return nil }
func (f *fakeStream) Resize(cols, rows int) error {
	f.resizes = append(f.resizes, [2]int{cols, rows})
	return nil
}

// runCommands executes a batch and returns the messages it produced.
func runCommands(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, child := range batch {
			out = append(out, runCommands(child)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// terminalFixture registers a page holding one terminal and focuses it.
func terminalFixture(t *testing.T) (*Runtime, *Terminal, *fakeStream) {
	t.Helper()
	r := NewRuntime()
	r.BindingTimeout = time.Hour
	stream := newFakeStream()
	r.Instance("console")
	term, err := r.AddTerminal("pane", stream, Parent("console"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("console", "Console", func(r *Runtime, w, h int) Block { return term.ViewBlock(w, h) }, Focus("pane")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("console"); err != nil {
		t.Fatal(err)
	}
	return r, term, stream
}

func press(t *testing.T, r *Runtime, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, err := r.Dispatch(MacroKey{Key: key}); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
}

// The leader contract: every key forwards; the leader waits; a bound key runs
// its command; the leader again sends itself; an unbound key forwards the
// leader and itself in order; a leader left alone past the timeout forwards
// the leader alone and the next key starts fresh.
func TestTerminalLeaderForwardsBoundRunsAndTimeoutForwardsPrefix(t *testing.T) {
	r, term, stream := terminalFixture(t)
	calls := recorder(t, r, "new-window")
	r.Define("terminal").Mode("leader_pending").Bind("c", Do("test.new-window"))
	mode := func() string { return r.Mode.Name }
	if !term.Focused() || mode() != "terminal" {
		t.Fatalf("focus %q mode %q", r.FocusID(), mode())
	}
	press(t, r, "x", "esc", "ctrl+c")
	if !reflect.DeepEqual(stream.sent, [][]string{{"x"}, {"esc"}, {"ctrl+c"}}) {
		t.Fatalf("plain keys: %v", stream.sent)
	}
	press(t, r, "ctrl+a")
	if mode() != "leader_pending" || r.Mode.Payload.Return != "terminal" || r.Pending != "ctrl+a" {
		t.Fatalf("after leader: mode %q payload %+v pending %q", mode(), r.Mode.Payload, r.Pending)
	}
	press(t, r, "c")
	if !reflect.DeepEqual(*calls, []string{"new-window"}) || mode() != "terminal" || r.Pending != "" || len(stream.sent) != 3 {
		t.Fatalf("bound leader key: calls %v mode %q pending %q sent %v", *calls, mode(), r.Pending, stream.sent)
	}
	press(t, r, "ctrl+a", "ctrl+a")
	press(t, r, "ctrl+a", "z")
	press(t, r, "ctrl+a", "esc")
	if got := stream.sent[3:]; !reflect.DeepEqual(got, [][]string{{"ctrl+a"}, {"ctrl+a", "z"}, {"ctrl+a", "esc"}}) || mode() != "terminal" {
		t.Fatalf("leader twice, unbound key, escape: %v mode %q", got, mode())
	}
	// Timeout through the next key: the leader is forwarded, then the key.
	press(t, r, "ctrl+a")
	r.prefix.deadline = time.Now().Add(-time.Second)
	press(t, r, "d")
	if got := stream.sent[6:]; !reflect.DeepEqual(got, [][]string{{"ctrl+a"}, {"d"}}) || mode() != "terminal" || r.FocusID() != "pane" {
		t.Fatalf("expired leader then d: %v mode %q focus %q", got, mode(), r.FocusID())
	}
	// Timeout through the timer message.
	press(t, r, "ctrl+a")
	cmd := r.BindingTimeoutCommand()
	if cmd == nil {
		t.Fatal("leader did not schedule the sequence timer")
	}
	r.prefix.deadline = time.Now().Add(-time.Second)
	if handled, err := r.HandleBindingTimeout(BindingTimeoutMsg{runtime: r, version: r.bindingVersion}); !handled || err != nil {
		t.Fatalf("timer: handled=%v err=%v", handled, err)
	}
	if got := stream.sent[8:]; !reflect.DeepEqual(got, [][]string{{"ctrl+a"}}) || mode() != "terminal" || r.Pending != "" {
		t.Fatalf("timer forwarded %v mode %q pending %q", got, mode(), r.Pending)
	}
	// d under the leader detaches: focus returns to the page and keys stop forwarding.
	press(t, r, "ctrl+a", "d")
	if r.FocusID() != "console" || mode() != "normal" || len(stream.sent) != 9 {
		t.Fatalf("detach: focus %q mode %q sent %v", r.FocusID(), mode(), stream.sent)
	}
	if _, err := r.Execute(Request{Verb: "attach", Noun: "terminal", Args: Arguments{"id": "pane"}}); err != nil || r.FocusID() != "pane" {
		t.Fatalf("attach: %v focus %q", err, r.FocusID())
	}
	// Paste forwards as text, in and out of the leader.
	if _, err := r.Dispatch(MacroKey{Key: "[ls -la]", Paste: true}); err != nil {
		t.Fatal(err)
	}
	press(t, r, "ctrl+a")
	if _, err := r.Dispatch(MacroKey{Key: "[pwd]", Paste: true}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stream.pasted, []string{"ls -la", "pwd"}) || !reflect.DeepEqual(stream.sent[9:], [][]string{{"ctrl+a"}}) || mode() != "terminal" {
		t.Fatalf("paste: %v sent %v mode %q", stream.pasted, stream.sent[9:], mode())
	}
}

// bindings.list in terminal mode is the terminal table alone: the leader, the
// any and paste rows, no editor rows and no app rows beneath the any row. In
// leader_pending it is the leader table.
func TestTerminalBindingsListLeaderRowsAndNoEditorRows(t *testing.T) {
	r, _, _ := terminalFixture(t)
	keys := func() []string {
		var out []string
		for _, b := range r.ListBindings("pane") {
			key := b.Keys
			if key == "" {
				key = "<" + b.Pattern + ">"
			}
			out = append(out, key+" "+b.Request.Key())
		}
		return out
	}
	if got := keys(); !reflect.DeepEqual(got, []string{"ctrl+a mode.enter", "<any> terminal.send", "<paste> terminal.paste"}) {
		t.Fatalf("terminal mode rows: %v", got)
	}
	press(t, r, "ctrl+a")
	if got := keys(); !reflect.DeepEqual(got, []string{"ctrl+a terminal.send", "d terminal.detach", "<any> terminal.send", "<paste> terminal.paste"}) {
		t.Fatalf("leader_pending rows: %v", got)
	}
	hints := BindingHints(r, "pane")
	if !strings.Contains(hints, "d terminal.detach") || strings.Contains(hints, "app.quit") {
		t.Fatalf("hints: %q", hints)
	}
}

// Frames reach the component through the delivery contract; a frame that
// completes after Close is stale and dropped. The pane is sized to the area
// it renders into.
func TestTerminalFramesArriveThroughDeliveryAndStaleFramesDrop(t *testing.T) {
	r, term, stream := terminalFixture(t)
	first := r.PendingCommands()
	if first == nil {
		t.Fatal("AddTerminal did not schedule the first read")
	}
	stream.frames <- TerminalFrame{Lines: []string{"$ ls", "go.mod"}, Cursor: Pos{X: 4, Y: 0}, Shape: "bar"}
	msgs := runCommands(first)
	if len(msgs) != 1 {
		t.Fatalf("messages: %#v", msgs)
	}
	done := msgs[0].(Completion)
	if done.Owner != "pane" || !r.Accepts(done.Ticket) {
		t.Fatalf("frame ticket %+v", done.Ticket)
	}
	if handled, err := r.Deliver(done); !handled || err != nil {
		t.Fatalf("deliver: %v %v", handled, err)
	}
	if term.Frame().Lines[1] != "go.mod" || term.Frame().Shape != "bar" {
		t.Fatalf("frame not stored: %+v", term.Frame())
	}
	// The read is re-armed as a newer job in the read slot.
	next := r.PendingCommands()
	if jobs := r.runningJobs(); next == nil || len(jobs) != 1 || jobs[0].Job <= done.Job || jobs[0].Owner != "pane" || jobs[0].Slot != "read" {
		t.Fatalf("next read: cmd=%v jobs=%+v", next != nil, jobs)
	}
	if term.ViewBlock(40, 10); !reflect.DeepEqual(stream.resizes, [][2]int{{40, 10}}) {
		t.Fatalf("resize: %v", stream.resizes)
	}
	if term.ViewBlock(40, 10); len(stream.resizes) != 1 {
		t.Fatal("unchanged size was sent again")
	}
	// Close while the frame is in flight: it completes stale and is dropped.
	stream.frames <- TerminalFrame{Lines: []string{"late"}}
	r.closeInstance("pane")
	late := runCommands(next)
	if len(late) != 1 || r.Accepts(late[0].(Completion).Ticket) {
		t.Fatalf("late frame should be stale: %#v", late)
	}
	if handled, err := r.Deliver(late[0]); !handled || err != nil || term.Frame().Lines[0] != "$ ls" {
		t.Fatalf("stale frame reached the terminal: %v %v %+v", handled, err, term.Frame())
	}
	if r.PendingCommands() != nil {
		t.Fatal("closed terminal scheduled another read")
	}
	// A cancelled read is silent; a stream error is reported and ends reading.
	if (&Terminal{id: "x"}).HandleMessage(Completion{Ticket: Ticket{Owner: "x"}, Err: context.Canceled}); term.Err() != "" {
		t.Fatalf("cancelled read reported %q", term.Err())
	}
	other := &Terminal{r: r, id: "pane", stream: stream}
	other.HandleMessage(Completion{Ticket: Ticket{Owner: "pane"}, Err: errors.New("pane closed")})
	if other.Err() != "pane closed" {
		t.Fatalf("stream error: %q", other.Err())
	}
}

// The real terminal cursor lands in the pane inside a panel while the
// terminal is focused, shaped as the pane reports, and disappears when focus
// leaves the pane.
func TestTerminalCursorLandsInsideThePanel(t *testing.T) {
	r, term, stream := terminalFixture(t)
	page := r.PageOf("console")
	page.Render = func(r *Runtime, width, height int) Block {
		layout := Layout{Styles: NewStyles(Appearance{Theme: "mono"})}
		cw, ch := PanelContentSize(width, height)
		return layout.PanelBlock("Pane", term.ViewBlock(cw, ch), width, height, term.Focused())
	}
	m := NewModel(r)
	// Update returns the read scheduled by AddTerminal; each delivered frame
	// returns the next read.
	_, pending := m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	deliver := func(frame TerminalFrame) {
		t.Helper()
		stream.frames <- frame
		msgs := runCommands(pending)
		if len(msgs) != 1 {
			t.Fatalf("expected one frame, got %#v", msgs)
		}
		_, pending = m.Update(msgs[0])
	}
	deliver(TerminalFrame{Lines: []string{"$ echo hi", "hi"}, Cursor: Pos{X: 2, Y: 1}, Shape: "underline"})
	view := m.View()
	if view.Cursor == nil {
		t.Fatalf("no cursor:\n%s", ansi.Strip(view.Content))
	}
	rows := strings.Split(ansi.Strip(view.Content), "\n")
	row := rows[view.Cursor.Y]
	if cell := cellIndex(row, "hi"); cell < 0 || cell+2 != view.Cursor.X || strings.Contains(row, "echo") {
		t.Fatalf("cursor %+v is not after hi inside the panel: %q", view.Cursor.Position, row)
	}
	if view.Cursor.Shape != tea.CursorUnderline {
		t.Fatalf("shape %v, want the pane's underline", view.Cursor.Shape)
	}
	deliver(TerminalFrame{Lines: []string{"$"}, Cursor: Pos{X: 1, Y: 0}})
	if view := m.View(); view.Cursor == nil || view.Cursor.Shape != tea.CursorBlock {
		t.Fatalf("default shape is block: %+v", view.Cursor)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "ctrl+a") {
		t.Fatal("footer does not show the leader")
	}
	press(t, r, "ctrl+a", "d")
	if view := m.View(); view.Cursor != nil {
		t.Fatalf("cursor %+v shown while the pane is not focused", view.Cursor)
	}
	deliver(TerminalFrame{Lines: []string{"$"}, Cursor: Pos{X: 1, Y: 0}, Hidden: true})
	press(t, r, "ctrl+a") // normal mode on the page: not a terminal key
	if r.Mode.Name != "normal" || len(stream.sent) != 0 {
		t.Fatalf("detached pane still took keys: mode %q sent %v", r.Mode.Name, stream.sent)
	}
	if _, err := r.Execute(Request{Verb: "attach", Noun: "terminal"}); err == nil {
		t.Fatal("attach without a terminal on the context or an id must fail")
	}
	if _, err := r.Execute(Request{Verb: "attach", Noun: "terminal", Target: "pane"}); err != nil {
		t.Fatal(err)
	}
	if view := m.View(); view.Cursor != nil {
		t.Fatalf("hidden pane cursor shown: %+v", view.Cursor)
	}
}

// The constructor family shares one option set.
func TestInstanceConstructorsShareOptions(t *testing.T) {
	r := NewRuntime()
	if _, err := r.AddPage("home", "Home", nil); err == nil {
		t.Fatal("page without a renderer accepted")
	}
	if _, err := r.AddPage("home", "Home", func(*Runtime, int, int) Block { return Text("") }, Parent("elsewhere")); err == nil {
		t.Fatal("page under a non-app parent accepted")
	}
	r.Instance("home")
	list, err := r.AddList("rows", []ListItem{{ID: "a", Label: "A", Noun: "home"}}, Parent("home"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddInput(NewInput("note", "", true), Parent("home"), Kind("prompt")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddTerminal("pane", nil, Parent("home")); err == nil {
		t.Fatal("terminal without a stream accepted")
	}
	term, err := r.AddTerminal("pane", newFakeStream(), Parent("home"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := r.AddPage("home", "Home", func(*Runtime, int, int) Block { return Text("") }, Focus("rows"))
	if err != nil {
		t.Fatal(err)
	}
	if page.Frame.Focus != "a" || r.PageOf("home") != page {
		t.Fatalf("page starts on the list's selected row: %+v", page.Frame)
	}
	for id, parent := range map[string]string{"rows": "home", "note": "home", "pane": "home", "a": "rows"} {
		if inst := r.Instances[id]; inst == nil || inst.Parent != parent {
			t.Fatalf("%s parent: %+v", id, inst)
		}
	}
	if r.List("rows") != list || r.Terminal("pane") != term || r.Input("note") == nil || r.Terminal("rows") != nil {
		t.Fatal("typed lookups disagree with the constructors")
	}
	if _, err := r.AddTerminal("pane", newFakeStream()); err == nil {
		t.Fatal("duplicate terminal accepted")
	}
	if kind, _ := JSONPointer(r.Snapshot(), "/components/pane/component"); kind != "terminal" {
		t.Fatalf("snapshot component kind %v", kind)
	}
}
