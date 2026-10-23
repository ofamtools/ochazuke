package ochazuke

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

func TestDemoPageKeysDoNotStealInsertEditing(t *testing.T) {
	m := NewDemo()
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if m.Runtime.ActivePage != "playground" {
		t.Fatalf("Ctrl-J page = %q", m.Runtime.ActivePage)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}, Alt: true})
	if m.Runtime.ActivePage != "playground" {
		t.Fatal("Alt-1 still switches pages")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("iabc")})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if got := string(m.Runtime.Inputs["directory"].Text); got != "abc" {
		t.Fatalf("insert Ctrl-K text = %q, want abc", got)
	}
	if m.Runtime.ActivePage != "playground" {
		t.Fatal("insert Ctrl-K switched page")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if m.Runtime.ActivePage != "overview" {
		t.Fatalf("normal Ctrl-K page = %q", m.Runtime.ActivePage)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":open")})
	if !m.Palette.IsActive() || m.Palette.query != "open" {
		t.Fatalf("colon search: active=%v query=%q", m.Palette.IsActive(), m.Palette.query)
	}
}
