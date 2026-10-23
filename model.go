package ochazuke

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	ModalViews map[string]ModalView
	Title      string
	Width      int
	Height     int
	Status     string
	views      map[string]*viewport.Model
}

func NewModel(r *Runtime) *Model {
	m := &Model{Runtime: r, Palette: NewPalette(r), Appearance: NewAppearance(r), ModalViews: map[string]ModalView{}, Title: "Ochazuke", Width: 80, Height: 24, views: map[string]*viewport.Model{}}
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
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = max(1, v.Width)
		m.Height = max(1, v.Height)
	case CallMsg:
		m.Runtime.HandleCall(v)
		m.Palette.publish()
	case tea.KeyMsg:
		if v.Type == tea.KeyRunes && !v.Paste && !v.Alt && len(v.Runes) > 1 {
			for i := range v.Runes {
				single := v
				single.Runes = v.Runes[i : i+1]
				if _, cmd := m.Update(single); cmd != nil {
					return m, cmd
				}
			}
			return m, nil
		}
		var err error
		if m.Palette.IsActive() {
			_, err = m.Palette.HandleKey(v.String())
		} else {
			var handled bool
			handled, err = m.Runtime.HandleBinding(v.String())
			if !handled && err == nil {
				c := m.Runtime.Context()
				in := m.Runtime.Inputs[c.InputID]
				if v.String() == "esc" && len(m.Runtime.Frames) > 1 && m.Runtime.Mode(c) == "normal" && (in == nil || (in.Pending == "" && in.Count == 0)) {
					_, err = m.Runtime.Execute(Request{Verb: "pop", Noun: "modal"})
				} else {
					_, err = HandleInputKey(m.Runtime, v.String())
				}
			}
		}
		if err != nil {
			m.Status = err.Error()
		} else {
			m.Status = ""
		}
	}
	if m.Runtime.Quit {
		return m, tea.Quit
	}
	return m, nil
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

// InputView shows a rune cursor and cursor-following viewport without changing editing state.
func InputView(in *Input, width, height int) string {
	if in == nil {
		return "(no focused input)"
	}
	width = max(1, width)
	height = max(1, height)
	cursor := min(max(0, in.Cursor), len(in.Text))
	before := string(in.Text[:cursor])
	after := string(in.Text[cursor:])
	lines := strings.Split(before+"▏"+after, "\n")
	row := strings.Count(before, "\n")
	start := max(0, row-height+1)
	end := min(len(lines), start+height)
	visible := lines[start:end]
	columnText := before[strings.LastIndex(before, "\n")+1:]
	offset := max(0, ansi.StringWidth(safeText(columnText))-width+2)
	for i, line := range visible {
		visible[i] = ansi.Cut(safeText(line), offset, offset+width)
	}
	return strings.Join(visible, "\n")
}
