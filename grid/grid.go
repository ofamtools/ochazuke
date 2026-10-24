// Package grid is a two-dimensional cursor component for ochazuke: a header
// and rows the application owns, a cursor and viewport in two axes, a visual
// selection in three shapes plus marks, and one instance per focused cell,
// parented to a row instance, parented to a column instance, parented to the
// grid, so the cell, row and column nouns all sit on the focus stack. The
// grid holds no domain data of its own: SetData hands it the application's
// slices and Refresh re-syncs instances and cursor after the application
// changed them. It is built on the public API only.
package grid

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"ochazuke"
)

// Ref addresses one cell by its row index in the application's rows and its
// column index in the header. Ids are positional and stable while the data
// does not move: r12, c2, r12:c2.
type Ref struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// ID is the cell instance id.
func (c Ref) ID() string { return RowID(c.Row) + ":" + ColID(c.Col) }

// RowID and ColID are the row and column instance ids.
func RowID(i int) string { return "r" + strconv.Itoa(i) }
func ColID(j int) string { return "c" + strconv.Itoa(j) }

// ParseID reads a cell, row or column id. Kind is "cell", "row" or "column".
func ParseID(id string) (ref Ref, kind string, ok bool) {
	row, col, cell := strings.Cut(id, ":")
	ref = Ref{Row: -1, Col: -1}
	parse := func(s string, prefix byte) (int, bool) {
		if len(s) < 2 || s[0] != prefix {
			return 0, false
		}
		n, err := strconv.Atoi(s[1:])
		return n, err == nil && n >= 0
	}
	switch {
	case cell:
		r, okR := parse(row, 'r')
		c, okC := parse(col, 'c')
		return Ref{Row: r, Col: c}, "cell", okR && okC
	case strings.HasPrefix(id, "r"):
		r, okR := parse(id, 'r')
		ref.Row = r
		return ref, "row", okR
	default:
		c, okC := parse(id, 'c')
		ref.Col = c
		return ref, "column", okC
	}
}

// Selection shapes: the global modes a visual selection is active in, set
// with mode.set like any other mode. The shape decides which cells the
// anchor and cursor span; marks add cells of any shape on top.
const (
	ShapeBlock  = "visual-block"
	ShapeRow    = "visual-row"
	ShapeColumn = "visual-column"
)

// Styles paints the grid. Lip Gloss always emits styles; tests strip them.
type Styles struct {
	Header   lipgloss.Style
	Cursor   lipgloss.Style
	Selected lipgloss.Style
	Marked   lipgloss.Style
	Gutter   lipgloss.Style
}

// DefaultStyles work without color: bold header, reverse cursor, underlined
// selection, bold underline for marks.
func DefaultStyles() Styles {
	return Styles{
		Header:   lipgloss.NewStyle().Bold(true),
		Cursor:   lipgloss.NewStyle().Reverse(true),
		Selected: lipgloss.NewStyle().Underline(true),
		Marked:   lipgloss.NewStyle().Bold(true).Underline(true),
		Gutter:   lipgloss.NewStyle().Faint(true),
	}
}

// Grid is the component of a grid instance.
type Grid struct {
	r       *ochazuke.Runtime
	id      string
	header  []string
	rows    [][]string
	filter  func(row []string) bool
	visible []int // row indices shown, in order
	vrow    int   // cursor row as an index into visible
	col     int
	offRow  int
	offCol  int
	anchor  Ref
	marks   map[Ref]bool
	live    []string // the column, row and cell instances of the focus, outermost first
	// Nouns are the nouns the focus instances answer to. Set them before the
	// first Refresh (New reads them from the options).
	Nouns  Nouns
	Styles Styles
	// MaxCellWidth caps a column's rendered width.
	MaxCellWidth int
}

// Nouns names the three nouns the focus instances are created with.
type Nouns struct{ Cell, Row, Column string }

// Option configures New.
type Option func(*Grid)

// WithNouns names the cell, row and column nouns (default cell, row, column).
func WithNouns(cell, row, column string) Option {
	return func(g *Grid) { g.Nouns = Nouns{Cell: cell, Row: row, Column: column} }
}

// New registers a grid instance of the grid noun under parent, defines the
// grid and selection operations once per runtime and creates the focus
// instances for the top-left cell when there is one.
func New(r *ochazuke.Runtime, id, parent string, header []string, rows [][]string, opts ...Option) (*Grid, error) {
	if id == "" || r.Instances[id] != nil {
		return nil, fmt.Errorf("grid id %q must be unique and nonempty", id)
	}
	if r.Instances[parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", parent)
	}
	g := &Grid{r: r, id: id, marks: map[Ref]bool{}, Nouns: Nouns{Cell: "cell", Row: "row", Column: "column"}, Styles: DefaultStyles(), MaxCellWidth: 24}
	for _, opt := range opts {
		opt(g)
	}
	Register(r)
	for _, noun := range []string{g.Nouns.Cell, g.Nouns.Row, g.Nouns.Column} {
		r.Define(noun)
	}
	r.Define("grid").Focusable()
	inst := r.Instance(id, ochazuke.Parent(parent), ochazuke.Kind("grid"))
	inst.Component = g
	g.SetData(header, rows)
	return g, nil
}

// Of returns the grid an instance fronts, or nil.
func Of(r *ochazuke.Runtime, id string) *Grid {
	if inst := r.Instances[id]; inst != nil {
		if g, ok := inst.Component.(*Grid); ok {
			return g
		}
	}
	return nil
}

// ID is the grid's instance id.
func (g *Grid) ID() string { return g.id }

// Header and Rows are the slices the application handed to SetData.
func (g *Grid) Header() []string  { return g.header }
func (g *Grid) Rows() [][]string  { return g.rows }
func (g *Grid) Visible() []int    { return g.visible }
func (g *Grid) VisibleCount() int { return len(g.visible) }

// SetData points the grid at the application's header and rows and refreshes.
// The grid keeps the slices, it does not copy them: call Refresh (or SetData
// again) after changing them in place.
func (g *Grid) SetData(header []string, rows [][]string) {
	g.header, g.rows = header, rows
	g.Refresh()
}

// SetFilter shows only the rows the predicate accepts (nil shows every row).
// A filter is view state, not an edit.
func (g *Grid) SetFilter(keep func(row []string) bool) {
	g.filter = keep
	g.Refresh()
}

// Refresh recomputes the visible rows, keeps the cursor on its row, drops
// marks that fell off the sheet and re-syncs the focus instances by id.
func (g *Grid) Refresh() {
	keep := -1
	if g.vrow >= 0 && g.vrow < len(g.visible) {
		keep = g.visible[g.vrow]
	}
	g.visible = g.visible[:0]
	for i, row := range g.rows {
		if g.filter == nil || g.filter(row) {
			g.visible = append(g.visible, i)
		}
	}
	// The cursor stays on its row when that row is still shown, else on the
	// nearest visible index.
	if v := g.visibleIndex(keep); v >= 0 {
		g.vrow = v
	}
	g.vrow = min(max(g.vrow, 0), max(0, len(g.visible)-1))
	g.col = min(max(g.col, 0), max(0, len(g.header)-1))
	for ref := range g.marks {
		if ref.Row >= len(g.rows) || ref.Col >= len(g.header) {
			delete(g.marks, ref)
		}
	}
	if g.anchor.Row >= len(g.rows) || g.anchor.Col >= len(g.header) {
		g.anchor = g.Cursor()
	}
	g.syncInstances()
}

// Cursor is the focused cell as a reference into the application's rows;
// Row is -1 when no row is visible.
func (g *Grid) Cursor() Ref {
	if len(g.visible) == 0 || len(g.header) == 0 {
		return Ref{Row: -1, Col: -1}
	}
	return Ref{Row: g.visible[g.vrow], Col: g.col}
}

// Position is the cursor as visible-row and column indexes.
func (g *Grid) Position() (vrow, col int) { return g.vrow, g.col }

// Move applies a motion with a count: left, right, up, down, row-start,
// row-end, top and bottom. Line, when positive, is the 1-based target row for
// top and bottom (5gg and 5G).
func (g *Grid) Move(motion string, steps, line int) error {
	steps = max(1, steps)
	switch motion {
	case "left":
		g.col -= steps
	case "right":
		g.col += steps
	case "up":
		g.vrow -= steps
	case "down":
		g.vrow += steps
	case "row-start":
		g.col = 0
	case "row-end":
		g.col = len(g.header) - 1
	case "top", "bottom":
		switch {
		case line > 0:
			g.vrow = line - 1
		case motion == "top":
			g.vrow = 0
		default:
			g.vrow = len(g.visible) - 1
		}
	default:
		return fmt.Errorf("unknown motion %q", motion)
	}
	g.Refresh()
	return nil
}

// Goto moves the cursor to a cell by reference; a hidden row is an error.
func (g *Grid) Goto(ref Ref) error {
	for v, row := range g.visible {
		if row == ref.Row {
			g.vrow, g.col = v, ref.Col
			g.Refresh()
			return nil
		}
	}
	return fmt.Errorf("row %d is not visible", ref.Row)
}

func isShape(mode string) bool {
	return mode == ShapeBlock || mode == ShapeRow || mode == ShapeColumn
}

// Shape is the active selection shape: the base mode when it is a shape and
// the focus sits in the grid (or in a modal opened over it), else "".
func (g *Grid) Shape() string {
	if mode := g.r.BaseMode(); isShape(mode) && slices.Contains(g.r.Ancestry(g.r.Focus), g.id) {
		return mode
	}
	return ""
}

// ModeChanged follows the global mode: entering a shape from outside one
// anchors the selection at the cursor, switching shapes keeps anchor and
// marks, and leaving the shapes drops the marks.
func (g *Grid) ModeChanged(from, to string) {
	switch {
	case isShape(to) && !isShape(from):
		g.anchor = g.Cursor()
	case !isShape(to) && isShape(from):
		g.marks = map[Ref]bool{}
	}
}

// Select starts a selection of shape at the cursor, or switches the shape of
// the active selection keeping its anchor and marks: mode.set to the shape.
func (g *Grid) Select(shape string) error {
	if !isShape(shape) {
		return fmt.Errorf("unknown selection shape %q", shape)
	}
	return g.r.SetMode(shape)
}

// Cancel ends the selection and drops its marks: mode.set normal.
func (g *Grid) Cancel() {
	if g.Shape() != "" {
		_ = g.r.SetMode("normal")
	}
	g.marks = map[Ref]bool{}
}

// SwapAnchor exchanges anchor and cursor.
func (g *Grid) SwapAnchor() error {
	if g.Shape() == "" {
		return nil
	}
	cursor := g.Cursor()
	if err := g.Goto(g.anchor); err != nil {
		return err
	}
	g.anchor = cursor
	return nil
}

// ToggleMark marks or unmarks the cells the current shape puts under the
// cursor: the cell, its row or its column.
func (g *Grid) ToggleMark() {
	cells := g.shapeCells(g.Cursor(), g.Cursor())
	for _, ref := range cells {
		if g.marks[ref] {
			delete(g.marks, ref)
		} else {
			g.marks[ref] = true
		}
	}
}

// ClearMarks drops the marks and keeps the selection.
func (g *Grid) ClearMarks() { g.marks = map[Ref]bool{} }

// SelectAll spans the whole visible sheet; without a selection it starts a
// block one.
func (g *Grid) SelectAll() {
	if len(g.visible) == 0 || len(g.header) == 0 {
		return
	}
	if g.Shape() == "" {
		_ = g.Select(ShapeBlock)
	}
	g.anchor = Ref{Row: g.visible[0], Col: 0}
	g.vrow, g.col = len(g.visible)-1, len(g.header)-1
	g.Refresh()
}

// Anchor is the selection anchor.
func (g *Grid) Anchor() Ref { return g.anchor }

// Marks lists the marked cells in row-major order.
func (g *Grid) Marks() []Ref { return sortRefs(g.marks) }

// Selected lists the cells of the selection: the range the shape spans
// between anchor and cursor, united with the marks, in row-major order. It
// is nil when no selection is active.
func (g *Grid) Selected() []Ref {
	if g.Shape() == "" {
		return nil
	}
	set := map[Ref]bool{}
	for _, ref := range g.shapeCells(g.anchor, g.Cursor()) {
		set[ref] = true
	}
	for ref := range g.marks {
		set[ref] = true
	}
	return sortRefs(set)
}

// Bounds is the rectangle the selection covers: the first and last visible
// row and the first and last column among its cells.
func Bounds(cells []Ref) (top, bottom, left, right int) {
	if len(cells) == 0 {
		return 0, -1, 0, -1
	}
	top, bottom, left, right = cells[0].Row, cells[0].Row, cells[0].Col, cells[0].Col
	for _, c := range cells {
		top, bottom = min(top, c.Row), max(bottom, c.Row)
		left, right = min(left, c.Col), max(right, c.Col)
	}
	return top, bottom, left, right
}

// shapeCells spans a and b by the active shape, over visible rows only.
func (g *Grid) shapeCells(a, b Ref) []Ref {
	if a.Row < 0 || b.Row < 0 {
		return nil
	}
	va, vb := g.visibleIndex(a.Row), g.visibleIndex(b.Row)
	if va < 0 {
		va = vb
	}
	if va < 0 {
		return nil
	}
	lo, hi := min(va, vb), max(va, vb)
	left, right := min(a.Col, b.Col), max(a.Col, b.Col)
	switch g.Shape() {
	case ShapeRow:
		left, right = 0, len(g.header)-1
	case ShapeColumn:
		lo, hi = 0, len(g.visible)-1
	}
	var out []Ref
	for v := lo; v <= hi; v++ {
		for c := left; c <= right; c++ {
			out = append(out, Ref{Row: g.visible[v], Col: c})
		}
	}
	return out
}

func (g *Grid) visibleIndex(row int) int {
	for v, r := range g.visible {
		if r == row {
			return v
		}
	}
	return -1
}

func sortRefs(set map[Ref]bool) []Ref {
	out := make([]Ref, 0, len(set))
	for ref := range set {
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Row != out[j].Row {
			return out[i].Row < out[j].Row
		}
		return out[i].Col < out[j].Col
	})
	return out
}

// Focus instances.

// focusTarget is the instance the focus should sit on: the cursor cell, or
// the grid when it is empty.
func (g *Grid) focusTarget() string {
	if cursor := g.Cursor(); cursor.Row >= 0 {
		return cursor.ID()
	}
	return g.id
}

// Focused reports whether the active frame points at the grid or its cell.
func (g *Grid) Focused() bool { return g.holds(g.r.Focus) }

func (g *Grid) holds(focus string) bool {
	if focus == g.id {
		return true
	}
	for _, id := range g.live {
		if id == focus {
			return true
		}
	}
	return false
}

// Focus points the focus at the cursor cell.
func (g *Grid) Focus() error { return g.r.SetFocus(g.focusTarget()) }

// syncInstances replaces the column, row and cell instances with the ones
// for the cursor and moves every focus reference that pointed into the grid
// (the focus, a page's remembered focus, a modal's return) to the new cell,
// as the list does for its selected row. Both moves stay inside the grid's
// focus stop, so the mode (a visual shape) is kept.
func (g *Grid) syncInstances() {
	var want []string
	cursor := g.Cursor()
	if cursor.Row >= 0 {
		want = []string{ColID(cursor.Col), RowID(cursor.Row), cursor.ID()}
	}
	same := len(want) == len(g.live)
	for i := 0; same && i < len(want); i++ {
		same = want[i] == g.live[i]
	}
	if !same {
		if len(g.live) > 0 {
			// Removing the outermost drops the row and cell beneath it and
			// points references into them at the grid.
			_ = g.r.RemoveInstance(g.live[0])
		}
		nouns := []string{g.Nouns.Column, g.Nouns.Row, g.Nouns.Cell}
		parent := g.id
		for i, id := range want {
			g.r.Instance(id, ochazuke.Parent(parent), ochazuke.Kind(nouns[i]))
			parent = id
		}
		g.live = want
	}
	g.r.RetargetFocus(func(id string) bool { return id == g.id }, g.focusTarget())
}

// At resolves the cell an operation addresses: the focused cell instance when
// the context points at one, else the cursor.
func (g *Grid) At(c ochazuke.Context) Ref {
	if len(c.Path) > 0 {
		if ref, kind, ok := ParseID(c.Path[len(c.Path)-1]); ok && kind == "cell" && ref.Row < len(g.rows) && ref.Col < len(g.header) {
			return ref
		}
	}
	return g.Cursor()
}

// The delivery contract: the grid schedules no work.

func (g *Grid) HandleMessage(tea.Msg) bool { return false }
func (g *Grid) PendingCommand() tea.Cmd    { return nil }

// Close drops the focus instances.
func (g *Grid) Close() {
	if len(g.live) > 0 && g.r.Instances[g.live[0]] != nil {
		_ = g.r.RemoveInstance(g.live[0])
	}
	g.live = nil
}

// Snapshot describes the grid for get.state.
func (g *Grid) Snapshot() map[string]any {
	return map[string]any{"id": g.id, "cursor": g.Cursor(), "shape": g.Shape(), "anchor": g.anchor, "marks": g.Marks(), "selected": g.Selected(), "visible": len(g.visible), "rows": len(g.rows), "columns": len(g.header), "focused": g.Focused()}
}

// Rendering.

// View renders the header and the visible rows into width x height cells,
// scrolling both axes to keep the cursor in view. Rows carry a 1-based
// gutter. The cursor cell is painted with Styles.Cursor, selected cells with
// Styles.Selected, marked cells with Styles.Marked.
func (g *Grid) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if len(g.header) == 0 {
		return pad("(empty sheet)", width)
	}
	widths := g.columnWidths()
	gutter := len(strconv.Itoa(len(g.rows))) + 1
	bodyRows := max(1, height-1)
	g.offRow = min(g.offRow, max(0, len(g.visible)-bodyRows))
	if g.vrow < g.offRow {
		g.offRow = g.vrow
	} else if g.vrow >= g.offRow+bodyRows {
		g.offRow = g.vrow - bodyRows + 1
	}
	avail := width - gutter - 1
	if g.col < g.offCol {
		g.offCol = g.col
	}
	for g.offCol < g.col && spanWidth(widths, g.offCol, g.col) > avail {
		g.offCol++
	}
	last := g.offCol
	for last+1 < len(widths) && spanWidth(widths, g.offCol, last+1) <= avail {
		last++
	}
	selected := map[Ref]bool{}
	for _, ref := range g.Selected() {
		selected[ref] = true
	}
	cursor := g.Cursor()
	shape := g.Shape()
	lines := make([]string, 0, height)
	var head strings.Builder
	head.WriteString(g.Styles.Gutter.Render(strings.Repeat(" ", gutter)))
	for c := g.offCol; c <= last; c++ {
		head.WriteString(" ")
		label := fit(g.header[c], widths[c])
		if shape == ShapeColumn && cursor.Row >= 0 && selected[Ref{Row: cursor.Row, Col: c}] {
			label = g.Styles.Selected.Render(label)
		}
		head.WriteString(g.Styles.Header.Render(label))
	}
	lines = append(lines, pad(head.String(), width))
	for v := g.offRow; v < len(g.visible) && len(lines) < height; v++ {
		row := g.visible[v]
		var b strings.Builder
		b.WriteString(g.Styles.Gutter.Render(fmt.Sprintf("%*d", gutter, row+1)))
		for c := g.offCol; c <= last; c++ {
			b.WriteString(" ")
			ref := Ref{Row: row, Col: c}
			text := fit(cellText(g.rows[row], c), widths[c])
			switch {
			case ref == cursor && g.Focused():
				text = g.Styles.Cursor.Render(text)
			case g.marks[ref]:
				text = g.Styles.Marked.Render(text)
			case selected[ref]:
				text = g.Styles.Selected.Render(text)
			}
			b.WriteString(text)
		}
		lines = append(lines, pad(b.String(), width))
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

func (g *Grid) columnWidths() []int {
	widths := make([]int, len(g.header))
	for c, name := range g.header {
		widths[c] = max(3, min(g.MaxCellWidth, ansi.StringWidth(name)))
	}
	for _, row := range g.visible {
		for c := range widths {
			widths[c] = max(widths[c], min(g.MaxCellWidth, ansi.StringWidth(cellText(g.rows[row], c))))
		}
	}
	return widths
}

func spanWidth(widths []int, from, to int) int {
	total := 0
	for c := from; c <= to; c++ {
		total += widths[c] + 1
	}
	return total
}

func cellText(row []string, c int) string {
	if c < len(row) {
		return row[c]
	}
	return ""
}

// fit pads or truncates a cell to width cells, replacing control characters.
func fit(s string, width int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r == '\r' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return '�'
		}
		return r
	}, s)
	s = ansi.Truncate(s, width, "…")
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

func pad(line string, width int) string {
	line = ansi.Truncate(line, width, "")
	if strings.ContainsRune(line, '\x1b') {
		line += "\x1b[0m"
	}
	return line + strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
}
