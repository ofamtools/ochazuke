package recorder

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fakeClock(start time.Time, step time.Duration) func() time.Time {
	cur := start
	first := true
	return func() time.Time {
		if first {
			first = false
			return cur
		}
		cur = cur.Add(step)
		return cur
	}
}

func TestRingEvictionByMaxEvents(t *testing.T) {
	t0 := time.Date(2026, 11, 23, 20, 0, 0, 0, time.UTC)
	r := New(Options{
		App:       "demo",
		Version:   "1.0.0",
		MaxEvents: 3,
		MaxBytes:  DefaultMaxBytes,
		Enabled:   true,
		Now:       fakeClock(t0, 10*time.Millisecond),
	})

	for i := 0; i < 7; i++ {
		r.Record(Event{Kind: KindKey, Key: fmt.Sprintf("k%d", i)})
	}

	hdr, events := r.Snapshot()
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}
	if hdr.TotalEvents != 7 || hdr.DroppedEvents != 4 || hdr.BufferedEvents != 3 || !hdr.Truncated {
		t.Fatalf("unexpected header counts: %+v", hdr)
	}
	for i, wantSeq := range []int{5, 6, 7} {
		if events[i].Seq != wantSeq {
			t.Errorf("events[%d].Seq = %d, want %d", i, events[i].Seq, wantSeq)
		}
		if events[i].Type != "event" {
			t.Errorf("events[%d].Type = %q, want event", i, events[i].Type)
		}
	}
	if events[0].Key != "k4" || events[1].Key != "k5" || events[2].Key != "k6" {
		t.Errorf("unexpected keys after ring wrap: %q %q %q", events[0].Key, events[1].Key, events[2].Key)
	}
	if events[0].OffsetMS != 50 || events[2].OffsetMS != 70 {
		t.Errorf("unexpected offsets: %d, %d", events[0].OffsetMS, events[2].OffsetMS)
	}
}

func TestRingEvictionByMaxBytes(t *testing.T) {
	payload := strings.Repeat("x", 100)
	// Each event is ~200 bytes (96 base + len("paste") + 100 = 201).
	r := New(Options{
		MaxEvents: 100,
		MaxBytes:  450,
		Enabled:   true,
	})

	for i := 0; i < 6; i++ {
		r.Record(Event{Kind: KindPaste, Text: payload})
	}

	st := r.Stats()
	if st.Total != 6 {
		t.Fatalf("Total = %d, want 6", st.Total)
	}
	if st.Buffered != 2 {
		t.Fatalf("Buffered = %d, want 2 (bytes=%d, max=%d)", st.Buffered, st.Bytes, st.MaxBytes)
	}
	if st.Dropped != 4 {
		t.Fatalf("Dropped = %d, want 4", st.Dropped)
	}
	if st.Bytes > st.MaxBytes {
		t.Fatalf("Bytes %d exceeds MaxBytes %d", st.Bytes, st.MaxBytes)
	}
	hdr := r.Header()
	if !hdr.Truncated || hdr.BufferedBytes != st.Bytes {
		t.Fatalf("Header mismatch: %+v vs stats %+v", hdr, st)
	}
}

func TestEnableDisableClearFilterAndNilSafety(t *testing.T) {
	var nilRec *Recorder
	nilRec.SetEnabled(true)
	nilRec.SetHeader("a", "b")
	nilRec.SetInitialSize(80, 24)
	nilRec.Record(Event{Kind: KindKey, Key: "j"})
	nilRec.RecordFrame(80, 24, "hello")
	nilRec.Clear()
	if nilRec.Enabled() || nilRec.Len() != 0 {
		t.Fatalf("nil recorder should report disabled and len 0")
	}
	if st := nilRec.Stats(); st.Total != 0 || st.Enabled {
		t.Fatalf("unexpected nil stats: %+v", st)
	}
	if _, evs := nilRec.Snapshot(); len(evs) != 0 {
		t.Fatalf("nil snapshot should be empty")
	}

	r := New(Options{
		Enabled: true,
		Filter: func(ev Event) bool {
			return ev.Kind != KindStatus
		},
	})
	r.SetHeader("mtm", "2.0")
	r.SetInitialSize(80, 24)
	r.SetInitialSize(120, 40) // must not overwrite initial size

	r.Record(Event{Kind: KindKey, Key: "a"})
	r.Record(Event{Kind: KindStatus, Status: "ignored"})
	r.SetEnabled(false)
	if r.Enabled() {
		t.Fatal("expected disabled")
	}
	r.Record(Event{Kind: KindKey, Key: "b"})
	r.SetEnabled(true)
	r.Record(Event{Kind: KindKey, Key: "c"})

	hdr, evs := r.Snapshot()
	if hdr.App != "mtm" || hdr.Version != "2.0" || hdr.Width != 80 || hdr.Height != 24 {
		t.Fatalf("unexpected header metadata: %+v", hdr)
	}
	if len(evs) != 2 || evs[0].Key != "a" || evs[1].Key != "c" {
		t.Fatalf("unexpected filtered/disabled events: %+v", evs)
	}
	if evs[0].Seq != 1 || evs[1].Seq != 2 {
		t.Fatalf("filtered/disabled events should not advance seq: %d, %d", evs[0].Seq, evs[1].Seq)
	}

	r.Clear()
	if r.Len() != 0 {
		t.Fatalf("Len after Clear = %d, want 0", r.Len())
	}
	st := r.Stats()
	if st.Total != 0 || st.Dropped != 0 || st.Bytes != 0 {
		t.Fatalf("Stats after Clear not reset: %+v", st)
	}
}

func TestRecordFrameDedupeAndCaptureFrames(t *testing.T) {
	// CaptureFrames = false (default)
	rOff := New(DefaultOptions())
	rOff.RecordFrame(80, 24, "\x1b[32mhello\x1b[0m")
	rOff.RecordFrame(80, 24, "\x1b[32mhello\x1b[0m") // duplicate: skipped
	rOff.RecordFrame(80, 24, "\x1b[31mworld\x1b[0m") // changed: recorded

	_, evs := rOff.Snapshot()
	if len(evs) != 2 {
		t.Fatalf("len(evs) = %d, want 2", len(evs))
	}
	if evs[0].Kind != KindFrame || evs[0].FrameHash == "" || evs[0].ANSI != "" {
		t.Fatalf("unexpected frame event (capture off): %+v", evs[0])
	}
	if evs[0].FrameHash == evs[1].FrameHash {
		t.Fatalf("distinct frames produced same hash %q", evs[0].FrameHash)
	}

	// Zero-alloc verification on idle duplicate frame.
	ansi := "\x1b[31mworld\x1b[0m"
	allocs := testing.AllocsPerRun(100, func() {
		rOff.RecordFrame(80, 24, ansi)
	})
	if allocs != 0 {
		t.Fatalf("idle RecordFrame allocs = %v, want 0", allocs)
	}

	// Clear resets frame dedupe state so the same ANSI is recorded again.
	rOff.Clear()
	rOff.RecordFrame(80, 24, ansi)
	if rOff.Len() != 1 {
		t.Fatalf("frame after Clear not recorded: len=%d", rOff.Len())
	}

	// CaptureFrames = true
	opts := DefaultOptions()
	opts.CaptureFrames = true
	rOn := New(opts)
	rOn.RecordFrame(80, 24, "\x1b[32mhello\x1b[0m")
	rOn.RecordFrame(80, 24, "\x1b[32mhello\x1b[0m")
	_, evsOn := rOn.Snapshot()
	if len(evsOn) != 1 || evsOn[0].ANSI != "\x1b[32mhello\x1b[0m" || evsOn[0].W != 80 || evsOn[0].H != 24 {
		t.Fatalf("unexpected frame event (capture on): %+v", evsOn)
	}
}

func TestJSONLRoundTrip(t *testing.T) {
	t0 := time.Date(2026, 11, 23, 20, 15, 30, 0, time.UTC)
	r := New(Options{
		App:           "ochazuke-demo",
		Version:       "0.9.1",
		Width:         100,
		Height:        30,
		MaxEvents:     128,
		MaxBytes:      64 << 10,
		Enabled:       true,
		CaptureFrames: true,
		Now:           fakeClock(t0, 25*time.Millisecond),
	})

	r.Record(Event{Kind: KindResize, W: 100, H: 30})
	r.Record(Event{Kind: KindKey, Key: "j", Text: "j"})
	r.Record(Event{Kind: KindCommand, Command: "motion.down", Source: "key:j", Handler: "global", Mode: "normal", Page: "main", FocusID: "list"})
	r.Record(Event{Kind: KindMouse, Button: "left", Action: "press", X: 4, Y: 7})
	r.RecordFrame(100, 30, "\x1b[1m<hello & world>\x1b[0m")

	var buf bytes.Buffer
	if err := r.WriteJSONL(&buf); err != nil {
		t.Fatalf("WriteJSONL: %v", err)
	}
	if strings.Contains(buf.String(), `\u003c`) {
		t.Fatalf("JSONL should not HTML-escape ANSI/brackets: %s", buf.String())
	}

	gotHdr, gotEvs, err := ReadJSONL(&buf)
	if err != nil {
		t.Fatalf("ReadJSONL: %v", err)
	}
	wantHdr, wantEvs := r.Snapshot()
	if gotHdr != wantHdr {
		t.Fatalf("Header round-trip mismatch:\n got: %+v\nwant: %+v", gotHdr, wantHdr)
	}
	if len(gotEvs) != len(wantEvs) {
		t.Fatalf("len(events) = %d, want %d", len(gotEvs), len(wantEvs))
	}
	for i := range wantEvs {
		if gotEvs[i] != wantEvs[i] {
			t.Errorf("event[%d] mismatch:\n got: %+v\nwant: %+v", i, gotEvs[i], wantEvs[i])
		}
	}
}

func TestSaveFileAtomicAndCollisionSuffix(t *testing.T) {
	fixed := time.Date(2026, 11, 23, 20, 30, 45, 0, time.UTC)
	r := New(Options{
		App:     "o-sandbox",
		Version: "1.0",
		Enabled: true,
		Now:     func() time.Time { return fixed },
	})
	r.Record(Event{Kind: KindKey, Key: "enter"})

	dir := filepath.Join(t.TempDir(), "nested", "sessions")
	p1, err := r.SaveFile(dir, "my app/test")
	if err != nil {
		t.Fatalf("SaveFile #1: %v", err)
	}
	want1 := filepath.Join(dir, "my-app-test-20261123-203045.jsonl")
	if p1 != want1 {
		t.Fatalf("path #1 = %q, want %q", p1, want1)
	}

	p2, err := r.SaveFile(dir, "my app/test")
	if err != nil {
		t.Fatalf("SaveFile #2: %v", err)
	}
	want2 := filepath.Join(dir, "my-app-test-20261123-203045-1.jsonl")
	if p2 != want2 {
		t.Fatalf("path #2 = %q, want %q", p2, want2)
	}

	info, err := os.Stat(p1)
	if err != nil {
		t.Fatalf("stat %s: %v", p1, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %04o, want 0600", perm)
	}

	// Verify no leftover temp files remain.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 files in dir, got %d", len(entries))
	}

	f, err := os.Open(p1)
	if err != nil {
		t.Fatalf("open %s: %v", p1, err)
	}
	defer f.Close()
	hdr, evs, err := ReadJSONL(f)
	if err != nil {
		t.Fatalf("ReadJSONL saved file: %v", err)
	}
	if hdr.Format != FormatV1 || len(evs) != 1 || evs[0].Key != "enter" {
		t.Fatalf("unexpected saved contents: hdr=%+v evs=%+v", hdr, evs)
	}
}

func TestConcurrentRaceSafety(t *testing.T) {
	r := New(Options{
		MaxEvents: 64,
		MaxBytes:  32 << 10,
		Enabled:   true,
	})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				r.Record(Event{Kind: KindKey, Key: "j"})
				r.RecordFrame(80, 24, fmt.Sprintf("frame-%d-%d", id, i%5))
				if i%10 == 0 {
					_ = r.Stats()
					_, _ = r.Snapshot()
					_ = r.Len()
				}
			}
		}(g)
	}
	wg.Wait()
	if st := r.Stats(); st.Buffered > st.MaxEvents || st.Total == 0 {
		t.Fatalf("unexpected concurrent stats: %+v", st)
	}
}
