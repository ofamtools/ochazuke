package ochazuke

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Model adapts the runtime to Bubble Tea; Render supplies the application's page content.
type Model struct {
	Runtime *Runtime
	Palette *Palette
	Render  func(*Runtime) string
	Width   int
	Height  int
	Status  string
}

func NewModel(r *Runtime, render func(*Runtime) string) *Model {
	return &Model{Runtime: r, Palette: NewPalette(r), Render: render, Width: 80, Height: 24}
}
func (m *Model) Init() tea.Cmd { return nil }
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = v.Width
		m.Height = v.Height
	case CallMsg:
		m.Runtime.HandleCall(v)
		m.Palette.publish()
	case tea.KeyMsg:
		key := v.String()
		var err error
		if m.Palette.IsActive() {
			_, err = m.Palette.HandleKey(key)
		} else {
			var handled bool
			handled, err = m.Runtime.HandleBinding(key)
			if !handled && err == nil {
				_, err = HandleInputKey(m.Runtime, key)
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
func (m *Model) View() string {
	body := ""
	if m.Palette.IsActive() {
		body = m.Palette.ViewSized(m.Width, max(1, m.Height-4))
	} else if m.Render != nil {
		body = m.Render(m.Runtime)
	}
	heading := fmt.Sprintf("OCHAZUKE  %s  %s", strings.ToUpper(m.Runtime.Mode(m.Runtime.Context())), strings.Join(m.Runtime.Context().Path, " > "))
	footer := "Ctrl-Space palette · Tab next field · Normal: Ctrl-O directory · Ctrl-T file"
	if m.Palette.IsActive() {
		footer = "Palette: Esc normal/back · Ctrl-C cancel"
	} else if m.Runtime.Mode(m.Runtime.Context()) == "normal" {
		footer = "Ctrl-P palette · Ctrl-N modal · ? help · Ctrl-C quit"
	}
	if m.Runtime.Pending != "" {
		footer = "Pending: " + m.Runtime.Pending + " · Esc cancel"
	}
	if m.Status != "" {
		footer = "Error: " + m.Status
	}
	// Sanitize application text before printing it; render styles are deliberately plain.
	lines := strings.Split(safeText(body), "\n")
	available := max(1, m.Height-4)
	if len(lines) > available {
		lines = append(lines[:available-1], "…")
	}
	out := append([]string{safeText(heading), ""}, lines...)
	out = append(out, "", safeText(footer))
	for i, line := range out {
		out[i] = ansi.Truncate(line, max(1, m.Width), "…")
	}
	return strings.Join(out, "\n")
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

// InputView shows a rune cursor and a cursor-following viewport without altering editing state.
func InputView(in *Input, width, height int) string {
	if in == nil {
		return "(no focused input)"
	}
	width = max(8, width)
	height = max(1, height)
	cursor := min(max(0, in.Cursor), len(in.Text))
	before := string(in.Text[:cursor])
	after := string(in.Text[cursor:])
	content := before + "▏" + after
	lines := strings.Split(content, "\n")
	row := strings.Count(before, "\n")
	start := max(0, row-height+1)
	end := min(len(lines), start+height)
	visible := lines[start:end]
	// Keep the active column visible in long lines, measured in terminal cells.
	columnText := before[strings.LastIndex(before, "\n")+1:]
	offset := max(0, ansi.StringWidth(safeText(columnText))-width+3)
	for i, line := range visible {
		visible[i] = ansi.Cut(safeText(line), offset, offset+width)
	}
	return strings.Join(visible, "\n")
}
