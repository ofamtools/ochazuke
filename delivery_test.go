package ochazuke

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// slowComponent is a fake component whose work finishes when the test says
// so; it records every completion the runtime delivers to it.
type slowComponent struct {
	r        *Runtime
	id       string
	received []Completion
	closed   int
}

func (s *slowComponent) HandleMessage(msg tea.Msg) bool {
	done, ok := msg.(Completion)
	if !ok || done.Owner != s.id {
		return false
	}
	s.received = append(s.received, done)
	return true
}
func (s *slowComponent) PendingCommand() tea.Cmd { return nil }
func (s *slowComponent) Close()                  { s.closed++ }

// start schedules work that completes with value when release is closed.
func (s *slowComponent) start(value string, release <-chan struct{}) (Ticket, tea.Cmd) {
	ticket, err := s.r.Start(s.id, func(ctx context.Context) (any, error) {
		select {
		case <-release:
			return value, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, nil, Ctx{})
	if err != nil {
		panic(err)
	}
	return ticket, s.r.PendingCommands()
}

// runCmd executes a command (or batch) and returns its messages.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, child := range batch {
			out = append(out, runCmd(child)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// The contract: a late result after Close is dropped, a result with an old
// generation is dropped, a current one is delivered to the owner.
func TestComponentDeliveryDropsStaleAndClosedResults(t *testing.T) {
	r := NewRuntime()
	r.Instance("job", Kind("job"))
	comp := &slowComponent{r: r, id: "job"}
	r.Instances["job"].Component = comp

	// An old generation: starting newer work supersedes it.
	oldRelease := make(chan struct{})
	oldTicket, oldCmd := comp.start("old", oldRelease)
	newRelease := make(chan struct{})
	newTicket, newCmd := comp.start("new", newRelease)
	if oldTicket.Generation >= newTicket.Generation || r.Generation("job") != newTicket.Generation {
		t.Fatalf("generations: old=%d new=%d current=%d", oldTicket.Generation, newTicket.Generation, r.Generation("job"))
	}
	// The superseded context is cancelled, so the old command returns at once.
	for _, msg := range runCmd(oldCmd) {
		if handled, _ := r.Deliver(msg); !handled {
			t.Fatal("a stale completion is consumed, not offered elsewhere")
		}
	}
	if len(comp.received) != 0 {
		t.Fatalf("stale generation delivered: %+v", comp.received)
	}
	close(newRelease)
	for _, msg := range runCmd(newCmd) {
		r.Deliver(msg)
	}
	if len(comp.received) != 1 || comp.received[0].Value != "new" || comp.received[0].Generation != newTicket.Generation {
		t.Fatalf("current generation not delivered: %+v", comp.received)
	}

	// A late result after Close is dropped and the work is cancelled.
	lateRelease := make(chan struct{})
	_, lateCmd := comp.start("late", lateRelease)
	r.closeInstance("job")
	if comp.closed != 1 || r.Instances["job"].work != nil {
		t.Fatalf("Close: closed=%d work=%v", comp.closed, r.Instances["job"].work)
	}
	msgs := runCmd(lateCmd) // returns promptly: the context was cancelled
	for _, msg := range msgs {
		r.Deliver(msg)
	}
	if len(comp.received) != 1 {
		t.Fatalf("late result delivered after Close: %+v", comp.received)
	}
	// The instance registry drives the component list: a removed instance
	// is closed, and a message for nobody is not consumed.
	r.removeInstance("job")
	if comp.closed != 2 {
		t.Fatalf("removeInstance must close the component: %d", comp.closed)
	}
	if handled, _ := r.Deliver(Completion{Ticket: Ticket{Owner: "job", Generation: 1}}); !handled {
		t.Fatal("an orphaned completion is dropped silently")
	}
	if handled, _ := r.Deliver(tea.WindowSizeMsg{}); handled {
		t.Fatal("an unclaimed message is reported unhandled")
	}
}

// An application verb returns a ticket, keys keep flowing while it runs,
// its completion lands on the instance that issued it, and a completion
// for an instance that closed meanwhile is dropped.
func TestApplicationVerbRunsOffTheLoop(t *testing.T) {
	r, list, _, _ := listFixture(t)
	release := make(chan struct{})
	var landed []string
	r.Define("a").Verb("sync", func(c Ctx) Result {
		id := c.Instance.ID
		return Pending(func(ctx context.Context) (any, error) {
			select {
			case <-release:
				return "synced " + id, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}).Then(func(c Ctx, value any, err error) {
			landed = append(landed, c.Instance.ID+"="+value.(string))
		})
	}, "Sync a row off the loop").Verb("now", func(c Ctx) Result { return Done("now " + c.Instance.ID) })
	if op := r.Operations["sync.a"]; op.Kind != KindApplication || r.Operations["now.a"].Kind != KindApplication || r.Operations["move.list"].Kind != KindInteraction {
		t.Fatalf("kinds: %+v", op)
	}
	if v, err := r.Execute(Request{Verb: "now", Noun: "a", Target: "one"}); err != nil || v != "now one" {
		t.Fatalf("synchronous application result: %v %v", v, err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	value, err := r.Execute(Request{Verb: "sync", Noun: "a"})
	if err != nil {
		t.Fatal(err)
	}
	ticket, ok := value.(Ticket)
	if !ok || ticket.Owner != "one" || time.Since(start) > time.Second {
		t.Fatalf("pending verb must return a ticket at once: %#v", value)
	}
	cmd := r.PendingCommands()
	if cmd == nil {
		t.Fatal("pending work must be scheduled through the adapter")
	}
	// Keys are handled while the work is outstanding.
	editorTestKeys(t, r, "j")
	if r.FocusID() != "two" {
		t.Fatalf("key handling blocked: focus %s", r.FocusID())
	}
	done := make(chan []tea.Msg, 1)
	go func() { done <- runCmd(cmd) }()
	close(release)
	for _, msg := range <-done {
		if handled, err := r.Deliver(msg); !handled || err != nil {
			t.Fatalf("deliver: %v %v", handled, err)
		}
	}
	if !reflect.DeepEqual(landed, []string{"one=synced one"}) {
		t.Fatalf("completion landed on %v", landed)
	}
	// A completion after the instance closed is dropped.
	release2 := make(chan struct{})
	release = release2
	if _, err := r.Execute(Request{Verb: "sync", Noun: "a", Target: "one"}); err != nil {
		t.Fatal(err)
	}
	cmd = r.PendingCommands()
	if err := list.SetItems([]ListItem{{ID: "two", Noun: "b"}}); err != nil {
		t.Fatal(err)
	}
	for _, msg := range runCmd(cmd) {
		r.Deliver(msg)
	}
	if len(landed) != 1 {
		t.Fatalf("closed instance received a completion: %v", landed)
	}
	// A failing synchronous application result is an ordinary error.
	r.Define("b").Verb("fail", func(Ctx) Result { return Failed(errors.New("nope")) })
	if _, err := r.Execute(Request{Verb: "fail", Noun: "b", Target: "two"}); err == nil || err.Error() != "nope" {
		t.Fatalf("Failed: %v", err)
	}
	if list := r.Catalog(); !slices.ContainsFunc(list, func(op Operation) bool { return op.Key() == "sync.a" && op.Kind == KindApplication }) {
		t.Fatal("list.operations must show the kind")
	}
}

// Confirm compiles to the framework confirm noun: a modal instance parented
// at the focus with the prompt and the stored request in its payload; y runs
// it, n and esc pop, other keys are swallowed; set.confirm adds or removes it.
func TestConfirmCompilesToAModalWithPayload(t *testing.T) {
	r, list, _, _ := listFixture(t)
	var removed []string
	r.Define("a").Verb("remove", func(_ *Runtime, c Context, _ Arguments) (any, error) {
		removed = append(removed, focusOf(c))
		return nil, nil
	}).Confirm("Remove {{noun}} {{id}}?").Mode("normal").Bind("d", Do("remove.a"))
	if r.Operations["remove.a"].Confirm == "" {
		t.Fatal("Confirm must mark the operation")
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "d")
	inst := r.Instances["confirm"]
	if len(r.Frames) != 2 || r.FocusID() != "confirm" || inst == nil || inst.Parent != "one" || inst.Payload.Prompt != "Remove a one?" || inst.Payload.Request == nil || !inst.Payload.Request.Confirmed || inst.Payload.Request.Target != "one" {
		t.Fatalf("confirm modal: frames=%d focus=%s inst=%+v", len(r.Frames), r.FocusID(), inst)
	}
	if rows := r.ListBindings("confirm"); !slices.ContainsFunc(rows, func(b Binding) bool { return b.Keys == "y" && b.Request.Key() == "accept.confirm" }) {
		t.Fatalf("confirm rows: %+v", rows)
	}
	editorTestKeys(t, r, "d", "x") // swallowed: no second confirm, no removal
	if len(r.Frames) != 2 || len(removed) != 0 {
		t.Fatalf("other keys must be swallowed: frames=%d removed=%v", len(r.Frames), removed)
	}
	editorTestKeys(t, r, "n")
	if len(r.Frames) != 1 || r.Instances["confirm"] != nil || len(removed) != 0 || r.FocusID() != "one" {
		t.Fatalf("n must pop without running: frames=%d removed=%v focus=%s", len(r.Frames), removed, r.FocusID())
	}
	editorTestKeys(t, r, "d", "esc")
	if len(r.Frames) != 1 || len(removed) != 0 {
		t.Fatal("esc must pop without running")
	}
	editorTestKeys(t, r, "d", "y")
	if len(r.Frames) != 1 || !reflect.DeepEqual(removed, []string{"one"}) {
		t.Fatalf("y must run the stored request at its origin: frames=%d removed=%v", len(r.Frames), removed)
	}
	// A confirmed request from a script skips the prompt; OpenModal is the
	// generic form behind it.
	if _, err := r.Execute(Request{Verb: "remove", Noun: "a", Target: "one", Confirmed: true}); err != nil || len(removed) != 2 {
		t.Fatalf("confirmed request: %v %v", err, removed)
	}
	if _, err := r.Execute(Request{Verb: "set", Noun: "confirm", Args: Arguments{"operation": "remove.a", "prompt": ""}}); err != nil || r.Operations["remove.a"].Confirm != "" {
		t.Fatalf("set.confirm removal: %v", err)
	}
	editorTestKeys(t, r, "d")
	if len(r.Frames) != 1 || len(removed) != 3 {
		t.Fatal("a removed confirm runs the verb directly")
	}
	if _, err := r.Execute(Request{Verb: "set", Noun: "confirm", Args: Arguments{"operation": "remove.a", "prompt": "Sure?"}}); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "d")
	if r.FocusID() != "confirm" || r.Instances["confirm"].Payload.Prompt != "Sure?" {
		t.Fatal("set.confirm must add a prompt")
	}
	if body, err := r.OpenModal("confirm", Payload{}); err == nil || body != nil {
		t.Fatal("a second modal of the same kind is refused while open")
	}
	editorTestKeys(t, r, "y")
	if v, _ := JSONPointer(r.Snapshot(), "/instances/confirm"); v != nil {
		t.Fatal("Pop removes the confirm instance")
	}
}

// Runtime holds registries, focus, frames, page, pending-sequence state and
// the adapter hook, and no component fields. The list is exact so the
// struct cannot regrow silently.
func TestRuntimeHoldsNoComponentFields(t *testing.T) {
	var fields []string
	typ := reflect.TypeOf(Runtime{})
	for i := 0; i < typ.NumField(); i++ {
		fields = append(fields, typ.Field(i).Name)
	}
	sort.Strings(fields)
	want := []string{
		"BindingTimeout", "Frames", "Instances", "Modes", "Nouns", "Operations", "Page", "Pending", "Quit",
		"bindingVersion", "commands", "guards", "modalInstances", "modeGeneration", "pendingModes", "prefix", "rows", "seq",
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("Runtime fields:\n got %v\nwant %v", fields, want)
	}
	// Inputs, lists and the registers are instances with typed lookups.
	r, list, input, _ := listFixture(t)
	if r.Input("note") != input || r.List("sidebar") != list || r.Input("sidebar") != nil || r.List("note") != nil {
		t.Fatal("typed lookups must read the registry")
	}
	if ids := func() (out []string) {
		for _, in := range r.Inputs() {
			out = append(out, in.ID)
		}
		return
	}(); !reflect.DeepEqual(ids, []string{"note"}) || len(r.Lists()) != 1 {
		t.Fatalf("Inputs()/Lists(): %v %d", ids, len(r.Lists()))
	}
	regs := r.Registers()
	if regs == nil || r.Instances[registersID].Parent != "app" || r.Instances[registersID].Component != regs {
		t.Fatal("registers must be one instance under app")
	}
	if _, err := r.Execute(Request{Verb: "set", Noun: "register", Args: Arguments{"name": "a", "text": "shared"}}); err != nil {
		t.Fatal(err)
	}
	snapshot := r.Snapshot()
	for path, want := range map[string]any{"/registers/a/yank/text": "shared", "/yankHistory/0/text": "shared", "/inputs/note/id": "note", "/lists/sidebar/id": "sidebar", "/components/__registers/component": "registers", "/components/note/component": "input", "/components/sidebar/contract": false} {
		if got, err := JSONPointer(snapshot, path); err != nil || got != want {
			t.Fatalf("%s = %v (%v), want %v", path, got, err, want)
		}
	}
}

// The editor fallback level exists only when an input is on the focus
// path: a list row lists no editor rows and an unbound editor key is
// unhandled, while counts, registers and macros (app rows) still answer.
func TestEditorFallbackNeedsAnInputOnThePath(t *testing.T) {
	r, list, input, _ := listFixture(t)
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	for _, b := range r.ListBindings("one") {
		if b.Noun == "editor" {
			t.Fatalf("editor row listed for a list row: %+v", b)
		}
	}
	for _, key := range []string{"x", "u", "p", "i", "d"} {
		if handled, err := r.Dispatch(MacroKey{Key: key}); handled || err != nil {
			t.Fatalf("%s on a list row: handled=%v err=%v", key, handled, err)
		}
	}
	if r.Mode(r.Context()) != "normal" {
		t.Fatalf("mode after unbound editor keys: %s", r.Mode(r.Context()))
	}
	editorTestKeys(t, r, "2", "q")
	if got := r.modeState("one"); got.Name != "macro_pending" {
		t.Fatalf("count and macro rows stay available on a row: %+v", got)
	}
	editorTestKeys(t, r, "esc")
	if err := r.Focus(input.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(r.ListBindings(input.ID), func(b Binding) bool { return b.Noun == "editor" }) {
		t.Fatal("an input keeps the editor fallback")
	}
	if !strings.Contains(strings.Join(func() (out []string) {
		for _, b := range r.ListBindings("one") {
			out = append(out, b.Keys)
		}
		return
	}(), " "), "q") {
		t.Fatal("q is an app row")
	}
}
