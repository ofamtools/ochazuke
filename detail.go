package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Detail is the component a detail instance fronts: a page's read-only
// detail region (the text beside or below a list about its selected row)
// that scrolls while the focus stays in the list. The application names
// the item the detail is about with the func it gives AddDetail (the
// selected row's id) and draws the text through Window; the framework
// keeps the offset. A new item starts at the top, checked on every Window
// and every scroll, so a scroll typed before the next frame lands on the
// new item; the same item keeps its offset while its text refreshes
// (clamped to the new length). J and K on
// the app noun (detail.scroll, counts work: 10J) scroll the current page's
// detail while it overflows and the focus is on the page outside it; a
// focused noun that binds J or K (the outline) keeps them, as inner rows
// answer first. A detail is not a focus stop.
type Detail struct {
	r  *Runtime
	id string
	// about names the item the detail shows now; item is the one the
	// offset belongs to; offset its first visible line;
	// total and height the wrapped line count and window of that frame;
	// widest its widest wrapped line.
	about         func() string
	item          string
	offset        int
	total, height int
	widest        int
}

// detailNoun is the noun of every detail instance.
const detailNoun = "ochazuke.detail"

// AddDetail registers a detail instance under its parent (Parent(page) or
// an instance beneath the page) and returns its component. item names what
// the detail shows now (the selected row's id); a different answer than
// the last one starts the detail at the top. A nil item never resets.
func (r *Runtime) AddDetail(id string, item func() string, opts ...InstanceOption) (*Detail, error) {
	if id == "" {
		return nil, errors.New("detail ID must be nonempty")
	}
	if r.Instances[id] != nil {
		return nil, fmt.Errorf("detail %q is already registered", id)
	}
	spec := instanceSpec{parent: "app", noun: detailNoun}
	for _, opt := range opts {
		opt(&spec)
	}
	if r.Instances[spec.parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", spec.parent)
	}
	d := &Detail{r: r, id: id, about: item}
	r.Instance(id, Parent(spec.parent), Kind(spec.noun)).Component = d
	r.recordDetail(d)
	return d, nil
}

// Detail returns the detail an instance fronts, or nil.
func (r *Runtime) Detail(id string) *Detail {
	if inst := r.Instances[id]; inst != nil {
		if d, ok := inst.Component.(*Detail); ok {
			return d
		}
	}
	return nil
}

// ID is the detail's instance id.
func (d *Detail) ID() string { return d.id }

// Window returns the lines of text the detail shows in a width by height
// window (text wrapped to width first when width > 0, so a line is a row
// on screen) and the position marker, "12–40 / 300", when the text
// overflows the window ("" when it fits).
func (d *Detail) Window(text string, width, height int) (body, marker string) {
	d.sync()
	text = strings.ReplaceAll(text, "\t", "    ")
	if width > 0 {
		text = ansi.Wrap(text, width, "")
	}
	lines := strings.Split(text, "\n")
	if text == "" {
		lines = nil
	}
	d.total, d.height = len(lines), max(0, height)
	d.widest = 0
	for _, line := range lines {
		d.widest = max(d.widest, ansi.StringWidth(line))
	}
	d.offset = d.clamp(d.offset)
	end := min(len(lines), d.offset+d.height)
	body = strings.Join(lines[d.offset:end], "\n")
	return body, d.Marker()
}

// Marker is the position marker of the last Window: first and last
// visible lines and the total, "12–40 / 300", or "" when the text fits.
func (d *Detail) Marker() string {
	if !d.Overflows() {
		return ""
	}
	return fmt.Sprintf("%d–%d / %d", d.offset+1, min(d.total, d.offset+d.height), d.total)
}

// Width is the widest line of the whole text the last Window measured
// (wrapped, so at most its width), not only of the lines in view: a panel
// sized from it keeps one width while the text scrolls.
func (d *Detail) Width() int { return d.widest }

// Overflows reports whether the last Window had more lines than it showed.
func (d *Detail) Overflows() bool { return d.height > 0 && d.total > d.height }

// Offset is the first visible line, from 0.
func (d *Detail) Offset() int { return d.offset }

// Item is the item the offset belongs to.
func (d *Detail) Item() string { return d.item }

// Scroll moves the window by lines (down when positive), clamped to the
// text the last Window measured, and returns the new offset. A new item
// starts at the top first.
func (d *Detail) Scroll(lines int) int {
	if d.sync() {
		// The new item's text is not measured yet: scroll freely, the
		// next Window clamps.
		d.offset = max(0, lines)
		return d.offset
	}
	d.offset = d.clamp(d.offset + lines)
	return d.offset
}

// MotionEdges is the first and the last line while the text overflows
// its window (CursorMotions): a detail has no cursor, so its edges move
// the window.
func (d *Detail) MotionEdges() []SpillEdge {
	if !d.Overflows() {
		return nil
	}
	return []SpillEdge{EdgeFirst, EdgeLast}
}

// MoveToEdge shows the text's first or last window, or with row the
// window starting on line row (1 the first) (CursorMotions).
func (d *Detail) MoveToEdge(edge SpillEdge, row int) {
	d.sync()
	switch {
	case row > 0:
		d.offset = d.clamp(row - 1)
	case edge == EdgeFirst:
		d.offset = 0
	case edge == EdgeLast:
		d.offset = d.clamp(d.total)
	}
}

// PageLines is the window's height while the text overflows it
// (CursorMotions).
func (d *Detail) PageLines() int {
	if !d.Overflows() {
		return 0
	}
	return d.height
}

// MovePage scrolls the window by lines (CursorMotions).
func (d *Detail) MovePage(lines int) { d.Scroll(lines) }

// sync starts a new item at the top and reports whether it was new.
func (d *Detail) sync() bool {
	if d.about == nil {
		return false
	}
	if item := d.about(); item != d.item {
		d.item, d.offset = item, 0
		return true
	}
	return false
}

func (d *Detail) clamp(offset int) int {
	return min(max(0, offset), max(0, d.total-d.height))
}

// pageDetail is the first detail on the page holding focus (depth first,
// children in registration order), else nil. It reads the page's recorded
// details rather than walking the page's subtree: the J/K guard runs on
// every frame's hints and every J/K, and a walk asking Children per node
// was quadratic in the number of rows.
func (r *Runtime) pageDetail(focus string) *Detail {
	var page *Page
	for _, id := range r.Ancestry(focus) {
		if page = r.PageOf(id); page != nil {
			break
		}
	}
	if page == nil {
		return nil
	}
	var best *Detail
	var bestPath []uint64
	live := page.details[:0]
	for _, d := range page.details {
		if inst := r.Instances[d.id]; inst == nil || inst.Component != d {
			continue // removed, or the id now fronts something else
		}
		live = append(live, d)
		path := r.seqPath(d.id, page.ID)
		if path != nil && (best == nil || slices.Compare(path, bestPath) < 0) {
			best, bestPath = d, path
		}
	}
	clear(page.details[len(live):])
	page.details = live
	return best
}

// recordDetail notes d on the page above it, when that page is registered;
// RegisterPage collects the details already beneath a page it registers.
func (r *Runtime) recordDetail(d *Detail) {
	for _, id := range r.Ancestry(d.id) {
		if page := r.PageOf(id); page != nil {
			page.details = append(page.details, d)
			return
		}
	}
}

// pageDetails is every detail beneath page, for RegisterPage.
func (r *Runtime) pageDetails(page string) []*Detail {
	var out []*Detail
	for _, inst := range r.Instances {
		if d, ok := inst.Component.(*Detail); ok && r.seqPath(inst.ID, page) != nil {
			out = append(out, d)
		}
	}
	return out
}

// seqPath is the registration stamps of the instances from page's child
// down to id, nil when page is not a proper ancestor of id. Comparing two
// paths orders their instances as a depth-first walk of page meets them.
func (r *Runtime) seqPath(id, page string) []uint64 {
	var out []uint64
	for _, one := range r.Ancestry(id) {
		if one == page {
			if len(out) == 0 {
				return nil
			}
			slices.Reverse(out)
			return out
		}
		out = append(out, r.Instances[one].seq)
	}
	return nil
}

// detailScrolls is the guard of the J/K rows: no modal is open, the page
// holding the focus has a detail that overflows, and the focus is not in
// that detail.
func detailScrolls(r *Runtime, ev KeyEvent) bool {
	if r.TopModal() != "" {
		return false
	}
	d := r.pageDetail(ev.Focus)
	if d == nil || !d.Overflows() {
		return false
	}
	for _, id := range r.Ancestry(ev.Focus) {
		if id == d.id {
			return false
		}
	}
	return true
}

func registerDetail(r *Runtime) {
	r.Define(detailNoun)
	r.DefineGuard("detail-scrolls", detailScrolls)
	r.Register(Operation{Verb: "scroll", Noun: detailNoun, Motion: true, Description: "Scroll the page's detail by lines times count without moving the focus",
		Parameters: map[string]Parameter{
			"lines": {Type: "integer", Description: "Lines to move, down when positive (default 1)"},
			"count": {Type: "integer", Description: "Repeat count (default 1)"},
			"id":    {Type: "string", Description: "The detail instance (default the first detail on the focused page)"},
		}, Result: "detail offset", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			d := r.Detail(StringArg(a, "id", ""))
			if StringArg(a, "id", "") == "" {
				d = r.pageDetail(r.BaseFocus())
			}
			if d == nil {
				return nil, errors.New("no detail on this page")
			}
			return d.Scroll(IntArg(a, "lines", 1) * max(1, IntArg(a, "count", 1))), nil
		}})
	// J and K on app (bindings/detail.json), so every page has them and an
	// inner noun's J or K (the outline's moves) answers first. One hint
	// row: J reads "J/K detail" while the detail overflows; K stays off the
	// footer.
}
