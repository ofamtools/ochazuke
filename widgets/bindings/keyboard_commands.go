package bindings

import (
	"strconv"

	"ofam.dev/ochazuke/core"
)

const (
	ViewTable    = "table"
	ViewKeyboard = "keyboard"

	CmdViewToggle   = "bindings.view.toggle"
	CmdPrefixNext   = "bindings.prefix.next"
	CmdPrefixPrev   = "bindings.prefix.prev"
	CmdPrefixSelect = "bindings.prefix.select"
	CmdPrefixClear  = "bindings.prefix.clear"
	CmdShiftToggle  = "bindings.shift.toggle"
	CmdKeySelect    = "bindings.key.select"
)

func (m *Model) refreshKeyboard() {
	entries := make([]core.Binding, 0, len(m.list.Rows))
	for _, row := range m.list.Rows {
		i, _ := strconv.Atoi(row.ID)
		entries = append(entries, m.current[i])
	}
	m.keyboard = newKeyboardIndex(entries)
	if m.keyboard.layerIndex(m.activePrefix) < 0 {
		m.activePrefix, m.selectedKey = "", ""
	}
}

func (m *Model) toggleView() {
	if m.ViewMode == ViewKeyboard {
		m.ViewMode = ViewTable
	} else {
		m.ViewMode = ViewKeyboard
	}
}

func (m *Model) stepPrefix(delta int) bool {
	i := m.keyboard.layerIndex(m.activePrefix) + delta
	if i < 0 || i >= len(m.keyboard.layers) {
		return false
	}
	return m.selectPrefix(m.keyboard.layers[i].Prefix)
}

func (m *Model) selectPrefix(prefix string) bool {
	if m.keyboard.layerIndex(prefix) < 0 {
		return false
	}
	if m.activePrefix != prefix {
		m.activePrefix, m.selectedKey = prefix, ""
	}
	return true
}

func (m *Model) selectKey(key string) bool {
	cap, ok := m.keyboard.project(m.activePrefix, m.shifted).keycap(key)
	if ok {
		m.selectedKey = cap.PhysicalKey
	}
	return ok
}
