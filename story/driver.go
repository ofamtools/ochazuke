package story

import (
	"errors"
	"fmt"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

// Tagged is a message from a session's command, tagged with the session's
// generation so a message of a session that was reset or left never
// reaches its successor (job ids restart with each runtime).
type Tagged struct {
	Gen int
	Msg tea.Msg
}

// Wrap tags every message cmd produces with gen; a batch is unwrapped by
// the receiver (Unbatch) and its parts wrapped again.
func Wrap(gen int, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg { return Tagged{Gen: gen, Msg: cmd()} }
}

// Unbatch turns a tagged batch into the batch of its wrapped parts.
func Unbatch(t Tagged) (tea.Cmd, bool) {
	batch, ok := t.Msg.(tea.BatchMsg)
	if !ok {
		return nil, false
	}
	var cmds []tea.Cmd
	for _, c := range batch {
		cmds = append(cmds, Wrap(t.Gen, c))
	}
	return tea.Batch(cmds...), true
}

// Classify names the event a message is, before it is delivered: a key, a
// paste, a current job's completion, a pane frame, a framework timer. Other
// messages (window size, colours) are not events.
func Classify(ss *Session, msg tea.Msg) *Event {
	switch v := msg.(type) {
	case tea.KeyPressMsg:
		return &Event{Key: v.String()}
	case tea.PasteMsg:
		return &Event{Paste: v.Content}
	case ochazuke.Completion:
		if !ss.R.Accepts(v.Ticket) {
			return nil
		}
		if ss.R.Terminal(v.Owner) != nil {
			return &Event{Tick: "terminal"}
		}
		for _, j := range ss.R.Jobs() {
			if j.Job == v.Job {
				state := ochazuke.JobDone
				if v.Err != nil {
					state = ochazuke.JobFailed
				}
				return &Event{Job: j.Verb, State: state}
			}
		}
	case ochazuke.JobLingerMsg:
		return &Event{Tick: "linger"}
	case ochazuke.BindingTimeoutMsg:
		return &Event{Tick: "binding"}
	}
	return nil
}

// Driver plays events against a session with no terminal: commands run on
// goroutines and their messages wait in a pool; a job or tick event takes
// the first matching message from the pool (waiting for it) and delivers
// it, so nothing reaches the model except through an event, in order.
type Driver struct {
	S    *Session
	Opt  Options
	Wait time.Duration // how long a job or tick event waits for its message
	Quit bool          // the script quit the story (q, ctrl+c)

	gen    int
	mu     sync.Mutex
	pool   []Tagged
	arrive chan struct{}
}

// NewDriver starts driving a built session.
func NewDriver(ss *Session, opt Options) *Driver {
	d := &Driver{S: ss, Opt: opt, Wait: 6 * time.Second, arrive: make(chan struct{}, 1), gen: 1}
	d.run(ss.R.PendingCommands())
	return d
}

// Close ends the session's fake goroutines.
func (d *Driver) Close() { d.S.Close() }

func (d *Driver) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	gen := d.gen
	go func() { d.push(Tagged{Gen: gen, Msg: cmd()}) }()
}

func (d *Driver) push(t Tagged) {
	if batch, ok := t.Msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				gen := t.Gen
				go func() { d.push(Tagged{Gen: gen, Msg: c()}) }()
			}
		}
		return
	}
	if t.Msg == nil {
		return
	}
	d.mu.Lock()
	d.pool = append(d.pool, t)
	d.mu.Unlock()
	select {
	case d.arrive <- struct{}{}:
	default:
	}
}

func (d *Driver) update(msg tea.Msg) {
	_, cmd := d.S.M.Update(msg)
	if d.S.R.Quit {
		d.Quit = true
		return
	}
	d.run(cmd)
}

// take delivers the first pooled message of this generation that match
// accepts, waiting up to Wait for one.
func (d *Driver) take(what string, match func(tea.Msg) bool) error {
	deadline := time.After(d.Wait)
	for {
		d.mu.Lock()
		for i, t := range d.pool {
			if t.Gen == d.gen && match(t.Msg) {
				d.pool = append(d.pool[:i:i], d.pool[i+1:]...)
				d.mu.Unlock()
				d.update(t.Msg)
				return nil
			}
		}
		d.mu.Unlock()
		select {
		case <-d.arrive:
		case <-deadline:
			return fmt.Errorf("no %s arrived within %s", what, d.Wait)
		}
	}
}

// Step plays one event.
func (d *Driver) Step(e Event) error {
	if d.Quit {
		return nil
	}
	switch {
	case e.Key != "":
		d.update(keytest.Msg(e.Key, false, false))
	case e.Type != "":
		for _, r := range e.Type {
			if d.Quit {
				break
			}
			d.update(keytest.Msg(string(r), true, false))
		}
	case e.Paste != "":
		d.update(keytest.Msg(e.Paste, false, true))
	case e.Job != "":
		err := d.take("completion of "+e.Job, func(msg tea.Msg) bool {
			ev := Classify(d.S, msg)
			return ev != nil && ev.Job == e.Job
		})
		if err != nil {
			return err
		}
	case e.Tick == "output":
		if !d.S.Emit() {
			return errors.New("tick output: no fake pane is live")
		}
	case e.Tick == "terminal" || e.Tick == "linger" || e.Tick == "binding":
		if err := d.take(e.Tick+" tick", func(msg tea.Msg) bool {
			ev := Classify(d.S, msg)
			return ev != nil && ev.Tick == e.Tick
		}); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown event %s", e)
	}
	if d.S.Reset {
		return d.reset()
	}
	return nil
}

// reset rebuilds the session from its story (demo.reset, r).
func (d *Driver) reset() error {
	d.S.Close()
	ss, err := Build(d.S.Story, d.Opt)
	if err != nil {
		return err
	}
	d.S = ss
	d.gen++
	d.run(ss.R.PendingCommands())
	return nil
}

// Play runs events in order, calling after (when set) with the count of
// events played so far after each one.
func (d *Driver) Play(events []Event, after func(n int) error) error {
	for i, e := range events {
		if err := d.Step(e); err != nil {
			return fmt.Errorf("event %d (%s): %w", i+1, e, err)
		}
		if after != nil {
			if err := after(i + 1); err != nil {
				return err
			}
		}
	}
	return nil
}
