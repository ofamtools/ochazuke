// Package table is a pure, deterministic table widget model: filter, sort,
// group, cursor, multi-select, and rendering to a ui.KindTable ViewNode.
//
// It performs no I/O and imports only the standard library, core, and ui.
package table

import (
	"fmt"
	"hash/fnv"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

// MatchMode selects how Query matches cells.
type MatchMode string

// Match modes.
const (
	MatchSubstring MatchMode = "substring"
	MatchFuzzy     MatchMode = "fuzzy"
	MatchRegex     MatchMode = "regex"
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
	ID     string
	Ref    *core.Ref
	Scope  string
	Cells  map[string]any
	Badges []ui.Badge
	Unread bool
	Facts  map[string]any
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
	Marked       map[string]bool
	VisualActive bool
	VisualAnchor int
	CellSelect   bool
	SelectedCol  int
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

func fuzzy(hay, needle string) bool {
	h := []rune(hay)
	i := 0
	for _, n := range []rune(needle) {
		for i < len(h) && h[i] != n {
			i++
		}
		if i == len(h) {
			return false
		}
		i++
	}
	return true
}

// matcher builds the row predicate and records any regex error.
func (m *Model) matcher() func(string) bool {
	q := m.Query
	m.FilterErr = ""
	sub := func(s string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(q)) }
	switch m.MatchMode {
	case MatchFuzzy:
		lq := strings.ToLower(q)
		return func(s string) bool { return fuzzy(strings.ToLower(s), lq) }
	case MatchRegex:
		re, err := regexp.Compile("(?i)" + q)
		if err != nil {
			m.FilterErr = err.Error()
			return sub
		}
		return re.MatchString
	}
	return sub
}

func (m *Model) rowMatches(r Row, match func(string) bool) bool {
	if m.FilterField != "" {
		return match(Text(r.Cells[m.FilterField]))
	}
	for _, c := range m.Columns {
		if match(Text(r.Cells[c.Key])) {
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
	var out []Row
	if m.Query == "" {
		m.FilterErr = ""
		out = slices.Clone(m.Rows)
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
	rows := m.Filtered()
	if m.GroupField == "" {
		out := make([]Item, len(rows))
		for i, r := range rows {
			out[i] = Item{Row: r}
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

func (m *Model) clamp(n int) {
	m.SelectedIdx = max(0, min(m.SelectedIdx, n-1))
	ps := max(m.PageSize, 1)
	if m.SelectedIdx < m.Offset {
		m.Offset = m.SelectedIdx
	}
	if m.SelectedIdx >= m.Offset+ps {
		m.Offset = m.SelectedIdx - ps + 1
	}
	m.Offset = max(0, m.Offset)
}

// Focused returns the focused display item.
func (m *Model) Focused() (Item, bool) {
	items := m.Items()
	m.clamp(len(items))
	if len(items) == 0 {
		return Item{}, false
	}
	return items[m.SelectedIdx], true
}

// FocusedRow returns the focused data row (false on a group header or empty).
func (m *Model) FocusedRow() (Row, bool) {
	it, ok := m.Focused()
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
	if m.Collapsed == nil {
		m.Collapsed = map[string]bool{}
	}
	m.Collapsed[it.Group] = !m.Collapsed[it.Group]
	m.snapToGroup(it.Group)
}

func (m *Model) snapToGroup(g string) {
	for i, it := range m.Items() {
		if it.Header && it.Group == g {
			m.SelectedIdx = i
			break
		}
	}
	m.clamp(len(m.Items()))
}

func (m *Model) groups() []string {
	var out []string
	for _, it := range m.Items() {
		if it.Header {
			out = append(out, it.Group)
		}
	}
	return out
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
func (m *Model) ExpandAllGroups() { m.Collapsed = nil; m.clamp(len(m.Items())) }

// CollapseAllGroups collapses every group.
func (m *Model) CollapseAllGroups() {
	if m.GroupField == "" {
		return
	}
	m.Collapsed = map[string]bool{}
	for _, g := range m.groupsAll() {
		m.Collapsed[g] = true
	}
	m.clamp(len(m.Items()))
}

// ToggleMark flips the mark on the focused row.
func (m *Model) ToggleMark() {
	r, ok := m.FocusedRow()
	if !ok {
		return
	}
	if m.Marked == nil {
		m.Marked = map[string]bool{}
	}
	if m.Marked[r.ID] {
		delete(m.Marked, r.ID)
	} else {
		m.Marked[r.ID] = true
	}
}

// ClearMarks clears all marks.
func (m *Model) ClearMarks() { m.Marked = nil }

// MarkedRows returns marked rows in model order.
func (m *Model) MarkedRows() []Row {
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
	if m.VisualActive {
		m.VisualActive = false
		return
	}
	items := m.Items()
	m.clamp(len(items))
	m.VisualActive = true
	m.VisualAnchor = m.SelectedIdx
}

// ClearVisual exits visual range selection.
func (m *Model) ClearVisual() { m.VisualActive = false }

func (m *Model) visualBounds(n int) (from, to int, ok bool) {
	if !m.VisualActive || n == 0 {
		return 0, 0, false
	}
	a := max(0, min(m.VisualAnchor, n-1))
	s := max(0, min(m.SelectedIdx, n-1))
	return min(a, s), max(a, s), true
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
	rows := m.VisualRows()
	if len(rows) > 0 {
		if m.Marked == nil {
			m.Marked = map[string]bool{}
		}
		for _, r := range rows {
			m.Marked[r.ID] = true
		}
	}
	m.VisualActive = false
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
	switch m.MatchMode {
	case MatchFuzzy:
		m.MatchMode = MatchRegex
	case MatchRegex:
		m.MatchMode = MatchSubstring
	default:
		m.MatchMode = MatchFuzzy
	}
}

// HandleCommand applies a motion or table command; it reports whether the
// command was handled.
func (m *Model) HandleCommand(cmd core.Command) bool {
	n := cmd.Times()
	items := m.Items()
	last := len(items) - 1
	ps := max(m.PageSize, 1)
	move := func(to int) { m.SelectedIdx = max(0, min(to, last)); m.clamp(len(items)) }
	m.clamp(len(items))
	switch cmd.ID {
	case "motion.down":
		move(m.SelectedIdx + n)
	case "motion.up":
		move(m.SelectedIdx - n)
	case "motion.top":
		move(0)
	case "motion.bottom":
		move(last)
	case "motion.page.down":
		move(m.SelectedIdx + n*ps)
	case "motion.page.up":
		move(m.SelectedIdx - n*ps)
	case "motion.half.down":
		move(m.SelectedIdx + n*max(ps/2, 1))
	case "motion.half.up":
		move(m.SelectedIdx - n*max(ps/2, 1))
	case "motion.block.next", "motion.block.prev":
		next := cmd.ID == "motion.block.next"
		for ; n > 0; n-- {
			to := m.SelectedIdx
			if next {
				for i := m.SelectedIdx + 1; i <= last; i++ {
					if items[i].Header {
						to = i
						break
					}
				}
			} else {
				cur := m.SelectedIdx
				if cur >= 0 && cur <= last && items[cur].Header {
					cur--
				}
				for i := cur; i >= 0; i-- {
					if items[i].Header {
						to = i
						break
					}
				}
			}
			move(to)
		}
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

func (m *Model) facts(it Item, ok bool) map[string]any {
	mode := m.MatchMode
	if mode == "" {
		mode = MatchSubstring
	}
	if len(m.SortKeys) > 0 {
		m.syncPrimarySort()
	}
	return map[string]any{
		"hasFilter":     m.Query != "",
		"matchMode":     string(mode),
		"groupField":    m.GroupField,
		"sortField":     m.SortField,
		"markedCount":   len(m.Marked),
		"rowCount":      len(m.Filtered()),
		"isGroupHeader": ok && it.Header,
		"visualActive":  m.VisualActive,
		"visualCount":   len(m.VisualRows()),
		"cellSelect":    m.CellSelect,
		"selectedCol":   m.SelectedCol,
	}
}

// ContextFrame describes the focused row for the modal context chain.
func (m *Model) ContextFrame(scope, nodeID string) core.ContextFrame {
	it, ok := m.Focused()
	f := core.ContextFrame{Scope: scope, NodeID: nodeID, Facts: m.facts(it, ok)}
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

// View renders the model as a ui.KindTable node.
func (m *Model) View(id string) ui.ViewNode {
	items := m.Items()
	m.clamp(len(items))
	sortKeys := m.effectiveSortKeys()
	cols := make([]any, len(m.Columns))
	for i, c := range m.Columns {
		title := c.Title
		if title == "" {
			title = c.Key
		}
		for idx, sk := range sortKeys {
			if c.Key == sk.Field && sk.Dir != SortNone {
				arrow := "▲"
				if sk.Dir == SortDesc {
					arrow = "▼"
				}
				if len(sortKeys) > 1 {
					title += fmt.Sprintf(" %s%d", arrow, idx+1)
				} else {
					title += " " + arrow
				}
				break
			}
		}
		col := map[string]any{"key": c.Key, "title": title}
		if c.Width > 0 {
			col["width"] = c.Width
		}
		if c.Align != "" {
			col["align"] = c.Align
		}
		cols[i] = col
	}
	rows := make([]any, len(items))
	for i, it := range items {
		cells := make([]any, len(m.Columns))
		if it.Header {
			mark := "▾"
			if it.Collapsed {
				mark = "▸"
			}
			if len(cells) > 0 {
				cells[0] = fmt.Sprintf("%s %s (%d)", mark, it.Group, it.Count)
			}
			for j := 1; j < len(cells); j++ {
				cells[j] = ""
			}
		} else {
			for j, c := range m.Columns {
				cells[j] = Text(it.Row.Cells[c.Key])
			}
			if len(cells) > 0 && m.Marked[it.Row.ID] {
				cells[0] = "● " + cells[0].(string)
			}
		}
		rows[i] = cells
	}
	props := map[string]any{"columns": cols, "rows": rows, "selected": m.SelectedIdx, "offset": m.Offset}
	vFrom, vTo, vOK := m.visualBounds(len(items))
	if vOK {
		props["visualFrom"] = vFrom
		props["visualTo"] = vTo
	}
	selCol := -1
	if m.CellSelect && len(m.Columns) > 0 {
		selCol = max(0, min(m.SelectedCol, len(m.Columns)-1))
		props["selectedCol"] = selCol
	}
	n := ui.ViewNode{
		ID: id, Kind: ui.KindTable, Focusable: true,
		Props: props,
	}
	it, ok := m.Focused()
	cf := m.ContextFrame("", id)
	n.Scope, n.Ref, n.Facts = cf.Scope, cf.Subject, m.facts(it, ok)
	if r, ok := m.FocusedRow(); ok {
		n.Unread, n.Badges = r.Unread, r.Badges
		for k, v := range r.Facts {
			if _, dup := n.Facts[k]; !dup {
				n.Facts[k] = v
			}
		}
	}
	h := fnv.New64a()
	fmt.Fprintf(h, "%v|%v|%d|%d|%v|%t|%d|%d|%d", cols, rows, m.SelectedIdx, m.Offset, n.Facts["isGroupHeader"], vOK, vFrom, vTo, selCol)
	n.CacheKey = h.Sum64() | 1
	return n
}
