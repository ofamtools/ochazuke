package bindings

import (
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/table"
)

// Model owns the live declarations and their original baseline. All edit
// indices refer to declaration order, independent of the filter and cursor.
type Model struct {
	modes             []core.Mode
	specs             []core.CommandSpec
	baseline, current []core.Binding
	undo, redo        [][]core.Binding
	reserved          []string
	km                *core.Keymap
	compileErr        error
	rows              []table.Row
	list              *table.Model
	query             string
	filterErr         string
}

// New opens the editor on a snapshot of bindings. App command specs augment
// the built-in registry. A registry's List() supplies its specs in order.
func New(modes []core.Mode, bindings []core.Binding, specs ...core.CommandSpec) *Model {
	m := &Model{modes: slices.Clone(modes), specs: slices.Clone(specs), list: table.New(columns(), nil)}
	m.current = copyBindings(bindings)
	m.baseline = slices.Clone(m.current)
	m.refresh()
	return m
}

func copyBindings(in []core.Binding) []core.Binding {
	out := slices.Clone(in)
	for i, b := range out {
		out[i].Keys = slices.Clone(b.Keys)
		if b.Args != nil {
			out[i].Args = cloneArg(b.Args).(map[string]any)
		}
		if b.Layer != nil {
			layer := *b.Layer
			layer.Modes = slices.Clone(layer.Modes)
			layer.ExcludeTags = slices.Clone(layer.ExcludeTags)
			out[i].Layer = &layer
		}
	}
	return out
}

// Arguments are JSON data; preserve native scalar values during copying.
func cloneArg(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := maps.Clone(x)
		for k, value := range m {
			m[k] = cloneArg(value)
		}
		return m
	case []any:
		a := slices.Clone(x)
		for i, value := range a {
			a[i] = cloneArg(value)
		}
		return a
	case []string:
		return slices.Clone(x)
	default:
		return v
	}
}

// Bindings returns a snapshot suitable for persistence or host reconfiguration.
func (m *Model) Bindings() []core.Binding { return copyBindings(m.current) }

// Keymap returns the immediately updated resolver and any compilation errors.
// core.NewKeymap retains all valid rows even when another row fails to compile.
func (m *Model) Keymap() (*core.Keymap, error) { return m.km, m.compileErr }

// SetReserved replaces the advisory reservations. nil restores the defaults.
func (m *Model) SetReserved(keys []string) {
	m.reserved = slices.Clone(keys)
	m.refresh()
}

func (m *Model) refresh() {
	m.km, m.compileErr = core.NewKeymap(m.modes, m.current, m.specs...)
	analysis := analyze(m.modes, m.km, m.current, m.reserved)
	m.rows = make([]table.Row, len(m.current))
	for i, b := range m.current {
		m.rows[i] = row(i, b, analysis[i])
	}
	m.filter()
}

func (m *Model) beginEdit() {
	m.undo = append(m.undo, m.current)
	m.current = slices.Clone(m.current)
	m.redo = nil
}

func (m *Model) valid(idx int) bool { return idx >= 0 && idx < len(m.current) }

// Rebind replaces the selector with an exact sequence. Conflicts remain
// editable and visible; empty keys and missing indices leave history intact.
func (m *Model) Rebind(idx int, keys []string) bool {
	if !m.valid(idx) || len(keys) == 0 || slices.Contains(keys, "") {
		return false
	}
	b := m.current[idx]
	b.Keys, b.Class = slices.Clone(keys), ""
	if b.Command != "" || b.Layer != nil {
		b.Unbind = false
	}
	if reflect.DeepEqual(b, m.current[idx]) {
		return false
	}
	m.beginEdit()
	m.current[idx] = b
	m.refresh()
	return true
}

// Add appends a declaration and returns its index.
func (m *Model) Add(b core.Binding) int {
	m.beginEdit()
	m.current = append(m.current, copyBindings([]core.Binding{b})[0])
	m.refresh()
	return len(m.current) - 1
}

// Unbind preserves the selector as a core unbind rule, masking lower priority
// declarations. Reset restores the baseline even after a rebind or unbind.
func (m *Model) Unbind(idx int) bool {
	if !m.valid(idx) || m.current[idx].Unbind {
		return false
	}
	m.beginEdit()
	m.current[idx].Unbind = true
	m.refresh()
	return true
}

// Reset restores an original declaration. A declaration added in the editor
// has no baseline: resetting it removes it and shifts later added indices.
func (m *Model) Reset(idx int) bool {
	if !m.valid(idx) || idx < len(m.baseline) && reflect.DeepEqual(m.current[idx], m.baseline[idx]) {
		return false
	}
	m.beginEdit()
	if idx < len(m.baseline) {
		m.current[idx] = m.baseline[idx]
	} else {
		m.current = slices.Delete(m.current, idx, idx+1)
	}
	m.refresh()
	return true
}

func (m *Model) restore(from, to *[][]core.Binding) bool {
	if len(*from) == 0 {
		return false
	}
	last := len(*from) - 1
	*to = append(*to, m.current)
	m.current = (*from)[last]
	*from = slices.Delete(*from, last, last+1)
	m.refresh()
	return true
}

func (m *Model) Undo() bool { return m.restore(&m.undo, &m.redo) }
func (m *Model) Redo() bool { return m.restore(&m.redo, &m.undo) }

func columns() []table.Column {
	return []table.Column{{Key: "key", Title: "Key"}, {Key: "scope", Title: "Scope"}, {Key: "mode", Title: "Mode"}, {Key: "verb", Title: "Command"}, {Key: "hint", Title: "Hint"}, {Key: "status", Title: "Status"}}
}

func row(i int, b core.Binding, analysis Analysis) table.Row {
	key := strings.Join(b.Keys, " ")
	if b.Class != "" {
		key = "<" + b.Class + ">"
	}
	scope, mode := b.Scope, b.Mode
	if scope == "" {
		scope = "*"
	}
	if mode == "" {
		mode = "*"
	}
	target := b.Command
	if b.Layer != nil {
		target = "layer.push " + b.Layer.Name
	}
	var conflicts []string
	for _, c := range analysis.Conflicts {
		if !slices.Contains(conflicts, c.Kind) {
			conflicts = append(conflicts, c.Kind)
		}
	}
	status := slices.Concat(conflicts, analysis.Leader)
	if b.Unbind {
		status = append(status, "unbound")
	}
	id := strconv.Itoa(i)
	r := table.Row{ID: id, Scope: "binding", Ref: &core.Ref{Kind: "binding", ID: id}, Cells: map[string]any{
		"key": key, "scope": scope, "mode": mode, "verb": target, "hint": b.Hint, "when": b.When,
		"conflict": strings.Join(conflicts, " "), "status": strings.Join(status, " "),
	}}
	for _, s := range status {
		r.Badges = append(r.Badges, ui.Badge{Text: s})
	}
	return r
}

// SetQuery applies space-separated field:value filters and free text together.
// With no field filters, the full query is a single substring/fuzzy/regex
// pattern. Structured fields are key, verb, mode, scope, when and conflict.
// Double quotes group field values containing spaces, e.g. key:"g g".
func (m *Model) SetQuery(q string) {
	m.query = q
	m.list.SelectedIdx, m.list.Offset = 0, 0
	m.filter()
}

// SetMatchMode applies the table widget's matching rules to every filter.
func (m *Model) SetMatchMode(mode table.MatchMode) { m.list.MatchMode = mode; m.SetQuery(m.query) }

// FilterError describes an invalid regex; table falls back to literal matching.
func (m *Model) FilterError() string { return m.filterErr }

var queryTokens = regexp.MustCompile(`[^[:space:]:]+:"[^"]*"|[^[:space:]]+`)

func (m *Model) filter() {
	type term struct{ field, value string }
	var terms []term
	var words []string
	for _, token := range queryTokens.FindAllString(m.query, -1) {
		field, value, ok := strings.Cut(token, ":")
		if ok && slices.Contains([]string{"key", "verb", "mode", "scope", "when", "conflict"}, field) {
			if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
				value = value[1 : len(value)-1]
			}
			terms = append(terms, term{field, value})
		} else {
			words = append(words, token)
		}
	}
	free := m.query
	if len(terms) > 0 {
		free = strings.Join(words, " ")
	}
	if free != "" {
		terms = append(terms, term{"", free})
	}

	filter := table.New(append(columns(), table.Column{Key: "when"}), m.rows)
	filter.MatchMode = m.list.MatchMode
	m.filterErr = ""
	for _, t := range terms {
		filter.Query, filter.FilterField = t.value, t.field
		filter.Rows = filter.Filtered()
		if filter.FilterErr != "" {
			m.filterErr = filter.FilterErr
		}
	}
	m.list.Rows = filter.Rows
}

// Filtered returns declaration indices in display order.
func (m *Model) Filtered() []int {
	var out []int
	for _, r := range m.list.Filtered() {
		i, _ := strconv.Atoi(r.ID)
		out = append(out, i)
	}
	return out
}

// Focused returns the current declaration index, or false for an empty list.
func (m *Model) Focused() (int, bool) {
	r, ok := m.list.FocusedRow()
	if !ok {
		return -1, false
	}
	i, _ := strconv.Atoi(r.ID)
	return i, true
}

// HandleCommand accepts table motions, filter text, undo, redo, and edits to
// the focused declaration. Use Rebind and Add to supply structured bindings.
func (m *Model) HandleCommand(c core.Command) bool {
	switch c.ID {
	case "bindings.undo":
		return m.Undo()
	case "bindings.redo":
		return m.Redo()
	case "bindings.unbind", "bindings.reset":
		i, ok := m.Focused()
		if !ok {
			return false
		}
		if c.ID == "bindings.unbind" {
			return m.Unbind(i)
		}
		return m.Reset(i)
	case "bindings.filter.set":
		m.SetQuery(c.Arg("query"))
	case "bindings.filter.append":
		m.SetQuery(m.query + c.Arg("text"))
	case "bindings.filter.backspace":
		_, size := utf8.DecodeLastRuneInString(m.query)
		m.SetQuery(m.query[:len(m.query)-size])
	case "table.filter.mode.cycle":
		m.list.CycleMatchMode()
		m.SetQuery(m.query)
	default:
		return m.list.HandleCommand(c)
	}
	return true
}

// View renders the filter and live declarations as focusable child nodes.
func (m *Model) View(id string) ui.ViewNode {
	input := ui.ViewNode{ID: id + ".filter", Kind: ui.KindInput, Title: "Filter", Focusable: true, Scope: "bindings.filter", Props: map[string]any{"value": m.query, "placeholder": "key:  verb:  mode:  scope:  when:  conflict:"}}
	view := ui.ViewNode{ID: id, Kind: ui.KindVStack, Scope: "bindings", Children: []ui.ViewNode{input, m.list.View(id + ".table")}}
	if m.filterErr != "" {
		view.Children = append(view.Children, ui.ViewNode{Kind: ui.KindText, Tone: "error", Props: map[string]any{"text": m.filterErr}})
	}
	return view
}
