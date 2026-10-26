package ochazuke

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
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

// BodySizeWith is BodySize less the rows the footer's jobs ticker takes
// (only with the complete chrome; a small viewport draws no ticker).
func (l Layout) BodySizeWith(f Footer) (width, height int) {
	width, height = l.BodySize()
	return width, height - l.jobRows(f)
}

// jobRows is how many ticker rows the shell draws: none without the
// complete chrome, and never the body's last row.
func (l Layout) jobRows(f Footer) int {
	if !l.full() {
		return 0
	}
	_, height := l.BodySize()
	return max(0, min(len(f.Jobs), height-1))
}

// Page renders title, subtitle, body and footer into exactly Width x Height
// cells. Body is trusted ANSI; title, subtitle and footer are raw labels.
// Overlong bodies are clipped; use Panel for a visible overflow marker.
func (l Layout) Page(title, subtitle, body, footer string) string {
	return l.PageBlock(title, subtitle, Text(body), footer).Text
}

// PageBlock is Page for a body that may carry a cursor; the cursor is moved by
// the page chrome around the body.
func (l Layout) PageBlock(title, subtitle string, body Block, footer string) Block {
	return l.Shell(Header{Title: title, Subtitle: subtitle}, body, Footer{Text: footer})
}

// Header is the top of the page chrome: the title row with an optional
// right-aligned badge (the shell shows the mode there) and the subtitle row,
// which is a label or, when Tabs is set, the page tabs with Active marked.
// Every field is a raw label.
type Header struct {
	Title    string
	Badge    string
	Subtitle string
	Tabs     []string
	Active   int
}

// Footer is the bottom row of the page chrome: a raw label, styled as an
// error when Error is set. Jobs are the jobs ticker's rows (Model.JobsTicker),
// drawn above the rule over the footer; the body gives up one row per job
// row, so no jobs means no rows.
type Footer struct {
	Text  string
	Error bool
	Jobs  []string
}

// Shell is the one page chrome: title row, subtitle row, a rule, the body,
// a rule, the footer and a bottom margin, inside a gutter, into exactly
// Width x Height cells. Viewports too small for the chrome keep the body
// and, from three rows up, the footer. The shell and PageBlock both render
// through it, so a page and the application around it reserve the same
// rows. The body's cursor is moved by the chrome around it.
func (l Layout) Shell(h Header, body Block, f Footer) Block {
	if l.Width <= 0 || l.Height <= 0 {
		return Block{}
	}
	s := l.Styles
	bw, bh := l.BodySizeWith(f)
	bodyRows := layoutRows(body.Text, bw, bh)
	cursor := body.Clip(bw, bh)
	footerStyle := s.Muted
	if f.Error {
		footerStyle = s.Error
	}
	footer := footerStyle.Render(ansi.Truncate(modalLabel(f.Text), max(0, bw), "…"))
	var rows []string
	if l.full() {
		cursor = cursor.Offset(layoutGutter, 3) // gutter; title, subtitle and rule rows
		pad := strings.Repeat(" ", layoutGutter)
		rule := s.Muted.Render(strings.Repeat("─", bw))
		rows = append(rows, pad+l.titleRow(h, bw), pad+l.subtitleRow(h, bw), pad+rule)
		for _, row := range bodyRows {
			rows = append(rows, pad+row)
		}
		for _, job := range f.Jobs[:l.jobRows(f)] {
			rows = append(rows, pad+s.Muted.Render(ansi.Truncate(modalLabel(job), max(0, bw), "…")))
		}
		rows = append(rows, pad+rule, pad+footer, "")
	} else {
		rows = append(rows, bodyRows...)
		if l.Height >= 3 {
			rows = append(rows, footer)
		}
	}
	for i := range rows {
		rows[i] = modalCells(rows[i], l.Width)
	}
	return cursor.WithText(strings.Join(rows, "\n"))
}

// titleRow renders the title with the badge right-aligned in width cells.
func (l Layout) titleRow(h Header, width int) string {
	s := l.Styles
	badge := ""
	if h.Badge != "" {
		badge = s.Badge.Render(ansi.Truncate(modalLabel(h.Badge), max(0, width-3), "…"))
	}
	reserved := 0
	if badge != "" {
		reserved = ansi.StringWidth(badge) + 1
	}
	title := s.Accent.Render("◆ ") + s.Title.Render(ansi.Truncate(modalLabel(h.Title), max(0, width-2-reserved), "…"))
	if badge == "" {
		return title
	}
	gap := max(1, width-ansi.StringWidth(title)-ansi.StringWidth(badge))
	return title + strings.Repeat(" ", gap) + badge
}

// subtitleRow renders the tabs when there are any, else the subtitle label.
func (l Layout) subtitleRow(h Header, width int) string {
	if len(h.Tabs) > 0 {
		return l.tabs(h.Tabs, h.Active, width)
	}
	return l.Styles.Subtitle.Render(ansi.Truncate(modalLabel(h.Subtitle), width, "…"))
}

// tabs renders the active tab and as many neighbours as fit in width cells,
// alternating left and right of it. When pages are hidden, the row shows
// "‹N" at the left edge and "N›" at the right, N the pages hidden on that
// side; the markers' cells are reserved before neighbours are added, so the
// active tab is always on the row and the counts add up to the hidden pages.
func (l Layout) tabs(titles []string, active, width int) string {
	if len(titles) == 0 {
		return ""
	}
	s := l.Styles
	active = min(max(0, active), len(titles)-1)
	cells := make([]string, len(titles))
	total := 0
	for i, title := range titles {
		if i == active {
			cells[i] = s.ActiveTab.Render(modalLabel(title))
		} else {
			cells[i] = s.Tab.Render(modalLabel(title))
		}
		total += ansi.StringWidth(cells[i])
	}
	if total <= width {
		return strings.Join(cells, "")
	}
	leftMarker := func(n int) string { return s.Tab.Render(fmt.Sprintf("‹%d", n)) }
	rightMarker := func(n int) string { return s.Tab.Render(fmt.Sprintf("%d›", n)) }
	// Reserve a marker on each side that may hide pages, as wide as its
	// largest count; once a side shows every page, give its cells back and
	// fit again (the other side may then take more).
	hideLeft, hideRight := active > 0, active < len(titles)-1
	var first, last int
	for range 3 {
		budget := width
		if hideLeft {
			budget -= ansi.StringWidth(leftMarker(active))
		}
		if hideRight {
			budget -= ansi.StringWidth(rightMarker(len(titles) - 1 - active))
		}
		first, last = l.tabWindow(cells, titles, active, budget)
		left, right := first > 0, last < len(titles)-1
		if left == hideLeft && right == hideRight {
			break
		}
		hideLeft, hideRight = left, right
	}
	var b strings.Builder
	if first > 0 {
		b.WriteString(leftMarker(first))
	}
	if first == last && first == active {
		// The active tab alone: cut it to what the markers leave.
		room := width - ansi.StringWidth(b.String())
		if last < len(titles)-1 {
			room -= ansi.StringWidth(rightMarker(len(titles) - 1 - last))
		}
		b.WriteString(ansi.Truncate(s.ActiveTab.Render(ansi.Truncate(modalLabel(titles[active]), max(1, room-4), "…")), max(0, room), ""))
	} else {
		b.WriteString(strings.Join(cells[first:last+1], ""))
	}
	if last < len(titles)-1 {
		b.WriteString(rightMarker(len(titles) - 1 - last))
	}
	return ansi.Truncate(b.String(), width, "")
}

// tabWindow is the run of tabs around active that fits in budget cells,
// adding neighbours left and right in turn until neither side fits.
func (l Layout) tabWindow(cells, titles []string, active, budget int) (first, last int) {
	first, last = active, active
	used := ansi.StringWidth(cells[active])
	left, right := active-1, active+1
	for left >= 0 || right < len(titles) {
		added := false
		if left >= 0 {
			if size := ansi.StringWidth(cells[left]); used+size <= budget {
				used += size
				first = left
				left--
				added = true
			} else {
				left = -1
			}
		}
		if right < len(titles) {
			if size := ansi.StringWidth(cells[right]); used+size <= budget {
				used += size
				last = right
				right++
				added = true
			} else {
				right = len(titles)
			}
		}
		if !added {
			break
		}
	}
	return first, last
}

// PanelContentSize returns the body cells available inside a Panel, excluding
// its border and horizontal padding. Tiny panels omit their chrome.
func PanelContentSize(width, height int) (int, int) {
	if width <= 0 || height <= 0 {
		return 0, 0
	}
	if width < 3 || height < 3 {
		return width, height
	}
	if width >= 6 {
		return width - 4, height - 2
	}
	return width - 2, height - 2
}

// Panel renders a rounded, titled box of exactly width x height outer cells.
// Focused panels use the heavy accent border. Body is trusted ANSI; title is a
// raw label. Content that does not fit is clipped and marked with "…" (columns)
// or a "… N more" last row (lines). Below 3x3 cells the border is dropped.
func (l Layout) Panel(title, body string, width, height int, focused bool) string {
	return l.PanelBlock(title, Text(body), width, height, focused).Text
}

// PanelBlock is Panel for a body that may carry a cursor; the cursor is moved
// inside the border and padding.
func (l Layout) PanelBlock(title string, body Block, width, height int, focused bool) Block {
	if width <= 0 || height <= 0 {
		return Block{}
	}
	s := l.Styles
	if width < 3 || height < 3 {
		return body.Clip(width, height).WithText(strings.Join(layoutRows(body.Text, width, height), "\n"))
	}
	frame := s.Panel
	titleStyle := s.Title
	if focused {
		frame, titleStyle = s.FocusedPanel, s.Accent
	}
	b := frame.GetBorderStyle()
	edge := lipgloss.NewStyle().Foreground(frame.GetBorderTopForeground())
	inner := width - 2
	cw, ih := PanelContentSize(width, height)
	rows := layoutRows(layoutClip(body.Text, ih, s), cw, ih)

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
	return body.Clip(cw, ih).Offset(1+len(margin), 1).WithText(strings.Join(out, "\n"))
}

// Split renders two panels filling BodySize: side by side (left ~40%) when the
// viewport is at least 72 cells wide, otherwise stacked (top ~40%). The left or
// top panel is the focused one.
func (l Layout) Split(leftTitle, leftBody, rightTitle, rightBody string) string {
	return l.SplitBlock(leftTitle, Text(leftBody), rightTitle, Text(rightBody)).Text
}

// SplitBlock is Split for bodies that may carry a cursor. One body should
// carry it; when both do, the left or top panel wins.
func (l Layout) SplitBlock(leftTitle string, leftBody Block, rightTitle string, rightBody Block) Block {
	bw, bh := l.BodySize()
	return l.panelPair(leftTitle, leftBody, rightTitle, rightBody, bw, bh, (bw-1)*2/5, l.Width < layoutStackAt)
}

// Sidebar renders a 28-cell navigation rail beside the main panel. Below 72
// columns it stacks above the main panel. Items are raw labels; mainBody is
// trusted ANSI. Selection is zero-based and clamped for rendering. The caller
// owns selection and key handling; this method keeps the selected item visible.
func (l Layout) Sidebar(title string, items []string, selected int, mainTitle, mainBody string) string {
	return l.SidebarBlock(title, items, selected, mainTitle, Text(mainBody)).Text
}

// SidebarBlock is Sidebar for a main body that may carry a cursor.
func (l Layout) SidebarBlock(title string, items []string, selected int, mainTitle string, mainBody Block) Block {
	bw, bh := l.BodySize()
	return l.sidebar(title, items, selected, mainTitle, mainBody, bw, bh, l.Width < layoutStackAt)
}

// sidebar also composes a model-owned content region without a second page shell.
func (l Layout) sidebar(title string, items []string, selected int, mainTitle string, mainBody Block, width, height int, stacked bool) Block {
	regionHeight := height
	if stacked {
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
	return l.panelPair(title, Text(strings.Join(rows, "\n")), mainTitle, mainBody, width, regionHeight, 28, stacked)
}

func sidebarMainSize(width, height int, stacked bool) (int, int) {
	if stacked {
		height -= height * 2 / 5
	} else {
		width -= 29
	}
	return PanelContentSize(width, height)
}

// panelPair places the left (or top) panel at the origin and the right panel
// one cell to its right (or the bottom panel below it).
func (l Layout) panelPair(leftTitle string, leftBody Block, rightTitle string, rightBody Block, bw, bh, leftWidth int, stacked bool) Block {
	if bw <= 0 || bh <= 0 {
		return Block{}
	}
	if !stacked {
		rw := bw - 1 - leftWidth
		left := l.PanelBlock(leftTitle, leftBody, leftWidth, bh, true)
		right := l.PanelBlock(rightTitle, rightBody, rw, bh, false).Offset(leftWidth+1, 0)
		leftRows, rightRows := strings.Split(left.Text, "\n"), strings.Split(right.Text, "\n")
		rows := make([]string, bh)
		for i := range rows {
			rows[i] = layoutRow(leftRows, i, leftWidth) + " " + layoutRow(rightRows, i, rw)
		}
		return Block{Text: strings.Join(rows, "\n"), Cursor: firstCursor(left, right)}
	}
	th := bh * 2 / 5
	top := l.PanelBlock(leftTitle, leftBody, bw, th, true)
	bottom := l.PanelBlock(rightTitle, rightBody, bw, bh-th, false).Offset(0, th)
	return Block{Text: top.Text + joinIfBoth(th, bh-th) + bottom.Text, Cursor: firstCursor(top, bottom)}
}

func joinIfBoth(a, b int) string {
	if a > 0 && b > 0 {
		return "\n"
	}
	return ""
}

func layoutRow(rows []string, i, width int) string {
	if i < len(rows) {
		return modalCells(rows[i], width)
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
		rows[i] = modalCells(line, width)
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
