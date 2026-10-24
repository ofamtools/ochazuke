package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"ochazuke"
)

func TestKeybindingHistory(t *testing.T) {
	m := &keybindingModel{width: 80, height: 24, sandbox: true}
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
	m := &keybindingModel{width: 100, height: 24, sandbox: true, runtime: ochazuke.NewRuntime()}
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

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestSandboxTimeoutResolvesLongestFallback(t *testing.T) {
	m := &keybindingModel{width: 100, height: 24, sandbox: true, runtime: ochazuke.NewRuntime()}
	m.runtime.BindingTimeout = 5 * time.Millisecond
	for _, spec := range []string{"g=single", "g g=double", "q q=quit-seq", "k=other"} {
		if err := m.addBinding(spec); err != nil {
			t.Fatal(err)
		}
	}
	_, cmd := m.Update(runeKey("g"))
	if cmd == nil || m.results[0] != "pending: g" {
		t.Fatalf("ambiguous g should defer with a timer: cmd=%v result=%q", cmd != nil, m.results[0])
	}
	msg, ok := cmd().(ochazuke.BindingTimeoutMsg)
	if !ok {
		t.Fatal("timer did not produce a BindingTimeoutMsg")
	}
	m.Update(msg)
	if m.count != 1 || m.results[0] != "ACTION: single" {
		t.Fatalf("timeout should run the fallback in the same row: count=%d result=%q", m.count, m.results[0])
	}
	// A pure prefix times out and clears without inventing a key row.
	_, cmd = m.Update(runeKey("q"))
	m.Update(cmd().(ochazuke.BindingTimeoutMsg))
	if m.count != 2 || m.results[1] != "sequence timed out" || m.runtime.Pending != "" {
		t.Fatalf("pure prefix timeout: count=%d result=%q pending=%q", m.count, m.results[1], m.runtime.Pending)
	}
	// Mismatch with a completed fallback consumes the key.
	m.runtime.BindingTimeout = time.Minute
	m.Update(runeKey("g"))
	m.Update(runeKey("k"))
	if m.results[3] != "ACTION: single" {
		t.Fatalf("mismatch should run fallback: %q", m.results[3])
	}
	// The earlier timer is stale now and must not change anything.
	count, results := m.count, m.results
	m.Update(msg)
	if m.count != count || m.results != results {
		t.Fatal("stale timeout changed history or outcome")
	}
}

func TestSandboxCoalescedKeysBatchTimers(t *testing.T) {
	m := &keybindingModel{width: 100, height: 24, sandbox: true, runtime: ochazuke.NewRuntime()}
	for _, spec := range []string{"g=single", "g g=double"} {
		if err := m.addBinding(spec); err != nil {
			t.Fatal(err)
		}
	}
	_, cmd := m.Update(runeKey("g"))
	if cmd == nil {
		t.Fatal("single ambiguous key should schedule a timer")
	}
	_, cmd = m.Update(runeKey("gg"))
	if cmd == nil || m.results[1] != "ACTION: double" || m.results[2] != "pending: g" {
		t.Fatalf("coalesced keys lost their commands or results: %v", m.results[:3])
	}
}

func TestParseBindingTimeout(t *testing.T) {
	for in, want := range map[string]time.Duration{"400ms": 400 * time.Millisecond, " 1s ": time.Second} {
		if got, err := parseBindingTimeout(in); err != nil || got != want {
			t.Fatalf("%q: got %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "abc", "0s", "-1s", "400"} {
		if _, err := parseBindingTimeout(in); err == nil {
			t.Fatalf("%q should be rejected", in)
		}
	}
	if err := runKeybinding([]string{"--timeout", "0s"}); err == nil {
		t.Fatal("--timeout 0s should be rejected")
	}
}

func TestSandboxRejectsInvalidBindings(t *testing.T) {
	for _, spec := range []string{"", "x", "=action", "x=", "ctrl+c=quit", "g ctrl+c=quit", "ctrl+g=reserved"} {
		m := &keybindingModel{runtime: ochazuke.NewRuntime()}
		if err := m.addBinding(spec); err == nil {
			t.Fatalf("accepted invalid binding %q", spec)
		}
	}
}

func TestSandboxResizeKeepsLatestOutcomeVisible(t *testing.T) {
	m := &keybindingModel{width: 80, height: 24, sandbox: true, runtime: ochazuke.NewRuntime()}
	if err := m.addBinding("x=save"); err != nil {
		t.Fatal(err)
	}
	m.Update(runeKey("x"))
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	view := m.View()
	latest, _, found := strings.Cut(view, "History")
	if !found || !strings.Contains(latest, "ACTION: save") {
		t.Fatalf("stacked layout hid the latest outcome:\n%s", view)
	}
}
