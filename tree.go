package ochazuke

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TreeNode is one node of a tree. Nodes are flat and in display order: a
// node's children follow it, and Parent "" is a root. Noun is the row's
// noun (default TreeRowNoun); Data, Cells and Tones are as on ListItem (cell
// 0 is the label: its tones stay on it, after the indent and fold marker).
type TreeNode struct {
	ID     string     `json:"id"`
	Parent string     `json:"parent,omitempty"`
	Label  string     `json:"label"`
	Noun   string     `json:"noun,omitempty"`
	Data   any        `json:"data,omitempty"`
	Props  Thing      `json:"props,omitempty"`
	Cells  []string   `json:"cells,omitempty"`
	Tones  []ToneSpan `json:"tones,omitempty"`
	// Section is the label line drawn above a run of rows in one section
	// (ListItem.Section); "" none.
	Section string `json:"section,omitempty"`
	// Fixed names the changes the node refuses: TreeRename, TreeMove
	// (move, indent, outdent) and TreeDelete (deleting it or any
	// ancestor). An outline's document root is Fixed all three; an array
	// item, whose key is its index, refuses rename alone. The refusal holds
	// from every path (keys, the palette, the CLI, Tree methods); the verb
	// is not available on the node (outline-renamable, outline-movable,
	// outline-deletable), so its hints and help rows drop out.
	Fixed []string `json:"fixed,omitempty"`
}

// Refuses reports whether the node refuses the change op (TreeRename,
// TreeMove, TreeDelete).
func (n TreeNode) Refuses(op string) bool { return slices.Contains(n.Fixed, op) }

// TreeChange is one committed edit of an outline: add, rename, move or
// delete. Index is the node's position among its parent's children after
// the change. The application writes each one to its store as it comes
// (Tree.OnChange); undo is a further change, never a discard.
type TreeChange struct {
	Op     string `json:"op"`
	ID     string `json:"id"`
	Parent string `json:"parent,omitempty"`
	Index  int    `json:"index"`
	Text   string `json:"text,omitempty"`
}

// Tree change operations.
const (
	TreeAdd    = "add"
	TreeRename = "rename"
	TreeMove   = "move"
	TreeDelete = "delete"
)

// String reads a change in one line: "add n1 under root at 1: text",
// "rename n1: text", "move n1 under a at 0", "delete n1".
func (c TreeChange) String() string {
	parent := c.Parent
	if parent == "" {
		parent = "root"
	}
	switch c.Op {
	case TreeAdd:
		return fmt.Sprintf("add %s under %s at %d: %s", c.ID, parent, c.Index, c.Text)
	case TreeRename:
		return fmt.Sprintf("rename %s: %s", c.ID, c.Text)
	case TreeMove:
		return fmt.Sprintf("move %s under %s at %d", c.ID, parent, c.Index)
	}
	return c.Op + " " + c.ID
}

// TreeRowNoun is the noun of a node that names none: enter toggles it.
const TreeRowNoun = "ochazuke.outline-row"

// DefaultTreeDebounce is how long typing in an outline line pauses before
// the line is written.
const DefaultTreeDebounce = 400 * time.Millisecond

// TreeDebounceMsg is the timer a pause in typing sets off: when Seq is
// still the line's latest change, the line is written.
type TreeDebounceMsg struct {
	Tree string
	Seq  uint64
}

// treeEdit is one undo step: the changes that undo it, in order, or an
// application action (Tree.Record).
type treeEdit struct {
	undo   []TreeChange
	action *TreeAction
	// steps is a group: the steps a verb over several rows made (a
	// visual selection), undone last first as one step (Tree.group).
	steps []treeEdit
}

// group starts one undo step for a verb run over several rows: its end
// folds every step recorded since into one, so one u restores them all.
func (t *Tree) group() (end func()) {
	from := len(t.undo)
	return func() {
		if len(t.undo)-from < 2 {
			return
		}
		steps := slices.Clone(t.undo[from:])
		t.undo = append(t.undo[:from], treeEdit{steps: steps})
	}
}

// TreeAction is an application change that goes on an outline's undo
// stack in order with the tree's own edits: a change the application keeps
// outside the tree's nodes, such as a task's status. u runs Undo and
// ctrl+r runs Redo; each is written by the application like any change.
// ID is the node selected after either runs ("" keeps the selection).
type TreeAction struct {
	ID    string
	Label string // what the change is ("done n1"), for logs
	Undo  func() error
	Redo  func() error
}

// Record puts an application change on the undo stack: an open line is
// sealed first (so its typing undoes after the action, not before), and
// the redo stack is cleared, as for any new change.
func (t *Tree) Record(a TreeAction) error {
	if a.Undo == nil || a.Redo == nil {
		return errors.New("tree action needs Undo and Redo")
	}
	err := t.seal()
	t.undo = append(t.undo, treeEdit{action: &a})
	t.redo = nil
	return err
}

// Tree is a list whose rows are nested nodes: expand and collapse, the
// same motions as a list (it is one: rows spill to the neighbouring stops
// in drawing order), the one highlighted row, columns like a table. Given
// OnChange it is an outline editor: every structural edit is written at
// once, typing within a line is written after a pause (Debounce) and when
// the line is left, undo and redo are changes too, and esc never discards.
//
// The tree is an instance of the outline noun with a list child
// (id+"/list") holding the visible rows; editing adds an input
// (id+"/line", noun outline-line) under the tree while a line is edited.
// (The nouns are "outline", not "tree", so palette prefixes such as "tr"
// stay unique in apps.)
type Tree struct {
	r               *Runtime
	id              string
	list            *List
	nodes           []TreeNode
	idx             *treeIndex // lookups over nodes, built on demand; nil after any change to their order or parents
	rowCache        map[string]*filterRow
	filterCollapsed map[string]bool // folds explicitly changed while prop filters show context
	collapsed       map[string]bool
	// Guides is the app's choice of branch lines (also set by Guides()).
	// tree.guides=off overrides it.
	Guides      bool
	guidesShown bool
	guideLast   map[string]string // last kept child of each parent
	zoom        string            // the node the view is zoomed on (tree_zoom.go); "" every level

	// OnChange makes the tree editable and receives each change as it is
	// made; an error keeps the change on screen, marks the row and retries
	// it before the next change.
	OnChange func(TreeChange) error
	// NewID names a new node (default "<tree>-n<seq>").
	NewID func() string
	// RowNoun is the noun of nodes the tree adds (default TreeRowNoun).
	RowNoun string
	// Refuse is the application's check on a change before it is made
	// (rename, move, delete and add; for add the node is the parent, the
	// zero node at the top): a non-nil error refuses it with that reason,
	// from every path. It runs after the nodes' Fixed. Undo and redo are
	// not checked: they take back changes that were allowed.
	Refuse func(node TreeNode, change TreeChange) error
	// Debounce is the pause in typing before a line is written.
	Debounce time.Duration

	seq       int
	undo      []treeEdit
	redo      []treeEdit
	failed    map[string]bool
	retry     []TreeChange
	input     *Input
	editing   string // the node whose line is open
	before    string // its text when the line opened
	written   string // its text as last written
	seen      string // its text at the last PendingCommand
	changeSeq uint64
	failure   error
}

// AddTree registers a tree under Parent (default app) with its nodes.
// Guides() draws branch lines; trees without it keep plain indentation.
// Columns(...) makes its rows a table; NoSpill keeps motions inside it;
// NoMarks makes it a read-only view whose rows take no marks;
// ClearFilterOnEnter makes enter in its filter line choose the match and
// clear the filter.
func (r *Runtime) AddTree(id string, nodes []TreeNode, opts ...InstanceOption) (*Tree, error) {
	if id == "" || r.Instances[id] != nil {
		return nil, fmt.Errorf("tree ID %q must be nonempty and unused", id)
	}
	spec := instanceSpec{parent: "app"}
	for _, opt := range opts {
		opt(&spec)
	}
	if r.Instances[spec.parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", spec.parent)
	}
	t := &Tree{r: r, id: id, collapsed: map[string]bool{}, failed: map[string]bool{}, RowNoun: TreeRowNoun, Debounce: DefaultTreeDebounce, Guides: spec.guides}
	r.Instance(id, Parent(spec.parent), Kind("ochazuke.outline"), FocusOnClose(spec.closeFocus))
	r.Instances[id].Component = t
	listOpts := []InstanceOption{Parent(id)}
	if spec.noSpill {
		listOpts = append(listOpts, NoSpill())
	}
	if spec.noMarks {
		listOpts = append(listOpts, NoMarks())
	}
	if spec.clearFilterOnEnter {
		listOpts = append(listOpts, ClearFilterOnEnter())
	}
	if len(spec.columns) > 0 {
		listOpts = append(listOpts, Columns(spec.columns...))
	}
	list, err := r.AddList(id+"/list", nil, listOpts...)
	if err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	t.list = list
	list.undoGroup = t.group
	if err := t.SetNodes(nodes); err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	return t, nil
}

// Tree is the tree with this id, or nil.
func (r *Runtime) Tree(id string) *Tree {
	if inst := r.Instances[id]; inst != nil {
		if t, ok := inst.Component.(*Tree); ok {
			return t
		}
	}
	return nil
}

// treeOf is the tree on a focus's ancestry, or nil.
func (r *Runtime) treeOf(focus string) *Tree {
	for _, id := range r.Ancestry(focus) {
		if t := r.Tree(id); t != nil {
			return t
		}
	}
	return nil
}

// ID is the tree's instance id.
func (t *Tree) ID() string { return t.id }

// List is the list that holds the visible rows.
func (t *Tree) List() *List { return t.list }

// FocusTarget is where a jump to the tree lands: its selected row.
func (t *Tree) FocusTarget() string { return t.list.FocusTarget() }

// Focus focuses the tree's selected row.
func (t *Tree) Focus() error { return t.list.Focus() }

// Nodes is a copy of every node in display order, collapsed ones included.
func (t *Tree) Nodes() []TreeNode { return slices.Clone(t.nodes) }

// Editable reports whether the tree takes edits (OnChange is set).
func (t *Tree) Editable() bool { return t.OnChange != nil }

// Editing is the node whose line is open, or "".
func (t *Tree) Editing() string { return t.editing }

// SetNodes replaces the nodes, keeping collapse state and the selection by
// id. Nodes must list each parent before its children.
func (t *Tree) SetNodes(nodes []TreeNode) error {
	ids := map[string]bool{}
	for _, n := range nodes {
		if n.ID == "" || ids[n.ID] {
			return fmt.Errorf("tree node ID %q must be nonempty and unique", n.ID)
		}
		if n.Parent != "" && !ids[n.Parent] {
			return fmt.Errorf("tree node %q: parent %q must come before it", n.ID, n.Parent)
		}
		ids[n.ID] = true
	}
	// Order children under their parents (a flat list may interleave).
	children := map[string][]TreeNode{}
	for _, n := range nodes {
		children[n.Parent] = append(children[n.Parent], n)
	}
	var ordered []TreeNode
	var walk func(parent string)
	walk = func(parent string) {
		for _, n := range children[parent] {
			ordered = append(ordered, n)
			walk(n.ID)
		}
	}
	walk("")
	t.nodes, t.idx, t.rowCache = ordered, nil, nil
	return t.refresh("")
}

// SetCollapsed collapses or expands one node.
func (t *Tree) SetCollapsed(id string, collapsed bool) error {
	if t.index(id) < 0 {
		return fmt.Errorf("unknown tree node %q", id)
	}
	t.setCollapsed(id, collapsed)
	return t.refresh("")
}

// CollapseAll collapses every node that has children.
func (t *Tree) CollapseAll() error {
	for _, n := range t.nodes {
		if t.hasChildren(n.ID) {
			t.setCollapsed(n.ID, true)
		}
	}
	return t.refresh("")
}

// Collapsed reports whether a node is collapsed.
func (t *Tree) Collapsed(id string) bool { return t.collapsed[id] }

// folded reads the filter's temporary folds while props reveal context.
func (t *Tree) folded(id string) bool {
	if len(t.list.propFilters) > 0 {
		return t.filterCollapsed[id]
	}
	return t.collapsed[id]
}

func (t *Tree) setCollapsed(id string, folded bool) {
	if len(t.list.propFilters) > 0 {
		if t.filterCollapsed == nil {
			t.filterCollapsed = map[string]bool{}
		}
		t.filterCollapsed[id] = folded
	} else {
		t.collapsed[id] = folded
	}
}

// Selected is the node of the selected row.
func (t *Tree) Selected() (TreeNode, bool) {
	item, ok := t.list.Selected()
	if !ok {
		return TreeNode{}, false
	}
	if i := t.index(item.ID); i >= 0 {
		return t.nodes[i], true
	}
	return TreeNode{}, false
}

// Select selects a node, expanding its ancestors, without taking focus.
func (t *Tree) Select(id string) error {
	i := t.index(id)
	if i < 0 {
		return fmt.Errorf("unknown tree node %q", id)
	}
	for p := t.nodes[i].Parent; p != ""; p = t.parentOf(p) {
		t.setCollapsed(p, false)
	}
	return t.refresh(id)
}

// treeIndex is what the tree looks up per node, built in one pass over
// the nodes: each id's position (its first, as slices.Index finds), the
// number of children under each id ("" counts the roots) and each node's
// depth. A tree of thousands of nodes asked each by a scan made every
// refresh quadratic.
type treeIndex struct {
	at    map[string]int
	kids  map[string]int
	depth []int
}

// lookup is the index over the current nodes, rebuilt after a change.
func (t *Tree) lookup() *treeIndex {
	if t.idx != nil {
		return t.idx
	}
	x := &treeIndex{at: make(map[string]int, len(t.nodes)), kids: map[string]int{}, depth: make([]int, len(t.nodes))}
	for i, n := range t.nodes {
		if _, ok := x.at[n.ID]; !ok {
			x.at[n.ID] = i
		}
		x.kids[n.Parent]++
	}
	// depth[i] counts the parent ids up node i's chain, as walking
	// parentOf did: a root is 0, and a parent that is not a node still
	// counts but ends the chain.
	done := make([]bool, len(t.nodes))
	var chain []int
	for i := range t.nodes {
		chain = chain[:0]
		j, d := i, 0
		for {
			if done[j] {
				d = x.depth[j]
				break
			}
			chain = append(chain, j)
			parent := t.nodes[j].Parent
			p, ok := x.at[parent]
			if parent == "" {
				d = -1
				break
			}
			if !ok {
				break
			}
			j = p
		}
		for k := len(chain) - 1; k >= 0; k-- {
			d++
			x.depth[chain[k]], done[chain[k]] = d, true
		}
	}
	t.idx = x
	return x
}

func (t *Tree) index(id string) int {
	if i, ok := t.lookup().at[id]; ok {
		return i
	}
	return -1
}

func (t *Tree) parentOf(id string) string {
	if i := t.index(id); i >= 0 {
		return t.nodes[i].Parent
	}
	return ""
}

func (t *Tree) depth(id string) int {
	if i := t.index(id); i >= 0 {
		return t.lookup().depth[i]
	}
	return 0
}

func (t *Tree) hasChildren(id string) bool { return t.lookup().kids[id] > 0 }

func (t *Tree) children(parent string) []string {
	var out []string
	for _, n := range t.nodes {
		if n.Parent == parent {
			out = append(out, n.ID)
		}
	}
	return out
}

// subtreeEnd is the index just past node i's last descendant.
func (t *Tree) subtreeEnd(i int) int {
	d := t.depth(t.nodes[i].ID)
	j := i + 1
	for j < len(t.nodes) && t.depth(t.nodes[j].ID) > d {
		j++
	}
	return j
}

// visible reports whether no ancestor of a node is collapsed.
func (t *Tree) visible(n TreeNode) bool {
	for p := n.Parent; p != ""; p = t.parentOf(p) {
		if t.folded(p) {
			return false
		}
	}
	return true
}

// prefix is a row's indent and expand marker.
func (t *Tree) prefix(n TreeNode) string {
	marker := "  "
	if t.hasChildren(n.ID) {
		marker = "▾ "
		if t.folded(n.ID) {
			marker = "▸ "
		}
	}
	if t.failed[n.ID] {
		marker = "! "
	}
	return t.indent(n.ID) + marker
}

// shiftTones moves a node's cell-0 tones past the row's indent and fold
// marker (prefix runes), so they stay on the label; a whole-cell tone
// starts after the prefix.
func shiftTones(tones []ToneSpan, prefix int) []ToneSpan {
	if len(tones) == 0 {
		return nil
	}
	out := slices.Clone(tones)
	for i := range out {
		if out[i].Cell != 0 {
			continue
		}
		out[i].From = max(out[i].From, 0) + prefix
		if out[i].To > 0 {
			out[i].To += prefix
		}
	}
	return out
}

// refresh pushes the visible nodes to the list, selecting id when given.
func (t *Tree) refresh(selectID string) error {
	t.fitZoom(selectID)
	cache := make(map[string]*filterRow, len(t.nodes))
	for _, n := range t.nodes {
		row := t.rowCache[n.ID]
		if row == nil || !reflect.DeepEqual(row.node, n) {
			noun := n.Noun
			if noun == "" {
				noun = t.RowNoun
			}
			row = &filterRow{node: n, item: ListItem{ID: n.ID, Label: n.Label, Noun: noun, Data: n.Data, Props: n.Props, Cells: n.Cells}}
		}
		cache[n.ID] = row
	}
	t.rowCache = cache
	from, to := t.zoomRange()
	items := make([]ListItem, 0, to-from)
	for _, n := range t.nodes[from:to] {
		if !t.visible(n) {
			continue
		}
		noun := n.Noun
		if noun == "" {
			noun = t.RowNoun
		}
		prefix := t.prefix(n)
		item := ListItem{ID: n.ID, Label: prefix + n.Label, Noun: noun, Data: n.Data, Props: n.Props, Tones: shiftTones(n.Tones, utf8.RuneCountInString(prefix)), Section: n.Section}
		if len(n.Cells) > 0 {
			item.Cells = slices.Clone(n.Cells)
			item.Cells[0] = prefix + item.Cells[0]
		}
		items = append(items, item)
	}
	if err := t.list.SetItems(items); err != nil {
		return err
	}
	// Row labels name the node, not its indent, for the palette and {{label}}.
	for _, n := range t.nodes {
		if inst := t.r.Instances[n.ID]; inst != nil && t.r.rows[n.ID] == t.list.id {
			inst.Label = n.Label
		}
	}
	if selectID != "" {
		return t.list.Select(selectID)
	}
	return nil
}

// View renders the visible rows (see Block).
func (t *Tree) View(width, height int) string { return t.Block(width, height).Text }

// Block renders the rows as the list does; while a line is open its row is
// the input, with the terminal cursor in it. A zoomed tree draws its
// breadcrumb line above the rows (tree.breadcrumb line) when there is room
// for a row under it; the frame first applies tree.zoom (frameZoom).
func (t *Tree) Block(width, height int) Block {
	t.frameZoom(width)
	crumb := ""
	if height > 1 {
		crumb = t.crumbLine(width)
	}
	if crumb == "" {
		return t.rows(width, height)
	}
	out := t.rows(width, height-1)
	out.Text = crumb + "\n" + out.Text
	if out.Cursor != nil {
		out.Cursor.Y++
	}
	return out
}

// rows renders the visible rows (Block without the breadcrumb).
func (t *Tree) rows(width, height int) Block {
	text := t.list.View(width, height)
	if t.input == nil || t.editing == "" {
		return Text(text)
	}
	i := t.index(t.editing)
	if i < 0 || t.list.selected < t.list.offset {
		return Text(text)
	}
	line := t.list.selected - t.list.offset
	if height > 1 && t.list.filterLine(width) != "" {
		line++
	}
	if len(t.list.columns) > 0 {
		line++
	}
	rows := strings.Split(text, "\n")
	if line >= len(rows) {
		return Text(text)
	}
	prefix := t.prefix(t.nodes[i])
	if t.guidesEnabled() {
		indent := t.indent(t.nodes[i].ID)
		if styles := t.r.Settings().page.styles; styles != nil {
			prefix = styles().Tone(ToneTreeGuide).Render(indent) + strings.TrimPrefix(prefix, indent)
		}
		prefix = ansi.Truncate(prefix, max(0, width-1), "")
	}
	pw := ansi.StringWidth(prefix)
	block := InputBlock(t.input, max(1, width-pw), 1)
	rows[line] = prefix + block.Text
	out := Text(strings.Join(rows, "\n"))
	if block.Cursor != nil {
		out.Cursor = &Pos{X: block.Cursor.X + pw, Y: line}
	}
	return out
}

// HandleMessage writes the open line when its pause timer is the latest.
func (t *Tree) HandleMessage(msg tea.Msg) bool {
	tick, ok := msg.(TreeDebounceMsg)
	if !ok || tick.Tree != t.id {
		return false
	}
	if t.DebounceCurrent(tick) {
		if err := t.flush(); err != nil {
			t.fail(err)
		}
	}
	return true
}

// DebounceTick is the pause timer in flight, for a host that delivers it
// at once (explore): false when no typing waits on one.
func (t *Tree) DebounceTick() (TreeDebounceMsg, bool) {
	msg := TreeDebounceMsg{Tree: t.id, Seq: t.changeSeq}
	return msg, t.DebounceCurrent(msg)
}

// DebounceCurrent reports whether a pause timer is for the line's latest
// change (an older timer writes nothing).
func (t *Tree) DebounceCurrent(tick TreeDebounceMsg) bool {
	return tick.Tree == t.id && tick.Seq == t.changeSeq && t.editing != ""
}

// PendingCommand notices typing in the open line and sets the pause timer;
// a line whose input lost the focus is closed (and written).
func (t *Tree) PendingCommand() tea.Cmd {
	if t.input == nil {
		return nil
	}
	if t.r.Focus != t.input.ID && t.r.Instances[t.input.ID] != nil && t.r.TopModal() == "" {
		if err := t.closeLine(false); err != nil {
			t.fail(err)
		}
		return nil
	}
	text := string(t.input.Text)
	if text == t.seen {
		return nil
	}
	t.seen = text
	t.changeSeq++
	seq, id, wait := t.changeSeq, t.id, t.Debounce
	return tea.Tick(wait, func(time.Time) tea.Msg { return TreeDebounceMsg{Tree: id, Seq: seq} })
}

// Close writes an open line.
func (t *Tree) Close() { _ = t.Flush() }

// Flush writes the open line now (on quit, and when the line is left).
func (t *Tree) Flush() error { return t.flush() }

func (t *Tree) flush() error {
	if t.editing == "" || t.input == nil {
		return nil
	}
	text := string(t.input.Text)
	if text == t.written {
		return nil
	}
	i := t.index(t.editing)
	if i < 0 {
		return nil
	}
	t.nodes[i].Label = text
	t.written = text
	return t.write([]TreeChange{{Op: TreeRename, ID: t.editing, Text: text}})
}

// write hands changes to OnChange, retried changes first; a failure keeps
// the rest queued and marks the rows.
func (t *Tree) write(changes []TreeChange) error {
	queue := append(t.retry, changes...)
	t.retry = nil
	for i, c := range queue {
		if t.OnChange == nil {
			break
		}
		if err := t.OnChange(c); err != nil {
			t.retry = slices.Clone(queue[i:])
			for _, q := range t.retry {
				t.failed[q.ID] = true
			}
			_ = t.refresh("")
			return fmt.Errorf("write %s: %w", c, err)
		}
		delete(t.failed, c.ID)
	}
	return t.refresh("")
}

// apply makes one change on the nodes and returns its inverse.
func (t *Tree) apply(c TreeChange) (TreeChange, error) {
	switch c.Op {
	case TreeAdd:
		if t.index(c.ID) >= 0 {
			return TreeChange{}, fmt.Errorf("tree node %q exists", c.ID)
		}
		noun := t.RowNoun
		t.insert(c.Parent, c.Index, []TreeNode{{ID: c.ID, Parent: c.Parent, Label: c.Text, Noun: noun}})
		return TreeChange{Op: TreeDelete, ID: c.ID}, nil
	case TreeRename:
		i := t.index(c.ID)
		if i < 0 {
			return TreeChange{}, fmt.Errorf("unknown tree node %q", c.ID)
		}
		old := t.nodes[i].Label
		t.nodes[i].Label = c.Text
		if c.ID == t.editing && t.input != nil {
			_ = t.input.Replace(0, len(t.input.Text), c.Text)
			t.written, t.seen = c.Text, c.Text
		}
		return TreeChange{Op: TreeRename, ID: c.ID, Text: old}, nil
	case TreeMove:
		i := t.index(c.ID)
		if i < 0 {
			return TreeChange{}, fmt.Errorf("unknown tree node %q", c.ID)
		}
		parent, index := t.nodes[i].Parent, slices.Index(t.children(t.nodes[i].Parent), c.ID)
		sub := slices.Clone(t.nodes[i:t.subtreeEnd(i)])
		t.nodes, t.idx = slices.Delete(t.nodes, i, i+len(sub)), nil
		sub[0].Parent = c.Parent
		t.insert(c.Parent, c.Index, sub)
		return TreeChange{Op: TreeMove, ID: c.ID, Parent: parent, Index: index}, nil
	case TreeDelete:
		i := t.index(c.ID)
		if i < 0 {
			return TreeChange{}, fmt.Errorf("unknown tree node %q", c.ID)
		}
		n := t.nodes[i]
		index := slices.Index(t.children(n.Parent), c.ID)
		t.nodes = slices.Delete(t.nodes, i, t.subtreeEnd(i))
		t.idx = nil
		delete(t.collapsed, c.ID)
		return TreeChange{Op: TreeAdd, ID: n.ID, Parent: n.Parent, Index: index, Text: n.Label}, nil
	}
	return TreeChange{}, fmt.Errorf("unknown tree change %q", c.Op)
}

// insert places nodes (a subtree, root first) as child index of parent.
func (t *Tree) insert(parent string, index int, sub []TreeNode) {
	kids := t.children(parent)
	pos := len(t.nodes)
	switch {
	case index < len(kids):
		pos = t.index(kids[index])
	case parent != "":
		pos = t.subtreeEnd(t.index(parent))
	}
	t.nodes, t.idx = slices.Insert(t.nodes, pos, sub...), nil
}

// commit applies changes, writes them and records one undo step.
func (t *Tree) commit(changes []TreeChange, selectID string) error {
	for _, c := range changes {
		if err := t.refuses(c); err != nil {
			return err
		}
	}
	var undo []TreeChange
	for _, c := range changes {
		inv, err := t.apply(c)
		if err != nil {
			return err
		}
		undo = append([]TreeChange{inv}, undo...)
	}
	t.undo = append(t.undo, treeEdit{undo: undo})
	t.redo = nil
	err := t.write(changes)
	if selectID != "" {
		if serr := t.Select(selectID); err == nil {
			err = serr
		}
	}
	return err
}

// refuses is why the change may not be made, nil when it may: a node's
// Fixed, then the application's Refuse.
func (t *Tree) refuses(c TreeChange) error {
	var node TreeNode
	if c.Op == TreeAdd {
		if i := t.index(c.Parent); i >= 0 {
			node = t.nodes[i]
		}
	} else if i := t.index(c.ID); i >= 0 {
		node = t.nodes[i]
		if blocker, ok := t.fixedFor(c.Op, c.ID); ok {
			if blocker.ID == node.ID {
				return fmt.Errorf("%s refuses %s", nodeName(node), changeVerb(c.Op))
			}
			return fmt.Errorf("%s holds %s, which refuses %s", nodeName(node), nodeName(blocker), changeVerb(c.Op))
		}
	}
	if t.Refuse != nil {
		return t.Refuse(node, c)
	}
	return nil
}

// fixedFor is the node that makes id refuse op by Fixed: id itself, or for
// a delete any node of its subtree.
func (t *Tree) fixedFor(op, id string) (TreeNode, bool) {
	i := t.index(id)
	if i < 0 {
		return TreeNode{}, false
	}
	end := i + 1
	if op == TreeDelete {
		end = t.subtreeEnd(i)
	}
	for _, n := range t.nodes[i:end] {
		if n.Refuses(op) {
			return n, true
		}
	}
	return TreeNode{}, false
}

// allows reports whether the node the focus acts on (targetOf) takes op
// by its Fixed: the availability of the outline verbs.
func (t *Tree) allows(op, focus string) bool {
	id := t.targetOf(focus)
	if id == "" {
		return true
	}
	_, fixed := t.fixedFor(op, id)
	return !fixed
}

func nodeName(n TreeNode) string {
	if n.Label != "" {
		return fmt.Sprintf("%q", n.Label)
	}
	return n.ID
}

func changeVerb(op string) string {
	switch op {
	case TreeRename:
		return "a rename"
	case TreeMove:
		return "a move"
	case TreeDelete:
		return "deletion"
	}
	return op
}

func (t *Tree) newID() string {
	if t.NewID != nil {
		return t.NewID()
	}
	for {
		t.seq++
		id := fmt.Sprintf("%s-n%d", t.id, t.seq)
		if t.index(id) < 0 && t.r.Instances[id] == nil {
			return id
		}
	}
}

// openLine edits a node's line in insert mode, the cursor at its end;
// selected opens it with its whole text selected (Input.SelectAll), so
// typing replaces it and esc or a motion keeps it.
func (t *Tree) openLine(id string, selected bool) error {
	i := t.index(id)
	if i < 0 {
		return fmt.Errorf("unknown tree node %q", id)
	}
	if t.editing != "" {
		if err := t.closeLine(true); err != nil {
			return err
		}
	}
	if err := t.Select(id); err != nil {
		return err
	}
	text := t.nodes[i].Label
	in := NewInput(t.id+"/line", text, false)
	if err := t.r.AddInput(in, Parent(t.id), Kind("ochazuke.outline-line"), FocusOnClose(id)); err != nil {
		return err
	}
	in.Cursor = len(in.Text)
	t.input, t.editing, t.before, t.written, t.seen = in, id, text, text, text
	if err := t.r.SetFocus(in.ID); err != nil {
		return err
	}
	if err := t.r.SetMode("insert"); err != nil {
		return err
	}
	if selected {
		return in.SelectAll()
	}
	return nil
}

// Edit opens a node's line as outline.edit does; selected opens it with
// its text selected, so typing replaces it (Input.SelectText).
func (t *Tree) Edit(id string, selected bool) error {
	if !t.Editable() {
		return errors.New("tree is read-only")
	}
	return t.rename(id, selected)
}

// rename opens an existing node's line unless the node refuses a rename.
func (t *Tree) rename(id string, selected bool) error {
	if i := t.index(id); i >= 0 {
		if err := t.refuses(TreeChange{Op: TreeRename, ID: id, Text: t.nodes[i].Label}); err != nil {
			return err
		}
	}
	return t.openLine(id, selected)
}

// closeLine writes the open line, records its rename as one undo step and
// returns the focus to its row (refocus) in normal mode.
func (t *Tree) closeLine(refocus bool) error {
	if t.editing == "" {
		return nil
	}
	err := t.seal()
	id := t.editing
	in := t.input
	t.input, t.editing = nil, ""
	if t.r.Instances[in.ID] != nil {
		focused := t.r.Focus == in.ID
		if rerr := t.r.RemoveInstance(in.ID); rerr != nil && err == nil {
			err = rerr
		}
		refocus = refocus || focused
	}
	if refocus && t.index(id) >= 0 {
		if serr := t.Select(id); serr == nil {
			if ferr := t.r.SetFocus(id); ferr != nil && err == nil {
				err = ferr
			}
		}
		if t.r.Mode.Name != "normal" {
			_ = t.r.SetMode("normal")
		}
	}
	return err
}

// seal writes the open line and records its typing so far as one undo
// step, so a structural edit made while the line is open (tab) undoes
// after it, not before.
func (t *Tree) seal() error {
	err := t.flush()
	if t.editing != "" && t.before != t.written {
		t.undo = append(t.undo, treeEdit{undo: []TreeChange{{Op: TreeRename, ID: t.editing, Text: t.before}}})
		t.redo = nil
		t.before = t.written
	}
	return err
}

// target is the node a verb acts on: the open line, else the row on the
// focus, else the selection.
func (t *Tree) target(c Context) string { return t.targetOf(focusOf(c)) }

// targetOf is target for a focus id.
func (t *Tree) targetOf(focus string) string {
	if t.editing != "" {
		return t.editing
	}
	if t.r.rows[focus] == t.list.id {
		return focus
	}
	if n, ok := t.Selected(); ok {
		return n.ID
	}
	return ""
}

// add inserts a new node holding text (below, above or as the last child
// of the target, or as the first root of an empty tree) and opens its
// line; selected opens it with the text selected, a placeholder that
// typing replaces.
func (t *Tree) add(c Context, where, text string, selected bool) error {
	if err := t.closeLine(false); err != nil {
		return err
	}
	at := t.target(c)
	parent, index := "", len(t.children(""))
	if at != "" {
		p := t.parentOf(at)
		pos := slices.Index(t.children(p), at)
		switch where {
		case "above":
			parent, index = p, pos
		case "child":
			parent, index = at, len(t.children(at))
			t.setCollapsed(at, false)
		default:
			parent, index = p, pos+1
		}
	}
	id := t.newID()
	if err := t.commit([]TreeChange{{Op: TreeAdd, ID: id, Parent: parent, Index: index, Text: text}}, id); err != nil {
		t.fail(err)
	}
	return t.openLine(id, selected)
}

// shift nests a node under its previous sibling (indent) or makes it the
// sibling after its parent (outdent); an open line stays open.
func (t *Tree) shift(c Context, indent bool) error {
	id := t.target(c)
	if id == "" {
		return errors.New("tree has no selected node")
	}
	if err := t.seal(); err != nil {
		t.fail(err)
	}
	parent := t.parentOf(id)
	kids := t.children(parent)
	pos := slices.Index(kids, id)
	var change TreeChange
	if indent {
		if pos <= 0 {
			return nil // the first child has nothing to nest under: quiet edge
		}
		prev := kids[pos-1]
		t.setCollapsed(prev, false)
		change = TreeChange{Op: TreeMove, ID: id, Parent: prev, Index: len(t.children(prev))}
	} else {
		if parent == "" {
			return nil
		}
		grand := t.parentOf(parent)
		change = TreeChange{Op: TreeMove, ID: id, Parent: grand, Index: slices.Index(t.children(grand), parent) + 1}
	}
	err := t.commit([]TreeChange{change}, id)
	if t.editing != "" && t.input != nil {
		_ = t.r.SetFocus(t.input.ID)
	}
	return err
}

// rowKey runs what key does on the open line's row in normal mode: the
// key resolves through the stack walk at the row (the row noun's own
// rows first, then the tree's, as in normal mode), so an application's
// row rows answer it from the line as they do outside it. The line is
// written and closed first (one undo step for its typing), and afterwards
// it is opened again on the row the focus is on (the row, or the one the
// action made of it) with the cursor, or the open select range, where it
// was: editing goes on in insert, as Vim's insert-mode ctrl-t and ctrl-d
// keep it. A key whose action leaves the tree (a modal, another pane)
// leaves the line closed. With no line open it is the key in normal mode
// on the target row.
func (t *Tree) rowKey(c Context, key string) error {
	if key == "" {
		return errors.New("no key to run on the row")
	}
	if t.editing == "" {
		focus := t.target(c)
		if focus == "" {
			return errors.New("tree has no selected node")
		}
		_, err := t.r.dispatchIn(focus, ModeState{Name: "normal"}, MacroKey{Key: key})
		return err
	}
	in := t.input
	start, end, selected := in.SelectedText()
	if !selected {
		start, end = in.Cursor, in.Cursor
	}
	if err := t.closeLine(true); err != nil {
		return err
	}
	_, err := t.r.dispatchIn(t.r.Focus, ModeState{Name: "normal"}, MacroKey{Key: key})
	if t.r.Pending != "" {
		t.r.CancelPendingBinding() // a prefix row in normal mode: no sequence goes on from a line
	}
	row := t.r.Focus
	if t.r.rows[row] != t.list.id || t.index(row) < 0 || t.editing != "" {
		return err
	}
	if oerr := t.openLine(row, false); oerr != nil {
		return errors.Join(err, oerr)
	}
	n := len(t.input.Text)
	start, end = min(start, n), min(end, n)
	if selected && end > start {
		return errors.Join(err, t.input.SelectText(start, end))
	}
	t.input.Cursor = end
	return err
}

// move swaps a node with its previous or next sibling.
func (t *Tree) move(c Context, delta int) error {
	if err := t.closeLine(false); err != nil {
		return err
	}
	id := t.target(c)
	parent := t.parentOf(id)
	kids := t.children(parent)
	pos := slices.Index(kids, id)
	to := pos + delta
	if id == "" || to < 0 || to >= len(kids) {
		return nil
	}
	return t.commit([]TreeChange{{Op: TreeMove, ID: id, Parent: parent, Index: to}}, id)
}

// remove deletes a node and its subtree: the deepest nodes first, so each
// write is a leaf, and undo adds them back root first.
func (t *Tree) remove(c Context) error {
	if err := t.closeLine(false); err != nil {
		return err
	}
	id := t.target(c)
	i := t.index(id)
	if i < 0 {
		return errors.New("tree has no selected node")
	}
	sub := slices.Clone(t.nodes[i:t.subtreeEnd(i)])
	var changes []TreeChange
	for j := len(sub) - 1; j >= 0; j-- {
		changes = append(changes, TreeChange{Op: TreeDelete, ID: sub[j].ID})
	}
	next := ""
	items := t.list.Items()
	if k := slices.IndexFunc(items, func(it ListItem) bool { return it.ID == id }); k >= 0 {
		for _, it := range items[k+1:] {
			if !slices.ContainsFunc(sub, func(n TreeNode) bool { return n.ID == it.ID }) {
				next = it.ID
				break
			}
		}
		if next == "" && k > 0 {
			next = items[k-1].ID
		}
	}
	err := t.commit(changes, next)
	if next != "" {
		_ = t.r.SetFocus(next)
	}
	return err
}

// history undoes (or redoes) one step: its changes are applied and
// written like any other, and the inverse goes on the other stack.
func (t *Tree) history(redo bool) error {
	if err := t.closeLine(true); err != nil {
		return err
	}
	from, to := &t.undo, &t.redo
	if redo {
		from, to = &t.redo, &t.undo
	}
	if len(*from) == 0 {
		return nil
	}
	step := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	if step.steps != nil {
		// A group: each step undone last first, through the stacks like
		// one step of its own, then their inverses folded into one again.
		at := len(*to)
		var err error
		for i := len(step.steps) - 1; i >= 0 && err == nil; i-- {
			*from = append(*from, step.steps[i])
			err = t.history(redo)
		}
		if len(*to)-at > 0 {
			*to = append((*to)[:at], treeEdit{steps: slices.Clone((*to)[at:])})
		}
		return err
	}
	if a := step.action; a != nil {
		if err := a.Undo(); err != nil {
			*from = append(*from, step)
			return err
		}
		*to = append(*to, treeEdit{action: &TreeAction{ID: a.ID, Label: a.Label, Undo: a.Redo, Redo: a.Undo}})
		err := t.refresh("")
		t.focusNode(a.ID)
		return err
	}
	var inverse []TreeChange
	for _, c := range step.undo {
		inv, err := t.apply(c)
		if err != nil {
			return err
		}
		inverse = append([]TreeChange{inv}, inverse...)
	}
	*to = append(*to, treeEdit{undo: inverse})
	err := t.write(step.undo)
	focus := step.undo[len(step.undo)-1].ID
	if last := step.undo[len(step.undo)-1]; last.Op == TreeDelete {
		focus = t.parentOf(last.ID)
		if focus == "" {
			if n, ok := t.Selected(); ok {
				focus = n.ID
			}
		}
	}
	t.focusNode(focus)
	return err
}

// focusNode selects id and focuses its row when it is in the tree.
func (t *Tree) focusNode(id string) {
	if id != "" && t.index(id) >= 0 {
		if err := t.Select(id); err == nil {
			_ = t.r.SetFocus(id)
		}
	}
}

// pasteLines parses an indented block: tabs or two-space steps nest a line
// under the line above; "- " and "* " bullets are dropped; blank lines
// are skipped. Depths are relative to the shallowest line.
func pasteLines(text string) (lines []string, depths []int) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var raw []int
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		width := 0
		rest := line
		for len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
			if rest[0] == '\t' {
				width += 2
			} else {
				width++
			}
			rest = rest[1:]
		}
		for _, bullet := range []string{"- ", "* "} {
			rest = strings.TrimPrefix(rest, bullet)
		}
		lines = append(lines, strings.TrimRight(rest, " \t"))
		raw = append(raw, width/2)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	low := slices.Min(raw)
	prev := -1
	for _, d := range raw {
		d -= low
		d = min(d, prev+1) // never deeper than one under the line above
		depths = append(depths, d)
		prev = d
	}
	return lines, depths
}

// paste inserts a pasted block below the target, nested by its indent. A
// paste of one line into an open line is plain text.
func (t *Tree) paste(c Context, text string) error {
	if t.editing != "" && !strings.Contains(strings.TrimRight(text, "\n"), "\n") {
		in := t.input
		cursor := min(max(0, in.Cursor), len(in.Text))
		line := strings.TrimRight(text, "\n")
		if err := in.Replace(cursor, cursor, line); err != nil {
			return err
		}
		in.Cursor = cursor + len([]rune(line))
		return nil
	}
	lines, depths := pasteLines(text)
	if len(lines) == 0 {
		return nil
	}
	if err := t.closeLine(false); err != nil {
		return err
	}
	at := t.target(c)
	parent, index := "", len(t.children(""))
	if at != "" {
		parent = t.parentOf(at)
		index = slices.Index(t.children(parent), at) + 1
	}
	// stack[d] is the node a line of depth d+1 nests under.
	stack := []string{}
	counts := map[string]int{}
	var changes []TreeChange
	last := ""
	for i, line := range lines {
		d := depths[i]
		stack = stack[:min(len(stack), d)]
		id := t.newIDAvoiding(changes)
		var p string
		var idx int
		if d == 0 {
			p, idx = parent, index
			index++
		} else {
			p = stack[d-1]
			idx = counts[p]
			counts[p]++
		}
		changes = append(changes, TreeChange{Op: TreeAdd, ID: id, Parent: p, Index: idx, Text: line})
		stack = append(stack, id)
		last = id
	}
	err := t.commit(changes, last)
	_ = t.r.SetFocus(last)
	return err
}

// newIDAvoiding is newID that also skips ids queued in changes.
func (t *Tree) newIDAvoiding(changes []TreeChange) string {
	for {
		id := t.newID()
		if !slices.ContainsFunc(changes, func(c TreeChange) bool { return c.ID == id }) {
			return id
		}
	}
}

// expand opens a collapsed node, else steps onto its first child.
func (t *Tree) expand(c Context) error {
	id := t.target(c)
	if id == "" || !t.hasChildren(id) {
		return nil
	}
	if t.folded(id) {
		t.setCollapsed(id, false)
		return t.refresh(id)
	}
	for _, child := range t.children(id) {
		if t.list.shows(child) {
			if err := t.list.Select(child); err != nil {
				return err
			}
			return t.r.SetFocus(child)
		}
	}
	return nil
}

// collapses reports whether collapse acts on node id: an open node with
// children closes, a node with a parent steps onto it (or out of the
// zoom), the zoomed-on node steps the zoom out. A closed or childless
// top-level node has nothing to do, so the collapse verb is unavailable
// there (guard outline-collapses) and h falls through to the next row.
func (t *Tree) collapses(id string) bool {
	if id == "" {
		return false
	}
	if t.hasChildren(id) && !t.folded(id) {
		return true
	}
	return t.parentOf(id) != "" || t.zoom != "" && id == t.zoom
}

// expands reports whether expand acts on node id: it opens a closed node
// or steps onto an open one's first child; a leaf has nothing to do.
func (t *Tree) expands(id string) bool { return id != "" && t.hasChildren(id) }

// yieldsEdge reports whether the tree's list takes the h/l edge rows
// (edgeList) in direction step (-1 h, 1 l): the tree's own verb for that
// direction has nothing to do on the row and the tree.spill setting
// yields that direction. Then h/l scroll a tree's columns back to the edge
// and move the focus to the previous or next stop, as on a table (R5.11).
func (t *Tree) yieldsEdge(focus string, step int) bool {
	id := t.targetOf(focus)
	switch mode := t.r.TreeSpill(); {
	case step < 0:
		return mode != TreeSpillOff && !t.collapses(id)
	case step > 0:
		return mode == TreeSpillBoth && !t.expands(id)
	}
	return false
}

// collapse closes an open node, else steps onto its parent.
func (t *Tree) collapse(c Context) error {
	id := t.target(c)
	if id == "" {
		return nil
	}
	if t.hasChildren(id) && !t.folded(id) {
		t.setCollapsed(id, true)
		return t.refresh(id)
	}
	if out, err := t.stepOut(id); out || err != nil {
		return err
	}
	if p := t.parentOf(id); p != "" {
		if err := t.list.Select(p); err != nil {
			return err
		}
		return t.r.SetFocus(p)
	}
	return nil
}

// fail keeps a failure of background work (a timed write, a line closed
// because the focus left it) for Failure.
func (t *Tree) fail(err error) {
	if err != nil {
		t.failure = err
	}
}

// Failure is the last background failure, cleared when read: Deliver
// returns it for the message the tree handled, so it reaches the status.
func (t *Tree) Failure() error {
	err := t.failure
	t.failure = nil
	return err
}

// registerTrees defines the outline, outline-row and outline-line nouns: expand and
// collapse on every tree, the outline verbs on an editable one (guard
// tree-editable), and the open line's insert rows.
func registerTrees(r *Runtime) {
	r.DefineGuard("tree-zoom", func(r *Runtime, ev KeyEvent) bool {
		return r.treeOf(ev.Focus) != nil && r.TreeZoomMode() != TreeZoomOff
	})
	// outline-collapses: collapse would act on the row (collapses), so a
	// row with nothing to fold, no parent and no zoom lets h fall through.
	r.DefineGuard("outline-collapses", func(r *Runtime, ev KeyEvent) bool {
		t := r.treeOf(ev.Focus)
		return t != nil && t.collapses(t.targetOf(ev.Focus))
	})
	r.DefineGuard("tree-editable", func(r *Runtime, ev KeyEvent) bool {
		t := r.treeOf(ev.Focus)
		return t != nil && t.Editable()
	})
	// The structural verbs are available where the node they act on takes
	// the change by its Fixed (TreeNode.Fixed): a fixed root offers no
	// d d, a fixed array item no rename.
	for guard, op := range map[string]string{"outline-renamable": TreeRename, "outline-movable": TreeMove, "outline-deletable": TreeDelete} {
		r.DefineGuard(guard, func(r *Runtime, ev KeyEvent) bool {
			t := r.treeOf(ev.Focus)
			return t != nil && t.Editable() && t.allows(op, ev.Focus)
		})
	}
	on := func(run func(*Tree, Ctx) error) AppHandler {
		return func(c Ctx) Result {
			t := c.Runtime.treeOf(focusOf(c.Context))
			if t == nil {
				return Failed(errors.New("no focused tree"))
			}
			if err := run(t, c); err != nil {
				return Failed(err)
			}
			return Done(t.target(c.Context))
		}
	}
	edit := func(run func(*Tree, Ctx) error) AppHandler {
		return on(func(t *Tree, c Ctx) error {
			if !t.Editable() {
				return errors.New("tree is read-only")
			}
			return run(t, c)
		})
	}
	r.Define("ochazuke.outline").
		Verb("expand", on(func(t *Tree, c Ctx) error { return t.expand(c.Context) }), "Expand the node, or step onto its first child when it is open").Motion().
		Verb("collapse", on(func(t *Tree, c Ctx) error { return t.collapse(c.Context) }), "Collapse the node, or step onto its parent when it is closed; past a zoom's top it steps the zoom out to show the parent; unavailable on a row with nothing to do (a closed or childless top-level node outside a zoom), so h falls through").Motion().When("outline-collapses").
		Verb("fold", on(func(t *Tree, c Ctx) error {
			id := t.target(c.Context)
			if id == "" || !t.hasChildren(id) {
				return nil
			}
			t.setCollapsed(id, !t.folded(id))
			return t.refresh(id)
		}), "Expand or collapse the node").
		Verb("zoom-in", on(func(t *Tree, c Ctx) error { return t.zoomIn(c.Context) }), "Zoom on the node: only its subtree is drawn, its ancestors become the breadcrumb (tree.zoom.root: the node ends the breadcrumb, or stays as the top row); view state, never undone").When("tree-zoom").
		Verb("zoom-out", on(func(t *Tree, c Ctx) error { return t.ZoomOut() }), "Step the zoom out one level, onto the zoomed node's parent").When("tree-zoom").
		Verb("zoom-reset", on(func(t *Tree, c Ctx) error { return t.Zoom("") }), "Show every level again").When("tree-zoom").
		Verb("add", edit(func(t *Tree, c Ctx) error {
			selected, _ := c.Args["select"].(bool)
			return t.add(c.Context, StringArg(c.Args, "where", "below"), StringArg(c.Args, "text", ""), selected)
		}), "Add a line below the node (where: below, above, child) holding text and edit it; select opens it with the text selected, so typing replaces it; written at once").When("tree-editable").
		Verb("edit", edit(func(t *Tree, c Ctx) error {
			// Editing a line renames its node: without select, the
			// rename.select setting says whether it opens selected.
			selected, given := c.Args["select"].(bool)
			if !given {
				selected = t.r.RenameSelect()
			}
			return t.rename(t.target(c.Context), selected)
		}), "Edit (rename) the node's line, with its text selected (select; omitted, the rename.select setting decides) so typing replaces it; typing is written after a pause and when the line is left").When("outline-renamable").
		Verb("indent", edit(func(t *Tree, c Ctx) error { return t.shift(c.Context, true) }), "Nest the node under the node above it").When("outline-movable").
		Verb("outdent", edit(func(t *Tree, c Ctx) error { return t.shift(c.Context, false) }), "Move the node out to follow its parent").When("outline-movable").
		Verb("move", edit(func(t *Tree, c Ctx) error { return t.move(c.Context, IntArg(c.Args, "delta", 1)) }), "Swap the node with its next (delta 1) or previous (-1) sibling").When("outline-movable").
		Verb("delete", edit(func(t *Tree, c Ctx) error { return t.remove(c.Context) }), "Delete the node and its children; u brings them back").When("outline-deletable").
		Verb("undo", edit(func(t *Tree, c Ctx) error { return t.history(false) }), "Undo the last change; the undo is written like any change").When("tree-editable").
		Verb("redo", edit(func(t *Tree, c Ctx) error { return t.history(true) }), "Redo the last undone change").When("tree-editable").
		Verb("paste", edit(func(t *Tree, c Ctx) error { return t.paste(c.Context, StringArg(c.Args, "text", "")) }), "Add pasted lines below the node, nested by their indent").When("tree-editable").
		Verb("row-key", edit(func(t *Tree, c Ctx) error { return t.rowKey(c.Context, StringArg(c.Args, "key", "")) }), "Run what key does on the line's row in normal mode (the row's own rows first), then go on editing the line where the cursor was: tab and shift+tab on an open line indent and outdent it, or do what the application binds them to on the row").When("tree-editable").
		Verb("leave", edit(func(t *Tree, c Ctx) error { return t.closeLine(true) }), "Leave the line: it is written, never discarded").When("tree-editable").
		Verb("next", edit(func(t *Tree, c Ctx) error { return t.add(c.Context, "below", "", false) }), "Write the line and start a new one below it").When("tree-editable")
	params := map[string]map[string]Parameter{
		"add":     {"where": {Type: "string", Enum: []string{"below", "above", "child"}}, "text": {Type: "string", Description: "the new line's text, a placeholder when select is set"}, "select": {Type: "boolean", Description: "open the line with its text selected: typing replaces it, esc or a motion keeps it"}},
		"edit":    {"select": {Type: "boolean", Description: "open the line with its text selected: typing replaces it, esc or a motion keeps it; omitted, the rename.select setting decides"}},
		"move":    {"delta": {Type: "integer", Required: true}},
		"paste":   {"text": {Type: "string", Required: true}},
		"row-key": {"key": {Type: "string", Required: true, Description: "the key, as bindings spell it, to run on the row in normal mode"}},
	}
	for verb, p := range params {
		op := r.Operations["ochazuke.outline."+verb]
		op.Parameters = p
		r.Operations["ochazuke.outline."+verb] = op
	}
	r.Define(TreeRowNoun)
}
