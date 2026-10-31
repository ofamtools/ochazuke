package ochazuke

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Tree zoom: deep in a tree the outer levels push the rows off the right
// edge. Zooming on a node draws only its subtree; the hidden ancestors are
// a breadcrumb (a line above the rows, or the header beside the mode), the
// nearest few named and dimmed, the rest folded into "…" (the nested-json
// 1-drill prototype shows the last two ancestors dimmed). Zoom is view
// state, like collapse: it never changes the nodes and never reaches the
// undo stack.

// The tree zoom settings, rows of settings/ochazuke.json.
const (
	// TreeZoomSetting is how a tree zooms: TreeZoomManual (the default)
	// only through the zoom verbs, TreeZoomAuto also when the selected
	// row's indent takes more than half the tree's width, TreeZoomOff
	// never (the verbs are unavailable and a zoom in place is dropped).
	TreeZoomSetting = "tree.zoom"
	// TreeZoomRootSetting is where the zoomed-on node goes:
	// TreeZoomRootCrumb (the default) ends the breadcrumb and its children
	// are the top rows; TreeZoomRootRow keeps it as the top row.
	TreeZoomRootSetting = "tree.zoom.root"
	// TreeBreadcrumbSetting is where a zoomed tree draws its breadcrumb:
	// TreeBreadcrumbLine (the default) a dimmed line above the rows,
	// TreeBreadcrumbTitle the header beside the mode (after the focus
	// display) while the tree holds the focus, TreeBreadcrumbOff nowhere.
	TreeBreadcrumbSetting = "tree.breadcrumb"
	// TreeBreadcrumbAncestorsSetting is how many of the nearest hidden
	// ancestors the breadcrumb names (default 2); farther ones fold into
	// "…". 0 names them all.
	TreeBreadcrumbAncestorsSetting = "tree.breadcrumb.ancestors"
	// TreeSpillSetting is which of a tree's h/l fall through to the list's
	// edge rows (scroll the columns, then move the focus to the previous
	// or next stop, R5.11) on a row where the tree's own verb has nothing
	// to do: TreeSpillLeft (the default) h only, TreeSpillBoth h and l (l
	// on a leaf), TreeSpillOff neither.
	TreeSpillSetting = "tree.spill"
)

// The tree zoom setting values.
const (
	TreeZoomManual      = "manual"
	TreeZoomAuto        = "auto"
	TreeZoomOff         = "off"
	TreeZoomRootCrumb   = "crumb"
	TreeZoomRootRow     = "row"
	TreeBreadcrumbLine  = "line"
	TreeBreadcrumbTitle = "title"
	TreeBreadcrumbOff   = "off"
	TreeSpillLeft       = "left"
	TreeSpillBoth       = "both"
	TreeSpillOff        = "off"
)

// treeAutoContext is how many levels above the selected row an automatic
// zoom keeps in view: it zooms on the selected row's grandparent.
const treeAutoContext = 2

// treeSetting is a string tree setting, fallback when it holds a value
// outside values.
func (r *Runtime) treeSetting(name, fallback string, values ...string) string {
	if v, _ := r.settingText(name); slices.Contains(values, v) {
		return v
	}
	return fallback
}

// TreeZoomMode is the tree.zoom setting: manual, auto or off.
func (r *Runtime) TreeZoomMode() string {
	return r.treeSetting(TreeZoomSetting, TreeZoomManual, TreeZoomManual, TreeZoomAuto, TreeZoomOff)
}

// TreeZoomRoot is the tree.zoom.root setting: crumb or row.
func (r *Runtime) TreeZoomRoot() string {
	return r.treeSetting(TreeZoomRootSetting, TreeZoomRootCrumb, TreeZoomRootCrumb, TreeZoomRootRow)
}

// TreeSpill is the tree.spill setting: left, both or off.
func (r *Runtime) TreeSpill() string {
	return r.treeSetting(TreeSpillSetting, TreeSpillLeft, TreeSpillLeft, TreeSpillBoth, TreeSpillOff)
}

// TreeBreadcrumb is the tree.breadcrumb setting: line, title or off.
func (r *Runtime) TreeBreadcrumb() string {
	return r.treeSetting(TreeBreadcrumbSetting, TreeBreadcrumbLine, TreeBreadcrumbLine, TreeBreadcrumbTitle, TreeBreadcrumbOff)
}

// TreeBreadcrumbAncestors is the tree.breadcrumb.ancestors setting: the
// hidden ancestors the breadcrumb names, 0 all of them.
func (r *Runtime) TreeBreadcrumbAncestors() int {
	v, _ := r.settingValue(TreeBreadcrumbAncestorsSetting)
	n, _ := v.(int)
	return max(n, 0)
}

// Zoomed is the node the tree is zoomed on, "" when it shows every level.
func (t *Tree) Zoomed() string { return t.zoom }

// Zoom draws only id's subtree (tree.zoom.root says whether id is the top
// row or ends the breadcrumb); "" shows every level again. The selection
// stays when it is inside the zoom, else moves to the first row in it.
// Zoom works whatever tree.zoom says; off only drops it at the next frame.
func (t *Tree) Zoom(id string) error {
	if id != "" && t.index(id) < 0 {
		return fmt.Errorf("unknown tree node %q", id)
	}
	if id != "" {
		t.setCollapsed(id, false)
	}
	t.zoom = id
	sel := ""
	if n, ok := t.Selected(); ok && t.inZoom(n.ID) {
		sel = n.ID
	} else if first := t.firstShown(); first != "" {
		sel = first
	}
	return t.refresh(sel)
}

// ZoomOut steps the zoom out one level, onto the zoomed node's parent (or
// shows every level when it is a root); the selection stays.
func (t *Tree) ZoomOut() error {
	if t.zoom == "" {
		return nil
	}
	return t.Zoom(t.parentOf(t.zoom))
}

// Breadcrumb is the zoom's path as the breadcrumb shows it, outermost
// first: the hidden ancestors (the nearest tree.breadcrumb.ancestors of
// them named, farther ones folded into one "…"), then the zoomed node
// when it is not a row (tree.zoom.root crumb). Nil when not zoomed.
func (t *Tree) Breadcrumb() []string {
	if t.zoom == "" || t.index(t.zoom) < 0 {
		return nil
	}
	var chain []string
	for p := t.parentOf(t.zoom); p != "" && t.index(p) >= 0; p = t.parentOf(p) {
		chain = append(chain, t.nodes[t.index(p)].Label)
	}
	slices.Reverse(chain)
	if n := t.r.TreeBreadcrumbAncestors(); n > 0 && len(chain) > n {
		chain = append([]string{"…"}, chain[len(chain)-n:]...)
	}
	if t.r.TreeZoomRoot() == TreeZoomRootCrumb {
		chain = append(chain, t.nodes[t.index(t.zoom)].Label)
	}
	return chain
}

// rootRow reports whether the zoomed node is drawn as the top row.
func (t *Tree) rootRow() bool { return t.r.TreeZoomRoot() == TreeZoomRootRow }

// zoomRange is the span of t.nodes the zoom shows, [from, to); the whole
// tree when not zoomed.
func (t *Tree) zoomRange() (from, to int) {
	i := t.index(t.zoom)
	if t.zoom == "" || i < 0 {
		return 0, len(t.nodes)
	}
	from = i + 1
	if t.rootRow() {
		from = i
	}
	return from, t.subtreeEnd(i)
}

// inZoom reports whether node id is a row of the zoom.
func (t *Tree) inZoom(id string) bool {
	i := t.index(id)
	from, to := t.zoomRange()
	return i >= from && i < to
}

// firstShown is the first visible node of the zoom, "" none.
func (t *Tree) firstShown() string {
	from, to := t.zoomRange()
	for _, n := range t.nodes[from:to] {
		if t.visible(n) {
			return n.ID
		}
	}
	return ""
}

// shownDepth is a node's indent level as drawn: its depth below the zoom.
func (t *Tree) shownDepth(id string) int {
	d := t.depth(id)
	if t.zoom == "" || t.index(t.zoom) < 0 {
		return d
	}
	base := t.depth(t.zoom)
	if !t.rootRow() {
		base++
	}
	return max(d-base, 0)
}

// fitZoom keeps the zoom valid before the rows are pushed: a zoomed node
// that is gone drops the zoom, the zoom steps out until selectID (when
// given) is a row of it, and a zoom showing no row steps out until it
// shows one.
func (t *Tree) fitZoom(selectID string) {
	if t.zoom != "" && t.index(t.zoom) < 0 {
		t.zoom = ""
	}
	if selectID != "" && t.index(selectID) >= 0 {
		for t.zoom != "" && !t.inZoom(selectID) {
			t.zoom = t.parentOf(t.zoom)
		}
	}
	for t.zoom != "" && t.firstShown() == "" {
		t.zoom = t.parentOf(t.zoom)
	}
}

// stepOut is h past the zoom's top: the target's parent is hidden by the
// zoom (or the target is a zoomed root with no parent), so the zoom steps
// out until the parent is a row, which is selected; true when it did.
func (t *Tree) stepOut(id string) (bool, error) {
	if t.zoom == "" {
		return false, nil
	}
	p := t.parentOf(id)
	if p == "" {
		if id != t.zoom {
			return false, nil
		}
		return true, t.Zoom("")
	}
	if t.inZoom(p) {
		return false, nil
	}
	for t.zoom != "" && !t.inZoom(p) {
		t.zoom = t.parentOf(t.zoom)
	}
	if err := t.refresh(p); err != nil {
		return true, err
	}
	return true, t.r.SetFocus(p)
}

// zoomIn zooms on the target: with tree.zoom.root crumb the target must
// have children and the first becomes the selection (focused when the
// target was); with row the target stays selected.
func (t *Tree) zoomIn(c Context) error {
	id := t.target(c)
	if id == "" || id == t.zoom {
		return nil
	}
	if !t.rootRow() {
		if !t.hasChildren(id) {
			return nil
		}
		focused := t.r.Focus == id
		first := t.children(id)[0]
		if err := t.Zoom(id); err != nil {
			return err
		}
		if err := t.list.Select(first); err != nil {
			return err
		}
		if focused {
			return t.r.SetFocus(first)
		}
		return nil
	}
	return t.Zoom(id)
}

// frameZoom applies the zoom setting at a frame of width cells: off drops
// a zoom in place; auto zooms on the selected row's grandparent when its
// indent takes more than half the width.
func (t *Tree) frameZoom(width int) {
	mode := t.r.TreeZoomMode()
	if mode == TreeZoomOff {
		if t.zoom != "" {
			_ = t.Zoom("")
		}
		return
	}
	if mode != TreeZoomAuto || width < 1 {
		return
	}
	n, ok := t.Selected()
	if !ok || ansi.StringWidth(t.prefix(n)) <= width/2 {
		return
	}
	root := n.ID
	for range treeAutoContext {
		root = t.parentOf(root)
	}
	if !t.rootRow() {
		root = t.parentOf(root)
	}
	if root == "" || root == t.zoom || (t.zoom != "" && !t.inZoom(root)) {
		return
	}
	focused := t.r.Focus == n.ID
	if err := t.Zoom(root); err == nil && focused {
		_ = t.r.SetFocus(n.ID)
	}
}

// crumbLine is the breadcrumb line for width cells, "" when there is none
// to draw: the ancestors dimmed (Styles.Muted), the zoomed node plain,
// cut from the left as the focus path is (FitFocusPath).
func (t *Tree) crumbLine(width int) string {
	if t.r.TreeBreadcrumb() != TreeBreadcrumbLine {
		return ""
	}
	levels := t.Breadcrumb()
	if len(levels) == 0 {
		return ""
	}
	line := FitFocusPath(levels, width)
	styles := t.r.Settings().page.styles
	if styles == nil {
		return line
	}
	s := styles()
	if t.rootRow() {
		return s.Muted.Render(line)
	}
	i := strings.LastIndex(line, focusStackSep)
	if i < 0 {
		return line
	}
	cut := i + len(focusStackSep)
	return s.Muted.Render(line[:cut]) + line[cut:]
}

// titleCrumb is the breadcrumb of the focused tree for the header
// (tree.breadcrumb title), nil when there is none.
func (r *Runtime) titleCrumb() []string {
	if r.TreeBreadcrumb() != TreeBreadcrumbTitle {
		return nil
	}
	if t := r.treeOf(r.BaseFocus()); t != nil {
		return t.Breadcrumb()
	}
	return nil
}

// Height is the lines the tree needs to draw every visible row without
// windowing (a table's header aside): its list's Height, plus the
// breadcrumb line while a zoom draws one. A host that sizes a tree to its
// rows sizes it with Height.
func (t *Tree) Height() int {
	h := t.list.Height()
	if t.r.TreeBreadcrumb() == TreeBreadcrumbLine && len(t.Breadcrumb()) > 0 {
		h++
	}
	return h
}
