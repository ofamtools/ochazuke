package diffview

import (
	"fmt"
	"slices"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/unixsuperhero/ochazuke"
)

// Layout chooses the visual arrangement; Auto uses Split at SplitWidth.
type Layout string

const (
	Auto              Layout = "auto"
	Unified           Layout = "unified"
	Split             Layout = "split"
	DefaultSplitWidth        = 140
	Noun                     = "ochazuke.diff"
	EventFile                = "diff.file"
)

// Option configures a view. Hosts map their settings to these options or
// the corresponding fields; there is no global diff setting or git state.
type Option func(*View)

func WithLayout(layout Layout) Option { return func(v *View) { v.Layout = layout } }
func WithHighlight(on bool) Option    { return func(v *View) { v.Highlight = on } }
func WithSplitWidth(width int) Option { return func(v *View) { v.SplitWidth = width } }

// WithFileChange receives a signed count after diff.file is emitted. The
// host loads the requested file and calls SetFile; the view never runs git.
func WithFileChange(fn func(delta int)) Option { return func(v *View) { v.OnFileChange = fn } }

type row struct{ hunk, left, right int }
type lineKey struct{ hunk, line int }

// View is a single focus stop displaying one immutable file snapshot. It
// implements ochazuke.Component and LineScroller. Call View each frame with
// the current Model.Styles; theme changes then apply without cached colours.
type View struct {
	r                                       *ochazuke.Runtime
	id                                      string
	file                                    File
	unified, split                          []row
	mates                                   [][]int
	rows                                    []row
	hunks                                   []int
	layout                                  Layout
	top, cursor, x, width, height, maxWidth int
	added, removed, digits                  int
	spans                                   map[lineKey][]Span
	// Layout defaults to Auto. Unknown values behave as Auto.
	Layout Layout
	// Highlight defaults true; false bypasses Chroma.
	Highlight bool
	// SplitWidth is the Auto threshold; nonpositive uses 140.
	SplitWidth   int
	OnFileChange func(delta int)
}

// New installs the diff noun once and creates an instance under parent.
func New(r *ochazuke.Runtime, id, parent string, file File, opts ...Option) (*View, error) {
	if id == "" || r.Instances[id] != nil {
		return nil, fmt.Errorf("diff id %q must be unique and nonempty", id)
	}
	if r.Instances[parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", parent)
	}
	Register(r)
	v := &View{r: r, id: id, Layout: Auto, Highlight: true, SplitWidth: DefaultSplitWidth}
	for _, opt := range opts {
		opt(v)
	}
	r.Instance(id, ochazuke.Parent(parent), ochazuke.Kind(Noun)).Component = v
	v.SetFile(file)
	return v, nil
}

// Of returns an instance's diff view, or nil.
func Of(r *ochazuke.Runtime, id string) *View {
	if i := r.Instances[id]; i != nil {
		v, _ := i.Component.(*View)
		return v
	}
	return nil
}
func (v *View) ID() string { return v.id }

// SetFile replaces the data and resets the viewport. The host may reuse or
// mutate its own slices afterward; the view owns its snapshot and caches.
func (v *View) SetFile(file File) {
	file.Hunks = slices.Clone(file.Hunks)
	for i := range file.Hunks {
		file.Hunks[i].Lines = slices.Clone(file.Hunks[i].Lines)
	}
	v.file = file
	v.added, v.removed, v.digits = file.Added, file.Removed, 1
	v.unified = nil
	v.split = nil
	v.mates = nil
	v.spans = map[lineKey][]Span{}
	v.top, v.cursor, v.x, v.maxWidth = 0, 0, 0, 0
	a, d := 0, 0
	for hi, h := range file.Hunks {
		v.digits = max(v.digits, len(strconv.Itoa(max(h.OldStart+max(0, h.OldCount-1), h.NewStart+max(0, h.NewCount-1)))))
		head := row{hi, -1, -1}
		v.unified = append(v.unified, head)
		v.split = append(v.split, head)
		kinds := make([]Kind, len(h.Lines))
		mates := make([]int, len(h.Lines))
		for i, l := range h.Lines {
			if l.Kind != Context && l.Kind != Add && l.Kind != Delete {
				l.Kind = Context
				v.file.Hunks[hi].Lines[i].Kind = Context
			}
			if l.Kind == Add {
				a++
			}
			if l.Kind == Delete {
				d++
			}
			kinds[i] = l.Kind
			mates[i] = -1
			v.unified = append(v.unified, row{hi, i, i})
		}
		for _, p := range Pairs(kinds) {
			v.split = append(v.split, row{hi, p.Left, p.Right})
			if p.Left >= 0 && p.Right >= 0 && p.Left != p.Right {
				mates[p.Left] = p.Right
				mates[p.Right] = p.Left
			}
		}
		v.mates = append(v.mates, mates)
	}
	if v.added == 0 {
		v.added = a
	}
	if v.removed == 0 {
		v.removed = d
	}
	v.rows = nil
	v.layout = ""
	v.selectLayout(v.width)
}
func (v *View) selectLayout(width int) {
	layout := v.Layout
	if layout != Split && layout != Unified {
		layout = Unified
		if width >= max(1, v.threshold()) {
			layout = Split
		}
	}
	if layout == v.layout {
		return
	}
	// Preserve the source line/hunk being read across responsive layout changes.
	at := row{-1, -1, -1}
	if len(v.rows) > 0 {
		at = v.rows[min(v.cursor, len(v.rows)-1)]
	}
	v.layout = layout
	v.rows = v.unified
	if layout == Split {
		v.rows = v.split
	}
	v.hunks = nil
	for i, r := range v.rows {
		if r.left < 0 && r.right < 0 {
			v.hunks = append(v.hunks, i)
		}
		if r.hunk == at.hunk && (at.left >= 0 && r.left == at.left || at.right >= 0 && r.right == at.right || at.left < 0 && at.right < 0 && r.left < 0 && r.right < 0) {
			v.cursor = i
			v.top = i
		}
	}
	v.clamp()
}
func (v *View) threshold() int {
	if v.SplitWidth > 0 {
		return v.SplitWidth
	}
	return DefaultSplitWidth
}
func (v *View) clamp() {
	v.top = min(max(0, v.top), max(0, len(v.rows)-max(1, v.height)))
	v.cursor = min(max(0, v.cursor), max(0, len(v.rows)-1))
}
func (v *View) CanScrollLines() bool { return v.height > 0 && len(v.rows) > v.height }
func (v *View) ScrollLines(lines int) {
	v.top += lines
	v.clamp()
	v.cursor = min(max(v.cursor, v.top), min(len(v.rows)-1, v.top+max(1, v.height)-1))
	v.cursor = max(0, v.cursor)
}

// Move advances logical display rows, following the framework half-page rule.
// As a text scroller it clamps at its edges; focus keys remain available.
func (v *View) Move(delta int) {
	if delta == 0 {
		return
	}
	target := min(max(0, v.cursor+delta), max(0, len(v.rows)-1))
	if target == v.cursor {
		return
	}
	want := 0
	if target < v.top {
		want = target - v.top
	} else if target >= v.top+max(1, v.height) {
		want = target - (v.top + max(1, v.height) - 1)
	}
	v.top += ochazuke.MotionScroll(want, v.height)
	v.clamp()
	v.cursor = min(max(target, v.top), v.top+max(1, v.height)-1)
}

// Hunk jumps between parsed hunk headers without wrapping. It is an explicit
// command, so it reaches its target directly even outside the current window.
func (v *View) Hunk(delta int) {
	if delta == 0 || len(v.hunks) == 0 {
		return
	}
	index, found := slices.BinarySearch(v.hunks, v.cursor)
	if delta > 0 {
		if !found {
			index--
		}
		index = min(len(v.hunks)-1, index+min(delta, len(v.hunks)))
	} else {
		index = max(0, index-max(1, min(abs(delta), len(v.hunks))))
	}
	at := v.hunks[index]
	v.cursor = at
	v.top = at
	v.clamp()
}
func (v *View) ScrollColumns(cells int) { v.x = min(max(0, v.x+cells), max(0, v.maxWidth-1)) }
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func (v *View) HandleMessage(tea.Msg) bool { return false }
func (v *View) PendingCommand() tea.Cmd    { return nil }
func (v *View) Close()                     { v.spans = nil }
func (v *View) Snapshot() map[string]any {
	return map[string]any{"layout": v.layout, "row": v.cursor, "top": v.top, "column": v.x, "hunks": len(v.file.Hunks), "rows": len(v.rows), "highlight": v.Highlight}
}
