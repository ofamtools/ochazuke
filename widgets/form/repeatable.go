package form

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"ofam.dev/ochazuke/core"
)

// AddLabelFor returns the action label for a repeatable field's "+ Add" row.
func AddLabelFor(s core.ParamSpec) string {
	if lbl := strings.TrimSpace(s.AddLabel); lbl != "" {
		return lbl
	}
	name := strings.TrimSpace(s.Name)
	if len(name) > 2 && strings.HasSuffix(name, "s") {
		return name[:len(name)-1]
	}
	if name != "" {
		return name
	}
	return "item"
}

func normalizeRepeatable(s core.ParamSpec, v any) []string {
	switch t := v.(type) {
	case nil:
		return []string{}
	case []string:
		cp := make([]string, len(t))
		copy(cp, t)
		return cp
	case []any:
		out := make([]string, len(t))
		for i, e := range t {
			out[i] = displayScalar(s, e)
		}
		return out
	case string:
		if strings.TrimSpace(t) == "" {
			return []string{}
		}
		out := make([]string, 0)
		for _, p := range strings.Split(t, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	default:
		str := displayScalar(s, v)
		if strings.TrimSpace(str) == "" {
			return []string{}
		}
		return []string{str}
	}
}

func (m *Model) repeatableItems(name string) []string {
	if xs, ok := m.Values[name].([]string); ok {
		return xs
	}
	if raw, ok := m.Values[name]; ok {
		for _, s := range m.Specs {
			if s.Name == name {
				return normalizeRepeatable(s, raw)
			}
		}
	}
	return nil
}

func (m *Model) rowText(s core.ParamSpec, row navRow) string {
	if row.Kind == rowItem {
		items := m.repeatableItems(s.Name)
		if row.ItemIdx >= 0 && row.ItemIdx < len(items) {
			return items[row.ItemIdx]
		}
		return ""
	}
	return m.text(s.Name)
}

func (m *Model) setActiveLineValue(s core.ParamSpec, row navRow, val string) {
	if row.Kind == rowItem {
		items := m.repeatableItems(s.Name)
		if row.ItemIdx < 0 || row.ItemIdx >= len(items) {
			return
		}
		cp := make([]string, len(items))
		copy(cp, items)
		cp[row.ItemIdx] = val
		m.Values[s.Name] = cp
	} else {
		m.Values[s.Name] = val
	}
	m.touchWithAnchor(s.Name, row, true)
}

func (m *Model) setRepeatableValue(specIdx int, s core.ParamSpec, v any, anchor navRow, hasAnchor bool) {
	switch t := v.(type) {
	case nil:
		m.Values[s.Name] = []string{}
	case []string:
		cp := make([]string, len(t))
		copy(cp, t)
		m.Values[s.Name] = cp
	case []any:
		m.Values[s.Name] = normalizeRepeatable(s, t)
	default:
		str := displayScalar(s, v)
		items := m.repeatableItems(s.Name)
		cp := make([]string, len(items))
		copy(cp, items)
		if hasAnchor && anchor.Kind == rowItem && anchor.SpecIdx == specIdx && anchor.ItemIdx >= 0 && anchor.ItemIdx < len(cp) {
			cp[anchor.ItemIdx] = str
			if m.lineField == s.Name && m.lineItem == anchor.ItemIdx {
				m.line.Set(str)
			}
		} else {
			cp = append(cp, str)
		}
		m.Values[s.Name] = cp
	}
}

// AddItem appends an empty item to the focused repeatable field, focuses the new
// item row, enters editing when editable, resets the line editor, and marks dirty.
func (m *Model) AddItem() bool {
	row, ok := m.activeRow()
	if !ok || (row.Kind != rowItem && row.Kind != rowAdd) {
		return false
	}
	s := m.spec(row.SpecIdx)
	if !s.Repeatable {
		return false
	}
	m.finishEditing()
	items := m.repeatableItems(s.Name)
	next := make([]string, len(items)+1)
	copy(next, items)
	newIdx := len(items)
	next[newIdx] = ""
	m.Values[s.Name] = next

	target := navRow{Kind: rowItem, SpecIdx: row.SpecIdx, ItemIdx: newIdx}
	m.touchWithAnchor(s.Name, target, true)
	m.lineField = ""
	m.lineItem = -1
	m.line.Set("")
	m.Editing = m.Editable()
	if m.Editing {
		m.syncLine()
	}
	return true
}

// RemoveItem removes the focused item from a repeatable field, clamps focus,
// exits editing, and marks dirty.
func (m *Model) RemoveItem() bool {
	row, ok := m.activeRow()
	if !ok || row.Kind != rowItem {
		return false
	}
	s := m.spec(row.SpecIdx)
	if !s.Repeatable {
		return false
	}
	items := m.repeatableItems(s.Name)
	if row.ItemIdx < 0 || row.ItemIdx >= len(items) {
		return false
	}
	next := make([]string, 0, len(items)-1)
	next = append(next, items[:row.ItemIdx]...)
	next = append(next, items[row.ItemIdx+1:]...)
	m.Values[s.Name] = next

	var target navRow
	if len(next) == 0 {
		target = navRow{Kind: rowAdd, SpecIdx: row.SpecIdx, ItemIdx: -1}
	} else {
		target = navRow{Kind: rowItem, SpecIdx: row.SpecIdx, ItemIdx: min(row.ItemIdx, len(next)-1)}
	}
	m.Editing = false
	m.lineField = ""
	m.lineItem = -1
	m.touchWithAnchor(s.Name, target, true)
	return true
}

func (m *Model) typedRepeatable(s core.ParamSpec) (any, error) {
	raw := m.repeatableItems(s.Name)
	out := make([]string, 0, len(raw))
	k := KindFor(s)
	for _, item := range raw {
		txt := strings.TrimSpace(item)
		if txt == "" {
			continue
		}
		if err := validateRepeatableItem(s, k, txt); err != nil {
			return nil, err
		}
		out = append(out, txt)
	}
	if len(out) == 0 {
		if s.Required {
			return nil, fmt.Errorf("required")
		}
		return nil, nil
	}
	return out, nil
}

func validateRepeatableItem(s core.ParamSpec, k FieldKind, txt string) error {
	if k == KindEnum {
		if !slices.Contains(s.Enum, txt) {
			return fmt.Errorf("must be one of %s", strings.Join(s.Enum, ", "))
		}
		return nil
	}
	switch k {
	case KindBool:
		if txt != "true" && txt != "false" {
			return fmt.Errorf("must be true or false")
		}
	case KindNumber:
		if s.Type == core.ParamInt {
			if _, err := strconv.Atoi(txt); err != nil {
				return fmt.Errorf("must be an integer")
			}
			return nil
		}
		if _, err := strconv.ParseFloat(txt, 64); err != nil {
			return fmt.Errorf("must be a number")
		}
	case KindDuration:
		if _, err := time.ParseDuration(txt); err != nil {
			return fmt.Errorf("must be a duration like 5m or 1h30m")
		}
	case KindJSON:
		var v any
		if err := json.Unmarshal([]byte(txt), &v); err != nil {
			return fmt.Errorf("invalid JSON")
		}
	}
	return nil
}
