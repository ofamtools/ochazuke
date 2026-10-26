package ochazuke

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func lastEntry(t *testing.T, r *Runtime) HistoryEntry {
	t.Helper()
	entries := r.History(1)
	if len(entries) != 1 {
		t.Fatal("history is empty")
	}
	return entries[0]
}

// A key records its row's request with the focus, page, mode and the key as
// source; a direct call is "api"; a transport call is "cli".
func TestHistoryRecordsRequestsWithSource(t *testing.T) {
	r, _ := editorTestRuntime(t, "one two", false)
	editorTestKeys(t, r, "x")
	e := lastEntry(t, r)
	if e.Request.Key() != "input.delete" || e.Context.Source != "key:x" || e.Context.Focus != "editor" || e.Context.Mode != "normal" || !e.Result.OK {
		t.Fatalf("key entry: %+v", e)
	}
	// g enters g_pending; the second g is the row that answers there (the
	// pending mode is consumed before the request runs, so mode is normal).
	editorTestKeys(t, r, "g", "g")
	if e := lastEntry(t, r); e.Request.Key() != "input.move" || e.Context.Source != "key:g" {
		t.Fatalf("pending-mode source: %+v %+v", e.Request, e.Context)
	}
	if _, err := r.Execute(Request{Verb: "insert", Noun: "input", Args: Arguments{"text": "a"}}); err != nil {
		t.Fatal(err)
	}
	if e := lastEntry(t, r); e.Context.Source != "api" || e.Request.Args["text"] != "a" {
		t.Fatalf("api entry: %+v", e)
	}
	reply := make(chan Response, 1)
	r.HandleCall(CallMsg{Request: Request{Verb: "ignore", Noun: "key"}, Reply: reply})
	<-reply
	if e := lastEntry(t, r); e.Context.Source != "cli" || e.Request.Key() != "key.ignore" {
		t.Fatalf("cli entry: %+v", e)
	}
	if _, err := r.Execute(Request{Verb: "nope", Noun: "thing"}); err == nil {
		t.Fatal("unknown operation must fail")
	}
	if e := lastEntry(t, r); e.Result.OK || e.Result.Error == "" || e.Seq != r.History(0)[len(r.History(0))-1].Seq {
		t.Fatalf("error entry: %+v", e)
	}
}

// Keys a macro replays record as macro:<register>.
func TestHistoryMacroReplaySource(t *testing.T) {
	r, in := editorTestRuntime(t, "abcdef", false)
	editorTestKeys(t, r, "q", "a", "x", "q", "@", "a")
	if string(in.Text) != "cdef" {
		t.Fatalf("text %q", in.Text)
	}
	found := false
	for _, e := range r.History(0) {
		found = found || e.Request.Key() == "input.delete" && e.Context.Source == "macro:a"
	}
	if !found {
		t.Fatalf("no macro-sourced delete in %+v", r.History(10))
	}
}

// The ring keeps the last HistoryLimit entries; history.list and state.get
// are fetches, recorded apart and left out of history.list's default view
// and the snapshot, which shows the last 50 actions and changes at /history.
func TestHistoryRingListAndSnapshot(t *testing.T) {
	r := NewRuntime()
	for range HistoryLimit + 10 {
		if _, err := r.Execute(Request{Verb: "ignore", Noun: "key"}); err != nil {
			t.Fatal(err)
		}
	}
	all := r.History(0)
	if len(all) != HistoryLimit || all[0].Seq != 11 || all[len(all)-1].Seq != HistoryLimit+10 {
		t.Fatalf("ring: %d entries, first %d", len(all), all[0].Seq)
	}
	listed, err := r.Execute(Request{Verb: "list", Noun: "history", Args: Arguments{"last": 3}})
	if err != nil || len(listed.([]HistoryEntry)) != 3 {
		t.Fatalf("history.list: %v %v", listed, err)
	}
	state, err := r.Execute(Request{Verb: "get", Noun: "state", Args: Arguments{"path": "/history"}})
	if err != nil || len(state.([]any)) != 50 {
		t.Fatalf("/history: %v", err)
	}
	if e := r.HistoryOf(1, historyUserKinds...); len(e) != 1 || e[0].Seq != HistoryLimit+10 {
		t.Fatalf("history.list or state.get is in the user's view: %+v", e)
	}
	if e := lastEntry(t, r); e.Kind != HistoryFetch || e.Request.Key() != "state.get" {
		t.Fatalf("state.get is not the last fetch: %+v", e)
	}
	data, err := json.Marshal(lastEntry(t, r))
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]any
	if err := json.Unmarshal(data, &shape); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"seq", "at", "kind", "request", "context", "result"} {
		if _, ok := shape[key]; !ok {
			t.Fatalf("entry lacks %s: %s", key, data)
		}
	}
}

// An application verb that launches work records its job id.
func TestHistoryRecordsJob(t *testing.T) {
	r := NewRuntime()
	r.Instance("box")
	r.Define("box").Verb("sync", func(c Ctx) Result {
		return Pending(func(context.Context) (any, error) { return nil, nil })
	})
	if _, err := r.Execute(Request{Verb: "sync", Noun: "box", Target: "box"}); err != nil {
		t.Fatal(err)
	}
	if e := lastEntry(t, r); e.Job == 0 || e.Context.Focus != "app" {
		t.Fatalf("job entry: %+v", e)
	}
}

// d w then . on another line deletes a word there: the request is re-run
// at the current focus and cursor with its arguments kept.
func TestDotRepeatsChangeOnAnotherLine(t *testing.T) {
	r, in := editorTestRuntime(t, "one two\nthree four", true)
	editorTestKeys(t, r, "d", "w", "j", "0", ".")
	if got := string(in.Text); got != "two\nfour" {
		t.Fatalf("text %q", got)
	}
	if e := lastEntry(t, r); e.Request.Key() != "history.repeat" || e.Context.Source != "key:." {
		t.Fatalf("repeat entry: %+v", e)
	}
}

// A count on . replaces the recorded count.
func TestDotCountReplacesRecordedCount(t *testing.T) {
	r, in := editorTestRuntime(t, "a b c d e f", false)
	editorTestKeys(t, r, "d", "w", "3", ".")
	if got := string(in.Text); got != "e f" {
		t.Fatalf("3. text %q", got)
	}
	editorTestKeys(t, r, ".")
	if got := string(in.Text); got != "" {
		t.Fatalf("the repeated count is what . repeats next: %q", got)
	}
}

// A motion between the change and . is not what . repeats.
func TestDotDoesNotRepeatMotion(t *testing.T) {
	r, in := editorTestRuntime(t, "a b c d", false)
	editorTestKeys(t, r, "d", "w", "w", ".")
	if got := string(in.Text); got != "b d" {
		t.Fatalf("text %q", got)
	}
	if op := r.Operations["input.move"]; op.Repeatable || !r.Operations["input.operator"].Repeatable {
		t.Fatal("motions are not repeatable, operators are")
	}
	fresh := NewRuntime()
	if _, err := fresh.Execute(Request{Verb: "repeat", Noun: "history"}); err == nil {
		t.Fatal("nothing to repeat must fail")
	}
}

// The recorded target is dropped unless the request came from the CLI and
// the instance still exists.
func TestDotRetargetsToFocusUnlessCLI(t *testing.T) {
	r, focused := editorTestRuntime(t, "xxxx", false)
	other := NewInput("other", "yyyy", false)
	if err := r.AddInput(other); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(Request{Verb: "delete", Noun: "input", Target: "other"}); err != nil {
		t.Fatal(err)
	}
	editorTestKeys(t, r, ".")
	if string(focused.Text) != "xxx" || string(other.Text) != "yyy" {
		t.Fatalf("api target re-resolves to focus: %q %q", focused.Text, other.Text)
	}
	reply := make(chan Response, 1)
	r.HandleCall(CallMsg{Request: Request{Verb: "delete", Noun: "input", Target: "other"}, Reply: reply})
	<-reply
	editorTestKeys(t, r, ".")
	if string(focused.Text) != "xxx" || string(other.Text) != "y" {
		t.Fatalf("cli target is kept: %q %q", focused.Text, other.Text)
	}
}

// A confirmed change repeats without asking again: Confirmed is recorded.
func TestDotDoesNotReaskConfirmation(t *testing.T) {
	r := NewRuntime()
	runs := 0
	r.Register(Operation{Verb: "wipe", Noun: "test", Confirm: "Wipe?", Repeatable: true, Run: func(*Runtime, Context, Arguments) (any, error) { runs++; return nil, nil }})
	if _, err := r.Execute(Request{Verb: "wipe", Noun: "test"}); err != nil || runs != 0 || len(r.Modals()) != 1 {
		t.Fatalf("confirm opens: %v runs=%d", err, runs)
	}
	if _, err := r.Execute(Request{Verb: "accept", Noun: "confirm"}); err != nil || runs != 1 {
		t.Fatalf("accept: %v runs=%d", err, runs)
	}
	if _, err := r.Execute(Request{Verb: "repeat", Noun: "history"}); err != nil || runs != 2 || len(r.Modals()) != 0 {
		t.Fatalf("repeat: %v runs=%d modals=%v", err, runs, r.Modals())
	}
}

// countedData counts how often it is encoded: a row's Data in the motion
// test below, so an entry that encoded the list's snapshot would show.
type countedData struct{ n *int }

func (d countedData) MarshalJSON() ([]byte, error) {
	*d.n++
	return []byte("1"), nil
}

// A motion over a large list is an entry with the request and the move
// (row A to row B), never the list: nothing of its rows is encoded, and
// the entry is the same size at 10 rows and 10,000.
func TestHistoryMotionRecordsTheMoveNotTheList(t *testing.T) {
	r := NewRuntime()
	r.Define("a")
	if err := r.RegisterPage(&Page{ID: "page", Title: "page", Render: func(*Runtime, int, int) Block { return Text("") }}); err != nil {
		t.Fatal(err)
	}
	if err := r.OpenPage("page"); err != nil {
		t.Fatal(err)
	}
	encoded := 0
	items := make([]ListItem, 10000)
	for i := range items {
		items[i] = ListItem{ID: fmt.Sprintf("r%d", i), Label: "row", Noun: "a", Data: countedData{&encoded}}
	}
	list, err := r.AddList("l", items, Parent("page"))
	if err != nil {
		t.Fatal(err)
	}
	if err := list.Focus(); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := r.Execute(Request{Verb: "move", Noun: "list", Args: Arguments{"delta": 1}}); err != nil {
			t.Fatal(err)
		}
	}
	if encoded != 0 {
		t.Fatalf("history encoded the rows %d times", encoded)
	}
	e := lastEntry(t, r)
	if e.Request.Key() != "list.move" || !e.Result.OK || e.Moved == nil || e.Moved.From.Focus != "r2" || e.Moved.To.Focus != "r3" || e.Moved.To.Page != "page" {
		t.Fatalf("move entry: %+v moved %+v", e, e.Moved)
	}
	line, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if len(line) > 512 {
		t.Fatalf("entry is %d bytes: %s", len(line), line)
	}
	// A request that moves nothing has no Moved; a non-motion lands too.
	if _, err := r.Execute(Request{Verb: "mark", Noun: "list"}); err != nil {
		t.Fatal(err)
	}
	out, err := r.Execute(Request{Verb: "list", Noun: "history", Args: Arguments{"last": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.([]HistoryEntry); len(got) != 1 || got[0].Request.Key() != "list.mark" || !got[0].Result.OK || got[0].Moved != nil {
		t.Fatalf("history.list: %+v", got)
	}
	if encoded != 0 {
		t.Fatalf("history encoded the rows %d times", encoded)
	}
}
