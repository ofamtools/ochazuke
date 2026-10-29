package ochazuke

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// BindingTimeoutMsg resolves a pending key sequence on the owning event loop.
// Messages from an older sequence or another runtime are ignored.
type BindingTimeoutMsg struct {
	runtime *Runtime
	version uint64
	due     bool // BindingTimeoutTick: the host's fake clock passed the deadline
}

// CancelPendingBinding leaves a key-sequence prefix mode without running its
// completed match and invalidates its timer.
func (r *Runtime) CancelPendingBinding() {
	if r.prefix.focus != "" && r.Mode.Name == r.prefix.mode {
		r.leaveMode()
	}
	r.Pending = ""
	r.bindingVersion++
	r.prefix = pendingPrefix{}
}

// BindingTimeoutCommand schedules the latest deadline once. Custom Tea models
// call it after dispatch and send BindingTimeoutMsg to HandleBindingTimeout.
// A prefix without a deadline (sequence-timeout 0, the default) schedules
// nothing.
func (r *Runtime) BindingTimeoutCommand() tea.Cmd {
	if r.Pending == "" {
		return nil
	}
	if !r.prefixStillPending() {
		r.CancelPendingBinding()
		return nil
	}
	if r.prefix.scheduled || r.prefix.forever {
		return nil
	}
	r.prefix.scheduled = true
	deadline, version := r.prefix.deadline, r.bindingVersion
	return func() tea.Msg {
		time.Sleep(time.Until(deadline))
		return BindingTimeoutMsg{runtime: r, version: version}
	}
}

// BindingTimeoutTick is the sequence timer in flight as the message its
// command delivers at the deadline, already due: a host with a fake clock
// (explore) delivers it at once instead of waiting sequence-timeout, as
// Model.JobLingerTick does for the linger timer. False when no sequence
// waits on a deadline (none pending, or sequence-timeout 0).
func (r *Runtime) BindingTimeoutTick() (BindingTimeoutMsg, bool) {
	if !r.prefixStillPending() || r.prefix.forever {
		return BindingTimeoutMsg{}, false
	}
	return BindingTimeoutMsg{runtime: r, version: r.bindingVersion, due: true}, true
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
	if r.prefix.forever || !msg.due && time.Now().Before(r.prefix.deadline) {
		return false, nil
	}
	_, err := r.finishPrefix()
	return true, err
}

// guardedOff reports whether a row's guard (Binding.When) is false at the
// focus, so the row would not answer there: ? help leaves it out, as the
// footer's hints do (bindings list keeps it, as the guard may hold for the
// key).
func (r *Runtime) guardedOff(b Binding, focus string) bool {
	if b.When == "" {
		return false
	}
	check := r.guard(b.When)
	return check == nil || !check(r, KeyEvent{Mode: r.Mode, Focus: focus})
}

// StopBindings is one entry of bindings.stops: the rows a focus stop
// answers to (ListBindings at Focus in Mode), with the page it sits on.
// Stop is the stop instance, Focus where a jump to it lands (its
// remembered child, a list's selected row), Mode the mode the focus has
// there. The last entry is app itself (Stop and Focus "app").
type StopBindings struct {
	Page     string    `json:"page,omitempty"`
	Stop     string    `json:"stop"`
	Focus    string    `json:"focus"`
	Mode     string    `json:"mode"`
	Bindings []Binding `json:"bindings"`
}

// StopBindings is the app-wide keyboard: the effective rows of every focus
// stop, as ListBindings gives them for the focus a jump to the stop lands
// on. The order is fixed: the pages in tab order (PageList), each page's
// stops in drawing order (the order focus.next walks, undrawn stops after
// in tree order), then the open modals bottom to top with theirs, then app
// in normal mode. The mode is the one the focus would have there: the
// current mode at the current focus, else the stop's pinned mode (a prompt
// is insert, a terminal terminal), else normal. Nothing moves: the focus
// and mode are back as they were on return.
//
// It differs from bindings.list --all, which exports every stored row
// (each noun's and instance's tables, every mode, as configuration JSON),
// active or not: this answers "what does each key do where", after
// shadowing, guards and the modal boundary.
func (r *Runtime) StopBindings() []StopBindings {
	saved := r.Mode
	defer func() { r.Mode = saved }()
	current := r.stopOf(r.Focus)
	out := []StopBindings{}
	add := func(page, stop string) {
		focus, mode := r.landing(stop), ModeState{Name: r.pinnedMode(r.landing(stop))}
		if stop == current {
			focus, mode = r.Focus, saved
		}
		r.Mode = mode
		out = append(out, StopBindings{Page: page, Stop: stop, Focus: focus, Mode: mode.Name, Bindings: r.ListBindings(focus)})
	}
	for _, page := range r.PageList() {
		stops, _ := r.units(page.ID, r.focusable)
		for _, stop := range stops {
			add(page.ID, stop)
		}
	}
	for _, modal := range r.modals {
		stops, _ := r.units(modal, r.focusable)
		if len(stops) == 0 {
			stops = []string{modal}
		}
		for _, stop := range stops {
			add("", stop)
		}
	}
	r.Mode = ModeState{Name: "normal"}
	out = append(out, StopBindings{Stop: "app", Focus: "app", Mode: "normal", Bindings: r.ListBindings("app")})
	return out
}

// pinnedMode is the mode a focus on id starts in: its input's or noun's
// pinned mode, else normal (focusMoved's rule for a move between stops).
func (r *Runtime) pinnedMode(id string) string {
	if in := r.Input(id); in != nil && in.pin != "" {
		return in.pin
	}
	if inst := r.Instances[id]; inst != nil {
		if noun := r.Nouns[inst.Noun]; noun != nil && noun.Pin != "" {
			return noun.Pin
		}
	}
	return "normal"
}
