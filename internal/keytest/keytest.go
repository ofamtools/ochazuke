// Package keytest drives Bubble Tea models from tests using the key names that
// bindings use ("ctrl+c", "esc", "enter", "shift+tab", "alt+b", "space", "x").
// Msg is the only place the test suite spells out a Bubble Tea key message, so
// a Bubble Tea upgrade changes this file and nothing else.
package keytest

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Model is the part of a Bubble Tea model that receives keys.
type Model interface {
	Update(tea.Msg) (tea.Model, tea.Cmd)
}

// Msg builds the Bubble Tea v1 message for one key. A name with an "alt+"
// prefix sets the Alt flag; a name Bubble Tea knows ("esc", "ctrl+j", "space")
// becomes that key type; anything else is literal runes. Literal text skips
// the name lookup, so Msg("ctrl+s", true, false) types the six characters.
// Multi-rune literal text is one coalesced message, as a terminal sends fast
// typing or a paste; Model.Update splits coalesced typing, never a paste.
func Msg(name string, literal, paste bool) tea.KeyMsg {
	if literal || paste {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name), Paste: paste}
	}
	msg := tea.KeyMsg{Type: tea.KeyRunes}
	if rest, ok := strings.CutPrefix(name, "alt+"); ok && rest != "" {
		msg.Alt, name = true, rest
	}
	if t, ok := keyTypes[name]; ok {
		msg.Type = t
		return msg
	}
	msg.Runes = []rune(name)
	return msg
}

// Press sends one named key, such as "ctrl+x", "esc" or "q", and returns the
// command the model produced.
func Press(m Model, name string) tea.Cmd {
	_, cmd := m.Update(Msg(name, false, false))
	return cmd
}

// Type sends text as one coalesced literal message, the way a terminal
// delivers fast typing. The model splits it into single keys.
func Type(m Model, text string) tea.Cmd {
	_, cmd := m.Update(Msg(text, true, false))
	return cmd
}

// Paste sends text as one bracketed paste event.
func Paste(m Model, text string) tea.Cmd {
	_, cmd := m.Update(Msg(text, false, true))
	return cmd
}

// keyTypes maps every name Bubble Tea v1 prints for a non-rune key back to its
// type, derived from tea.Key.String so the names match what Model.Update sees.
var keyTypes = func() map[string]tea.KeyType {
	names := map[string]tea.KeyType{"space": tea.KeySpace}
	for t := tea.KeyType(-128); t <= 127; t++ {
		if t == tea.KeyRunes || t == tea.KeySpace {
			continue
		}
		if s := (tea.Key{Type: t}).String(); s != "" {
			names[s] = t
		}
	}
	return names
}()
