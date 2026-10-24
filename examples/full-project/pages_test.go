package main

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"ochazuke"
	"ochazuke/internal/keytest"
)

func TestResponsiveFieldsAndModalRestoration(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	m, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	key := func(k string) { keytest.Press(m, k) }
	key("ctrl+j")
	if m.Runtime.ActivePage != "paths" {
		t.Fatal("page navigation did not open Paths")
	}
	for _, size := range [][2]int{{80, 24}, {60, 18}, {120, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "▸ Paths") || !strings.Contains(view, "go.mod") {
			t.Fatalf("sidebar or selected field disappeared:\n%s", view)
		}
		rows := strings.Split(view, "\n")
		if len(rows) != size[1] {
			t.Fatalf("got %d rows, want %d", len(rows), size[1])
		}
		for _, row := range rows {
			if ansi.StringWidth(row) != size[0] {
				t.Fatalf("row escaped viewport: %q", row)
			}
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 18})
	key("tab")
	key("tab")
	if m.Runtime.Context().InputID != "attachments" || !strings.Contains(ansi.Strip(m.View()), "examples") {
		t.Fatalf("compact layout hid the newly focused field:\n%s", m.View())
	}
	key("ctrl+s")
	if !strings.Contains(ansi.Strip(m.View()), "workspace saved") {
		t.Fatalf("compact layout hid action feedback:\n%s", m.View())
	}
	before := m.Runtime.Context().Clone()
	key("ctrl+o")
	key("ctrl+n")
	if len(m.Runtime.Frames) != 3 {
		t.Fatal("nested modal did not open")
	}
	key("ctrl+j")
	if m.Runtime.ActivePage != "paths" {
		t.Fatal("sidebar navigation escaped the active modal")
	}
	key("esc")
	key("esc")
	if len(m.Runtime.Frames) != 1 || !reflect.DeepEqual(m.Runtime.Context(), before) || !strings.Contains(ansi.Strip(m.View()), "examples") {
		t.Fatalf("modal close failed to restore selected field: %+v\n%s", m.Runtime.Context(), m.View())
	}
	if _, err := m.Runtime.Execute(ochazuke.Request{Verb: "open", Noun: "page", Args: ochazuke.Arguments{"id": "help"}}); err != nil {
		t.Fatal(err)
	}
	initial := ansi.Strip(m.View())
	key("pgdown")
	if ansi.Strip(m.View()) == initial || !strings.Contains(ansi.Strip(m.View()), "▸ Guide") {
		t.Fatal("scrolling guide moved the sidebar or failed to reveal more content")
	}
}
