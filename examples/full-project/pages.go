package main

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/unixsuperhero/ochazuke"
)

func plain(text string) string {
	return strings.Map(func(ch rune) rune {
		if unicode.IsControl(ch) && ch != '\n' && ch != '\t' {
			return -1
		}
		return ch
	}, ansi.Strip(text))
}

func registerPages(m *ochazuke.Model, specs []PageConfig) error {
	for _, spec := range specs {
		if spec.Renderer != "workspace" && spec.Renderer != "text" && spec.Renderer != "list" && spec.Renderer != "terminal" {
			return fmt.Errorf("page %q: unknown renderer %q", spec.ID, spec.Renderer)
		}
		for _, id := range spec.Inputs {
			path := m.Runtime.InstancePath(id)
			if len(path) < 2 || path[1] != spec.ID {
				return fmt.Errorf("page %q: input %q is unknown or belongs to another page", spec.ID, id)
			}
		}
		focusedInput := m.Runtime.Input(spec.Focus) != nil
		if focusedInput && !slices.Contains(spec.Inputs, spec.Focus) {
			return fmt.Errorf("page %q: focused input must appear in inputs", spec.ID)
		}
		if spec.Renderer == "list" && (len(spec.Inputs) != 1 || focusedInput) {
			return fmt.Errorf("list page %q requires one companion input and a list-focused start", spec.ID)
		}
		// A terminal page starts on its pane, which it creates under the page
		// fed by the application's stream; the framework owns no PTY.
		var term *ochazuke.Terminal
		if spec.Renderer == "terminal" {
			if len(spec.Inputs) != 0 || spec.Focus == "" || m.Runtime.Instances[spec.Focus] != nil {
				return fmt.Errorf("terminal page %q requires a fresh pane id as focus and no inputs", spec.ID)
			}
			var err error
			m.Runtime.EnsureTerminalLeader()
			term, err = m.Runtime.AddTerminal(spec.Focus, newEchoStream(), ochazuke.Parent(spec.ID))
			if err != nil {
				return fmt.Errorf("page %q: %w", spec.ID, err)
			}
		}
		var list *ochazuke.List
		if spec.Renderer == "list" {
			var err error
			list, err = m.Runtime.AddList(spec.Focus, spec.Items, ochazuke.Parent(spec.ID))
			if err != nil {
				return fmt.Errorf("page %q: %w", spec.ID, err)
			}
		}
		render := func(r *ochazuke.Runtime, width, height int) ochazuke.Block {
			styles := ochazuke.NewStyles(*m.Appearance)
			if spec.Renderer == "text" {
				return ochazuke.Text(ansi.Wrap(plain(spec.Text), max(1, width), ""))
			}
			if spec.Renderer == "list" {
				return listPageView(r, list, spec, styles, width, height)
			}
			if spec.Renderer == "terminal" {
				return terminalPageView(r, term, spec, styles, width, height)
			}
			return workspaceView(r, r.PageFocus(spec.ID), spec, styles, width, height)
		}
		if _, err := m.Runtime.AddPage(spec.ID, spec.Title, render, ochazuke.Focus(spec.Focus)); err != nil {
			return fmt.Errorf("page %q: %w", spec.ID, err)
		}
	}
	return nil
}

// workspaceState is the example's per-page application state: the last
// action, summaries, greetings and inspections the renderers show. It is
// application data, so it lives with the application, not on a frame.
var workspaceState = map[string]map[string]any{}

func pageState(id string) map[string]any {
	if workspaceState[id] == nil {
		workspaceState[id] = map[string]any{}
	}
	return workspaceState[id]
}

// workspaceView keeps all fields visible when they fit. Small windows show the
// focused field instead; Tab still visits every configured field in order.
func workspaceView(r *ochazuke.Runtime, focus string, spec PageConfig, styles ochazuke.Styles, width, height int) ochazuke.Block {
	var status []string
	state := pageState(spec.ID)
	for _, key := range []string{"lastAction", "summary", "greeting", "inspection"} {
		if value, ok := state[key]; ok {
			status = append(status, styles.Accent.Render(ansi.Truncate(plain(fmt.Sprint(value)), width, "…")))
		}
	}
	selected, minimum := 0, 2+len(status)
	for i, id := range spec.Inputs {
		minimum += 4 // Three panel rows and a separator.
		if r.Input(id).Multiline {
			minimum++
		}
		if focus == id {
			selected = i
		}
	}
	minimum-- // No separator after the last field.
	inputs := spec.Inputs
	compact := len(inputs) > 0 && minimum > height
	var rows []ochazuke.Block
	if compact {
		inputs = inputs[selected : selected+1]
		rows = append(rows, ochazuke.Text(styles.Subtitle.Render(ansi.Truncate(fmt.Sprintf("Field %d/%d · Tab / Shift-Tab", selected+1, len(spec.Inputs)), width, "…"))))
	} else {
		rows = append(rows, ochazuke.Text(styles.Muted.Render(ansi.Truncate(plain(spec.Text), width, "…"))), ochazuke.Text(""))
	}
	extra := max(0, height-minimum)
	layout := ochazuke.Layout{Styles: styles}
	for i, id := range inputs {
		input := r.Input(id)
		panelHeight := 3
		if input.Multiline {
			panelHeight = 4 + min(4, extra)
			extra -= panelHeight - 4
		}
		if compact {
			panelHeight = max(1, height-1-len(status))
		}
		contentWidth, contentHeight := ochazuke.PanelContentSize(width, panelHeight)
		label := id
		if input.Kind == "path" || input.Kind == "paths" {
			label += " · " + input.PathFilter
		}
		// Only the focused field carries a cursor; the panel moves it inside its border.
		focused := focus == id
		body := ochazuke.InputBlock(input, contentWidth, contentHeight)
		if !focused {
			body.Cursor = nil
		}
		rows = append(rows, layout.PanelBlock(label, body, width, panelHeight, focused))
		if i < len(inputs)-1 {
			rows = append(rows, ochazuke.Text(""))
		}
	}
	for _, line := range status {
		rows = append(rows, ochazuke.Text(line))
	}
	return stack(rows...)
}
