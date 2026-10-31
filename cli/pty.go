package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/xpty"
	"github.com/unixsuperhero/ochazuke"
)

// ErrReleased is what Next returns once the stream was released.
var ErrReleased = errors.New("pty stream released")

// PtyStream is an ochazuke.TerminalStream for a local process: the command
// runs in a pseudo-terminal (xpty), its output is replayed into a terminal
// emulator (an Emulator of the engine it was started with, tuios.dev/vt
// by default), and every frame is the emulator's screen with its cursor
// cell, shape and visibility. Keys arrive as the binding names a terminal
// instance forwards ("ctrl+a", "enter", "up") and are encoded by the
// emulator, so a program in application cursor mode gets the sequences it
// asked for; a paste is bracketed when the program enabled bracketed paste.
// The emulator's replies to the program (device attributes, cursor
// reports) go back into the pty. Release (Terminal.Close) hangs up the
// process group, kills what is left of it and closes the pty, once.
//
// The root package owns no PTY; this stream is the application side of the
// contract, for tools that run their own commands (the Commands page).
type PtyStream struct {
	mu       sync.Mutex
	emu      Emulator
	pty      xpty.Pty
	cmd      *exec.Cmd
	cols     int
	rows     int
	dirty    bool
	notify   chan struct{}
	out      *byteQueue
	exited   bool
	exitErr  error
	released bool
	done     chan struct{}
	once     sync.Once
	rel      error
}

// StartPty starts cmd in a new pseudo-terminal of cols x rows cells (80x24
// when either is not positive) as the leader of its own session, so
// Release reaches every process it started. cmd must not be started and
// must leave Stdin, Stdout, Stderr and SysProcAttr unset: the pty is all
// three and the session is the stream's. The emulator is
// DefaultTerminalEngine; StartPtyWith picks another.
func StartPty(cmd *exec.Cmd, cols, rows int) (*PtyStream, error) {
	return StartPtyWith(cmd, cols, rows, PtyOptions{})
}

// PtyOptions are StartPtyWith's choices.
type PtyOptions struct {
	// Engine names the emulator (a TerminalEngine row; "" is
	// DefaultTerminalEngine). An application passes
	// TerminalEngineOf(runtime), the terminal.engine setting.
	Engine string
}

// StartPtyWith is StartPty with options. An engine this build cannot run
// fails before the command starts.
func StartPtyWith(cmd *exec.Cmd, cols, rows int, opts PtyOptions) (*PtyStream, error) {
	if cmd == nil {
		return nil, errors.New("command is required")
	}
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	emu, err := NewEmulator(opts.Engine, cols, rows)
	if err != nil {
		return nil, err
	}
	p, err := xpty.NewPty(cols, rows)
	if err != nil {
		_ = emu.Close()
		return nil, fmt.Errorf("open pty: %w", err)
	}
	configureCommand(cmd)
	if err := p.Start(cmd); err != nil {
		_ = p.Close()
		_ = emu.Close()
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	// The parent's copy of the terminal side would keep the pty open after
	// the process exits, so reading would never see the end of its output.
	if u, ok := p.(*xpty.UnixPty); ok {
		_ = u.Slave().Close()
	}
	s := &PtyStream{
		emu: emu, pty: p, cmd: cmd, cols: cols, rows: rows,
		dirty: true, notify: make(chan struct{}, 1), out: newByteQueue(), done: make(chan struct{}),
	}
	go s.replies()
	go s.input()
	go s.output()
	return s, nil
}

// output feeds the program's output to the emulator until the pty ends,
// then reaps the process; the stream is exited only after both, so the
// last frame holds all of the output.
func (s *PtyStream) output() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.change(func() { s.write(buf[:n]) })
		}
		if err != nil {
			break
		}
	}
	err := s.cmd.Wait()
	s.change(func() { s.exited, s.exitErr = true, err })
	close(s.done)
}

// write feeds bytes to the emulator; a panic inside it on bytes it cannot
// parse is kept off the program's loop. The caller holds the lock.
func (s *PtyStream) write(p []byte) {
	defer func() { _ = recover() }()
	_, _ = s.emu.Write(p)
}

// replies drains what the emulator answers the program into the queue the
// input goroutine writes to the pty, so the emulator never blocks on a
// program that is not reading its input.
func (s *PtyStream) replies() {
	buf := make([]byte, 4096)
	for {
		n, err := s.emu.Read(buf)
		if n > 0 {
			s.out.push(buf[:n])
		}
		if err != nil {
			s.out.close()
			return
		}
	}
}

// input writes queued keys, pastes and replies to the pty.
func (s *PtyStream) input() {
	for {
		b, ok := s.out.pop()
		if !ok {
			return
		}
		if _, err := s.pty.Write(b); err != nil {
			s.out.close()
			return
		}
	}
}

// change runs fn under the lock, marks the screen changed and wakes Next.
func (s *PtyStream) change(fn func()) {
	s.mu.Lock()
	fn()
	s.dirty = true
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// Next returns the screen when it changed since the last read, otherwise
// waits for a change or ctx. After the process exits the last screen stays
// and later changes (a resize) still arrive.
func (s *PtyStream) Next(ctx context.Context) (ochazuke.TerminalFrame, error) {
	for {
		s.mu.Lock()
		if s.released {
			s.mu.Unlock()
			return ochazuke.TerminalFrame{}, ErrReleased
		}
		if s.dirty {
			s.dirty = false
			frame := s.frame()
			s.mu.Unlock()
			return frame, nil
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ochazuke.TerminalFrame{}, ctx.Err()
		case <-s.notify:
		}
	}
}

// frame is the emulator's screen and cursor. The caller holds the lock.
// The caller holds the lock. The frame carries Status, so a read that only
// reports the exit still redraws what shows it.
func (s *PtyStream) frame() ochazuke.TerminalFrame {
	f := EmulatorFrame(s.emu)
	f.Status = s.status()
	return f
}

// Send encodes each key name and writes it to the program. An unknown name
// fails before anything is sent.
func (s *PtyStream) Send(keys ...string) error {
	events := make([]uv.KeyPressEvent, 0, len(keys))
	for _, key := range keys {
		ev, err := keyEvent(key)
		if err != nil {
			return err
		}
		events = append(events, ev)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.released {
		return ErrReleased
	}
	for _, ev := range events {
		s.emu.SendKey(ev)
	}
	return nil
}

// Paste writes text to the program, bracketed when it asked for that.
func (s *PtyStream) Paste(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.released {
		return ErrReleased
	}
	s.emu.Paste(text)
	return nil
}

// Resize sizes the pty (the program gets SIGWINCH) and the emulator.
func (s *PtyStream) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	var err error
	s.change(func() {
		if s.released || cols == s.cols && rows == s.rows {
			return
		}
		s.cols, s.rows = cols, rows
		s.emu.Resize(cols, rows)
		err = s.pty.Resize(cols, rows)
	})
	return err
}

// Release ends the process group (a hangup, then a kill for what is left),
// closes the pty and stops the emulator, once; later calls return the
// first call's error. It is ochazuke.TerminalReleaser.
func (s *PtyStream) Release() error {
	s.once.Do(func() {
		s.mu.Lock()
		s.released = true
		s.mu.Unlock()
		select {
		case <-s.done:
		default:
			endGroup(s.cmd, s.done)
		}
		s.rel = closePty(s.pty)
		// Closing the emulator ends the replies goroutine (its Read sees
		// io.EOF); the input goroutine ends on the queue's close.
		s.mu.Lock()
		_ = s.emu.Close()
		s.mu.Unlock()
		s.out.close()
		select {
		case s.notify <- struct{}{}:
		default:
		}
	})
	return s.rel
}

// closePty closes the pty's master side; StartPty already closed the
// parent's copy of the terminal side of a Unix pty.
func closePty(p xpty.Pty) error {
	if u, ok := p.(*xpty.UnixPty); ok {
		return u.Master().Close()
	}
	return p.Close()
}

// Done is closed when the process has exited and its output is read.
func (s *PtyStream) Done() <-chan struct{} { return s.done }

// Status is "running", "exited 0", "exited 2", or how the process ended
// otherwise ("signal: interrupt").
func (s *PtyStream) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status()
}

// status is Status; the caller holds the lock.
func (s *PtyStream) status() string {
	if !s.exited {
		return "running"
	}
	var ee *exec.ExitError
	switch {
	case s.exitErr == nil:
		return "exited 0"
	case errors.As(s.exitErr, &ee) && ee.ExitCode() >= 0:
		return fmt.Sprintf("exited %d", ee.ExitCode())
	}
	return s.exitErr.Error()
}

// Exited reports whether the process has exited, and its Wait error.
func (s *PtyStream) Exited() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited, s.exitErr
}

// Pid is the process id of the command (its process group's id).
func (s *PtyStream) Pid() int { return s.cmd.Process.Pid }

// byteQueue is an unbounded FIFO of byte slices: writers never block.
type byteQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  [][]byte
	closed bool
}

func newByteQueue() *byteQueue {
	q := &byteQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *byteQueue) push(b []byte) {
	q.mu.Lock()
	if !q.closed {
		q.items = append(q.items, append([]byte(nil), b...))
		q.cond.Signal()
	}
	q.mu.Unlock()
}

func (q *byteQueue) pop() ([]byte, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if q.closed {
		return nil, false
	}
	b := q.items[0]
	q.items = q.items[1:]
	return b, true
}

func (q *byteQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}
