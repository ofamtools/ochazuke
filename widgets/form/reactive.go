package form

import (
	"strconv"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/expr"
)

type navRowKind int

const (
	rowField navRowKind = iota
	rowItem
	rowAdd
	rowSubmit
)

type navRow struct {
	Kind    navRowKind
	SpecIdx int
	ItemIdx int
}

// exprEnv builds the predicate evaluation environment for VisibleWhen rules.
// Every field is exposed both under values.<field> and at the top level <field>.
func (m *Model) exprEnv() map[string]any {
	vals := make(map[string]any, len(m.Specs))
	env := make(map[string]any, len(m.Specs)+1)
	seen := make(map[string]bool, len(m.Specs))

	for _, s := range m.Specs {
		seen[s.Name] = true
		raw, exists := m.Values[s.Name]
		if !exists && s.Default != nil {
			raw = display(s, s.Default)
		}
		v := normalizeEnvValue(s, raw)
		vals[s.Name] = v
		env[s.Name] = v
	}
	for k, raw := range m.Values {
		if seen[k] {
			continue
		}
		v := cloneValue(raw)
		vals[k] = v
		env[k] = v
	}
	env["values"] = vals
	return env
}

func normalizeEnvValue(s core.ParamSpec, raw any) any {
	if s.Repeatable {
		return normalizeRepeatable(s, raw)
	}
	if KindFor(s) == KindBool {
		switch t := raw.(type) {
		case bool:
			return t
		case string:
			return t == "true"
		}
		return false
	}
	switch s.Type {
	case core.ParamInt:
		if n, ok := raw.(int); ok {
			return n
		}
		str := displayScalar(s, raw)
		if n, err := strconv.Atoi(strings.TrimSpace(str)); err == nil {
			return n
		}
		return str
	case core.ParamFloat:
		if f, ok := raw.(float64); ok {
			return f
		}
		str := displayScalar(s, raw)
		if f, err := strconv.ParseFloat(strings.TrimSpace(str), 64); err == nil {
			return f
		}
		return str
	}
	return displayScalar(s, raw)
}

func (m *Model) compileWhen(src string) expr.Expr {
	if m.compiledWhen != nil {
		if c, ok := m.compiledWhen[src]; ok {
			return c
		}
	} else {
		m.compiledWhen = map[string]expr.Expr{}
	}
	c, err := expr.Parse(src)
	if err != nil {
		// Fail open so a malformed expression never hides fields permanently.
		c = expr.Expr{}
	}
	m.compiledWhen[src] = c
	return c
}

func (m *Model) isFieldVisibleWithEnv(idx int, env map[string]any) bool {
	if idx < 0 || idx >= len(m.Specs) {
		return false
	}
	if m.ShowAllFields {
		return true
	}
	src := strings.TrimSpace(m.Specs[idx].VisibleWhen)
	if src == "" {
		return true
	}
	if env == nil {
		env = m.exprEnv()
	}
	return m.compileWhen(src).Eval(env)
}

// IsFieldVisible reports whether m.Specs[idx] is currently visible.
func (m *Model) IsFieldVisible(idx int) bool {
	return m.isFieldVisibleWithEnv(idx, nil)
}

func (m *Model) hasVisibleWhen() bool {
	if m.ShowAllFields {
		return false
	}
	for _, s := range m.Specs {
		if strings.TrimSpace(s.VisibleWhen) != "" {
			return true
		}
	}
	return false
}

// VisibleIndices returns the indices into m.Specs of all currently visible fields.
func (m *Model) VisibleIndices() []int {
	var env map[string]any
	if m.hasVisibleWhen() {
		env = m.exprEnv()
	}
	out := make([]int, 0, len(m.Specs))
	for i := range m.Specs {
		if m.isFieldVisibleWithEnv(i, env) {
			out = append(out, i)
		}
	}
	return out
}

// VisibleFields returns the currently visible ParamSpecs with effective choices applied.
func (m *Model) VisibleFields() []core.ParamSpec {
	indices := m.VisibleIndices()
	out := make([]core.ParamSpec, len(indices))
	for i, idx := range indices {
		out[i] = m.spec(idx)
	}
	return out
}

func (m *Model) navRows() []navRow {
	indices := m.VisibleIndices()
	rows := make([]navRow, 0, len(indices)+1)
	for _, idx := range indices {
		s := m.Specs[idx]
		if !s.Repeatable {
			rows = append(rows, navRow{Kind: rowField, SpecIdx: idx, ItemIdx: -1})
			continue
		}
		items := m.repeatableItems(s.Name)
		for itemIdx := range items {
			rows = append(rows, navRow{Kind: rowItem, SpecIdx: idx, ItemIdx: itemIdx})
		}
		rows = append(rows, navRow{Kind: rowAdd, SpecIdx: idx, ItemIdx: -1})
	}
	if m.SubmitRow {
		rows = append(rows, navRow{Kind: rowSubmit, SpecIdx: -1, ItemIdx: -1})
	}
	return rows
}

func (m *Model) activeRow() (navRow, bool) {
	rows := m.navRows()
	if m.ActiveIdx < 0 || m.ActiveIdx >= len(rows) {
		return navRow{}, false
	}
	return rows[m.ActiveIdx], true
}

func (m *Model) activeEditableRow() (core.ParamSpec, navRow, bool) {
	row, ok := m.activeRow()
	if !ok || (row.Kind != rowField && row.Kind != rowItem) {
		return core.ParamSpec{}, navRow{}, false
	}
	return m.spec(row.SpecIdx), row, true
}

func (m *Model) reanchor(anchor navRow, hadAnchor bool) {
	rows := m.navRows()
	if len(rows) == 0 {
		m.ActiveIdx = 0
		m.Editing = false
		m.lineField = ""
		m.lineItem = -1
		return
	}
	if hadAnchor {
		for i, r := range rows {
			if r == anchor {
				m.ActiveIdx = i
				return
			}
		}
		// Previously focused row disappeared: clamp ActiveIdx and exit editing cleanly.
		m.ActiveIdx = max(0, min(m.ActiveIdx, len(rows)-1))
		m.Editing = false
		m.lineField = ""
		m.lineItem = -1
		return
	}
	if m.ActiveIdx < 0 || m.ActiveIdx >= len(rows) {
		m.ActiveIdx = max(0, min(m.ActiveIdx, len(rows)-1))
		m.Editing = false
		m.lineField = ""
		m.lineItem = -1
	}
}

// ActiveField returns the ParamSpec of the currently focused row (excluding SubmitRow).
func (m *Model) ActiveField() (core.ParamSpec, bool) {
	row, ok := m.activeRow()
	if !ok || row.Kind == rowSubmit || row.SpecIdx < 0 || row.SpecIdx >= len(m.Specs) {
		return core.ParamSpec{}, false
	}
	return m.spec(row.SpecIdx), true
}

// ActiveFieldName returns the focused field's name, or "" when none or on SubmitRow.
func (m *Model) ActiveFieldName() string {
	if s, ok := m.ActiveField(); ok {
		return s.Name
	}
	return ""
}

// ActiveKind returns the inferred FieldKind of the focused field, or "" on SubmitRow.
func (m *Model) ActiveKind() FieldKind {
	if s, ok := m.ActiveField(); ok {
		return KindFor(s)
	}
	return ""
}

// ActiveItemIndex returns the focused item index (0..N-1) on a repeatable item row, or -1.
func (m *Model) ActiveItemIndex() int {
	if row, ok := m.activeRow(); ok && row.Kind == rowItem {
		return row.ItemIdx
	}
	return -1
}

// OnAddRow reports whether the focusable repeatable "+ Add" row is active.
func (m *Model) OnAddRow() bool {
	row, ok := m.activeRow()
	return ok && row.Kind == rowAdd
}

// OnSubmitRow reports whether the focusable Submit button row is active.
func (m *Model) OnSubmitRow() bool {
	row, ok := m.activeRow()
	return ok && row.Kind == rowSubmit
}

// FocusField moves ActiveIdx to the first visible row of the named field and stops editing.
func (m *Model) FocusField(name string) bool {
	rows := m.navRows()
	for i, r := range rows {
		if r.SpecIdx >= 0 && r.SpecIdx < len(m.Specs) && m.Specs[r.SpecIdx].Name == name {
			m.move(i)
			return true
		}
	}
	return false
}
