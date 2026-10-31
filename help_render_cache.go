package ochazuke

import (
	"encoding/json"
	"reflect"
	"slices"
)

type hintMemo struct {
	focus, mode     string
	generation      uint64
	hidden          bool
	rows            []levelRow
	motions, guards []bool
	bindings        []Binding
}

type hintCellsMemo struct {
	generation uint64
	display    string
	bindings   []Binding
	cells      []HintCell
	line       string
}

func (c *runtimeRenderCache) hintCells(r *Runtime, focus string) []HintCell {
	bindings := HintBindings(r, focus)
	old := &c.cells
	display := r.nounsDisplay()
	valid := old.generation == c.generation && old.display == display && len(old.bindings) == len(bindings)
	for i, b := range bindings {
		if !valid || !equalRenderBinding(old.bindings[i], b) {
			valid = false
			break
		}
	}
	if !valid {
		*old = hintCellsMemo{generation: c.generation, display: display, bindings: cloneBindings(bindings)}
		for _, b := range bindings {
			old.cells = append(old.cells, rowHintCell(r, b))
		}
		old.line = JoinHints(old.cells)
	}
	return slices.Clone(old.cells)
}

func (c *runtimeRenderCache) hintBindings(r *Runtime, focus string) []Binding {
	rows := r.groupedRows(focus)
	data, err := json.Marshal(r.Mode)
	if err != nil {
		return hintBindingsUncached(r, focus, rows)
	}
	mode, hidden := string(data), r.Settings().Bool(HintsShowHiddenSetting)
	old := &c.hints
	valid := old.focus == focus && old.mode == mode && old.generation == c.generation && old.hidden == hidden && len(old.rows) == len(rows)
	for i, row := range rows {
		if !valid {
			break
		}
		if old.rows[i].level != row.level || !equalRenderBinding(old.rows[i].Binding, row.Binding) || old.motions[i] != r.Operations[row.Request.Key()].Motion || old.guards[i] != !r.guardedOffIn(row.Binding, focus, r.Mode) {
			valid = false
		}
	}
	if valid {
		return slices.Clone(old.bindings)
	}
	*old = hintMemo{focus: focus, mode: mode, generation: c.generation, hidden: hidden, rows: slices.Clone(rows)}
	bindings := make([]Binding, len(rows))
	for i, row := range rows {
		bindings[i] = row.Binding
		old.motions = append(old.motions, r.Operations[row.Request.Key()].Motion)
		old.guards = append(old.guards, !r.guardedOffIn(row.Binding, focus, r.Mode))
	}
	for i, binding := range cloneBindings(bindings) {
		old.rows[i].Binding = binding
	}
	old.bindings = hintBindingsUncached(r, focus, rows)
	return slices.Clone(old.bindings)
}

// Cache the static HelpRow formatting independently of its tree placement.
// Resolved arguments and operation descriptions stay live; aliases and noun
// spellings use the registry generation and nouns.display setting. The row's
// steps, prefix grouping and guard filtering are still built from live rows.
type helpRowKey struct {
	generation                                                                                        uint64
	mode, noun, instance, keys, pattern, when, hint, help, level, guard, action, description, display string
}

func (c *runtimeRenderCache) helpRow(r *Runtime, row levelRow, args Arguments) HelpRow {
	if row.Request.IsProp() {
		return r.helpRowUncached(row, args)
	}
	k := helpRowKey{c.generation, row.Mode, row.Noun, row.Instance, row.Keys, row.Pattern, row.When, row.Hint, row.Help, row.level, row.guard, row.Request.Key(), r.Operations[row.Request.Key()].Description, r.nounsDisplay()}
	if old, ok := c.help[k]; ok && reflect.DeepEqual(old.Args, args) {
		return cloneHelpRow(old)
	}
	out := r.helpRowUncached(row, args)
	if c.help == nil {
		c.help = map[helpRowKey]HelpRow{}
	}
	if len(c.help) >= 512 {
		clear(c.help)
	}
	c.help[k] = cloneHelpRow(out)
	return out
}

func cloneHelpRow(row HelpRow) HelpRow {
	if row.Args != nil {
		row.Args = cloneRenderValue(row.Args).(Arguments)
	}
	row.Aliases = slices.Clone(row.Aliases)
	return row
}
