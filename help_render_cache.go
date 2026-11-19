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
	grouped             bool
	built               bool
	cells               []HintCell
	line                string
}

func (c *runtimeRenderCache) hintCells(r *Runtime, focus string) []HintCell {
	_, version := c.hintBindingsShared(r, focus)
	old := &c.cells
	display, grouped := r.nounsDisplay(), r.hintsGrouped()
	if !old.built || old.generation != c.generation || old.version != version || old.display != display || old.grouped != grouped {
		bindings := c.hints.bindings
		*old = hintCellsMemo{generation: c.generation, version: version, display: display, grouped: grouped, built: true}
		for _, b := range bindings {
			cell := rowHintCell(r, b)
			if grouped {
				cell = groupHintCell(r, b, cell)
			}
			old.cells = append(old.cells, cell)
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

// helpStoredMemo is every table's stored rows and their help page rows
// before a walk places them (HelpRows' rows off the walk), read once per
// change of the tables rather than on every ?: thousands of rows, whose
// reading, sort and guard text were most of opening the page. Valid while
// the binding generation (every table, the props and stores, the
// settings), the operation registry, the alias names and the set of
// instances that hold rows of their own are unchanged.
type helpStoredMemo struct {
	built                         bool
	generation, operations, names uint64
	bound                         []string
	rows                          []Binding
	page                          []HelpPageRow
}

// storedHelpRows is storedRows and each row's helpPageRow (Order 0),
// memoized; callers copy a page row before changing it.
func (c *runtimeRenderCache) storedHelpRows(r *Runtime) ([]Binding, []HelpPageRow) {
	var bound []string
	for id, inst := range r.Instances {
		if len(inst.Bindings) > 0 {
			bound = append(bound, id)
		}
	}
	slices.Sort(bound)
	m := &c.stored
	ops := uint64(r.Operations.Invalidations)
	if m.built && m.generation == c.generation && m.operations == ops && m.names == c.names && slices.Equal(m.bound, bound) {
		return m.rows, m.page
	}
	rows, user := r.storedRows(), r.userRowKeys()
	page := make([]HelpPageRow, len(rows))
	for i, b := range rows {
		page[i] = r.helpPageRow(b, user, 0)
	}
	*m = helpStoredMemo{built: true, generation: c.generation, operations: ops, names: c.names, bound: bound, rows: rows, page: page}
	return rows, page
}

// helpFrameMemo is BindingHelp's answers in the frame being drawn. An
// application that names keys in its own text (a sentence of "KEY does"
// per action) asks for one focus's help once per action, several times a
// frame; nothing a help row reads changes while a frame is drawn, so each
// focus and mode is read once per frame. Between frames (drawing 0: an
// update, a CLI or MCP query) every call reads afresh.
type helpFrameMemo struct {
	frame uint64
	rows  map[helpFrameKey][]HelpRow
}

type helpFrameKey struct {
	focus string
	mode  ModeState
}

func (c *runtimeRenderCache) bindingHelp(r *Runtime, focus string) []HelpRow {
	if c.drawing == 0 {
		return r.bindingHelpIn(r.liveResolution(focus))
	}
	if c.helpFrame.frame != c.drawing {
		c.helpFrame = helpFrameMemo{frame: c.drawing, rows: map[helpFrameKey][]HelpRow{}}
	}
	k := helpFrameKey{focus, r.Mode}
	rows, ok := c.helpFrame.rows[k]
	if !ok {
		rows = r.bindingHelpIn(r.liveResolution(focus))
		c.helpFrame.rows[k] = rows
	}
	out := make([]HelpRow, len(rows))
	for i, row := range rows {
		out[i] = cloneHelpRow(row)
	}
	return out
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
	row.Steps = slices.Clone(row.Steps)
	return row
}
