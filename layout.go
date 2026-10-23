package ochazuke

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Layout renders page chrome, bounded panels and panel pairs for a viewport.
// It is a plain value: every method only renders and keeps no state.
type Layout struct {
	Width, Height int
	Styles        Styles
}

const (
	layoutGutter  = 2  // blank cells on each side of the page chrome
	layoutChrome  = 6  // title, subtitle, two rules, footer, bottom margin
	layoutStackAt = 72 // below this viewport width Split stacks its panels
)

// full reports whether the viewport has room for the complete page chrome.
func (l Layout) full() bool { return l.Width >= 12 && l.Height >= layoutChrome+2 }

// BodySize returns the exact cells available to Page's body. Viewports that
// cannot hold any body report 0, 0. Small viewports drop the header and keep
// only a footer row.
func (l Layout) BodySize() (width, height int) {
	if l.Width <= 0 || l.Height <= 0 {
		return 0, 0
	}
	if l.full() {
		return l.Width - 2*layoutGutter, l.Height - layoutChrome
	}
	if l.Height >= 3 {
		return l.Width, l.Height - 1
	}
	return l.Width, l.Height
}

// Page renders title, subtitle, body and footer into exactly Width x Height
// cells. Body is trusted ANSI; title, subtitle and footer are raw labels.
// Overlong bodies are clipped; use Panel for a visible overflow marker.
func (l Layout) Page(title, subtitle, body, footer string) string {
	if l.Width <= 0 || l.Height <= 0 {
		return ""
	}
	s := l.Styles
	bw, bh := l.BodySize()
	bodyRows := layoutRows(body, bw, bh)
	footer = s.Muted.Render(ansi.Truncate(modalLabel(footer), max(0, bw), "…"))
	var rows []string
	if l.full() {
		pad := strings.Repeat(" ", layoutGutter)
		rule := s.Muted.Render(strings.Repeat("─", bw))
		rows = append(rows,
			pad+s.Accent.Render("◆ ")+s.Title.Render(ansi.Truncate(modalLabel(title), max(0, bw-2), "…")),
			pad+s.Subtitle.Render(ansi.Truncate(modalLabel(subtitle), bw, "…")),
			pad+rule)
		for _, row := range bodyRows {
			rows = append(rows, pad+row)
		}
		rows = append(rows, pad+rule, pad+footer, "")
	} else {
		rows = append(rows, bodyRows...)
		if l.Height >= 3 {
			rows = append(rows, footer)
		}
	}
	for i := range rows {
		rows[i] = modalCells(rows[i], 0, l.Width)
	}
	return strings.Join(rows, "\n")
}

// Panel renders a rounded, titled box of exactly width x height outer cells.
// Focused panels use the heavy accent border. Body is trusted ANSI; title is a
// raw label. Content that does not fit is clipped and marked with "…" (columns)
// or a "… N more" last row (lines). Below 3x3 cells the border is dropped.
func (l Layout) Panel(title, body string, width, height int, focused bool) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	s := l.Styles
	if width < 3 || height < 3 {
		return strings.Join(layoutRows(body, width, height), "\n")
	}
	frame := s.Panel
	titleStyle := s.Title
	if focused {
		frame, titleStyle = s.FocusedPanel, s.Accent
	}
	b := frame.GetBorderStyle()
	edge := lipgloss.NewStyle().Foreground(frame.GetBorderTopForeground())
	inner := width - 2
	cw := inner
	if inner >= 4 {
		cw = inner - 2
	}
	ih := height - 2
	rows := layoutRows(layoutClip(body, ih, s), cw, ih)

	label := ""
	if inner >= 5 {
		label = " " + ansi.Truncate(modalLabel(title), inner-4, "…") + " "
	}
	fill := max(0, inner-ansi.StringWidth(label)-1)
	top := edge.Render(b.TopLeft+b.Top) + titleStyle.Render(label) + edge.Render(strings.Repeat(b.Top, fill)+b.TopRight)
	if label == "" {
		top = edge.Render(b.TopLeft + strings.Repeat(b.Top, inner) + b.TopRight)
	}
	out := make([]string, 0, height)
	out = append(out, top)
	margin := strings.Repeat(" ", (inner-cw)/2)
	for _, row := range rows {
		out = append(out, edge.Render(b.Left)+margin+row+margin+edge.Render(b.Right))
	}
	out = append(out, edge.Render(b.BottomLeft+strings.Repeat(b.Bottom, inner)+b.BottomRight))
	return strings.Join(out, "\n")
}

// Split renders two panels filling BodySize: side by side (left ~40%) when the
// viewport is at least 72 cells wide, otherwise stacked (top ~40%). The left or
// top panel is the focused one.
func (l Layout) Split(leftTitle, leftBody, rightTitle, rightBody string) string {
	bw, _ := l.BodySize()
	return l.panelPair(leftTitle, leftBody, rightTitle, rightBody, (bw-1)*2/5)
}

// Sidebar renders a 28-cell navigation rail beside the main panel. Below 72
// columns it stacks above the main panel. Items are raw labels; mainBody is
// trusted ANSI. Selection is zero-based and clamped for rendering. The caller
// owns selection and key handling; this method keeps the selected item visible.
func (l Layout) Sidebar(title string, items []string, selected int, mainTitle, mainBody string) string {
	_, height := l.BodySize()
	if l.Width < layoutStackAt {
		height = height * 2 / 5
	}
	if height >= 3 {
		height -= 2 // Panel borders.
	}
	rows := make([]string, 0, max(0, height))
	if len(items) == 0 {
		rows = append(rows, l.Styles.Muted.Render("No items."))
	} else if height > 0 {
		selected = min(max(0, selected), len(items)-1)
		visible := height
		if len(items) > height && height > 1 {
			visible-- // Keep the position hint separate from selectable rows.
		}
		start := min(max(0, selected-visible+1), max(0, len(items)-visible))
		end := min(len(items), start+visible)
		for i := start; i < end; i++ {
			label := modalLabel(items[i])
			if i == selected {
				rows = append(rows, l.Styles.Accent.Render("▸ "+label))
			} else {
				rows = append(rows, "  "+label)
			}
		}
		if len(items) > height && height > 1 {
			rows = append(rows, l.Styles.Muted.Render(fmt.Sprintf("… %d–%d / %d", start+1, end, len(items))))
		}
	}
	return l.panelPair(title, strings.Join(rows, "\n"), mainTitle, mainBody, 28)
}

func (l Layout) panelPair(leftTitle, leftBody, rightTitle, rightBody string, leftWidth int) string {
	bw, bh := l.BodySize()
	if bw <= 0 || bh <= 0 {
		return ""
	}
	if l.Width >= layoutStackAt {
		rw := bw - 1 - leftWidth
		left := strings.Split(l.Panel(leftTitle, leftBody, leftWidth, bh, true), "\n")
		right := strings.Split(l.Panel(rightTitle, rightBody, rw, bh, false), "\n")
		rows := make([]string, bh)
		for i := range rows {
			rows[i] = layoutRow(left, i, leftWidth) + " " + layoutRow(right, i, rw)
		}
		return strings.Join(rows, "\n")
	}
	th := bh * 2 / 5
	return l.Panel(leftTitle, leftBody, bw, th, true) + joinIfBoth(th, bh-th) +
		l.Panel(rightTitle, rightBody, bw, bh-th, false)
}

func joinIfBoth(a, b int) string {
	if a > 0 && b > 0 {
		return "\n"
	}
	return ""
}

func layoutRow(rows []string, i, width int) string {
	if i < len(rows) {
		return modalCells(rows[i], 0, width)
	}
	return strings.Repeat(" ", width)
}

// layoutRows returns exactly height rows of exactly width cells.
func layoutRows(body string, width, height int) []string {
	rows := make([]string, max(0, height))
	lines := strings.SplitN(strings.ReplaceAll(body, "\t", "    "), "\n", len(rows)+1)
	for i := range rows {
		line := ""
		if i < len(lines) {
			line = lines[i]
			if ansi.StringWidth(line) > width && width > 1 {
				line = ansi.Truncate(line, width, "…")
			}
		}
		rows[i] = modalCells(line, 0, width)
	}
	return rows
}

// layoutClip keeps height lines, replacing the last kept line with a count of
// the lines hidden when the body is taller.
func layoutClip(body string, height int, s Styles) string {
	lines := strings.Split(strings.ReplaceAll(body, "\t", "    "), "\n")
	if len(lines) <= height || height <= 0 {
		return strings.Join(lines, "\n")
	}
	hidden := len(lines) - height + 1
	lines = append(lines[:height-1], s.Muted.Render(fmt.Sprintf("… %d more", hidden)))
	return strings.Join(lines, "\n")
}
