package cli

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"ofam.dev/ochazuke"
)

// ServePool keeps up to Size processes of one tool running its host serve
// loop (BIN ARGS ochazuke host serve) and sends them the calls a host would
// otherwise exec, one call per process at a time. Every rule of it is
// data the host passes in (its settings), and exec stays the exact
// fallback: the first After-1 calls, a call while every process is busy and
// the pool is full, a call the tool answers Exec, a binary that cannot be
// stat'd, and every call after the tool failed hello or spoke another
// protocol. A process whose binary changed (BinStamp) is drained and the
// host told (OnStale); a read that fails because the process broke is
// retried once by exec, a write never (it may have applied). The zero
// value of each field is usable: Size 2, After 1 (serve from the first
// call), no idle exit, a 1 s wait before a cancelled call's process is
// killed.
type ServePool struct {
	Bin  string
	Args []string // global arguments: a call's Argv starting with them is served, the rest exec
	Dir  string
	// After is the call (counted from 1) on which the pool starts serving;
	// the calls before it exec, so a one-shot CLI never pays a server's start.
	After int
	// Size is the most processes kept for this tool; beyond it a call execs.
	Size int
	// IdleTimeout ends a process idle that long (stdin closed); 0 never.
	IdleTimeout time.Duration
	// WaitDelay is how long a cancelled call has to answer before its
	// process (group) is killed and replaced.
	WaitDelay time.Duration
	// StartEnv are the environment keys whose values a process keeps from
	// its start (the data directory the tool opened, say): a call with other
	// values for them runs on another process.
	StartEnv []string
	// CatalogHash, when set, is CatalogHash of the catalog the host
	// registered; a process whose hello differs reports OnStale.
	CatalogHash string
	// OnStale is told once per change that the tool's binary or catalog is
	// not the one the host registered (the host refreshes its catalog).
	OnStale func(reason string)
	// OnHostRun answers a served tool's host.run (Host.Call, HostCommand);
	// nil execs the call's argv.
	OnHostRun func(ctx context.Context, call ServeCall) ServeAnswer
	// Notice, when set, hears why the pool went exec-only.
	Notice func(string)

	mu     sync.Mutex
	calls  int
	procs  []*poolProc
	path   string
	stamp  string
	broken bool
	closed bool
	served int // calls a process answered (tests read it)
}

// PoolConnection is one live process of a pool (a host's connection noun).
type PoolConnection struct {
	PID       int       `json:"pid"`
	State     string    `json:"state"` // starting, idle, busy, stale
	Calls     int       `json:"calls"`
	Started   time.Time `json:"started"`
	Stamp     string    `json:"stamp"`
	LastError string    `json:"lastError,omitempty"`
}

type poolProc struct {
	cmd      *exec.Cmd
	conn     *serveConn
	stdin    io.WriteCloser
	kill     func()
	stderr   *tailBuffer
	dead     chan struct{}
	key      string
	stamp    string
	started  time.Time
	pid      int
	busy     bool
	starting bool
	stale    bool
	calls    int
	idle     *time.Timer
	lastErr  string
	linesMu  sync.Mutex
	lines    map[string]func([]byte)
}

// errServeExec is a call the pool hands to exec.
var errServeExec = errors.New("exec")

// BinStamp is a binary's size and modification time, which change when it
// is rebuilt or replaced.
func BinStamp(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%d", info.Size(), info.ModTime().UnixNano()), nil
}

// Run answers a call as an exec of Bin with call.Argv would: served when it
// can be, else exec. read marks a call safe to repeat (a fetch): one whose
// process breaks under it runs again by exec. The error is a failure to
// run at all (the context, a process that broke during a write); a tool's
// own failure is the answer's Exit and Stderr.
func (p *ServePool) Run(ctx context.Context, call ServeCall, read bool) (ServeAnswer, error) {
	return p.call(ctx, "run", call, read, nil)
}

// Watch runs a long call (a watch) whose stdout lines go to line as they
// come; it holds a process until the call ends (ctx ends it). Beyond the
// pool's size it execs, streaming the same lines.
func (p *ServePool) Watch(ctx context.Context, call ServeCall, line func([]byte)) (ServeAnswer, error) {
	return p.call(ctx, "watch", call, false, line)
}

func (p *ServePool) call(ctx context.Context, method string, call ServeCall, read bool, line func([]byte)) (ServeAnswer, error) {
	words, ok := p.words(call.Argv)
	if !ok {
		return p.exec(ctx, call, line)
	}
	proc := p.acquire(call.Env)
	if proc == nil {
		return p.exec(ctx, call, line)
	}
	served := call
	served.Argv = words
	answer, err := p.send(ctx, proc, method, served, line)
	p.release(proc, err)
	switch {
	case errors.Is(err, errServeExec):
		return p.exec(ctx, call, line)
	case err != nil && ctx.Err() == nil && read:
		return p.exec(ctx, call, line)
	}
	return answer, err
}

// words strips the pool's global arguments from argv.
func (p *ServePool) words(argv []string) ([]string, bool) {
	if len(argv) <= len(p.Args) || !slices.Equal(argv[:len(p.Args)], p.Args) {
		return nil, false
	}
	return argv[len(p.Args):], true
}

func (p *ServePool) startKey(env []string) string {
	if env == nil {
		env = os.Environ()
	}
	var b strings.Builder
	for _, key := range p.StartEnv {
		b.WriteString(key + "=" + lookupEnv(env, key) + "\x00")
	}
	return b.String()
}

func lookupEnv(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if value, ok := strings.CutPrefix(env[i], key+"="); ok {
			return value
		}
	}
	return ""
}

// acquire takes a free process for env, or reserves a slot and starts one;
// nil means exec this call.
func (p *ServePool) acquire(env []string) *poolProc {
	p.mu.Lock()
	if p.closed || p.broken {
		p.mu.Unlock()
		return nil
	}
	p.calls++
	if p.calls < max(p.After, 1) {
		p.mu.Unlock()
		return nil
	}
	stamp, stale := p.restamp()
	if stamp == "" {
		p.mu.Unlock()
		return nil
	}
	key := p.startKey(env)
	proc := p.free(key)
	if proc == nil {
		proc = p.reserve(key, stamp)
	}
	p.mu.Unlock()
	if stale && p.OnStale != nil {
		p.OnStale("binary changed")
	}
	if proc != nil && proc.starting && !p.start(proc, env) {
		return nil
	}
	return proc
}

// restamp reads the binary's stamp; a change marks every process stale
// (drained: idle ones end now, busy ones after their call).
func (p *ServePool) restamp() (stamp string, changed bool) {
	if p.path == "" {
		path, err := exec.LookPath(p.Bin)
		if err != nil {
			return "", false
		}
		p.path = path
	}
	stamp, err := BinStamp(p.path)
	if err != nil {
		return "", false
	}
	if p.stamp != "" && stamp != p.stamp {
		changed = true
		for _, proc := range p.procs {
			proc.stale = true
			if !proc.busy {
				p.drop(proc)
			}
		}
	}
	p.stamp = stamp
	return stamp, changed
}

// free is an idle, current process started for key.
func (p *ServePool) free(key string) *poolProc {
	for _, proc := range p.procs {
		if !proc.busy && !proc.stale && proc.key == key && !isDone(proc.dead) {
			proc.busy = true
			if proc.idle != nil {
				proc.idle.Stop()
			}
			return proc
		}
	}
	return nil
}

// reserve claims a slot for a new process: under Size, or in place of an
// idle process started for another key.
func (p *ServePool) reserve(key, stamp string) *poolProc {
	if len(p.procs) >= max(p.Size, 1) {
		i := slices.IndexFunc(p.procs, func(proc *poolProc) bool { return !proc.busy })
		if i < 0 {
			return nil
		}
		p.drop(p.procs[i])
	}
	proc := &poolProc{key: key, stamp: stamp, busy: true, starting: true, started: time.Now(), dead: make(chan struct{}), stderr: &tailBuffer{}, lines: map[string]func([]byte){}}
	p.procs = append(p.procs, proc)
	return proc
}

// drop removes a process from the pool and ends it. Under p.mu.
func (p *ServePool) drop(proc *poolProc) {
	p.procs = slices.DeleteFunc(p.procs, func(q *poolProc) bool { return q == proc })
	if proc.idle != nil {
		proc.idle.Stop()
	}
	go proc.close(p.waitDelay())
}

func (p *ServePool) waitDelay() time.Duration {
	if p.WaitDelay > 0 {
		return p.WaitDelay
	}
	return time.Second
}

// start runs the process and says hello; false gives the slot back (the
// call execs), and a hello the pool cannot use makes it exec-only.
func (p *ServePool) start(proc *poolProc, env []string) bool {
	err := proc.spawn(p, env)
	var hello ServeHello
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var raw json.RawMessage
		raw, err = proc.conn.call(ctx, "c", "hello", map[string]bool{"catalog_hash": p.CatalogHash != ""})
		cancel()
		if err == nil {
			err = json.Unmarshal(raw, &hello)
		}
		if err == nil && hello.Protocol != ServeProtocol {
			err = fmt.Errorf("protocol %d, want %d", hello.Protocol, ServeProtocol)
		}
	}
	p.mu.Lock()
	proc.starting = false
	if err == nil {
		proc.pid = proc.cmd.Process.Pid
	}
	if err != nil {
		p.broken = true
		p.drop(proc)
	}
	p.mu.Unlock()
	if err != nil {
		if p.Notice != nil {
			p.Notice(fmt.Sprintf("%s: serving off, every call execs: %v%s", filepath.Base(p.Bin), err, proc.stderr.tail()))
		}
		return false
	}
	if p.CatalogHash != "" && hello.CatalogHash != p.CatalogHash && p.OnStale != nil {
		p.OnStale("catalog changed")
	}
	return true
}

// spawn starts BIN ARGS ochazuke host serve with env and reads its answers.
func (proc *poolProc) spawn(p *ServePool, env []string) error {
	cmd := exec.Command(p.path, append(slices.Clone(p.Args), "ochazuke", "host", "serve")...)
	cmd.Env, cmd.Dir = env, p.Dir
	cmd.Stderr = proc.stderr
	proc.kill = capturedGroup(cmd)
	cmd.Cancel = nil // no context: the pool ends it (close, kill)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	proc.cmd, proc.stdin, proc.conn = cmd, stdin, newServeConn(stdin)
	go proc.read(p, stdout)
	return nil
}

// read handles what the tool sends until it exits: answers, a watch's out
// lines, host.run calls and their cancels.
func (proc *poolProc) read(p *ServePool, stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 256<<20)
	for scanner.Scan() {
		var m serveMsg
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			continue
		}
		switch m.Method {
		case "":
			proc.conn.deliver(m)
		case "out":
			proc.out(m.Params)
		case "$/cancel":
			proc.conn.cancel(m.Params)
		case "host.run":
			go proc.hostRun(p, m)
		}
	}
	proc.conn.shut()
	_ = proc.cmd.Wait()
	close(proc.dead)
}

func (proc *poolProc) out(params json.RawMessage) {
	var o struct {
		ID   json.RawMessage `json:"id"`
		Line string          `json:"line"`
	}
	if json.Unmarshal(params, &o) != nil {
		return
	}
	proc.linesMu.Lock()
	line := proc.lines[string(o.ID)]
	proc.linesMu.Unlock()
	if line != nil {
		line([]byte(o.Line))
	}
}

// hostRun answers the tool's host.run.
func (proc *poolProc) hostRun(p *ServePool, m serveMsg) {
	var call ServeCall
	if err := json.Unmarshal(m.Params, &call); err != nil || len(call.Argv) == 0 {
		_ = proc.conn.reply(m.ID, nil, &serveError{Code: -32602, Message: "host.run needs argv"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	proc.conn.track(m.ID, cancel)
	defer proc.conn.untrack(m.ID)
	defer cancel()
	var answer ServeAnswer
	if p.OnHostRun != nil {
		answer = p.OnHostRun(ctx, call)
	} else {
		bin := &ServePool{Bin: call.Argv[0]}
		answer, _ = bin.exec(ctx, ServeCall{Argv: call.Argv[1:], Env: call.Env, Dir: call.Dir}, nil)
	}
	_ = proc.conn.reply(m.ID, answer, nil)
}

// send runs one call on proc. A cancelled call has WaitDelay to answer,
// else its process is killed (release drops it).
func (p *ServePool) send(ctx context.Context, proc *poolProc, method string, call ServeCall, line func([]byte)) (ServeAnswer, error) {
	id := proc.conn.newID("c")
	answer := proc.conn.expect(id)
	defer proc.conn.forget(id)
	if line != nil {
		proc.linesMu.Lock()
		proc.lines[string(id)] = line
		proc.linesMu.Unlock()
		defer func() {
			proc.linesMu.Lock()
			delete(proc.lines, string(id))
			proc.linesMu.Unlock()
		}()
	}
	if err := proc.conn.request(id, method, call); err != nil {
		return ServeAnswer{}, proc.broke(err)
	}
	select {
	case m, ok := <-answer:
		return proc.answer(m, ok)
	case <-ctx.Done():
	}
	_ = proc.conn.notify("$/cancel", map[string]json.RawMessage{"id": id})
	select {
	case <-answer:
	case <-time.After(p.waitDelay()):
		p.mu.Lock()
		proc.stale = true // release drops it
		p.mu.Unlock()
		proc.kill()
	}
	return ServeAnswer{}, ctx.Err()
}

func (proc *poolProc) answer(m serveMsg, ok bool) (ServeAnswer, error) {
	raw, err := m.outcome(ok)
	if err != nil {
		return ServeAnswer{}, proc.broke(err)
	}
	var a ServeAnswer
	if err := json.Unmarshal(raw, &a); err != nil {
		return ServeAnswer{}, proc.broke(err)
	}
	if a.Exec {
		return ServeAnswer{}, errServeExec
	}
	return a, nil
}

// broke is the error of a process that failed under a call: the tool's
// stderr tail says why.
func (proc *poolProc) broke(err error) error {
	select {
	case <-proc.dead:
	case <-time.After(100 * time.Millisecond):
	}
	return fmt.Errorf("served process: %w%s", err, proc.stderr.tail())
}

// release gives proc back after a call: a broken, dead or stale process is
// dropped, an idle one ends after IdleTimeout.
func (p *ServePool) release(proc *poolProc, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	proc.busy = false
	proc.calls++
	if err == nil {
		p.served++
	}
	if brokeUnder(err) {
		proc.lastErr = err.Error()
	}
	if !slices.Contains(p.procs, proc) {
		go proc.close(p.waitDelay())
		return
	}
	if proc.stale || p.closed || isDone(proc.dead) || brokeUnder(err) {
		p.drop(proc)
		return
	}
	if p.IdleTimeout > 0 {
		proc.idle = time.AfterFunc(p.IdleTimeout, func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if !proc.busy && slices.Contains(p.procs, proc) {
				p.drop(proc)
			}
		})
	}
}

// close ends the process: stdin closed (the server's loop ends), then the
// group killed if it has not exited within wait.
func (proc *poolProc) close(wait time.Duration) {
	if proc.stdin != nil {
		_ = proc.stdin.Close()
	}
	if proc.cmd == nil {
		return
	}
	select {
	case <-proc.dead:
	case <-time.After(wait):
		proc.kill()
	}
}

// brokeUnder reports whether a call's error means its process is no longer
// usable: not an exec hand-off, and not a cancel the process answered.
func brokeUnder(err error) bool {
	return err != nil && !errors.Is(err, errServeExec) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func isDone(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// Connections lists the pool's live processes.
func (p *ServePool) Connections() []PoolConnection {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]PoolConnection, 0, len(p.procs))
	for _, proc := range p.procs {
		c := PoolConnection{PID: proc.pid, Calls: proc.calls, Started: proc.started, Stamp: proc.stamp, LastError: proc.lastErr, State: "idle"}
		switch {
		case proc.starting:
			c.State = "starting"
		case proc.stale:
			c.State = "stale"
		case proc.busy:
			c.State = "busy"
		}
		out = append(out, c)
	}
	return out
}

// Restart drains the pool: idle processes end now, busy ones after their
// call; the next call starts a fresh one. It also clears exec-only.
func (p *ServePool) Restart() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.broken = false
	for _, proc := range slices.Clone(p.procs) {
		proc.stale = true
		if !proc.busy {
			p.drop(proc)
		}
	}
}

// Close ends every process; later calls exec.
func (p *ServePool) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.Restart()
	p.mu.Lock()
	p.broken = true
	p.mu.Unlock()
}

// exec runs the call as its own process, as RunMounted always has: the
// process group is killed when ctx ends (after WaitDelay).
func (p *ServePool) exec(ctx context.Context, call ServeCall, line func([]byte)) (ServeAnswer, error) {
	cmd := exec.CommandContext(ctx, p.Bin, call.Argv...)
	cmd.Env, cmd.Dir = call.Env, cmp.Or(call.Dir, p.Dir)
	cmd.WaitDelay = p.waitDelay()
	stop := capturedGroup(cmd)
	defer stop()
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if line != nil {
		cmd.Stdout = &lineWriter{emit: line}
	}
	err := cmd.Run()
	if w, ok := cmd.Stdout.(*lineWriter); ok {
		w.flush()
	}
	answer := ServeAnswer{Stdout: out.Bytes(), Stderr: errOut.Bytes()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return answer, ctx.Err()
	case errors.As(err, &exit):
		answer.Exit = exit.ExitCode()
	default:
		return answer, err
	}
	return answer, nil
}

// tailBuffer keeps the last bytes a process wrote to stderr.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(b), nil
}

// tail is ": LAST LINE" of stderr, or "".
func (t *tailBuffer) tail() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	text := strings.TrimSpace(string(t.buf))
	if i := strings.LastIndex(text, "\n"); i >= 0 {
		text = text[i+1:]
	}
	if text == "" {
		return ""
	}
	return ": " + text
}

// PoolWatch is a HostMount.Watch over a pool: call gives the words and env
// of a noun's watch; each stdout line is a WatchEvent; stop ends the call
// ($/cancel, or the exec's process).
func PoolWatch(p *ServePool, call func(noun string, q ochazuke.WatchQuery) ServeCall) func(string, ochazuke.WatchQuery, func(ochazuke.WatchEvent)) (func(), error) {
	return func(noun string, q ochazuke.WatchQuery, emit func(ochazuke.WatchEvent)) (func(), error) {
		ctx, cancel := context.WithCancel(context.Background())
		c := call(noun, q)
		go func() {
			_, _ = p.Watch(ctx, c, func(line []byte) {
				var e ochazuke.WatchEvent
				if json.Unmarshal(line, &e) == nil && e.Type != "" {
					emit(e)
				}
			})
		}()
		return cancel, nil
	}
}
