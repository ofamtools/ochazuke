package main

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"ochazuke"
)

func listPageView(r *ochazuke.Runtime, list *ochazuke.List, spec PageConfig, styles ochazuke.Styles, width, height int) ochazuke.Block {
	if width <= 0 || height <= 0 {
		return ochazuke.Block{}
	}
	frame := r.PageOf(spec.ID).Frame
	selected, ok := list.Selected()
	id := "none"
	if ok {
		id = selected.ID
	}
	noun := "none"
	if ok {
		noun = selected.Noun
	}
	state := pageState(spec.ID)
	status := fmt.Sprintf("Selected: %s (%s) · Active: %s", id, noun, frame.Focus)
	if last, ok := state["lastAction"].(string); ok {
		status += " · " + last
	}
	status = ansi.Truncate(status, width, "…")
	hints := strings.Split(ansi.Wrap(plain(spec.Text), width, ""), "\n")
	hints = hints[:min(len(hints), min(2, max(0, height-2)))]
	bodyHeight := height - 1 - len(hints)
	layout := ochazuke.Layout{Styles: styles}
	listPanel := func(w, h int) ochazuke.Block {
		cw, ch := ochazuke.PanelContentSize(w, h)
		return ochazuke.Text(layout.Panel("Sample items", list.View(cw, ch), w, h, list.Focused()))
	}
	input := r.Inputs[spec.Inputs[0]]
	editorPanel := func(w, h int) ochazuke.Block {
		cw, ch := ochazuke.PanelContentSize(w, h)
		var rows []ochazuke.Block
		if inspection, ok := state["inspection"].(map[string]any); ok && ch > 1 {
			if inspected, ok := inspection["path"].([]string); ok && len(inspected) > 0 {
				rows = append(rows, ochazuke.Text(ansi.Truncate("Inspected: "+inspected[len(inspected)-1], cw, "…")))
			}
		}
		focused := r.Context().InputID == input.ID
		editor := ochazuke.InputBlock(input, cw, max(0, ch-len(rows)))
		if !focused {
			editor.Cursor = nil
		}
		return layout.PanelBlock("Readline note", stack(append(rows, editor)...), w, h, focused)
	}
	var body ochazuke.Block
	if width >= 72 {
		leftWidth := (width - 1) * 2 / 5
		body = beside(" ", listPanel(leftWidth, bodyHeight), editorPanel(width-leftWidth-1, bodyHeight))
	} else if bodyHeight >= 8 {
		topHeight := bodyHeight / 2
		body = stack(listPanel(width, topHeight), editorPanel(width, bodyHeight-topHeight))
	} else if r.Inputs[frame.Focus] == nil {
		body = listPanel(width, bodyHeight)
	} else {
		body = editorPanel(width, bodyHeight)
	}
	rows := []ochazuke.Block{ochazuke.Text(status)}
	if bodyHeight > 0 {
		rows = append(rows, body)
	}
	for _, hint := range hints {
		rows = append(rows, ochazuke.Text(hint))
	}
	return stack(rows...)
}

// stack joins blocks top to bottom, moving each cursor down by the rows above it.
func stack(blocks ...ochazuke.Block) ochazuke.Block {
	var out ochazuke.Block
	texts := make([]string, 0, len(blocks))
	row := 0
	for _, b := range blocks {
		if b.Cursor != nil && out.Cursor == nil {
			out = b.Offset(0, row)
		}
		texts = append(texts, b.Text)
		row += strings.Count(b.Text, "\n") + 1
	}
	out.Text = strings.Join(texts, "\n")
	return out
}

// beside joins blocks left to right on their top rows, moving each cursor
// right by the widths of the blocks and gaps before it.
func beside(gap string, blocks ...ochazuke.Block) ochazuke.Block {
	var out ochazuke.Block
	texts := make([]string, 0, len(blocks)*2)
	column := 0
	for i, b := range blocks {
		if i > 0 {
			texts = append(texts, gap)
			column += lipgloss.Width(gap)
		}
		if b.Cursor != nil && out.Cursor == nil {
			out = b.Offset(column, 0)
		}
		texts = append(texts, b.Text)
		column += lipgloss.Width(b.Text)
	}
	out.Text = lipgloss.JoinHorizontal(lipgloss.Top, texts...)
	return out
}

// bindingsView lists the rows the originating focus answers to, under the
// instance and noun that own it, so the same modal reads differently per row.
func bindingsView(r *ochazuke.Runtime, focus string, width int) string {
	if focus == "" {
		return "(no originating context)"
	}
	noun := "input"
	if inst := r.Instances[focus]; inst != nil && inst.Noun != "" {
		noun = inst.Noun
	}
	lines := []string{fmt.Sprintf("Focus: %s · noun: %s · mode: %s", focus, noun, r.Mode(r.Context()))}
	for _, b := range r.ListBindings(focus) {
		key := b.Keys
		if key == "" {
			key = "<" + b.Pattern + ">"
		}
		line := fmt.Sprintf("%-12s %s.%s", key, b.Request.Verb, b.Request.Noun)
		if len(b.Request.Args) > 0 {
			line += fmt.Sprintf(" %v", map[string]any(b.Request.Args))
		}
		lines = append(lines, ansi.Truncate(line, max(1, width), "…"))
	}
	return strings.Join(lines, "\n")
}

// modalOrigins remembers the focus each configured modal was opened from, so
// the bindings modal can describe it.
var modalOrigins = map[string]string{}

func registerModals(m *ochazuke.Model, specs []ModalConfig) error {
	for _, spec := range specs {
		if strings.TrimSpace(spec.ID) == "" || m.ModalViews["modal:"+spec.ID].Render != nil {
			return fmt.Errorf("modal %q is empty or duplicate", spec.ID)
		}
		if spec.Renderer != "text" && spec.Renderer != "confirm" && spec.Renderer != "bindings" {
			return fmt.Errorf("modal %q: unknown renderer %q", spec.ID, spec.Renderer)
		}
		if spec.Input != "" && m.Runtime.Inputs[spec.Input] == nil {
			return fmt.Errorf("modal %q: unknown input %q", spec.ID, spec.Input)
		}
		if spec.Renderer == "confirm" && spec.Input == "" {
			return fmt.Errorf("modal %q: confirm renderer requires an input", spec.ID)
		}
		m.ModalViews["modal:"+spec.ID] = ochazuke.ModalView{Title: spec.Title, Render: func(r *ochazuke.Runtime, frame *ochazuke.Frame, w, h int) ochazuke.Block {
			body := ochazuke.Text(ansi.Wrap(plain(spec.Text), max(1, w), ""))
			if spec.Renderer == "bindings" {
				body = stack(body, ochazuke.Text(""), ochazuke.Text(bindingsView(r, modalOrigins[frame.ID], w)))
			}
			if spec.Renderer == "confirm" {
				inputHeight := max(1, h-strings.Count(body.Text, "\n")-3)
				body = stack(body, ochazuke.Text(""), ochazuke.InputBlock(r.Inputs[frame.Focus], w, inputHeight))
			}
			return body
		}}
	}
	return nil
}
