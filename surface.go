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
	outerWidth := min(width-2, max(1, width/5*4+width%5*4/5))
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
	return ComposeModalBlock(background, title, Text(body), footer, width, height, styles).Text
}

// ComposeModalBlock is ComposeModal for a body that may carry a cursor. The
// result's cursor is the body's, moved to where the panel placed it; the
// backdrop never contributes one because a modal covers whatever it held.
func ComposeModalBlock(background, title string, body Block, footer string, width, height int, styles Styles) Block {
	return composeModalAt(background, title, body, footer, nil, width, height, styles, nil)
}

// composeModalAt is ComposeModalBlock with the panel placed by place (given
// the panel's size, its top-left cell) instead of centered when place is
// not nil.
//
// styled, when not nil, draws the footer in the panel's width as the
// chrome colours a hint line (Styles.hintLine); its ok false, or a nil
// styled, draws footer as one muted label. footer still sizes the panel.
func composeModalAt(background, title string, body Block, footer string, styled func(width int) (string, bool), width, height int, styles Styles, place func(panelWidth, panelHeight int) (left, top int)) Block {
	contentWidth, contentHeight, bordered := modalBounds(width, height)
	if contentWidth == 0 || contentHeight == 0 {
		if width <= 0 || height <= 0 {
			return Block{}
		}
		return Text(fitSurface(background, width, height))
	}
	title, footer = modalLabel(title), modalLabel(footer)
	lines := strings.SplitN(strings.ReplaceAll(body.Text, "\t", "    "), "\n", contentHeight+1)
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
		lines[i] = modalCells(line, panelWidth)
	}
	cursor := body.Clip(panelWidth, len(lines))
	if bordered {
		rows := make([]string, 0, len(lines)+2)
		rows = append(rows, styles.Title.Render(modalCells(ansi.Truncate(title, panelWidth, "…"), panelWidth)))
		rows = append(rows, lines...)
		foot := styles.Muted.Render(modalCells(ansi.Truncate(footer, panelWidth, "…"), panelWidth))
		if styled != nil {
			if line, ok := styled(panelWidth); ok {
				foot = modalCells(line, panelWidth)
			}
		}
		rows = append(rows, foot)
		panel := lipgloss.NewStyle().Foreground(styles.Panel.GetForeground()).Border(lipgloss.RoundedBorder()).BorderForeground(styles.Chrome("border.modal", lipgloss.NewStyle().Foreground(styles.Accent.GetForeground())).GetForeground()).Padding(0, 1)
		lines = strings.Split(panel.Render(strings.Join(rows, "\n")), "\n")
		panelWidth += 4
		// Border and padding on the left; border and title row above.
		cursor = cursor.Offset(2, 2)
	}
	left, top := (width-panelWidth)/2, (height-len(lines))/2
	if place != nil {
		left, top = place(panelWidth, len(lines))
	}
	// The backdrop is drawn dimmed (Styles.Backdrop, modal-dim) as one layer and the panel as a second layer
	// on top; the canvas owns cell clipping, including wide graphemes under the
	// panel's edges.
	if styles.backdropPlain {
		background = unhighlight(background)
	}
	backdrop := dim(fitSurface(strings.ReplaceAll(background, "\t", "    "), width, height), styles.Backdrop)
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(backdrop),
		lipgloss.NewLayer(strings.Join(lines, "\n")).X(left).Y(top).Z(1),
	))
	// Canvas.Render trims trailing blanks; every row must stay exactly width cells.
	text := fitSurface(canvas.Render(), width, height)
	return cursor.Offset(left, top).WithText(text)
}

// dim draws styled text in a dimming style without dropping its own
// styles: the style's attributes open every line and are opened again after
// every reset inside it, so a reverse-video cursor or an underlined
// selection beneath a modal, or in an unfocused list, stays visible, dimmed.
func dim(text string, muted lipgloss.Style) string {
	sample := muted.Render("x")
	open := sample[:strings.Index(sample, "x")]
	if open == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+open)
		line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+open)
		lines[i] = open + line + "\x1b[m"
	}
	return strings.Join(lines, "\n")
}

// unhighlight drops reverse video and underline (SGR 7, 27, 4, 24: a
// list's cursor bar, a table's current cell) from text's escape
// sequences, colour parameters (38/48/58 with 5;N or 2;R;G;B) kept whole:
// the backdrop under modal-highlights plain. Adapted from
// o-json's unreverse (detail.go).
func unhighlight(text string) string {
	if !strings.Contains(text, "\x1b[") {
		return text
	}
	var out strings.Builder
	for {
		i := strings.Index(text, "\x1b[")
		if i < 0 {
			out.WriteString(text)
			return out.String()
		}
		j := i + 2
		for j < len(text) && (text[j] >= '0' && text[j] <= '9' || text[j] == ';' || text[j] == ':') {
			j++
		}
		if j >= len(text) || text[j] != 'm' {
			out.WriteString(text[:j])
			text = text[j:]
			continue
		}
		out.WriteString(text[:i])
		params := strings.Split(text[i+2:j], ";")
		var kept []string
		for k := 0; k < len(params); k++ {
			p := params[k]
			switch {
			case p == "7" || p == "27" || p == "4" || p == "24" || strings.HasPrefix(p, "4:"):
				continue
			case (p == "38" || p == "48" || p == "58") && k+1 < len(params):
				n := 2
				if params[k+1] == "2" {
					n = 4
				}
				end := min(len(params), k+1+n)
				kept = append(kept, params[k:end]...)
				k = end - 1
				continue
			}
			kept = append(kept, p)
		}
		if len(kept) > 0 || len(params) == 1 && params[0] == "" {
			out.WriteString("\x1b[" + strings.Join(kept, ";") + "m")
		}
		text = text[j+1:]
	}
}

// modalCells pads or truncates a line to exactly width cells. Trusted styles
// are closed so an unclosed SGR cannot color the adjacent cells.
func modalCells(line string, width int) string {
	return fitCells(line, width, "")
}

func modalLabel(label string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, ansi.Strip(label))
}
