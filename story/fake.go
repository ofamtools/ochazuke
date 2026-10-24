package story

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

// FakeStream is a pane with no process behind it: it echoes what the
// terminal sends and prints a numbered output line per Emit (the TUI emits
// one a second; a script asks for one with the "output" tick). Every change
// queues one frame and Next hands them out in order, so the frames a
// session sees depend only on the order of its events, never on timing.
type FakeStream struct {
	mu       sync.Mutex
	lines    []string
	input    string
	outputs  int
	rows     int
	queue    []ochazuke.TerminalFrame
	notify   chan struct{}
	done     <-chan struct{}
	isClosed bool
}

func newFakeStream(done <-chan struct{}) *FakeStream {
	s := &FakeStream{lines: []string{"fake pane: keys echo here; ctrl+a d detaches"}, notify: make(chan struct{}, 1), done: done}
	s.change(func() {})
	return s
}

// Next returns the oldest queued frame, waiting for one.
func (s *FakeStream) Next(ctx context.Context) (ochazuke.TerminalFrame, error) {
	for {
		s.mu.Lock()
		if len(s.queue) > 0 {
			f := s.queue[0]
			s.queue = s.queue[1:]
			s.mu.Unlock()
			return f, nil
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ochazuke.TerminalFrame{}, ctx.Err()
		case <-s.done:
			return ochazuke.TerminalFrame{}, context.Canceled
		case <-s.notify:
		}
	}
}

func (s *FakeStream) frame() ochazuke.TerminalFrame {
	all := append(slices.Clone(s.lines), "$ "+s.input)
	if s.rows > 0 && len(all) > s.rows {
		all = all[len(all)-s.rows:]
	}
	return ochazuke.TerminalFrame{Lines: all, Cursor: ochazuke.Pos{X: ansi.StringWidth("$ " + s.input), Y: len(all) - 1}, Shape: "bar"}
}

func (s *FakeStream) change(fn func()) {
	s.mu.Lock()
	fn()
	s.queue = append(s.queue, s.frame())
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// Send echoes keys: printable ones join the line, enter prints it, any
// other key name is shown in angle brackets.
func (s *FakeStream) Send(keys ...string) error {
	s.change(func() {
		for _, key := range keys {
			switch {
			case key == "enter":
				s.lines = append(s.lines, "$ "+s.input, "echo: "+s.input)
				s.input = ""
			case key == "space":
				s.input += " "
			case utf8.RuneCountInString(key) == 1:
				s.input += key
			default:
				s.input += "<" + key + ">"
			}
		}
	})
	return nil
}

func (s *FakeStream) Paste(text string) error {
	s.change(func() { s.input += strings.ReplaceAll(text, "\n", " ") })
	return nil
}

// Resize keeps the row count for later frames and queues nothing, so the
// frames do not depend on when the pane was first drawn.
func (s *FakeStream) Resize(cols, rows int) error {
	s.mu.Lock()
	s.rows = rows
	s.mu.Unlock()
	return nil
}

// Emit prints the next output line.
func (s *FakeStream) Emit() {
	s.change(func() {
		s.outputs++
		s.lines = append(s.lines, fmt.Sprintf("output %d", s.outputs))
	})
}

// Release marks the pane gone (a CloseOnDetach detach releases it).
func (s *FakeStream) Release() error {
	s.mu.Lock()
	s.isClosed = true
	s.mu.Unlock()
	return nil
}

func (s *FakeStream) released() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isClosed
}
