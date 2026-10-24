package main

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
	"github.com/unixsuperhero/ochazuke/internal/keytest"
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
	if m.Runtime.Page != "paths" {
		t.Fatal("page navigation did not open Paths")
	}
	for _, size := range [][2]int{{80, 24}, {60, 18}, {120, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(m.View().Content)
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
	if m.Runtime.Context().InputID != "attachments" || !strings.Contains(ansi.Strip(m.View().Content), "examples") {
		t.Fatalf("compact layout hid the newly focused field:\n%s", m.View().Content)
	}
	key("ctrl+s")
	if !strings.Contains(ansi.Strip(m.View().Content), "workspace saved") {
		t.Fatalf("compact layout hid action feedback:\n%s", m.View().Content)
	}
	before := m.Runtime.Context().Clone()
	key("ctrl+o")
	key("ctrl+n")
	if len(m.Runtime.Modals()) != 2 {
		t.Fatal("nested modal did not open")
	}
	key("ctrl+j")
	if m.Runtime.Page != "paths" {
		t.Fatal("sidebar navigation escaped the active modal")
	}
	key("esc")
	key("esc")
	if len(m.Runtime.Modals()) != 0 || !reflect.DeepEqual(m.Runtime.Context(), before) || !strings.Contains(ansi.Strip(m.View().Content), "examples") {
		t.Fatalf("modal close failed to restore selected field: %+v\n%s", m.Runtime.Context(), m.View().Content)
	}
	if _, err := m.Runtime.Execute(ochazuke.Request{Verb: "open", Noun: "page", Args: ochazuke.Arguments{"id": "help"}}); err != nil {
		t.Fatal(err)
	}
	initial := ansi.Strip(m.View().Content)
	key("pgdown")
	if ansi.Strip(m.View().Content) == initial || !strings.Contains(ansi.Strip(m.View().Content), "▸ Guide") {
		t.Fatal("scrolling guide moved the sidebar or failed to reveal more content")
	}
}
