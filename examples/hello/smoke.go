package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/cli"
	"ofam.dev/oden"
)

// smoke renders the Commands page headless: it lists the inline commands
// and an external on a PATH of its own, runs count 3 in a real pty
// (re-executing this program), detaches, shows the external's help and
// prints the frame of the finished run.
func smoke() error {
	dir, err := os.MkdirTemp("", "hello-smoke")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	script, err := os.ReadFile(filepath.Join("examples", "hello", "bin", "hello-web"))
	if err != nil {
		script = []byte("#!/bin/sh\necho \"usage: hello web [PORT]\"\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "hello-web"), script, 0o755); err != nil {
		return err
	}
	app := newApp(nil, oden.WithPath(dir))
	m, c, err := cli.NewCommandsModel(app, cli.CommandsConfig{})
	if err != nil {
		return err
	}
	if _, err := m.Runtime.ValidateBindings(); err != nil {
		return err
	}
	m.Width, m.Height = 100, 22
	m.Appearance.Theme = "mono"
	p := newPump(m)
	if _, err := p.until("the commands", "greet", "count", "web", "external"); err != nil {
		return err
	}
	if err := c.List().Select("command:count"); err != nil {
		return err
	}
	if _, err := p.exec(ochazuke.Request{Noun: "command", Verb: "run", Args: ochazuke.Arguments{"args": "3"}}); err != nil {
		return err
	}
	if m.Runtime.Mode.Name != "terminal" {
		return fmt.Errorf("run left the mode %q, want terminal", m.Runtime.Mode.Name)
	}
	running, err := p.until("count running", "count · running · attached", "1")
	if err != nil {
		return err
	}
	frame, err := p.until("count finishing", "count · exited 0", "3")
	if err != nil {
		return err
	}
	p.key("ctrl+a", "d")
	if m.Runtime.Focus != "command:count" || m.Runtime.Mode.Name != "normal" {
		return fmt.Errorf("detach left focus %q mode %q", m.Runtime.Focus, m.Runtime.Mode.Name)
	}
	if err := c.List().Select("command:web"); err != nil {
		return err
	}
	if _, err := p.exec(ochazuke.Request{Noun: "command", Verb: "help"}); err != nil {
		return err
	}
	if _, err := p.until("web's help", "web · help", "usage: hello web [PORT]"); err != nil {
		return err
	}
	fmt.Println(running)
	fmt.Println()
	fmt.Println(frame)
	fmt.Println("hello smoke: commands page lists inline and PATH commands, run attaches a pty, ctrl+a d detaches, help shows an external's --help: PASS")
	return nil
}

// pump drives a model headless: commands run on goroutines and their
// messages are delivered in arrival order until a frame shows what is
// awaited.
type pump struct {
	m    *ochazuke.Model
	msgs chan tea.Msg
}

func newPump(m *ochazuke.Model) *pump {
	p := &pump{m: m, msgs: make(chan tea.Msg, 256)}
	p.run(m.Runtime.PendingCommands())
	return p
}

func (p *pump) run(cmd tea.Cmd) {
	if cmd != nil {
		go func() { p.push(cmd()) }()
	}
}

func (p *pump) push(msg tea.Msg) {
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			p.run(c)
		}
		return
	}
	if msg != nil {
		p.msgs <- msg
	}
}

func (p *pump) update(msg tea.Msg) {
	_, cmd := p.m.Update(msg)
	p.run(cmd)
}

func (p *pump) key(names ...string) {
	for _, name := range names {
		p.update(keyMsg(name))
	}
}

func (p *pump) exec(q ochazuke.Request) (any, error) {
	v, err := p.m.Runtime.Execute(q)
	p.run(p.m.Runtime.PendingCommands())
	return v, err
}

func (p *pump) until(what string, want ...string) (string, error) {
	deadline := time.After(10 * time.Second)
	for {
		frame := ansi.Strip(p.m.View().Content)
		ok := true
		for _, w := range want {
			ok = ok && strings.Contains(frame, w)
		}
		if ok {
			return frame, nil
		}
		select {
		case msg := <-p.msgs:
			p.update(msg)
		case <-deadline:
			return "", errors.New(what + ": not seen; frame:\n" + frame)
		}
	}
}

// keyMsg is the key press a binding name stands for: ctrl+<letter> or one
// printable character.
func keyMsg(name string) tea.Msg {
	if letter, ok := strings.CutPrefix(name, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(letter[0]), Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(name[0]), Text: name}
}
