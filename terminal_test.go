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
	frames   chan TerminalFrame
	sent     [][]string
	pasted   []string
	resizes  [][2]int
	closed   bool
	released int
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
func (f *fakeStream) Release() error { f.released++; return nil }

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
// leader and then itself (sequence-unknown pass); a leader left alone past the timeout forwards
// the leader alone and the next key starts fresh.
func TestTerminalLeaderForwardsBoundRunsAndTimeoutForwardsPrefix(t *testing.T) {
	r, term, stream := terminalFixture(t)
	calls := recorder(t, r, "new-window")
	r.Define("ochazuke.terminal").Mode("leader_pending").Bind("c", Do("test.new-window"))
	mode := func() string { return r.Mode.Name }
	if !term.Focused() || mode() != "terminal" {
		t.Fatalf("focus %q mode %q", r.Focus, mode())
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
	if got := stream.sent[3:]; !reflect.DeepEqual(got, [][]string{{"ctrl+a"}, {"ctrl+a"}, {"z"}, {"ctrl+a"}, {"esc"}}) || mode() != "terminal" {
		t.Fatalf("leader twice, unbound key, escape: %v mode %q", got, mode())
	}
	// Timeout through the next key: the leader is forwarded, then the key.
	press(t, r, "ctrl+a")
	r.prefix.deadline = time.Now().Add(-time.Second)
	press(t, r, "d")
	if got := stream.sent[8:]; !reflect.DeepEqual(got, [][]string{{"ctrl+a"}, {"d"}}) || mode() != "terminal" || r.Focus != "pane" {
		t.Fatalf("expired leader then d: %v mode %q focus %q", got, mode(), r.Focus)
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
	if got := stream.sent[10:]; !reflect.DeepEqual(got, [][]string{{"ctrl+a"}}) || mode() != "terminal" || r.Pending != "" {
		t.Fatalf("timer forwarded %v mode %q pending %q", got, mode(), r.Pending)
	}
	// d under the leader detaches: focus returns to the page and keys stop forwarding.
	press(t, r, "ctrl+a", "d")
	if r.Focus != "console" || mode() != "normal" || len(stream.sent) != 11 {
		t.Fatalf("detach: focus %q mode %q sent %v", r.Focus, mode(), stream.sent)
	}
	if _, err := r.Execute(Request{Verb: "attach", Noun: "ochazuke.terminal", Args: Arguments{"id": "pane"}}); err != nil || r.Focus != "pane" {
		t.Fatalf("attach: %v focus %q", err, r.Focus)
	}
	// Paste forwards as text, in and out of the leader.
	if _, err := r.Dispatch(MacroKey{Key: "[ls -la]", Paste: true}); err != nil {
		t.Fatal(err)
	}
	press(t, r, "ctrl+a")
	if _, err := r.Dispatch(MacroKey{Key: "[pwd]", Paste: true}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stream.pasted, []string{"ls -la", "pwd"}) || !reflect.DeepEqual(stream.sent[11:], [][]string{{"ctrl+a"}}) || mode() != "terminal" {
		t.Fatalf("paste: %v sent %v mode %q", stream.pasted, stream.sent[11:], mode())
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
	if got := keys(); !reflect.DeepEqual(got, []string{"ctrl+a ochazuke.mode.enter", "<any> ochazuke.terminal.send", "<paste> ochazuke.terminal.paste"}) {
		t.Fatalf("terminal mode rows: %v", got)
	}
	if got := BindingHints(r, "pane"); !strings.HasPrefix(got, "ctrl+a leader: d detaches, ctrl+a sends ctrl+a") || strings.Contains(got, "ochazuke.mode.enter") || strings.Contains(got, "ochazuke.terminal.send") {
		t.Fatalf("terminal mode hints: %q", got)
	}
	m := NewModel(r)
	m.Width, m.Height = 120, 20
	if footer := m.footer().Text; !strings.Contains(footer, "detach") {
		t.Fatalf("attached footer: %q", footer)
	}
	for _, b := range r.ListBindings("pane") {
		if b.Keys == "ctrl+a" && b.Hint == "" {
			t.Fatalf("bindings list drops the leader hint: %+v", b)
		}
	}
	press(t, r, "ctrl+a")
	if got := keys(); !reflect.DeepEqual(got, []string{"a ochazuke.terminal.send", "ctrl+a ochazuke.terminal.send", "d ochazuke.terminal.detach", "o ochazuke.focus.next"}) {
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
	if err := r.Settings().Set(RenderMaxFPSSetting, 0); err != nil {
		t.Fatal(err)
	} // this test requests each stream frame explicitly
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
	press(t, r, "ctrl+a") // the app's pane prefix, not a terminal key
	if r.Mode.Name != "normal ctrl+a" || len(stream.sent) != 0 {
		t.Fatalf("detached pane still took keys: mode %q sent %v", r.Mode.Name, stream.sent)
	}
	r.CancelPendingBinding()
	if _, err := r.Execute(Request{Verb: "attach", Noun: "ochazuke.terminal"}); err == nil {
		t.Fatal("attach without a terminal on the context or an id must fail")
	}
	if _, err := r.Execute(Request{Verb: "attach", Noun: "ochazuke.terminal", Target: "pane"}); err != nil {
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
	if err := r.AddInput(NewInput("note", "", true), Parent("home"), Kind("ochazuke.prompt")); err != nil {
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
	if page.Remembered != "a" || r.PageOf("home") != page {
		t.Fatalf("page starts on the list's selected row: %+v", page)
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

// Closing a terminal releases its stream once: RemoveInstance releases it, a
// plain detach keeps it for a later attach, and a CloseOnDetach terminal is
// removed (and released) by detach.
func TestTerminalCloseReleasesStream(t *testing.T) {
	r, term, stream := terminalFixture(t)
	if _, err := r.Execute(Request{Verb: "detach", Noun: "ochazuke.terminal"}); err != nil || stream.released != 0 || r.Terminal("pane") != term {
		t.Fatalf("plain detach: %v released=%d", err, stream.released)
	}
	if err := r.RemoveInstance("pane"); err != nil {
		t.Fatal(err)
	}
	term.Close()
	if stream.released != 1 || r.Terminal("pane") != nil {
		t.Fatalf("RemoveInstance: released=%d", stream.released)
	}
	other := newFakeStream()
	if _, err := r.AddTerminal("live", other, Parent("console"), CloseOnDetach()); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("live"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Verb: "detach", Noun: "ochazuke.terminal"}); err != nil {
		t.Fatal(err)
	}
	if other.released != 1 || r.Instances["live"] != nil || r.Focus != "console" {
		t.Fatalf("CloseOnDetach: released=%d instance=%v focus=%q", other.released, r.Instances["live"], r.Focus)
	}
}

// focusOnCloseFixture is a page holding a list of rows with b selected.
func focusOnCloseFixture(t *testing.T) *Runtime {
	t.Helper()
	r := NewRuntime()
	r.BindingTimeout = time.Hour
	r.Define("row")
	r.Instance("panes")
	list, err := r.AddList("rows", []ListItem{{ID: "a", Label: "A", Noun: "row"}, {ID: "b", Label: "B", Noun: "row"}}, Parent("panes"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddPage("panes", "Panes", func(*Runtime, int, int) Block { return Text("") }, Focus("rows")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("panes"); err != nil {
		t.Fatal(err)
	}
	if err := list.Select("b"); err != nil {
		t.Fatal(err)
	}
	return r
}

// A CloseOnDetach terminal with FocusOnClose hands the focus to the named
// row on the leader detach; a target that does not exist falls back to the
// parent.
func TestTerminalFocusOnCloseReturnsToNamedInstance(t *testing.T) {
	for _, tc := range []struct{ target, want string }{{"b", "b"}, {"missing", "panes"}} {
		r := focusOnCloseFixture(t)
		stream := newFakeStream()
		if _, err := r.AddTerminal("live", stream, Parent("panes"), CloseOnDetach(), FocusOnClose(tc.target)); err != nil {
			t.Fatal(err)
		}
		if err := r.SetFocus("live"); err != nil {
			t.Fatal(err)
		}
		press(t, r, "ctrl+a", "d")
		if stream.released != 1 || r.Instances["live"] != nil || r.Focus != tc.want {
			t.Fatalf("FocusOnClose(%q): released=%d instance=%v focus=%q want %q", tc.target, stream.released, r.Instances["live"], r.Focus, tc.want)
		}
	}
}

// FocusOnClose is not terminal-specific: a modal input removed by Pop and an
// input removed by RemoveInstance both send the focus to their target.
func TestFocusOnCloseOnPopAndRemoveInstance(t *testing.T) {
	r := focusOnCloseFixture(t)
	if err := r.SetFocus("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.OpenModal("ask", Payload{}, WithInput(NewInput("answer", "", false), FocusOnClose("b"))); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "answer" {
		t.Fatalf("modal focus %q", r.Focus)
	}
	if err := r.Pop(); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "b" || r.Instances["answer"] != nil {
		t.Fatalf("Pop: focus=%q", r.Focus)
	}
	if err := r.AddInput(NewInput("note", "", false), Parent("panes"), FocusOnClose("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetFocus("note"); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveInstance("note"); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "a" {
		t.Fatalf("RemoveInstance: focus=%q", r.Focus)
	}
}

// The terminal-leader setting moves the leader: EnsureTerminalLeader
// declares it, settings.json's value applies, the old leader becomes ordinary
// input, the new one detaches, sends itself, forwards with an unbound key
// and alone on timeout, the hint names it, an application's leader_pending
// row stays, and a change applies at once. A key that is not a ctrl+ or
// alt+ combination is refused.
func TestTerminalLeaderSetting(t *testing.T) {
	r, _, stream := terminalFixture(t)
	calls := recorder(t, r, "zoom")
	r.Define("ochazuke.terminal").Mode("leader_pending").Bind("z", Do("test.zoom"))
	dir := t.TempDir()
	r.Config().Dir = dir
	writeConfig(t, dir, SettingsFile, `{"terminal-leader": "ctrl+b"}`)
	m := NewModel(r)
	r.EnsureTerminalLeader()
	m.Init()
	if got := r.TerminalLeader(); got != "ctrl+b" {
		t.Fatalf("leader %q", got)
	}
	if got := BindingHints(r, "pane"); !strings.HasPrefix(got, "ctrl+b leader: d detaches, ctrl+b sends ctrl+b") {
		t.Fatalf("hint %q", got)
	}
	press(t, r, "ctrl+a", "ctrl+b", "ctrl+b", "ctrl+b", "x", "ctrl+b", "z")
	want := [][]string{{"ctrl+a"}, {"ctrl+b"}, {"ctrl+b"}, {"x"}}
	if !reflect.DeepEqual(stream.sent, want) || !reflect.DeepEqual(*calls, []string{"zoom"}) || r.Mode.Name != "terminal" {
		t.Fatalf("sent %v calls %v mode %q", stream.sent, *calls, r.Mode.Name)
	}
	press(t, r, "ctrl+b")
	if r.Pending != "ctrl+b" {
		t.Fatalf("pending %q", r.Pending)
	}
	r.prefix.deadline = time.Now().Add(-time.Second)
	if handled, err := r.HandleBindingTimeout(BindingTimeoutMsg{runtime: r, version: r.bindingVersion}); !handled || err != nil {
		t.Fatalf("timer: %v %v", handled, err)
	}
	if got := stream.sent[4:]; !reflect.DeepEqual(got, [][]string{{"ctrl+b"}}) {
		t.Fatalf("timeout sent %v", got)
	}
	press(t, r, "ctrl+b", "d")
	if r.Focus != "console" || r.Mode.Name != "normal" {
		t.Fatalf("detach: focus %q mode %q", r.Focus, r.Mode.Name)
	}
	s := r.Settings()
	if err := s.Set(TerminalLeaderSetting, "x"); err == nil || !strings.Contains(err.Error(), "ctrl+ or alt+") {
		t.Fatalf("plain key accepted: %v", err)
	}
	if err := s.Set(TerminalLeaderSetting, "alt+a"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Verb: "attach", Noun: "ochazuke.terminal", Args: Arguments{"id": "pane"}}); err != nil {
		t.Fatal(err)
	}
	press(t, r, "ctrl+b", "alt+a", "alt+a", "alt+a", "d")
	if got := stream.sent[5:]; !reflect.DeepEqual(got, [][]string{{"ctrl+b"}, {"alt+a"}}) || r.Focus != "console" {
		t.Fatalf("after the change: sent %v focus %q", got, r.Focus)
	}
	keys := map[string]bool{}
	for _, b := range r.Nouns["ochazuke.terminal"].Bindings["leader_pending"] {
		keys[b.Keys] = true
	}
	if keys["ctrl+a"] || keys["ctrl+b"] || !keys["alt+a"] || !keys["z"] {
		t.Fatalf("leader_pending keys %v", keys)
	}
	if err := r.SetTerminalLeader("ctrl+a d"); err == nil {
		t.Fatal("a sequence accepted as the leader")
	}
}

// A leader row an application defines with its own hint names the
// setting as the framework's does ({terminal-leader}, in its keys, prefix
// and request), so it moves with the setting, declared before or after it.
func TestTerminalLeaderMovesApplicationLeaderRows(t *testing.T) {
	r := NewRuntime()
	leader := Request{Verb: "send", Noun: "ochazuke.terminal", Args: Arguments{"keys": "{terminal-leader}"}}
	r.Define("ochazuke.terminal").Mode("terminal").Bind("{terminal-leader}", Enter("leader_pending", Payload{Prefix: "{terminal-leader}", Request: &leader}).Hinted("leader: v vim · d detach"))
	if err := r.DeclareSetting(Setting{Name: TerminalLeaderSetting, Type: SettingString, Default: "ctrl+t"}); err != nil {
		t.Fatal(err)
	}
	r.Instance("console")
	if _, err := r.AddTerminal("pane", newFakeStream(), Parent("console")); err != nil {
		t.Fatal(err)
	}
	var row Binding
	for _, b := range r.Nouns["ochazuke.terminal"].Bindings["terminal"] {
		if b.Keys == "ctrl+t" {
			row = b
		}
	}
	q, _ := row.Request.Args["request"].(Request)
	if row.Hint != "leader: v vim · d detach" || row.Request.Args["prefix"] != "ctrl+t" || q.Args["keys"] != "ctrl+t" || leader.Args["keys"] != "{terminal-leader}" {
		t.Fatalf("row %+v request %+v", row, q)
	}
	if _, err := r.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
}
