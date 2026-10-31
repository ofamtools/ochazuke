package ochazuke

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

// taskFields is the schema the tests edit: a required title, a status
// chosen from three, a whole-number estimate and multiline notes.
func taskFields() []RecordField {
	return []RecordField{
		{Name: "title", Parameter: Parameter{Type: "string", Required: true}},
		{Name: "status", Parameter: Parameter{Type: "string", Required: true, Enum: []string{"todo", "doing", "done"}}},
		{Name: "estimate", Parameter: Parameter{Type: "integer"}},
		{Name: "notes", Multiline: true},
	}
}

// recordFixture is a page with a list and a placed record editor beside
// it, the editor focused; writes are logged by their String.
func recordFixture(t *testing.T, save func(RecordWrite) Result) (*Runtime, *Model, *RecordEditor, *[]string) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 80, 24
	var ed *RecordEditor
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block {
		list := r.List("tasks").View(w, 2)
		if ed == nil || ed.Closed() {
			return Text(list)
		}
		b := ed.Block(w, h-3)
		return b.Offset(0, 3).WithText(list + "\n\n" + b.Text)
	}); err != nil {
		t.Fatal(err)
	}
	r.Define("task")
	if _, err := r.AddList("tasks", []ListItem{{ID: "t1", Label: "one", Noun: "task"}, {ID: "t2", Label: "two", Noun: "task"}}, Parent("main")); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	var writes []string
	if save == nil {
		save = func(RecordWrite) Result { return Done(nil) }
	}
	spec := RecordSpec{Fields: taskFields(), Debounce: time.Millisecond, Save: func(w RecordWrite) Result {
		res := save(w)
		if res.err == nil && res.work == nil {
			writes = append(writes, w.String())
		}
		return res
	}, Load: func() (Record, error) {
		return Record{"title": "buy milk", "status": "todo"}, nil
	}}
	var err error
	ed, err = r.AddRecordEditor("ed", spec, Parent("main"), FocusOnClose("t1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ed.Focus(); err != nil {
		t.Fatal(err)
	}
	m.View()
	return r, m, ed, &writes
}

// settle runs a command and feeds every message it produces back to the
// model until nothing is left (ticks are 1ms in these fixtures).
func settle(m *Model, cmd tea.Cmd) {
	for range 20 {
		if cmd == nil {
			return
		}
		msg := cmd()
		var next []tea.Cmd
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c == nil {
					continue
				}
				_, out := m.Update(c())
				next = append(next, out)
			}
		} else {
			_, out := m.Update(msg)
			next = append(next, out)
		}
		cmd = tea.Batch(next...)
	}
}

func screen(m *Model) string { return ansi.Strip(m.View().Content) }

// Typing is written once after the pause, not per key, and leaving insert
// writes what the pause has not.
func TestRecordTypingWritesAfterThePause(t *testing.T) {
	r, m, ed, writes := recordFixture(t, nil)
	keytest.Press(m, "A")
	var last tea.Cmd
	for _, k := range []string{" ", "n", "o", "w"} {
		_, last = m.Update(keytest.Msg(k, true, false))
	}
	if len(*writes) != 0 {
		t.Fatalf("written before the pause: %v", *writes)
	}
	settle(m, last)
	if want := []string{`write title="buy milk now"`}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	m.Update(keytest.Msg("!", true, false))
	keytest.Press(m, "esc")
	if want := `write title="buy milk now!"`; len(*writes) != 2 || (*writes)[1] != want {
		t.Fatalf("writes %v, want %s last", *writes, want)
	}
	if r.Mode.Name != "normal" || r.Focus != "ed/title" || ed.Written()["title"] != "buy milk now!" {
		t.Fatalf("mode %s focus %s written %v", r.Mode.Name, r.Focus, ed.Written())
	}
}

// An invalid value is never written; its label says why, and fixing it
// writes it.
func TestRecordInvalidFieldIsNotWritten(t *testing.T) {
	r, m, ed, writes := recordFixture(t, nil)
	keytest.Press(m, "j")
	keytest.Press(m, "j")
	if r.Focus != "ed/estimate" {
		t.Fatalf("focus %s, want ed/estimate", r.Focus)
	}
	keytest.Press(m, "i")
	m.Update(keytest.Msg("x", true, false))
	keytest.Press(m, "esc")
	if len(*writes) != 0 || ed.Problem("estimate") != "a whole number" {
		t.Fatalf("writes %v problem %q", *writes, ed.Problem("estimate"))
	}
	if s := screen(m); !strings.Contains(s, "estimate · not written: a whole number") {
		t.Fatalf("label lacks why:\n%s", s)
	}
	keytest.Press(m, "k")
	keytest.Press(m, "k")
	keytest.Press(m, "A")
	keytest.Press(m, "ctrl+u")
	keytest.Press(m, "esc")
	if !strings.Contains(screen(m), "title · not written: required") || len(*writes) != 0 {
		t.Fatalf("an empty required title was written: %v\n%s", *writes, screen(m))
	}
	_ = ed
}

// A choice field is edited through the chooser: letters do not type into
// it, enter opens the choices, and a pick is written at once.
func TestRecordChoiceFieldUsesTheChooser(t *testing.T) {
	r, m, ed, writes := recordFixture(t, nil)
	keytest.Press(m, "j")
	if r.Focus != "ed/status" || r.Input(r.Focus) != nil {
		t.Fatalf("focus %s, want the status choice", r.Focus)
	}
	keytest.Press(m, "x")
	if ed.Values()["status"] != "todo" {
		t.Fatalf("a letter changed the choice: %v", ed.Values())
	}
	keytest.Press(m, "enter")
	if r.TopModal() != formChooseID {
		t.Fatalf("top modal %q, want the chooser", r.TopModal())
	}
	if s := screen(m); !strings.Contains(s, "Choose status") || !strings.Contains(s, "doing") {
		t.Fatalf("chooser not drawn:\n%s", s)
	}
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	if r.TopModal() != "" || r.Focus != "ed/status" {
		t.Fatalf("modal %q focus %s after the pick", r.TopModal(), r.Focus)
	}
	if want := []string{"write status=doing"}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	// esc leaves the chooser without a write.
	keytest.Press(m, "enter")
	keytest.Press(m, "esc")
	if r.TopModal() != "" || len(*writes) != 1 {
		t.Fatalf("modal %q writes %v", r.TopModal(), *writes)
	}
}

// u undoes the record's last written change (in any field) and ctrl+r
// redoes it; each is written.
func TestRecordUndoRedoAreWritten(t *testing.T) {
	r, m, _, writes := recordFixture(t, nil)
	keytest.Press(m, "A")
	m.Update(keytest.Msg("!", true, false))
	keytest.Press(m, "esc")
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	keytest.Press(m, "j")
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	keytest.Press(m, "u")
	keytest.Press(m, "u")
	if r.Focus != "ed/title" {
		t.Fatalf("focus %s after undoing the title", r.Focus)
	}
	keytest.Press(m, "ctrl+r")
	want := []string{`write title="buy milk!"`, "write status=done", "write status=todo", `write title="buy milk"`, `write title="buy milk!"`}
	if !slices.Equal(*writes, want) {
		t.Fatalf("writes\n%q\nwant\n%q", *writes, want)
	}
}

// Fields are stops: tab jumps between them, tab while typing moves on and
// keeps typing, and q leaves to the row the editor names.
func TestRecordFieldsAreStopsAndQLeaves(t *testing.T) {
	r, m, ed, writes := recordFixture(t, nil)
	keytest.Press(m, "tab")
	if r.Focus != "ed/status" {
		t.Fatalf("tab: focus %s", r.Focus)
	}
	keytest.Press(m, "tab")
	keytest.Press(m, "i")
	m.Update(keytest.Msg("3", true, false))
	keytest.Press(m, "tab")
	if r.Focus != "ed/notes" || r.Mode.Name != "insert" {
		t.Fatalf("focus %s mode %s after tab in insert", r.Focus, r.Mode.Name)
	}
	if want := []string{"write estimate=3"}; !slices.Equal(*writes, want) {
		t.Fatalf("leaving the field did not write: %v", *writes)
	}
	m.Update(keytest.Msg("hi", true, true))
	keytest.Press(m, "esc")
	keytest.Press(m, "q")
	if !ed.Closed() || r.Focus != "t1" || r.Instances["ed"] != nil {
		t.Fatalf("closed %v focus %s", ed.Closed(), r.Focus)
	}
	if want := "write notes=hi"; (*writes)[len(*writes)-1] != want {
		t.Fatalf("writes %v, want %s last", *writes, want)
	}
}

// A pending save runs off the loop, one at a time: a change made while one
// is in flight goes in the next write when the first lands.
func TestRecordPendingWritesAreSerialized(t *testing.T) {
	var sent []string
	release := make(chan struct{}, 4)
	_, m, ed, _ := recordFixture(t, func(w RecordWrite) Result {
		sent = append(sent, w.String())
		return Pending(func(context.Context) (any, error) { <-release; return nil, nil })
	})
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	keytest.Press(m, "j")
	_, first := m.Update(keytest.Msg("enter", false, false))
	keytest.Press(m, "enter")
	keytest.Press(m, "k")
	keytest.Press(m, "enter")
	if want := []string{"write status=doing"}; !slices.Equal(sent, want) {
		t.Fatalf("sent %v while one write is in flight, want %v", sent, want)
	}
	if ed.Written()["status"] != "todo" {
		t.Fatalf("written %v", ed.Written())
	}
	release <- struct{}{}
	release <- struct{}{}
	settle(m, first)
	if want := []string{"write status=doing", "write status=todo"}; !slices.Equal(sent, want) {
		t.Fatalf("sent %v, want %v", sent, want)
	}
}

// A failed write keeps the value, says so and is retried at the next
// write (here leaving the field).
func TestRecordFailedWriteRetries(t *testing.T) {
	fail, calls := 1, 0
	var stored []string
	_, m, ed, _ := recordFixture(t, func(w RecordWrite) Result {
		calls++
		if fail > 0 {
			fail--
			return Failed(errors.New("store unavailable"))
		}
		stored = append(stored, w.String())
		return Done(nil)
	})
	keytest.Press(m, "A")
	m.Update(keytest.Msg("!", true, false))
	keytest.Press(m, "esc")
	if ed.WriteError() == "" || !strings.Contains(screen(m), "! not saved: store unavailable") {
		t.Fatalf("no failure shown:\n%s", screen(m))
	}
	m.Update(nil)
	m.Update(nil)
	if len(stored) != 0 || calls != 1 {
		t.Fatalf("stored %v after %d calls before the next write", stored, calls)
	}
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	if want := []string{`write title="buy milk!"`, "write status=doing"}; !slices.Equal(stored, want) {
		t.Fatalf("stored %v, want %v", stored, want)
	}
	if strings.Contains(screen(m), "not saved") {
		t.Fatal("the failure stayed after a good write")
	}
}

// A modal editor opens over the focus and q gives it back, in its mode.
func TestRecordModalEditorRestoresTheFocus(t *testing.T) {
	r, m, ed, _ := recordFixture(t, nil)
	keytest.Press(m, "q")
	if !ed.Closed() || r.Focus != "t1" {
		t.Fatalf("focus %s", r.Focus)
	}
	var writes []string
	modal, err := r.OpenRecordEditor("edit-task", RecordSpec{Title: "Edit task", Fields: taskFields(), Insert: true, Save: func(w RecordWrite) Result {
		writes = append(writes, w.String())
		return Done(nil)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if r.TopModal() != "edit-task" || r.Focus != "edit-task/title" || r.Mode.Name != "insert" {
		t.Fatalf("modal %q focus %s mode %s", r.TopModal(), r.Focus, r.Mode.Name)
	}
	m.Update(keytest.Msg("x", true, false))
	if s := screen(m); !strings.Contains(s, "Edit task") || !strings.Contains(s, "title") {
		t.Fatalf("modal editor not drawn:\n%s", s)
	}
	keytest.Press(m, "esc")
	keytest.Press(m, "esc")
	if !modal.Closed() || r.TopModal() != "" || r.Focus != "t1" || r.Mode.Name != "normal" {
		t.Fatalf("closed %v modal %q focus %s", modal.Closed(), r.TopModal(), r.Focus)
	}
	if want := []string{"write title=x"}; !slices.Equal(writes, want) {
		t.Fatalf("writes %v, want %v", writes, want)
	}
}

// A verb's parameters are a form's schema, in CLI order.
func TestRecordFieldsFromAnOperation(t *testing.T) {
	op := Operation{Verb: "edit", Noun: "torrent", Positional: []string{"name"}, Parameters: map[string]Parameter{
		"name":  {Type: "string", Required: true},
		"state": {Type: "string", Enum: []string{"paused", "running"}},
		"limit": {Type: "integer"},
	}}
	var names []string
	for _, f := range RecordFields(op) {
		names = append(names, f.Name)
	}
	if want := []string{"name", "limit", "state"}; !slices.Equal(names, want) {
		t.Fatalf("fields %v, want %v", names, want)
	}
}

// The form's rows are atomic and its verbs show in the help.
func TestRecordBindingsValidate(t *testing.T) {
	r, _, _, _ := recordFixture(t, nil)
	if warnings, err := r.ValidateBindings(); err != nil || len(warnings) > 0 {
		t.Fatal(warnings, err)
	}
	ops := map[string]bool{}
	for _, row := range BindingHelp(r, "ed/status") {
		ops[row.Operation] = true
	}
	for _, op := range []string{"form.leave", "form.undo", "form.choose", "form.next"} {
		if !ops[op] {
			t.Fatalf("help lacks %s: %v", op, ops)
		}
	}
}

// An edit in normal mode (x) is written at once, as its own undo step.
func TestRecordNormalModeEditWritesAtOnce(t *testing.T) {
	_, m, _, writes := recordFixture(t, nil)
	keytest.Press(m, "0")
	keytest.Press(m, "x")
	if want := []string{`write title="uy milk"`}; !slices.Equal(*writes, want) {
		t.Fatalf("writes %v, want %v", *writes, want)
	}
	keytest.Press(m, "u")
	if want := `write title="buy milk"`; len(*writes) != 2 || (*writes)[1] != want {
		t.Fatalf("writes %v, want %s last", *writes, want)
	}
}

// suggestFixture is a placed editor over a free model field with
// suggestions (from source, else three enum values) and a title.
func suggestFixture(t *testing.T, source string) (*Runtime, *Model, *RecordEditor, *[]string) {
	t.Helper()
	r := NewRuntime()
	m := NewModel(r)
	m.Width, m.Height = 80, 24
	var ed *RecordEditor
	if _, err := r.AddPage("main", "Main", func(r *Runtime, w, h int) Block { return ed.Block(w, h) }); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("main"); err != nil {
		t.Fatal(err)
	}
	model := Parameter{Type: "string", Free: true, Enum: []string{"opus", "sonnet", "haiku"}}
	if source != "" {
		model = Parameter{Type: "string", Free: true, Source: source}
	}
	var writes []string
	spec := RecordSpec{
		Fields: []RecordField{{Name: "model", Parameter: model}, {Name: "title"}},
		Load:   func() (Record, error) { return Record{"model": "opus"}, nil },
		Save:   func(w RecordWrite) Result { writes = append(writes, w.String()); return Done(nil) },
	}
	var err error
	if ed, err = r.AddRecordEditor("ed", spec, Parent("main")); err != nil {
		t.Fatal(err)
	}
	if err := ed.Focus(); err != nil {
		t.Fatal(err)
	}
	m.View()
	return r, m, ed, &writes
}

// A free field with an Enum is a text field: typing stays free and a value
// outside the suggestions is written as typed.
func TestRecordFreeFieldWithSuggestionsTypesFreely(t *testing.T) {
	r, m, ed, writes := suggestFixture(t, "")
	if inst := r.Instances["ed/model"]; inst.Noun != FormSuggestNoun || r.Input("ed/model") == nil {
		t.Fatalf("the model field is %+v, want a form-suggest input", inst)
	}
	ops := map[string]bool{}
	for _, row := range BindingHelp(r, "ed/model") {
		ops[row.Key+" "+row.Operation] = true
	}
	if !ops["enter form.suggest"] || !ops["ctrl+space form.suggest"] {
		t.Fatalf("help lacks the suggest keys: %v", ops)
	}
	if s := screen(m); !strings.Contains(s, "enter form.suggest") {
		t.Fatalf("the footer hints the suggest key:\n%s", s)
	}
	keytest.Press(m, "A")
	keytest.Press(m, "ctrl+u")
	keytest.Type(m, "gpt-x")
	keytest.Press(m, "esc")
	if want := []string{"write model=gpt-x"}; !slices.Equal(*writes, want) || ed.Problem("model") != "" {
		t.Fatalf("writes %v problem %q, want %v", *writes, ed.Problem("model"), want)
	}
}

// enter in normal mode opens the framework chooser over the Enum; a pick
// fills the field, is written at once and the focus is back on the field.
func TestRecordSuggestionsPickFillsTheField(t *testing.T) {
	r, m, ed, writes := suggestFixture(t, "")
	keytest.Press(m, "enter")
	c := r.Chooser(FormSuggestionsKind)
	if c == nil || r.TopModal() != FormSuggestionsKind {
		t.Fatalf("top modal %q, want the suggestions chooser", r.TopModal())
	}
	if s := screen(m); !strings.Contains(s, "Suggest model") || !strings.Contains(s, "sonnet") || !strings.Contains(s, "haiku") {
		t.Fatalf("chooser not drawn:\n%s", s)
	}
	keytest.Type(m, "son")
	keytest.Press(m, "enter")
	if r.TopModal() != "" || r.Focus != "ed/model" || r.Mode.Name != "normal" {
		t.Fatalf("modal %q focus %s mode %s after the pick", r.TopModal(), r.Focus, r.Mode.Name)
	}
	if ed.Values()["model"] != "sonnet" || !slices.Equal(*writes, []string{"write model=sonnet"}) {
		t.Fatalf("values %v writes %v", ed.Values(), *writes)
	}
	// A query nothing matches is taken as typed; ctrl+space opens from insert and insert comes back.
	keytest.Press(m, "A")
	keytest.Press(m, "ctrl+space")
	if r.TopModal() != FormSuggestionsKind {
		t.Fatalf("ctrl+space in insert: top modal %q", r.TopModal())
	}
	keytest.Type(m, "zz-local")
	keytest.Press(m, "enter")
	if r.Focus != "ed/model" || r.Mode.Name != "insert" || ed.Values()["model"] != "zz-local" {
		t.Fatalf("focus %s mode %s values %v", r.Focus, r.Mode.Name, ed.Values())
	}
	if want := "write model=zz-local"; len(*writes) != 2 || (*writes)[1] != want {
		t.Fatalf("writes %v, want %s last", *writes, want)
	}
	// u undoes the pick like any written change.
	keytest.Press(m, "esc")
	keytest.Press(m, "u")
	if ed.Values()["model"] != "sonnet" {
		t.Fatalf("undo after a pick: %v", ed.Values())
	}
}

// A Source verb that returns Pending loads off the loop as a job of the
// chooser: loading… until it lands, then its rows; esc cancels a load.
func TestRecordSuggestionsLoadOffTheLoop(t *testing.T) {
	release := make(chan struct{}, 2)
	calls := 0
	r, m, ed, writes := suggestFixture(t, "harness.models")
	r.Define("harness").Verb("models", func(Ctx) Result {
		calls++
		return Pending(func(ctx context.Context) (any, error) {
			select {
			case <-release:
				return []string{"opus", "gpt-5"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
	})
	cmd := keytest.Press(m, "enter")
	if calls != 1 || r.TopModal() != FormSuggestionsKind {
		t.Fatalf("calls %d top %q", calls, r.TopModal())
	}
	if s := screen(m); !strings.Contains(s, "loading…") {
		t.Fatalf("before the load lands the chooser says loading:\n%s", s)
	}
	release <- struct{}{}
	for _, msg := range runCmd(cmd) {
		if _, ok := msg.(Completion); ok {
			m.Update(msg)
		}
	}
	// The completion lands inside the frame budget, so its rows wait for the
	// frame tick Update returned, which this test does not run; take an
	// explicit snapshot (TestRenderBackgroundCompletionShowsInNextFrame
	// covers the tick).
	m.InvalidateRender()
	if s := screen(m); !strings.Contains(s, "gpt-5") {
		t.Fatalf("the source's rows did not land:\n%s", s)
	}
	keytest.Type(m, "gpt")
	keytest.Press(m, "enter")
	if ed.Values()["model"] != "gpt-5" || !slices.Equal(*writes, []string{"write model=gpt-5"}) {
		t.Fatalf("values %v writes %v", ed.Values(), *writes)
	}
	// esc closes the chooser and cancels a load in flight.
	keytest.Press(m, "enter")
	keytest.Press(m, "esc")
	cancelled := false
	for _, j := range r.Jobs() {
		if j.Verb == "harness.models" && j.State == JobCancelled {
			cancelled = true
		}
	}
	if !cancelled || r.TopModal() != "" {
		t.Fatalf("jobs %+v top %q, want the load cancelled", r.Jobs(), r.TopModal())
	}
}

// Reload takes a value written outside the editor into a field that holds
// no unsaved typing, keeps the typing of the field being typed in (written
// over the new value as the later change), keeps the focus and the mode,
// and drops the undo steps of the fields it changed.
func TestRecordReloadKeepsTypingAndFocus(t *testing.T) {
	r, m, ed, writes := recordFixture(t, nil)
	stored := Record{"title": "buy milk", "status": "todo"}
	ed.spec.Load = func() (Record, error) { return maps.Clone(stored), nil }
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	keytest.Press(m, "j")
	keytest.Press(m, "enter") // status doing, written
	keytest.Press(m, "k")
	keytest.Press(m, "A")
	keytest.Type(m, " now") // unsaved typing in the title
	stored["title"], stored["status"] = "buy bread", "done"
	if err := ed.Reload(); err != nil {
		t.Fatal(err)
	}
	if r.Focus != "ed/title" || r.Mode.Name != "insert" || ed.Closed() {
		t.Fatalf("focus %s mode %s closed %v", r.Focus, r.Mode.Name, ed.Closed())
	}
	if v := ed.Values(); v["title"] != "buy milk now" || v["status"] != "done" {
		t.Fatalf("values %v, want the typing kept and the status reloaded", v)
	}
	if len(ed.undo) != 0 {
		t.Fatalf("undo %v, want the reloaded fields' steps dropped", ed.undo)
	}
	keytest.Press(m, "esc")
	if got := (*writes)[len(*writes)-1]; got != "write title=\"buy milk now\"" {
		t.Fatalf("last write %q", got)
	}
	if w := ed.Written(); w["status"] != "done" {
		t.Fatalf("written %v, want the reloaded status kept", w)
	}
}

// A field whose write is in flight keeps its value through a reload: the
// store has not taken the write yet.
func TestRecordReloadKeepsTheWriteInFlight(t *testing.T) {
	_, m, ed, _ := recordFixture(t, func(RecordWrite) Result {
		return Pending(func(context.Context) (any, error) { return nil, nil })
	})
	ed.spec.Load = func() (Record, error) { return Record{"title": "buy milk", "status": "todo"}, nil }
	keytest.Press(m, "j")
	keytest.Press(m, "enter")
	keytest.Press(m, "j")
	keytest.Press(m, "enter") // status doing, in flight
	if err := ed.Reload(); err != nil {
		t.Fatal(err)
	}
	if v := ed.Values()["status"]; v != "doing" {
		t.Fatalf("status %q, want the write in flight kept", v)
	}
}

// A closed editor's id is free again: Close (the delivery contract's,
// which leaves the instance) does not stop AddRecordEditor, and Leave
// removes the instance and gives the focus back.
func TestRecordClosedEditorIDIsFree(t *testing.T) {
	r, _, ed, _ := recordFixture(t, nil)
	ed.Close()
	again, err := r.AddRecordEditor("ed", RecordSpec{Fields: taskFields()}, Parent("main"), FocusOnClose("t1"))
	if err != nil {
		t.Fatalf("reopen after Close: %v", err)
	}
	if err := again.Focus(); err != nil {
		t.Fatal(err)
	}
	if err := again.Leave(); err != nil {
		t.Fatal(err)
	}
	if r.Instances["ed"] != nil || r.Focus != "t1" {
		t.Fatalf("after Leave: instance %v focus %s", r.Instances["ed"] != nil, r.Focus)
	}
}
