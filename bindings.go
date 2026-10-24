package ochazuke

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const DefaultBindingTimeout = 400 * time.Millisecond

type pendingBinding struct {
	deadline    time.Time
	scheduled   bool
	context     Context
	frame       *Frame
	mode        string
	fallback    Request
	hasFallback bool
}

// BindingTimeoutMsg resolves a pending sequence on the owning event loop.
// Messages from an older sequence or another runtime are ignored.
type BindingTimeoutMsg struct {
	runtime *Runtime
	version uint64
}

// CancelPendingBinding discards the sequence and its deferred action.
func (r *Runtime) CancelPendingBinding() {
	r.Pending = ""
	r.bindingVersion++
	r.pendingBinding = pendingBinding{}
}

func (r *Runtime) bindingContextMatches() bool {
	frame := r.Frames[len(r.Frames)-1]
	pending := &r.pendingBinding
	return frame == pending.frame && frame.Context.InputID == pending.context.InputID &&
		slices.Equal(frame.Context.Path, pending.context.Path) && r.Mode(frame.Context) == pending.mode
}

func (r *Runtime) finishPendingBinding() (bool, error) {
	pending := r.pendingBinding
	r.CancelPendingBinding()
	if !pending.hasFallback {
		return false, nil
	}
	_, err := r.ExecuteAt(pending.context, pending.fallback)
	return true, err
}

// BindingTimeoutCommand schedules the latest deadline once. Custom Tea models
// call it after dispatch and send BindingTimeoutMsg to HandleBindingTimeout.
func (r *Runtime) BindingTimeoutCommand() tea.Cmd {
	if r.Pending == "" {
		return nil
	}
	if !r.bindingContextMatches() {
		r.CancelPendingBinding()
		return nil
	}
	if r.pendingBinding.scheduled {
		return nil
	}
	r.pendingBinding.scheduled = true
	deadline, version := r.pendingBinding.deadline, r.bindingVersion
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
	if !r.bindingContextMatches() {
		r.CancelPendingBinding()
		return false, nil
	}
	if time.Now().Before(r.pendingBinding.deadline) {
		return false, nil
	}
	_, err := r.finishPendingBinding()
	return true, err
}

// HandleBinding waits when a binding is also a prefix. A timeout or mismatch
// runs the longest completed match. A mismatch is consumed when that match exists.
func (r *Runtime) HandleBinding(key string) (bool, error) {
	if r.Pending != "" {
		if !r.bindingContextMatches() {
			r.CancelPendingBinding()
		} else if !time.Now().Before(r.pendingBinding.deadline) {
			if _, err := r.finishPendingBinding(); err != nil {
				return true, err
			}
		}
	}
	if key == "esc" && r.Pending != "" {
		r.CancelPendingBinding()
		return true, nil
	}
	sequence := key
	if r.Pending != "" {
		sequence = r.Pending + " " + key
	}
	var exact Request
	matched, longer := false, false
	for _, binding := range r.EffectiveBindings(r.Context()) {
		if binding.Keys == sequence {
			exact, matched = binding.Request, true
		} else if strings.HasPrefix(binding.Keys, sequence+" ") {
			longer = true
		}
	}
	if longer {
		if r.Pending == "" {
			r.pendingBinding.context = r.Context()
			r.pendingBinding.frame = r.Frames[len(r.Frames)-1]
			r.pendingBinding.mode = r.Mode(r.pendingBinding.context)
		}
		if matched {
			r.pendingBinding.fallback, r.pendingBinding.hasFallback = exact, true
		}
		timeout := r.BindingTimeout
		if timeout <= 0 {
			timeout = DefaultBindingTimeout
		}
		r.Pending = sequence
		r.bindingVersion++
		r.pendingBinding.deadline = time.Now().Add(timeout)
		r.pendingBinding.scheduled = false
		return true, nil
	}
	if matched {
		r.CancelPendingBinding()
		_, err := r.Execute(exact)
		return true, err
	}
	if r.Pending != "" {
		if handled, err := r.finishPendingBinding(); handled || err != nil {
			return handled, err
		}
		return r.HandleBinding(key)
	}
	return false, nil
}
