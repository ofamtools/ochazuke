package ochazuke

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const DefaultBindingTimeout = 400 * time.Millisecond

// BindingTimeoutMsg resolves a pending key sequence on the owning event loop.
// Messages from an older sequence or another runtime are ignored.
type BindingTimeoutMsg struct {
	runtime *Runtime
	version uint64
}

// CancelPendingBinding leaves a key-sequence prefix mode without running its
// completed match and invalidates its timer.
func (r *Runtime) CancelPendingBinding() {
	if r.prefix.focus != "" {
		if inst := r.Instances[r.prefix.focus]; inst != nil && inst.State == r.prefix.mode {
			r.leaveMode(inst)
		}
	}
	r.Pending = ""
	r.bindingVersion++
	r.prefix = pendingPrefix{}
}

// BindingTimeoutCommand schedules the latest deadline once. Custom Tea models
// call it after dispatch and send BindingTimeoutMsg to HandleBindingTimeout.
func (r *Runtime) BindingTimeoutCommand() tea.Cmd {
	if r.Pending == "" {
		return nil
	}
	if !r.prefixStillPending() {
		r.CancelPendingBinding()
		return nil
	}
	if r.prefix.scheduled {
		return nil
	}
	r.prefix.scheduled = true
	deadline, version := r.prefix.deadline, r.bindingVersion
	return func() tea.Msg {
		time.Sleep(time.Until(deadline))
		return BindingTimeoutMsg{runtime: r, version: version}
	}
}

// HandleBindingTimeout executes the longest completed match, or clears an
// incomplete prefix. Its result is false for a stale or cancelled timer.
func (r *Runtime) HandleBindingTimeout(msg BindingTimeoutMsg) (bool, error) {
	if msg.runtime != r || msg.version != r.bindingVersion || r.Pending == "" {
		return false, nil
	}
	if !r.prefixStillPending() {
		r.CancelPendingBinding()
		return false, nil
	}
	if time.Now().Before(r.prefix.deadline) {
		return false, nil
	}
	_, err := r.finishPrefix()
	return true, err
}
