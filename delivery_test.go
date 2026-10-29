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

// The contract: jobs on one owner accumulate by default, so every
// completion lands with its ticket in finish order; a job in a slot is
// replaced by a newer job in the same slot; a result for a closed owner is
// dropped and an orphaned completion is consumed silently.
func TestComponentDeliveryAccumulatesIndependentJobs(t *testing.T) {
	r := NewRuntime()
	r.Instance("job", Kind("job"))
	comp := &slowComponent{r: r, id: "job"}
	r.Instances["job"].Component = comp
	values := []string{"fetch", "commit", "push", "pull", "status"}
	releases := map[string]chan struct{}{}
	tickets := map[string]Ticket{}
	var cmds []tea.Cmd
	for _, value := range values {
		releases[value] = make(chan struct{})
		ticket, cmd := comp.start(value, releases[value])
		tickets[value] = ticket
		cmds = append(cmds, cmd)
	}
	for _, value := range values {
		if ticket := tickets[value]; ticket.Owner != "job" || ticket.Slot != "" || !r.Accepts(ticket) {
			t.Fatalf("%s: ticket %+v is not a running accumulate job", value, ticket)
		}
	}
	if jobs := r.runningJobs(); len(jobs) != 5 || jobs[0].Verb != "work" || jobs[4].Job != tickets["status"].Job {
		t.Fatalf("running jobs: %+v", jobs)
	}
	// Finish in an order different from the start order: every result lands.
	finish := []string{"push", "fetch", "status", "commit", "pull"}
	for _, value := range finish {
		close(releases[value])
		for _, msg := range runCmd(cmds[slices.Index(values, value)]) {
			if handled, err := r.Deliver(msg); !handled || err != nil {
				t.Fatalf("deliver %s: %v %v", value, handled, err)
			}
		}
	}
	var got []string
	for _, done := range comp.received {
		if done.Ticket != tickets[done.Value.(string)] {
			t.Fatalf("completion %v carries ticket %+v, want %+v", done.Value, done.Ticket, tickets[done.Value.(string)])
		}
		got = append(got, done.Value.(string))
	}
	if !reflect.DeepEqual(got, finish) {
		t.Fatalf("delivered %v in finish order %v", got, finish)
	}
	if jobs := r.runningJobs(); len(jobs) != 0 {
		t.Fatalf("jobs still running: %+v", jobs)
	}
	// A late result after Close is dropped and the work is cancelled.
	lateRelease := make(chan struct{})
	late, lateCmd := comp.start("late", lateRelease)
	r.closeInstance("job")
	if comp.closed != 1 || r.Accepts(late) || len(r.runningJobs()) != 0 {
		t.Fatalf("Close: closed=%d accepts=%v running=%+v", comp.closed, r.Accepts(late), r.runningJobs())
	}
	msgs := runCmd(lateCmd) // returns promptly: the context was cancelled
	for _, msg := range msgs {
		if handled, err := r.Deliver(msg); !handled || err != nil {
			t.Fatal("a stale completion is consumed, not offered elsewhere")
		}
	}
	if len(comp.received) != 5 {
		t.Fatalf("late result delivered after Close: %+v", comp.received)
	}
	// The instance registry drives the component list: a removed instance
	// is closed, and a message for nobody is not consumed.
	_ = r.RemoveInstance("job")
	if comp.closed != 2 {
		t.Fatalf("removeInstance must close the component: %d", comp.closed)
	}
	if handled, _ := r.Deliver(Completion{Ticket: Ticket{Owner: "job", Job: 1}}); !handled {
		t.Fatal("an orphaned completion is dropped silently")
	}
	if handled, _ := r.Deliver(tea.WindowSizeMsg{}); handled {
		t.Fatal("an unclaimed message is reported unhandled")
	}
}

// Replacing(slot): starting a newer job in a slot cancels the older one on
// the same owner, so only the newest completion lands; another slot and
// another owner are untouched.
func TestComponentDeliveryReplacesJobsInASlot(t *testing.T) {
	r := NewRuntime()
	r.Instance("pane", Kind("ochazuke.pane"))
	r.Instance("other", Kind("ochazuke.pane"))
	pane := &slowComponent{r: r, id: "pane"}
	other := &slowComponent{r: r, id: "other"}
	r.Instances["pane"].Component, r.Instances["other"].Component = pane, other
	start := func(owner, slot, value string, release chan struct{}) (Ticket, tea.Cmd) {
		ticket, err := r.Launch(owner, "preview", Pending(func(ctx context.Context) (any, error) {
			select {
			case <-release:
				return value, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}).Replacing(slot), Ctx{})
		if err != nil {
			t.Fatal(err)
		}
		return ticket, r.PendingCommands()
	}
	oldRelease, newRelease, indexRelease, otherRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	oldTicket, oldCmd := start("pane", "preview", "old", oldRelease)
	indexTicket, indexCmd := start("pane", "index", "index", indexRelease)
	otherTicket, otherCmd := start("other", "preview", "other", otherRelease)
	newTicket, newCmd := start("pane", "preview", "new", newRelease)
	if r.Accepts(oldTicket) || !r.Accepts(newTicket) || !r.Accepts(indexTicket) || !r.Accepts(otherTicket) || oldTicket.Slot != "preview" {
		t.Fatalf("slot claim: old=%v new=%v index=%v other=%v", r.Accepts(oldTicket), r.Accepts(newTicket), r.Accepts(indexTicket), r.Accepts(otherTicket))
	}
	// The replaced job is cancelled, so its command returns at once, and the
	// list records it as cancelled until read.
	for _, msg := range runCmd(oldCmd) {
		if handled, err := r.Deliver(msg); !handled || err != nil {
			t.Fatalf("stale completion: %v %v", handled, err)
		}
	}
	if len(pane.received) != 0 {
		t.Fatalf("replaced job delivered: %+v", pane.received)
	}
	if jobs := r.Jobs(); len(jobs) != 4 || jobs[0].State != JobCancelled || jobs[0].Job != oldTicket.Job || jobs[3].State != JobRunning {
		t.Fatalf("jobs after replace: %+v", jobs)
	}
	close(newRelease)
	close(indexRelease)
	close(otherRelease)
	for _, cmd := range []tea.Cmd{newCmd, indexCmd, otherCmd} {
		for _, msg := range runCmd(cmd) {
			r.Deliver(msg)
		}
	}
	if len(pane.received) != 2 || pane.received[0].Value != "new" || pane.received[0].Ticket != newTicket || pane.received[1].Value != "index" || len(other.received) != 1 || other.received[0].Value != "other" {
		t.Fatalf("delivered: pane=%+v other=%+v", pane.received, other.received)
	}
	// jobs.list returns the finished entries once, then drops them.
	value, err := r.Execute(Request{Verb: "list", Noun: "ochazuke.jobs"})
	if err != nil {
		t.Fatal(err)
	}
	listed := value.([]JobInfo)
	states := map[string]int{}
	for _, j := range listed {
		states[j.State]++
	}
	if len(listed) != 4 || states[JobCancelled] != 1 || states[JobDone] != 3 || listed[0].Verb != "preview" || listed[0].Owner != "pane" || listed[0].Started.IsZero() {
		t.Fatalf("jobs.list: %+v", listed)
	}
	if again, _ := r.Execute(Request{Verb: "list", Noun: "ochazuke.jobs"}); len(again.([]JobInfo)) != 0 {
		t.Fatalf("finished jobs retained after a read: %+v", again)
	}
	// A failed job reports its error and a running one shows in the snapshot.
	failRelease := make(chan struct{})
	failTicket, err := r.Launch("pane", "fetch", Pending(func(ctx context.Context) (any, error) {
		<-failRelease
		return nil, errors.New("boom")
	}), Ctx{})
	if err != nil {
		t.Fatal(err)
	}
	failCmd := r.PendingCommands()
	if got, err := JSONPointer(r.Snapshot(), "/jobs/0/verb"); err != nil || got != "fetch" {
		t.Fatalf("/jobs: %v %v", got, err)
	}
	close(failRelease)
	for _, msg := range runCmd(failCmd) {
		if handled, err := r.Deliver(msg); !handled || err == nil {
			t.Fatalf("failed job: %v %v", handled, err)
		}
	}
	if jobs := r.Jobs(); len(jobs) != 1 || jobs[0].Job != failTicket.Job || jobs[0].State != JobFailed || jobs[0].Error != "boom" {
		t.Fatalf("failed job listed: %+v", jobs)
	}
	if _, err := r.Launch("nobody", "x", Pending(func(context.Context) (any, error) { return nil, nil }), Ctx{}); err == nil {
		t.Fatal("Launch on an unknown instance must fail")
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
	if op := r.Operations["a.sync"]; op.Kind != KindApplication || r.Operations["a.now"].Kind != KindApplication || r.Operations["ochazuke.list.move"].Kind != KindInteraction {
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
	if r.Focus != "two" {
		t.Fatalf("key handling blocked: focus %s", r.Focus)
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
	if list := r.Catalog(); !slices.ContainsFunc(list, func(op Operation) bool { return op.Key() == "a.sync" && op.Kind == KindApplication }) {
		t.Fatal("operations.list must show the kind")
	}
}

// Confirm compiles to the framework confirm noun: a modal instance parented
// at the focus with the prompt and the stored request in its payload; y runs
// it, n and esc pop, other keys are swallowed; confirm.set adds or removes it.
func TestConfirmCompilesToAModalWithPayload(t *testing.T) {
	r, list, _, _ := listFixture(t)
	var removed []string
	r.Define("a").Verb("remove", func(_ *Runtime, c Context, _ Arguments) (any, error) {
		removed = append(removed, focusOf(c))
		return nil, nil
	}).Confirm("Remove {{noun}} {{id}}?").Mode("normal").Bind("d", Do("a.remove"))
	if r.Operations["a.remove"].Confirm == "" {
		t.Fatal("Confirm must mark the operation")
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "d")
	inst := r.Instances["confirm"]
	if len(r.Modals()) != 1 || r.Focus != "confirm" || inst == nil || inst.Parent != "one" || inst.Payload.Prompt != "Remove a one?" || inst.Payload.Request == nil || !inst.Payload.Request.Confirmed || inst.Payload.Request.Target != "one" {
		t.Fatalf("confirm modal: frames=%d focus=%s inst=%+v", len(r.Modals()), r.Focus, inst)
	}
	if rows := r.ListBindings("confirm"); !slices.ContainsFunc(rows, func(b Binding) bool { return b.Keys == "y" && b.Request.Key() == "ochazuke.confirm.accept" }) {
		t.Fatalf("confirm rows: %+v", rows)
	}
	editorTestKeys(t, r, "d", "x") // swallowed: no second confirm, no removal
	if len(r.Modals()) != 1 || len(removed) != 0 {
		t.Fatalf("other keys must be swallowed: frames=%d removed=%v", len(r.Modals()), removed)
	}
	editorTestKeys(t, r, "n")
	if len(r.Modals()) != 0 || r.Instances["confirm"] != nil || len(removed) != 0 || r.Focus != "one" {
		t.Fatalf("n must pop without running: frames=%d removed=%v focus=%s", len(r.Modals()), removed, r.Focus)
	}
	editorTestKeys(t, r, "d", "esc")
	if len(r.Modals()) != 0 || len(removed) != 0 {
		t.Fatal("esc must pop without running")
	}
	editorTestKeys(t, r, "d", "y")
	if len(r.Modals()) != 0 || !reflect.DeepEqual(removed, []string{"one"}) {
		t.Fatalf("y must run the stored request at its origin: frames=%d removed=%v", len(r.Modals()), removed)
	}
	// A confirmed request from a script skips the prompt; OpenModal is the
	// generic form behind it.
	if _, err := r.Execute(Request{Verb: "remove", Noun: "a", Target: "one", Confirmed: true}); err != nil || len(removed) != 2 {
		t.Fatalf("confirmed request: %v %v", err, removed)
	}
	if _, err := r.Execute(Request{Verb: "set", Noun: "ochazuke.confirm", Args: Arguments{"operation": "a.remove", "prompt": ""}}); err != nil || r.Operations["a.remove"].Confirm != "" {
		t.Fatalf("confirm.set removal: %v", err)
	}
	editorTestKeys(t, r, "d")
	if len(r.Modals()) != 0 || len(removed) != 3 {
		t.Fatal("a removed confirm runs the verb directly")
	}
	if _, err := r.Execute(Request{Verb: "set", Noun: "ochazuke.confirm", Args: Arguments{"operation": "a.remove", "prompt": "Sure?"}}); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, "d")
	if r.Focus != "confirm" || r.Instances["confirm"].Payload.Prompt != "Sure?" {
		t.Fatal("confirm.set must add a prompt")
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
		"BindingTimeout", "Focus", "Instances", "Mode", "Modes", "Nouns", "Operations", "Page", "Pending", "Quit", "ResolveTarget", "actionNames", "aliases",
		"bindingVersion", "builtinDepth", "commands", "defaults", "deprecations", "guards", "history", "holders", "jobSeq", "jobs", "modalInstances", "modals", "modeExtends", "modeGeneration", "origin", "passthroughModes", "pendingModes", "predicates", "prefix", "rowRequest", "rows", "selectionModes", "seq", "subjects",
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
	if _, err := r.Execute(Request{Verb: "set", Noun: "ochazuke.register", Args: Arguments{"name": "a", "text": "shared"}}); err != nil {
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
		if b.Noun == "ochazuke.editor" {
			t.Fatalf("editor row listed for a list row: %+v", b)
		}
	}
	for _, key := range []string{"x", "u", "p", "i", "d"} {
		if handled, err := r.Dispatch(MacroKey{Key: key}); handled || err != nil {
			t.Fatalf("%s on a list row: handled=%v err=%v", key, handled, err)
		}
	}
	if r.Mode.Name != "normal" {
		t.Fatalf("mode after unbound editor keys: %s", r.Mode.Name)
	}
	editorTestKeys(t, r, "2", "q")
	if got := r.Mode; got.Name != "macro_pending" {
		t.Fatalf("count and macro rows stay available on a row: %+v", got)
	}
	editorTestKeys(t, r, "esc")
	if !strings.Contains(strings.Join(func() (out []string) {
		for _, b := range r.ListBindings("one") {
			out = append(out, b.Keys)
		}
		return
	}(), " "), "q") {
		t.Fatal("q is an app row")
	}
	if err := r.SetFocus(input.ID); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(r.ListBindings(input.ID), func(b Binding) bool { return b.Noun == "ochazuke.editor" }) {
		t.Fatal("an input keeps the editor fallback")
	}
}

// A completion whose Then removes its own owner (a kill verb dropping its
// row) settles the job and drops the message instead of panicking.
func TestDeliverToleratesOwnerRemovedByItsCompletion(t *testing.T) {
	r := NewRuntime()
	r.Instance("row", Kind("row"))
	comp := &slowComponent{r: r, id: "row"}
	r.Instances["row"].Component = comp
	removed := false
	ticket, err := r.Launch("row", "kill", Pending(func(context.Context) (any, error) { return "killed", nil }).Then(func(c Ctx, _ any, _ error) {
		removed = c.Runtime.RemoveInstance("row") == nil
	}), Ctx{Runtime: r})
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range runCmd(r.PendingCommands()) {
		if handled, err := r.Deliver(msg); !handled || err != nil {
			t.Fatalf("deliver: %v %v", handled, err)
		}
	}
	if !removed || r.Instances["row"] != nil || len(comp.received) != 0 || comp.closed != 1 {
		t.Fatalf("removed=%v instance=%v received=%+v closed=%d", removed, r.Instances["row"], comp.received, comp.closed)
	}
	if jobs := r.Jobs(); len(jobs) != 1 || jobs[0].Job != ticket.Job || jobs[0].State != JobDone {
		t.Fatalf("job not settled: %+v", jobs)
	}
}

func TestDeliverCompletesSlowJobAfterHistoryFills(t *testing.T) {
	r := NewRuntime()
	r.Instance("loader", Kind("loader"))
	comp := &slowComponent{r: r, id: "loader"}
	r.Instances["loader"].Component = comp
	var loaded any
	_, err := r.Start("loader", func(context.Context) (any, error) {
		return "slow rows", nil
	}, func(_ Ctx, value any, _ error) {
		loaded = value
	}, Ctx{Runtime: r})
	if err != nil {
		t.Fatal(err)
	}
	slow := r.PendingCommands()
	for range 40 {
		if _, err := r.Start("loader", func(context.Context) (any, error) {
			return "fast rows", nil
		}, nil, Ctx{Runtime: r}); err != nil {
			t.Fatal(err)
		}
		for _, msg := range runCmd(r.PendingCommands()) {
			if handled, err := r.Deliver(msg); !handled || err != nil {
				t.Fatalf("fast delivery: %v %v", handled, err)
			}
		}
	}
	for _, msg := range runCmd(slow) {
		if handled, err := r.Deliver(msg); !handled || err != nil {
			t.Fatalf("slow delivery: %v %v", handled, err)
		}
	}
	if loaded != "slow rows" || len(comp.received) != 41 || comp.received[40].Value != "slow rows" {
		t.Fatalf("loaded=%v received=%+v", loaded, comp.received)
	}
}

// ReplacingAt(list, slot): previews asked from two rows of one list share
// the list's slot, so the second cancels the first; another list's slot is
// its own, the completion lands on the row that asked, and a scope off the
// owner's ancestry is refused.
func TestReplacingAtScopesTheSlotToAnAncestor(t *testing.T) {
	r := NewRuntime()
	r.Define("row")
	for _, list := range []string{"left", "right"} {
		if _, err := r.AddList(list, []ListItem{{ID: list + "1", Noun: "row"}, {ID: list + "2", Noun: "row"}}); err != nil {
			t.Fatal(err)
		}
	}
	landed := map[string]string{}
	preview := func(row, scope string, release chan struct{}) (Ticket, tea.Cmd) {
		ticket, err := r.Launch(row, "row.preview", Pending(func(ctx context.Context) (any, error) {
			select {
			case <-release:
				return row, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}).ReplacingAt(scope, "preview").Then(func(c Ctx, v any, err error) {
			if err == nil {
				landed[c.Instance.ID] = v.(string)
			}
		}), Ctx{Runtime: r, Instance: r.Instances[row]})
		if err != nil {
			t.Fatal(err)
		}
		return ticket, r.PendingCommands()
	}
	r1, r2, r3 := make(chan struct{}), make(chan struct{}), make(chan struct{})
	first, firstCmd := preview("left1", "left", r1)
	second, secondCmd := preview("left2", "left", r2)
	other, otherCmd := preview("right1", "right", r3)
	if r.Accepts(first) || !r.Accepts(second) || !r.Accepts(other) {
		t.Fatalf("scoped slot: first=%v second=%v other=%v", r.Accepts(first), r.Accepts(second), r.Accepts(other))
	}
	close(r1)
	close(r2)
	close(r3)
	for _, cmd := range []tea.Cmd{firstCmd, secondCmd, otherCmd} {
		for _, msg := range runCmd(cmd) {
			r.Deliver(msg)
		}
	}
	if !reflect.DeepEqual(landed, map[string]string{"left2": "left2", "right1": "right1"}) {
		t.Fatalf("landed: %v", landed)
	}
	jobs := r.Jobs()
	if len(jobs) != 3 || jobs[0].State != JobCancelled || jobs[0].Owner != "left1" || jobs[0].Scope != "left" || jobs[1].Scope != "left" || jobs[2].Scope != "right" {
		t.Fatalf("jobs: %+v", jobs)
	}
	// Per-owner stays the default: two rows' Replacing slots do not interact.
	a, _ := r.Launch("right1", "x", Pending(func(context.Context) (any, error) { return nil, nil }).Replacing("preview"), Ctx{})
	b, _ := r.Launch("right2", "x", Pending(func(context.Context) (any, error) { return nil, nil }).Replacing("preview"), Ctx{})
	if !r.Accepts(a) || !r.Accepts(b) {
		t.Fatal("per-owner slots must not replace each other")
	}
	if got, _ := JSONPointer(r.Snapshot(), "/jobs/0/scope"); got != "right1" {
		t.Fatalf("snapshot scope: %v", got)
	}
	if _, err := r.Launch("left1", "x", Pending(func(context.Context) (any, error) { return nil, nil }).ReplacingAt("right", "preview"), Ctx{}); err == nil {
		t.Fatal("a scope off the owner's ancestry must fail")
	}
	r.PendingCommands()
}
