package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"ofam.dev/oden"
)

// ServeProtocol is the version of the host serve protocol (BIN ochazuke host
// serve): a catalog that declares it (CatalogDoc.Serve, the Serves option)
// tells a host it may keep the tool running and send it calls over a pipe
// (ServePool) instead of starting one process per call.
const ServeProtocol = 1

// HostPipeEnv is set to 1 in the environment of each call a served tool
// runs, so Host.Call and HostCommand answer over the pipe (host.run)
// instead of starting the host.
const HostPipeEnv = "OCHAZUKE_HOST_PIPE"

// Serves declares the host serve protocol in the catalog. A tool sets it
// when everything it reads from the environment it reads per call (the
// call's env is applied for the call): a tool that keeps env values from
// its start must not, so hosts keep starting it per call.
func Serves() CatalogOption {
	return func(doc *CatalogDoc) { doc.Serve = ServeProtocol }
}

// ServeCall is one call of the protocol, host to tool (run, watch) or tool
// to host (host.run): the command words after the program and its global
// arguments, the complete environment (nil keeps the process's) and the
// working directory ("" keeps it).
type ServeCall struct {
	Argv []string `json:"argv"`
	Env  []string `json:"env,omitempty"`
	Dir  string   `json:"dir,omitempty"`
}

// ServeAnswer is a call's outcome: what an exec of the same command would
// have printed and its exit status. Exec, from a tool, means it does not
// serve these words (an application command outside its nouns, an
// external): the host runs them by exec, unchanged.
type ServeAnswer struct {
	Stdout []byte `json:"stdout"`
	Stderr []byte `json:"stderr"`
	Exit   int    `json:"exit"`
	Exec   bool   `json:"exec,omitempty"`
}

// ServeHello is the answer to hello.
type ServeHello struct {
	Protocol    int    `json:"protocol"`
	Version     string `json:"version,omitempty"`
	Commit      string `json:"commit,omitempty"`
	PID         int    `json:"pid"`
	CatalogHash string `json:"catalog_hash,omitempty"`
}

// serveMsg is one newline JSON-RPC 2.0 message in either direction.
type serveMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *serveError     `json:"error,omitempty"`
}

type serveError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *serveError) Error() string { return e.Message }

// serveConn is one end of the pipe: a locked encoder, the cancel functions
// of the calls it received and the waiters of the calls it sent.
type serveConn struct {
	mu      sync.Mutex
	enc     *json.Encoder
	next    atomic.Int64
	cancels map[string]context.CancelFunc
	waiting map[string]chan serveMsg
	closed  bool
}

func newServeConn(w io.Writer) *serveConn {
	return &serveConn{enc: json.NewEncoder(w), cancels: map[string]context.CancelFunc{}, waiting: map[string]chan serveMsg{}}
}

func (c *serveConn) send(m serveMsg) error {
	m.JSONRPC = "2.0"
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enc.Encode(m)
}

func (c *serveConn) reply(id json.RawMessage, result any, err error) error {
	m := serveMsg{ID: id}
	if err != nil {
		var se *serveError
		if !errors.As(err, &se) {
			se = &serveError{Code: -32000, Message: err.Error()}
		}
		m.Error = se
	} else {
		raw, merr := json.Marshal(result)
		if merr != nil {
			return c.reply(id, nil, merr)
		}
		m.Result = raw
	}
	return c.send(m)
}

func (c *serveConn) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(serveMsg{Method: method, Params: raw})
}

// track registers a received call's cancel function under its id.
func (c *serveConn) track(id json.RawMessage, cancel context.CancelFunc) {
	c.mu.Lock()
	c.cancels[string(id)] = cancel
	c.mu.Unlock()
}

func (c *serveConn) untrack(id json.RawMessage) {
	c.mu.Lock()
	delete(c.cancels, string(id))
	c.mu.Unlock()
}

// cancel ends the received call params.id names ($/cancel).
func (c *serveConn) cancel(params json.RawMessage) {
	var p struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	c.mu.Lock()
	cancel := c.cancels[string(p.ID)]
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// newID is a fresh request id; the prefix keeps it apart from the other
// side's ids.
func (c *serveConn) newID(prefix string) json.RawMessage {
	id, _ := json.Marshal(fmt.Sprintf("%s%d", prefix, c.next.Add(1)))
	return id
}

// expect registers the channel id's answer arrives on; it is closed when
// the other side goes away first.
func (c *serveConn) expect(id json.RawMessage) chan serveMsg {
	answer := make(chan serveMsg, 1)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		close(answer)
		return answer
	}
	c.waiting[string(id)] = answer
	return answer
}

func (c *serveConn) forget(id json.RawMessage) {
	c.mu.Lock()
	delete(c.waiting, string(id))
	c.mu.Unlock()
}

func (c *serveConn) request(id json.RawMessage, method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(serveMsg{ID: id, Method: method, Params: raw})
}

// call sends a request and waits for its answer; ctx ending sends $/cancel
// and returns.
func (c *serveConn) call(ctx context.Context, prefix, method string, params any) (json.RawMessage, error) {
	id := c.newID(prefix)
	answer := c.expect(id)
	defer c.forget(id)
	if err := c.request(id, method, params); err != nil {
		return nil, err
	}
	select {
	case m, ok := <-answer:
		return m.outcome(ok)
	case <-ctx.Done():
		_ = c.notify("$/cancel", map[string]json.RawMessage{"id": id})
		return nil, ctx.Err()
	}
}

// outcome is an answer's result or error; ok false is a closed pipe.
func (m serveMsg) outcome(ok bool) (json.RawMessage, error) {
	if !ok {
		return nil, io.ErrClosedPipe
	}
	if m.Error != nil {
		return nil, m.Error
	}
	return m.Result, nil
}

// deliver hands a response to the call waiting for it.
func (c *serveConn) deliver(m serveMsg) {
	c.mu.Lock()
	answer := c.waiting[string(m.ID)]
	delete(c.waiting, string(m.ID))
	c.mu.Unlock()
	if answer != nil {
		answer <- m
	}
}

// shut fails every waiting call: the other side is gone.
func (c *serveConn) shut() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for id, answer := range c.waiting {
		close(answer)
		delete(c.waiting, id)
	}
}

// hostPipe is the pipe of the running host serve loop, nil when the process
// is not served. Host.Call and HostCommand use it for host.run.
var hostPipe atomic.Pointer[serveConn]

// servedCall is a request read from the host, with the context its
// $/cancel ends.
type servedCall struct {
	msg    serveMsg
	ctx    context.Context
	cancel context.CancelFunc
}

// hostCommand is BIN ochazuke host serve: the stdio server owns stdout.
func (s *Shell) hostCommand(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] != "serve" {
		return fmt.Errorf("usage: %s ochazuke host serve", s.Bin)
	}
	if hostPipe.Load() != nil {
		return errors.New("host serve is already running in this process")
	}
	in := s.Stdin
	if in == nil {
		in = strings.NewReader("")
	}
	out, restore := protocolStdout(s.stdout())
	defer restore()
	return s.HostServe(ctx, in, out)
}

// HostServe answers the host serve protocol on in and out until in ends:
// hello, run and watch from the host, $/cancel for a running call, and the
// answers to the host.run calls the tool's own code sends (Host.Call,
// HostCommand). Calls run one at a time, each with its env and directory
// applied and restored, its jobs under its context and the runtime's
// modals popped after it, through the same command words an exec reads
// (the application's oden commands and Mount's), so its stdout bytes and
// exit status are an exec's. Words the tool's own dispatch would not run
// here (an external, an application command outside Mount) answer Exec.
func (s *Shell) HostServe(ctx context.Context, in io.Reader, out io.Writer) error {
	conn := newServeConn(out)
	if !hostPipe.CompareAndSwap(nil, conn) {
		return errors.New("host serve is already running in this process")
	}
	defer hostPipe.Store(nil)
	s.Runtime.EnsureHistory()
	defer s.Runtime.Config().Flush()
	calls := make(chan servedCall, 64)
	read := make(chan error, 1)
	go func() { read <- conn.readLoop(ctx, in, calls) }()
	for call := range calls {
		s.serveOne(conn, call)
		call.cancel()
		conn.untrack(call.msg.ID)
	}
	conn.shut()
	return <-read
}

// readLoop reads messages until in ends: requests are queued for the call
// loop (their cancel registered first, so a $/cancel for a queued call
// counts), $/cancel ends one, responses go to their waiting host.run.
func (c *serveConn) readLoop(ctx context.Context, in io.Reader, calls chan<- servedCall) error {
	defer close(calls)
	defer c.shut()
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), 256<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var m serveMsg
		if err := json.Unmarshal(line, &m); err != nil {
			_ = c.send(serveMsg{ID: json.RawMessage("null"), Error: &serveError{Code: -32700, Message: "parse error"}})
			continue
		}
		switch {
		case m.Method == "$/cancel":
			c.cancel(m.Params)
		case m.Method == "":
			c.deliver(m)
		case len(m.ID) == 0:
			// Another notification: nothing to answer.
		default:
			callCtx, cancel := context.WithCancel(ctx)
			c.track(m.ID, cancel)
			calls <- servedCall{msg: m, ctx: callCtx, cancel: cancel}
		}
	}
	return scanner.Err()
}

// serveOne answers one request by its method (serveMethods).
func (s *Shell) serveOne(conn *serveConn, call servedCall) {
	answer := s.serveMethods()[call.msg.Method]
	if answer == nil {
		_ = conn.reply(call.msg.ID, nil, &serveError{Code: -32601, Message: "unknown method " + call.msg.Method})
		return
	}
	answer(conn, call)
}

// serveMethods are the protocol's requests from the host: hello, run (a
// call answered when it ends) and watch (a call whose stdout lines are sent
// as out notifications while it runs).
func (s *Shell) serveMethods() map[string]func(*serveConn, servedCall) {
	return map[string]func(*serveConn, servedCall){
		"hello": func(conn *serveConn, call servedCall) {
			var p struct {
				CatalogHash bool `json:"catalog_hash"`
			}
			_ = json.Unmarshal(call.msg.Params, &p)
			_ = conn.reply(call.msg.ID, s.serveHello(call.ctx, p.CatalogHash), nil)
		},
		"run": func(conn *serveConn, call servedCall) { s.serveWords(conn, call, nil) },
		"watch": func(conn *serveConn, call servedCall) {
			id := call.msg.ID
			s.serveWords(conn, call, &lineWriter{emit: func(line []byte) {
				_ = conn.notify("out", map[string]any{"id": id, "line": string(line)})
			}})
		},
	}
}

// serveWords answers a run or watch request.
func (s *Shell) serveWords(conn *serveConn, call servedCall, lines *lineWriter) {
	var p ServeCall
	if err := json.Unmarshal(call.msg.Params, &p); err != nil || len(p.Argv) == 0 {
		_ = conn.reply(call.msg.ID, nil, &serveError{Code: -32602, Message: "run needs argv"})
		return
	}
	_ = conn.reply(call.msg.ID, s.serveRun(call.ctx, p, lines), nil)
}

func (s *Shell) serveHello(ctx context.Context, withHash bool) ServeHello {
	h := ServeHello{Protocol: ServeProtocol, Version: s.version(), PID: os.Getpid()}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				h.Commit = setting.Value
			}
		}
	}
	if withHash {
		if raw, err := Catalog(s.Runtime, s.Bin, s.version(), s.catalogOptions...); err == nil {
			h.CatalogHash = CatalogHash(raw)
		}
	}
	return h
}

// CatalogHash identifies a catalog's content, independent of its spacing
// and of the commands list (the oden commands the program's PATH adds): a
// host compares the hash of the catalog it registered with a served
// tool's (ServeHello.CatalogHash) to know whether it is stale.
func CatalogHash(raw []byte) string {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	delete(doc, "commands")
	canonical, _ := json.Marshal(doc)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// serveRun runs one call's words as an exec of them would, with the call's
// environment and directory applied until it ends. A watch call's stdout
// goes to lines as it is written.
func (s *Shell) serveRun(ctx context.Context, p ServeCall, lines *lineWriter) ServeAnswer {
	restore, err := applyCallEnv(p)
	if err != nil {
		return ServeAnswer{Stderr: []byte(err.Error() + "\n"), Exit: 1}
	}
	defer restore()
	defer s.Runtime.WithJobContext(ctx)()
	var out, errOut bytes.Buffer
	var stdout io.Writer = &out
	if lines != nil {
		stdout = lines
	}
	served, err := s.serveDispatch(ctx, p.Argv, stdout, &errOut)
	if !served {
		return ServeAnswer{Exec: true}
	}
	if lines != nil {
		lines.flush()
	}
	exit := oden.Report(&errOut, s.Bin, err)
	// This headless runtime has no place to leave a modal: clear what the
	// call left so the next call starts as a fresh process would.
	for s.Runtime.TopModal() != "" {
		if s.Runtime.Pop() != nil {
			break
		}
	}
	return ServeAnswer{Stdout: out.Bytes(), Stderr: errOut.Bytes(), Exit: exit}
}

// applyCallEnv sets the call's environment and directory for this process
// and returns what puts the previous ones back. The pipe marker is set in
// both, so the tool's host calls go over the pipe.
func applyCallEnv(p ServeCall) (func(), error) {
	before := os.Environ()
	dir := ""
	if p.Dir != "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return nil, err
		}
		if err := os.Chdir(p.Dir); err != nil {
			return nil, err
		}
	}
	if p.Env != nil {
		setEnviron(p.Env)
	}
	os.Setenv(HostPipeEnv, "1")
	return func() {
		if p.Env != nil {
			setEnviron(before)
			os.Setenv(HostPipeEnv, "1")
		}
		if dir != "" {
			_ = os.Chdir(dir)
		}
	}, nil
}

func setEnviron(env []string) {
	os.Clearenv()
	for _, kv := range env {
		if key, value, ok := strings.Cut(kv, "="); ok && key != "" {
			os.Setenv(key, value)
		}
	}
}

// serveDispatch runs argv through a fresh oden app mounted like the
// program's own (the app Mount saw first), so a word reads as an exec reads
// it. served is false when the program would run the words some other way:
// no mounted app to compare with is fine (the shell's own words), but a
// word the program's app resolves to a different command, or one this app
// does not resolve to an inline command (an external, a Default), is not
// served.
func (s *Shell) serveDispatch(ctx context.Context, argv []string, stdout, stderr io.Writer) (served bool, err error) {
	clone := *s
	clone.Stdin = strings.NewReader("") // an exec's stdin is closed: a confirmation declines
	clone.Stdout, clone.Stderr = stdout, stderr
	clone.in = nil
	clone.mounted = nil
	clone.catalogOptions = slices.Clone(s.catalogOptions)
	if words, ok := s.verbWords(argv); ok {
		return true, clone.Run(ctx, words)
	}
	kept := len(clone.catalogOptions)
	name := s.Bin
	if s.mounted != nil {
		name = s.mounted.Name
	}
	// No PATH: an external is the program's to run, and the words it
	// would resolve to one are not served (servesWords), so this app
	// skips the discovery an exec pays for.
	app := oden.New(name, slices.Clone(argv), oden.WithBuiltins(), oden.WithStdio(clone.Stdin, stdout, stderr), oden.WithPath(""))
	Mount(app, &clone)
	if s.mounted != nil {
		// The catalog's commands are the program's, not this app's.
		clone.catalogOptions = clone.catalogOptions[:kept]
	}
	if !s.servesWords(app) {
		return false, nil
	}
	return true, app.Run(ctx)
}

// verbWords is the shell command the program's own app runs for argv when
// argv spells a mounted verb's command exactly (its noun's group words, then
// the verb's name, then no help flag), which is what Mount's leaf for it
// runs (Shell.Run of the noun and verb): the call skips building a fresh
// app. Any other spelling (an alias, a prefix, a dotted noun, help) takes
// the full path.
func (s *Shell) verbWords(argv []string) ([]string, bool) {
	if s.mounted == nil || len(argv) < 2 || slices.ContainsFunc(argv, func(w string) bool { return w == "-h" || w == "--help" }) || s.bareAlias(argv[0]) {
		return nil, false
	}
	if _, deprecated := s.Runtime.DeprecatedNounAlias(argv[0]); deprecated {
		return nil, false
	}
	e := s.mounted.Catalog().Lookup(argv[0])
	if e == nil || e.Kind != oden.KindInline || e.Command == nil || e.Command.Yield || e.Name != argv[0] {
		return nil, false
	}
	group := e.Command.Children
	for i := 1; i < len(argv); i++ {
		j := slices.IndexFunc(group, func(c oden.Command) bool { return c.Name == argv[i] })
		if j < 0 {
			return nil, false
		}
		c := group[j]
		if c.Children != nil {
			group = c.Children
			continue
		}
		op, ok := s.Runtime.Operation(c.Source)
		if !ok || op.Key.Short() != c.Source || op.Name() != c.Name {
			return nil, false
		}
		return append([]string{op.Noun(), op.Name()}, argv[i+1:]...), true
	}
	return nil, false
}

// servesWords reports whether app runs its first word as an inline command
// the program's own app runs too.
func (s *Shell) servesWords(app *oden.App) bool {
	if len(app.Args) == 0 {
		return false
	}
	res := app.Catalog().Resolve(app.Args[0])
	if !res.Resolved() || res.Entry.Kind != oden.KindInline {
		return false
	}
	if c := res.Entry.Command; res.Exact && c != nil && c.Yield && app.Claims != nil && app.Claims(app.Args) {
		return false
	}
	if s.mounted == nil {
		return true
	}
	own := s.mounted.Catalog().Resolve(app.Args[0])
	return own.Resolved() && own.Entry.Kind == oden.KindInline && own.Entry.Name == res.Entry.Name
}

// lineWriter hands each complete line written to emit (a watch's events).
type lineWriter struct {
	emit func([]byte)
	buf  []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		w.emit(slices.Clone(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
}

func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.emit(w.buf)
		w.buf = nil
	}
}

// pipeHostRun runs a host command line over the served pipe; env is the
// call's complete environment. ok is false when this process is not served
// or env does not carry the pipe marker (the caller execs).
func pipeHostRun(ctx context.Context, argv, env []string) (answer ServeAnswer, ok bool, err error) {
	conn := hostPipe.Load()
	if conn == nil || lookupEnv(env, HostPipeEnv) != "1" {
		return ServeAnswer{}, false, nil
	}
	env = slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return strings.HasPrefix(kv, HostPipeEnv+"=") })
	raw, err := conn.call(ctx, "h", "host.run", ServeCall{Argv: argv, Env: env})
	if err != nil {
		return ServeAnswer{}, true, err
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return ServeAnswer{}, true, err
	}
	return answer, true, nil
}
