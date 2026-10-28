package ochazuke

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// SegmentTickMsg is the segments' one-shot timer: it fires at the
// earliest next refresh of a shown segment (Model.segmentCommand), so a
// reading changes on an idle screen. At is the deadline it was scheduled
// for; only the message for the timer in flight clears it. Nothing shown,
// nothing is scheduled.
type SegmentTickMsg struct{ At time.Time }

// segmentTimer is the Model's segment scheduler state.
type segmentTimer struct {
	tickAt time.Time // the deadline of the SegmentTickMsg in flight, zero when none
}

// segmentCommand starts every shown segment that is due (never answered,
// past its interval, or asked with segment.refresh) and not already
// running, each as its own tea.Cmd, and schedules one SegmentTickMsg for
// the earliest next refresh, unless one is in flight for it.
func (m *Model) segmentCommand(now time.Time) tea.Cmd {
	r := m.Runtime
	rows := r.active()
	var commands []tea.Cmd
	var next time.Time
	for _, row := range rows {
		if row.inflight {
			continue
		}
		due := row.started.Add(row.row().every())
		if row.started.IsZero() || row.due || !due.After(now) {
			run := r.startSegment(row, now)
			commands = append(commands, func() tea.Msg { return run.do() })
			due = now.Add(row.row().every())
		}
		if next.IsZero() || due.Before(next) {
			next = due
		}
	}
	if !next.IsZero() && (m.segTimer.tickAt.IsZero() || next.Before(m.segTimer.tickAt)) {
		m.segTimer.tickAt = next
		at := next
		commands = append(commands, tea.Tick(max(0, next.Sub(now)), func(time.Time) tea.Msg { return SegmentTickMsg{At: at} }))
	}
	return tea.Batch(commands...)
}

// segmentFired clears the timer in flight when its message arrives.
func (m *Model) segmentFired(msg SegmentTickMsg) {
	if msg.At.Equal(m.segTimer.tickAt) {
		m.segTimer.tickAt = time.Time{}
	}
}

// SegmentTick is the segment timer in flight, the message its tea.Tick
// will deliver at its deadline (At), and false when none is scheduled. A
// host with a fake Now advances the clock to At and delivers the message
// itself rather than waiting.
func (m *Model) SegmentTick() (SegmentTickMsg, bool) {
	if m.segTimer.tickAt.IsZero() {
		return SegmentTickMsg{}, false
	}
	return SegmentTickMsg{At: m.segTimer.tickAt}, true
}

// SegmentsInFlight counts the refreshes started and not yet answered.
func (m *Model) SegmentsInFlight() int {
	n := 0
	for _, row := range m.Runtime.segs().rows {
		if row.inflight {
			n++
		}
	}
	return n
}

// StatusSegments are the segment texts the header rule draws this frame,
// first to last, before fitting (Layout.HeaderRule cuts what does not fit).
func (m *Model) StatusSegments() []string {
	var out []string
	for _, c := range m.Runtime.SegmentCells() {
		out = append(out, c.Text)
	}
	return out
}

// segmentSep joins the segments in the header rule.
const segmentSep = " · "

// HeaderRule is the rule line under the header, width cells: the plain
// rule with no segments; with segments, rule characters, then the
// segments right-aligned, joined by " · ", then " ──". Segments are
// dropped from the last while they do not fit, and when the first alone
// does not, it is cut with "…"; at least "── " of rule stays on the
// left. Each segment draws in its tone (muted without one), the rule and
// separators muted, so the chrome still reads as a rule.
func (l Layout) HeaderRule(cells []SegmentCell, width int) string {
	muted := l.Styles.Muted
	const lead, tail = "── ", " ──"
	room := width - ansi.StringWidth(lead) - ansi.StringWidth(tail)
	if len(cells) == 0 || room < 2 {
		return muted.Render(strings.Repeat("─", max(0, width)))
	}
	n, used := 0, 0
	for i, c := range cells {
		w := ansi.StringWidth(modalLabel(c.Text))
		if i > 0 {
			w += ansi.StringWidth(segmentSep)
		}
		if used+w > room {
			break
		}
		n, used = i+1, used+w
	}
	var b strings.Builder
	draw := func(c SegmentCell, text string) {
		style := muted
		if c.Tone != "" {
			style = l.Styles.Tone(c.Tone)
		}
		b.WriteString(style.Render(text))
	}
	if n == 0 {
		text := ansi.Truncate(modalLabel(cells[0].Text), room, "…")
		draw(cells[0], text)
		used = ansi.StringWidth(text)
	} else {
		for i, c := range cells[:n] {
			if i > 0 {
				b.WriteString(muted.Render(segmentSep))
			}
			draw(c, modalLabel(c.Text))
		}
	}
	fill := width - used - ansi.StringWidth(tail) - 1
	return muted.Render(strings.Repeat("─", fill)+" ") + b.String() + muted.Render(tail)
}
