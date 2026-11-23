// Package recorder provides an in-memory circular ring-buffer session recorder
// for Ochazuke TUI applications in the o-sandbox/v1 JSONL format.
//
// recorder is a Tier 0 package importing only the Go standard library. It
// performs zero disk I/O outside SaveFile.
package recorder

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FormatV1 is the session recording JSONL format version identifier.
const FormatV1 = "o-sandbox/v1"

// Default ring-buffer retention bounds.
const (
	DefaultMaxEvents = 4096
	DefaultMaxBytes  = 8 << 20 // 8 MiB
)

// EventKind identifies the kind of a recorded session event.
type EventKind string

// Supported session event kinds.
const (
	KindKey     EventKind = "key"
	KindPaste   EventKind = "paste"
	KindMouse   EventKind = "mouse"
	KindResize  EventKind = "resize"
	KindCommand EventKind = "command"
	KindFrame   EventKind = "frame"
	KindStatus  EventKind = "status"
)

// Options configures a Recorder.
type Options struct {
	App           string
	Version       string
	Width         int
	Height        int
	MaxEvents     int
	MaxBytes      int
	Enabled       bool
	CaptureFrames bool
	Now           func() time.Time
	Filter        func(Event) bool
}

// DefaultOptions returns Options with default buffer limits and recording enabled.
func DefaultOptions() Options {
	return Options{
		MaxEvents: DefaultMaxEvents,
		MaxBytes:  DefaultMaxBytes,
		Enabled:   true,
	}
}

// Header is the first JSONL line of an o-sandbox/v1 session recording.
type Header struct {
	Type           string    `json:"type"`
	Format         string    `json:"format"`
	App            string    `json:"app,omitempty"`
	Version        string    `json:"version,omitempty"`
	StartedAt      time.Time `json:"startedAt"`
	Width          int       `json:"width,omitempty"`
	Height         int       `json:"height,omitempty"`
	MaxEvents      int       `json:"maxEvents"`
	MaxBytes       int       `json:"maxBytes"`
	TotalEvents    int       `json:"totalEvents"`
	DroppedEvents  int       `json:"droppedEvents"`
	BufferedEvents int       `json:"bufferedEvents"`
	BufferedBytes  int       `json:"bufferedBytes"`
	Truncated      bool      `json:"truncated"`
}

// Event is a single JSONL event line in an o-sandbox/v1 session recording.
type Event struct {
	Type      string    `json:"type"`
	Seq       int       `json:"seq"`
	OffsetMS  int64     `json:"offsetMs"`
	At        time.Time `json:"at"`
	Kind      EventKind `json:"kind"`
	Key       string    `json:"key,omitempty"`
	Text      string    `json:"text,omitempty"`
	Button    string    `json:"button,omitempty"`
	Action    string    `json:"action,omitempty"`
	X         int       `json:"x,omitempty"`
	Y         int       `json:"y,omitempty"`
	W         int       `json:"w,omitempty"`
	H         int       `json:"h,omitempty"`
	Command   string    `json:"command,omitempty"`
	Source    string    `json:"source,omitempty"`
	Handler   string    `json:"handler,omitempty"`
	Mode      string    `json:"mode,omitempty"`
	Page      string    `json:"page,omitempty"`
	FocusID   string    `json:"focusId,omitempty"`
	Status    string    `json:"status,omitempty"`
	FrameHash string    `json:"frameHash,omitempty"`
	ANSI      string    `json:"ansi,omitempty"`
}

// Stats reports a point-in-time summary of a Recorder's buffer state.
type Stats struct {
	Enabled   bool
	Buffered  int
	Total     int
	Dropped   int
	Bytes     int
	MaxEvents int
	MaxBytes  int
	Width     int
	Height    int
	StartedAt time.Time
}

type ringEntry struct {
	ev    Event
	bytes int
}

// Recorder is a concurrency-safe circular ring buffer of session events.
type Recorder struct {
	mu            sync.Mutex
	app           string
	version       string
	width         int
	height        int
	maxEvents     int
	maxBytes      int
	enabled       bool
	captureFrames bool
	now           func() time.Time
	filter        func(Event) bool

	startedAt time.Time
	seq       int
	total     int
	dropped   int

	buf   []ringEntry
	head  int
	count int
	bytes int

	hasLastFrame  bool
	lastFrameHash uint64
}

// New returns a Recorder initialized with opts. Non-positive MaxEvents or
// MaxBytes fall back to DefaultMaxEvents and DefaultMaxBytes.
func New(opts Options) *Recorder {
	if opts.MaxEvents <= 0 {
		opts.MaxEvents = DefaultMaxEvents
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Recorder{
		app:           opts.App,
		version:       opts.Version,
		width:         opts.Width,
		height:        opts.Height,
		maxEvents:     opts.MaxEvents,
		maxBytes:      opts.MaxBytes,
		enabled:       opts.Enabled,
		captureFrames: opts.CaptureFrames,
		now:           opts.Now,
		filter:        opts.Filter,
		startedAt:     opts.Now().UTC(),
	}
}

// Enabled reports whether the recorder is currently capturing events.
func (r *Recorder) Enabled() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enabled
}

// SetEnabled toggles event recording on or off.
func (r *Recorder) SetEnabled(on bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.enabled = on
	r.mu.Unlock()
}

// SetHeader updates the application name and version reported in Header.
func (r *Recorder) SetHeader(app, version string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.app = app
	r.version = version
	r.mu.Unlock()
}

// SetInitialSize records the initial terminal dimensions if they have not yet
// been set and both w and h are positive.
func (r *Recorder) SetInitialSize(w, h int) {
	if r == nil || w <= 0 || h <= 0 {
		return
	}
	r.mu.Lock()
	if r.width == 0 && r.height == 0 {
		r.width = w
		r.height = h
	}
	r.mu.Unlock()
}

// Record appends ev to the ring buffer when r is non-nil, enabled, and accepted
// by Filter. It assigns Type, Seq, At, and OffsetMS and evicts oldest entries
// as needed to honor MaxEvents and MaxBytes.
func (r *Recorder) Record(ev Event) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.enabled {
		return
	}
	r.recordLocked(ev)
}

func (r *Recorder) recordLocked(ev Event) bool {
	ev.Type = "event"
	if r.filter != nil && !r.filter(ev) {
		return false
	}

	now := r.now()
	offset := now.Sub(r.startedAt).Milliseconds()
	if offset < 0 {
		offset = 0
	}

	r.seq++
	r.total++
	ev.Seq = r.seq
	ev.At = now.UTC()
	ev.OffsetMS = offset

	evBytes := estimateEventBytes(ev)
	for r.count > 0 && (r.count >= r.maxEvents || r.bytes+evBytes > r.maxBytes) {
		r.evictOldestLocked()
	}

	if r.count == len(r.buf) && len(r.buf) < r.maxEvents {
		newCap := len(r.buf) * 2
		if newCap < 64 {
			newCap = 64
		}
		if newCap > r.maxEvents {
			newCap = r.maxEvents
		}
		next := make([]ringEntry, newCap)
		for i := 0; i < r.count; i++ {
			next[i] = r.buf[(r.head+i)%len(r.buf)]
		}
		r.buf = next
		r.head = 0
	}

	idx := (r.head + r.count) % len(r.buf)
	r.buf[idx] = ringEntry{ev: ev, bytes: evBytes}
	r.count++
	r.bytes += evBytes
	return true
}

func (r *Recorder) evictOldestLocked() {
	r.bytes -= r.buf[r.head].bytes
	r.buf[r.head] = ringEntry{}
	r.head = (r.head + 1) % len(r.buf)
	r.count--
	r.dropped++
}

// RecordFrame hashes ansi using an inline zero-allocation FNV-1a 64-bit hash
// and records a KindFrame event only when the hash differs from the previous
// recorded frame. ANSI content is stored only when CaptureFrames is enabled.
func (r *Recorder) RecordFrame(w, h int, ansi string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.enabled {
		return
	}
	hash := fnv1a64String(ansi)
	if r.hasLastFrame && hash == r.lastFrameHash {
		return
	}
	ev := Event{
		Kind:      KindFrame,
		W:         w,
		H:         h,
		FrameHash: formatHex64(hash),
	}
	if r.captureFrames {
		ev.ANSI = ansi
	}
	if r.recordLocked(ev) {
		r.hasLastFrame = true
		r.lastFrameHash = hash
	}
}

// Clear resets the ring buffer, counters, start timestamp, and frame-hash state.
func (r *Recorder) Clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.buf = nil
	r.head = 0
	r.count = 0
	r.bytes = 0
	r.seq = 0
	r.total = 0
	r.dropped = 0
	r.hasLastFrame = false
	r.lastFrameHash = 0
	r.startedAt = r.now().UTC()
	r.mu.Unlock()
}

// Len returns the number of currently buffered events.
func (r *Recorder) Len() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// Stats returns a snapshot of the recorder's buffer counters and settings.
func (r *Recorder) Stats() Stats {
	if r == nil {
		return Stats{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return Stats{
		Enabled:   r.enabled,
		Buffered:  r.count,
		Total:     r.total,
		Dropped:   r.dropped,
		Bytes:     r.bytes,
		MaxEvents: r.maxEvents,
		MaxBytes:  r.maxBytes,
		Width:     r.width,
		Height:    r.height,
		StartedAt: r.startedAt,
	}
}

// Header returns the current recording Header metadata.
func (r *Recorder) Header() Header {
	if r == nil {
		return Header{Type: "header", Format: FormatV1}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.headerLocked()
}

func (r *Recorder) headerLocked() Header {
	return Header{
		Type:           "header",
		Format:         FormatV1,
		App:            r.app,
		Version:        r.version,
		StartedAt:      r.startedAt,
		Width:          r.width,
		Height:         r.height,
		MaxEvents:      r.maxEvents,
		MaxBytes:       r.maxBytes,
		TotalEvents:    r.total,
		DroppedEvents:  r.dropped,
		BufferedEvents: r.count,
		BufferedBytes:  r.bytes,
		Truncated:      r.dropped > 0,
	}
}

// Snapshot returns a consistent copy of the Header and all buffered events in
// chronological order.
func (r *Recorder) Snapshot() (Header, []Event) {
	if r == nil {
		return Header{Type: "header", Format: FormatV1}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	hdr := r.headerLocked()
	if r.count == 0 {
		return hdr, nil
	}
	events := make([]Event, r.count)
	for i := 0; i < r.count; i++ {
		events[i] = r.buf[(r.head+i)%len(r.buf)].ev
	}
	return hdr, events
}

// WriteJSONL writes the current Header and buffered events as JSON Lines to w.
func (r *Recorder) WriteJSONL(w io.Writer) error {
	if r == nil {
		return errors.New("recorder: nil recorder")
	}
	if w == nil {
		return errors.New("recorder: nil writer")
	}
	hdr, events := r.Snapshot()
	bw := bufio.NewWriter(w)
	enc := json.NewEncoder(bw)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(hdr); err != nil {
		return err
	}
	for i := range events {
		if err := enc.Encode(&events[i]); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// ReadJSONL decodes an o-sandbox/v1 JSONL stream into its Header and Events.
func ReadJSONL(r io.Reader) (Header, []Event, error) {
	if r == nil {
		return Header{}, nil, errors.New("recorder: nil reader")
	}
	br := bufio.NewReader(r)
	var (
		hdr     Header
		hasHdr  bool
		events  []Event
		lineNum int
	)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return Header{}, nil, err
		}
		lineNum++
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			if !hasHdr {
				if uerr := json.Unmarshal(trimmed, &hdr); uerr != nil {
					return Header{}, nil, fmt.Errorf("recorder: decode header at line %d: %w", lineNum, uerr)
				}
				if hdr.Type != "header" {
					return Header{}, nil, fmt.Errorf("recorder: line %d expected type %q, got %q", lineNum, "header", hdr.Type)
				}
				if hdr.Format != FormatV1 {
					return Header{}, nil, fmt.Errorf("recorder: unsupported format %q", hdr.Format)
				}
				hasHdr = true
			} else {
				var ev Event
				if uerr := json.Unmarshal(trimmed, &ev); uerr != nil {
					return Header{}, nil, fmt.Errorf("recorder: decode event at line %d: %w", lineNum, uerr)
				}
				if ev.Type != "" && ev.Type != "event" {
					return Header{}, nil, fmt.Errorf("recorder: line %d expected type %q, got %q", lineNum, "event", ev.Type)
				}
				events = append(events, ev)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if !hasHdr {
		return Header{}, nil, errors.New("recorder: empty JSONL stream")
	}
	return hdr, events, nil
}

// SaveFile atomically writes the current session snapshot into dir as
// <appName>-<YYYYMMDD-HHMMSS>.jsonl (appending -1, -2, ... on filename
// collision). It creates dir with mode 0o755 and writes the file via a 0o600
// temporary file renamed into place.
func (r *Recorder) SaveFile(dir, appName string) (string, error) {
	if r == nil {
		return "", errors.New("recorder: nil recorder")
	}
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("recorder: empty directory")
	}

	r.mu.Lock()
	fallbackApp := r.app
	now := r.now()
	r.mu.Unlock()

	name := sanitizeAppName(appName)
	if name == "" {
		name = sanitizeAppName(fallbackApp)
	}
	if name == "" {
		name = "session"
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(dir, ".recorder-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := r.WriteJSONL(tmp); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	base := fmt.Sprintf("%s-%s", name, now.UTC().Format("20060102-150405"))
	for suffix := 0; ; suffix++ {
		filename := base + ".jsonl"
		if suffix > 0 {
			filename = fmt.Sprintf("%s-%d.jsonl", base, suffix)
		}
		target := filepath.Join(dir, filename)
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_ = f.Close()
		if err := os.Rename(tmpPath, target); err != nil {
			_ = os.Remove(target)
			return "", err
		}
		cleanupTmp = false
		return target, nil
	}
}

func sanitizeAppName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if isAlphaNum || c == '-' || c == '_' || c == '.' {
			b.WriteByte(c)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-.")
}

func estimateEventBytes(ev Event) int {
	const baseOverhead = 96
	return baseOverhead +
		len(ev.Kind) +
		len(ev.Key) +
		len(ev.Text) +
		len(ev.Button) +
		len(ev.Action) +
		len(ev.Command) +
		len(ev.Source) +
		len(ev.Handler) +
		len(ev.Mode) +
		len(ev.Page) +
		len(ev.FocusID) +
		len(ev.Status) +
		len(ev.FrameHash) +
		len(ev.ANSI)
}

const (
	fnvOffset64 uint64 = 14695981039346656037
	fnvPrime64  uint64 = 1099511628211
)

func fnv1a64String(s string) uint64 {
	h := fnvOffset64
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	return h
}

func formatHex64(v uint64) string {
	const digits = "0123456789abcdef"
	var buf [16]byte
	for i := 15; i >= 0; i-- {
		buf[i] = digits[v&0xf]
		v >>= 4
	}
	return string(buf[:])
}
