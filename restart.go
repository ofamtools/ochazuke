package ochazuke

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Restarting in place. An agent that installed a new build posts a restart
// request (restart-request.post {reason, version}): the runtime keeps it in
// memory, tells the person with a notice and draws a RESTART REQUESTED
// badge at the left of the title row (Runtime.LeadCells, setting
// restart.badge). app.restart asks first, naming the running build, the
// installed one and the request; yes quits through the ordinary quit path
// (queued writes, app.quitting hooks) and, once the terminal is restored,
// replaces the process with the installed binary, same arguments and
// environment. What the application knows (the binary's path, how to ask
// it its version, its own cleanup) it gives through one hook,
// Runtime.SetRestart(RestartHook). Without a path or a way to exec (no
// hook, or a platform with no exec) the same verb asks to quit and quits:
// one code path, the capability is data.

const (
	// QuitConfirmSetting is when a quit verb asks first: always,
	// when-busy (only while jobs run) or never.
	QuitConfirmSetting = "quit.confirm"
	// RestartBadgeSetting draws the RESTART REQUESTED badge.
	RestartBadgeSetting = "restart.badge"
	// RestartProbeTimeoutSetting bounds the installed-version probe.
	RestartProbeTimeoutSetting = "restart.probe-timeout"

	// RestartBadgeText is the badge's label.
	RestartBadgeText = "RESTART REQUESTED"
	// restartBadgeShort is the label when the row is too narrow for it.
	restartBadgeShort = "RESTART"

	restartStateID = "__restart-state"

	installedChecking = "checking…"
)

// RestartHook is what an application gives the runtime so app.restart can
// start the installed build in place of the running one. Every field is
// optional.
type RestartHook struct {
	// Name is the program's name in the dialog; "" is the base name of
	// os.Args[0].
	Name string
	// Path is the binary to start (the installed build); nil or "" means
	// this program cannot restart itself and app.restart quits instead.
	Path func() string
	// Running is the running build as its --version prints it; nil is
	// read from the build info (RunningVersion).
	Running func() string
	// Installed asks the binary at path which build it is; nil runs
	// ProbeVersion (path --version, its first line). It runs off the loop
	// under restart.probe-timeout, so a slow or failing probe never holds
	// the dialog: it reads "checking…", then the answer or "unknown".
	Installed func(ctx context.Context, path string) (string, error)
	// Args are the arguments to start it with, argv[0] first; nil is
	// os.Args.
	Args func() []string
	// Env is its environment; nil is os.Environ().
	Env func() []string
	// BeforeExec runs once the terminal is restored and before the
	// runtime shuts down: the application's own cleanup (its daemon
	// client, extension processes). An error stops the restart and the
	// program exits with it printed.
	BeforeExec func() error
	// Exec replaces the process; nil is the platform's (syscall.Exec on
	// unix, none elsewhere, where app.restart quits instead). Tests
	// inject one.
	Exec func(path string, argv, env []string) error
}

// RestartRequest is a pending request to restart: who asked, when, why and
// what was installed. Count is how many requests it gathers (a second
// request while one is pending updates it).
type RestartRequest struct {
	Pending bool      `json:"pending"`
	Reason  string    `json:"reason,omitempty"`
	Version string    `json:"version,omitempty"`
	Actor   Actor     `json:"actor"`
	At      time.Time `json:"at"`
	Count   int       `json:"count"`
}

// restartState is the runtime's restart state: the hook, the pending
// request, its notice, the last probe and whether a confirmed restart
// waits for the quit to finish.
type restartState struct {
	hook      RestartHook
	request   RestartRequest
	notice    NoticeID
	installed string // the last probe's answer, "" before one
	armed     bool
	err       error
}

func (s *restartState) HandleMessage(tea.Msg) bool { return false }
func (s *restartState) PendingCommand() tea.Cmd    { return nil }
func (s *restartState) Close()                     {}

func (r *Runtime) restart() *restartState {
	if inst := r.Instances[restartStateID]; inst != nil {
		if s, ok := inst.Component.(*restartState); ok {
			return s
		}
	}
	s := &restartState{}
	inst := r.Instance(restartStateID, Kind("ochazuke.restart-state"))
	inst.Component = s
	return s
}

// SetRestart gives the runtime the application's restart hook.
func (r *Runtime) SetRestart(h RestartHook) { r.restart().hook = h }

// RestartRequested is the pending restart request, Pending false when
// there is none.
func (r *Runtime) RestartRequested() RestartRequest { return r.restart().request }

// RestartError is why the last confirmed restart did not start the
// installed binary (the program then exited), nil when none failed.
func (r *Runtime) RestartError() error { return r.restart().err }

// LeadCell is a badge drawn at the left of the title row, before the
// title (Header.Lead): Text, or Short when the row is too narrow for Text.
type LeadCell struct {
	ID    string
	Text  string
	Short string
}

// LeadCells are the badges drawn at the left of the title row: the
// restart request while one is pending and restart.badge is on.
func (r *Runtime) LeadCells() []LeadCell {
	if !r.restart().request.Pending || !r.Settings().Bool(RestartBadgeSetting) {
		return nil
	}
	return []LeadCell{{ID: "restart", Text: RestartBadgeText, Short: restartBadgeShort}}
}

// programName is the program's name in the dialogs.
func (r *Runtime) programName() string {
	if n := r.restart().hook.Name; n != "" {
		return n
	}
	if len(os.Args) > 0 {
		return filepath.Base(os.Args[0])
	}
	return "the application"
}

// RunningVersion is the running build read from its build info: the main
// module's name and version and, when it is not ochazuke itself, the
// ochazuke version it was built with.
func RunningVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return runningVersionOf(bi)
}

func runningVersionOf(bi *debug.BuildInfo) string {
	const framework = "ofam.dev/ochazuke"
	out := filepath.Base(bi.Main.Path) + " " + bi.Main.Version
	if bi.Main.Path == framework {
		return out
	}
	for _, d := range bi.Deps {
		if d.Path == framework {
			return out + " (ochazuke " + d.Version + ")"
		}
	}
	return out
}

// ProbeVersion runs path --version and answers its first line: the
// default RestartHook.Installed.
func ProbeVersion(ctx context.Context, path string) (string, error) {
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line, nil
		}
	}
	return "", nil // no version printed: the dialog reads unknown
}

// restartPath is the binary app.restart would start and whether it can
// (a path that names an executable file and a way to exec); why says what
// is missing when it cannot.
func (r *Runtime) restartPath() (path string, ok bool, why string) {
	h := r.restart().hook
	if h.Path != nil {
		path = h.Path()
	}
	switch {
	case path == "":
		return "", false, "no installed binary is known"
	case h.Exec == nil && platformExec == nil:
		return path, false, "this platform cannot replace a running program"
	}
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return path, false, err.Error()
	case info.IsDir() || info.Mode()&0o111 == 0:
		return path, false, path + " is not an executable file"
	}
	return path, true, ""
}

func (r *Runtime) runningVersion() string {
	if f := r.restart().hook.Running; f != nil {
		return f()
	}
	return RunningVersion()
}

// restartPrompt is app.restart's question: restart (or quit, when the
// program cannot restart itself), the running and installed builds, and
// the pending request.
func (r *Runtime) restartPrompt(installed string) string {
	path, can, why := r.restartPath()
	name := r.programName()
	var b strings.Builder
	if can {
		fmt.Fprintf(&b, "Restart %s in this terminal?\n", name)
	} else {
		fmt.Fprintf(&b, "Quit %s? It cannot restart itself here (%s); start it again yourself.\n", name, why)
	}
	running := r.runningVersion()
	fmt.Fprintf(&b, "\nRunning:   %s\n", running)
	if path != "" {
		fmt.Fprintf(&b, "Installed: %s\n", installed)
		if installed == running {
			b.WriteString("Already running the installed build.\n")
		}
	}
	if req := r.restart().request; req.Pending {
		fmt.Fprintf(&b, "\nRequested by %s at %s", req.Actor.String(), req.At.Format("15:04"))
		if req.Count > 1 {
			fmt.Fprintf(&b, " (%d requests)", req.Count)
		}
		if req.Version != "" {
			fmt.Fprintf(&b, "\nNew version: %s", req.Version)
		}
		if req.Reason != "" {
			fmt.Fprintf(&b, "\nReason: %s", req.Reason)
		}
		b.WriteString("\n")
	}
	if v := r.quitChecksAsk(); v != "" {
		fmt.Fprintf(&b, "\n%s\n", v)
	}
	// A prompt's {{field}} is expanded by the confirm; text from a request
	// is never a field.
	return strings.TrimRight(strings.ReplaceAll(b.String(), "{{", "{ {"), "\n")
}

// quitChecksAsk is the question app.quit's own checks (BeforeQuit) would
// ask now, so the restart's one question carries it.
func (r *Runtime) quitChecksAsk() string {
	op, ok := r.Operation(AppNoun + ".quit")
	if !ok {
		return ""
	}
	return r.runChecks(r.contextOf(r.Focus), Request{Noun: AppNoun, Verb: "quit"}, op).Ask
}

// restartConfirm is app.restart's ConfirmFor: the prompt with the
// installed build checking, and the probe started off the loop; its answer
// replaces the line in the open confirm when it lands.
func restartConfirm(r *Runtime, c Context, q Request, _ string) string {
	path, _, _ := r.restartPath()
	if path == "" {
		return r.restartPrompt("")
	}
	r.probeInstalled(c, path)
	return r.restartPrompt(installedChecking)
}

// probeInstalled asks the installed binary its version off the loop and,
// on the loop, writes the answer into every open restart confirm.
func (r *Runtime) probeInstalled(c Context, path string) {
	s := r.restart()
	probe := s.hook.Installed
	if probe == nil {
		probe = ProbeVersion
	}
	limit := r.Settings().Duration(RestartProbeTimeoutSetting)
	work := func(ctx context.Context) (any, error) {
		if limit > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, limit)
			defer cancel()
		}
		return probe(ctx, path)
	}
	then := func(ctx Ctx, v any, err error) {
		text, _ := v.(string)
		if err != nil || text == "" {
			text = "unknown"
			if err != nil {
				text += " (" + err.Error() + ")"
			}
		}
		s.installed = text
		r.updateRestartConfirms(text)
	}
	if _, err := r.Start(restartStateID, work, then, Ctx{Runtime: r, Context: c}); err != nil {
		s.installed = "unknown (" + err.Error() + ")"
	}
}

// updateRestartConfirms rewrites the open restart confirms' prompts with
// the installed build now known.
func (r *Runtime) updateRestartConfirms(installed string) {
	for _, id := range r.modalIDs() {
		inst := r.Instances[id]
		if inst == nil || inst.Payload.Request == nil || inst.Payload.Request.Key() != AppNoun+".restart" {
			continue
		}
		prompt := inst.Payload.Prompt
		prompt = strings.Replace(prompt, "Installed: "+installedChecking, "Installed: "+installed, 1)
		if installed == r.runningVersion() && !strings.Contains(prompt, "Already running the installed build.") {
			prompt = strings.Replace(prompt, "Installed: "+installed, "Installed: "+installed+"\nAlready running the installed build.", 1)
		}
		inst.Payload.Prompt = prompt
	}
}

// postRestart records or updates the pending request and its notice.
func (r *Runtime) postRestart(reason, version string) RestartRequest {
	s := r.restart()
	req := s.request
	req.Pending = true
	req.Actor = r.Actor()
	req.At = time.Now()
	req.Count++
	if reason != "" {
		req.Reason = reason
	}
	if version != "" {
		req.Version = version
	}
	s.request = req
	text := "Restart requested by " + req.Actor.String()
	if req.Version != "" {
		text += ": " + req.Version
	}
	if req.Reason != "" {
		text += " (" + req.Reason + ")"
	}
	if keys := r.keysFor(AppNoun + ".restart"); keys != "" {
		text += "; " + keys + " restarts"
	}
	ui := r.UI()
	if s.notice != 0 {
		ui.ClearNotice(s.notice)
	}
	s.notice = ui.Notify(text)
	return req
}

// dismissRestart drops the pending request and its notice.
func (r *Runtime) dismissRestart() RestartRequest {
	s := r.restart()
	was := s.request
	s.request = RestartRequest{}
	if s.notice != 0 {
		r.UI().ClearNotice(s.notice)
		s.notice = 0
	}
	return was
}

// keysFor is the keys the first row at the focus that runs key is bound
// to, "" when none does.
func (r *Runtime) keysFor(key string) string {
	for _, b := range r.bindingRows(r.Focus) {
		if b.Request.Key() == key && b.Keys != "" {
			return b.Keys
		}
	}
	return ""
}

// restartCommand is what the Model does in place of tea.Quit once a
// confirmed restart's quit has finished: release the terminal and start
// the installed binary (restartProcess); nil when no restart is armed.
func (r *Runtime) restartCommand(report []string) tea.Cmd {
	s := r.restart()
	if !s.armed {
		return nil
	}
	p := &restartProcess{r: r, report: report, stderr: os.Stderr}
	return tea.Exec(p, func(error) tea.Msg { return tea.QuitMsg{} })
}

// restartProcess is the restart as a program bubbletea runs with the
// terminal released: report what quitting left unfinished, run the
// application's cleanup, flush and shut the runtime down, then exec.
type restartProcess struct {
	r      *Runtime
	report []string
	stderr io.Writer
}

func (*restartProcess) SetStdin(io.Reader)      {}
func (*restartProcess) SetStdout(io.Writer)     {}
func (p *restartProcess) SetStderr(w io.Writer) { p.stderr = w }

// Run replaces the process. It returns only when that failed: the error
// is printed, kept (Runtime.RestartError), and the program exits.
func (p *restartProcess) Run() error {
	path, err := p.r.execRestart(p.report, p.stderr)
	if err != nil {
		p.r.restart().err = err
		fmt.Fprintf(p.stderr, "restart failed: start %s: %v\n", path, err)
	}
	return err
}

// execRestart is the restart's sequence after the terminal is restored:
// the path it started (or tried to) and the error that stopped it. A
// binary gone since the question was asked is said on stderr and the
// program just quits.
func (r *Runtime) execRestart(report []string, stderr io.Writer) (string, error) {
	for _, line := range report {
		fmt.Fprintln(stderr, line)
	}
	s := r.restart()
	path, can, why := r.restartPath()
	if !can {
		fmt.Fprintln(stderr, "not restarted: "+why)
		return path, nil
	}
	if s.hook.BeforeExec != nil {
		if err := s.hook.BeforeExec(); err != nil {
			return path, err
		}
	}
	if c := r.Config(); c != nil {
		c.Flush()
	}
	r.Shutdown()
	argv, env := os.Args, os.Environ()
	if s.hook.Args != nil {
		argv = s.hook.Args()
	}
	if s.hook.Env != nil {
		env = s.hook.Env()
	}
	run := s.hook.Exec
	if run == nil {
		run = platformExec
	}
	return path, run(path, argv, env)
}

func registerRestart(r *Runtime) {
	r.restart()
	r.Register(Operation{Key: "ochazuke.restart-request.post", HistoryKind: HistoryChange, Agent: AgentAllow, Description: "Ask the person to restart the application (after installing a new build): a notice and a RESTART REQUESTED badge until they restart or dismiss it; a second request while one is pending updates it", Parameters: map[string]Parameter{
		"reason":  {Type: "string", Description: "Why, in a few words"},
		"version": {Type: "string", Description: "The build that was installed"},
	}, Positional: []string{"reason"}, Result: "restart-request", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.postRestart(StringArg(a, "reason", ""), StringArg(a, "version", "")), nil
	}})
	r.Register(Operation{Key: "ochazuke.restart-request.show", HistoryKind: HistoryFetch, Agent: AgentAllow, Description: "Answer the pending restart request (pending false when there is none)", Result: "restart-request", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.restart().request, nil
	}})
	r.Register(Operation{Key: "ochazuke.restart-request.dismiss", HistoryKind: HistoryChange, Agent: AgentAllow, Description: "Drop the pending restart request, its notice and its badge, without restarting", Result: "restart-request", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.dismissRestart(), nil
	}})
	r.Register(Operation{Key: AppNoun + ".restart", HistoryKind: HistoryAction, Agent: AgentConfirm, Confirm: "Restart?", ConfirmFor: restartConfirm, Loses: "the running session: its open views and what it holds in memory", Description: "Restart the application in this terminal: ask first (the running and installed builds, the pending request), then quit as app.quit does and start the installed binary with the same arguments; where it cannot restart itself it asks to quit and quits", Result: "boolean", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if _, can, why := r.restartPath(); can {
			r.restart().armed = true
		} else if r.restart().hook.Path != nil {
			r.UI().Info("quitting: " + why)
		}
		r.quit(false)
		return true, nil
	}})
}

// quit is app.quit's run: the first request starts quitting (the Model
// waits for queued writes and app.quitting's hooks); force, or a second
// request quit.force-after or more later, leaves without waiting.
func (r *Runtime) quit(force bool) {
	h := r.eventHub()
	now := time.Now()
	if force || r.Quit && now.Sub(h.quitRequested) >= r.Settings().Duration("quit.force-after") {
		h.quitForced = true
	}
	if !r.Quit {
		h.quitRequested = now
	}
	r.Quit = true
}

// quitConfirm is app.quit's ConfirmFor, read when a request would ask:
// nothing with force or while quitting already (the quit-wait keys), else
// per quit.confirm: always asks, when-busy asks only while jobs the ticker
// shows run (and says how many), never does not ask.
func quitConfirm(r *Runtime, c Context, q Request, _ string) string {
	if q.Args["force"] == true || r.Quit {
		return ""
	}
	prompt := "Quit " + r.programName() + "?"
	busy := 0
	for _, j := range r.runningJobs() {
		if tickerShows(r, j) {
			busy++
		}
	}
	if busy > 0 {
		prompt += fmt.Sprintf("\n%d running jobs are stopped.", busy)
	}
	switch r.Settings().String(QuitConfirmSetting) {
	case "never":
		return ""
	case "when-busy":
		if busy == 0 {
			return ""
		}
	}
	return prompt
}
