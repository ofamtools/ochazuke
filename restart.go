package ochazuke

import (
	"context"
	"encoding/json"
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
// request (restart-request.post {reason, version}). The request is a
// stored singleton, the noun ochazuke.restart-request, kept in a DocTable
// the application gives (Runtime.SetRestartTable: a table in its database,
// so a request posted by any process on it, a command or the MCP gateway,
// reaches every program running on it; in memory by default). A running
// program reads it through the table's change token: the lead status
// segment "restart" (placement lead, verb restart-request.alert) draws a
// RESTART REQUESTED badge at the left of the title row (setting
// restart.badge), and the first read of a new request tells the person
// with a notice. A request applies to the programs started before it was
// posted: the restarted program, or one started later, has the build
// already.
//
// app.restart asks first, naming the running build, the installed one and
// the request; yes runs app.quit (the one verb that quits: its checks,
// queued writes, app.quitting hooks and quit hooks, QuitHook) with
// restart: once the terminal is restored the process is replaced with the
// installed binary, same arguments and environment, after every quit
// hook's Close. What the application knows (the binary's path, how to ask
// it its version) it gives through one hook, Runtime.SetRestart(RestartHook);
// its cleanup is a quit hook. Without a path or a way to exec (no hook, or
// a platform with no exec) the same verb asks to quit and quits: one code
// path, the capability is data.

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

	// RestartRequestNoun is the stored restart request's noun.
	RestartRequestNoun = "ochazuke.restart-request"
	// RestartSegment is the badge's segment id (placement lead).
	RestartSegment = "restart"
	// RestartPollEvery is how often a program whose request lives in a
	// table other processes write (SetRestartTable) reads its change
	// token: the badge segment's interval, with no backoff.
	RestartPollEvery = time.Second

	restartStateID = "__restart-state"
	// restartDoc is the request's key in its table.
	restartDoc = "current"

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
	// Exec replaces the process; nil is the platform's (syscall.Exec on
	// unix, none elsewhere, where app.restart quits instead). Tests
	// inject one. The application's cleanup before it runs is a quit
	// hook's Close (AddQuitHook).
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

// restartState is the runtime's restart wiring: the hook, the table the
// request is stored in, when this program started (a request posted
// before it is not for it), the last read of the table and the token it
// was read at, the notice and the request it tells, the last probe and
// whether a confirmed restart waits for the quit to finish.
type restartState struct {
	hook      RestartHook
	table     DocTable
	started   time.Time
	token     string
	read      bool
	stored    RestartRequest
	notice    NoticeID
	noticed   RestartRequest
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
	s := &restartState{started: time.Now()}
	s.table = NewMemDocTable(r, RestartRequestNoun)
	inst := r.Instance(restartStateID, Kind("ochazuke.restart-state"))
	inst.Component = s
	return s
}

// SetRestart gives the runtime the application's restart hook.
func (r *Runtime) SetRestart(h RestartHook) { r.restart().hook = h }

// SetRestartTable keeps the restart request in table (the application's
// database), so a request any process posts on it reaches this program:
// the badge segment then reads the table's change token every
// RestartPollEvery. The default is a table in memory, this process alone.
func (r *Runtime) SetRestartTable(table DocTable) {
	s := r.restart()
	s.table, s.read, s.token = table, false, ""
	_ = r.AddSegment(Segment{ID: RestartSegment, Every: RestartPollEvery, MaxEvery: RestartPollEvery})
}

// RestartRequested is the restart request pending for this program:
// Pending false when there is none, or when the stored one was posted
// before this program started (it runs that build already).
func (r *Runtime) RestartRequested() RestartRequest {
	req, err := r.storedRestart()
	if err != nil || !req.Pending || req.At.Before(r.restart().started) {
		return RestartRequest{}
	}
	return req
}

// storedRestart is the request as its table holds it (Pending false when
// it holds none), read again only when the table's change token moved.
func (r *Runtime) storedRestart() (RestartRequest, error) {
	s := r.restart()
	token, err := s.table.Version()
	if err == nil && s.read && token != "" && token == s.token {
		return s.stored, nil
	}
	doc, ok, err := s.table.Get(restartDoc)
	if err != nil {
		return RestartRequest{}, err
	}
	req := RestartRequest{}
	if ok {
		req = restartRequestOf(doc)
	}
	s.token, s.read, s.stored = token, true, req
	return req, nil
}

// writeRestart stores req as the request.
func (r *Runtime) writeRestart(req RestartRequest) error {
	s := r.restart()
	if err := s.table.Put(restartDoc, restartDocOf(req)); err != nil {
		return err
	}
	s.read = false
	return nil
}

// restartRequestOf reads a stored document as a request (a field it
// lacks is its zero value).
func restartRequestOf(doc Thing) RestartRequest {
	var req RestartRequest
	if data, err := json.Marshal(doc); err == nil {
		_ = json.Unmarshal(data, &req)
	}
	return req
}

// restartDocOf is the document a request is stored as: every field, a
// zero one as nil so a table that merges removes it.
func restartDocOf(req RestartRequest) Thing {
	doc := Thing{"pending": req.Pending, "count": req.Count, "reason": nil, "version": nil, "actor": nil, "at": nil}
	if req.Reason != "" {
		doc["reason"] = req.Reason
	}
	if req.Version != "" {
		doc["version"] = req.Version
	}
	if !req.At.IsZero() {
		doc["at"] = req.At.UTC().Format(time.RFC3339Nano)
	}
	if req.Actor != (Actor{}) {
		var actor map[string]any
		if data, err := json.Marshal(req.Actor); err == nil && json.Unmarshal(data, &actor) == nil {
			doc["actor"] = actor
		}
	}
	return doc
}

// restartThing is a request as the noun's thing.
func restartThing(req RestartRequest) Thing {
	t := restartDocOf(req)
	for k, v := range t {
		if v == nil {
			delete(t, k)
		}
	}
	return t
}

// RestartError is why the last confirmed restart did not start the
// installed binary (the program then exited), nil when none failed.
func (r *Runtime) RestartError() error { return r.restart().err }

// restartBadge is the badge segment's answer: RESTART REQUESTED while a
// request is pending for this program and restart.badge is on, else no
// text. Reading it tells the notice about a request it has not told yet
// (observeRestart), so a request another process posts is noticed even
// with the badge off.
func (r *Runtime) restartBadge() SegmentValue {
	req := r.observeRestart()
	if !req.Pending || !r.Settings().Bool(RestartBadgeSetting) {
		return SegmentValue{}
	}
	return SegmentValue{Text: RestartBadgeText}
}

// observeRestart reads the request pending for this program and keeps its
// notice in step: a new request (another time or count) replaces the
// notice, none clears it.
func (r *Runtime) observeRestart() RestartRequest {
	s := r.restart()
	req := r.RestartRequested()
	if req.Pending && req.At.Equal(s.noticed.At) && req.Count == s.noticed.Count {
		return req
	}
	ui := r.UI()
	if s.notice != 0 {
		ui.ClearNotice(s.notice)
		s.notice = 0
	}
	s.noticed = req
	if !req.Pending {
		return req
	}
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
	s.notice = ui.Notify(text)
	return req
}

// restartChanged is what a change to the request in this program does:
// the notice now, the badge at the next update.
func (r *Runtime) restartChanged() {
	r.observeRestart()
	if row := r.segs().find(RestartSegment); row != nil {
		row.due, row.wait = true, 0
	}
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
	if req := r.RestartRequested(); req.Pending {
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
	// A prompt's {{field}} is expanded by the confirm; text from a request
	// is never a field.
	return strings.TrimRight(strings.ReplaceAll(b.String(), "{{", "{ {"), "\n")
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

// postRestart stores a new request, or updates the pending one: who
// asked, when, and the reason and version when given. It is a Handler,
// not an AppHandler: the table's failure (the disk) is its answer as it
// is.
func postRestart(r *Runtime, _ Context, a Arguments) (any, error) {
	req, err := r.storedRestart()
	if err != nil {
		return nil, err
	}
	if !req.Pending {
		req = RestartRequest{}
	}
	req.Pending = true
	req.Actor = r.Actor()
	req.At = time.Now()
	req.Count++
	if reason := StringArg(a, "reason", ""); reason != "" {
		req.Reason = reason
	}
	if version := StringArg(a, "version", ""); version != "" {
		req.Version = version
	}
	if err := r.writeRestart(req); err != nil {
		return nil, err
	}
	r.restartChanged()
	return req, nil
}

// dismissRestart drops the stored request; it answers the one it dropped.
func dismissRestart(r *Runtime, _ Context, _ Arguments) (any, error) {
	was, err := r.storedRestart()
	if err != nil {
		return nil, err
	}
	if err := r.writeRestart(RestartRequest{}); err != nil {
		return nil, err
	}
	r.restartChanged()
	return was, nil
}

// showRestart answers the stored request (pending false when there is
// none).
func showRestart(r *Runtime, _ Context, _ Arguments) (any, error) {
	return r.storedRestart()
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
	if err := r.CloseQuitHooks(); err != nil {
		return path, err
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
	// The request is a stored singleton: get, show (its own), watch and the
	// rest of the standard set read it through the table; post and dismiss
	// write it.
	r.Define(RestartRequestNoun).About("The request to restart the application after a new build was installed: who asked, when, why and what was installed; stored where the application keeps it, so every program running on that store sees it").Props(
		PropSpec{Name: "pending", ReadOnly: true, Parameter: Parameter{Type: SettingBoolean, Description: "Whether a request is pending"}},
		PropSpec{Name: "reason", ReadOnly: true, Parameter: Parameter{Description: "Why, in a few words"}},
		PropSpec{Name: "version", ReadOnly: true, Parameter: Parameter{Description: "The build that was installed"}},
		PropSpec{Name: "actor", ReadOnly: true, Parameter: Parameter{Type: PropObject, Description: "Who asked"}},
		PropSpec{Name: "at", ReadOnly: true, Parameter: Parameter{Description: "When it was asked (RFC 3339)"}},
		PropSpec{Name: "count", ReadOnly: true, Parameter: Parameter{Type: SettingInteger, Description: "How many requests it gathers"}},
	).Store(Store{
		Kind: Singleton, NoOverlay: true,
		Get: func(c Ctx, _ string) (Thing, error) {
			req, err := c.Runtime.storedRestart()
			if err != nil {
				return nil, err
			}
			return restartThing(req), nil
		},
		ChangeToken: func(c Ctx) (string, error) { return c.Runtime.restart().table.Version() },
	})
	r.Register(Operation{Key: RestartRequestNoun + ".post", HistoryKind: HistoryChange, Agent: AgentAllow, Description: "Ask the person to restart the application (after installing a new build): stored, so every program running on the store shows a notice and a RESTART REQUESTED badge until they restart or dismiss it; a second request while one is pending updates it", Parameters: map[string]Parameter{
		"reason":  {Type: "string", Description: "Why, in a few words"},
		"version": {Type: "string", Description: "The build that was installed"},
	}, Positional: []string{"reason"}, Result: "restart-request", Run: postRestart})
	r.Register(Operation{Key: RestartRequestNoun + ".show", HistoryKind: HistoryFetch, Agent: AgentAllow, Description: "Answer the stored restart request (pending false when there is none)", Result: "restart-request", Run: showRestart})
	r.Register(Operation{Key: RestartRequestNoun + ".dismiss", HistoryKind: HistoryChange, Agent: AgentAllow, Description: "Drop the stored restart request, its notice and its badge in every program, without restarting", Result: "restart-request", Run: dismissRestart})
	r.Register(Operation{Key: RestartRequestNoun + ".alert", HistoryKind: HistoryFetch, Agent: AgentAllow, Description: "Answer the restart badge's text for this program: RESTART REQUESTED while a request posted after it started is pending and restart.badge is on, else nothing (the source of the lead status segment restart; reading it tells this program's notice about a new request)", Result: "segment-value", AppHandler: func(c Ctx) Result { return Done(c.Runtime.restartBadge()) }})
	r.Register(Operation{Key: AppNoun + ".restart", HistoryKind: HistoryAction, Agent: AgentConfirm, Confirm: "Restart?", ConfirmFor: restartConfirm, Loses: "the running session: its open views and what it holds in memory", Description: "Restart the application in this terminal: ask first (the running and installed builds, the pending request), then run app.quit and start the installed binary with the same arguments; where it cannot restart itself it asks to quit and quits", Result: "boolean", Run: func(r *Runtime, _ Context, _ Arguments) (any, error) {
		// The one quit: its checks, hooks and history run once, and this
		// verb's question stands for quit's own (asked).
		return r.Execute(Request{Noun: AppNoun, Verb: "quit", Args: Arguments{"asked": true, "restart": true}})
	}})
}

// registerRestartBadge adds the badge's lead segment, once the segments
// and settings are registered.
func registerRestartBadge(r *Runtime) {
	// The badge follows restart.badge at once, not at the next refresh.
	r.Settings().Observe(RestartBadgeSetting, func(string, any) { r.restartChanged() })
	_ = r.AddSegment(Segment{ID: RestartSegment, Placement: SegmentPlaceLead, Verb: RestartRequestNoun + ".alert", Short: restartBadgeShort,
		Description: "Draw the restart badge at the left of the title row and look for restart requests (restart.badge hides the badge alone)"})
}

// quitParameters are app.quit's parameters.
var quitParameters = map[string]Parameter{
	"force":   {Type: "boolean", Description: "quit without checks, the question or waiting for writes; report unfinished values"},
	"asked":   {Type: "boolean", Plumbing: true, Description: "the caller asked already (app.restart's question): quit.confirm does not ask again; checks still run"},
	"restart": {Type: "boolean", Plumbing: true, Description: "once quit, start the installed binary in place (app.restart passes it after its question)"},
}

// quitApp is app.quit: arm the restart when asked to and possible, then
// quit.
func quitApp(c Ctx) Result {
	r := c.Runtime
	if c.Args["restart"] == true {
		if _, can, why := r.restartPath(); can {
			r.restart().armed = true
		} else if r.restart().hook.Path != nil {
			c.Info("quitting: " + why)
		}
	}
	r.quit(c.Args["force"] == true)
	return Done(true)
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
	if q.Args["force"] == true || q.Args["asked"] == true || r.Quit {
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
