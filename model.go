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

// ModalBody is a modal body component that draws itself: a modal whose
// instance's Component implements it needs no ModalView (a record editor
// opened as a modal, its chooser, a Chooser). A ModalView for the id still
// wins.
type ModalBody interface {
	ModalTitle() string
	ModalBlock(s Styles, width, height int) Block
}

// Model is the Bubble Tea adapter. Pages and modal instances retain their
// state in Runtime; components (picker, registers, application components)
// are instances the runtime drives through the delivery contract, so the
// adapter holds no component list of its own.
type Model struct {
	// Foreground answers a ForegroundMsg (a verb's Foreground program);
	// nil runs it through tea.ExecProcess, suspending the TUI. o-story
	// and tests replace it to run the program headless.
	Foreground func(ForegroundMsg) tea.Cmd
	Runtime    *Runtime
	Palette    *Palette
	Appearance *Appearance
	ModalViews map[string]ModalView
	Title      string
	Width      int
	Height     int
	// Status is the status line in its error tone: the last key's error,
	// or what a handler set with ctx.Error (ctx.Status); the footer reads
	// "Error: TEXT" in the error style in place of the hints. Info is the
	// line in its neutral tone (ctx.Info), drawn plain before the hints.
	// Setting one clears the other, and the next key clears both.
	Status string
	Info   string
	// Notices are persistent lines (a host's mount warnings), kept across
	// keys and errors until notice.clear (or the application) clears them.
	// The footer shows the newest before the hints, cut to half the width,
	// with "(+N more)" for the rest (notice.list has them all), so notices
	// never hide the hints. A notice does not go with the next key. The
	// Model is the runtime's UI sink (Runtime.UI), so ctx.Notify,
	// ctx.Error and ctx.Info land here.
	Notices   []string
	noticeIDs []NoticeID // the UI's id of each notice, 0 for one appended to Notices directly
	// Sidebar replaces page tabs with a navigation rail. Existing page operations
	// remain the only navigation state; input focus and modal routing are unchanged.
	Sidebar bool
	views   map[string]*viewport.Model
	scroll  map[string]int // modal ID or page surface key -> scroll offset
	// followed is the focus each page surface last scrolled to follow, so a
	// frame scrolls only when the focus moved (pgup and pgdown may leave it
	// off screen until the next motion).
	followed map[string]string
	ticker   jobTicker    // the finished jobs the ticker remembers (ticker.go)
	segTimer segmentTimer // the status segments' refresh timer (segment_view.go)
	// Now is the Model's clock: the ticker's memory of finished jobs and
	// the linger timer read it (time.Now by default, time.Now when nil). A
	// headless host fakes it, advances it to JobLingerTick's deadline and
	// delivers that message instead of waiting JobLinger.
	Now func() time.Time
}

// NewModel returns the Bubble Tea model that renders and drives r.
func NewModel(r *Runtime) *Model {
	var m *Model
	r.Builtin(func() { m = newModel(r) })
	return m
}

// newModel is NewModel; the operations it registers are builtin.
func newModel(r *Runtime) *Model {
	m := &Model{Runtime: r, Palette: NewPalette(r), Appearance: NewAppearance(r), ModalViews: map[string]ModalView{}, Title: "Ochazuke", Width: 80, Height: 24, views: map[string]*viewport.Model{}, scroll: map[string]int{}, followed: map[string]string{}, Now: time.Now}
	// The Model shows what handlers tell the user: notices in the footer,
	// the status in the status line. Messages posted before it existed
	// (setup) are replayed now.
	r.UI().Attach(modelSink{m})
	// Config problems (a skipped bindings row) are persistent notices,
	// each once, taken back when a reload no longer finds them.
	if c := r.Config(); c != nil && c.Report == nil {
		reported := map[string]NoticeID{}
		c.Report = func(problem string) {
			if id, ok := reported[problem]; ok && slices.Contains(m.noticeIDs, id) {
				return
			}
			reported[problem] = r.UI().Notify(problem)
		}
		c.Withdraw = func(problem string) {
			if id, ok := reported[problem]; ok {
				delete(reported, problem)
				r.UI().ClearNotice(id)
			}
		}
	}
	r.Settings().flushWarnings()
	// The framework's theme setting (EnsureConfigSettings) drives the
	// scheme; an application that declared its own theme applies it itself.
	r.Settings().Observe(ThemeSetting, func(_ string, v any) {
		if name, ok := v.(string); ok && r.Config().FrameworkTheme() {
			m.Appearance.Theme = name
		}
	})
	m.ModalViews[yankHistoryID] = ModalView{Title: "Yank history", Render: yankHistoryView}
	m.ModalViews["confirm"] = ModalView{Title: "Confirm", Render: confirmView}
	m.ModalViews[paletteResultsID] = ModalView{Title: "Results", Render: paletteResultsView}
	// The Settings page and unfocused page lists draw in the Model's styles.
	r.Settings().page.styles = m.Styles
	m.ModalViews[settingPromptID] = ModalView{Title: "Set", Render: settingPromptView}
	r.Register(Operation{Verb: "scroll", Noun: "ochazuke.surface", Description: "Scroll the active page or modal without changing focus", Parameters: map[string]Parameter{"lines": {Type: "integer", Required: true}}, Result: "scroll offset", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
	r.Register(Operation{Verb: "list", Noun: "ochazuke.notice", Description: "List the persistent notices shown in the footer", Result: "string[]", Run: func(*Runtime, Context, Arguments) (any, error) {
		return append([]string{}, m.Notices...), nil
	}})
	r.Register(Operation{Verb: "clear", Noun: "ochazuke.notice", Description: "Clear the persistent notices from the footer", Result: "cleared count", Run: func(*Runtime, Context, Arguments) (any, error) {
		n := len(m.Notices)
		m.Notices, m.noticeIDs = nil, nil
		return n, nil
	}})
	// The app noun's keys (bindings/app.json): :, ?, ', ctrl+j and ctrl+k,
	// /, ctrl+space, ctrl+c, pgup and pgdown, and ctrl+\ for the help in
	// every input's insert mode (HelpInsertKey; insert mode types ?). The
	// file's groups are fallbacks: a key the application bound on the app
	// noun (or the editor's insert table) before NewModel keeps its row.
	// ctrl+c quits from a capture row, so an inner any row cannot swallow
	// it; inside a modal the capture row stands aside and the bubbling row
	// below answers unless the modal binds ctrl+c. In a passthrough mode (an
	// attached terminal) there is no capture pass: ctrl+c goes to the pane.
	r.loadFrameworkBindings("app.json")
	return m
}

// Init installs the user actions file (Runtime.EnsureUserActions) and
// applies the user bindings file (Runtime.EnsureUserBindings), after the
// application defined its own nouns and rows, declares the framework's settings
// (EnsureConfigSettings), reads settings.json (its problems are notices),
// draws themes.json's schemes and the theme setting's (applyThemeSetting),
// restores and persists the action history (EnsureHistory), leaves a start
// page the pages.hidden setting hides for the first page in the tabs
// (OpenVisibleStart), starts the settings.json poll (SettingsPoll) when the
// application declared settings, and asks for the background colour.
func (m *Model) Init() tea.Cmd {
	m.Runtime.EnsureUserActions()
	m.Runtime.EnsureUserBindings()
	m.Runtime.EnsureConfigSettings()
	m.Runtime.Settings().ensure()
	m.applyThemeSetting()
	m.Runtime.EnsureHistory()
	m.Runtime.EnsureNounPlaces()
	if err := m.Runtime.OpenVisibleStart(); err != nil {
		m.Status, m.Info = err.Error(), ""
	}
	return tea.Batch(tea.RequestBackgroundColor, m.settingsPoll(), m.segmentCommand(m.clock()))
}
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// A foreground program suspends the TUI while it has the terminal
	// (foreground.go); its exit comes back as the job's completion.
	if fg, ok := msg.(ForegroundMsg); ok {
		if !m.Runtime.Accepts(fg.Ticket) {
			return m, nil
		}
		return m, m.foregroundCmd(fg)
	}
	// Off-loop results reach their owning instance first; a stale or
	// orphaned completion is dropped here.
	if _, err := m.Runtime.Deliver(msg); err != nil {
		m.Status, m.Info = err.Error(), ""
	}
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = max(1, v.Width)
		m.Height = max(1, v.Height)
	case CallMsg:
		m.Runtime.HandleCall(v)
	case BindingTimeoutMsg:
		status, info := m.Status, m.Info
		m.Status, m.Info = "", "" // what the timed-out request sets stands
		if handled, err := m.Runtime.HandleBindingTimeout(v); handled {
			if err != nil {
				m.Status, m.Info = err.Error(), ""
			}
			m.Palette.sync()
		} else {
			m.Status, m.Info = status, info
		}
	case JobLingerMsg:
		m.ticker.fired(v)
	case SegmentTickMsg:
		m.segmentFired(v)
	case SegmentMsg:
		m.Runtime.settleSegment(v, m.clock())
	case SettingsPollMsg:
		m.Runtime.Settings().ReloadIfChanged()
	case tea.BackgroundColorMsg:
		m.Appearance.Dark = v.IsDark()
	case tea.KeyPressMsg:
		m.handleKey(MacroKey{Key: v.String()})
	case tea.PasteMsg:
		// Bubble Tea v1 spelled a paste "[text]"; macros and the editor still expect it.
		m.handleKey(MacroKey{Key: pasteKey(v.Content), Paste: true})
	}
	m.ticker.observe(m.Runtime, m.clock())
	m.Runtime.settlePlacements()
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
	if cmd := m.segmentCommand(m.clock()); cmd != nil {
		commands = append(commands, cmd)
	}
	if _, ok := msg.(SettingsPollMsg); ok {
		commands = append(commands, m.settingsPoll())
	}
	return m, tea.Batch(commands...)
}

// applyThemeSetting draws the user's schemes and, when settings.json names
// one in the framework's theme setting, selects it (else the theme the
// application set stays).
func (m *Model) applyThemeSetting() {
	c := m.Runtime.Config()
	m.Appearance.Schemes = NewSchemeSet(c.Schemes())
	if !c.FrameworkTheme() {
		return
	}
	if v, err := m.Runtime.Settings().Get(ThemeSetting); err == nil && v.Set {
		if name, ok := v.Value.(string); ok {
			m.Appearance.Theme = name
		}
	}
}

// handleKey dispatches one key through the runtime's stack walk and shows
// the error, if any, in the status line.
func (m *Model) handleKey(key MacroKey) {
	m.Status, m.Info = "", "" // a key clears the status line; what its handler sets stands
	if _, err := m.Runtime.Dispatch(key); err != nil {
		m.Status, m.Info = err.Error(), ""
	}
	m.Palette.sync()
}

// layout is the shell's chrome for the current viewport; page renderers get
// its BodySize, less the sidebar when one is shown.
func (m *Model) layout() Layout {
	return Layout{Width: m.Width, Height: m.Height, Styles: m.Styles()}
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
		return "Command palette", m.Palette.viewBlock(m.Styles(), width, height)
	case m.ModalViews[id].Render != nil:
		body = m.ModalViews[id].Render(m.Runtime, m.Runtime.Instances[id], width, height)
	case m.modalBody(id) != nil:
		mb := m.modalBody(id)
		body = mb.ModalBlock(m.Styles(), width, height)
		if id != m.Runtime.TopModal() {
			body.Cursor = nil
		}
		return mb.ModalTitle(), body
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

// modalBody is the modal instance's self-drawing component, or nil.
func (m *Model) modalBody(id string) ModalBody {
	if inst := m.Runtime.Instances[id]; inst != nil {
		if mb, ok := inst.Component.(ModalBody); ok {
			return mb
		}
	}
	return nil
}

// pageTitles lists the tab titles in order (TabPages: hidden pages left
// out) and the index of the current page.
func (m *Model) pageTitles() ([]string, int) {
	pages := m.Runtime.TabPages()
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

// Notify adds a persistent notice to the footer (Notices) through the
// runtime's UI, as ctx.Notify does, and returns its id; notice.clear
// removes them.
func (m *Model) Notify(text string) NoticeID { return m.Runtime.UI().Notify(text) }

// modelSink is the Model as the runtime's UISink.
type modelSink struct{ m *Model }

// Notify appends the notice to the footer's.
func (s modelSink) Notify(id NoticeID, text string) {
	m := s.m
	m.alignNoticeIDs()
	m.Notices = append(m.Notices, text)
	m.noticeIDs = append(m.noticeIDs, id)
}

// ClearNotice removes the notice posted as id.
func (s modelSink) ClearNotice(id NoticeID) {
	m := s.m
	m.alignNoticeIDs()
	if i := slices.Index(m.noticeIDs, id); i >= 0 {
		m.Notices = slices.Delete(m.Notices, i, i+1)
		m.noticeIDs = slices.Delete(m.noticeIDs, i, i+1)
	}
}

// Status replaces the status line with an error.
func (s modelSink) Status(text string) { s.m.Status, s.m.Info = text, "" }

// Info replaces the status line with a plain message.
func (s modelSink) Info(text string) { s.m.Status, s.m.Info = "", text }

// alignNoticeIDs keeps one id per notice when the application changed
// Notices itself: a notice it appended has no id (0), one it dropped from
// the end takes its id along.
func (m *Model) alignNoticeIDs() {
	for len(m.noticeIDs) < len(m.Notices) {
		m.noticeIDs = append(m.noticeIDs, 0)
	}
	m.noticeIDs = m.noticeIDs[:len(m.Notices)]
}

// footerNotice is the newest notice as the footer shows it: at most half
// the width, with how many older ones notice.list holds, so a notice never
// takes over the footer and the hints keep the rest of the line.
func (m *Model) footerNotice() string {
	n := len(m.Notices)
	if n == 0 {
		return ""
	}
	text := "Notice: " + safeText(m.Notices[n-1])
	more := ""
	if n > 1 {
		more = fmt.Sprintf(" (+%d more)", n-1)
	}
	if m.Width > 0 {
		limit := max(16, m.Width/2) - ansi.StringWidth(more)
		text = ansi.Truncate(text, max(8, limit), "…")
	}
	return text + more
}

// footer is the status row: the error of the last key (or ctx.Error) when
// there is one, in place of the hints; else the hints for the focus, led
// by the plain status (ctx.Info) cut to half the width and the newest
// notice. The jobs ticker's entries ride along for the rule line above it
// (Layout.TickerRule).
func (m *Model) footer() Footer {
	jobs := m.JobsTicker(m.Width)
	notice := m.footerNotice()
	if m.Status != "" {
		text := "Error: " + safeText(m.Status)
		if notice != "" {
			text += " · " + notice
		}
		return Footer{Text: text, Error: true, Jobs: jobs}
	}
	var parts []string
	if info := m.footerInfo(); info != "" {
		parts = append(parts, info)
	}
	if notice != "" {
		parts = append(parts, notice)
	}
	if hints := BindingHints(m.Runtime, m.Runtime.Focus); hints != "" {
		parts = append(parts, hints)
	}
	return Footer{Text: strings.Join(parts, " · "), Jobs: jobs}
}

// footerInfo is the plain status as the footer shows it: one line, at most
// half the width, so the hints keep the rest.
func (m *Model) footerInfo() string {
	if m.Info == "" {
		return ""
	}
	text := safeText(strings.ReplaceAll(m.Info, "\n", " "))
	if m.Width > 0 {
		text = ansi.Truncate(text, max(8, max(16, m.Width/2)), "…")
	}
	return text
}

// BindingHints renders the rows a focus answers to in its current mode as a
// hint line, from the innermost level outward (the focused instance's rows,
// its noun's, then its parents' up to the page and app):
// "enter demo.open · : palette.open · ? bindings.help". Motions
// (Operation.Motion) and hidden rows (counts, registers, macros, ctrl+c)
// are left to ? help, except a motion row with its own Hint (J reads
// "J/K detail"; a hint that starts with the row's key and a slash is shown
// alone), and of rows that run the same request only the first
// (innermost) key is shown.
// It is the data bindings.list prints, so a hint never names a key that is
// not bound; the descriptions are in BindingHelp (the ? modal).
func BindingHints(r *Runtime, focus string) string {
	var hints []string
	for _, b := range HintBindings(r, focus) {
		hints = append(hints, rowHint(r, b))
	}
	return strings.Join(hints, " · ")
}

// HintBindings is the rows BindingHints shows, in its order: what a check
// of the footer presses to prove every hint answers.
func HintBindings(r *Runtime, focus string) []Binding {
	var out []Binding
	index := map[string]bool{}
	for _, row := range r.groupedRows(focus) {
		b := row.Binding
		if b.Hidden || r.Operations[b.Request.Key()].Motion && b.Hint == "" {
			continue // a motion with its own Hint (J/K detail) shows
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
		out = append(out, b)
	}
	return out
}

// groupedRows is levelRows grouped by the noun of the level that answers,
// innermost first, a capture row with its own level (app's ctrl+c comes
// after the focused row's verbs).
func (r *Runtime) groupedRows(focus string) []levelRow { return groupLevelRows(r.levelRows(focus)) }

// groupLevelRows groups rows in walk order by their level (groupedRows).
func groupLevelRows(rows []levelRow) []levelRow {
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
// when it has one, else the operation it runs (noun.verb, the noun by its
// shortest name: DisplayKey).
func rowHint(r *Runtime, b Binding) string {
	hint := modalLabel(rowKey(b))
	if b.Hint != "" && strings.HasPrefix(b.Hint, rowKey(b)+"/") {
		return modalLabel(b.Hint) // the hint names its keys: "J/K detail"
	}
	if b.Hint != "" {
		return hint + " " + modalLabel(b.Hint)
	}
	if b.Request.Verb == "" {
		return hint
	}
	return hint + " " + modalLabel(r.DisplayKey(b.Request.Key()))
}

// rowKey is a row's key sequence, or its pattern as <pattern>.
func rowKey(b Binding) string {
	if b.Keys == "" {
		return "<" + b.Pattern + ">"
	}
	return b.Keys
}

// HelpRow is one binding as the help shows it: the key, the operation it
// runs (noun.verb, or the row's Hint when it has one), arguments resolved
// when help opens, its description, the answering noun, and the help's
// section: that noun, or "movement" for a motion (Operation.Motion).
// Prefix and Final are Key split by SplitKeys (the help's prefix and key
// columns); Request is the operation the row runs (noun.verb, the noun
// by its shortest name, also when Operation shows the Hint; "" for a mode
// transition) and Hint the row's own Hint.
type HelpRow struct {
	Key         string    `json:"key"`
	Prefix      string    `json:"prefix,omitempty"`
	Final       string    `json:"final,omitempty"`
	Operation   string    `json:"operation"`
	Request     string    `json:"request,omitempty"`
	Hint        string    `json:"hint,omitempty"`
	Args        Arguments `json:"args,omitempty"`
	Description string    `json:"description,omitempty"`
	Noun        string    `json:"noun"`
	Section     string    `json:"section,omitempty"`
	// Guard is the guard a hypothetical row (the help's pick and table
	// views) answers under; the live help judges guards and leaves it out.
	Guard string `json:"when,omitempty"`
	// Aliases are the other names of the row's operation (ActionAlias),
	// listed once on the row and after its description ("also: …"); the
	// row itself always names the canonical operation.
	Aliases []string `json:"aliases,omitempty"`
	// Enters is the pending mode the row's key enters when the help lists
	// that mode's rows after it (the terminal leader: leader_pending).
	Enters string `json:"enters,omitempty"`
	// Group marks a prefix row: its key starts longer sequences (g of
	// g c, the leader ctrl+a), which are listed Under it. A prefix bound
	// to nothing alone is a row helpGroups makes, with no Operation.
	Group bool `json:"group,omitempty"`
	// Under is the prefix row this row is listed under (its longest
	// prefix that is a Group row: "ctrl+a" for "ctrl+a v", "g c" for
	// "g c x"); "" at the top of its section. Key is the whole sequence.
	Under string `json:"under,omitempty"`
	// Steps are a sequence's keys one by one, each with what it does at
	// that point: entering a mode, waiting for the next key, then the
	// final operation. A single key has none.
	Steps []HelpStep `json:"steps,omitempty"`
	// cancel marks a generated prefix cancel (d esc), which helpGroups
	// lists in the section its sequences are in and never counts.
	cancel bool
}

// MovementSection is the help section that holds every motion, after the
// app's rows.
const MovementSection = "movement"

// BindingHelp lists the rows a focus answers to in its current mode with
// their descriptions, grouped by the noun of the level that answers, from
// the innermost instance outward (the focused instance's rows, its noun's,
// its parents' up to the page, then app; a capture row sits with its own
// level, so app's ctrl+c comes after the focused row's verbs), and the
// motions of every level last, under "movement". A row whose key enters a
// pending mode (the terminal leader) is followed by that mode's rows as
// whole sequences (helpEnters, R6.10).
func BindingHelp(r *Runtime, focus string) []HelpRow {
	w := helpWalk{
		args:  func(b Binding) Arguments { return bindingHelpArgs(r, b, focus) },
		focus: focus,
		mode:  r.Mode,
		enter: func(mode ModeState) []levelRow {
			return groupLevelRows(r.scanRows(r.walk(focus), mode, focus, false))
		},
	}
	return helpGroups(r.helpRows(w, r.groupedRows(focus), 0))
}

// NounHelp is BindingHelp for a hypothetical focus on noun in mode, with
// no instance (nounRows: the noun's tables, app, the editor fallback for
// an input noun): a guarded row is listed with its Guard, its operation
// marked "?", and the rows answering when that guard is false follow it;
// arguments a row takes from the key, the payload or the row read as
// "$source".
func NounHelp(r *Runtime, noun string, mode ModeState) []HelpRow {
	return PlaceHelp(r, NounPlace{Noun: noun}, mode)
}

// PlaceHelp is NounHelp with a parent chain: the hypothetical focus on
// place's noun has the levels the place recorded between it and app (its
// list, its page; the modal noun inside a modal), each answering with its
// noun's tables, so a list row's keys from its list and page are there.
func PlaceHelp(r *Runtime, place NounPlace, mode ModeState) []HelpRow {
	w := helpWalk{
		args: sourceArgs,
		mode: mode,
		enter: func(mode ModeState) []levelRow {
			return groupLevelRows(r.nounRows(place, mode))
		},
	}
	return helpGroups(r.helpRows(w, groupLevelRows(r.nounRows(place, mode)), 0))
}

// helpWalk is how helpRows reads a walk: the arguments a row shows, the
// focus (live) or "" (a hypothetical walk: no guard judged, each guarded
// row marked), the mode the rows answer in, and the rows the same walk
// answers with in a pending mode a row enters.
type helpWalk struct {
	args  func(Binding) Arguments
	focus string
	mode  ModeState
	enter func(ModeState) []levelRow
}

// helpRows is the help rows of grouped level rows. depth counts the
// pending modes expanded around them (helpEnters).
func (r *Runtime) helpRows(w helpWalk, rows []levelRow, depth int) []HelpRow {
	var out, motions []HelpRow
	// A generated prefix cancel ("d esc") is listed only while a sequence
	// on its prefix answers here: a read-only outline's guarded "d d" takes
	// its "d esc" out of the help with it.
	sequences := map[string]bool{}
	exact := map[string]levelRow{}
	for _, row := range rows {
		if !isPrefixCancel(row.Binding) {
			parts := strings.Fields(row.Keys)
			for i := 1; i < len(parts); i++ {
				sequences[row.Mode+"\x00"+strings.Join(parts[:i], " ")] = true
			}
		}
		if _, seen := exact[row.Keys]; row.Keys != "" && !seen {
			exact[row.Keys] = row
		}
	}
	for _, row := range rows {
		if isPrefixCancel(row.Binding) && !sequences[row.Mode+"\x00"+strings.TrimSuffix(row.Keys, " "+CancelKey)] || w.focus != "" && r.guardedOffIn(row.Binding, w.focus, w.mode) {
			continue
		}
		if depth > 0 && row.Hidden {
			continue // a pending mode's hidden rows (the leader's any) stay out
		}
		help := r.helpRow(row, w.args(row.Binding))
		help.cancel = isPrefixCancel(row.Binding)
		if row.guard != "" {
			help.Operation += " ?"
			help.Description = strings.TrimSuffix("when "+row.guard+" · "+help.Description, " · ")
		}
		if entered, sub := r.helpEnters(w, row, depth); sub != nil {
			out = append(out, r.enteringRows(help, row, entered, sub, exact)...)
			continue
		}
		help.Steps = r.helpSteps(help, exact, HelpStep{Key: help.Final, Operation: help.Operation, Description: help.Description})
		if depth == 0 && r.Operations[row.Request.Key()].Motion {
			help.Section = MovementSection
			motions = append(motions, help)
			continue
		}
		out = append(out, help)
	}
	return append(out, motions...)
}

// helpRow is one level row as the help shows it: the row's Hint or its
// operation, the description (the row's Help over the operation's), the
// answering level as its noun and section.
func (r *Runtime) helpRow(row levelRow, args Arguments) HelpRow {
	req := ""
	if row.Request.Verb != "" {
		req = r.DisplayKey(row.Request.Key())
	}
	op := row.Hint
	if op == "" {
		op = req
	}
	desc := r.Operations[row.Request.Key()].Description
	if row.Help != "" {
		desc = row.Help
	}
	var aliases []string
	if row.Request.Verb != "" {
		aliases = r.aliasLabels(row.Request.Key())
	}
	if len(aliases) > 0 {
		desc = strings.TrimPrefix(desc+" · also: "+strings.Join(aliases, ", "), " · ")
	}
	key := rowKey(row.Binding)
	prefix, final := SplitKeys(key)
	return HelpRow{Key: key, Prefix: prefix, Final: final, Operation: op, Request: req, Hint: row.Hint, Args: args, Description: desc, Noun: row.level, Section: r.DisplayNoun(row.level), Guard: row.guard, Aliases: aliases}
}

// SplitKeys splits a key sequence into its prefix and its final key: "z l"
// is "z" and "l", "ctrl+a v" is "ctrl+a" and "v", "g c x" is "g c" and
// "x"; a single key (or a pattern, "<digit>") has no prefix.
func SplitKeys(keys string) (prefix, final string) {
	parts := strings.Fields(keys)
	if len(parts) < 2 {
		return "", strings.TrimSpace(keys)
	}
	return strings.Join(parts[:len(parts)-1], " "), parts[len(parts)-1]
}

// View renders the shell as a full-screen tea.View; View().Content is the
// string. The terminal cursor follows the rendered block through the page or
// modal that holds it: a bar in insert mode, a block in normal mode and an
// underline in visual modes. It is hidden when the block reports no cursor.
func (m *Model) View() tea.View {
	block := m.render()
	v := tea.NewView(block.Text)
	v.AltScreen = true
	v.Cursor = m.cursor(block)
	return v
}

// cursor shapes the block's cursor by mode, or by the focused terminal's
// reported shape.
func (m *Model) cursor(block Block) *tea.Cursor {
	if block.Cursor == nil {
		return nil
	}
	cursor := tea.NewCursor(block.Cursor.X, block.Cursor.Y)
	cursor.Blink = false
	// A focused pane shows its own cursor, shaped as the pane reports.
	if term := m.Runtime.Terminal(m.Runtime.Focus); term != nil {
		cursor.Shape = term.cursorShape()
		return cursor
	}
	switch mode := m.Runtime.Mode.Name; {
	case mode == "insert":
		cursor.Shape = tea.CursorBar
		cursor.Blink = true
	case editorShape(m.Runtime, mode) != "":
		cursor.Shape = tea.CursorUnderline
	default:
		cursor.Shape = tea.CursorBlock
	}
	return cursor
}

// Styles is the Model's appearance with the dim settings applied
// (modal-dim, dim-unfocused); the shell, modals and page lists draw in it.
func (m *Model) Styles() Styles {
	r := m.Runtime
	return NewStyles(*m.Appearance).WithDims(r.dimSetting(ModalDimSetting, DefaultModalDim), r.dimSetting(DimUnfocusedSetting, DefaultDimUnfocused))
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
	header := Header{Title: m.Title, Subtitle: pageTitle, Badge: m.modeBadge(), Focus: m.Runtime.focusDisplayLevels(), Segments: m.Runtime.SegmentCells()}
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
	if m.Runtime.keepFocusShown() {
		// The focus rested on a stop this frame did not draw: it moved to
		// one it did (R3.13), so draw the page again around the new focus
		// (its highlight, its scroll) before the footer reads it.
		header.Badge, header.Focus = m.modeBadge(), m.Runtime.focusDisplayLevels()
		content = m.pageContent(pageWidth, pageHeight)
	}
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
		if id == m.Runtime.TopModal() {
			switch {
			case m.Status != "":
				hint = "Error: " + m.Status
			case m.Info != "":
				hint = ansi.Truncate(m.footerInfo()+" · "+hint, min(modalWidth, max(bodyWidth, 48)), "…")
			}
		}
		surface = ComposeModalBlock(surface.Text, title, body, hint, m.Width, m.Height, layout.Styles)
	}
	// Open suggestions sit over everything, under the token they complete.
	if c := m.Runtime.Completer(); c != nil {
		surface = c.overlay(surface, m.Width, m.Height, layout.Styles)
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
// Visual selections and an open select range are painted in reverse video
// (the visual-selection style); the cursor itself is left
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
	if in.shape() == SelectionBlock && (cursor == len(in.Text) || in.Text[cursor] == '\n') {
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
	painted := in.shape() != ""
	// An open select range (SelectText) is drawn as a visual selection.
	if start, end, ok := in.SelectedText(); ok {
		lo, hi, painted = start, end, true
	}
	top, bottom, left, right := 0, -1, 0, 0
	block := in.shape() == SelectionBlock
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
		selected := painted && i < hi && runeIndex > lo
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
