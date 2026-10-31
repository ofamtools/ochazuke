package ochazuke

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ListItem is application-owned identity, display text, and the registered
// noun the row answers to. Each row is an Instance whose ID is the item ID and
// whose Parent is the list; its bindings and actions come from its noun.
// Data is an opaque value the application keeps on the row (a host's
// record of the row, an extension's own id); SetItems copies it to the
// row instance's Data, where a verb reads it as Ctx.Instance.Data. The
// framework never reads it. Cells are the row's text per column when the
// list has columns (a table, Columns); Label stays the row's name for the
// palette, search and {{label}}, and is the first cell of a row without
// cells. Section groups a table's rows: List.View draws the section as a
// label line above the first row of each run of rows with the same
// Section (a list without columns ignores it). Tones give cells, or runs
// inside them, a named tone the theme colours (ToneSpan, tone.go; in a
// list without columns cell 0 is the label); they never change the text.
type ListItem struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Noun  string `json:"noun"`
	Data  any    `json:"data,omitempty"`
	// Props supplies the row's noun prop values when Data is an opaque handle.
	// Nil reads props from Data via ThingOf instead.
	Props   Thing      `json:"props,omitempty"`
	Cells   []string   `json:"cells,omitempty"`
	Section string     `json:"section,omitempty"`
	Tones   []ToneSpan `json:"tones,omitempty"`
	// Classes are application-defined labels for list.next-class and previous-class.
	Classes []string `json:"classes,omitempty"`
	// Search is more text the / filter matches the row by, beside its
	// label and cells, never drawn: what the row stands for (the Settings
	// page's section row holds its settings' names, descriptions, values,
	// defaults and choices).
	Search []string `json:"search,omitempty"`
}

// List owns flat selection, marks and scrolling on the runtime's event loop.
// Focus sits on the selected row when there is one, else on the list.
type List struct {
	r           *Runtime
	id          string
	all         []ListItem // every row, as SetItems gave them
	headerTones []ToneSpan // the header row's tones (SetHeaderTones)
	items       []ListItem // the rows shown: all, or the ones the filter lets through (list_filter.go)
	linger      filterLinger
	query       string       // the filter query; "" shows every row
	propFilters []PropFilter // typed prop conditions, ANDed with the text query
	savedFilter string       // the applied saved filter's name; empty after an ad-hoc change
	rowCache    map[string]*filterRow
	derived     fieldProps // the rows' undeclared fields as props (list_props.go); nil until asked, cleared with the rows
	nouns       []string
	match       string // the filter's match mode set by search.cycle-match/set-match; "" is search.match (search_match.go)
	back        string // the row selected when the filter line opened, which filter-clear selects again
	selected    int
	offset      int
	marks       map[string]bool
	noSpill     bool            // NoSpill: list.move clamps at the edges instead of spilling
	toSelected  bool            // SpillToSelection: a spill into the list lands on its selected row
	noMarks     bool            // NoMarks: a read-only view, its rows take no marks
	choose      bool            // ClearFilterOnEnter: enter in the filter line chooses the match and clears the filter
	columns     []Column        // Columns: a table; none draws the labels
	widest      []int           // a table's widest cell per column over the items (cellWidths); nil after SetItems
	window      int             // rows the last frame showed (list_motions.go's page); 0 before any frame
	windowed    bool            // the last frame windowed the rows (more rows than fit)
	colOffset   int             // a table's column offset (table_columns.go); 0 unscrolled
	colWidth    int             // the width the last frame laid the table's columns out at
	cellSelect  bool            // CellSelect: a table with a current column (table_cells.go)
	col         int             // a cell-mode table's current column
	colWidths   map[int]int     // widths given with z > and z < (table_cells.go)
	colHeader   bool            // the last frame drew the table's header (edge markers need it)
	edges       colEdges        // the edge markers the last frame drew (table_columns.go)
	pins        []int           // the user's pinned columns (table_pins.go), when pinsSet
	pinsSet     bool            // pins holds a choice; else the default pins
	restored    bool            // the remembered TableState was read
	drawn       []int           // the columns the last frame showed
	drawnWidths []int           // and their widths
	visual      bool            // V: a visual row selection over the marks (list_visual.go)
	anchor      string          // the row V started on
	base        map[string]bool // the marks before V
	applying    bool            // list.apply is running its rows' requests: they are not replaced again (visualApply)
	part        string          // an instance under the list that holds its focus as a row does (a timeline's body, timeline.go)

	// A detached list filter controller can serve another row view. Its
	// callback reapplies that view's text matching without list focus/motions.
	filterChanged func()
	filterFocus   string
	// onFilter, when set (OnFilter), hears every change of the text
	// filter: a host that draws something by the list's matches follows it.
	onFilter func(*List)
	// matchNoun names the rows in the empty line of a filter that lets
	// none through (SetMatchNoun): "no settings match 'path'".
	matchNoun string

	// motion is a component's own j/k on the rows (a timeline's cards
	// scroll by the half-page rule, R3.14); nil moves the list's way.
	motion func(delta, count int)
	// motionAtEdge reports whether motion(delta) would only spill (the
	// component shows all there is on that side); nil: the selection's edge.
	motionAtEdge func(delta int) bool
	// undoGroup, when a component under the list keeps an undo stack (a
	// tree's), starts one undo step for a run over several rows; its end
	// seals the step (Runtime.undoGroup).
	undoGroup func() (end func())
}

// NewList is AddList for callers that hold the runtime as a value.
func NewList(r *Runtime, id string, items []ListItem, opts ...InstanceOption) (*List, error) {
	return r.AddList(id, items, opts...)
}

// AddList registers a list instance of the list noun with its rows. Options:
// Parent(id) (default app), Kind(noun) for a list that answers to its
// own noun as well, NoSpill() for a list whose motions stay inside it,
// SpillToSelection() for one a spill lands in on its selected row,
// NoMarks() for a read-only view, ClearFilterOnEnter() for a list whose
// filter enter chooses a row and clears, Columns(...) for a table, and
// CellSelect() for a table with a current column (table_cells.go).
func (r *Runtime) AddList(id string, items []ListItem, opts ...InstanceOption) (*List, error) {
	if id == "" {
		return nil, errors.New("list ID must be nonempty")
	}
	if r.Instances[id] != nil {
		return nil, fmt.Errorf("list %q is already registered", id)
	}
	spec := instanceSpec{parent: "app", noun: "ochazuke.list"}
	for _, opt := range opts {
		opt(&spec)
	}
	if r.Instances[spec.parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", spec.parent)
	}
	list := &List{r: r, id: id, selected: -1, marks: map[string]bool{}, noSpill: spec.noSpill, toSelected: spec.spillToSelection, noMarks: spec.noMarks, choose: spec.clearFilterOnEnter, columns: slices.Clone(spec.columns), cellSelect: spec.cellSelect}
	r.Instance(id, Parent(spec.parent), Kind(spec.noun), FocusOnClose(spec.closeFocus))
	r.Instances[id].Component = list
	if err := list.SetItems(items); err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	return list, nil
}

// SetItems copies rows, preserves selection by ID, and falls back to the nearest
// remaining index if that ID disappears. Invalid updates leave the list unchanged.
// A filter (SetFilter) stays and applies to the new rows.
// It creates one instance per new row under the list and removes the instances
// of rows that disappear; a retained row keeps its instance and any bindings
// the application put on it. Rows whose cells are unchanged keep a table's
// measured widths. A record-level change is Patch.
func (l *List) SetItems(items []ListItem) error {
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		if item.ID == "" || ids[item.ID] {
			return fmt.Errorf("list item ID %q must be nonempty and unique", item.ID)
		}
		if l.r.Noun(item.Noun) == nil {
			return fmt.Errorf("list item %q: unknown noun %q", item.ID, item.Noun)
		}
		if owner := l.r.rows[item.ID]; owner != l.id && l.r.Instances[item.ID] != nil {
			return fmt.Errorf("list item ID %q is already another instance", item.ID)
		}
		ids[item.ID] = true
	}
	gone := map[string]bool{}
	var drop []string
	for _, old := range l.all {
		if !ids[old.ID] && !l.treeHasNode(old.ID) {
			gone[old.ID] = true
			delete(l.marks, old.ID)
			delete(l.base, old.ID)
			drop = append(drop, old.ID)
		}
	}
	if t := l.tree(); t != nil {
		for id, owner := range l.r.rows {
			if owner == l.id && !ids[id] && t.index(id) < 0 && !gone[id] {
				gone[id] = true
				delete(l.marks, id)
				delete(l.base, id)
				drop = append(drop, id)
			}
		}
	}
	l.r.removeInstances(drop)
	for _, item := range items {
		l.r.rows[item.ID] = l.id
		inst := l.r.Instances[item.ID]
		if inst == nil {
			inst = l.r.Instance(item.ID, Parent(l.id), Kind(item.Noun))
		}
		inst.Noun, inst.Parent, inst.Label, inst.Data = l.r.CanonicalNoun(item.Noun), l.id, item.Label, item.Data
	}
	l.r.noteRows(l.id, items)
	previous, _ := l.Selected()
	at := l.selected
	if !sameCells(l.all, items) {
		l.widest = nil // a table remeasures; rows drawing the same cells keep the widths
	}
	l.all = cloneItems(items)
	l.cacheRows()
	// The filter applies to the new rows; the selection stays on its row
	// when it still shows, else on the shown row nearest its old index.
	l.refilter(previous.ID)
	if (l.tree() == nil || !l.treeHasNode(previous.ID)) && (l.selected >= 0 && l.items[l.selected].ID != previous.ID || l.selected < 0 && len(l.items) > 0) {
		l.selected = min(max(at, 0), len(l.items)-1)
	}
	l.syncVisual()
	l.syncFocus(previous, gone)
	return nil
}

// Items returns a copy of every row in display order, the ones a filter
// hides included. Cells and Tones are copies too, so editing a returned
// row in place changes nothing until SetItems, which re-measures it;
// Data is the application's value, shared as given.
func (l *List) Items() []ListItem { return cloneItems(l.all) }

// cloneItems copies rows with their own Cells, Tones and Classes (one backing
// array each for all cells and tones): the list and its callers never share a slice, so
// SetItems's same-cells test (sameCells) always compares what was drawn
// with what is given.
func cloneItems(items []ListItem) []ListItem {
	out := slices.Clone(items)
	cells, tones := 0, 0
	for _, it := range items {
		cells += len(it.Cells)
		tones += len(it.Tones)
	}
	cellBuf, toneBuf := make([]string, 0, cells), make([]ToneSpan, 0, tones)
	for i, it := range items {
		out[i].Props = maps.Clone(it.Props)
		out[i].Classes = slices.Clone(it.Classes)
		out[i].Search = slices.Clone(it.Search)
		if it.Cells != nil {
			n := len(cellBuf)
			cellBuf = append(cellBuf, it.Cells...)
			out[i].Cells = cellBuf[n:len(cellBuf):len(cellBuf)]
		}
		if it.Tones != nil {
			n := len(toneBuf)
			toneBuf = append(toneBuf, it.Tones...)
			out[i].Tones = toneBuf[n:len(toneBuf):len(toneBuf)]
		}
	}
	return out
}

// cloneItem is one row with its own Cells, Tones and Classes.
func cloneItem(it ListItem) ListItem {
	it.Cells, it.Tones = slices.Clone(it.Cells), slices.Clone(it.Tones)
	it.Classes, it.Search = slices.Clone(it.Classes), slices.Clone(it.Search)
	it.Props = maps.Clone(it.Props)
	return it
}

// Len is the number of rows, the ones a filter hides included (Shown
// counts the rest).
func (l *List) Len() int { return len(l.all) }

// Selected returns a value copy; empty lists have no selected item.
func (l *List) Selected() (ListItem, bool) {
	if l.selected < 0 {
		return ListItem{}, false
	}
	return cloneItem(l.items[l.selected]), true
}

// Move clamps at either end and does not take focus from another component.
func (l *List) Move(delta int) {
	if l.selected < 0 {
		return
	}
	next := l.selected + min(max(delta, -l.selected), len(l.items)-1-l.selected)
	if next != l.selected {
		previous := l.items[l.selected]
		l.selected = next
		l.syncVisual()
		l.syncFocus(previous, nil)
	}
}

// spill is list.move past an edge of a focused list on a page (a list in
// a visual selection clamps, so j and k only extend it): delta beyond
// the last row moves the focus to the next stop in drawing order (a list on
// its first row), before the first row to the previous stop (a list on its
// last row); other stops land as a focus jump does. It reports false, and
// the move clamps, for a list that opted out (NoSpill), a list not holding
// the focus, one in a modal, and at the page's first and last stop: motions
// never wrap.
func (l *List) spill(delta int) bool {
	inside := l.selected >= 0 && l.selected+delta >= 0 && l.selected+delta < len(l.items)
	if delta == 0 || inside || l.trappedAt(rowEdge(delta)) || l.visual || !l.Focused() {
		return false
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	next, ok := l.neighbour(step)
	if !ok {
		return false
	}
	if other := l.r.List(next); other != nil && other.Shown() > 0 && !(other.toSelected && other.selected >= 0) {
		row := 0
		if step < 0 {
			row = other.Shown() - 1
		}
		other.Move(row - other.selected)
	}
	l.r.setFocus(l.r.landing(next))
	return true
}

// ListNoSpillSetting is list.no-spill: the ids of lists, tables, trees
// and timelines whose motions clamp at their edges as NoSpill() does (the
// per-user opt-out from the spill, UX-RULES R6.12). An entry ending in *
// is a prefix; an entry may end in one edge (ID:last, ID:first,
// ID:first-col, ID:last-col; SpillEdges), else it traps all four.
const ListNoSpillSetting = "list.no-spill"

// trappedAt reports whether the list's motions stay inside it at edge
// instead of spilling to the neighbouring stop: a list made with
// NoSpill() (every edge), or one the list.no-spill setting names at that
// edge or at every edge (its own id, or the id of the tree or timeline it
// holds the rows of). Read on every motion, so a settings change applies
// at once. Every spill path (j/k past an edge, h/l at a table's column
// edge, a timeline's last step) and the availability of the motions at a
// trapped edge (list-moves, list-column-moves, timeline-step-moves) ask
// it.
func (l *List) trappedAt(edge SpillEdge) bool {
	if l.noSpill {
		return true
	}
	ids := []string{l.id}
	if inst := l.r.Instances[l.id]; inst != nil && inst.Parent != "" {
		if owner := l.r.Instances[inst.Parent]; owner != nil {
			switch owner.Component.(type) {
			case *Tree, *Timeline:
				ids = append(ids, owner.ID)
			}
		}
	}
	return l.r.noSpillNames(edge, ids...)
}

// noSpillNames reports whether the list.no-spill setting names one of ids
// at edge: an entry is an instance id or a prefix ending in * (panes/*),
// with an optional edge suffix (ParseNoSpillEntry).
func (r *Runtime) noSpillNames(edge SpillEdge, ids ...string) bool {
	v, _ := r.settingValue(ListNoSpillSetting)
	names, _ := v.([]string)
	for _, entry := range names {
		name, at := ParseNoSpillEntry(entry)
		if name == "" || at != "" && at != edge {
			continue
		}
		for _, id := range ids {
			if noSpillMatches(name, id) {
				return true
			}
		}
	}
	return false
}

// atRowEdge reports whether a list.move of delta would do nothing but
// spill: the selection is on the edge delta leaves by, and (for a list
// whose motion is a component's own, a timeline's cards) the component
// has nothing left to show on that side.
func (l *List) atRowEdge(delta int) bool {
	if delta == 0 || l.selected < 0 {
		return false
	}
	if l.motion != nil && !l.visual && l.motionAtEdge != nil {
		return l.motionAtEdge(delta)
	}
	next := l.selected + delta
	return next < 0 || next >= len(l.items)
}

// neighbour is the stop after (step 1) or before (step -1) the list in
// its page's drawing order; false in a modal, off a page, and past the
// page's first or last stop (motions never wrap). j and k spill into it
// (spill); h and l at a table's column edge move the focus to it
// (table-spills-left/right, table_columns.go).
func (l *List) neighbour(step int) (string, bool) {
	root := l.r.focusRoot()
	if l.r.PageOf(root) == nil {
		return "", false
	}
	ring, _ := l.r.units(root, l.r.focusable)
	i := slices.Index(ring, l.id)
	if i < 0 || i+step < 0 || i+step >= len(ring) {
		return "", false
	}
	return ring[i+step], true
}

// Select changes the selected row by stable ID without taking focus. A
// row the filter hides is shown by clearing the filter.
func (l *List) Select(id string) error {
	l.reveal(id)
	for i, item := range l.items {
		if item.ID == id {
			l.Move(i - l.selected)
			return nil
		}
	}
	return fmt.Errorf("unknown list item %q", id)
}

// selectID selects the row with id without touching the focus: the
// runtime calls it when the focus lands on the row (a row the filter
// hides clears the filter). An unknown id is ignored.
func (l *List) selectID(id string) {
	at := func(item ListItem) bool { return item.ID == id }
	i := slices.IndexFunc(l.items, at)
	if i < 0 && (l.query != "" || len(l.propFilters) > 0) {
		l.reveal(id)
		i = slices.IndexFunc(l.items, at)
	}
	if i >= 0 {
		l.selected = i
		l.syncVisual()
	}
}

// ToggleMark marks or unmarks a row for fan-out verbs; a list added with
// NoMarks refuses.
func (l *List) ToggleMark(id string) error {
	if l.noMarks {
		return fmt.Errorf("list %s takes no marks", l.id)
	}
	if !slices.ContainsFunc(l.all, func(item ListItem) bool { return item.ID == id }) {
		return fmt.Errorf("unknown list item %q", id)
	}
	marks := l.marks
	if l.visual {
		marks = l.base // the range is drawn over the marks; space edits the marks beneath it
	}
	if marks[id] {
		delete(marks, id)
	} else {
		marks[id] = true
	}
	l.syncVisual()
	return nil
}

// Marked lists the marked rows in list order, the ones a filter hides
// included (a mark survives the filter).
func (l *List) Marked() []ListItem { return l.markedIn(l.all) }

// markedShown lists the marked rows the filter shows: what list.apply
// fans out to, so a key acts on the rows on screen.
func (l *List) markedShown() []ListItem { return l.markedIn(l.items) }

func (l *List) markedIn(rows []ListItem) []ListItem {
	if len(l.marks) == 0 {
		return nil
	}
	var out []ListItem
	for _, item := range rows {
		if l.marks[item.ID] {
			out = append(out, cloneItem(item))
		}
	}
	return out
}

// Focus points the active frame at the list's selected row, or the list.
func (l *List) Focus() error { return l.r.SetFocus(l.focusTarget()) }

// Focused derives focus from the active frame instead of retaining a stale flag.
func (l *List) Focused() bool { return l.holds(l.r.Focus) }

func (l *List) holds(focus string) bool {
	return focus == l.id || l.r.rows[focus] == l.id || l.part != "" && focus == l.part
}

// FocusTarget is where a focus jump to the list lands: its selected row.
func (l *List) FocusTarget() string { return l.focusTarget() }

func (l *List) focusTarget() string {
	if l.selected >= 0 {
		return l.items[l.selected].ID
	}
	return l.id
}

// syncFocus moves every focus reference that pointed at this list or one of
// its rows to the current row, so hidden pages and modal returns restore
// the live selection rather than a removed row's bindings.
func (l *List) syncFocus(previous ListItem, gone map[string]bool) {
	current, _ := l.Selected()
	if l.Focused() && (previous.ID != current.ID || previous.Noun != current.Noun) {
		l.r.CancelPendingBinding()
	}
	// The list's part (a timeline's body) belongs to the selected row: it
	// keeps the focus while that row stays selected, and gives it to the
	// new selection when the selection moves or the row goes.
	stays := previous.ID != "" && previous.ID == current.ID && !gone[previous.ID]
	l.r.RetargetFocus(func(id string) bool {
		if stays && l.part != "" && id == l.part {
			return false
		}
		return l.holds(id) || gone[id] || id == previous.ID && previous.ID != ""
	}, l.focusTarget())
}

// Snapshot is the list's state with a copy of its rows (the snapshot's
// /lists entry); changing it never reaches the list.
func (l *List) Snapshot() map[string]any {
	out := l.state()
	out["items"] = slices.Clone(l.items)
	return out
}

// The state's items are the rows shown (selected indexes them); with a
// filter, "filter" is its query and "total" counts every row.

// state is Snapshot without the copy, the result of the list verbs (a
// move, a select, a mark): its items share the list's rows, which SetItems
// replaces whole and never edits in place, so a keypress on a list of many
// rows copies none of them. A verb's result is read, never written: the
// CLI and the API encode it, a key drops it.
func (l *List) state() map[string]any {
	selected, _ := l.Selected()
	marked := make([]string, 0, len(l.marks))
	if len(l.marks) > 0 {
		for _, item := range l.Marked() {
			marked = append(marked, item.ID)
		}
	}
	out := map[string]any{"id": l.id, "items": slices.Clip(l.items), "selected": l.selected, "selectedId": selected.ID, "offset": l.offset, "focused": l.Focused(), "marked": marked}
	if l.query != "" || len(l.propFilters) > 0 || l.savedFilter != "" {
		out["filter"], out["total"] = l.query, len(l.all)
		if len(l.propFilters) > 0 {
			out["propFilters"] = l.PropFilters()
		}
		if l.savedFilter != "" {
			out["savedFilter"] = l.savedFilter
		}
	}
	if len(l.columns) > 0 {
		out["columns"] = slices.Clone(l.columns)
		out["columnOffset"] = l.colOffset
		if l.cellMode() {
			out["column"] = l.Column()
		}
		if len(l.colWidths) > 0 {
			out["columnWidths"] = maps.Clone(l.colWidths)
		}
	}
	if l.visual {
		out["visual"] = true
	}
	return out
}

// View fits the viewport and updates its scroll offset to keep selection
// visible; a list with columns draws as a table (tableView). Calling it places the list in its page's drawing order (Placed),
// even at zero height, and reports its selected row's line (PlacedAt) so
// the page scrolls to keep that row on screen.
func (l *List) View(width, height int) string {
	if t := l.tree(); t != nil && t.guidesShown != t.guidesEnabled() {
		t.guideRows()
	}
	l.r.Placed(l.id)
	if width <= 0 || height <= 0 {
		return ""
	}
	// The filter line, while typed or kept, takes the list's first line.
	line := ""
	if height > 1 {
		if line = l.filterLine(width); line != "" {
			height--
		}
	}
	var text string
	var focus, focusHeight int
	switch {
	case len(l.columns) > 0:
		text, focus, focusHeight = l.tableView(width, height)
	default:
		text, focus, focusHeight = l.rowsView(width, height)
	}
	if line != "" {
		text = line + "\n" + text
		if focusHeight == height {
			focusHeight++ // the whole list
		} else {
			focus++
		}
	}
	if l.faded() {
		if styles := l.r.Settings().page.styles; styles != nil {
			text = dim(text, styles().Unfocused)
		}
	}
	return l.r.PlacedAt(l.id, text, focus, focusHeight)
}

// marksSelection reports whether the selected row is drawn highlighted:
// always while the list has the focus, and on a page list without it too
// (dimmed with the list). A list in a modal keeps the older rule, plain
// until focused, so a modal's secondary list never shows a second cursor.
func (l *List) marksSelection() bool { return l.Focused() || l.onPage() }

// faded reports whether View fades the list (dim-unfocused): a page list
// without the focus, unless it is a tree's list and the focus is the line
// the tree is editing, which is typing into this list.
func (l *List) faded() bool {
	if l.Focused() || !l.onPage() {
		return false
	}
	if inst := l.r.Instances[l.id]; inst != nil {
		if t := l.r.Tree(inst.Parent); t != nil && t.list == l && t.editing != "" && t.input != nil && l.r.Focus == t.input.ID {
			return false
		}
	}
	return true
}

// onPage reports whether the list sits on a page rather than in a modal:
// the first page or open modal among its ancestors decides.
func (l *List) onPage() bool {
	for id := l.id; id != ""; {
		if slices.Contains(l.r.modals, id) {
			return false
		}
		inst := l.r.Instances[id]
		if inst == nil {
			return false
		}
		if _, ok := inst.Component.(*Page); ok {
			return true
		}
		id = inst.Parent
	}
	return false
}

// rowsView is View for a list without columns: the rows in the list's
// window and the "x–y / n" line when they overflow, with the line of the
// selected row (else 0 and the whole height) the page keeps on screen.
func (l *List) rowsView(width, height int) (string, int, int) {
	if len(l.items) == 0 {
		return strings.Join(layoutRows(l.emptyLine(), width, height), "\n"), 0, height
	}
	visible := height
	if len(l.items) > height && height > 1 {
		visible--
	}
	l.offset = windowOffset(l.offset, l.selected, visible, len(l.items), l.scrolloff())
	end := min(len(l.items), l.offset+visible)
	l.window, l.windowed = visible, len(l.items) > visible
	rows := make([]string, 0, height)
	// The selected row is the inverted highlight alone, no cursor marker. A
	// page list keeps it while unfocused, where View fades the whole list
	// (dim-unfocused), so the focused row is the one at full strength. Marked
	// rows take a "* " gutter, which every row gets while the list has a mark.
	marked, gutter := l.marksSelection(), l.hasGutter()
	toned, faded := l.listTones(), l.faded()
	for i := l.offset; i < end; i++ {
		text := modalLabel(l.items[i].Label)
		label := text
		if gutter {
			label = l.rowGutter(l.items[i].ID) + label
		}
		row := ansi.Truncate(label, width, "…")
		selected := i == l.selected && marked
		if segs := l.labelSegs(toned, l.items[i], text, ansi.StringWidth(row), gutter); len(segs) > 0 {
			rows = append(rows, paintLine(row, ansi.StringWidth(row), segs, toned, selected, faded, span{}))
			continue
		}
		if selected {
			row = lipgloss.NewStyle().Reverse(true).Render(row)
		}
		rows = append(rows, row)
	}
	if len(l.items) > height && height > 1 {
		rows = append(rows, fmt.Sprintf("%d–%d / %d", l.offset+1, end, len(l.items)))
	}
	// The page keeps the selected row's line on screen; the list has
	// already kept the row inside its own window above.
	focus, focusHeight := 0, height
	if l.selected >= l.offset && l.selected < end {
		focus, focusHeight = l.selected-l.offset, 1
	}
	return strings.Join(layoutRows(strings.Join(rows, "\n"), width, height), "\n"), focus, focusHeight
}

// labelSegs places a plain list row's cell-0 tones on its label, after the
// mark gutter; none without styles to draw them.
func (l *List) labelSegs(toned *Styles, item ListItem, text string, drawn int, gutter bool) []toneSeg {
	if toned == nil {
		return nil
	}
	x := 0
	if gutter {
		x = 2
	}
	return cellSegs(spansOf(l.rowTones(item), 0), text, x, drawn-x)
}

// emptyLine is what an empty list draws: "(empty list)", in reverse video
// while the list has the focus, so a spill into it shows where the next key
// lands (one thing stands out, UX review 2 F5).
func (l *List) emptyLine() string {
	text := "(empty list)"
	if len(l.items) == 0 && len(l.all) > 0 && l.matchNoun != "" && strings.TrimSpace(l.query) != "" {
		text = fmt.Sprintf("no %s match '%s'", l.matchNoun, l.query)
	} else if len(l.items) == 0 && l.Filtering() && !l.r.Settings().Bool("list.filter-strip") {
		text = fmt.Sprintf("0 of %d (filtered)", len(l.all))
	} else if len(l.all) > 0 {
		text = "(no matches)" // every row is filtered out
	}
	if l.Focused() {
		return lipgloss.NewStyle().Reverse(true).Render(text)
	}
	return text
}

// scrolloff is the margin the list keeps around its selected row inside
// its own window: the open page's (Page.Scrolloff), else the default 2.
func (l *List) scrolloff() int {
	if page := l.r.PageOf(l.r.Page); page != nil {
		return page.scrolloff()
	}
	return 2
}

// windowOffset is the first row a visible-row window over total rows shows
// so the selected row keeps margin rows above and below it, the same rule
// as the page's follow (scrollToShow): the margin shrinks to (visible-1)/2
// in a short window and relaxes at the list's first and last rows.
func windowOffset(offset, selected, visible, total, margin int) int {
	return scrollToShow(offset, selected, selected+1, visible, total, margin)
}

func registerLists(r *Runtime) {
	// j/k take a count (3j): the list's count_pending extends its normal
	// table (bindings/list.json), so its rows answer with the count in the
	// payload.
	r.Define("ochazuke.list").Focusable()
	// list.mark is guarded off on a list added with NoMarks, so space does
	// not answer there and no hint offers it.
	r.DefineGuard("list-marks", func(r *Runtime, ev KeyEvent) bool {
		for _, id := range r.Ancestry(ev.Focus) {
			if list := r.List(id); list != nil {
				return !list.noMarks
			}
		}
		return true
	})
	resolve := func(c Context, args Arguments) (*List, error) {
		if id := StringArg(args, "id", ""); id != "" {
			if list := r.List(id); list != nil {
				return list, nil
			}
			return nil, fmt.Errorf("unknown list %q", id)
		}
		for _, id := range r.Ancestry(focusOf(c)) {
			if list := r.List(id); list != nil && c.InputID == "" {
				return list, nil
			}
		}
		return nil, errors.New("no focused list")
	}
	// rowOf names the row an operation addresses: args.item, else the row on
	// the context, else the selection.
	rowOf := func(l *List, c Context, args Arguments) (string, error) {
		if item := StringArg(args, "item", ""); item != "" {
			return item, nil
		}
		if focus := focusOf(c); l.r.rows[focus] == l.id {
			return focus, nil
		}
		if selected, ok := l.Selected(); ok {
			return selected.ID, nil
		}
		return "", errors.New("list has no selected row")
	}
	for _, spec := range []struct {
		verb, description string
		parameters        map[string]Parameter
		run               func(*List, Context, Arguments) (any, error)
	}{
		{"focus", "Focus a list and its selected row", map[string]Parameter{"id": {Type: "string", Required: true, Noun: "ochazuke.list", Free: true}}, func(l *List, _ Context, _ Arguments) (any, error) { return l.state(), l.Focus() }},
		{"move", "Move the selection; past the edge, on to the next stop on the page", map[string]Parameter{"id": {Type: "string"}, "delta": {Type: "integer", Required: true}, "steps": {Type: "integer", Description: "Repeat count (3j); stops at the list's edge, and spills as one move only from the edge"}, "clamp": {Type: "boolean", Description: "Stop at the list's edges instead of spilling (the filter line's moves)"}}, func(l *List, _ Context, a Arguments) (any, error) {
			delta, steps := IntArg(a, "delta", 0), max(1, IntArg(a, "steps", 1))
			if clamp, _ := a["clamp"].(bool); !clamp && l.motion != nil && !l.visual {
				l.motion(delta, steps)
				return l.state(), nil
			}
			if steps > 1 && l.selected >= 0 {
				if to := min(max(l.selected+delta*steps, 0), len(l.items)-1); to != l.selected {
					l.Move(to - l.selected)
					return l.state(), nil
				}
			}
			if clamp, _ := a["clamp"].(bool); clamp || !l.spill(delta) {
				l.Move(delta)
			}
			return l.state(), nil
		}},
		{"select", "Select a list item by stable ID without stealing focus", map[string]Parameter{"id": {Type: "string"}, "item": {Type: "string", Required: true}}, func(l *List, _ Context, a Arguments) (any, error) {
			return l.state(), l.Select(StringArg(a, "item", ""))
		}},
		{"mark", "Toggle the mark on a row (the selected row by default) for fan-out verbs", map[string]Parameter{"id": {Type: "string"}, "item": {Type: "string"}}, func(l *List, c Context, a Arguments) (any, error) {
			id, err := rowOf(l, c, a)
			if err != nil {
				return nil, err
			}
			return l.state(), l.ToggleMark(id)
		}},
		{"apply", "Run <row noun>.verb for every marked row the filter shows (the selected row when none is), the rows its subjects: a verb that takes many runs once for all of them, any other once per row; rows whose noun lacks the verb are skipped and reported; a verb with a confirm asks once for all the rows; available when a row it would act on has the verb and the verb is available there (read while a visual selection still holds); over a visual selection (V) the selection then ends or stays as visual.after-action says", map[string]Parameter{"id": {Type: "string"}, "verb": {Type: "string", Required: true}, "args": {Type: "object", Description: "Arguments every row's request carries"}, "confirmed": {Type: "boolean", Description: "The rows' confirm was answered (the confirm modal sets it)"}}, func(l *List, c Context, a Arguments) (any, error) {
			return l.apply(c, a)
		}},
	} {
		r.Register(Operation{Verb: spec.verb, Noun: "ochazuke.list", Description: spec.description, Parameters: spec.parameters, Result: "list state", Run: func(_ *Runtime, c Context, args Arguments) (any, error) {
			list, err := resolve(c, args)
			if err != nil {
				return nil, err
			}
			return spec.run(list, c, args)
		}})
	}
	if r.Nouns["ochazuke.list"].Guards == nil {
		r.Nouns["ochazuke.list"].Guards = map[string]string{}
	}
	r.Nouns["ochazuke.list"].Guards["mark"] = "list-marks"
	// list-moves: list.move is unavailable where it would only spill past
	// an edge the list traps (NoSpill, list.no-spill ID:last / ID:first):
	// the hints and help leave it out there and the key goes on to the
	// next row bound to it, as any unavailable action's does. A clamped
	// move (the filter line's) stays available.
	r.DefineGuard("list-moves", func(r *Runtime, ev KeyEvent) bool {
		if clamp, _ := ev.Args["clamp"].(bool); clamp {
			return true
		}
		delta := IntArg(ev.Args, "delta", 0)
		var l *List
		if id := StringArg(ev.Args, "id", ""); id != "" {
			l = r.List(id)
		} else {
			l = r.focusedList(ev.Focus)
		}
		return l == nil || !(l.atRowEdge(delta) && l.trappedAt(rowEdge(delta)))
	})
	r.Nouns["ochazuke.list"].Guards["move"] = "list-moves"
	registerListMotions(r)
	registerTableCells(r)
	registerListVisual(r)
	registerListFilter(r)
	registerListPropFilter(r)
	for _, verb := range []string{"ochazuke.list.column", "ochazuke.list.scroll-columns"} {
		if err := r.AddVerbAvailability(verb, "list-column-moves"); err != nil {
			panic(err)
		}
	}
}
