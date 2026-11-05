package ochazuke

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// QuitHookWait is how long quitting waits for the jobs app.quitting's
// hooks started (a sync that queues a commit) before it exits anyway.
const QuitHookWait = 2 * time.Second

// quitHookTimeoutMsg wakes the Model once QuitHookWait has passed.
type quitHookTimeoutMsg struct{}

const quitWaitMode = "quit-wait"

// quitHooks bounds both hook jobs and queued editor writes. A second quit
// cancels the drain; unfinished values use the same notice route as failures.
func (m *Model) quitHooks() (tea.Cmd, bool) {
	r := m.Runtime
	h := r.eventHub()
	var timeout tea.Cmd
	if !h.quitEmitted {
		h.quitEmitted = true
		h.quitDeadline = m.clock().Add(QuitHookWait)
		wait := max(time.Duration(0), r.Settings().Duration("quit.write-wait"))
		h.quitWriteDeadline = m.clock().Add(wait)
		r.emitQuitting()
		var timers []tea.Cmd
		if r.quitHooksRunning() {
			timers = append(timers, tea.Tick(QuitHookWait, func(time.Time) tea.Msg { return quitHookTimeoutMsg{} }))
		}
		if r.flushJobsRunning() && wait > 0 {
			timers = append(timers, tea.Tick(wait, func(time.Time) tea.Msg { return quitHookTimeoutMsg{} }))
		}
		timeout = tea.Batch(timers...)
	}
	if h.quitForced || !m.clock().Before(h.quitWriteDeadline) {
		reason := "quit write deadline reached; completion was not confirmed"
		if h.quitForced {
			reason = "quit forced; completion was not confirmed"
		}
		for _, j := range r.jobs {
			if j.flush && j.State == JobRunning {
				if j.abandon == nil {
					text := "Not saved: " + j.Verb + " " + j.Label + ": " + reason
					r.UI().Notify(text)
					m.quitReport = append(m.quitReport, text)
				}
				m.quitReport = append(m.quitReport, r.finish(j, JobCancelled, fmt.Errorf("%s", reason))...)
			}
		}
	}
	if h.quitForced {
		return nil, false
	}
	if !r.flushJobsRunning() && (!r.quitHooksRunning() || !m.clock().Before(h.quitDeadline)) {
		return nil, false
	}
	var labels []string
	for _, j := range r.jobs {
		if j.flush && j.State == JobRunning {
			labels = append(labels, j.Label)
		}
	}
	if len(labels) > 0 {
		r.UI().Info("Saving " + strings.Join(labels, "; ") + " — ctrl+c or q to leave now")
	}
	return tea.Batch(r.PendingCommands(), timeout), true
}

func (r *Runtime) flushJobsRunning() bool {
	for _, j := range r.jobs {
		if j.flush && j.State == JobRunning {
			return true
		}
	}
	return false
}

func (m *Model) quitCommand() tea.Cmd {
	if m.quitClosing {
		return nil
	}
	m.quitClosing = true
	if len(m.quitReport) == 0 {
		return tea.Quit
	}
	// Exec releases the alternate screen before printing. The notice remains
	// on the main screen after the program's final terminal teardown.
	return tea.Exec(&quitNotice{lines: m.quitReport}, func(error) tea.Msg { return tea.QuitMsg{} })
}

type quitNotice struct {
	lines  []string
	stderr io.Writer
}

func (*quitNotice) SetStdin(io.Reader)      {}
func (*quitNotice) SetStdout(io.Writer)     {}
func (n *quitNotice) SetStderr(w io.Writer) { n.stderr = w }
func (n *quitNotice) Run() error {
	for _, text := range n.lines {
		if _, err := fmt.Fprintln(n.stderr, "notice: "+text); err != nil {
			return err
		}
	}
	return nil
}
