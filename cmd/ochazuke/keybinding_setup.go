package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"ochazuke"
)

func (m *keybindingModel) openForm(kind string, values ...string) tea.Cmd {
	m.form, m.field, m.notice = kind, 0, ""
	m.fields = make([]textinput.Model, len(values))
	for i, value := range values {
		m.fields[i] = textinput.New()
		m.fields[i].SetValue(value)
	}
	m.resizeForm()
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
					m.picker.AutoHeight = false
					m.picker.KeyMap.Select.SetKeys("ctrl+s")
					m.picker.KeyMap.Open.SetKeys("right", "enter", "l", "ctrl+s")
					m.form = "browse-" + m.form
					m.resizeForm()
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

// styles and layout are render-time values derived from the viewport.
func (m *keybindingModel) styles() ochazuke.Styles {
	return ochazuke.NewStyles(ochazuke.Appearance{Theme: "adaptive", Density: "compact"})
}

func (m *keybindingModel) layout() ochazuke.Layout {
	return ochazuke.Layout{Width: max(0, m.width), Height: max(0, m.height), Styles: m.styles()}
}

// cleanLabel drops control characters from user-typed text before it enters
// a trusted-ANSI body.
func cleanLabel(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// resizeForm fits the active fields and picker to the modal, not the window.
func (m *keybindingModel) resizeForm() {
	cw, ch := ochazuke.ModalSize(m.width, m.height)
	for i := range m.fields {
		m.fields[i].Width = max(1, cw-4)
	}
	if strings.HasPrefix(m.form, "browse-") {
		m.picker.SetHeight(max(1, ch-1))
	}
}

func (m *keybindingModel) setupPage() string {
	st := m.styles()
	bindings := m.bindings()
	items := make([]string, len(bindings))
	for i, binding := range bindings {
		items[i] = binding.Keys + " → " + binding.Action
	}
	var details []string
	if len(bindings) > 0 && m.selected < len(bindings) {
		b := bindings[min(m.selected, len(bindings)-1)]
		details = append(details, st.Subtitle.Render("Keys:   "+cleanLabel(b.Keys)), "Action: "+cleanLabel(b.Action))
	}
	details = append(details,
		"Timeout: "+m.runtime.BindingTimeout.String(),
		st.Muted.Render("↑↓ select · t timeout"),
		st.Muted.Render("i import · o export"))
	if m.notice != "" {
		details = append(details, st.Accent.Render(cleanLabel(m.notice)))
	}
	footer := "a add · e edit · d delete · Enter test · ^C exit"
	return m.layout().Page("Keybinding sandbox", "Stage 1 · Define bindings",
		m.layout().Sidebar(fmt.Sprintf("Bindings (%d)", len(bindings)), items, m.selected, "Details", strings.Join(details, "\n")), footer)
}

func (m *keybindingModel) setupView() string {
	page := m.setupPage()
	if m.form == "" {
		return page
	}
	st := m.styles()
	label := func(active bool, text string) string {
		if active {
			return st.Accent.Render("▸ " + text)
		}
		return st.Muted.Render("  " + text)
	}
	var title, footer string
	var body []string
	switch {
	case strings.HasPrefix(m.form, "browse-"):
		title, footer = "Choose a file or folder", "↑↓ move · ←→ folders · ^S select · Esc back"
		body = []string{m.picker.View()}
	case m.form == "overwrite":
		title, footer = "Overwrite file?", "y overwrite · n / Esc cancel"
		body = []string{"File already exists:", cleanLabel(m.path)}
	case m.form == "binding":
		title, footer = "Add binding", "Tab field · Enter next/save · Esc cancel"
		if m.editing >= 0 {
			title = "Edit binding"
		}
		body = []string{label(m.field == 0, "Keys (e.g. ctrl+s or g g)"), m.fields[0].View(), "", label(m.field == 1, "Action identifier"), m.fields[1].View()}
	case m.form == "timeout":
		title, footer = "Sequence timeout", "Enter save · Esc cancel"
		body = []string{label(true, "Duration (e.g. 400ms, 1s)"), m.fields[0].View(), st.Muted.Render("Not saved in JSON.")}
	default:
		title, footer = strings.ToUpper(m.form[:1])+m.form[1:]+" bindings", "F4 browse · Enter confirm · Esc cancel"
		body = []string{label(true, "JSON file path"), m.fields[0].View()}
		if m.form == "import" {
			body = append(body, "", st.Muted.Render("Replaces in-memory bindings."))
		}
	}
	if m.notice != "" && m.form != "overwrite" {
		body = append(body, "", st.Error.Render(cleanLabel(m.notice)))
	}
	return ochazuke.ComposeModal(page, title, strings.Join(body, "\n"), footer, m.width, m.height, st)
}
