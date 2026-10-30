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
	// Shift moves the divider of every split this layout draws, in percent
	// points of the split's length: positive grows the first (left or top)
	// panel, negative the second, 0 is the split's default. It is the
	// page's remembered divider (Runtime.PaneLayout sets it; pane.grow,
	// pane.shrink and pane.reset change it). Divide applies it.
	Shift int
}

// The pane limits: PaneStep is the percent points one pane.grow or
// pane.shrink moves a divider; PaneShiftLimit bounds Layout.Shift either
// way; Divide keeps PaneMinWidth cells on each side of a side-by-side split
// and PaneMinHeight rows on each side of a stacked one, so neither panel
// collapses.
const (
	PaneStep       = 5
	PaneShiftLimit = 30
	PaneMinWidth   = 16
	PaneMinHeight  = 3
)

// Divide is where a split of total cells puts its divider: first is the
// split's default size of its first (left or top) panel, moved by Shift
// percent points of total and clamped so each panel keeps least cells. A
// split with no shift, or too small for two panels of least cells, keeps
// first. A layout of its own (a page body split by hand) calls it with its
// default; Split and its kin call it for theirs.
func (l Layout) Divide(first, total, least int) int {
	if l.Shift == 0 || total < 2*least {
		return first
	}
	return min(max(first+l.Shift*total/100, least), total-least)
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

// BodySizeWith is the body's size with the footer f: BodySize, since the
// jobs ticker rides the rule line above the footer and takes no body row,
// so a job starting or lingering out never moves or hides the body (UX
// review 2 F13).
func (l Layout) BodySizeWith(f Footer) (width, height int) {
	return l.BodySize()
}

// TickerRule is the rule line above the footer, width cells: the plain rule
// with no jobs; with jobs, "── " then the ticker entries (Footer.Jobs, at
// most JobTickerRows) joined by " · ", cut with "…" so at least " ─" of
// rule is left, and the rest of the row filled with rule characters, all
// muted, so the chrome still reads as a rule.
func (l Layout) TickerRule(jobs []string, width int) string {
	muted := l.Styles.Muted
	const lead = "── "
	room := width - ansi.StringWidth(lead) - 2
	if len(jobs) == 0 || room < 1 {
		return muted.Render(strings.Repeat("─", max(0, width)))
	}
	text := ansi.Truncate(modalLabel(strings.Join(jobs[:min(len(jobs), JobTickerRows)], " · ")), room, "…")
	fill := width - ansi.StringWidth(lead) - ansi.StringWidth(text) - 1
	return muted.Render(lead + text + " " + strings.Repeat("─", fill))
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
// Focus is the focus beside the badge (the focus-display setting): its
// levels, outermost first, drawn dim after "·" and cut from the left
// (FitFocusPath) so the innermost level stays. Segments are the status
// segments (Runtime.SegmentCells), drawn right-aligned in the rule under
// the header (HeaderRule); they take no row. Every field is a raw label.
type Header struct {
	Title    string
	Badge    string
	Focus    []string
	Subtitle string
	Tabs     []string
	Active   int
	Segments []SegmentCell
	// Keys is the key echo when keys.echo puts it beside the badge.
	Keys *KeyEchoCell
}

// Footer is the bottom row of the page chrome: a raw label, styled as an
// error when Error is set. Jobs are the jobs ticker's entries
// (Model.JobsTicker), drawn in the rule line above the footer (TickerRule);
// the body keeps its size, and no jobs means the plain rule.
type Footer struct {
	Text  string
	Error bool
	Jobs  []string
	// Keys is the key echo when keys.echo puts it in the footer: drawn at
	// the right end in the cells it reserves, the text cut before them.
	Keys *KeyEchoCell
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
	if kw := f.Keys.cells(bw); kw > 0 {
		// The echo keeps its cells whatever the text: the line never
		// reflows as keys come and go.
		text := footerStyle.Render(ansi.Truncate(modalLabel(f.Text), max(0, bw-kw-1), "…"))
		footer = text + strings.Repeat(" ", max(1, bw-kw-ansi.StringWidth(text))) + l.echoCell(f.Keys.Labels, kw)
	}
	var rows []string
	if l.full() {
		cursor = cursor.Offset(layoutGutter, 3) // gutter; title, subtitle and rule rows
		pad := strings.Repeat(" ", layoutGutter)
		rows = append(rows, pad+l.titleRow(h, bw), pad+l.subtitleRow(h, bw), pad+l.HeaderRule(h.Segments, bw))
		for _, row := range bodyRows {
			rows = append(rows, pad+row)
		}
		rows = append(rows, pad+l.TickerRule(f.Jobs, bw), pad+footer, "")
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

// titleRow renders the title with the badge right-aligned in width cells,
// and after the badge the focus (Header.Focus), dim. The focus takes what
// the badge and the title's first cells (focusTitleKeep) leave.
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
	if kw := h.Keys.cells(width); kw > 0 {
		// The echo before the badge, in cells it keeps whatever it holds.
		cell := l.echoCell(h.Keys.Labels, kw)
		if badge != "" {
			cell += " "
		}
		badge = cell + badge
		reserved = ansi.StringWidth(badge) + 1
	}
	if len(h.Focus) > 0 {
		// The badge's own padding is the space before the separator.
		sep := focusSep
		if badge != "" {
			sep = strings.TrimLeft(focusSep, " ")
		}
		title := ansi.StringWidth(modalLabel(h.Title))
		room := width - 2 - min(title, focusTitleKeep(width)) - reserved - ansi.StringWidth(sep)
		if focus := FitFocusPath(h.Focus, room); focus != "" {
			focus = s.Muted.Render(sep + focus)
			badge += focus
			reserved += ansi.StringWidth(focus)
		}
	}
	title := s.Accent.Render("◆ ") + s.Title.Render(ansi.Truncate(modalLabel(h.Title), max(0, width-2-reserved), "…"))
	if badge == "" {
		return title
	}
	gap := max(1, width-ansi.StringWidth(title)-ansi.StringWidth(badge))
	return title + strings.Repeat(" ", gap) + badge
}

// focusTitleKeep is the most of the title the focus may not take from it:
// a quarter of the row.
func focusTitleKeep(width int) int { return max(1, width/4) }

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
// viewport is at least 72 cells wide, otherwise stacked (top ~40%), the
// divider moved by Shift (Divide). The left or top panel is the focused one;
// SplitFocusBlock takes which.
func (l Layout) Split(leftTitle, leftBody, rightTitle, rightBody string) string {
	return l.SplitBlock(leftTitle, Text(leftBody), rightTitle, Text(rightBody)).Text
}

// SplitBlock is Split for bodies that may carry a cursor. One body should
// carry it; when both do, the left or top panel wins.
func (l Layout) SplitBlock(leftTitle string, leftBody Block, rightTitle string, rightBody Block) Block {
	return l.SplitFocusBlock(SplitLeft, leftTitle, leftBody, rightTitle, rightBody)
}

// SplitSide names the panel of a split that draws focused.
type SplitSide int

const (
	SplitLeft  SplitSide = iota // the left (or top) panel, Split's default
	SplitRight                  // the right (or bottom) panel
	SplitNone                   // neither: the focus is elsewhere
)

// SplitFocusBlock is SplitBlock with the focused panel named: the one
// holding the focus draws in the focused style (accent title and border),
// the other plain, so a view whose focus moves between its panes (o-json's
// tree and detail) shows where it is. When both bodies carry a cursor, the
// focused panel's wins.
func (l Layout) SplitFocusBlock(focused SplitSide, leftTitle string, leftBody Block, rightTitle string, rightBody Block) Block {
	bw, bh := l.BodySize()
	stacked := l.Width < layoutStackAt
	first := l.Divide((bw-1)*2/5, bw-1, PaneMinWidth)
	if stacked {
		first = l.Divide(bh*2/5, bh, PaneMinHeight)
	}
	return l.panelPair(leftTitle, leftBody, rightTitle, rightBody, bw, bh, first, stacked, focused)
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
	first := 28
	if stacked {
		first = regionHeight * 2 / 5
	}
	return l.panelPair(title, Text(strings.Join(rows, "\n")), mainTitle, mainBody, width, regionHeight, first, stacked, SplitLeft)
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
// one cell to its right (or the bottom panel below it); first is the left
// panel's width, or the top panel's height when stacked.
func (l Layout) panelPair(leftTitle string, leftBody Block, rightTitle string, rightBody Block, bw, bh, first int, stacked bool, focused SplitSide) Block {
	if bw <= 0 || bh <= 0 {
		return Block{}
	}
	leftFocused, rightFocused := focused == SplitLeft, focused == SplitRight
	cursor := func(first, second Block) *Pos {
		if rightFocused {
			return firstCursor(second, first)
		}
		return firstCursor(first, second)
	}
	if !stacked {
		leftWidth := first
		rw := bw - 1 - leftWidth
		left := l.PanelBlock(leftTitle, leftBody, leftWidth, bh, leftFocused)
		right := l.PanelBlock(rightTitle, rightBody, rw, bh, rightFocused).Offset(leftWidth+1, 0)
		leftRows, rightRows := strings.Split(left.Text, "\n"), strings.Split(right.Text, "\n")
		rows := make([]string, bh)
		for i := range rows {
			rows[i] = layoutRow(leftRows, i, leftWidth) + " " + layoutRow(rightRows, i, rw)
		}
		return Block{Text: strings.Join(rows, "\n"), Cursor: cursor(left, right)}
	}
	th := first
	top := l.PanelBlock(leftTitle, leftBody, bw, th, leftFocused)
	bottom := l.PanelBlock(rightTitle, rightBody, bw, bh-th, rightFocused).Offset(0, th)
	return Block{Text: top.Text + joinIfBoth(th, bh-th) + bottom.Text, Cursor: cursor(top, bottom)}
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
