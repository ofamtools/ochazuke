package ochazuke

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (p *Picker) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	label := fmt.Sprintf("Path [%s]: ", p.Filter)
	if width < 24 {
		label = "Path: "
	}
	label = ansi.Truncate(label, max(0, width-1), "")
	rows := []string{label + InputView(p.input, width-ansi.StringWidth(label), 1)}
	if height == 1 {
		return strings.Join(layoutRows(rows[0], width, height), "\n")
	}
	help := p.bindingHelp()
	var helpRows []string
	if help != "" {
		helpRows = strings.Split(ansi.Wrap(help, width, ""), "\n")
		helpRows = helpRows[:min(len(helpRows), min(3, max(0, height-2)))]
	}
	if p.Loading && height-len(helpRows)-len(rows) > 1 {
		rows = append(rows, "Indexing/searching…")
	}
	if p.Error != "" && height-len(helpRows)-len(rows) > 1 {
		rows = append(rows, "Error: "+safeText(p.Error))
	}
	bodyHeight := height - len(rows) - len(helpRows)
	if width < 50 {
		listHeight := bodyHeight
		if bodyHeight >= 3 && p.preview.path != "" {
			listHeight--
		}
		rows = append(rows, p.resultRows(width, listHeight)...)
		if listHeight < bodyHeight {
			rows = append(rows, modalLabel(filepath.Base(p.preview.path)+" · "+p.preview.metadata))
		}
	} else {
		leftWidth := max(22, width*2/5)
		left := p.resultRows(leftWidth, bodyHeight)
		right := p.previewRows(width-leftWidth-1, bodyHeight)
		for i := range left {
			rows = append(rows, left[i]+"│"+right[i])
		}
	}
	rows = append(rows, helpRows...)
	return strings.Join(layoutRows(strings.Join(rows, "\n"), width, height), "\n")
}

func (p *Picker) resultRows(width, height int) []string {
	if len(p.Entries) == 0 {
		return layoutRows("(no matching paths)", width, height)
	}
	var rows []string
	if height > 1 {
		rows = append(rows, fmt.Sprintf("Results %d/%d", p.Selected+1, len(p.Entries)))
	}
	limit := max(0, height-len(rows))
	start := max(0, p.Selected-limit+1)
	end := min(len(p.Entries), start+limit)
	for i := start; i < end; i++ {
		entry := p.Entries[i]
		name := strings.ReplaceAll(entry.Path, "\n", "\\n")
		if name == "." {
			name = ". (current directory)"
		} else if entry.Dir {
			name += "/"
		}
		mark := "  "
		if i == p.Selected {
			mark = "> "
		}
		row := modalCells(ansi.Truncate(mark+safeText(name), width, "…"), 0, width)
		if i == p.Selected {
			row = lipgloss.NewStyle().Reverse(true).Render(row)
		}
		rows = append(rows, row)
	}
	return layoutRows(strings.Join(rows, "\n"), width, height)
}

func (p *Picker) previewRows(width, height int) []string {
	if p.preview.path == "" {
		return layoutRows("No selection", width, height)
	}
	rows := []string{modalLabel(filepath.Base(p.preview.path)), p.preview.metadata}
	switch {
	case p.preview.loading:
		rows = append(rows, "Loading preview…")
	case p.preview.err != "":
		rows = append(rows, "Preview: "+safeText(p.preview.err))
	default:
		limit := max(0, height-len(rows))
		lines := strings.SplitN(p.preview.text, "\n", limit+1)
		for _, line := range lines[:min(limit, len(lines))] {
			rows = append(rows, safeText(line))
		}
	}
	return layoutRows(strings.Join(rows, "\n"), width, height)
}

func (p *Picker) bindingHelp() string {
	if !p.IsOpen() {
		return ""
	}
	var hints []string
	for _, binding := range p.Runtime.EffectiveBindings(p.frame.Context) {
		request := binding.Request
		if request.Noun != "picker" {
			continue
		}
		label := ""
		switch request.Verb {
		case "enter":
			label = "open/select"
		case "choose":
			label = "confirm"
			if current, _ := request.Args["current"].(bool); current {
				label = "choose root"
			}
		case "close":
			label = "cancel"
		case "complete":
			label = "complete"
		case "parent":
			label = "parent"
		case "descend":
			label = "enter folder"
		case "move":
			delta := IntArg(request.Args, "delta", 0)
			if delta == 1 {
				label = "next"
			} else if delta == -1 {
				label = "previous"
			} else {
				label = fmt.Sprintf("move %+d", delta)
			}
		}
		if label != "" {
			hints = append(hints, modalLabel(binding.Keys)+" "+label)
		}
	}
	return strings.Join(hints, " · ")
}
