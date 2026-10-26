package ochazuke

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// ModalView renders one modal without replacing or inspecting its parent page.
// Render receives the modal instance (its Payload carries what OpenModal
// gave it) and returns the body as a Block; its cursor, when the modal holds
// the focused input, is relative to the body's top-left cell.
type ModalView struct {
	Title  string
	Render func(*Runtime, *Instance, int, int) Block
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
	// Notices are persistent lines (a host's mount warnings): drawn in the
	// footer before the hints, kept across keys and errors until notice.clear
	// (or the application) clears them. Status is the last key's error and
	// goes with the next key; a notice does not.
	Notices []string
	// Sidebar replaces page tabs with a navigation rail. Existing page operations
	// remain the only navigation state; input focus and modal routing are unchanged.
	Sidebar bool
	views   map[string]*viewport.Model
	scroll  map[string]int // modal ID or page surface key -> scroll offset
	// followed is the focus each page surface last scrolled to follow, so a
	// frame scrolls only when the focus moved (pgup and pgdown may leave it
	// off screen until the next motion).
	followed map[string]string
	ticker   jobTicker // the finished jobs the ticker remembers (ticker.go)
	// Now is the Model's clock: the ticker's memory of finished jobs and
	// the linger timer read it (time.Now by default, time.Now when nil). A
	// headless host fakes it, advances it to JobLingerTick's deadline and
	// delivers that message instead of waiting JobLinger.
	Now func() time.Time
}

// NewModel returns the Bubble Tea model that renders and drives r.
func NewModel(r *Runtime) *Model {
	m := &Model{Runtime: r, Palette: NewPalette(r), Appearance: NewAppearance(r), ModalViews: map[string]ModalView{}, Title: "Ochazuke", Width: 80, Height: 24, views: map[string]*viewport.Model{}, scroll: map[string]int{}, followed: map[string]string{}, Now: time.Now}
	m.ModalViews[yankHistoryID] = ModalView{Title: "Yank history", Render: yankHistoryView}
	m.ModalViews["confirm"] = ModalView{Title: "Confirm", Render: confirmView}
	m.ModalViews[helpID] = ModalView{Title: "Keys", Render: bindingHelpView}
	r.Register(Operation{Verb: "scroll", Noun: "surface", Description: "Scroll the active page or modal without changing focus", Parameters: map[string]Parameter{"lines": {Type: "integer", Required: true}}, Result: "scroll offset", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		key := m.surfaceKey()
		width, height := m.pageSize()
		var content string
		if top := r.TopModal(); top != "" {
			width, height = ModalSize(m.Width, m.Height)
			_, body := m.modalContent(top, width, height)
			content = body.Text
		} else {
			content = m.pageContent(width, height).Text
		}
		view := m.viewport(key, content, width, height)
		delta := min(max(IntArg(a, "lines", 0), -view.TotalLineCount()), view.TotalLineCount())
		view.SetYOffset(view.YOffset() + delta)
		m.scroll[key] = view.YOffset()
		return view.YOffset(), nil
	}})
	r.Register(Operation{Verb: "list", Noun: "notice", Description: "List the persistent notices shown in the footer", Result: "string[]", Run: func(*Runtime, Context, Arguments) (any, error) {
		return append([]string{}, m.Notices...), nil
	}})
	r.Register(Operation{Verb: "clear", Noun: "notice", Description: "Clear the persistent notices from the footer", Result: "cleared count", Run: func(*Runtime, Context, Arguments) (any, error) {
		n := len(m.Notices)
		m.Notices = nil
		return n, nil
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
	defaultRow("normal", ":", Do("palette.open"))
	defaultRow("normal", "ctrl+y", Do("yank-history.open").Hide())
	defaultRow("normal", "?", Do("bindings.help"))
	defaultRow("normal", "ctrl+j", Do("page.next"))
	defaultRow("normal", "ctrl+k", Do("page.previous"))
	defaultRow("", "ctrl+space", Do("palette.open"))
	// ctrl+c quits from a capture row, so an inner any row cannot swallow
	// it; inside a modal the capture row stands aside and the bubbling row
	// below answers unless the modal binds ctrl+c. In a passthrough mode (an
	// attached terminal) there is no capture pass: ctrl+c goes to the pane.
	captureQuit := true
	for _, existing := range app.Bindings[""] {
		captureQuit = captureQuit && existing.Keys != "ctrl+c"
	}
	defaultRow("", "ctrl+c", Do("app.quit").Hide())
	if captureQuit {
		r.Define("app").Mode("").Bind("ctrl+c", Do("app.quit").Captured().Guard("no-modal").Hide())
	}
	defaultRow("", "pgdown", Do("surface.scroll", Arguments{"lines": 8}))
	defaultRow("", "pgup", Do("surface.scroll", Arguments{"lines": -8}))
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
			m.Palette.sync()
		}
	case JobLingerMsg:
		m.ticker.fired(v)
	case tea.BackgroundColorMsg:
		m.Appearance.Dark = v.IsDark()
	case tea.KeyPressMsg:
		m.handleKey(MacroKey{Key: v.String()})
	case tea.PasteMsg:
		// Bubble Tea v1 spelled a paste "[text]"; macros and the editor still expect it.
		m.handleKey(MacroKey{Key: "[" + v.Content + "]", Paste: true})
	}
	m.ticker.observe(m.Runtime, m.clock())
	if m.Runtime.Quit {
		// Quitting writes what components still hold (an open outline line).
		for _, inst := range m.Runtime.components() {
			if f, ok := inst.Component.(interface{ Flush() error }); ok {
				_ = f.Flush()
			}
		}
		return m, tea.Quit
	}
	var commands []tea.Cmd
	if cmd := m.Runtime.BindingTimeoutCommand(); cmd != nil {
		commands = append(commands, cmd)
	}
	if cmd := m.Runtime.PendingCommands(); cmd != nil {
		commands = append(commands, cmd)
	}
	if cmd := m.ticker.lingerCommand(m.clock()); cmd != nil {
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
	m.Palette.sync()
}

// layout is the shell's chrome for the current viewport; page renderers get
// its BodySize, less the sidebar when one is shown.
func (m *Model) layout() Layout {
	return Layout{Width: m.Width, Height: m.Height, Styles: NewStyles(*m.Appearance)}
}
func (m *Model) pageSize() (int, int) {
	layout := m.layout()
	width, height := layout.BodySizeWith(Footer{Jobs: m.JobsTicker(layout.Width)})
	if m.Sidebar {
		width, height = sidebarMainSize(width, height, m.Width < layoutStackAt)
	}
	return max(1, width), max(1, height)
}
func (m *Model) pageContent(width, height int) Block {
	if page := m.Runtime.PageOf(m.Runtime.Page); page != nil {
		return page.render(m.Runtime, width, height)
	}
	return Text("No page selected. Open a registered page through the palette or API.")
}

// surfaceKey names the surface that scrolls: the top modal, else the page.
func (m *Model) surfaceKey() string {
	if top := m.Runtime.TopModal(); top != "" {
		return top
	}
	if m.Runtime.Page != "" {
		return m.Runtime.Page
	}
	return "main"
}

func (m *Model) viewport(key string, content string, width, height int) *viewport.Model {
	view := m.views[key]
	if view == nil {
		v := viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
		view = &v
		m.views[key] = view
	}
	view.SetWidth(max(1, width))
	view.SetHeight(max(1, height))
	view.SetContent(content)
	view.SetYOffset(m.scroll[key])
	return view
}

// modalContent renders a modal body. Only the top modal's input is focused,
// so a covered modal reports no cursor.
func (m *Model) modalContent(id string, width, height int) (string, Block) {
	var body Block
	focus := m.Runtime.ModalFocus(id)
	switch {
	case id == paletteID && m.Palette.IsOpen():
		return "Command palette", m.Palette.viewBlock(width, height)
	case m.ModalViews[id].Render != nil:
		body = m.ModalViews[id].Render(m.Runtime, m.Runtime.Instances[id], width, height)
	case m.Runtime.Input(focus) != nil:
		body = InputBlock(m.Runtime.Input(focus), width, height)
	default:
		body = Text("This modal has no focused input.")
	}
	if id != m.Runtime.TopModal() {
		body.Cursor = nil
	}
	if view, ok := m.ModalViews[id]; ok {
		return view.Title, body
	}
	return id, body
}

// pageTitles lists the page titles in order and the index of the current page.
func (m *Model) pageTitles() ([]string, int) {
	pages := m.Runtime.PageList()
	titles := make([]string, len(pages))
	active := 0
	for i, page := range pages {
		titles[i] = page.Title
		if page.ID == m.Runtime.Page {
			active = i
		}
	}
	return titles, active
}

// modeBadge labels the current mode, with the recording register while a
// macro is being recorded.
func (m *Model) modeBadge() string {
	label := strings.ToUpper(modeLabelOf(m.Runtime.Mode.Name))
	if recording := m.Runtime.Registers().Macro.Recording; recording != "" {
		label += " · REC @" + recording
	}
	return label
}

// Notify adds a persistent notice to the footer (Notices); notice.clear
// removes them.
func (m *Model) Notify(text string) { m.Notices = append(m.Notices, text) }

// footer is the status row: the error of the last key when there is one,
// else the hints for the focus; the notices, when there are any, follow
// the error or lead the hints.
func (m *Model) footer() Footer {
	jobs := m.JobsTicker(m.Width)
	notice := ""
	if len(m.Notices) > 0 {
		notice = "Notice: " + safeText(strings.Join(m.Notices, "; "))
	}
	if m.Status != "" {
		text := "Error: " + safeText(m.Status)
		if notice != "" {
			text += " · " + notice
		}
		return Footer{Text: text, Error: true, Jobs: jobs}
	}
	hints := BindingHints(m.Runtime, m.Runtime.Focus)
	switch {
	case notice == "":
		return Footer{Text: hints, Jobs: jobs}
	case hints == "":
		return Footer{Text: notice, Jobs: jobs}
	}
	return Footer{Text: notice + " · " + hints, Jobs: jobs}
}

// BindingHints renders the rows a focus answers to in its current mode as a
// hint line, from the innermost level outward (the focused instance's rows,
// its noun's, then its parents' up to the page and app):
// "enter demo.open · : palette.open · ? bindings.help". Motions
// (Operation.Motion) and hidden rows (counts, registers, macros, ctrl+c)
// are left to ? help, and of rows that run the same request only the first
// (innermost) key is shown.
// It is the data bindings.list prints, so a hint never names a key that is
// not bound; the descriptions are in BindingHelp (the ? modal).
func BindingHints(r *Runtime, focus string) string {
	var hints []string
	index := map[string]bool{}
	for _, row := range r.groupedRows(focus) {
		b := row.Binding
		if b.Hidden || r.Operations[b.Request.Key()].Motion {
			continue
		}
		if isPrefixCancel(b) && r.Mode.Payload.Prefix == "" {
			continue // "d esc" matters only once d is pending
		}
		if b.When != "" {
			// A row whose guard is false here would not answer: no hint
			// (bindings list keeps it, as the guard may hold for the key).
			if check := r.guard(b.When); check == nil || !check(r, KeyEvent{Mode: r.Mode, Focus: focus}) {
				continue
			}
		}
		same := fmt.Sprintf("%s\x00%v\x00%v\x00%s", b.Request.Key(), b.Request.Args, b.From, b.Hint)
		if index[same] && b.Request.Verb != "" {
			continue
		}
		index[same] = true
		hints = append(hints, rowHint(b))
	}
	return strings.Join(hints, " · ")
}

// groupedRows is levelRows grouped by the noun of the level that answers,
// innermost first, a capture row with its own level (app's ctrl+c comes
// after the focused row's verbs).
func (r *Runtime) groupedRows(focus string) []levelRow {
	rows := r.levelRows(focus)
	var order []string
	for _, capture := range []bool{false, true} {
		for _, row := range rows {
			if row.Capture == capture && !slices.Contains(order, row.level) {
				order = append(order, row.level)
			}
		}
	}
	out := make([]levelRow, 0, len(rows))
	for _, noun := range order {
		for _, row := range rows {
			if row.level == noun {
				out = append(out, row)
			}
		}
	}
	return out
}

// rowHint is one row as a hint: its key or pattern, then the row's own Hint
// when it has one, else the operation it runs (noun.verb).
func rowHint(b Binding) string {
	hint := modalLabel(rowKey(b))
	if b.Hint != "" {
		return hint + " " + modalLabel(b.Hint)
	}
	if b.Request.Verb == "" {
		return hint
	}
	return hint + " " + modalLabel(b.Request.Key())
}

// rowKey is a row's key sequence, or its pattern as <pattern>.
func rowKey(b Binding) string {
	if b.Keys == "" {
		return "<" + b.Pattern + ">"
	}
	return b.Keys
}

// HelpRow is one binding as the help shows it: the key, the operation it
// runs (noun.verb, or the row's Hint when it has one), the operation's
// description, the noun of the level that answers with it, and the help's
// section: that noun, or "movement" for a motion (Operation.Motion).
type HelpRow struct {
	Key         string `json:"key"`
	Operation   string `json:"operation"`
	Description string `json:"description,omitempty"`
	Noun        string `json:"noun"`
	Section     string `json:"section,omitempty"`
}

// MovementSection is the help section that holds every motion, after the
// app's rows.
const MovementSection = "movement"

// BindingHelp lists the rows a focus answers to in its current mode with
// their descriptions, grouped by the noun of the level that answers, from
// the innermost instance outward (the focused instance's rows, its noun's,
// its parents' up to the page, then app; a capture row sits with its own
// level, so app's ctrl+c comes after the focused row's verbs), and the
// motions of every level last, under "movement".
func BindingHelp(r *Runtime, focus string) []HelpRow {
	var out, motions []HelpRow
	for _, row := range r.groupedRows(focus) {
		op := row.Hint
		if op == "" && row.Request.Verb != "" {
			op = row.Request.Key()
		}
		help := HelpRow{Key: rowKey(row.Binding), Operation: op, Description: r.Operations[row.Request.Key()].Description, Noun: row.level, Section: row.level}
		if r.Operations[row.Request.Key()].Motion {
			help.Section = MovementSection
			motions = append(motions, help)
			continue
		}
		out = append(out, help)
	}
	return append(out, motions...)
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

// cursor shapes the block's cursor by the focused input's mode, or by the
// focused terminal's reported shape.
func (m *Model) cursor(block Block) *tea.Cursor {
	if block.Cursor == nil {
		return nil
	}
	context := m.Runtime.Context()
	cursor := tea.NewCursor(block.Cursor.X, block.Cursor.Y)
	cursor.Blink = false
	// A focused pane shows its own cursor, shaped as the pane reports.
	if term := m.Runtime.Terminal(m.Runtime.Focus); term != nil {
		cursor.Shape = term.cursorShape()
		return cursor
	}
	if m.Runtime.Input(context.InputID) == nil {
		return nil
	}
	switch mode := m.Runtime.Mode.Name; {
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
// the child cursor by where it placed the child; a modal hides the terminal
// cursor of everything beneath it, while the page keeps its highlights
// (beneath) and the backdrop dims them.
func (m *Model) render() Block {
	layout := m.layout()
	pageTitle := ""
	if page := m.Runtime.PageOf(m.Runtime.Page); page != nil {
		pageTitle = page.Title
	}
	header := Header{Title: m.Title, Subtitle: pageTitle, Badge: m.modeBadge()}
	if !m.Sidebar {
		header.Tabs, header.Active = m.pageTitles()
	}
	base := m.Runtime.Page
	if base == "" {
		base = "main"
	}
	pageWidth, pageHeight := m.pageSize()
	restore := m.beneath()
	content := m.pageContent(pageWidth, pageHeight)
	m.follow(base, content.Text, pageHeight)
	page := m.scrolled(base, content, pageWidth, pageHeight)
	restore()
	if m.Sidebar {
		titles, selected := m.pageTitles()
		width, height := layout.BodySize()
		page = layout.sidebar("Pages", titles, selected, pageTitle, page, width, height, m.Width < layoutStackAt)
	}
	surface := layout.Shell(header, page, m.footer())
	for _, id := range m.Runtime.modals {
		modalWidth, modalHeight := ModalSize(m.Width, m.Height)
		title, body := m.modalContent(id, modalWidth, modalHeight)
		bodyHeight := min(modalHeight, max(1, lipgloss.Height(body.Text)))
		bodyWidth := min(modalWidth, max(1, lipgloss.Width(body.Text)))
		body = m.scrolled(id, body, bodyWidth, bodyHeight)
		// The hint is the modal focus's own binding rows; it may widen the
		// panel up to a readable width but never past the modal bounds.
		hint := ansi.Truncate(BindingHints(m.Runtime, m.Runtime.ModalFocus(id)), min(modalWidth, max(bodyWidth, 48)), "…")
		if id == m.Runtime.TopModal() && m.Status != "" {
			hint = "Error: " + m.Status
		}
		surface = ComposeModalBlock(surface.Text, title, body, hint, m.Width, m.Height, layout.Styles)
	}
	return surface
}

// beneath shows the page renderer the level under the modals as it stands
// there: the focus and mode the outermost modal opened over, so a grid's
// cursor cell and selection, a list's selected row and a field's visual
// selection keep their highlight under a modal, which the backdrop then
// dims. The fields are set for the render and put back by the returned
// func; no setter runs, so nothing observes the swap and no state changes.
func (m *Model) beneath() (restore func()) {
	r := m.Runtime
	if len(r.modals) == 0 {
		return func() {}
	}
	focus, mode := r.Focus, r.Mode
	if outer := r.Instances[r.modals[0]]; outer != nil {
		r.Focus = outer.Payload.Return
		if outer.returnMode.Name != "" {
			r.Mode = outer.returnMode
		}
	}
	return func() { r.Focus, r.Mode = focus, mode }
}

// follow scrolls the page surface key, after a frame where the focus moved,
// so the focused element's lines (Page.focusLines, as this frame drew them)
// sit inside the height-line viewport with the page's scrolloff above and
// below; an element taller than the viewport is aligned to its top. A list
// has already windowed its rows around the selection with the same margin
// (windowOffset), so this moves only the page, once.
func (m *Model) follow(key, content string, height int) {
	r := m.Runtime
	page := r.PageOf(r.Page)
	if page == nil || m.followed[key] == r.Focus {
		return
	}
	m.followed[key] = r.Focus
	top, bottom, ok := page.focusLines(r, r.Focus)
	if !ok {
		return
	}
	total := strings.Count(content, "\n") + 1
	m.scroll[key] = scrollToShow(m.scroll[key], top, bottom, height, total, page.scrolloff())
}

// scrollToShow is the offset that shows lines [top, bottom) of total in a
// height-line view at offset with margin lines around them (fewer when the
// view is too short for both margins), moving as little as it can; a span
// taller than the view gets its top line first.
func scrollToShow(offset, top, bottom, height, total, margin int) int {
	margin = min(margin, max(0, (height-1)/2))
	if bottom-top > height {
		offset = top
	} else {
		if bottom+margin > offset+height {
			offset = bottom + margin - height
		}
		if top-margin < offset {
			offset = top - margin
		}
	}
	return min(max(0, offset), max(0, total-height))
}

// scrolled runs a block through the surface's viewport, moving the cursor
// with the scrolled content and dropping it when it scrolls out of view.
func (m *Model) scrolled(key string, content Block, width, height int) Block {
	view := m.viewport(key, content.Text, width, height)
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
	in.r.Placed(in.ID)
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
	text := body.String()
	// The page keeps the whole box on screen when it follows the focus.
	text = in.r.PlacedAt(in.ID, text, 0, strings.Count(text, "\n")+1)
	return Block{Text: text, Cursor: &Pos{X: cursorColumn - offset, Y: cursorRow - start}}
}
