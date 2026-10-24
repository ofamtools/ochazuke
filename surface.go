package ochazuke

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ModalSize returns safe body bounds, reserving the same chrome as ComposeModal.
// Modal chrome uses one horizontal padding cell regardless of page density.
func ModalSize(width, height int) (contentWidth, contentHeight int) {
	contentWidth, contentHeight, _ = modalBounds(width, height)
	return
}

func modalBounds(width, height int) (contentWidth, contentHeight int, bordered bool) {
	if width < 3 || height < 3 {
		return 0, 0, false
	}
	outerWidth := min(78, width-2, max(1, width/5*4+width%5*4/5))
	outerHeight := min(height-2, max(1, height/4*3+height%4*3/4))
	if outerWidth >= 5 && outerHeight >= 5 {
		// Two borders, two horizontal padding cells, title and footer rows.
		return outerWidth - 4, outerHeight - 4, true
	}
	return outerWidth, outerHeight, false
}

// ComposeModal centers a content-sized panel over the existing page, not a blank
// screen. Body and backdrop may contain trusted ANSI; title and footer are raw labels.
func ComposeModal(background, title, body, footer string, width, height int, styles Styles) string {
	contentWidth, contentHeight, bordered := modalBounds(width, height)
	if contentWidth == 0 || contentHeight == 0 {
		if width <= 0 || height <= 0 {
			return ""
		}
		return fitSurface(background, width, height)
	}
	title, footer = modalLabel(title), modalLabel(footer)
	body = strings.ReplaceAll(body, "\t", "    ")
	lines := strings.SplitN(body, "\n", contentHeight+1)
	overflow := len(lines) > contentHeight
	if overflow {
		lines = lines[:contentHeight]
		lines[len(lines)-1] = styles.Muted.Render("… more")
	}
	panelWidth := 1
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, contentWidth, "…")
		panelWidth = max(panelWidth, ansi.StringWidth(lines[i]))
	}
	if bordered {
		panelWidth = min(contentWidth, max(panelWidth, ansi.StringWidth(title), ansi.StringWidth(footer)))
	}
	for i, line := range lines {
		lines[i] = modalCells(line, 0, panelWidth)
	}
	if bordered {
		rows := make([]string, 0, len(lines)+2)
		rows = append(rows, styles.Title.Render(modalCells(ansi.Truncate(title, panelWidth, "…"), 0, panelWidth)))
		rows = append(rows, lines...)
		rows = append(rows, styles.Muted.Render(modalCells(ansi.Truncate(footer, panelWidth, "…"), 0, panelWidth)))
		panel := lipgloss.NewStyle().Foreground(styles.Panel.GetForeground()).Border(lipgloss.RoundedBorder()).BorderForeground(styles.Accent.GetForeground()).Padding(0, 1)
		lines = strings.Split(panel.Render(strings.Join(rows, "\n")), "\n")
		panelWidth += 4
	}
	left, top := (width-panelWidth)/2, (height-len(lines))/2
	backdrop := strings.SplitN(strings.ReplaceAll(background, "\t", "    "), "\n", height+1)
	out := make([]string, height)
	for row := range out {
		line := ""
		if row < len(backdrop) {
			line = backdrop[row]
		}
		if row >= top && row < top+len(lines) {
			out[row] = styles.Muted.Render(modalCells(line, 0, left)) +
				lines[row-top] + styles.Muted.Render(modalCells(line, left+panelWidth, width))
		} else {
			out[row] = styles.Muted.Render(modalCells(line, 0, width))
		}
	}
	return strings.Join(out, "\n")
}

// modalCells pads a cell slice exactly. x/ansi retains a wide grapheme crossing
// the left cut; replace that partial cell with a blank instead of shifting text.
func modalCells(line string, left, right int) string {
	width := right - left
	if width <= 0 {
		return ""
	}
	prefix := ""
	if left > 0 && ansi.StringWidth(line) > left && ansi.StringWidth(ansi.Truncate(line, left, "")) < left {
		prefix = " "
		left++
	}
	text := prefix + ansi.Truncate(ansi.Cut(line, left, right), width-len(prefix), "")
	// Isolate trusted styles so an unclosed SGR cannot color the adjacent panel.
	if strings.ContainsRune(text, '\x1b') {
		text += "\x1b[0m"
	}
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}

func modalLabel(label string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, ansi.Strip(label))
}
