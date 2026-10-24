package ochazuke

import (
	"testing"

	"ochazuke/internal/keytest"
)

func TestModelCoalescedTypingPreservesModeTransitions(t *testing.T) {
	m := NewDemo()
	_, err := m.Runtime.Execute(Request{Verb: "open", Noun: "page", Args: Arguments{"id": "playground"}})
	if err != nil {
		t.Fatal(err)
	}
	keytest.Type(m, "iabc")
	in := m.Runtime.Inputs["directory"]
	if string(in.Text) != "abc/tmp" || in.Mode != "insert" {
		t.Fatalf("coalesced iabc: text=%q mode=%q; want abc/tmp in insert mode", string(in.Text), in.Mode)
	}
	keytest.Paste(m, "ctrl+a")
	if string(in.Text) != "abcctrl+a/tmp" {
		t.Fatalf("paste interpreted as command: %q", string(in.Text))
	}
}

func TestModelCoalescedPaletteSearchNavigatesPage(t *testing.T) {
	m := NewDemo()
	if err := m.Palette.Open(); err != nil {
		t.Fatal(err)
	}
	typed := func(s string) { keytest.Type(m, s) }
	enter := func() { keytest.Press(m, "enter") }
	typed("open")
	enter()
	typed("page")
	enter()
	typed("playground")
	enter()
	enter()
	if m.Runtime.ActivePage != "playground" || len(m.Runtime.Frames) != 1 {
		t.Fatalf("coalesced palette navigation: page=%q frames=%d; want playground with palette closed", m.Runtime.ActivePage, len(m.Runtime.Frames))
	}
}
