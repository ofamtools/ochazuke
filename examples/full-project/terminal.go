package main

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

// echoStream is the example's pane: an in-process line buffer that echoes
// what the terminal instance sends to it and reports a bar cursor after the
// prompt. A real application puts its daemon client here; the framework
// sees only the ochazuke.TerminalStream interface. Next runs off the loop
// (a tea.Cmd), Send, Paste and Resize run on it, so the buffer is locked.
type echoStream struct {
	mu     sync.Mutex
	lines  []string
	input  string
	cols   int
	rows   int
	dirty  bool
	notify chan struct{}
}

func newEchoStream() *echoStream {
	return &echoStream{
		lines:  []string{"Keys typed here are echoed back; Enter repeats the line.", "ctrl+a d detaches, ctrl+a : opens the palette, ctrl+a ? lists the keys."},
		cols:   80,
		rows:   24,
		dirty:  true,
		notify: make(chan struct{}, 1),
	}
}

// Next returns the pane's frame when it has changed since the last read and
// otherwise waits for a change or the context.
func (s *echoStream) Next(ctx context.Context) (ochazuke.TerminalFrame, error) {
	for {
		s.mu.Lock()
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

// frame renders the last rows that fit, clipped to the pane's columns, with
// the cursor after the prompt. The caller holds the lock.
func (s *echoStream) frame() ochazuke.TerminalFrame {
	all := append(slices.Clone(s.lines), "$ "+s.input)
	start := max(0, len(all)-s.rows)
	visible := slices.Clone(all[start:])
	for i, line := range visible {
		visible[i] = ansi.Truncate(line, s.cols, "")
	}
	cursor := ochazuke.Pos{X: min(ansi.StringWidth("$ "+s.input), max(0, s.cols-1)), Y: len(visible) - 1}
	return ochazuke.TerminalFrame{Lines: visible, Cursor: cursor, Shape: "bar"}
}

func (s *echoStream) change(fn func()) {
	s.mu.Lock()
	fn()
	s.dirty = true
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// Send appends printable keys to the line, Enter echoes the line, Backspace
// deletes a rune and any other key name is shown in angle brackets.
func (s *echoStream) Send(keys ...string) error {
	s.change(func() {
		for _, key := range keys {
			switch {
			case key == "enter":
				s.lines = append(s.lines, "$ "+s.input, "echo: "+s.input)
				s.input = ""
			case key == "backspace":
				if n := len(s.input); n > 0 {
					_, size := utf8.DecodeLastRuneInString(s.input)
					s.input = s.input[:n-size]
				}
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

func (s *echoStream) Paste(text string) error {
	s.change(func() { s.input += strings.ReplaceAll(text, "\n", " ") })
	return nil
}

func (s *echoStream) Resize(cols, rows int) error {
	s.change(func() { s.cols, s.rows = max(1, cols), max(1, rows) })
	return nil
}

// terminalPageView composes the pane in a panel under a status row, with the
// page's hint text beneath. The pane's cursor is composed only while the
// terminal is focused; the shell then shows the real cursor there.
func terminalPageView(r *ochazuke.Runtime, term *ochazuke.Terminal, spec PageConfig, styles ochazuke.Styles, width, height int) ochazuke.Block {
	if width <= 0 || height <= 0 {
		return ochazuke.Block{}
	}
	state := "detached · Enter attaches"
	if term.Focused() {
		state = "attached · keys go to the pane"
	}
	status := ansi.Truncate(fmt.Sprintf("Pane: %s · %s", term.ID(), state), width, "…")
	if err := term.Err(); err != "" {
		status = ansi.Truncate("Pane error: "+plain(err), width, "…")
	}
	hints := strings.Split(ansi.Wrap(plain(spec.Text), width, ""), "\n")
	hints = hints[:min(len(hints), min(2, max(0, height-2)))]
	bodyHeight := height - 1 - len(hints)
	rows := []ochazuke.Block{ochazuke.Text(styles.Muted.Render(status))}
	if bodyHeight > 0 {
		cw, ch := ochazuke.PanelContentSize(width, bodyHeight)
		layout := ochazuke.Layout{Styles: styles}
		panel := layout.PanelBlock("Console", term.ViewBlock(cw, ch), width, bodyHeight, term.Focused())
		if !term.Focused() {
			panel.Cursor = nil
		}
		rows = append(rows, panel)
	}
	for _, hint := range hints {
		rows = append(rows, ochazuke.Text(hint))
	}
	return stack(rows...)
}
