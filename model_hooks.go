package ochazuke

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// QuitHookWait is how long quitting waits for the jobs app.quitting's
// hooks started (a sync that queues a commit) before it exits anyway.
const QuitHookWait = 2 * time.Second

// quitHookTimeoutMsg wakes the Model once QuitHookWait has passed.
type quitHookTimeoutMsg struct{}

// quitHooks emits app.quitting once and runs the commands needed to finish
// pending work. Hook jobs keep their QuitHookWait deadline; queued editor
// writes are drained to completion, including writes queued by a finishing save.
func (m *Model) quitHooks() (tea.Cmd, bool) {
	r := m.Runtime
	h := r.eventHub()
	var timeout tea.Cmd
	if !h.quitEmitted {
		h.quitEmitted = true
		h.quitDeadline = m.clock().Add(QuitHookWait)
		r.emitQuitting()
		if r.quitHooksRunning() {
			timeout = tea.Tick(QuitHookWait, func(time.Time) tea.Msg { return quitHookTimeoutMsg{} })
		}
	}
	if !r.flushJobsRunning() && (!r.quitHooksRunning() || !m.clock().Before(h.quitDeadline)) {
		return nil, false
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
