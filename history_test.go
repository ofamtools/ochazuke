package ochazuke

import (
	"context"
	"encoding/json"
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
	if e.Request.Key() != "input.delete" || e.Context.Source != "key:x" || e.Context.Focus != "editor" || e.Context.Mode != "normal" || !e.Result.OK || len(e.Result.Value) == 0 {
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

// history.list and state.get are not recorded; the ring keeps the last
// HistoryLimit entries; the snapshot shows the last 50 at /history.
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
	if e := lastEntry(t, r); e.Seq != HistoryLimit+10 {
		t.Fatalf("history.list or state.get was recorded: %+v", e.Request)
	}
	data, err := json.Marshal(lastEntry(t, r))
	if err != nil {
		t.Fatal(err)
	}
	var shape map[string]any
	if err := json.Unmarshal(data, &shape); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"seq", "at", "request", "context", "result"} {
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
