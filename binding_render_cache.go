package ochazuke

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
)

type runtimeRenderCache struct {
	generation uint64
	instances  instanceIndex
	scans      []*bindingScan
	lastScan   *bindingScan
	collecting *bindingScan
	hints      hintMemo
	cells      hintCellsMemo
	help       map[helpRowKey]HelpRow
}

type guardReading struct {
	name           string
	event          KeyEvent
	present, value bool
}

type scanTable struct {
	rows         []Binding
	guards       map[string]string
	availability map[string][]string
}

type bindingScan struct {
	focus, mode    string
	editing, drop  bool
	modeGeneration uint64
	levels         []level
	tables         []scanTable
	guards         []guardReading
	rows           []levelRow
	grouped        []levelRow
}

// A small bounded cache holds only the current focus and pending-mode
// walks. Snapshots detect table edits even through public binding maps;
// arbitrary application guards are always re-evaluated with their original
// event. A false guard that hid a row is a dependency too.
func (c *runtimeRenderCache) scan(r *Runtime, levels []level, mode ModeState, focus string, editing bool) []levelRow {
	c.lastScan = nil
	if c.collecting != nil {
		return r.scanRowsUncached(levels, mode, focus, editing)
	}
	data, err := json.Marshal(mode)
	if err != nil {
		return r.scanRowsUncached(levels, mode, focus, editing)
	}
	key := string(data)
	base, _ := baseAndPrefix(mode)
	exclusive := r.pendingModes[mode.Name] || mode.Payload.Shadow != nil
	var tables []scanTable
	for _, lv := range levels {
		if lv.inst != nil {
			tables = append(tables, scanTable{rows: rowsForMode(lv.inst.Bindings, base, exclusive)})
		}
		if n := lv.noun; n != nil {
			for _, rows := range r.nounTables(n, base) {
				tables = append(tables, scanTable{rows, n.Guards, n.Availability})
			}
			if !exclusive {
				tables = append(tables, scanTable{n.Bindings[""], n.Guards, n.Availability})
			}
		}
	}
	// Standard and prop requests additionally depend on a store schema's
	// mutable write plan; leave those walks uncached rather than guessing.
	var operationNouns []*Noun
	for _, table := range tables {
		for _, b := range table.rows {
			if b.Request.IsProp() || r.Operations[b.Request.Key()].Standard != "" {
				return r.scanRowsUncached(levels, mode, focus, editing)
			}
			if op, ok := r.Operations[b.Request.Key()]; ok {
				if n := r.Nouns[op.Noun]; n != nil && !slices.Contains(operationNouns, n) {
					operationNouns = append(operationNouns, n)
				}
			}
		}
	}
	for _, n := range operationNouns {
		tables = append(tables, scanTable{guards: n.Guards, availability: n.Availability})
	}
	drop := r.dropsInheritedSequences()
	for _, old := range c.scans {
		if old.focus != focus || old.mode != key || old.editing != editing || old.drop != drop || old.modeGeneration != r.modeGeneration || !slices.Equal(old.levels, levels) || !equalScanTables(old.tables, tables) {
			continue
		}
		valid := true
		for _, dep := range old.guards {
			fn := r.rawGuard(dep.name)
			if (fn != nil) != dep.present || fn != nil && fn(r, dep.event) != dep.value {
				valid = false
				break
			}
		}
		if valid {
			c.lastScan = old
			return old.rows // internal borrowed rows; public APIs copy them
		}
	}
	entry := &bindingScan{focus: focus, mode: key, editing: editing, drop: drop, modeGeneration: r.modeGeneration, levels: slices.Clone(levels)}
	entry.tables = make([]scanTable, len(tables))
	for i, table := range tables {
		entry.tables[i] = scanTable{cloneBindings(table.rows), maps.Clone(table.guards), make(map[string][]string, len(table.availability))}
		for verb, guards := range table.availability {
			entry.tables[i].availability[verb] = slices.Clone(guards)
		}
	}
	c.collecting = entry
	func() {
		defer func() { c.collecting = nil }()
		entry.rows = r.scanRowsUncached(levels, mode, focus, editing)
	}()
	if len(c.scans) >= 8 {
		c.scans = c.scans[:0]
	}
	c.scans = append(c.scans, entry)
	c.lastScan = entry
	return entry.rows
}

func (c *runtimeRenderCache) grouped(r *Runtime, focus string) []levelRow {
	rows := r.levelRows(focus)
	if entry := c.lastScan; entry != nil {
		if entry.grouped == nil {
			entry.grouped = groupLevelRows(rows)
		}
		return entry.grouped
	}
	return groupLevelRows(rows)
}

func cloneBindings(rows []Binding) []Binding {
	out := slices.Clone(rows)
	for i := range out {
		if rows[i].Request.Args != nil {
			out[i].Request.Args = cloneRenderValue(rows[i].Request.Args).(Arguments)
		}
		out[i].From = maps.Clone(rows[i].From)
	}
	return out
}

func cloneRenderValue(value any) any {
	if value == nil {
		return nil
	}
	return cloneRenderReflect(reflect.ValueOf(value)).Interface()
}

func cloneRenderReflect(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(cloneRenderReflect(v.Elem()))
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for iter := v.MapRange(); iter.Next(); {
			out.SetMapIndex(iter.Key(), cloneRenderReflect(iter.Value()))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(cloneRenderReflect(v.Index(i)))
		}
		return out
	default:
		return v
	}
}

func equalScanTables(a, b []scanTable) bool {
	if len(a) != len(b) {
		return false
	}
	for i, left := range a {
		right := b[i]
		if len(left.rows) != len(right.rows) || !maps.Equal(left.guards, right.guards) || !maps.EqualFunc(left.availability, right.availability, slices.Equal[[]string]) {
			return false
		}
		for j, row := range left.rows {
			if !equalRenderBinding(row, right.rows[j]) {
				return false
			}
		}
	}
	return true
}

func equalRenderBinding(a, b Binding) bool {
	if a.Mode != b.Mode || a.Noun != b.Noun || a.Instance != b.Instance || a.Keys != b.Keys || a.Pattern != b.Pattern || a.When != b.When || a.Unbind != b.Unbind || a.Replay != b.Replay || a.Capture != b.Capture || a.Hint != b.Hint || a.Hidden != b.Hidden || a.Help != b.Help || !maps.Equal(a.From, b.From) {
		return false
	}
	x, y := a.Request, b.Request
	return x.Verb == y.Verb && x.Noun == y.Noun && x.Target == y.Target && x.Confirmed == y.Confirmed && x.Via == y.Via && x.Prop == y.Prop && x.Toggle == y.Toggle && reflect.DeepEqual(x.Args, y.Args) && reflect.DeepEqual(x.Value, y.Value) && slices.Equal(x.Subjects, y.Subjects)
}

func (c *runtimeRenderCache) guard(r *Runtime, name string) Guard {
	fn := r.rawGuard(name)
	entry := c.collecting
	if entry == nil {
		return fn
	}
	if fn == nil {
		entry.guards = append(entry.guards, guardReading{name: name})
		return nil
	}
	return func(r *Runtime, ev KeyEvent) bool {
		value := fn(r, ev)
		entry.guards = append(entry.guards, guardReading{name, ev, true, value})
		return value
	}
}
