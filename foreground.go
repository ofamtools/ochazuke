package ochazuke

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Foreground programs: a verb hands the terminal to another full-screen
// program (an editor, o-csv FILE) and the TUI resumes when it exits. The
// verb returns Foreground(cmd); the runtime admits it as a job of the verb
// (the ticker shows it while it runs) and queues a ForegroundMsg. The
// Model answers that message with tea.Exec and a stderr tee: the TUI
// suspends, the program has the terminal, and its exit is the job's
// completion. A host without a Model (the CLI shell, a test) delivers the
// message to Runtime.Deliver, which runs the program in place on the
// process's own standard streams. Either way the exit is told the user:
// ctx.Info on status 0, ctx.Error with the final stderr reason and exit
// otherwise. The foreground.hold setting controls the failed-exit pause.
// An actions.json command row with "foreground": true uses this path.

// Foreground is the Result that runs cmd in the foreground (see above).
// Its stdio left nil is the terminal's; Then and Label work as on Pending.
// The completion's value is a ForegroundExit.
func Foreground(cmd *exec.Cmd) Result {
	if cmd == nil {
		return Failed(errors.New("Foreground needs a command"))
	}
	return Result{fg: cmd}
}

// ForegroundExit is a foreground program's completion value: the program
// (its base name) and its exit status.
type ForegroundExit struct {
	Program string `json:"program"`
	Code    int    `json:"code"`
	Stderr  string `json:"stderr,omitempty"`
}

// ForegroundMsg asks the host to give the terminal to Cmd. The Model runs
// it through tea.Exec (Model.Foreground replaces that, for a story
// or a test); Runtime.Deliver runs it in place. Done turns the program's
// exit into the job's completion.
type ForegroundMsg struct {
	Ticket Ticket
	Cmd    *exec.Cmd
	stderr *stderrTail
	hold   bool
}

// Done is the completion a foreground program's exit (Cmd.Run's error)
// delivers.
func (m ForegroundMsg) Done(err error) tea.Msg {
	exit := ForegroundExit{Program: filepath.Base(m.Cmd.Path), Code: 0}
	if len(m.Cmd.Args) > 0 {
		exit.Program = filepath.Base(m.Cmd.Args[0])
	}
	exit.Stderr = m.stderr.line()
	if err != nil {
		exit.Code = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit.Code = ee.ExitCode()
			err = fmt.Errorf("%s exited with status %d", exit.Program, exit.Code)
		} else {
			err = fmt.Errorf("%s: %w", exit.Program, err)
		}
		if reason := m.stderr.line(); reason != "" {
			reason = strings.TrimPrefix(reason, exit.Program+": ")
			err = fmt.Errorf("%s: %s (exit %d)", exit.Program, reason, exit.Code)
		}
	}
	return Completion{Ticket: m.Ticket, Value: exit, Err: err}
}

// Run runs the program in place on the process's standard streams (those
// the command leaves nil) and returns its completion: the headless path.
func (m ForegroundMsg) Run() tea.Msg {
	if m.Cmd.Stdin == nil {
		m.Cmd.Stdin = os.Stdin
	}
	if m.Cmd.Stdout == nil {
		m.Cmd.Stdout = os.Stdout
	}
	if m.Cmd.Stderr == nil {
		m.Cmd.Stderr = os.Stderr
	}
	p := &foregroundProcess{msg: m}
	return m.Done(p.Run())
}

// launchForeground admits a Foreground result as a job and queues its
// ForegroundMsg; the job's completion tells the user how the program
// exited, then runs the result's Then.
func (r *Runtime) launchForeground(owner, verb string, res Result, c Ctx) (Ticket, error) {
	verb = r.CanonicalKey(verb)
	ticket, _, err := r.IssueAt(owner, res.scope, verb, res.slot, c)
	if err != nil {
		return Ticket{}, err
	}
	j := r.jobs[ticket.Job]
	j.Label = res.label
	if j.Label == "" && len(res.fg.Args) > 0 {
		j.Label = filepath.Base(res.fg.Args[0])
	}
	then := res.then
	j.then = func(c Ctx, value any, err error) error {
		if err != nil {
			c.Error(err.Error())
		} else if exit, ok := value.(ForegroundExit); ok {
			if exit.Stderr != "" {
				c.Info(exit.Program + ": " + strings.TrimPrefix(exit.Stderr, exit.Program+": "))
			} else {
				c.Info(exit.Program + " exited")
			}
		}
		if then != nil {
			then(c, value, err)
		}
		return nil
	}
	msg := ForegroundMsg{Ticket: ticket, Cmd: res.fg, stderr: &stderrTail{}, hold: r.Settings().String("foreground.hold") == "on-failure"}
	r.commands = append(r.commands, func() tea.Msg { return msg })
	return ticket, nil
}

// foregroundCmd is how the Model answers a ForegroundMsg: its Foreground
// hook, else tea.Exec with the foreground adapter.
func (m *Model) foregroundCmd(msg ForegroundMsg) tea.Cmd {
	if m.Foreground != nil {
		return m.Foreground(msg)
	}
	return tea.Exec(&foregroundProcess{msg: msg}, msg.Done)
}
