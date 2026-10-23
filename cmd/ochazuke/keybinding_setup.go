package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func (m *keybindingModel) openForm(kind string, values ...string) tea.Cmd {
	m.form, m.field, m.notice = kind, 0, ""
	m.fields = make([]textinput.Model, len(values))
	for i, value := range values {
		m.fields[i] = textinput.New()
		m.fields[i].SetValue(value)
		m.fields[i].Width = max(10, m.width-16)
	}
	return m.fields[0].Focus()
}

func (m *keybindingModel) replaceBindings(bindings []sandboxBinding) {
	m.resetRuntime()
	for _, binding := range bindings {
		m.installBinding(binding)
	}
	m.selected = min(m.selected, max(0, len(bindings)-1))
}

func (m *keybindingModel) updateSetup(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if strings.HasPrefix(m.form, "browse-") {
		if isKey && key.String() == "esc" {
			m.form = strings.TrimPrefix(m.form, "browse-")
			return m, m.fields[0].Focus()
		}
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		if ok, path := m.picker.DidSelectFile(msg); ok {
			m.form = strings.TrimPrefix(m.form, "browse-")
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				path = filepath.Join(path, "bindings.json")
			}
			m.fields[0].SetValue(path)
			return m, m.fields[0].Focus()
		}
		return m, cmd
	}
	if m.form == "overwrite" {
		if isKey {
			switch key.String() {
			case "y":
				if err := m.exportBindings(m.path, true); err != nil {
					m.notice = err.Error()
				} else {
					m.notice = "Exported " + m.path
				}
				m.form = ""
			case "n", "esc":
				m.form, m.notice = "", "Export cancelled"
			}
		}
		return m, nil
	}
	if m.form != "" {
		if isKey {
			switch key.String() {
			case "esc":
				m.form = ""
				return m, nil
			case "tab", "shift+tab":
				m.fields[m.field].Blur()
				m.field = (m.field + 1) % len(m.fields)
				return m, m.fields[m.field].Focus()
			case "f4":
				if m.form == "import" || m.form == "export" {
					m.picker = filepicker.New()
					m.picker.CurrentDirectory = filepath.Dir(m.fields[0].Value())
					m.picker.FileAllowed = true
					m.picker.DirAllowed = m.form == "export"
					m.picker.SetHeight(max(3, m.height-8))
					m.picker.KeyMap.Select.SetKeys("ctrl+s")
					m.picker.KeyMap.Open.SetKeys("right", "enter", "l", "ctrl+s")
					m.form = "browse-" + m.form
					return m, m.picker.Init()
				}
			case "enter":
				if m.form == "binding" && m.field == 0 {
					m.fields[0].Blur()
					m.field = 1
					return m, m.fields[1].Focus()
				}
				m.submitForm()
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.fields[m.field], cmd = m.fields[m.field].Update(msg)
		return m, cmd
	}
	if !isKey {
		return m, nil
	}
	bindings := m.bindings()
	switch key.String() {
	case "up", "k":
		m.selected = max(0, m.selected-1)
	case "down", "j":
		m.selected = min(max(0, len(bindings)-1), m.selected+1)
	case "a":
		m.editing = -1
		return m, m.openForm("binding", "", "")
	case "e":
		if len(bindings) > 0 {
			m.editing = m.selected
			b := bindings[m.selected]
			return m, m.openForm("binding", b.Keys, b.Action)
		}
	case "d", "delete":
		if len(bindings) > 0 {
			m.replaceBindings(append(bindings[:m.selected], bindings[m.selected+1:]...))
			m.notice = "Binding removed (export to save changes)"
		}
	case "i", "o":
		kind := "import"
		if key.String() == "o" {
			kind = "export"
		}
		path := m.path
		if path == "" {
			path = "bindings.json"
		}
		return m, m.openForm(kind, path)
	case "enter":
		m.sandbox, m.notice = true, ""
		m.runtime.CancelPendingBinding()
	case "t":
		return m, m.openForm("timeout", m.runtime.BindingTimeout.String())
	}
	return m, nil
}

func (m *keybindingModel) submitForm() {
	switch m.form {
	case "binding":
		binding := sandboxBinding{Keys: strings.TrimSpace(m.fields[0].Value()), Action: strings.TrimSpace(m.fields[1].Value())}
		if err := validateSandboxBinding(binding); err != nil {
			m.notice = err.Error()
			return
		}
		bindings := m.bindings()
		if m.editing >= 0 {
			bindings = append(bindings[:m.editing], bindings[m.editing+1:]...)
		}
		m.replaceBindings(bindings)
		m.installBinding(binding)
		m.selected = len(m.bindings()) - 1
		m.notice = "Binding saved in memory (export to keep it)"
	case "timeout":
		timeout, err := parseBindingTimeout(m.fields[0].Value())
		if err != nil {
			m.notice = err.Error()
			return
		}
		m.runtime.CancelPendingBinding()
		m.runtime.BindingTimeout = timeout
		m.notice = "Timeout set to " + timeout.String() + " (not saved in JSON)"
	case "import", "export":
		path := m.fields[0].Value()
		var err error
		if m.form == "import" {
			err = m.importBindings(path)
		} else {
			err = m.exportBindings(path, false)
		}
		if errors.Is(err, os.ErrExist) && m.form == "export" {
			m.path, m.form = path, "overwrite"
			return
		}
		if err != nil {
			m.notice = err.Error()
			return
		}
		m.path = path
		m.notice = fmt.Sprintf("%s complete: %s", m.form, path)
	}
	m.form = ""
}

func (m *keybindingModel) setupView() string {
	lines := []string{"Keybinding sandbox · Stage 1: Define bindings", ""}
	switch {
	case strings.HasPrefix(m.form, "browse-"):
		lines = append(lines, "Choose a file / export directory", "↑/↓: move · ←/→: folders · Ctrl-S: select · Esc: back", m.picker.View())
	case m.form == "overwrite":
		lines = append(lines, "File already exists: "+m.path, "Overwrite? y / n")
	case m.form != "":
		if m.form == "binding" {
			lines = append(lines, "Keys (Tea names, e.g. ctrl+s or g g):", m.fields[0].View(), "Action identifier:", m.fields[1].View(), "", "Tab: change field · Enter: next / save · Esc: cancel")
		} else if m.form == "timeout" {
			lines = append(lines, "Ambiguous-sequence timeout (e.g. 400ms, 1s):", m.fields[0].View(), "", "Enter: save · Esc: cancel")
		} else {
			lines = append(lines, strings.ToUpper(m.form)+" JSON bindings", m.fields[0].View(), "", "F4: browse files / folders · Enter: confirm · Esc: cancel")
			if m.form == "import" {
				lines = append(lines, "Import replaces the in-memory bindings; export first to keep them.")
			}
		}
	default:
		lines = append(lines, "a: add · e: edit · d: delete · ↑/↓: select", "i: import · o: export · t: timeout · Enter: ENTER SANDBOX", "Ctrl-C: exit · F2 returns here from sandbox", "Timeout: "+m.runtime.BindingTimeout.String()+" (not saved in JSON)", "", "KEYS → ACTION")
		bindings := m.bindings()
		if len(bindings) == 0 {
			lines = append(lines, "No bindings yet. Press a to add one, or i to import.")
		}
		visible := max(1, m.height-12)
		start := max(0, m.selected-visible+1)
		for i := start; i < min(len(bindings), start+visible); i++ {
			marker := "  "
			if i == m.selected {
				marker = "> "
			}
			lines = append(lines, marker+bindings[i].Keys+" → "+bindings[i].Action)
		}
	}
	if m.notice != "" {
		lines = append(lines, "", m.notice)
	}
	lines = strings.Split(strings.Join(lines, "\n"), "\n")
	lines = lines[:min(len(lines), max(1, m.height))]
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], max(1, m.width), "")
	}
	return strings.Join(lines, "\n")
}
