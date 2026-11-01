package ochazuke

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (p *Picker) View(width, height int) string { return p.ViewBlock(width, height).Text }

// ViewBlock renders the picker and reports the cursor of its path editor on
// the first row, after the label.
func (p *Picker) ViewBlock(width, height int) Block {
	if width <= 0 || height <= 0 {
		return Block{}
	}
	label := fmt.Sprintf("Path [%s]: ", p.Filter)
	if width < 24 {
		label = "Path: "
	}
	label = ansi.Truncate(label, max(0, width-1), "")
	// The path line's right end says why nothing can match (an invalid
	// regex) and the match mode (search.show-match), when there is room.
	var right []string
	if err := p.matcher().Err(); err != nil {
		right = append(right, err.Error())
	}
	if tag := p.Runtime.matchTagFor(p.MatchMode()); tag != "" {
		right = append(right, tag)
	}
	tail := strings.Join(right, "  ")
	editorWidth := width - ansi.StringWidth(label)
	if tail != "" && editorWidth-ansi.StringWidth(tail)-1 >= 12 {
		editorWidth -= ansi.StringWidth(tail) + 1
	} else {
		tail = ""
	}
	editor := InputBlock(p.input, editorWidth, 1).Offset(ansi.StringWidth(label), 0).Clip(width, 1)
	first := label + editor.Text
	if tail != "" {
		first = searchLine(first, tail, width)
	}
	rows := []string{first}
	if height == 1 {
		return editor.WithText(strings.Join(layoutRows(rows[0], width, height), "\n"))
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
		split := "│"
		if styles := p.Runtime.Settings().page.styles; styles != nil {
			split = styles().chromeRender("border.split", split)
		}
		for i := range left {
			rows = append(rows, left[i]+split+right[i])
		}
	}
	rows = append(rows, helpRows...)
	return editor.WithText(strings.Join(layoutRows(strings.Join(rows, "\n"), width, height), "\n"))
}

// pickerColumns are the picker results table: the path takes the room the
// size and modified columns leave, and those drop (modified first) when
// the results pane is narrow. Size and modified have fixed widths, so a
// stat landing (picker_stats.go) never moves a column.
var pickerColumns = []Column{
	{Title: "path", Min: 16},
	{Title: "size", Width: 6, Align: AlignRight, Priority: 1},
	{Title: "modified", Width: 16, Priority: 2},
}

// pickerCells is an entry's row of the results table; size and modified
// stay blank until the entry's stat has landed (picker_stats.go).
func pickerCells(entry PickerEntry, stat pickerStat) []string {
	name := strings.ReplaceAll(entry.Path, "\n", "\\n")
	if name == "." {
		name = ". (current directory)"
	} else if entry.Dir {
		name += "/"
	}
	size, modified := "", ""
	if stat.ok {
		if !entry.Dir {
			size = pickerSize(stat.size)
		}
		modified = stat.modified.Local().Format("2006-01-02 15:04")
	}
	return []string{safeText(name), size, modified}
}

// pickerSize is a byte count in at most six cells: 512, 1.5K, 34K, 2.0M.
func pickerSize(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10)
	}
	value := float64(n)
	for _, unit := range []string{"K", "M", "G", "T"} {
		value /= 1024
		if value < 1024 || unit == "T" {
			if value < 10 {
				return fmt.Sprintf("%.1f%s", value, unit)
			}
			return fmt.Sprintf("%.0f%s", value, unit)
		}
	}
	return ""
}

// resultRows draws the results as a table (pickerColumns): the count line,
// the header, then the rows in the window around the selection, the
// selected row marked "> " and reversed across the width.
func (p *Picker) resultRows(width, height int) []string {
	// The rows this frame can draw, which the stat job follows.
	p.drawnRows = max(0, height-min(2, max(0, height-1)))
	if len(p.Entries) == 0 {
		return layoutRows("(no matching paths)", width, height)
	}
	var rows []string
	if height > 1 {
		rows = append(rows, fmt.Sprintf("Results %d/%d", p.Selected+1, len(p.Entries)))
	}
	// The path's natural width is the whole row, so it takes every cell
	// the other columns leave and the columns stay put while scrolling.
	shown, widths := tableLayoutOf(pickerColumns, []int{width, 0, 0}, max(1, width))
	if height > 2 {
		titles := make([]string, len(pickerColumns))
		for i, c := range pickerColumns {
			titles[i] = c.Title
		}
		head := strings.TrimRight(tableLine(pickerColumns, titles, shown, widths), " ")
		rows = append(rows, lipgloss.NewStyle().Bold(true).Render(ansi.Truncate(head, width, "…")))
	}
	start, end := p.window(p.drawnRows)
	for i := start; i < end; i++ {
		stat, _ := p.stats.get(p.statKey(p.Entries[i]))
		line := tableLine(pickerColumns, pickerCells(p.Entries[i], stat), shown, widths)
		row := modalCells(ansi.Truncate(line, width, "…"), width)
		if i == p.Selected {
			row = p.Runtime.Selection(true).Render(row)
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

// bindingHelp is the picker's rows as hints, derived the way the footer is.
func (p *Picker) bindingHelp() string {
	if !p.IsOpen() {
		return ""
	}
	var hints []string
	for _, binding := range p.Runtime.ListBindings(p.input.ID) {
		if binding.Request.Noun != "ochazuke.picker" {
			continue
		}
		hints = append(hints, rowHint(p.Runtime, binding))
	}
	return strings.Join(hints, " · ")
}
