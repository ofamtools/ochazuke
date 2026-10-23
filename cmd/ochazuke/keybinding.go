package main

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const keyHistorySize = 200

type keybindingModel struct {
	width, height int
	history       [keyHistorySize]string
	count         int
}

func (m *keybindingModel) Init() tea.Cmd { return nil }

func (m *keybindingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		m.history[m.count%keyHistorySize] = msg.String()
		m.count++
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *keybindingModel) View() string {
	width, height := max(1, m.width), max(1, m.height)
	leftWidth := max(1, (width-3)/2)
	rightWidth := max(1, width-leftWidth-3)
	rows := max(0, height-4)
	latest := "Waiting for a key…"
	if m.count > 0 {
		latest = strconv.Quote(m.history[(m.count-1)%keyHistorySize])
	}
	left := []string{"LATEST KEY", latest, "", "Names: tea.KeyMsg.String()", "Quoted to expose whitespace."}
	right := []string{"HISTORY · newest first"}
	for i := range min(m.count, keyHistorySize, rows-1) {
		n := m.count - 1 - i
		right = append(right, fmt.Sprintf("%d  %s", n+1, strconv.Quote(m.history[n%keyHistorySize])))
	}
	column := func(lines []string, w int) string {
		visible := lines[:min(len(lines), rows)]
		clipped := make([]string, len(visible))
		for i, line := range visible {
			clipped[i] = ansi.Truncate(line, w, "…")
		}
		return lipgloss.NewStyle().Width(w).Height(rows).Render(strings.Join(clipped, "\n"))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, column(left, leftWidth), " │ ", column(right, rightWidth))
	view := "Keybinding sandbox\n\n" + body + "\nCtrl-C exits · Last 200 key messages retained"
	lines := strings.Split(view, "\n")
	lines = lines[:min(len(lines), height)]
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}

func runKeybinding(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: ochazuke keybinding")
	}
	_, err := tea.NewProgram(&keybindingModel{width: 80, height: 24}, tea.WithAltScreen()).Run()
	return err
}
