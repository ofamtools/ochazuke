package ochazuke

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// ModalView renders one modal without replacing or inspecting its parent page.
// Render returns the body as a Block; its cursor, when the modal holds the
// focused input, is relative to the body's top-left cell.
type ModalView struct {
	Title  string
	Render func(*Runtime, *Frame, int, int) Block
}

// Model is the Bubble Tea adapter. Pages and modal instances retain their
// state in Runtime; components (picker, registers, application components)
// are instances the runtime drives through the delivery contract, so the
// adapter holds no component list of its own.
type Model struct {
	Runtime    *Runtime
	Palette    *Palette
	Appearance *Appearance
	ModalViews map[string]ModalView
	Title      string
	Width      int
	Height     int
	Status     string
	// Sidebar replaces page tabs with a navigation rail. Existing page operations
	// remain the only navigation state; input focus and modal routing are unchanged.
	Sidebar bool
	views   map[string]*viewport.Model
	scroll  map[string]int // frame ID -> scroll offset
}

func NewModel(r *Runtime) *Model {
	m := &Model{Runtime: r, Palette: NewPalette(r), Appearance: NewAppearance(r), ModalViews: map[string]ModalView{}, Title: "Ochazuke", Width: 80, Height: 24, views: map[string]*viewport.Model{}, scroll: map[string]int{}}
	m.ModalViews[yankHistoryID] = ModalView{Title: "Yank history", Render: yankHistoryView}
	m.ModalViews["confirm"] = ModalView{Title: "Confirm", Render: confirmView}
	r.Register(Operation{Verb: "scroll", Noun: "surface", Description: "Scroll the active page or modal without changing focus", Parameters: map[string]Parameter{"lines": {Type: "integer", Required: true}}, Result: "scroll offset", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		frame := r.top()
		width, height := m.pageSize()
		var content string
		if len(r.Frames) > 1 {
			width, height = ModalSize(m.Width, m.Height)
			_, body := m.modalContent(frame, width, height)
			content = body.Text
		} else {
			content = m.pageContent(width, height).Text
		}
		view := m.viewport(frame, content, width, height)
		delta := min(max(IntArg(a, "lines", 0), -view.TotalLineCount()), view.TotalLineCount())
		view.SetYOffset(view.YOffset() + delta)
		m.scroll[frame.ID] = view.YOffset()
		return view.YOffset(), nil
	}})
	// Application rows already on the app noun keep precedence over these defaults.
	app := r.Noun("app")
	defaultRow := func(mode, keys string, row Binding) {
		for _, existing := range app.Bindings[mode] {
			if existing.Keys == keys {
				return
			}
		}
		r.Define("app").Mode(mode).Bind(keys, row)
	}
	defaultRow("normal", ":", Do("open.palette"))
	defaultRow("normal", "ctrl+y", Do("open.yank-history"))
	defaultRow("normal", "ctrl+j", Do("next.page"))
	defaultRow("normal", "ctrl+k", Do("previous.page"))
	defaultRow("", "ctrl+space", Do("open.palette"))
	defaultRow("", "ctrl+c", Do("quit.app"))
	defaultRow("", "pgdown", Do("scroll.surface", Arguments{"lines": 8}))
	defaultRow("", "pgup", Do("scroll.surface", Arguments{"lines": -8}))
	return m
}
func (m *Model) Init() tea.Cmd { return tea.RequestBackgroundColor }
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Off-loop results reach their owning instance first; a stale or
	// orphaned completion is dropped here.
	if _, err := m.Runtime.Deliver(msg); err != nil {
		m.Status = err.Error()
	}
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = max(1, v.Width)
		m.Height = max(1, v.Height)
	case CallMsg:
		m.Runtime.HandleCall(v)
	case BindingTimeoutMsg:
		if handled, err := m.Runtime.HandleBindingTimeout(v); handled {
			if err != nil {
				m.Status = err.Error()
			} else {
				m.Status = ""
			}
			m.Palette.collectEditors()
		}
	case tea.BackgroundColorMsg:
		m.Appearance.Dark = v.IsDark()
	case tea.KeyPressMsg:
		m.handleKey(MacroKey{Key: v.String()})
	case tea.PasteMsg:
		// Bubble Tea v1 spelled a paste "[text]"; macros and the editor still expect it.
		m.handleKey(MacroKey{Key: "[" + v.Content + "]", Paste: true})
	}
	if m.Runtime.Quit {
		return m, tea.Quit
	}
	var commands []tea.Cmd
	if cmd := m.Runtime.BindingTimeoutCommand(); cmd != nil {
		commands = append(commands, cmd)
	}
	if cmd := m.Runtime.PendingCommands(); cmd != nil {
		commands = append(commands, cmd)
	}
	return m, tea.Batch(commands...)
}

// handleKey dispatches one key through the runtime's stack walk and shows
// the error, if any, in the status line.
func (m *Model) handleKey(key MacroKey) {
	if _, err := m.Runtime.Dispatch(key); err != nil {
		m.Status = err.Error()
	} else {
		m.Status = ""
	}
	m.Palette.collectEditors()
}
func (m *Model) pageSize() (int, int) {
	width, height := m.surfaceSize()
	if m.Sidebar {
		width, height = sidebarMainSize(width, height, width < layoutStackAt)
	}
	return max(1, width), max(1, height)
}
func (m *Model) surfaceSize() (int, int) {
	pad := NewStyles(*m.Appearance).Padding
	return max(1, m.Width-2*pad), max(1, m.Height-6)
}
func (m *Model) pageContent(width, height int) Block {
	if page := m.Runtime.PageOf(m.Runtime.Page); page != nil {
		return page.Render(m.Runtime, width, height)
	}
	return Text("No page selected. Open a registered page through the palette or API.")
}
func (m *Model) viewport(frame *Frame, content string, width, height int) *viewport.Model {
	view := m.views[frame.ID]
	if view == nil {
		v := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
		view = &v
		m.views[frame.ID] = view
	}
	view.SetWidth(max(1, width))
	view.SetHeight(max(1, height))
	view.SetContent(content)
	view.SetYOffset(m.scroll[frame.ID])
	return view
}

// modalContent renders a modal body. Only the top frame's input is focused, so
// a covered modal reports no cursor.
func (m *Model) modalContent(frame *Frame, width, height int) (string, Block) {
	var body Block
	switch {
	case frame == m.Palette.frame:
		body = m.Palette.viewBlock(width, height)
		body = body.WithText(strings.TrimPrefix(body.Text, "Command palette\n")).Offset(0, -1)
		return "Command palette", body
	case m.ModalViews[frame.ID].Render != nil:
		body = m.ModalViews[frame.ID].Render(m.Runtime, frame, width, height)
	case m.Runtime.Input(frame.Focus) != nil:
		body = InputBlock(m.Runtime.Input(frame.Focus), width, height)
	default:
		body = Text("This modal has no focused input.")
	}
	if frame != m.Runtime.top() {
		body.Cursor = nil
	}
	if view, ok := m.ModalViews[frame.ID]; ok {
		return view.Title, body
	}
	return frame.ID, body
}
func (m *Model) pageTabs(styles Styles, width int) string {
	order := m.Runtime.PageList()
	if len(order) == 0 {
		return ""
	}
	active := 0
	for i, page := range order {
		if page.ID == m.Runtime.Page {
			active = i
			break
		}
	}
	title := safeText(order[active].Title)
	tabs := ansi.Truncate(styles.ActiveTab.Render(ansi.Truncate(title, max(1, width-4), "…")), width, "")
	used := ansi.StringWidth(tabs)
	left, right := active-1, active+1
	for left >= 0 || right < len(order) {
		added := false
		if left >= 0 {
			tab := styles.Tab.Render(safeText(order[left].Title))
			if size := ansi.StringWidth(tab); used+size <= width {
				tabs = tab + tabs
				used += size
				left--
				added = true
			} else {
				left = -1
			}
		}
		if right < len(order) {
			tab := styles.Tab.Render(safeText(order[right].Title))
			if size := ansi.StringWidth(tab); used+size <= width {
				tabs += tab
				used += size
				right++
				added = true
			} else {
				right = len(order)
			}
		}
		if !added {
			break
		}
	}
	return tabs
}

// View renders the shell as a full-screen tea.View; View().Content is the
// string. The terminal cursor follows the focused input through the page or
// modal that holds it: a bar in insert mode, a block in normal mode and an
// underline in visual modes. It is hidden when no input is focused.
func (m *Model) View() tea.View {
	block := m.render()
	v := tea.NewView(block.Text)
	v.AltScreen = true
	v.Cursor = m.cursor(block)
	return v
}

// cursor shapes the block's cursor by the focused input's mode.
func (m *Model) cursor(block Block) *tea.Cursor {
	context := m.Runtime.Context()
	if block.Cursor == nil || m.Runtime.Input(context.InputID) == nil {
		return nil
	}
	cursor := tea.NewCursor(block.Cursor.X, block.Cursor.Y)
	cursor.Blink = false
	switch mode := m.Runtime.Mode(context); {
	case mode == "insert":
		cursor.Shape = tea.CursorBar
		cursor.Blink = true
	case isVisual(mode):
		cursor.Shape = tea.CursorUnderline
	default:
		cursor.Shape = tea.CursorBlock
	}
	return cursor
}

// render composes the page shell and the modal stack. Each composer offsets
// the child cursor by where it placed the child; a modal hides the cursor of
// everything beneath it.
func (m *Model) render() Block {
	styles := NewStyles(*m.Appearance)
	width, height := m.surfaceSize()
	pageTitle := ""
	if page := m.Runtime.PageOf(m.Runtime.Page); page != nil {
		pageTitle = page.Title
	}
	heading := styles.Title.Render(safeText(m.Title)) + "  " + styles.Muted.Render(safeText(pageTitle))
	modeLabel := strings.ToUpper(modeLabelOf(m.Runtime.Mode(m.Runtime.Context())))
	if m.Runtime.Registers().Macro.Recording != "" {
		modeLabel += " · REC @" + m.Runtime.Registers().Macro.Recording
	}
	mode := styles.Badge.Render(modeLabel)
	heading = ansi.Truncate(heading, max(0, width-lipgloss.Width(mode)-1), "…")
	gap := max(1, m.Width-lipgloss.Width(heading)-lipgloss.Width(mode)-2*styles.Padding)
	heading += strings.Repeat(" ", gap) + mode
	tabs := ""
	if !m.Sidebar {
		tabs = m.pageTabs(styles, width)
	}
	base := m.Runtime.Frames[0]
	pageWidth, pageHeight := m.pageSize()
	page := m.scrolled(base, m.pageContent(pageWidth, pageHeight), pageWidth, pageHeight)
	if m.Sidebar {
		pages := m.Runtime.PageList()
		titles := make([]string, len(pages))
		selected := 0
		for i, page := range pages {
			titles[i] = page.Title
			if page.ID == m.Runtime.Page {
				selected = i
			}
		}
		layout := Layout{Styles: styles}
		page = layout.sidebar("Pages", titles, selected, pageTitle, page, width, height, width < layoutStackAt)
	}
	footer := "Ctrl-J/K pages · : palette · PgUp/PgDn scroll · Ctrl-C quit"
	currentMode := m.Runtime.Mode(m.Runtime.Context())
	if currentMode == "insert" {
		footer = "INSERT · Esc normal · Ctrl-A/E line · Ctrl-W/U/K kill · Ctrl-Space palette"
		if in := m.Runtime.Input(m.Runtime.FocusID()); in != nil && in.pin == "insert" {
			footer = "READLINE · Ctrl-A/E line · Ctrl-W/U/K kill · Ctrl-Y yank · Ctrl-Space palette"
		}
	}
	status := styles.Muted.Render(footer)
	switch currentMode {
	case "macro_pending":
		status = styles.Accent.Render("Record macro: choose a register · Esc cancel")
	case "replay_pending":
		status = styles.Accent.Render("Replay macro: choose a register · Esc cancel")
	case "register_pending":
		status = styles.Accent.Render("Register: choose a name · Esc cancel")
	}
	if m.Runtime.Pending != "" {
		status = styles.Accent.Render("Pending " + safeText(m.Runtime.Pending) + " · Esc cancel")
	}
	if m.Status != "" {
		status = styles.Error.Render("Error: " + safeText(m.Status))
	}
	inset := lipgloss.NewStyle().PaddingLeft(styles.Padding).PaddingRight(styles.Padding)
	// Heading, tabs and a blank row sit above the page; the inset adds Padding cells on the left.
	surface := page.Offset(styles.Padding, 3).Clip(m.Width, m.Height)
	background := strings.Join([]string{inset.Render(heading), inset.Render(tabs), "", inset.Render(page.Text), "", inset.Render(status)}, "\n")
	surface.Text = fitSurface(background, m.Width, m.Height)
	for _, frame := range m.Runtime.Frames[1:] {
		modalWidth, modalHeight := ModalSize(m.Width, m.Height)
		title, body := m.modalContent(frame, modalWidth, modalHeight)
		bodyHeight := min(modalHeight, max(1, lipgloss.Height(body.Text)))
		bodyWidth := min(modalWidth, max(1, lipgloss.Width(body.Text)))
		body = m.scrolled(frame, body, bodyWidth, bodyHeight)
		hint := "Esc normal / close · PgUp/PgDn scroll"
		if frame == m.Palette.frame {
			hint = "Esc normal / back · Ctrl-C cancel"
		} else if in := m.Runtime.Input(frame.Focus); in != nil && in.pin == "insert" {
			hint = "Readline editing · PgUp/PgDn scroll"
		}
		if frame == m.Runtime.top() && m.Status != "" {
			hint = "Error: " + m.Status
		}
		surface = ComposeModalBlock(surface.Text, title, body, hint, m.Width, m.Height, styles)
	}
	return surface
}

// scrolled runs a block through the frame's viewport, moving the cursor with
// the scrolled content and dropping it when it scrolls out of view.
func (m *Model) scrolled(frame *Frame, content Block, width, height int) Block {
	view := m.viewport(frame, content.Text, width, height)
	return content.WithText(view.View()).Offset(0, -view.YOffset()).Clip(width, height)
}

// modeLabelOf shortens a pending mode name for the badge: "normal g" stays,
// operator_pending becomes "operator pending".
func modeLabelOf(mode string) string { return strings.ReplaceAll(mode, "_", " ") }

func fitSurface(content string, width, height int) string {
	width = max(1, width)
	height = max(1, height)
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		line = ansi.Truncate(line, width, "")
		lines[i] = line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
	}
	return strings.Join(lines, "\n")
}
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, s)
}

// InputView renders an input's visible text without a cursor, for inputs that
// are not focused. See InputBlock.
func InputView(in *Input, width, height int) string { return InputBlock(in, width, height).Text }

// InputBlock renders width x height cells of an input around its cursor and
// reports where the cursor sits, without changing text or editing state.
// Visual selections are painted in reverse video; the cursor itself is left
// to the terminal, so compose this block only for the focused input.
func InputBlock(in *Input, width, height int) Block {
	if in == nil {
		return Text("(no focused input)")
	}
	width = max(1, width)
	height = max(1, height)
	cursor := min(max(0, in.Cursor), len(in.Text))
	clusters := uniseg.NewGraphemes(safeText(string(in.Text)))
	cursorRow, cursorColumn, cursorWidth, runeIndex := 0, 0, 1, 0
	for clusters.Next() {
		text := clusters.Str()
		count := utf8.RuneCountInString(text)
		if runeIndex+count > cursor {
			cursorWidth = max(1, cellWidth(text))
			break
		}
		runeIndex += count
		if text == "\n" {
			cursorRow++
			cursorColumn = 0
		} else {
			cursorColumn += cellWidth(text)
		}
	}
	if in.Mode == "visual-block" && (cursor == len(in.Text) || in.Text[cursor] == '\n') {
		cursorColumn = max(cursorColumn, in.goalColumn)
	}
	start := max(0, cursorRow-height+1)
	offset := max(0, cursorColumn+min(width, cursorWidth)-width)
	var body strings.Builder
	rowIndex, column := 0, 0
	write := func(text string, cells int, selected bool) {
		lo, hi := max(column, offset), min(column+cells, offset+width)
		visible := rowIndex >= start && rowIndex < start+height
		if visible && (hi > lo || cells == 0 && column >= offset && column < offset+width) {
			if lo != column || hi != column+cells {
				text = strings.Repeat(" ", hi-lo)
			}
			if selected {
				body.WriteString("\x1b[7m")
			}
			body.WriteString(text)
			if selected {
				body.WriteString("\x1b[27m")
			}
		}
		column += cells
	}
	lo, hi := in.selectionRange()
	top, bottom, left, right := 0, -1, 0, 0
	block := in.Mode == "visual-block"
	if block {
		top, bottom, left, right = in.blockBounds()
	}
	// A line end still gets a blank cell under the cursor so the terminal
	// cursor lands on a rendered cell and the text keeps the same shape.
	writeLineEnd := func(selected, onCursor bool) {
		end := column
		blockRow := block && rowIndex >= top && rowIndex <= bottom
		if blockRow {
			end = max(end, right)
		} else if !block && selected {
			end++
		}
		if onCursor {
			end = max(end, cursorColumn+1)
		}
		for column < end {
			next := end
			reversed := !block && selected
			if blockRow {
				reversed = column >= left && column < right
				if column < left {
					next = min(next, left)
				} else if column < right {
					next = min(next, right)
				}
			}
			write(strings.Repeat(" ", next-column), next-column, reversed)
		}
	}
	clusters.Reset()
	runeIndex = 0
	for clusters.Next() {
		text := clusters.Str()
		i := runeIndex
		runeIndex += utf8.RuneCountInString(text)
		onCursor := i <= cursor && cursor < runeIndex
		selected := isVisual(in.Mode) && i < hi && runeIndex > lo
		w := cellWidth(text)
		if block {
			selected = rowIndex >= top && rowIndex <= bottom && column < right && column+w > left
		}
		if text == "\n" {
			writeLineEnd(selected, onCursor)
			if rowIndex >= start && rowIndex < start+height-1 {
				body.WriteByte('\n')
			}
			rowIndex++
			column = 0
			continue
		}
		// A block selection crossing part of a wide grapheme blanks the cells
		// so the selected half can be painted on its own.
		if selected && block && w > 0 && (column < left || column+w > right) {
			before := max(0, left-column)
			middle := min(column+w, right) - max(column, left)
			after := max(0, column+w-right)
			if before > 0 {
				write(strings.Repeat(" ", before), before, false)
			}
			write(strings.Repeat(" ", middle), middle, true)
			if after > 0 {
				write(strings.Repeat(" ", after), after, false)
			}
		} else {
			write(text, w, selected)
		}
	}
	writeLineEnd(false, cursor == len(in.Text))
	return Block{Text: body.String(), Cursor: &Pos{X: cursorColumn - offset, Y: cursorRow - start}}
}
