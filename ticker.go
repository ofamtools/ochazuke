package ochazuke

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// JobTickerRows is the most rows the jobs ticker draws, over the body's
// last rows above the footer.
const JobTickerRows = 2

// JobLinger is how long a finished job stays on the ticker after the
// frame that first saw it finished. The Model schedules one JobLingerMsg
// for the earliest remembered finish (Model.lingerCommand), so the row
// goes on an idle screen; any earlier frame past the deadline drops it
// too, and a newer job can push it off sooner.
const JobLinger = 4 * time.Second

// jobTicker is the Model's short memory of finished jobs: jobs.list deletes
// finished jobs on read, so the ticker keeps its own copy for JobLinger.
// It reads the runtime through Jobs, never through jobs.list.
type jobTicker struct {
	finished []tickerJob     // newest finish first
	seen     map[uint64]bool // finished jobs already remembered (or expired)
	tickAt   time.Time       // the deadline of the JobLingerMsg in flight, zero when none
}

// JobLingerMsg is the ticker's one-shot timer: it fires when the oldest
// remembered finished job passes JobLinger, so the frame it causes drops
// the row with no key pressed. At is the deadline it was scheduled for;
// only the message for the timer in flight clears it.
type JobLingerMsg struct{ At time.Time }

// lingerCommand schedules a JobLingerMsg for the earliest remembered
// finished job, unless one is already in flight for it. Nothing lingers,
// nothing is scheduled: there is no periodic tick.
func (t *jobTicker) lingerCommand(now time.Time) tea.Cmd {
	if len(t.finished) == 0 {
		return nil
	}
	next := t.finished[len(t.finished)-1].at.Add(JobLinger)
	if !t.tickAt.IsZero() && !next.Before(t.tickAt) {
		return nil
	}
	t.tickAt = next
	return tea.Tick(next.Sub(now), func(time.Time) tea.Msg { return JobLingerMsg{At: next} })
}

// fired clears the timer in flight when its message arrives.
func (t *jobTicker) fired(msg JobLingerMsg) {
	if msg.At.Equal(t.tickAt) {
		t.tickAt = time.Time{}
	}
}

type tickerJob struct {
	JobInfo
	at time.Time
}

// tickerShows reports whether a job belongs on the ticker: an application
// verb the definition did not mark quiet (Operation.Quiet). Framework jobs
// (a terminal's stream read) never show.
func tickerShows(r *Runtime, j JobInfo) bool {
	op, ok := r.Operations[j.Verb]
	return ok && op.Kind == KindApplication && !op.Quiet
}

// observe remembers the jobs that finished since the last frame, stamped
// now, and forgets the ones past JobLinger.
func (t *jobTicker) observe(r *Runtime, now time.Time) {
	if t.seen == nil {
		t.seen = map[uint64]bool{}
	}
	live := map[uint64]bool{}
	for _, j := range r.Jobs() {
		live[j.Job] = true
		if j.State == JobRunning || t.seen[j.Job] || !tickerShows(r, j) {
			continue
		}
		t.seen[j.Job] = true
		t.finished = slices.Insert(t.finished, 0, tickerJob{JobInfo: j, at: now})
	}
	t.finished = slices.DeleteFunc(t.finished, func(j tickerJob) bool { return now.Sub(j.at) >= JobLinger })
	for id := range t.seen {
		if !live[id] && !slices.ContainsFunc(t.finished, func(j tickerJob) bool { return j.Job == id }) {
			delete(t.seen, id)
		}
	}
}

// rows are the ticker's jobs, top row first: the running jobs newest
// first, then the remembered finished jobs newest finish first, at most
// JobTickerRows. A running job stays on top; when it finishes it falls
// below the running ones (Tetris), and a newer job pushes the bottom row
// off. A job finished since the last observe counts as finished now.
func (t *jobTicker) rows(r *Runtime, now time.Time) []JobInfo {
	var running, fresh []JobInfo
	for _, j := range r.Jobs() {
		if !tickerShows(r, j) {
			continue
		}
		switch {
		case j.State == JobRunning:
			running = slices.Insert(running, 0, j)
		case !t.seen[j.Job]:
			fresh = slices.Insert(fresh, 0, j)
		}
	}
	out := append(running, fresh...)
	for _, j := range t.finished {
		if now.Sub(j.at) < JobLinger {
			out = append(out, j.JobInfo)
		}
	}
	return out[:min(len(out), JobTickerRows)]
}

// JobsTicker renders ticker rows: one message per row, "noun.verb state",
// with ": error" on a failure, truncated to width.
func JobsTicker(jobs []JobInfo, width int) []string {
	lines := make([]string, 0, len(jobs))
	for _, j := range jobs {
		line := j.Verb + " " + j.State
		if j.Error != "" {
			line += ": " + j.Error
		}
		lines = append(lines, ansi.Truncate(safeLine(line), max(0, width), "…"))
	}
	return lines
}

// safeLine is safeText on one row: newlines become spaces.
func safeLine(s string) string {
	out := []rune(safeText(s))
	for i, c := range out {
		if c == '\n' {
			out[i] = ' '
		}
	}
	return string(out)
}

// JobsTicker is the ticker the shell draws above the footer for the
// current frame, as plain lines at most width cells: zero rows when no
// job shows.
func (m *Model) JobsTicker(width int) []string {
	return JobsTicker(m.ticker.rows(m.Runtime, m.clock()), width)
}

// JobLingerTick is the linger timer in flight, the message its tea.Tick
// will deliver at its deadline (At), and false when none is scheduled. A
// host with a fake Now advances the clock to At and delivers the message
// itself rather than waiting JobLinger.
func (m *Model) JobLingerTick() (JobLingerMsg, bool) {
	if m.ticker.tickAt.IsZero() {
		return JobLingerMsg{}, false
	}
	return JobLingerMsg{At: m.ticker.tickAt}, true
}

func (m *Model) clock() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}
