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

// quitHooks is the Model's quit with the app.quitting event: the first
// time it emits the event (its hooks run now) and, while a job a hook
// started is still running and QuitHookWait has not passed, answers the
// commands that let the jobs finish and wait true, so the Model keeps
// running; every later message asks again. A hook never holds quitting
// for longer than QuitHookWait.
func (m *Model) quitHooks() (tea.Cmd, bool) {
	r := m.Runtime
	h := r.eventHub()
	if !h.quitEmitted {
		h.quitEmitted = true
		h.quitDeadline = m.clock().Add(QuitHookWait)
		r.emitQuitting()
		if !r.quitHooksRunning() {
			return nil, false
		}
		return tea.Batch(r.PendingCommands(), tea.Tick(QuitHookWait, func(time.Time) tea.Msg { return quitHookTimeoutMsg{} })), true
	}
	if !r.quitHooksRunning() || !m.clock().Before(h.quitDeadline) {
		return nil, false
	}
	return r.PendingCommands(), true
}
