package recorder

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLastFrameAndRecordNoteWhilePaused(t *testing.T) {
	t0 := time.Date(2026, 11, 23, 22, 0, 0, 0, time.UTC)
	r := New(Options{
		App:           "matomeru",
		Version:       "1.14.0",
		Enabled:       false, // passive recording paused
		CaptureFrames: false, // frame ANSI not stored on KindFrame
		Now:           fakeClock(t0, 50*time.Millisecond),
		Filter: func(ev Event) bool {
			return false // passive filter rejects everything; RecordNote must bypass
		},
	})

	if snap := r.LastFrame(); !snap.Empty() {
		t.Fatalf("expected empty LastFrame before render, got %+v", snap)
	}

	ansi := "\x1b[1mMain Space\x1b[0m\nrow 1\nrow 2"
	r.RecordFrame(120, 36, ansi)
	if r.Len() != 0 {
		t.Fatalf("paused recorder should not emit KindFrame, got len=%d", r.Len())
	}

	snap := r.LastFrame()
	if snap.W != 120 || snap.H != 36 || snap.ANSI != ansi || snap.Hash == "" {
		t.Fatalf("LastFrame mismatch: %+v", snap)
	}

	// RecordNote always records even when paused and auto-attaches LastFrame().
	ev := r.RecordNote(NoteInput{
		Note:     "Title bar overlaps filter chip",
		Tag:      "layout",
		Severity: "bug",
		Mode:     "normal",
		Page:     "tasks",
		Modal:    "picker",
		FocusID:  "tasks.table",
		Scope:    "table",
		Status:   "ready",
	})
	if ev.Kind != KindNote || ev.NoteID != "note-001" || ev.Seq != 1 {
		t.Fatalf("unexpected note event: %+v", ev)
	}
	if ev.ANSI != ansi || ev.W != 120 || ev.H != 36 || ev.FrameHash != snap.Hash {
		t.Fatalf("note did not auto-attach LastFrame: got (%dx%d hash=%q len=%d), want (%dx%d hash=%q)",
			ev.W, ev.H, ev.FrameHash, len(ev.ANSI), snap.W, snap.H, snap.Hash)
	}
	if ev.Modal != "picker" || ev.Scope != "table" || ev.Tag != "layout" || ev.Severity != "bug" {
		t.Fatalf("note metadata mismatch: %+v", ev)
	}

	// Explicit ANSI overrides LastFrame() and hashes itself.
	ev2 := r.RecordNote(NoteInput{
		Note:      "Explicit snapshot",
		ANSI:      "frozen-before-modal",
		W:         80,
		H:         24,
		TargetSeq: 1,
	})
	if ev2.NoteID != "note-002" || ev2.ANSI != "frozen-before-modal" || ev2.FrameHash == "" || ev2.TargetSeq != 1 {
		t.Fatalf("explicit ANSI note mismatch: %+v", ev2)
	}
}

func TestFlushToFileIncrementalAndZeroDuplicates(t *testing.T) {
	t0 := time.Date(2026, 11, 23, 22, 10, 0, 0, time.UTC)
	r := New(Options{
		App:     "matomeru",
		Version: "1.14.0",
		Width:   120,
		Height:  36,
		Enabled: true,
		Now:     fakeClock(t0, 20*time.Millisecond),
	})

	path := filepath.Join(t.TempDir(), "recordings", "session.jsonl")
	r.Record(Event{Kind: KindKey, Key: "j"})
	r.RecordFrame(120, 36, "frame-1")
	n1 := r.RecordNote(NoteInput{Note: "First UX note", Page: "tasks"})

	if err := r.FlushToFile(path); err != nil {
		t.Fatalf("FlushToFile #1: %v", err)
	}
	// Repeated flush with no new events must be a no-op (zero duplicates).
	if err := r.FlushToFile(path); err != nil {
		t.Fatalf("FlushToFile #2 (no-op): %v", err)
	}

	// Add more events and another note, then flush incrementally.
	r.Record(Event{Kind: KindKey, Key: "k"})
	r.RecordFrame(120, 36, "frame-2")
	n2 := r.RecordNote(NoteInput{Note: "Second UX note", Page: "panes"})
	if err := r.FlushToFile(path); err != nil {
		t.Fatalf("FlushToFile #3: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open flushed file: %v", err)
	}
	defer f.Close()
	hdr, evs, err := ReadJSONL(f)
	if err != nil {
		t.Fatalf("ReadJSONL flushed file: %v", err)
	}
	if hdr.Format != FormatV1 || hdr.App != "matomeru" {
		t.Fatalf("unexpected header: %+v", hdr)
	}
	if len(evs) != 6 {
		t.Fatalf("expected 6 unique events, got %d: %+v", len(evs), evs)
	}
	for i := range evs {
		if evs[i].Seq != i+1 {
			t.Fatalf("evs[%d].Seq = %d, want %d (possible duplicate/gap)", i, evs[i].Seq, i+1)
		}
	}
	notes := FilterNotes(evs)
	if len(notes) != 2 || notes[0].NoteID != n1.NoteID || notes[1].NoteID != n2.NoteID {
		t.Fatalf("FilterNotes mismatch: %+v", notes)
	}
	if notes[0].ANSI != "frame-1" || notes[1].ANSI != "frame-2" {
		t.Fatalf("note frames mismatch: %q vs %q", notes[0].ANSI, notes[1].ANSI)
	}
}

func TestAppendEventToFileContextTrailAndFrameAt(t *testing.T) {
	t0 := time.Date(2026, 11, 23, 22, 20, 0, 0, time.UTC)
	r := New(Options{
		App:           "matomeru",
		Version:       "1.14.0",
		Enabled:       true,
		CaptureFrames: true,
		Now:           fakeClock(t0, 10*time.Millisecond),
	})
	r.RecordFrame(80, 24, "screen-A")
	r.Record(Event{Kind: KindKey, Key: "j"})
	r.Record(Event{Kind: KindCommand, Command: "motion.down", Mode: "normal", Page: "tasks"})
	r.RecordFrame(80, 24, "screen-B")
	r.Record(Event{Kind: KindKey, Key: "enter"})
	n1 := r.RecordNote(NoteInput{Note: "Initial note", Page: "tasks", ANSI: "screen-frozen-note"})

	path := filepath.Join(t.TempDir(), "replay.jsonl")
	if err := r.FlushToFile(path); err != nil {
		t.Fatalf("FlushToFile: %v", err)
	}

	// Append a replay note referencing seq 3 (which has screen-A as active frame).
	if err := AppendEventToFile(path, Event{
		Kind:      KindNote,
		Note:      "Follow-up note added in replay",
		Tag:       "triage",
		TargetSeq: 3,
	}); err != nil {
		t.Fatalf("AppendEventToFile: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open replay file: %v", err)
	}
	defer f.Close()
	_, evs, err := ReadJSONL(f)
	if err != nil {
		t.Fatalf("ReadJSONL: %v", err)
	}
	if len(evs) != 7 {
		t.Fatalf("expected 7 events after append, got %d", len(evs))
	}
	last := evs[len(evs)-1]
	if last.Seq != 7 || last.NoteID != "note-002" || last.TargetSeq != 3 {
		t.Fatalf("appended note fields mismatch: %+v", last)
	}

	// ContextTrail up to n1.Seq excludes frames and notes and respects limit.
	trail := ContextTrail(evs, n1.Seq, 2)
	if len(trail) != 2 || trail[0].Kind != KindCommand || trail[1].Key != "enter" {
		t.Fatalf("ContextTrail(seq=%d, limit=2) = %+v", n1.Seq, trail)
	}

	// FrameAt resolves: direct note ANSI, TargetSeq fallback, and earlier KindFrame.
	if snap, ok := FrameAt(evs, n1.Seq); !ok || snap.ANSI != "screen-frozen-note" {
		t.Fatalf("FrameAt(note with ANSI) = (%+v, %v)", snap, ok)
	}
	if snap, ok := FrameAt(evs, last.Seq); !ok || snap.ANSI != "screen-A" {
		t.Fatalf("FrameAt(replay note targeting seq 3) = (%+v, %v), want screen-A", snap, ok)
	}
	if snap, ok := FrameAt(evs, 5); !ok || snap.ANSI != "screen-B" {
		t.Fatalf("FrameAt(seq 5) = (%+v, %v), want screen-B", snap, ok)
	}

	// AppendEventToFile also bootstraps a brand-new file cleanly.
	fresh := filepath.Join(t.TempDir(), "fresh.jsonl")
	if err := AppendEventToFile(fresh, Event{Kind: KindNote, Note: "Standalone note", ANSI: "solo"}); err != nil {
		t.Fatalf("AppendEventToFile fresh: %v", err)
	}
	ff, err := os.Open(fresh)
	if err != nil {
		t.Fatalf("open fresh: %v", err)
	}
	defer ff.Close()
	fhdr, fevs, err := ReadJSONL(ff)
	if err != nil {
		t.Fatalf("ReadJSONL fresh: %v", err)
	}
	if fhdr.Format != FormatV1 || len(fevs) != 1 || fevs[0].NoteID != "note-001" || fevs[0].ANSI != "solo" {
		t.Fatalf("unexpected fresh file: hdr=%+v evs=%+v", fhdr, fevs)
	}
}
