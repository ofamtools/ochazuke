// Package picker is a reusable, filterable list picker widget. It owns the
// query, match mode, selection and marks, handles the standard motion
// commands, and renders itself as a ui.KindPicker ViewNode (or a split with a
// preview panel). It performs no I/O.
//
// picker imports only the standard library, core and ui.
package picker

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/traits"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/input"
)

// MatchMode selects how the query is matched against items.
type MatchMode = traits.MatchMode

// Match modes.
const (
	MatchSubstring = traits.MatchSubstring
	MatchFuzzy     = traits.MatchFuzzy
	MatchRegex     = traits.MatchRegex
)

// Command IDs handled by Model.HandleCommand besides the standard motions.
const (
	CmdConfirm     = "picker.confirm"
	CmdMarkToggle  = "picker.mark.toggle"
	CmdModeCycle   = "picker.mode.cycle"
	CmdFilterStart = "picker.filter.start"
	CmdFilterStop  = "picker.filter.stop"
	CmdFilterClear = "picker.filter.clear"
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
	Filtering   bool // true while editing the filter query; false in normal mode
	MatchMode   MatchMode
	FilterErr   string // set while Query is an invalid regex; all items then match
	SelectedIdx int    // index into Filtered()
	Offset      int    // first visible row index
	ScrollOff   int    // vertical context rows kept around selection
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

	line input.Line
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

// SetQuery replaces the query, puts the cursor at the end, and resets selection.
func (m *Model) SetQuery(q string) {
	m.Query = q
	m.line.Set(q)
	m.SelectedIdx = 0
	m.Offset = 0
	m.refresh()
}

// QueryCursor returns the rune offset of the query cursor.
func (m *Model) QueryCursor() int {
	m.line.Sync(m.Query)
	return m.line.Cursor
}

// AppendQuery inserts text at the query cursor.
func (m *Model) AppendQuery(s string) {
	m.line.Sync(m.Query)
	if m.line.Insert(s) {
		m.Query = m.line.Value()
		m.SelectedIdx = 0
		m.Offset = 0
		m.refresh()
	}
}

// BackspaceQuery removes the rune before the query cursor.
func (m *Model) BackspaceQuery() {
	m.line.Sync(m.Query)
	if changed, _ := m.line.HandleCommand(core.Command{ID: input.CmdBackspace}); changed {
		m.Query = m.line.Value()
		m.SelectedIdx = 0
		m.Offset = 0
		m.refresh()
	}
}

// CycleMatchMode advances substring -> fuzzy -> regex -> substring.
func (m *Model) CycleMatchMode() {
	m.MatchMode = traits.CycleMatchMode(m.MatchMode, traits.FilterConfig{})
	m.SelectedIdx = 0
	m.Offset = 0
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

func (m *Model) viewportState(n int) traits.ViewportState {
	return traits.ViewportState{Cursor: m.SelectedIdx, Offset: m.Offset, Total: n, Height: m.page()}
}

func (m *Model) viewportConfig() traits.ViewportConfig {
	return traits.ViewportConfig{ScrollOff: m.ScrollOff}
}

func (m *Model) clamp() { m.clampTo(m.rows()) }

func (m *Model) clampTo(n int) {
	st := traits.ViewportClamp(m.viewportState(n), m.viewportConfig())
	m.SelectedIdx, m.Offset = st.Cursor, st.Offset
}

func haystack(it Item) string {
	parts := append([]string{it.Title, it.Subtitle}, it.Keywords...)
	return strings.Join(parts, " ")
}

// match returns the matching items in Items order and a regex error message
// ("" when none). An invalid regex matches everything so the list never
// blanks mid-typing.
func (m *Model) match() ([]Item, string) {
	if m.Query == "" {
		return m.Items, ""
	}
	mode := m.mode()
	matcher, errMsg := traits.CompileFilter(m.Query, mode, traits.FilterConfig{})
	if errMsg != "" {
		return m.Items, errMsg
	}
	var out []Item
	for _, it := range m.Items {
		if mode == MatchRegex {
			parts := append([]string{it.Title, it.Subtitle}, it.Keywords...)
			if matcher.MatchAny(parts...) {
				out = append(out, it)
			}
		} else if matcher.Match(haystack(it)) {
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

// HandleCommand applies a picker command. handled reports whether cmd was
// one the picker understands; picked is non-nil only for picker.confirm on a
// focused item (or the first marked item in MultiSelect mode when no item is
// focused).
func (m *Model) HandleCommand(cmd core.Command) (picked *Item, handled bool) {
	if next, _, ok := traits.ViewportStep(m.viewportState(m.rows()), m.viewportConfig(), cmd); ok {
		m.SelectedIdx, m.Offset = next.Cursor, next.Offset
		return nil, true
	}
	switch cmd.ID {
	case CmdConfirm:
		if it, ok := m.Focused(); ok {
			return &it, true
		}
		if m.MultiSelect {
			if marked := m.MarkedItems(); len(marked) > 0 {
				it := marked[0]
				return &it, true
			}
		}
	case CmdMarkToggle:
		m.ToggleMark()
	case CmdModeCycle:
		m.CycleMatchMode()
	case CmdFilterStart:
		m.Filtering = true
	case CmdFilterStop:
		m.Filtering = false
	case CmdFilterClear:
		m.SetQuery("")
		m.Filtering = false
	default:
		if cmd.ID == input.CmdInsert || input.IsReadlineCommand(cmd.ID) {
			m.line.Sync(m.Query)
			if changed, _ := m.line.HandleCommand(cmd); changed {
				m.Query = m.line.Value()
				m.SelectedIdx = 0
				m.Offset = 0
				m.refresh()
			}
			return nil, true
		}
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
	w(strconv.FormatBool(m.Filtering))
	w(itoa(m.QueryCursor()))
	w(string(m.mode()))
	w(m.FilterErr)
	w(itoa(m.SelectedIdx))
	w(itoa(m.Offset))
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
		"cursor":    m.QueryCursor(),
		"matchMode": string(m.mode()),
		"selected":  sel,
		"filtering": m.Filtering,
	}
	if m.Offset > 0 {
		p["offset"] = m.Offset
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
	facts := traits.MergeFacts(nil, m.Facts)
	facts = traits.MergeFacts(facts, traits.ViewportFacts(m.viewportState(len(f)), m.viewportConfig()))
	facts = traits.MergeFacts(facts, traits.FilterFacts(traits.FilterState{
		Query: m.Query, Cursor: m.QueryCursor(), Active: m.Filtering, Mode: m.mode(), Err: m.FilterErr,
	}, traits.FilterConfig{}))
	facts["count"] = len(f) - m.pinnedRows() // matching Items; Pinned is not counted
	facts["total"] = len(m.Items)            // all Items; Pinned is not one
	facts["pinned"] = m.Pinned != nil
	facts["selected"] = sel
	facts["multiSelect"] = m.MultiSelect
	facts["marked"] = len(m.MarkedItems())
	if focused != nil {
		facts["focusedId"] = focused.ID
	}
	list := ui.ViewNode{
		ID: id, Kind: ui.KindPicker, Scope: scope,
		Traits: []string{traits.TraitNav, traits.TraitScroll, traits.TraitFilter},
		Ref:    ref, Facts: facts,
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
