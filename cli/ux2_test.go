package cli

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"ofam.dev/ochazuke"
)

// A person cancels a hung write from the jobs list. Later edits to that thing must still reach
// the store (or be refused out loud), and reopening must not show the cancelled value as saved.
func TestCancelledWriteReleasesThing(t *testing.T) {
	t.Parallel()
	var hung atomic.Bool
	hung.Store(true)
	r, m, thing := mountedEditFixture(t, "change", func(ctx context.Context) error {
		if hung.Load() {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	pump := newPump(t, r)
	open := func() { t.Helper(); pump.open() }
	press := func(keys ...string) {
		t.Helper()
		e := r.RecordEditor("edit-form")
		for _, key := range keys {
			if _, err := r.HandleBinding(key); err != nil {
				t.Fatal(err)
			}
			e.PendingCommand()
		}
	}
	open()
	press("A", "x", "esc", "esc")
	var job uint64
	for _, j := range r.Jobs() {
		if j.Verb == "ext-parcel.set" && j.Status == ochazuke.JobRunning {
			job = j.Job
		}
	}
	if job == 0 {
		t.Fatal("no running set job")
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.job", Verb: "cancel", Args: ochazuke.Arguments{"job": int(job)}}); err != nil {
		t.Fatal(err)
	}
	if notices := strings.Join(m.Notices, "\n"); !strings.Contains(notices, `title = "Originalx"`) || !strings.Contains(notices, "cancelled") {
		t.Fatalf("cancelled value lost: %s", notices)
	}
	hung.Store(false)
	open()
	if got := r.RecordEditor("edit-form").Values()["title"]; got != "Original" {
		t.Fatalf("reopen overlaid cancelled value: %q", got)
	}
	press("A", "y", "esc", "esc")
	pump.until("the writes to end", pump.idle)
	if title, _ := thing["title"].(string); title != "Originaly" {
		t.Errorf("edit after cancel never reached the store: store %q", title)
	}
}
