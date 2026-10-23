package ochazuke

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestModelCoalescedTypingPreservesModeTransitions(t *testing.T) {
	m := NewDemo()
	_, err := m.Runtime.Execute(Request{Verb: "open", Noun: "page", Args: Arguments{"id": "playground"}})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("iabc")})
	in := m.Runtime.Inputs["directory"]
	if string(in.Text) != "abc/tmp" || in.Mode != "insert" {
		t.Fatalf("coalesced iabc: text=%q mode=%q; want abc/tmp in insert mode", string(in.Text), in.Mode)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ctrl+a"), Paste: true})
	if string(in.Text) != "abcctrl+a/tmp" {
		t.Fatalf("paste interpreted as command: %q", string(in.Text))
	}
}

func TestModelCoalescedPaletteSearchNavigatesPage(t *testing.T) {
	m := NewDemo()
	if err := m.Palette.Open(); err != nil {
		t.Fatal(err)
	}
	typed := func(s string) { m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}) }
	enter := func() { m.Update(tea.KeyMsg{Type: tea.KeyEnter}) }
	typed("open")
	enter()
	typed("/page")
	enter()
	typed("playground")
	enter()
	enter()
	if m.Runtime.ActivePage != "playground" || len(m.Runtime.Frames) != 1 {
		t.Fatalf("coalesced palette navigation: page=%q frames=%d; want playground with palette closed", m.Runtime.ActivePage, len(m.Runtime.Frames))
	}
}
