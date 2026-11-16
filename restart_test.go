package ochazuke

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke/keytest"
)

// One ctrl+c per quit.confirm value: always asks and a second ctrl+c
// over the question quits; when-busy with nothing running and never quit
// at once.
func TestQuitConfirmSetting(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"always", "when-busy", "never"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			r := NewRuntime()
			m := NewModel(r)
			if _, err := r.Settings().SetSession(QuitConfirmSetting, value); err != nil {
				t.Fatal(err)
			}
			keytest.Press(m, "ctrl+c")
			if value != "always" {
				if !r.Quit || r.TopModal() != "" {
					t.Fatalf("%s: quit %v, modal %q", value, r.Quit, r.TopModal())
				}
				return
			}
			inst := r.Instances[r.TopModal()]
			if r.Quit || inst == nil || !strings.HasPrefix(inst.Payload.Prompt, "Quit ") {
				t.Fatalf("always: quit %v, modal %v", r.Quit, inst)
			}
			keytest.Press(m, "ctrl+c")
			if !r.Quit || r.TopModal() != "" {
				t.Fatalf("second ctrl+c: quit %v, modal %q", r.Quit, r.TopModal())
			}
		})
	}
}

// A request records who asked, shows a notice and the badge at the left of
// the title row; a second request updates it; restart.badge off hides the
// badge only; dismiss clears everything.
func TestRestartRequestBadge(t *testing.T) {
	t.Parallel()
	r := NewRuntime()
	m := NewModel(r)
	m.Title = "Demo"
	m.Width, m.Height = 80, 12
	mustRun(t, r, Request{Noun: "ochazuke.restart-request", Verb: "post", Args: Arguments{"reason": "new build", "version": "v2"}})
	mustRun(t, r, Request{Noun: "ochazuke.restart-request", Verb: "post", Args: Arguments{"reason": "again"}})
	req := r.RestartRequested()
	if !req.Pending || req.Count != 2 || req.Reason != "again" || req.Version != "v2" || req.Actor.String() == "" {
		t.Fatalf("request %+v", req)
	}
	if len(m.Notices) != 1 || !strings.Contains(m.Notices[0], "again") || !strings.Contains(m.Notices[0], "& restarts") {
		t.Fatalf("notices %q", m.Notices)
	}
	top := titleLine(m.View().Content)
	if !strings.Contains(top, RestartBadgeText) || !strings.Contains(top, "Demo") || strings.Index(top, RestartBadgeText) > strings.Index(top, "Demo") {
		t.Fatalf("title row %q", top)
	}
	// Narrow, the badge keeps at most half the row: its short label.
	m.Update(tea.WindowSizeMsg{Width: 36, Height: 12})
	if top := titleLine(m.View().Content); !strings.Contains(top, "RESTART") || strings.Contains(top, "REQUESTED") || !strings.Contains(top, "NORMAL") {
		t.Fatalf("narrow title row %q", top)
	}
	if _, err := r.Settings().SetSession(RestartBadgeSetting, "false"); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 81, Height: 12}) // a new frame
	if top := titleLine(m.View().Content); strings.Contains(top, "RESTART") {
		t.Fatalf("badge off: %q", top)
	}
	mustRun(t, r, Request{Noun: "ochazuke.restart-request", Verb: "dismiss"})
	if r.RestartRequested().Pending || len(m.Notices) != 0 {
		t.Fatalf("dismissed: %+v %q", r.RestartRequested(), m.Notices)
	}
}

func titleLine(view string) string {
	line, _, _ := strings.Cut(view, "\n")
	return ansi.Strip(line)
}

// The restart question names the running and installed builds: checking
// at first, then the probe's answer (the same build says so; a failing
// probe reads unknown); without a path it asks to quit.
func TestRestartConfirmVersions(t *testing.T) {
	t.Parallel()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, installed, want string
		fail, noPath          bool
	}{
		{name: "same", installed: "demo v1", want: "Already running the installed build."},
		{name: "different", installed: "demo v2", want: "Installed: demo v2"},
		{name: "unknown", fail: true, want: "Installed: unknown (no answer)"},
		{name: "no-path", noPath: true, want: "Quit demo? It cannot restart itself here"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := NewRuntime()
			m := NewModel(r)
			m.Width, m.Height = 100, 30
			h := RestartHook{Name: "demo", Running: func() string { return "demo v1" }, Exec: func(string, []string, []string) error { return nil },
				Installed: func(context.Context, string) (string, error) {
					if tc.fail {
						return "", errors.New("no answer")
					}
					return tc.installed, nil
				}}
			if !tc.noPath {
				h.Path = func() string { return self }
			}
			r.SetRestart(h)
			mustRun(t, r, Request{Noun: "app", Verb: "restart"})
			inst := r.Instances[r.TopModal()]
			if inst == nil {
				t.Fatal("no confirm opened")
			}
			if !tc.noPath && (!strings.HasPrefix(inst.Payload.Prompt, "Restart demo in this terminal?") || !strings.Contains(inst.Payload.Prompt, "Installed: checking…")) {
				t.Fatalf("before the probe: %q", inst.Payload.Prompt)
			}
			_ = m.View()
			runModelCommand(m, r.PendingCommands())
			_ = m.View() // the frame the completion's throttled redraw skips
			if view := ansi.Strip(m.View().Content); !strings.Contains(view, tc.want) {
				t.Fatalf("the open dialog does not show %q:\n%s", tc.want, view)
			}
			if !strings.Contains(inst.Payload.Prompt, tc.want) || !strings.Contains(inst.Payload.Prompt, "Running:   demo v1") {
				t.Fatalf("prompt %q, want %q", inst.Payload.Prompt, tc.want)
			}
		})
	}
}

// Yes arms the restart and quits; the restart runs the application's
// cleanup, shuts the runtime down, then execs the installed binary with the
// arguments given; an exec that fails is printed and kept.
func TestRestartShutsDownThenExecs(t *testing.T) {
	t.Parallel()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := NewRuntime()
	m := NewModel(r)
	var order []string
	execErr := error(nil)
	r.SetRestart(RestartHook{
		Path:       func() string { return self },
		Installed:  func(context.Context, string) (string, error) { return "x", nil },
		Args:       func() []string { return []string{"demo", "--flag"} },
		BeforeExec: func() error { order = append(order, "cleanup"); return nil },
		Exec: func(path string, argv, env []string) error {
			order = append(order, "exec shut="+boolText(r.ShuttingDown())+" "+path+" "+strings.Join(argv, " "))
			return execErr
		},
	})
	mustRun(t, r, Request{Noun: "app", Verb: "restart"})
	keytest.Press(m, "y")
	if !r.Quit || !r.restart().armed {
		t.Fatalf("yes: quit %v armed %v", r.Quit, r.restart().armed)
	}
	var stderr bytes.Buffer
	p := &restartProcess{r: r, stderr: &stderr}
	if err := p.Run(); err != nil {
		t.Fatal(err)
	}
	want := []string{"cleanup", "exec shut=true " + self + " demo --flag"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("order %q", order)
	}
	execErr = errors.New("denied")
	if err := p.Run(); err == nil || r.RestartError() == nil || !strings.Contains(stderr.String(), "restart failed: start "+self+": denied") {
		t.Fatalf("failed exec: %v %v %q", err, r.RestartError(), stderr.String())
	}
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// runModelCommand delivers a command's messages through the Model, as the
// program would.
func runModelCommand(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, next := range batch {
			runModelCommand(m, next)
		}
		return
	}
	m.Update(msg)
}
