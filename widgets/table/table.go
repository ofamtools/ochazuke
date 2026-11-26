// Package table is a pure, deterministic table widget model: filter, sort,
// group, cursor, multi-select, and rendering to a ui.KindTable ViewNode.
//
// It performs no I/O and imports only the standard library, core, and ui.
package table

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/ui"
)

// MatchMode selects how Query matches cells.
type MatchMode = traits.MatchMode

// Match modes.
const (
	MatchSubstring = traits.MatchSubstring
	MatchFuzzy     = traits.MatchFuzzy
	MatchRegex     = traits.MatchRegex
)

// SortDir is a sort direction.
type SortDir int

// Sort directions.
const (
	SortNone SortDir = iota
	SortAsc
	SortDesc
)

// SortKey is one sort criterion.
type SortKey struct {
	Field string  `json:"field"`
	Dir   SortDir `json:"dir"`
}

// Column describes one displayed column.
type Column struct {
	Key, Title string
	Width      int
	Align      string
}

// Row is one record. Cells holds values by key; any key may be filtered,
// sorted or grouped, whether or not a Column displays it.
type Row struct {
	ID        string
	Ref       *core.Ref
	Scope     string
	Cells     map[string]any
	Badges    []ui.Badge
	Unread    bool
	Facts     map[string]any
	Tone      string
	CellTones map[string]string
	CellIcons map[string]string
	IconTones map[string]string
	CellSpans map[string][]ui.ToneSpan
}

// Item is one display line: a group header or a row.
type Item struct {
	Header    bool
	Group     string
	Count     int
	Collapsed bool
	Row       Row
}

// Model is the table state. Use pointer methods to mutate.
type Model struct {
	Columns []Column
	Rows    []Row

	Query       string
	FilterField string // "" = all columns
	MatchMode   MatchMode
	FilterErr   string

	SortField string
	SortDir   SortDir
	SortKeys  []SortKey

	GroupField string
	Collapsed  map[string]bool

	SelectedIdx  int
	Offset       int
	PageSize     int
	ScrollOff    int
	Marked       map[string]bool
	VisualActive bool
	VisualAnchor int
	CellSelect   bool
	SelectedCol  int

	// MotionSelection is the range the last motion.object.select reported, in
	// MotionNodes indices (nil when the focused item has no such object).
	MotionSelection *traits.ObjectRange
}

// New returns a model with defaults.
func New(cols []Column, rows []Row) *Model {
	return &Model{Columns: cols, Rows: rows, MatchMode: MatchSubstring, PageSize: 10}
}

// Copy returns an independent copy (maps cloned; Rows shared read-only).
func (m Model) Copy() Model {
	m.Collapsed = cloneBool(m.Collapsed)
	m.Marked = cloneBool(m.Marked)
	m.SortKeys = slices.Clone(m.SortKeys)
	return m
}

func cloneBool(in map[string]bool) map[string]bool {
	if in == nil {
		return nil
	}
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Text renders a cell value as display text.
func Text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	default:
		if runs, ok := ui.DecodeInlineRuns(v); ok {
			plain, _, _ := ui.CompileInlineRuns(runs, -1)
			return plain
		}
		return fmt.Sprint(v)
	}
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, !math.IsNaN(x)
	}
	return 0, false
}

func compare(a, b any) int {
	if x, ok := number(a); ok {
		if y, ok := number(b); ok {
			switch {
			case x < y:
				return -1
			case x > y:
				return 1
			}
			return 0
		}
	}
	if x, ok := a.(bool); ok {
		if y, ok := b.(bool); ok {
			switch {
			case x == y:
				return 0
			case !x:
				return -1
			}
			return 1
		}
	}
	return strings.Compare(strings.ToLower(Text(a)), strings.ToLower(Text(b)))
}

// matcher builds the row predicate and records any regex error.
func (m *Model) matcher() traits.Matcher {
	mat, err := traits.CompileFilter(m.Query, m.MatchMode, traits.FilterConfig{})
	m.FilterErr = err
	return mat
}

func (m *Model) rowMatches(r Row, match traits.Matcher) bool {
	if m.FilterField != "" {
		return match.Match(Text(r.Cells[m.FilterField]))
	}
	for _, c := range m.Columns {
		if match.Match(Text(r.Cells[c.Key])) {
			return true
		}
	}
	return false
}

func (m *Model) resolveFieldKey(r Row, field string) (string, bool) {
	if field == "" {
		return "", false
	}
	for _, c := range m.Columns {
		if c.Key == field {
			return c.Key, true
		}
	}
	if _, ok := r.Cells[field]; ok {
		return field, true
	}
	lf := strings.ToLower(field)
	for _, c := range m.Columns {
		if strings.ToLower(c.Key) == lf {
			return c.Key, true
		}
	}
	for k := range r.Cells {
		if strings.ToLower(k) == lf {
			return k, true
		}
	}
	return "", false
}

func (m *Model) rowContains(r Row, term string) bool {
	lt := strings.ToLower(term)
	if m.FilterField != "" {
		return strings.Contains(strings.ToLower(Text(r.Cells[m.FilterField])), lt)
	}
	for _, c := range m.Columns {
		if strings.Contains(strings.ToLower(Text(r.Cells[c.Key])), lt) {
			return true
		}
	}
	return false
}

func (m *Model) evalAlt(r Row, alt string) bool {
	if m.FilterField == "" {
		if idx := strings.Index(alt, "!="); idx > 0 {
			if key, ok := m.resolveFieldKey(r, alt[:idx]); ok {
				val := alt[idx+2:]
				return !strings.Contains(strings.ToLower(Text(r.Cells[key])), strings.ToLower(val))
			}
		}
		if idx := strings.Index(alt, ":"); idx > 0 {
			if key, ok := m.resolveFieldKey(r, alt[:idx]); ok {
				val := alt[idx+1:]
				neg := false
				if strings.HasPrefix(val, "!") && len(val) > 1 {
					neg, val = true, val[1:]
				}
				hit := strings.Contains(strings.ToLower(Text(r.Cells[key])), strings.ToLower(val))
				if neg {
					return !hit
				}
				return hit
			}
		}
	}
	return m.rowContains(r, alt)
}

func (m *Model) evalClause(r Row, tok string) bool {
	neg := false
	body := tok
	switch {
	case strings.HasPrefix(body, "!=") && len(body) > 2:
		neg, body = true, body[2:]
	case strings.HasPrefix(body, "!") && len(body) > 1:
		neg, body = true, body[1:]
	}
	// Check for field:a|b or field!=a|b before splitting on |.
	if m.FilterField == "" {
		if idx := strings.Index(body, "!="); idx > 0 {
			if key, ok := m.resolveFieldKey(r, body[:idx]); ok {
				cell := strings.ToLower(Text(r.Cells[key]))
				alts := strings.Split(body[idx+2:], "|")
				matched := false
				for _, a := range alts {
					if strings.Contains(cell, strings.ToLower(a)) {
						matched = true
						break
					}
				}
				// field!=val negates the match; a leading ! flips it back.
				res := !matched
				if neg {
					return !res
				}
				return res
			}
		}
		if idx := strings.Index(body, ":"); idx > 0 {
			if key, ok := m.resolveFieldKey(r, body[:idx]); ok {
				rest := body[idx+1:]
				if strings.HasPrefix(rest, "!") && len(rest) > 1 {
					neg, rest = !neg, rest[1:]
				}
				cell := strings.ToLower(Text(r.Cells[key]))
				alts := strings.Split(rest, "|")
				matched := false
				for _, a := range alts {
					if strings.Contains(cell, strings.ToLower(a)) {
						matched = true
						break
					}
				}
				if neg {
					return !matched
				}
				return matched
			}
		}
	}
	alts := strings.Split(body, "|")
	matched := false
	for _, a := range alts {
		if a == "" && len(alts) > 1 {
			continue
		}
		if m.evalAlt(r, a) {
			matched = true
			break
		}
	}
	if neg {
		return !matched
	}
	return matched
}

func (m *Model) rowMatchesExpr(r Row, clauses []string) bool {
	for _, c := range clauses {
		if !m.evalClause(r, c) {
			return false
		}
	}
	return true
}

func (m *Model) effectiveSortKeys() []SortKey {
	if len(m.SortKeys) > 0 {
		out := make([]SortKey, 0, len(m.SortKeys))
		for _, k := range m.SortKeys {
			if k.Field != "" && k.Dir != SortNone {
				out = append(out, k)
			}
		}
		return out
	}
	if m.SortField != "" && m.SortDir != SortNone {
		return []SortKey{{Field: m.SortField, Dir: m.SortDir}}
	}
	return nil
}

func (m *Model) syncPrimarySort() {
	if len(m.SortKeys) > 0 {
		m.SortField = m.SortKeys[0].Field
		m.SortDir = m.SortKeys[0].Dir
	}
}

// Filtered returns rows passing the filter, sorted. It refreshes FilterErr.
func (m *Model) Filtered() []Row {
	return m.filteredSlice(true)
}

func (m *Model) filteredSlice(cloneUnmodified bool) []Row {
	var out []Row
	unmodified := false
	if m.Query == "" {
		m.FilterErr = ""
		if cloneUnmodified {
			out = slices.Clone(m.Rows)
		} else {
			out = m.Rows
			unmodified = true
		}
	} else if m.MatchMode == "" || m.MatchMode == MatchSubstring {
		m.FilterErr = ""
		clauses := strings.Fields(m.Query)
		if len(clauses) == 0 {
			match := m.matcher()
			for _, r := range m.Rows {
				if m.rowMatches(r, match) {
					out = append(out, r)
				}
			}
		} else {
			for _, r := range m.Rows {
				if m.rowMatchesExpr(r, clauses) {
					out = append(out, r)
				}
			}
		}
	} else {
		match := m.matcher()
		for _, r := range m.Rows {
			if m.rowMatches(r, match) {
				out = append(out, r)
			}
		}
	}
	if len(m.SortKeys) > 0 {
		m.syncPrimarySort()
	}
	if keys := m.effectiveSortKeys(); len(keys) > 0 {
		if unmodified {
			out = slices.Clone(m.Rows)
		}
		sort.SliceStable(out, func(i, j int) bool {
			for _, k := range keys {
				c := compare(out[i].Cells[k.Field], out[j].Cells[k.Field])
				if c != 0 {
					if k.Dir == SortDesc {
						return c > 0
					}
					return c < 0
				}
			}
			return out[i].ID < out[j].ID
		})
	}
	return out
}

// Items returns the display lines: filtered, sorted, grouped.
func (m *Model) Items() []Item {
	rows := m.filteredSlice(false)
	if m.GroupField == "" {
		out := make([]Item, len(rows))
		for i := range rows {
			out[i] = Item{Row: rows[i]}
		}
		return out
	}
	var order []string
	groups := map[string][]Row{}
	for _, r := range rows {
		g := Text(r.Cells[m.GroupField])
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], r)
	}
	var out []Item
	for _, g := range order {
		col := m.Collapsed[g]
		out = append(out, Item{Header: true, Group: g, Count: len(groups[g]), Collapsed: col})
		if !col {
			for _, r := range groups[g] {
				out = append(out, Item{Group: g, Row: r})
			}
		}
	}
	return out
}

func (m *Model) viewportState(n int) traits.ViewportState {
	st := traits.ViewportState{Cursor: m.SelectedIdx, Offset: m.Offset, Total: n, Height: max(m.PageSize, 1)}
	if m.CellSelect && len(m.Columns) > 0 {
		st.ScrollX, st.ContentWidth, st.ViewWidth = m.SelectedCol, len(m.Columns), 1
	}
	return st
}

func (m *Model) viewportConfig() traits.ViewportConfig {
	return traits.ViewportConfig{ScrollOff: m.ScrollOff}
}

func (m *Model) clamp(n int) {
	st := traits.ViewportClamp(m.viewportState(n), m.viewportConfig())
	m.SelectedIdx, m.Offset = st.Cursor, st.Offset
}

// Focused returns the focused display item.
func (m *Model) Focused() (Item, bool) {
	return m.focusedFor(m.Items())
}

func (m *Model) focusedFor(items []Item) (Item, bool) {
	m.clamp(len(items))
	if len(items) == 0 {
		return Item{}, false
	}
	return items[m.SelectedIdx], true
}

// FocusedRow returns the focused data row (false on a group header or empty).
func (m *Model) FocusedRow() (Row, bool) {
	return m.focusedRowFor(m.Items())
}

func (m *Model) focusedRowFor(items []Item) (Row, bool) {
	it, ok := m.focusedFor(items)
	if !ok || it.Header {
		return Row{}, false
	}
	return it.Row, true
}

// ToggleGroup flips collapse of the focused item's group.
func (m *Model) ToggleGroup() {
	it, ok := m.Focused()
	if !ok || m.GroupField == "" {
		return
	}
	target := traits.FoldTarget{ID: it.Group, HasChildren: true}
	if !it.Header {
		target = traits.FoldTarget{ID: it.Row.ID, ParentID: it.Group}
	}
	st, focusID, _, _ := traits.FoldHandle(traits.FoldState{Collapsed: m.Collapsed}, traits.FoldConfig{LeafFoldsParent: true}, target, nil, core.Command{ID: traits.FoldCmdToggle})
	m.Collapsed = st.Collapsed
	if focusID == "" {
		focusID = it.Group
	}
	m.snapToGroup(focusID)
}

func (m *Model) snapToGroup(g string) {
	items := m.Items()
	for i, it := range items {
		if it.Header && it.Group == g {
			m.SelectedIdx = i
			break
		}
	}
	m.clamp(len(items))
}

// groupsAll lists group values ignoring collapse state.
func (m *Model) groupsAll() []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range m.Filtered() {
		g := Text(r.Cells[m.GroupField])
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	return out
}

// ExpandAllGroups expands every group.
func (m *Model) ExpandAllGroups() {
	st, _, _, _ := traits.FoldHandle(traits.FoldState{Collapsed: m.Collapsed}, traits.FoldConfig{}, traits.FoldTarget{}, nil, core.Command{ID: traits.FoldCmdOpenAll})
	m.Collapsed = st.Collapsed
	m.clamp(len(m.Items()))
}

// CollapseAllGroups collapses every group.
func (m *Model) CollapseAllGroups() {
	if m.GroupField == "" {
		return
	}
	st, _, _, _ := traits.FoldHandle(traits.FoldState{Collapsed: m.Collapsed}, traits.FoldConfig{}, traits.FoldTarget{}, m.groupsAll(), core.Command{ID: traits.FoldCmdCloseAll})
	m.Collapsed = st.Collapsed
	m.clamp(len(m.Items()))
}

func (m *Model) selectState() traits.SelectState {
	return traits.SelectState{
		Marked:       m.Marked,
		VisualActive: m.VisualActive,
		VisualAnchor: m.VisualAnchor,
	}
}

func (m *Model) applySelectState(st traits.SelectState) {
	m.Marked = st.Marked
	m.VisualActive = st.VisualActive
	m.VisualAnchor = st.VisualAnchor
}

func itemVisibleIDs(items []Item) []string {
	ids := make([]string, len(items))
	for i, it := range items {
		if !it.Header {
			ids[i] = it.Row.ID
		}
	}
	return ids
}

// ToggleMark flips the mark on the focused row.
func (m *Model) ToggleMark() {
	items := m.Items()
	m.clamp(len(items))
	st := m.selectState()
	st.VisualActive = false
	next, _, _ := traits.SelectHandle(st, itemVisibleIDs(items), m.SelectedIdx, core.Command{ID: traits.SelectCmdMarkToggle})
	m.Marked = next.Marked
}

// ClearMarks clears all marks.
func (m *Model) ClearMarks() { m.Marked = nil }

// MarkedRows returns marked rows in model order.
func (m *Model) MarkedRows() []Row {
	if len(m.Marked) == 0 {
		return nil
	}
	var out []Row
	for _, r := range m.Rows {
		if m.Marked[r.ID] {
			out = append(out, r)
		}
	}
	return out
}

// ToggleVisual enters visual range selection anchored at SelectedIdx, or exits it.
func (m *Model) ToggleVisual() {
	items := m.Items()
	m.clamp(len(items))
	next, _, _ := traits.SelectHandle(m.selectState(), itemVisibleIDs(items), m.SelectedIdx, core.Command{ID: traits.SelectCmdVisualToggle})
	m.applySelectState(next)
}

// ClearVisual exits visual range selection.
func (m *Model) ClearVisual() { m.VisualActive = false }

func (m *Model) visualBounds(n int) (from, to int, ok bool) {
	return traits.SelectBounds(m.selectState(), n, m.SelectedIdx)
}

// VisualRows returns the data rows within the active visual range, skipping group headers.
func (m *Model) VisualRows() []Row {
	items := m.Items()
	from, to, ok := m.visualBounds(len(items))
	if !ok {
		return nil
	}
	var out []Row
	for i := from; i <= to; i++ {
		if !items[i].Header {
			out = append(out, items[i].Row)
		}
	}
	return out
}

// MarkVisual marks all data rows in the active visual range and exits visual mode.
func (m *Model) MarkVisual() {
	items := m.Items()
	m.clamp(len(items))
	next, _, _ := traits.SelectHandle(m.selectState(), itemVisibleIDs(items), m.SelectedIdx, core.Command{ID: traits.SelectCmdVisualMark})
	m.applySelectState(next)
}

// ToggleCellSelect toggles cell-level selection mode.
func (m *Model) ToggleCellSelect() {
	m.CellSelect = !m.CellSelect
	if len(m.Columns) > 0 {
		m.SelectedCol = max(0, min(m.SelectedCol, len(m.Columns)-1))
	} else {
		m.SelectedCol = 0
	}
}

// FocusedCell returns the column key and cell value at SelectedCol on the focused data row.
func (m *Model) FocusedCell() (colKey string, value any, ok bool) {
	r, ok := m.FocusedRow()
	if !ok || len(m.Columns) == 0 {
		return "", nil, false
	}
	col := max(0, min(m.SelectedCol, len(m.Columns)-1))
	key := m.Columns[col].Key
	return key, r.Cells[key], true
}

// CycleSort advances none -> asc -> desc -> none on field ("" = current, or
// the first column). A different field starts at asc.
func (m *Model) CycleSort(field string) {
	m.SortKeys = nil
	if field == "" {
		field = m.SortField
	}
	if field == "" && len(m.Columns) > 0 {
		field = m.Columns[0].Key
	}
	if field != m.SortField {
		m.SortField, m.SortDir = field, SortAsc
		return
	}
	m.SortDir = (m.SortDir + 1) % 3
	if m.SortDir == SortNone {
		m.SortField = ""
	}
}

// AddSort cycles an existing sort key in SortKeys (asc -> desc -> removed) or
// appends field as an ascending secondary sort key, keeping SortField/SortDir
// synced to SortKeys[0].
func (m *Model) AddSort(field string) {
	if field == "" && len(m.Columns) > 0 {
		field = m.Columns[0].Key
	}
	if field == "" {
		return
	}
	if len(m.SortKeys) == 0 && m.SortField != "" && m.SortDir != SortNone {
		m.SortKeys = []SortKey{{Field: m.SortField, Dir: m.SortDir}}
	}
	found := -1
	for i, k := range m.SortKeys {
		if k.Field == field {
			found = i
			break
		}
	}
	if found >= 0 {
		next := (m.SortKeys[found].Dir + 1) % 3
		if next == SortNone {
			m.SortKeys = slices.Delete(m.SortKeys, found, found+1)
		} else {
			m.SortKeys[found].Dir = next
		}
	} else {
		m.SortKeys = append(m.SortKeys, SortKey{Field: field, Dir: SortAsc})
	}
	if len(m.SortKeys) > 0 {
		m.SortField = m.SortKeys[0].Field
		m.SortDir = m.SortKeys[0].Dir
	} else {
		m.SortField = ""
		m.SortDir = SortNone
	}
}

// CycleMatchMode advances substring -> fuzzy -> regex.
func (m *Model) CycleMatchMode() {
	m.MatchMode = traits.CycleMatchMode(m.MatchMode, traits.FilterConfig{})
}

var _ traits.Filterable = (*Model)(nil)

// Filter implements traits.Filterable in place on the table without deleting
// underlying rows and reports whether at least one row remains visible.
func (m *Model) Filter(query string, mode traits.MatchMode) bool {
	m.Query = query
	if mode != "" {
		m.MatchMode = mode
	}
	m.SelectedIdx, m.Offset = 0, 0
	return len(m.Filtered()) > 0
}

func (m *Model) FilterQuery() string { return m.Query }

func (m *Model) FilterMode() traits.MatchMode {
	if m.MatchMode == "" {
		return traits.MatchSubstring
	}
	return m.MatchMode
}

func (m *Model) FilterError() string { return m.FilterErr }
func (m *Model) IsFiltered() bool    { return strings.TrimSpace(m.Query) != "" }

// HandleCommand applies a motion or table command; it reports whether the
// command was handled.
func (m *Model) HandleCommand(cmd core.Command) bool {
	items := m.Items()
	m.clamp(len(items))
	if next, _, handled := traits.ViewportStep(m.viewportState(len(items)), m.viewportConfig(), cmd); handled {
		m.SelectedIdx, m.Offset = next.Cursor, next.Offset
		return true
	}
	if next, _, handled := traits.SelectHandle(m.selectState(), itemVisibleIDs(items), m.SelectedIdx, cmd); handled {
		m.applySelectState(next)
		return true
	}
	if m.stepMotion(items, cmd) {
		return true
	}
	n := cmd.Times()
	switch cmd.ID {
	case "motion.left":
		if !m.CellSelect {
			return false
		}
		if len(m.Columns) > 0 {
			m.SelectedCol = max(0, min(m.SelectedCol-n, len(m.Columns)-1))
		}
	case "motion.right":
		if !m.CellSelect {
			return false
		}
		if len(m.Columns) > 0 {
			m.SelectedCol = max(0, min(m.SelectedCol+n, len(m.Columns)-1))
		}
	case "table.sort.cycle":
		m.CycleSort(cmd.Arg("field"))
	case "table.sort.add":
		m.AddSort(cmd.Arg("field"))
	case "table.visual.toggle":
		m.ToggleVisual()
	case "table.visual.mark":
		m.MarkVisual()
	case "table.visual.clear":
		m.ClearVisual()
	case "table.cell.toggle":
		m.ToggleCellSelect()
	case "esc":
		if !m.VisualActive {
			return false
		}
		m.ClearVisual()
	case "table.group.toggle":
		if f := cmd.Arg("field"); f != "" {
			if m.GroupField == f {
				f = ""
			}
			m.GroupField, m.Collapsed = f, nil
		} else {
			m.ToggleGroup()
		}
	case "table.mark.toggle":
		m.ToggleMark()
	case "table.filter.mode.cycle":
		m.CycleMatchMode()
	default:
		return false
	}
	m.clamp(len(m.Items()))
	return true
}

func rowCountFor(items []Item, grouped bool) int {
	if !grouped {
		return len(items)
	}
	n := 0
	for i := range items {
		if items[i].Header {
			n += items[i].Count
		}
	}
	return n
}

func (m *Model) selectFactsFor(it Item, ok bool, items []Item) map[string]any {
	if m.VisualActive {
		return traits.SelectFacts(m.selectState(), itemVisibleIDs(items), m.SelectedIdx)
	}
	if ok && !it.Header && len(m.Marked) > 0 {
		return traits.SelectFacts(m.selectState(), []string{it.Row.ID}, 0)
	}
	return traits.SelectFacts(m.selectState(), nil, 0)
}

func (m *Model) facts(it Item, ok bool, items []Item) map[string]any {
	if len(m.SortKeys) > 0 {
		m.syncPrimarySort()
	}
	f := traits.ViewportFacts(m.viewportState(len(items)), m.viewportConfig())
	f = traits.MergeFacts(f, traits.FilterFacts(traits.FilterState{Query: m.Query, Mode: m.MatchMode, Err: m.FilterErr}, traits.FilterConfig{}))
	f = traits.MergeFacts(f, m.selectFactsFor(it, ok, items))
	return traits.MergeFacts(f, map[string]any{
		"groupField":    m.GroupField,
		"sortField":     m.SortField,
		"rowCount":      rowCountFor(items, m.GroupField != ""),
		"isGroupHeader": ok && it.Header,
		"cellSelect":    m.CellSelect,
		"selectedCol":   m.SelectedCol,
	})
}

// ContextFrame describes the focused row for the modal context chain.
func (m *Model) ContextFrame(scope, nodeID string) core.ContextFrame {
	return m.contextFrameFor(scope, nodeID, m.Items())
}

func (m *Model) contextFrameFor(scope, nodeID string, items []Item) core.ContextFrame {
	m.clamp(len(items))
	var it Item
	var ok bool
	if len(items) > 0 {
		it, ok = items[m.SelectedIdx], true
	}
	f := core.ContextFrame{Scope: scope, NodeID: nodeID, Facts: m.facts(it, ok, items)}
	if ok && !it.Header {
		if it.Row.Scope != "" && scope == "" {
			f.Scope = it.Row.Scope
		}
		if it.Row.Ref != nil {
			r := *it.Row.Ref
			f.Subject = &r
		}
		for k, v := range it.Row.Facts {
			if _, dup := f.Facts[k]; !dup {
				f.Facts[k] = v
			}
		}
	}
	for _, r := range m.MarkedRows() {
		if r.Ref != nil {
			f.Subjects = append(f.Subjects, *r.Ref)
		}
	}
	return f
}
