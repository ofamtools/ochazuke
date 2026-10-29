package ochazuke

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// drain runs the queued commands and delivers their messages until none
// are left, as a headless host does.
func drain(t *testing.T, r *Runtime, deliver func(tea.Msg)) {
	t.Helper()
	for range 10 {
		cmd := r.PendingCommands()
		if cmd == nil {
			return
		}
		var run func(tea.Cmd)
		run = func(c tea.Cmd) {
			switch msg := c().(type) {
			case tea.BatchMsg:
				for _, c := range msg {
					if c != nil {
						run(c)
					}
				}
			case nil:
			default:
				deliver(msg)
			}
		}
		run(cmd)
	}
}

// A verb returning Foreground runs the program as a job; headless,
// Deliver runs it in place. Status 0 tells the user "PROGRAM exited";
// another status fails the job and tells "exited with status N".
func TestForegroundHeadlessReportsTheExit(t *testing.T) {
	r := NewRuntime()
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	var out bytes.Buffer
	var exits []ForegroundExit
	r.Define("doc").Verb("open", func(c Ctx) Result {
		cmd := exec.Command("sh", "-c", "echo hi; exit "+StringArg(c.Args, "code", "0"))
		cmd.Stdout = &out
		return Foreground(cmd).Then(func(_ Ctx, v any, _ error) { exits = append(exits, v.(ForegroundExit)) })
	})
	op := r.Operations["doc.open"]
	op.Parameters = map[string]Parameter{"code": {Type: "string"}}
	r.Operations["doc.open"] = op
	if _, err := r.Execute(Request{Noun: "doc", Verb: "open"}); err != nil {
		t.Fatal(err)
	}
	if jobs := r.Jobs(); len(jobs) != 1 || jobs[0].State != JobRunning || jobs[0].Label != "sh" {
		t.Fatalf("jobs %+v", jobs)
	}
	drain(t, r, func(msg tea.Msg) { _, _ = r.Deliver(msg) })
	if out.String() != "hi\n" || sink.LastInfo() != "sh exited" {
		t.Fatalf("out %q info %q", out.String(), sink.LastInfo())
	}
	if _, err := r.Execute(Request{Noun: "doc", Verb: "open", Args: Arguments{"code": "3"}}); err != nil {
		t.Fatal(err)
	}
	var deliverErr error
	drain(t, r, func(msg tea.Msg) {
		if _, err := r.Deliver(msg); err != nil {
			deliverErr = err
		}
	})
	if sink.LastError() != "sh exited with status 3" || deliverErr == nil || len(exits) != 2 || exits[1].Code != 3 {
		t.Fatalf("error %q deliver %v exits %+v", sink.LastError(), deliverErr, exits)
	}
	if jobs := r.Jobs(); jobs[len(jobs)-1].State != JobFailed {
		t.Fatalf("jobs %+v", jobs)
	}
}

// The Model answers a ForegroundMsg with its Foreground hook (else
// tea.ExecProcess) and does not run the program itself; the exit's
// completion reaches the job.
func TestForegroundModelSuspends(t *testing.T) {
	r := NewRuntime()
	m := NewModel(r)
	var asked []string
	m.Foreground = func(msg ForegroundMsg) tea.Cmd {
		asked = append(asked, strings.Join(msg.Cmd.Args, " "))
		return func() tea.Msg { return msg.Done(nil) }
	}
	r.Define("doc").Verb("open", func(Ctx) Result { return Foreground(exec.Command("o-csv", "export.csv")) })
	if _, err := r.Execute(Request{Noun: "doc", Verb: "open"}); err != nil {
		t.Fatal(err)
	}
	drain(t, r, func(msg tea.Msg) {
		_, cmd := m.Update(msg)
		if _, ok := msg.(ForegroundMsg); ok && cmd != nil {
			if c, ok := cmd().(Completion); ok {
				m.Update(c) // its own commands (the ticker's linger) are not run
			}
		}
	})
	if len(asked) != 1 || asked[0] != "o-csv export.csv" || m.Info != "o-csv exited" {
		t.Fatalf("asked %v info %q", asked, m.Info)
	}
	if m2 := NewModel(NewRuntime()); m2.foregroundCmd(ForegroundMsg{Cmd: exec.Command("true")}) == nil {
		t.Fatal("the default answer is tea.ExecProcess")
	}
}

// An actions.json command row with "foreground": true runs its command
// in the foreground; foreground refuses a timeout.
func TestActionsForegroundRow(t *testing.T) {
	r := NewRuntime()
	sink := &RecordingSink{}
	r.UI().Attach(sink)
	dir := t.TempDir()
	r.Config().Dir = dir
	marker := filepath.Join(dir, "ran")
	text := `[{"noun": "app", "verb": "open-csv", "command": ["sh", "-c", "touch ` + marker + `"], "foreground": true},
		{"noun": "app", "verb": "bad", "command": ["true"], "foreground": true, "timeout": "1s"}]`
	if err := os.WriteFile(filepath.Join(dir, ActionsFile), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	problems := r.EnsureUserActions()
	if len(problems) != 1 || !strings.Contains(problems[0], "foreground takes no timeout") {
		t.Fatalf("problems %q", problems)
	}
	if _, err := r.Execute(Request{Noun: "app", Verb: "open-csv"}); err != nil {
		t.Fatal(err)
	}
	drain(t, r, func(msg tea.Msg) {
		if fg, ok := msg.(ForegroundMsg); ok {
			fg.Cmd.Stdin, fg.Cmd.Stdout, fg.Cmd.Stderr = strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}
		}
		_, _ = r.Deliver(msg)
	})
	if _, err := os.Stat(marker); err != nil || sink.LastInfo() != "sh exited" {
		t.Fatalf("ran: %v info %q", err, sink.LastInfo())
	}
}
