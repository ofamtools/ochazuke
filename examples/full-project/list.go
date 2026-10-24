package main

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"ochazuke"
)

func listPageView(r *ochazuke.Runtime, list *ochazuke.List, spec PageConfig, styles ochazuke.Styles, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	frame := r.Pages[spec.ID].Frame
	selected, ok := list.Selected()
	id := "none"
	if ok {
		id = selected.ID
	}
	path := frame.Context.Path
	status := ansi.Truncate(fmt.Sprintf("Selected: %s · Active: %s", id, path[len(path)-1]), width, "…")
	hints := strings.Split(ansi.Wrap(plain(spec.Text), width, ""), "\n")
	hints = hints[:min(len(hints), min(2, max(0, height-2)))]
	bodyHeight := height - 1 - len(hints)
	layout := ochazuke.Layout{Styles: styles}
	listPanel := func(w, h int) string {
		cw, ch := ochazuke.PanelContentSize(w, h)
		return layout.Panel("Sample items", list.View(cw, ch), w, h, list.Focused())
	}
	input := r.Inputs[spec.Inputs[0]]
	editorPanel := func(w, h int) string {
		cw, ch := ochazuke.PanelContentSize(w, h)
		var rows []string
		if inspection, ok := frame.State["inspection"].(map[string]any); ok && ch > 1 {
			if inspected, ok := inspection["path"].([]string); ok && len(inspected) > 0 {
				rows = append(rows, ansi.Truncate("Inspected: "+inspected[len(inspected)-1], cw, "…"))
			}
		}
		rows = append(rows, ochazuke.InputView(input, cw, max(0, ch-len(rows))))
		return layout.Panel("Readline note", strings.Join(rows, "\n"), w, h, r.Context().InputID == input.ID)
	}
	var body string
	if width >= 72 {
		leftWidth := (width - 1) * 2 / 5
		body = lipgloss.JoinHorizontal(lipgloss.Top, listPanel(leftWidth, bodyHeight), " ", editorPanel(width-leftWidth-1, bodyHeight))
	} else if bodyHeight >= 8 {
		topHeight := bodyHeight / 2
		body = listPanel(width, topHeight) + "\n" + editorPanel(width, bodyHeight-topHeight)
	} else if frame.Context.InputID == "" {
		body = listPanel(width, bodyHeight)
	} else {
		body = editorPanel(width, bodyHeight)
	}
	rows := []string{status}
	if bodyHeight > 0 {
		rows = append(rows, body)
	}
	rows = append(rows, hints...)
	return strings.Join(rows, "\n")
}
