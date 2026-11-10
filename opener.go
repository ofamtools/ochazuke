package ochazuke

import (
	tea "charm.land/bubbletea/v2"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Opener is the one place the framework hands a file, a folder or a URL to
// the operating system: Open launches the default application (macOS
// open, Linux xdg-open, Windows rundll32) and Reveal shows a path in the
// file manager (macOS open -R, elsewhere the containing folder). Nothing
// goes through a shell, and the target follows "--" where the tool takes
// it, so text that looks like a flag is never one.
//
// Start is the injectable runner: it launches the command without
// waiting. A test sets it (or SetOpener on a Runtime) to a fake and
// reads what would have run; tests never launch an application.
type Opener struct {
	// GOOS picks the command set; "" is runtime.GOOS.
	GOOS string
	// Start launches name with args and returns once it has started; nil
	// is the real runner (exec.Command, reaped in the background).
	Start func(name string, args ...string) error
}

// DefaultOpener is the opener on the real runner and the running OS.
func DefaultOpener() Opener { return Opener{} }

func (o Opener) goos() string {
	if o.GOOS != "" {
		return o.GOOS
	}
	return runtime.GOOS
}

// OpenCommand is the command that opens target with the system's default
// application.
func (o Opener) OpenCommand(target string) (name string, args []string) {
	switch o.goos() {
	case "darwin":
		return "open", []string{"--", target}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", target}
	}
	return "xdg-open", []string{target}
}

// RevealCommand is the command that shows path in the file manager.
func (o Opener) RevealCommand(path string) (name string, args []string) {
	switch o.goos() {
	case "darwin":
		return "open", []string{"-R", "--", path}
	case "windows":
		return "explorer", []string{"/select," + path}
	}
	return "xdg-open", []string{filepath.Dir(path)}
}

// OpenWithCommand is the command that opens target with the named
// application: macOS open -a, elsewhere the application as a command.
func (o Opener) OpenWithCommand(app, target string) (name string, args []string) {
	if o.goos() == "darwin" {
		return "open", []string{"-a", app, "--", target}
	}
	return app, []string{target}
}

// OpenWith opens target with the named application.
func (o Opener) OpenWith(app, target string) error {
	name, args := o.OpenWithCommand(app, target)
	return o.start(name, args)
}

// Open opens target (a path or a URL) with the system's default application.
func (o Opener) Open(target string) error {
	name, args := o.OpenCommand(target)
	return o.start(name, args)
}

// Reveal shows path in the file manager.
func (o Opener) Reveal(path string) error {
	name, args := o.RevealCommand(path)
	return o.start(name, args)
}

func (o Opener) start(name string, args []string) error {
	if o.Start != nil {
		return o.Start(name, args...)
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// openerStateID is the instance that holds the runtime's opener.
const openerStateID = "__opener-state"

// openerState is the runtime's opener, kept as an instance's component like
// the About state (the Runtime struct holds no component fields).
type openerState struct{ o Opener }

func (*openerState) HandleMessage(tea.Msg) bool { return false }
func (*openerState) PendingCommand() tea.Cmd    { return nil }
func (*openerState) Close()                     {}

func (r *Runtime) openerState() *openerState {
	if inst := r.Instances[openerStateID]; inst != nil {
		if st, ok := inst.Component.(*openerState); ok {
			return st
		}
	}
	st := &openerState{}
	r.Instance(openerStateID, Kind("ochazuke.opener-state")).Component = st
	return st
}

// Opener is the runtime's opener: the one set by SetOpener, else the
// default.
func (r *Runtime) Opener() Opener { return r.openerState().o }

// SetOpener replaces the runtime's opener (a host or a test with a fake
// runner).
func (r *Runtime) SetOpener(o Opener) { r.openerState().o = o }
