package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
)

func TestKeybindingHistory(t *testing.T) {
	m := &keybindingModel{width: 80, height: 24, sandbox: true}
	for range keyHistorySize {
		keytest.Type(m, "x")
	}
	keytest.Press(m, "tab")
	keytest.Press(m, "alt+a")
	// Lip Gloss v2 renders styles unconditionally; Bubble Tea downsamples on output.
	view := ansi.Strip(m.View().Content)
	newest := strings.Index(view, `202  "alt+a"`)
	previous := strings.Index(view, `201  "tab"`)
	if newest < 0 || previous <= newest {
		t.Fatalf("expected normalized keys newest first after history wraps:\n%s", view)
	}
	cmd := keytest.Press(m, "ctrl+c")
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
		key   string
		paste bool
		want  string
	}{
		{"ctrl+s", false, "ACTION: save"},
		{"z", false, "unmatched"},
		{"g", false, "pending: g"},
		{"g", false, "ACTION: go-top"},
		{"gg", false, "ACTION: go-top"},
		{"g", false, "pending: g"},
		{"esc", false, "sequence cancelled"},
		{"x", false, "ACTION: new"},
		{"x", true, "paste (not dispatched)"},
	} {
		if step.paste {
			keytest.Paste(m, step.key)
		} else {
			keytest.Press(m, step.key)
		}
		if got := m.results[(m.count-1)%keyHistorySize]; got != step.want {
			t.Fatalf("key %q: got %q, want %q", step.key, got, step.want)
		}
		if !strings.Contains(m.View().Content, step.want) {
			t.Fatalf("missing visible outcome %q", step.want)
		}
	}
}

func TestSandboxTimeoutResolvesLongestFallback(t *testing.T) {
	m := &keybindingModel{width: 100, height: 24, sandbox: true, runtime: ochazuke.NewRuntime()}
	m.runtime.BindingTimeout = 5 * time.Millisecond
	for _, spec := range []string{"g=single", "g g=double", "q q=quit-seq", "k=other"} {
		if err := m.addBinding(spec); err != nil {
			t.Fatal(err)
		}
	}
	cmd := keytest.Type(m, "g")
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
	cmd = keytest.Type(m, "q")
	m.Update(cmd().(ochazuke.BindingTimeoutMsg))
	if m.count != 2 || m.results[1] != "sequence timed out" || m.runtime.Pending != "" {
		t.Fatalf("pure prefix timeout: count=%d result=%q pending=%q", m.count, m.results[1], m.runtime.Pending)
	}
	// Mismatch with a completed fallback runs the fallback, then the
	// mismatched key is dispatched fresh: its row records the later action.
	m.runtime.BindingTimeout = time.Minute
	keytest.Type(m, "g")
	keytest.Type(m, "k")
	if m.results[3] != "ACTION: other" {
		t.Fatalf("mismatch should run the fallback and then the key: %q", m.results[3])
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
	cmd := keytest.Type(m, "g")
	if cmd == nil {
		t.Fatal("single ambiguous key should schedule a timer")
	}
	cmd = keytest.Type(m, "gg")
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
	keytest.Type(m, "x")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	view := m.View().Content
	latest, _, found := strings.Cut(view, "History")
	if !found || !strings.Contains(latest, "ACTION: save") {
		t.Fatalf("stacked layout hid the latest outcome:\n%s", view)
	}
}
