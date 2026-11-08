package cli

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke"
)

// pump drives a runtime's commands as Bubble Tea does: each command runs
// on its own goroutine and its message is delivered on the test's, so one
// command (a hung write) never holds up another. Settle runs commands one
// after another, which a test that leaves a write hung cannot use.
type pump struct {
	t    *testing.T
	r    *ochazuke.Runtime
	msgs chan tea.Msg
	live int
}

func newPump(t *testing.T, r *ochazuke.Runtime) *pump {
	return &pump{t: t, r: r, msgs: make(chan tea.Msg, 64)}
}

func (p *pump) start(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	p.live++
	go func() { p.msgs <- cmd() }()
}

// until delivers messages until cond holds.
func (p *pump) until(what string, cond func() bool) {
	p.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		p.start(p.r.PendingCommands())
		if cond() {
			return
		}
		select {
		case msg := <-p.msgs:
			p.live--
			switch m := msg.(type) {
			case nil:
			case tea.BatchMsg:
				for _, c := range m {
					p.start(c)
				}
			default:
				_, _ = p.r.Deliver(msg)
			}
		case <-deadline:
			p.t.Fatalf("pump: %s never happened", what)
		}
	}
}

// open runs the edit verb for t1 and delivers its get's answer, with every
// other command (a hung write) left running.
func (p *pump) open() {
	p.t.Helper()
	if _, err := p.r.Execute(ochazuke.Request{Noun: "ext-parcel", Verb: "edit", Args: ochazuke.Arguments{"id": "t1"}}); err != nil {
		p.t.Fatal(err)
	}
	p.until("the edit form opening", func() bool { return p.r.RecordEditor("edit-form") != nil && !p.editLoading() })
}

// editLoading reports a running edit open job.
func (p *pump) editLoading() bool {
	for _, j := range p.r.Jobs() {
		if j.Status == ochazuke.JobRunning && j.Verb == "ext-parcel.edit" {
			return true
		}
	}
	return false
}

// idle reports no running job.
func (p *pump) idle() bool {
	for _, j := range p.r.Jobs() {
		if j.Status == ochazuke.JobRunning {
			return false
		}
	}
	return true
}
