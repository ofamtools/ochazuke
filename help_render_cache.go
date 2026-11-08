package ochazuke

import (
	"reflect"
	"slices"
)

// hintMemo is the hint bindings of one scan entry. It is valid while that
// entry answers (a hit on its stamp), the hidden setting is unchanged and the
// guards of the guarded rows read as before. Version counts rebuilds, so the
// cell memo below compares one integer, not the bindings.
type hintMemo struct {
	focus      string
	hidden     bool
	stamp      RenderStamp
	scan       *bindingScan
	rows       []levelRow
	whens      []int
	whenValues []bool
	bindings   []Binding
	Version    uint64
}

type hintCellsMemo struct {
	generation, version uint64
	display             string
	built               bool
	cells               []HintCell
	line                string
}

func (c *runtimeRenderCache) hintCells(r *Runtime, focus string) []HintCell {
	_, version := c.hintBindingsShared(r, focus)
	old := &c.cells
	display := r.nounsDisplay()
	if !old.built || old.generation != c.generation || old.version != version || old.display != display {
		bindings := c.hints.bindings
		*old = hintCellsMemo{generation: c.generation, version: version, display: display, built: true}
		for _, b := range bindings {
			old.cells = append(old.cells, rowHintCell(r, b))
		}
		old.line = JoinHints(old.cells)
	}
	return slices.Clone(old.cells)
}

func (c *runtimeRenderCache) hintBindings(r *Runtime, focus string) []Binding {
	bindings, _ := c.hintBindingsShared(r, focus)
	return slices.Clone(bindings)
}

// hintBindingsShared is the memoized hint bindings, borrowed (callers copy
// them) with the memo's version. On a hit it compares the scan entry, the
// stamp and the hidden setting, and re-reads only the guards of guarded rows.
func (c *runtimeRenderCache) hintBindingsShared(r *Runtime, focus string) ([]Binding, uint64) {
	rows := r.groupedRows(focus)
	scan, hidden, stamp := c.lastScan, r.Settings().Bool(HintsShowHiddenSetting), c.stamp(r)
	old := &c.hints
	if scan == nil {
		old.scan = nil
		old.Version++
		old.bindings = hintBindingsUncached(r, focus, rows)
		return old.bindings, old.Version
	}
	valid := old.scan == scan && old.focus == focus && old.hidden == hidden && old.stamp == stamp
	for i := 0; valid && i < len(old.whens); i++ {
		valid = old.whenValues[i] == !r.guardedOffIn(old.rows[old.whens[i]].Binding, focus, r.Mode)
	}
	if valid {
		return old.bindings, old.Version
	}
	version := old.Version + 1
	*old = hintMemo{focus: focus, hidden: hidden, stamp: stamp, scan: scan, rows: rows, Version: version}
	for i, row := range rows {
		if row.When != "" {
			old.whens = append(old.whens, i)
			old.whenValues = append(old.whenValues, !r.guardedOffIn(row.Binding, focus, r.Mode))
		}
	}
	old.bindings = hintBindingsUncached(r, focus, rows)
	return old.bindings, version
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
	k := helpRowKey{c.generation, row.Mode, row.Noun, row.Instance, row.Keys, row.Pattern, row.When, row.Hint, row.Help, row.level, row.guard, row.Request.Key(), r.Operations.At(Key(row.Request.Key())).Description, r.nounsDisplay()}
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
