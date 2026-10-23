package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"ochazuke"
)

func TestKeybindingHistory(t *testing.T) {
	m := &keybindingModel{width: 80, height: 24}
	for range keyHistorySize {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a"), Alt: true})
	view := m.View()
	newest := strings.Index(view, `202  "alt+a"`)
	previous := strings.Index(view, `201  "tab"`)
	if newest < 0 || previous <= newest {
		t.Fatalf("expected normalized keys newest first after history wraps:\n%s", view)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Ctrl-C did not exit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("Ctrl-C did not issue a quit message")
	}
}

func TestSandboxBindings(t *testing.T) {
	m := &keybindingModel{width: 100, height: 24, runtime: ochazuke.NewRuntime()}
	for _, spec := range []string{"ctrl+s=save", "g g=go-top", "x=old", "x=new"} {
		if err := m.addBinding(spec); err != nil {
			t.Fatal(err)
		}
	}
	for _, step := range []struct {
		key  tea.KeyMsg
		want string
	}{
		{tea.KeyMsg{Type: tea.KeyCtrlS}, "ACTION: save"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")}, "unmatched"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}, "pending: g"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}, "ACTION: go-top"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("gg")}, "ACTION: go-top"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}, "pending: g"},
		{tea.KeyMsg{Type: tea.KeyEsc}, "sequence cancelled"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}, "ACTION: new"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Paste: true}, "paste (not dispatched)"},
	} {
		m.Update(step.key)
		if got := m.results[(m.count-1)%keyHistorySize]; got != step.want {
			t.Fatalf("key %q: got %q, want %q", step.key.String(), got, step.want)
		}
		if !strings.Contains(m.View(), step.want) {
			t.Fatalf("missing visible outcome %q", step.want)
		}
	}
}

func TestSandboxRejectsInvalidBindings(t *testing.T) {
	for _, spec := range []string{"", "x", "=action", "x=", "ctrl+c=quit", "g ctrl+c=quit"} {
		m := &keybindingModel{runtime: ochazuke.NewRuntime()}
		if err := m.addBinding(spec); err == nil {
			t.Fatalf("accepted invalid binding %q", spec)
		}
	}
}
