package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
