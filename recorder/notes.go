package recorder

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FrameSnapshot holds the most recently rendered terminal frame cached by a
// Recorder or reconstructed from a recorded event stream.
type FrameSnapshot struct {
	W    int
	H    int
	Hash string
	ANSI string
}

// Empty reports whether the frame snapshot carries neither ANSI content nor a hash.
func (f FrameSnapshot) Empty() bool {
	return f.ANSI == "" && f.Hash == ""
}

// NoteInput describes an in-context UX note to record onto a session stream.
type NoteInput struct {
	NoteID    string
	Note      string
	Tag       string
	Severity  string
	Mode      string
	Page      string
	Modal     string
	FocusID   string
	Scope     string
	Status    string
	TargetSeq int
	W         int
	H         int
	FrameHash string
	ANSI      string
}

// LastFrame returns the latest rendered terminal frame cached by RecordFrame,
// even when CaptureFrames is false or passive recording is disabled.
func (r *Recorder) LastFrame() FrameSnapshot {
	if r == nil {
		return FrameSnapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.hasLastFrame {
		return FrameSnapshot{}
	}
	return FrameSnapshot{
		W:    r.lastFrameW,
		H:    r.lastFrameH,
		Hash: formatHex64(r.lastFrameHash),
		ANSI: r.lastFrameANSI,
	}
}

// RecordNote appends a KindNote event to the ring buffer. Unlike Record,
// RecordNote always captures the note even when passive recording is paused or
// a passive event filter is configured, and automatically attaches LastFrame()
// when in.ANSI is empty.
func (r *Recorder) RecordNote(in NoteInput) Event {
	if r == nil {
		return Event{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.noteSeq++
	noteID := strings.TrimSpace(in.NoteID)
	if noteID == "" {
		noteID = fmt.Sprintf("note-%03d", r.noteSeq)
	}

	ev := Event{
		Kind:      KindNote,
		NoteID:    noteID,
		Note:      in.Note,
		Tag:       strings.TrimSpace(in.Tag),
		Severity:  strings.TrimSpace(in.Severity),
		Mode:      in.Mode,
		Page:      in.Page,
		Modal:     in.Modal,
		FocusID:   in.FocusID,
		Scope:     in.Scope,
		Status:    in.Status,
		TargetSeq: in.TargetSeq,
		W:         in.W,
		H:         in.H,
		FrameHash: in.FrameHash,
		ANSI:      in.ANSI,
	}

	if ev.ANSI == "" && r.hasLastFrame {
		ev.ANSI = r.lastFrameANSI
		if ev.W <= 0 {
			ev.W = r.lastFrameW
		}
		if ev.H <= 0 {
			ev.H = r.lastFrameH
		}
		if ev.FrameHash == "" {
			ev.FrameHash = formatHex64(r.lastFrameHash)
		}
	} else if ev.ANSI != "" && ev.FrameHash == "" {
		ev.FrameHash = formatHex64(fnv1a64String(ev.ANSI))
	}

	return r.appendEventLocked(ev)
}

// FlushToFile persists the recorder's buffer to path in o-sandbox/v1 JSONL
// format. On the first flush (when path does not yet exist or is empty), it
// writes the Header followed by all buffered events atomically. On subsequent
// flushes to the same path, it incrementally appends only events with
// Seq > flushedSeq so repeated flushes produce zero duplicates.
func (r *Recorder) FlushToFile(path string) error {
	if r == nil {
		return errors.New("recorder: nil recorder")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("recorder: empty path")
	}

	r.flushMu.Lock()
	defer r.flushMu.Unlock()

	hdr, events := r.Snapshot()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	info, statErr := os.Stat(path)
	needsCreate := errors.Is(statErr, os.ErrNotExist) || (statErr == nil && info.Size() == 0)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	if needsCreate {
		if err := writeJSONLFileAtomic(path, hdr, events); err != nil {
			return err
		}
		r.flushedPath = path
		if len(events) > 0 {
			r.flushedSeq = events[len(events)-1].Seq
		} else {
			r.flushedSeq = 0
		}
		return nil
	}

	afterSeq := r.flushedSeq
	if r.flushedPath != path {
		_, existing, err := readJSONLFile(path)
		if err != nil {
			return err
		}
		afterSeq = maxEventSeq(existing)
	}

	var pending []Event
	for i := range events {
		if events[i].Seq > afterSeq {
			pending = append(pending, events[i])
		}
	}
	if len(pending) == 0 {
		r.flushedPath = path
		r.flushedSeq = afterSeq
		return nil
	}

	if err := appendEventsRaw(path, pending); err != nil {
		return err
	}
	r.flushedPath = path
	r.flushedSeq = pending[len(pending)-1].Seq
	return nil
}

// AppendEventToFile appends a single Event line to an o-sandbox/v1 JSONL file.
// If path does not exist or is empty, it initializes the file with a Header
// line first. Missing Seq, Type, At, OffsetMS, FrameHash, and NoteID fields are
// populated automatically from the existing recording.
func AppendEventToFile(path string, ev Event) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("recorder: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	ev.Type = "event"
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	} else {
		ev.At = ev.At.UTC()
	}
	if ev.ANSI != "" && ev.FrameHash == "" {
		ev.FrameHash = formatHex64(fnv1a64String(ev.ANSI))
	}

	info, statErr := os.Stat(path)
	needsCreate := errors.Is(statErr, os.ErrNotExist) || (statErr == nil && info.Size() == 0)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	if needsCreate {
		if ev.Seq <= 0 {
			ev.Seq = 1
		}
		if ev.Kind == KindNote && strings.TrimSpace(ev.NoteID) == "" {
			ev.NoteID = "note-001"
		}
		hdr := Header{
			Type:           "header",
			Format:         FormatV1,
			StartedAt:      ev.At,
			Width:          ev.W,
			Height:         ev.H,
			MaxEvents:      DefaultMaxEvents,
			MaxBytes:       DefaultMaxBytes,
			TotalEvents:    1,
			BufferedEvents: 1,
			BufferedBytes:  estimateEventBytes(ev),
		}
		return writeJSONLFileAtomic(path, hdr, []Event{ev})
	}

	hdr, existing, err := readJSONLFile(path)
	if err != nil {
		return err
	}
	if ev.Seq <= 0 {
		ev.Seq = maxEventSeq(existing) + 1
	}
	if ev.Kind == KindNote && strings.TrimSpace(ev.NoteID) == "" {
		ev.NoteID = fmt.Sprintf("note-%03d", len(FilterNotes(existing))+1)
	}
	if ev.OffsetMS == 0 && !hdr.StartedAt.IsZero() {
		offset := ev.At.Sub(hdr.StartedAt).Milliseconds()
		if offset > 0 {
			ev.OffsetMS = offset
		}
	}
	return appendEventsRaw(path, []Event{ev})
}

// FilterNotes returns all KindNote events in events, preserving chronological order.
func FilterNotes(events []Event) []Event {
	var notes []Event
	for i := range events {
		if events[i].Kind == KindNote {
			notes = append(notes, events[i])
		}
	}
	return notes
}

// ContextTrail returns up to limit chronological non-frame, non-note events
// occurring at or before upToSeq (or across all events when upToSeq <= 0).
func ContextTrail(events []Event, upToSeq int, limit int) []Event {
	if limit <= 0 || len(events) == 0 {
		return nil
	}
	matched := make([]Event, 0, min(limit, len(events)))
	for i := range events {
		ev := events[i]
		if upToSeq > 0 && ev.Seq > upToSeq {
			continue
		}
		if ev.Kind == KindFrame || ev.Kind == KindNote {
			continue
		}
		matched = append(matched, ev)
	}
	if len(matched) == 0 {
		return nil
	}
	if len(matched) > limit {
		matched = matched[len(matched)-limit:]
	}
	out := make([]Event, len(matched))
	copy(out, matched)
	return out
}

// FrameAt returns the best ANSI frame snapshot associated with seq in events.
// If the event at seq directly embeds ANSI (for example a KindNote or captured
// KindFrame), that frame is returned. If the event at seq is a KindNote without
// embedded ANSI and specifies TargetSeq, FrameAt resolves the frame at
// TargetSeq. Otherwise, it falls back to the most recent event at or before seq
// that carries non-empty ANSI.
func FrameAt(events []Event, seq int) (FrameSnapshot, bool) {
	if len(events) == 0 {
		return FrameSnapshot{}, false
	}
	if seq > 0 {
		for i := len(events) - 1; i >= 0; i-- {
			ev := events[i]
			if ev.Seq != seq {
				continue
			}
			if ev.ANSI != "" {
				return frameFromEvent(ev), true
			}
			if ev.Kind == KindNote && ev.TargetSeq > 0 && ev.TargetSeq != seq {
				if snap, ok := FrameAt(events, ev.TargetSeq); ok {
					return snap, true
				}
			}
			break
		}
	}
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if seq > 0 && ev.Seq > seq {
			continue
		}
		if ev.ANSI != "" {
			return frameFromEvent(ev), true
		}
	}
	return FrameSnapshot{}, false
}

func frameFromEvent(ev Event) FrameSnapshot {
	hash := ev.FrameHash
	if hash == "" && ev.ANSI != "" {
		hash = formatHex64(fnv1a64String(ev.ANSI))
	}
	return FrameSnapshot{
		W:    ev.W,
		H:    ev.H,
		Hash: hash,
		ANSI: ev.ANSI,
	}
}

func maxEventSeq(events []Event) int {
	maxSeq := 0
	for i := range events {
		if events[i].Seq > maxSeq {
			maxSeq = events[i].Seq
		}
	}
	return maxSeq
}

func readJSONLFile(path string) (Header, []Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return Header{}, nil, err
	}
	defer f.Close()
	return ReadJSONL(f)
}

func writeJSONLFileAtomic(path string, hdr Header, events []Event) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".recorder-flush-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	bw := bufio.NewWriter(tmp)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(hdr); err != nil {
		_ = tmp.Close()
		return err
	}
	for i := range events {
		if err := enc.Encode(&events[i]); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func appendEventsRaw(path string, events []Event) error {
	needsNewline, err := fileMissingTrailingNewline(path)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	bw := bufio.NewWriter(f)
	if needsNewline {
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	for i := range events {
		if err := enc.Encode(&events[i]); err != nil {
			return err
		}
	}
	return bw.Flush()
}

func fileMissingTrailingNewline(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() == 0 {
		return false, nil
	}
	var last [1]byte
	if _, err := f.ReadAt(last[:], info.Size()-1); err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	return last[0] != '\n', nil
}
