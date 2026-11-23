// Package toast is a pure, non-blocking transient notification strip widget.
//
// It manages timed notification items with an injectable clock, supports
// explicit dismissal and opt-out via Disabled, and renders to a ui.ViewNode
// without performing I/O or importing Bubble Tea / Lip Gloss.
package toast

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/layout"
	"ofam.dev/ochazuke/ui"
)

// Level classifies a toast notification's severity.
type Level string

// Notification levels.
const (
	Info Level = "info"
	Ok   Level = "ok"
	Warn Level = "warn"
	Err  Level = "err"
)

// Defaults for newly created toast stores.
const (
	DefaultTTL       = 3 * time.Second
	DefaultMaxActive = 5
)

// Command IDs handled by Model.HandleCommand.
const (
	CmdDismiss    = "toast.dismiss"
	CmdDismissAll = "toast.dismiss.all"
)

// Toast is a single transient notification.
type Toast struct {
	ID        string        `json:"id"`
	Text      string        `json:"text"`
	Level     Level         `json:"level"`
	CreatedAt time.Time     `json:"createdAt"`
	TTL       time.Duration `json:"ttl"`
}

// Expired reports whether t has reached or passed its expiry at now.
// A Toast with TTL <= 0 is sticky and never expires automatically.
func (t Toast) Expired(now time.Time) bool {
	if t.TTL <= 0 {
		return false
	}
	return !now.Before(t.CreatedAt.Add(t.TTL))
}

// Tone returns the semantic ui.Theme tone name for t.Level.
func (t Toast) Tone() string {
	switch t.Level {
	case Ok:
		return "success"
	case Warn:
		return "warning"
	case Err:
		return "error"
	default:
		return "info"
	}
}

// BadgeTone returns the badge tone suffix matching t.Level.
func (t Toast) BadgeTone() string {
	switch t.Level {
	case Ok:
		return "success"
	case Warn:
		return "warning"
	case Err:
		return "error"
	default:
		return "accent"
	}
}

// Icon returns the short leading glyph for t.Level.
func (t Toast) Icon() string {
	switch t.Level {
	case Ok:
		return "✓"
	case Warn:
		return "▲"
	case Err:
		return "✗"
	default:
		return "ℹ"
	}
}

// Model holds active transient notifications. Apps can bind Disabled directly
// to a user setting (e.g. !settings.Bool("ui.toasts", true)) to opt out.
type Model struct {
	Disabled   bool
	DefaultTTL time.Duration
	MaxActive  int
	Now        func() time.Time

	items []Toast
	seq   uint64
}

// Store is an alias for Model for callers that prefer store terminology.
type Store = Model

// New returns a Model configured with DefaultTTL and DefaultMaxActive.
func New() *Model {
	return &Model{
		DefaultTTL: DefaultTTL,
		MaxActive:  DefaultMaxActive,
	}
}

// NewWithClock returns a Model using now as its clock source.
func NewWithClock(now func() time.Time) *Model {
	m := New()
	m.Now = now
	return m
}

// Enabled reports whether notifications are enabled.
func (m *Model) Enabled() bool { return !m.Disabled }

// SetEnabled toggles notifications on or off.
func (m *Model) SetEnabled(enabled bool) { m.Disabled = !enabled }

func (m *Model) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Model) defaultTTL() time.Duration {
	if m.DefaultTTL > 0 {
		return m.DefaultTTL
	}
	return DefaultTTL
}

func (m *Model) maxActive() int {
	if m.MaxActive > 0 {
		return m.MaxActive
	}
	return DefaultMaxActive
}

// Push records a new toast using the store's clock and returns it.
// Pass an optional ttl: 0 or omitted uses DefaultTTL; a negative value creates
// a sticky toast (TTL == 0) that stays until dismissed.
// When Disabled is true, Push is a no-op and returns a zero Toast.
func (m *Model) Push(text string, level Level, ttl ...time.Duration) Toast {
	return m.PushAt(text, level, m.now(), ttl...)
}

// PushAt records a new toast stamped with now.
func (m *Model) PushAt(text string, level Level, now time.Time, ttl ...time.Duration) Toast {
	if m.Disabled {
		return Toast{}
	}
	d := m.defaultTTL()
	if len(ttl) > 0 {
		switch {
		case ttl[0] < 0:
			d = 0
		case ttl[0] > 0:
			d = ttl[0]
		}
	}
	if level == "" {
		level = Info
	}
	return m.appendToast(Toast{
		Text:      text,
		Level:     level,
		CreatedAt: now,
		TTL:       d,
	})
}

// PushToast inserts a caller-constructed Toast, filling any missing ID, Level,
// or CreatedAt. A negative TTL is normalized to 0 (sticky); a zero TTL uses
// DefaultTTL.
func (m *Model) PushToast(t Toast) Toast {
	if m.Disabled {
		return Toast{}
	}
	if t.Level == "" {
		t.Level = Info
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = m.now()
	}
	switch {
	case t.TTL < 0:
		t.TTL = 0
	case t.TTL == 0:
		t.TTL = m.defaultTTL()
	}
	return m.appendToast(t)
}

func (m *Model) appendToast(t Toast) Toast {
	if t.ID == "" {
		m.seq++
		t.ID = "toast-" + strconv.FormatUint(m.seq, 10)
	}
	// Replace an existing toast with the same ID to allow in-place updates.
	for i := range m.items {
		if m.items[i].ID == t.ID {
			m.items[i] = t
			return t
		}
	}
	m.items = append(m.items, t)
	if limit := m.maxActive(); len(m.items) > limit {
		m.items = slices.Clone(m.items[len(m.items)-limit:])
	}
	return t
}

// Dismiss removes the toast with the given ID and reports whether it existed.
func (m *Model) Dismiss(id string) bool {
	for i := range m.items {
		if m.items[i].ID == id {
			m.items = slices.Delete(m.items, i, i+1)
			return true
		}
	}
	return false
}

// DismissAll clears every active toast.
func (m *Model) DismissAll() {
	m.items = nil
}

// Tick prunes all toasts expired at now and reports whether any were removed.
func (m *Model) Tick(now time.Time) bool {
	if len(m.items) == 0 {
		return false
	}
	kept := m.items[:0]
	removed := false
	for _, t := range m.items {
		if t.Expired(now) {
			removed = true
			continue
		}
		kept = append(kept, t)
	}
	if len(kept) == 0 {
		m.items = nil
	} else {
		m.items = kept
	}
	return removed
}

// NextExpiry returns the earliest expiry timestamp among active timed toasts.
func (m *Model) NextExpiry() (time.Time, bool) {
	if m.Disabled {
		return time.Time{}, false
	}
	var earliest time.Time
	found := false
	for _, t := range m.items {
		if t.TTL <= 0 {
			continue
		}
		exp := t.CreatedAt.Add(t.TTL)
		if !found || exp.Before(earliest) {
			earliest = exp
			found = true
		}
	}
	return earliest, found
}

// Active returns a copy of currently active toasts (oldest to newest), or nil
// when Disabled or empty.
func (m *Model) Active() []Toast {
	if m.Disabled || len(m.items) == 0 {
		return nil
	}
	return slices.Clone(m.items)
}

// Latest returns the most recently pushed active toast.
func (m *Model) Latest() (Toast, bool) {
	if m.Disabled || len(m.items) == 0 {
		return Toast{}, false
	}
	return m.items[len(m.items)-1], true
}

// Len returns the count of currently active toasts (0 when Disabled).
func (m *Model) Len() int {
	if m.Disabled {
		return 0
	}
	return len(m.items)
}

// HandleCommand processes toast dismissal commands.
func (m *Model) HandleCommand(cmd core.Command) bool {
	switch cmd.ID {
	case CmdDismiss:
		if id := cmd.Arg("id"); id != "" {
			return m.Dismiss(id)
		}
		if latest, ok := m.Latest(); ok {
			return m.Dismiss(latest.ID)
		}
		return false
	case CmdDismissAll:
		if len(m.items) == 0 {
			return false
		}
		m.DismissAll()
		return true
	}
	return false
}

// View renders the active toasts as a single-row non-blocking ui.KindText strip
// (newest first, each styled by its semantic level tone). When Disabled or
// empty, it returns an empty KindText node with active=false in Facts.
func (m *Model) View(id string) ui.ViewNode {
	if id == "" {
		id = "toast"
	}
	active := m.Active()
	if len(active) == 0 {
		return ui.ViewNode{
			ID:    id,
			Kind:  ui.KindText,
			Scope: "toast",
			Box:   layout.Constraints{FixedH: 1},
			Props: map[string]any{"text": ""},
			Facts: map[string]any{
				"active":   false,
				"count":    0,
				"disabled": m.Disabled,
			},
			CacheKey: 1,
		}
	}

	var sb strings.Builder
	var spans []ui.ToneSpan
	runePos := 0
	const sep = " · "
	sepRunes := utf8.RuneCountInString(sep)

	for i := len(active) - 1; i >= 0; i-- {
		t := active[i]
		if sb.Len() > 0 {
			sb.WriteString(sep)
			spans = append(spans, ui.ToneSpan{
				From: runePos,
				To:   runePos + sepRunes,
				Tone: "muted",
			})
			runePos += sepRunes
		}
		seg := t.Icon() + " " + t.Text
		segRunes := utf8.RuneCountInString(seg)
		sb.WriteString(seg)
		spans = append(spans, ui.ToneSpan{
			From: runePos,
			To:   runePos + segRunes,
			Tone: t.Tone(),
		})
		runePos += segRunes
	}

	latest := active[len(active)-1]
	text := sb.String()
	return ui.ViewNode{
		ID:    id,
		Kind:  ui.KindText,
		Scope: "toast",
		Tone:  latest.Tone(),
		Box:   layout.Constraints{FixedH: 1},
		Props: map[string]any{
			"text":  text,
			"spans": spans,
		},
		Facts: map[string]any{
			"active":   true,
			"count":    len(active),
			"disabled": false,
			"level":    string(latest.Level),
		},
		CacheKey: cacheKey(active),
	}
}

// ViewOverlay renders the active toasts as a floating ui.KindOverlay containing
// one KindText child per active toast (newest first). Returns an empty KindText
// node when Disabled or empty.
func (m *Model) ViewOverlay(id string) ui.ViewNode {
	if id == "" {
		id = "toast.overlay"
	}
	active := m.Active()
	if len(active) == 0 {
		return m.View(id)
	}
	latest := active[len(active)-1]
	kids := make([]ui.ViewNode, 0, len(active))
	for i := len(active) - 1; i >= 0; i-- {
		t := active[i]
		kids = append(kids, ui.ViewNode{
			ID:   id + "." + t.ID,
			Kind: ui.KindText,
			Tone: t.Tone(),
			Props: map[string]any{
				"text": t.Icon() + " " + t.Text,
			},
		})
	}
	return ui.ViewNode{
		ID:       id,
		Kind:     ui.KindOverlay,
		Scope:    "toast",
		Title:    "Notifications",
		Tone:     latest.Tone(),
		Badges:   []ui.Badge{{Text: strconv.Itoa(len(active)), Tone: latest.BadgeTone()}},
		Children: kids,
		Facts: map[string]any{
			"active":   true,
			"count":    len(active),
			"disabled": false,
			"level":    string(latest.Level),
		},
		CacheKey: cacheKey(active),
	}
}

func cacheKey(active []Toast) uint64 {
	h := fnv.New64a()
	for _, t := range active {
		fmt.Fprintf(h, "%s|%s|%s\n", t.ID, t.Level, t.Text)
	}
	return h.Sum64() | 1
}
