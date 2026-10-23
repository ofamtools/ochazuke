package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"ochazuke"
)

const keyHistorySize = 200

type keybindingModel struct {
	width, height int
	history       [keyHistorySize]string
	results       [keyHistorySize]string
	runtime       *ochazuke.Runtime
	count         int
}

func (m *keybindingModel) Init() tea.Cmd { return nil }

func (m *keybindingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if msg.Type == tea.KeyRunes && !msg.Paste && !msg.Alt && len(msg.Runes) > 1 {
			for i := range msg.Runes {
				key := msg
				key.Runes = msg.Runes[i : i+1]
				m.Update(key)
			}
			return m, nil
		}
		m.history[m.count%keyHistorySize] = msg.String()
		m.count++
		index := (m.count - 1) % keyHistorySize
		m.results[index] = "unmatched"
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if msg.Paste {
			m.results[index] = "paste (not dispatched)"
			return m, nil
		}
		if m.runtime != nil {
			handled, err := m.runtime.HandleBinding(msg.String())
			switch {
			case err != nil:
				m.results[index] = "error: " + err.Error()
			case m.runtime.Pending != "":
				m.results[index] = "pending: " + m.runtime.Pending
			case handled && m.results[index] == "unmatched":
				m.results[index] = "sequence cancelled"
			}
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
	if m.count > 0 {
		left = append(left, "", m.results[(m.count-1)%keyHistorySize])
	}
	if m.runtime != nil {
		left = append(left, "", "DEFINED BINDINGS")
		for _, binding := range m.runtime.EffectiveBindings(m.runtime.Context()) {
			left = append(left, binding.Keys+" → "+binding.Request.Verb)
		}
	}
	right := []string{"HISTORY · newest first"}
	for i := range min(m.count, keyHistorySize, rows-1) {
		n := m.count - 1 - i
		right = append(right, fmt.Sprintf("%d  %s · %s", n+1, strconv.Quote(m.history[n%keyHistorySize]), m.results[n%keyHistorySize]))
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
	m := &keybindingModel{width: 80, height: 24, runtime: ochazuke.NewRuntime()}
	flags := flag.NewFlagSet("keybinding", flag.ContinueOnError)
	flags.Func("bind", "Dummy binding KEYS=ACTION; repeat for multiple bindings (e.g. 'g g=go-top')", m.addBinding)
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: ochazuke keybinding [--bind 'KEYS=ACTION']...")
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *keybindingModel) addBinding(spec string) error {
	keys, action, ok := strings.Cut(spec, "=")
	keys, action = strings.TrimSpace(keys), strings.TrimSpace(action)
	if !ok || keys == "" || action == "" {
		return fmt.Errorf("binding must be KEYS=ACTION with non-empty keys and action")
	}
	for _, key := range strings.Fields(keys) {
		if key == "ctrl+c" {
			return fmt.Errorf("ctrl+c is reserved for exiting the sandbox")
		}
	}
	m.runtime.Register(ochazuke.Operation{
		Verb: action, Noun: "sandbox",
		Run: func(_ *ochazuke.Runtime, _ ochazuke.Context, _ ochazuke.Arguments) (any, error) {
			m.results[(m.count-1)%keyHistorySize] = "ACTION: " + action
			return action, nil
		},
	})
	layer := m.runtime.Layers["app"]
	binding := ochazuke.Binding{Keys: keys, Request: ochazuke.Request{Verb: action, Noun: "sandbox"}}
	for i, existing := range layer.Bindings {
		if existing.Keys == keys {
			layer.Bindings[i] = binding
			return nil
		}
	}
	layer.Bindings = append(layer.Bindings, binding)
	return nil
}
