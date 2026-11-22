// Package picker is a reusable, filterable list picker widget. It owns the
// query, match mode, selection and marks, handles the standard motion
// commands, and renders itself as a ui.KindPicker ViewNode (or a split with a
// preview panel). It performs no I/O.
//
// picker imports only the standard library, core and ui.
package picker

import (
	"hash/fnv"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
)

// MatchMode selects how the query is matched against items.
type MatchMode string

// Match modes.
const (
	MatchSubstring MatchMode = "substring"
	MatchFuzzy     MatchMode = "fuzzy"
	MatchRegex     MatchMode = "regex"
)

// Command IDs handled by Model.HandleCommand besides the standard motions.
const (
	CmdConfirm    = "picker.confirm"
	CmdMarkToggle = "picker.mark.toggle"
	CmdModeCycle  = "picker.mode.cycle"
)

// DefaultPageSize is the page length used when Model.PageSize is zero.
const DefaultPageSize = 10

// Item is one pickable entry. Domain records never appear here; Ref and Data
// let the host map a pick back to one.
type Item struct {
	ID       string
	Title    string
	Subtitle string
	Badges   []ui.Badge
	Unread   bool
	Ref      *core.Ref
	Scope    string
	Data     any
	Keywords []string
}

// Model is the picker state.
type Model struct {
	Title       string
	Items       []Item
	Query       string
	MatchMode   MatchMode
	FilterErr   string // set while Query is an invalid regex; all items then match
	SelectedIdx int    // index into Filtered()
	MultiSelect bool
	Marked      map[string]bool
	PreviewFn   func(Item) *ui.ViewNode

	PageSize    int            // rows per page for page/half motions; 0 = DefaultPageSize
	Placeholder string         // shown in the empty prompt
	Badges      []ui.Badge     // node-level badges
	Facts       map[string]any // extra node facts, merged under the picker's own
	Scope       string         // node scope when no item is focused or it has none; default "picker"

	// Pinned, when set, is the first row of Filtered and View whatever the
	// query. It is not one of Items: it is never filtered, ranked or marked.
	Pinned *Item
	// Rank, when set, orders the matches of a non-empty substring or fuzzy
	// query: lower ranks first, ties keep Items order. It gets the
	// lower-cased query. An empty query and regex mode keep Items order.
	Rank func(it Item, lowerQuery string) int
}

// New returns a Model over items with substring matching.
func New(title string, items []Item) *Model {
	return &Model{Title: title, Items: items, MatchMode: MatchSubstring}
}

func (m *Model) mode() MatchMode {
	if m.MatchMode == "" {
		return MatchSubstring
	}
	return m.MatchMode
}

// SetItems replaces the items, drops marks of vanished items and re-clamps.
func (m *Model) SetItems(items []Item) {
	m.Items = items
	if len(m.Marked) > 0 {
		ids := map[string]bool{}
		for _, it := range items {
			ids[it.ID] = true
		}
		for id := range m.Marked {
			if !ids[id] {
				delete(m.Marked, id)
			}
		}
	}
	m.refresh()
}

// SetMatchMode changes the match mode and re-filters.
func (m *Model) SetMatchMode(mode MatchMode) {
	m.MatchMode = mode
	m.refresh()
}

// SetQuery replaces the query and resets the selection to the first match.
func (m *Model) SetQuery(q string) {
	m.Query = q
	m.SelectedIdx = 0
	m.refresh()
}

// AppendQuery appends text to the query.
func (m *Model) AppendQuery(s string) { m.SetQuery(m.Query + s) }

// BackspaceQuery removes the last rune of the query.
func (m *Model) BackspaceQuery() {
	rs := []rune(m.Query)
	if len(rs) > 0 {
		m.SetQuery(string(rs[:len(rs)-1]))
	}
}

// CycleMatchMode advances substring -> fuzzy -> regex -> substring.
func (m *Model) CycleMatchMode() {
	switch m.mode() {
	case MatchSubstring:
		m.MatchMode = MatchFuzzy
	case MatchFuzzy:
		m.MatchMode = MatchRegex
	default:
		m.MatchMode = MatchSubstring
	}
	m.SelectedIdx = 0
	m.refresh()
}

// refresh recomputes FilterErr and clamps the selection.
func (m *Model) refresh() {
	f, err := m.match()
	m.FilterErr = err
	m.clampTo(len(f) + m.pinnedRows())
}

func (m *Model) pinnedRows() int {
	if m.Pinned != nil {
		return 1
	}
	return 0
}

// rows counts the rows of Filtered without ranking them.
func (m *Model) rows() int {
	f, _ := m.match()
	return len(f) + m.pinnedRows()
}

func (m *Model) clamp() { m.clampTo(m.rows()) }

func (m *Model) clampTo(n int) {
	m.SelectedIdx = max(0, min(m.SelectedIdx, n-1))
}

func haystack(it Item) string {
	parts := append([]string{it.Title, it.Subtitle}, it.Keywords...)
	return strings.ToLower(strings.Join(parts, " "))
}

func fuzzy(hay, q string) bool {
	qs := []rune(q)
	i := 0
	for _, r := range hay {
		if i < len(qs) && r == qs[i] {
			i++
		}
	}
	return i == len(qs)
}

// match returns the matching items in Items order and a regex error message
// ("" when none). An invalid regex matches everything so the list never
// blanks mid-typing.
func (m *Model) match() ([]Item, string) {
	if m.Query == "" {
		return m.Items, ""
	}
	var match func(Item) bool
	q := strings.ToLower(m.Query)
	switch m.mode() {
	case MatchFuzzy:
		match = func(it Item) bool { return fuzzy(haystack(it), q) }
	case MatchRegex:
		re, err := regexp.Compile("(?i)" + m.Query)
		if err != nil {
			return m.Items, err.Error()
		}
		match = func(it Item) bool {
			parts := append([]string{it.Title, it.Subtitle}, it.Keywords...)
			for _, p := range parts {
				if re.MatchString(p) {
					return true
				}
			}
			return false
		}
	default:
		match = func(it Item) bool { return strings.Contains(haystack(it), q) }
	}
	var out []Item
	for _, it := range m.Items {
		if match(it) {
			out = append(out, it)
		}
	}
	return out, ""
}

// filter is match ordered by Rank when it applies.
func (m *Model) filter() ([]Item, string) {
	out, errMsg := m.match()
	if m.Rank != nil && m.Query != "" && m.mode() != MatchRegex {
		q := strings.ToLower(m.Query)
		ranked := make([]struct {
			it   Item
			rank int
		}, len(out))
		for i, it := range out {
			ranked[i].it, ranked[i].rank = it, m.Rank(it, q)
		}
		sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].rank < ranked[j].rank })
		for i := range ranked {
			out[i] = ranked[i].it
		}
	}
	return out, errMsg
}

// Filtered returns Pinned (when set) followed by the items matching the
// query, in original order or by Rank.
func (m *Model) Filtered() []Item {
	out, _ := m.filter()
	if m.Pinned != nil {
		return append([]Item{*m.Pinned}, out...)
	}
	return out
}

// Focused returns the highlighted item.
func (m *Model) Focused() (Item, bool) {
	f := m.Filtered()
	if m.SelectedIdx < 0 || m.SelectedIdx >= len(f) {
		return Item{}, false
	}
	return f[m.SelectedIdx], true
}

// ToggleMark toggles the mark of the focused item (MultiSelect only).
func (m *Model) ToggleMark() {
	it, ok := m.Focused()
	if !ok || !m.MultiSelect || (m.Pinned != nil && m.SelectedIdx == 0) {
		return
	}
	if m.Marked == nil {
		m.Marked = map[string]bool{}
	}
	if m.Marked[it.ID] {
		delete(m.Marked, it.ID)
	} else {
		m.Marked[it.ID] = true
	}
}

// MarkedItems returns the marked items in original order.
func (m *Model) MarkedItems() []Item {
	var out []Item
	for _, it := range m.Items {
		if m.Marked[it.ID] {
			out = append(out, it)
		}
	}
	return out
}

func (m *Model) page() int {
	if m.PageSize > 0 {
		return m.PageSize
	}
	return DefaultPageSize
}

func (m *Model) move(delta int) {
	m.SelectedIdx += delta
	m.clamp()
}

// HandleCommand applies a picker command. handled reports whether cmd was
// one the picker understands; picked is non-nil only for picker.confirm on a
// focused item.
func (m *Model) HandleCommand(cmd core.Command) (picked *Item, handled bool) {
	n := cmd.Times()
	switch cmd.ID {
	case "motion.down":
		m.move(n)
	case "motion.up":
		m.move(-n)
	case "motion.top":
		m.SelectedIdx = 0
		m.clamp()
	case "motion.bottom":
		n := m.rows()
		m.SelectedIdx = n - 1
		m.clampTo(n)
	case "motion.page.down":
		m.move(n * m.page())
	case "motion.page.up":
		m.move(-n * m.page())
	case "motion.half.down":
		m.move(n * max(1, m.page()/2))
	case "motion.half.up":
		m.move(-n * max(1, m.page()/2))
	case CmdConfirm:
		if it, ok := m.Focused(); ok {
			return &it, true
		}
	case CmdMarkToggle:
		m.ToggleMark()
	case CmdModeCycle:
		m.CycleMatchMode()
	default:
		return nil, false
	}
	return nil, true
}

func itoa(i int) string { return strconv.Itoa(i) }

// Hash returns a deterministic key over everything that affects the list
// rendering. It is never zero.
func (m *Model) hash(f []Item) uint64 {
	h := fnv.New64a()
	w := func(s string) { h.Write([]byte(s)); h.Write([]byte{0}) }
	w(m.Title)
	w(m.Query)
	w(string(m.mode()))
	w(m.FilterErr)
	w(itoa(m.SelectedIdx))
	w(m.Placeholder)
	w(strconv.FormatBool(m.MultiSelect))
	for _, b := range m.Badges {
		w(b.Text + "|" + b.Tone)
	}
	for _, it := range f {
		w(it.ID)
		w(it.Title)
		w(it.Subtitle)
		w(strconv.FormatBool(it.Unread))
		w(strconv.FormatBool(m.Marked[it.ID]))
		for _, b := range it.Badges {
			w(b.Text + "|" + b.Tone)
		}
	}
	if v := h.Sum64(); v != 0 {
		return v
	}
	return 1
}

func (m *Model) props(f []Item, sel int) map[string]any {
	items := make([]any, len(f))
	for i, it := range f {
		label := it.Title
		if it.Unread {
			label = "● " + label
		}
		switch {
		case m.MultiSelect && i < m.pinnedRows():
			label = "    " + label // Pinned is never markable; keep columns aligned
		case m.MultiSelect:
			if m.Marked[it.ID] {
				label = "[x] " + label
			} else {
				label = "[ ] " + label
			}
		}
		e := map[string]any{"label": label}
		if it.Subtitle != "" {
			e["desc"] = it.Subtitle
		}
		if len(it.Badges) > 0 {
			var bs []string
			for _, b := range it.Badges {
				bs = append(bs, b.Text)
			}
			e["badge"] = strings.Join(bs, " ")
			if it.Badges[0].Tone != "" {
				e["tone"] = it.Badges[0].Tone
			}
		}
		items[i] = e
	}
	p := map[string]any{
		"items":     items,
		"query":     m.Query,
		"matchMode": string(m.mode()),
		"selected":  sel,
	}
	if m.Placeholder != "" {
		p["placeholder"] = m.Placeholder
	}
	return p
}

// View renders the picker as a focusable ui.KindPicker node with ID id. When
// PreviewFn returns a node for the focused item, the result is a split whose
// first child is the picker (still ID id) and whose second is a preview panel.
func (m *Model) View(id string) ui.ViewNode {
	f := m.Filtered()
	m.clampTo(len(f))
	sel := -1
	if len(f) > 0 {
		sel = min(max(m.SelectedIdx, 0), len(f)-1)
	}
	scope := m.Scope
	if scope == "" {
		scope = "picker"
	}
	var ref *core.Ref
	var focused *Item
	if sel >= 0 {
		it := f[sel]
		focused = &it
		if it.Scope != "" {
			scope = it.Scope
		}
		if it.Ref != nil {
			r := *it.Ref
			ref = &r
		}
	}
	facts := map[string]any{}
	for k, v := range m.Facts {
		facts[k] = v
	}
	facts["query"] = m.Query
	facts["matchMode"] = string(m.mode())
	facts["count"] = len(f) - m.pinnedRows() // matching Items; Pinned is not counted
	facts["total"] = len(m.Items)            // all Items; Pinned is not one
	facts["pinned"] = m.Pinned != nil
	facts["selected"] = sel
	facts["multiSelect"] = m.MultiSelect
	facts["marked"] = len(m.MarkedItems())
	facts["filterError"] = m.FilterErr
	if focused != nil {
		facts["focusedId"] = focused.ID
	}
	list := ui.ViewNode{
		ID: id, Kind: ui.KindPicker, Scope: scope, Ref: ref, Facts: facts,
		Focusable: true, Title: m.Title, Badges: m.Badges,
		CacheKey: m.hash(f), Props: m.props(f, sel),
	}
	if focused != nil && m.PreviewFn != nil {
		if pv := m.PreviewFn(*focused); pv != nil {
			return ui.ViewNode{
				ID: id + ".split", Kind: ui.KindSplit, Ratio: 0.5, Gap: 1,
				Children: []ui.ViewNode{list, {
					ID: id + ".preview", Kind: ui.KindPanel, Title: focused.Title,
					Children: []ui.ViewNode{*pv},
				}},
			}
		}
	}
	return list
}
