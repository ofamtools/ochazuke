package ochazuke

import (
	"maps"
	"reflect"
	"slices"
)

type runtimeRenderCache struct {
	reach      map[reachKey]*reachWalk // graph_reach.go
	reaching   bool                    // a graph walk is reading stores
	generation uint64
	// changes moves on every change the runtime is told of (Changed, a
	// change request or a change job's end whose verb may write a store,
	// Model.InvalidateRender): what a guard or a store could answer
	// differently since (the palette's line index, palette_parse.go). A
	// verb that declares it writes no store (Operation.Writes empty: text
	// typed into an input, keys sent to a pane) leaves it.
	changes uint64
	// loadTokens are the stores' ChangeTokens as the last load that wrote
	// them saw them (Runtime.storeTokenSame): a periodic load whose store
	// answers the same token leaves changes where it is.
	loadTokens map[string]string
	// names moves on every write to the noun alias or action alias table
	// (setAlias, dropAlias, aliasesChanged, claimName): with the operation
	// registry's Invalidations it is the shape the palette's static index is
	// built from (paletteShape).
	names      uint64
	instances  instanceIndex
	scans      []*bindingScan
	lastScan   *bindingScan
	collecting *bindingScan
	hints      hintMemo
	cells      hintCellsMemo
	help       map[helpRowKey]HelpRow
	stored     helpStoredMemo
	// drawing is the frame Model.View is drawing (watchHub.frames), 0
	// between frames; helpFrame is BindingHelp's answers in that frame.
	drawing   uint64
	helpFrame helpFrameMemo
}

type guardReading struct {
	name           string
	event          KeyEvent
	present, value bool
}

// RenderStamp is every counter a binding walk depends on besides focus, mode
// and the live guards: one comparison of four integers replaces comparing the
// tables themselves. Generation moves on every write to a binding, guard,
// availability or extends table, a mode definition, a noun, its props or its
// store (what a standard or prop row's availability reads: Store.Missing,
// Kind, the calls it has, the writable props), a setting or an alias
// (Runtime.InvalidateBindings, and TestRenderInputWritersBump holds every
// writer in the package to it); Mode on every mode change; Instances on every
// instance added or removed; Operations on every registry invalidation.
type RenderStamp struct {
	Generation, Mode, Instances, Operations uint64
}

func (c *runtimeRenderCache) stamp(r *Runtime) RenderStamp {
	return RenderStamp{c.generation, r.modeGeneration, c.instances.generation, uint64(r.Operations.Invalidations)}
}

type bindingScan struct {
	focus     string
	modeState ModeState
	drop      bool
	stamp     RenderStamp
	levels    []level
	guards    []guardReading
	reaches   []reachDependency // what nearestOf answered while the walk was read
	rows      []levelRow
	grouped   []levelRow
}

// A small bounded cache holds only the current focus and pending-mode walks.
// An entry stays valid while its RenderStamp is unchanged; a hit compares the
// stamp, the focus, the mode state and the walk's levels, never a table.
// Arbitrary application guards are always re-evaluated with their original
// event. A false guard that hid a row is a dependency too. Code that edits a
// public Noun or Instance table in place calls Runtime.InvalidateBindings.
func (c *runtimeRenderCache) scan(r *Runtime, levels []level, mode ModeState, focus string) []levelRow {
	c.lastScan = nil
	if c.collecting != nil {
		return r.scanRowsUncached(levels, mode, focus, nil)
	}
	stamp, drop := c.stamp(r), r.dropsInheritedSequences()
	for _, old := range c.scans {
		if old.focus != focus || old.modeState != mode || old.drop != drop || old.stamp != stamp || !slices.Equal(old.levels, levels) {
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
		for _, dep := range old.reaches {
			if !valid {
				break
			}
			id, things := r.nearestOf(dep.noun, dep.focus, dep.distant)
			valid = reachAnswer(id, things) == dep.answer
		}
		if valid {
			c.lastScan = old
			return old.rows // internal borrowed rows; public APIs copy them
		}
	}
	entry := &bindingScan{focus: focus, modeState: mode, drop: drop, stamp: stamp, levels: slices.Clone(levels)}
	if len(c.scans) >= 8 {
		c.scans = c.scans[:0]
	}
	c.scans = append(c.scans, entry)
	c.collecting = entry
	func() {
		defer func() { c.collecting = nil }()
		entry.rows = r.scanRowsUncached(levels, mode, focus, nil)
	}()
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

func equalRenderBinding(a, b Binding) bool {
	if a.Mode != b.Mode || a.Noun != b.Noun || a.Instance != b.Instance || a.Keys != b.Keys || a.Pattern != b.Pattern || a.When != b.When || a.Unbind != b.Unbind || a.Replay != b.Replay || a.Capture != b.Capture || !slices.Equal(boolList(a.Present), boolList(b.Present)) || a.Hint != b.Hint || a.Hidden != b.Hidden || a.Help != b.Help || !maps.Equal(a.From, b.From) {
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

// boolList is an optional bool as a slice, for comparing rows.
func boolList(p *bool) []bool {
	if p == nil {
		return nil
	}
	return []bool{*p}
}

// InvalidateBindings tells the render caches that a binding, guard,
// availability or extends table, a mode definition, or a noun's props or
// store, changed. Every writer in this package calls it
// (TestRenderInputWritersBump); a host that edits a public Noun or Instance
// table in place, past Bind and the builders (a Noun's Store replaced, its
// Props edited), calls it afterwards, or hints and help keep showing the old
// rows.
func (r *Runtime) InvalidateBindings() { r.renderCache.generation++ }
