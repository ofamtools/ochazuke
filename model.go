package ochazuke

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// ModalView renders one modal without replacing or inspecting its parent page.
type ModalView struct {
	Title  string
	Render func(*Runtime, *Frame, int, int) string
}

// Model is the Bubble Tea adapter. Pages and modal instances retain their state in Runtime.
type Model struct {
	Runtime    *Runtime
	Palette    *Palette
	Appearance *Appearance
	Pickers    []*Picker
	ModalViews map[string]ModalView
	Title      string
	Width      int
	Height     int
	Status     string
	views      map[string]*viewport.Model
}

func NewModel(r *Runtime) *Model {
	m := &Model{Runtime: r, Palette: NewPalette(r), Appearance: NewAppearance(r), ModalViews: map[string]ModalView{}, Title: "Ochazuke", Width: 80, Height: 24, views: map[string]*viewport.Model{}}
	r.dispatchKey = m.dispatchKey
	m.ModalViews[yankHistoryID] = ModalView{Title: "Yank history", Render: yankHistoryView}
	r.Register(Operation{Verb: "scroll", Noun: "surface", Description: "Scroll the active page or modal without changing focus", Parameters: map[string]Parameter{"lines": {Type: "integer", Required: true}}, Result: "scroll offset", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		frame := r.Frames[len(r.Frames)-1]
		width, height := m.pageSize()
		var content string
		if len(r.Frames) > 1 {
			width, height = ModalSize(m.Width, m.Height)
			_, content = m.modalContent(frame, width, height)
		} else {
			content = m.pageContent(width, height)
		}
		view := m.viewport(frame, content, width, height)
		delta := min(max(IntArg(a, "lines", 0), -view.TotalLineCount()), view.TotalLineCount())
		view.SetYOffset(view.YOffset + delta)
		frame.State["scroll"] = view.YOffset
		return view.YOffset, nil
	}})
	defaults := []Binding{
		{Mode: "normal", Keys: "ctrl+p", Request: Request{Verb: "open", Noun: "palette"}},
		{Mode: "normal", Keys: "ctrl+y", Request: Request{Verb: "open", Noun: "yank-history"}},
		{Keys: "ctrl+@", Request: Request{Verb: "open", Noun: "palette"}},
		{Keys: "ctrl+c", Request: Request{Verb: "quit", Noun: "app"}},
		{Keys: "alt+right", Request: Request{Verb: "next", Noun: "page"}},
		{Keys: "alt+left", Request: Request{Verb: "previous", Noun: "page"}},
		{Keys: "pgdown", Request: Request{Verb: "scroll", Noun: "surface", Args: Arguments{"lines": 8}}},
		{Keys: "pgup", Request: Request{Verb: "scroll", Noun: "surface", Args: Arguments{"lines": -8}}},
	}
	r.Layers["app"].Bindings = append(defaults, r.Layers["app"].Bindings...)
	return m
}
func (m *Model) Init() tea.Cmd { return nil }
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	for _, picker := range m.Pickers {
		picker.HandleMessage(msg)
	}
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = max(1, v.Width)
		m.Height = max(1, v.Height)
	case CallMsg:
		m.Runtime.HandleCall(v)
		m.Palette.publish()
	case tea.KeyMsg:
		if v.Type == tea.KeyRunes && !v.Paste && !v.Alt && len(v.Runes) > 1 {
			var commands []tea.Cmd
			for i := range v.Runes {
				single := v
				single.Runes = v.Runes[i : i+1]
				_, cmd := m.Update(single)
				if cmd != nil {
					commands = append(commands, cmd)
				}
				if m.Runtime.Quit {
					break
				}
			}
			return m, tea.Batch(commands...)
		}
		err := m.dispatchKey(MacroKey{Key: v.String(), Paste: v.Paste})
		if err != nil {
			m.Status = err.Error()
		} else {
			m.Status = ""
		}
	}
	if m.Runtime.Quit {
		return m, tea.Quit
	}
	var commands []tea.Cmd
	for _, picker := range m.Pickers {
		if cmd := picker.PendingCommand(); cmd != nil {
			commands = append(commands, cmd)
		}
	}
	return m, tea.Batch(commands...)
}

// dispatchKey is shared by live input and macro playback, including modal and
// layer bindings. Pasted text stays one literal event, never a command sequence.
func (m *Model) dispatchKey(key MacroKey) error {
	r := m.Runtime
	in := r.Inputs[r.Context().InputID]
	stop := key.Key == "q" && !key.Paste && r.Mode(r.Context()) == "normal" && r.Macro.Pending == "" && (in == nil || in.Pending == "")
	if r.Macro.Recording != "" && r.Macro.Depth == 0 && !stop {
		if len(r.Macro.Keys) >= 10000 {
			return fmt.Errorf("macro recording exceeds 10000 keys")
		}
		r.Macro.Keys = append(r.Macro.Keys, key)
	}
	if !key.Paste && (r.Macro.Pending != "" || r.Macro.Recording != "" && stop) {
		handled, err := r.handleMacroKey(key.Key)
		if handled || err != nil {
			return err
		}
	}
	if m.Palette.IsActive() {
		_, err := m.Palette.HandleKey(key.Key)
		return err
	}
	// A field's active grammar wins over unrelated app bindings, e.g. dw
	// when w also switches a page. Fresh keys still honor layer overrides.
	if !key.Paste && (r.Macro.Pending != "" || in == nil && r.Macro.Count > 0 || in != nil && (in.Pending != "" || in.Count > 0)) {
		handled, err := HandleInputKey(r, key.Key)
		if handled || err != nil {
			return err
		}
	}
	handled, err := r.HandleBinding(key.Key)
	if handled || err != nil {
		return err
	}
	if key.Key == "esc" && len(r.Frames) > 1 && r.Mode(r.Context()) == "normal" && (in == nil || in.Pending == "" && in.Count == 0) {
		_, err = r.Execute(Request{Verb: "pop", Noun: "modal"})
		return err
	}
	_, err = HandleInputKey(r, key.Key)
	return err
}
func (m *Model) pageSize() (int, int) {
	pad := NewStyles(*m.Appearance).Padding
	return max(1, m.Width-2*pad), max(1, m.Height-6)
}
func (m *Model) pageContent(width, height int) string {
	if page := m.Runtime.Pages[m.Runtime.ActivePage]; page != nil {
		return page.Render(m.Runtime, width, height)
	}
	return "No page selected. Open a registered page through the palette or API."
}
func (m *Model) viewport(frame *Frame, content string, width, height int) *viewport.Model {
	view := m.views[frame.ID]
	if view == nil {
		v := viewport.New(width, height)
		view = &v
		m.views[frame.ID] = view
	}
	view.Width = max(1, width)
	view.Height = max(1, height)
	view.SetContent(content)
	view.SetYOffset(IntArg(Arguments(frame.State), "scroll", 0))
	return view
}
func (m *Model) modalContent(frame *Frame, width, height int) (string, string) {
	if frame == m.Palette.frame {
		return "Command palette", strings.TrimPrefix(m.Palette.ViewSized(width, height), "Command palette\n")
	}
	if view, ok := m.ModalViews[frame.ID]; ok {
		return view.Title, view.Render(m.Runtime, frame, width, height)
	}
	if input := m.Runtime.Inputs[frame.Context.InputID]; input != nil {
		return frame.ID, InputView(input, width, height)
	}
	return frame.ID, "This modal has no focused input."
}
func (m *Model) View() string {
	styles := NewStyles(*m.Appearance)
	width, height := m.pageSize()
	pageTitle := ""
	if page := m.Runtime.Pages[m.Runtime.ActivePage]; page != nil {
		pageTitle = page.Title
	}
	heading := styles.Title.Render(safeText(m.Title)) + "  " + styles.Muted.Render(safeText(pageTitle))
	mode := styles.Badge.Render(strings.ToUpper(m.Runtime.Mode(m.Runtime.Context())))
	gap := max(1, m.Width-lipgloss.Width(heading)-lipgloss.Width(mode)-2*styles.Padding)
	heading += strings.Repeat(" ", gap) + mode
	tabs := make([]string, 0, len(m.Runtime.PageOrder))
	for i, id := range m.Runtime.PageOrder {
		label := fmt.Sprintf("%d %s", i+1, safeText(m.Runtime.Pages[id].Title))
		style := styles.Tab
		if id == m.Runtime.ActivePage {
			style = styles.ActiveTab
		}
		tabs = append(tabs, style.Render(label))
	}
	base := m.Runtime.Frames[0]
	content := m.pageContent(width, height)
	page := m.viewport(base, content, width, height).View()
	footer := "Alt-←/→ pages · Ctrl-P palette · PgUp/PgDn scroll · Ctrl-C quit"
	if m.Runtime.Mode(m.Runtime.Context()) == "insert" {
		footer = "INSERT · Esc normal · Ctrl-A/E line · Ctrl-W/U/K kill · Ctrl-Space palette"
	}
	status := styles.Muted.Render(footer)
	if m.Runtime.Pending != "" {
		status = styles.Accent.Render("Pending " + safeText(m.Runtime.Pending) + " · Esc cancel")
	}
	if m.Status != "" {
		status = styles.Error.Render("Error: " + safeText(m.Status))
	}
	inset := lipgloss.NewStyle().PaddingLeft(styles.Padding).PaddingRight(styles.Padding)
	background := strings.Join([]string{inset.Render(heading), inset.Render(lipgloss.JoinHorizontal(lipgloss.Top, tabs...)), "", inset.Render(page), "", inset.Render(status)}, "\n")
	background = fitSurface(background, m.Width, m.Height)
	for _, frame := range m.Runtime.Frames[1:] {
		modalWidth, modalHeight := ModalSize(m.Width, m.Height)
		title, body := m.modalContent(frame, modalWidth, modalHeight)
		bodyHeight := min(modalHeight, max(1, lipgloss.Height(body)))
		bodyWidth := min(modalWidth, max(1, lipgloss.Width(body)))
		body = m.viewport(frame, body, bodyWidth, bodyHeight).View()
		hint := "Esc normal / close · PgUp/PgDn scroll"
		if frame == m.Palette.frame {
			hint = "Esc normal / back · Ctrl-C cancel"
		}
		if frame == m.Runtime.Frames[len(m.Runtime.Frames)-1] && m.Status != "" {
			hint = "Error: " + m.Status
		}
		background = ComposeModal(background, title, body, hint, m.Width, m.Height, styles)
	}
	return background
}
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

// InputView inverts the cursor's grapheme without changing text or editing state.
func InputView(in *Input, width, height int) string {
	if in == nil {
		return "(no focused input)"
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
	write := func(text string, cells int, reversed bool) {
		lo, hi := max(column, offset), min(column+cells, offset+width)
		visible := rowIndex >= start && rowIndex < start+height
		if visible && (hi > lo || cells == 0 && column >= offset && column < offset+width) {
			if lo != column || hi != column+cells {
				text = strings.Repeat(" ", hi-lo)
			}
			if reversed {
				body.WriteString("\x1b[7m")
			}
			body.WriteString(text)
			if reversed {
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
			atCursor := onCursor && column == cursorColumn
			if onCursor && column < cursorColumn {
				next = min(next, cursorColumn)
			} else if atCursor {
				next = min(next, column+1)
			}
			write(strings.Repeat(" ", next-column), next-column, reversed != atCursor)
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
		// Reverse video composes by toggling it on the selected cursor cell.
		if selected && block && w > 0 && (column < left || column+w > right) {
			before := max(0, left-column)
			middle := min(column+w, right) - max(column, left)
			after := max(0, column+w-right)
			if before > 0 {
				write(strings.Repeat(" ", before), before, onCursor)
			}
			write(strings.Repeat(" ", middle), middle, !onCursor)
			if after > 0 {
				write(strings.Repeat(" ", after), after, onCursor)
			}
		} else {
			write(text, w, selected != onCursor)
		}
	}
	writeLineEnd(false, cursor == len(in.Text))
	return body.String()
}
