package ochazuke

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The rail is a region beside or over a page's main area that lists what
// relates to the thing the page shows (on a terminal page, the pane: the
// links it printed, the docs it made, the decisions it waits on). What it
// lists is a related stack (RelatedSpec): its sections are the shown
// thing's referrers, split, grouped and listed as the spec says, each row
// the noun's own row with the noun's own keys. The rail adds where it is
// drawn and how wide: one look of two (full, float) by a setting, stepped
// down when the area is narrow (FitRail), and the ochazuke.rail verbs.
//
// An application draws it in two steps: Fit gives the main area's size
// (so a terminal behind it is sized to it), the application draws the
// main at that size, and Compose adds the rail. The rail is drawn after
// the main, so the focus reaches it in drawing order (} tab ctrl+w l from
// the main), and it is a focus region (the ) and ( jumps).

// RailNoun is the rail region's noun: its instances are rails, its verbs
// show, hide, size, pin and read them.
const RailNoun = "ochazuke.rail"

// The rail's settings (settings/rail.json), declared by AddRail.
const (
	RailShowSetting        = "rail.show"
	RailLookSetting        = "rail.look"
	RailWidthSetting       = "rail.width"
	RailMinMainSetting     = "rail.min-main"
	RailFloatHeightSetting = "rail.float-height"
	RailOrderSetting       = "rail.order"
	RailHiddenSetting      = "rail.hidden"
	RailCollapsedSetting   = "rail.collapsed"
	RailEmptySetting       = "rail.empty"
	RailCountsSetting      = "rail.counts"
	RailGroupsSetting      = "rail.groups"
	RailRefreshSetting     = "rail.refresh"
	RailEndsSetting        = "rail.ends"
)

var railSettings = []string{RailShowSetting, RailLookSetting, RailWidthSetting, RailMinMainSetting, RailFloatHeightSetting,
	RailOrderSetting, RailHiddenSetting, RailCollapsedSetting, RailEmptySetting, RailCountsSetting, RailGroupsSetting,
	RailRefreshSetting, RailEndsSetting}

// The rail's looks (RailFit.Look). Full and float are the setting's values;
// strip and only are what the width rules and pane.only make of them.
const (
	// RailLookFull is a bordered column at the right of the area: the
	// main is narrowed by its width and a one-cell gap.
	RailLookFull = "full"
	// RailLookFloat is a bordered panel over the main's top-right corner:
	// nothing is resized. Unfocused it shows the headings alone.
	RailLookFloat = "float"
	// RailLookStrip is one line of counts at the top of the main, for an
	// area too narrow for the float; focusing it draws the float.
	RailLookStrip = "strip"
	// RailLookOnly is the focused rail alone over the whole area
	// (ochazuke.pane.only, ctrl+w o).
	RailLookOnly = "alone"
)

// The width rules (copied from tuios's sidebarResponsiveWidth steps): a
// percent width is kept to RailMinWidth..RailMaxWidth cells, any width to
// at most 2/5 of the area; the float needs RailFloatMinWidth cells.
const (
	RailMinWidth      = 28
	RailMaxWidth      = 56
	RailFloatMinWidth = 24
)

// RailSpec describes a rail: its title and the stack it lists. The
// stack's setting names left empty take the rail's (rail.order,
// rail.hidden, rail.collapsed, rail.counts, rail.empty, rail.groups,
// rail.refresh, rail.ends; rail.show turns it off), and its sections draw
// as plain lists.
type RailSpec struct {
	// Title is the panel's title, default "rail".
	Title string
	// Stack is the related stack the rail lists: Target (or Noun and Key)
	// names the thing, Rows, GroupBy, Lists as for any stack.
	Stack RelatedSpec
}

// Rail is a rail region: an instance of RailNoun holding its related
// stack.
type Rail struct {
	r      *Runtime
	id     string
	title  string
	stack  *Related
	pinned string // noun:key the rail follows instead of the target (rail.pin)
}

// AddRail registers a rail under Parent (default app; give the page so
// pane.only and the page's focus rules reach it) and declares the rail
// settings the application has not declared first.
func (r *Runtime) AddRail(id string, spec RailSpec, opts ...InstanceOption) (*Rail, error) {
	if id == "" || r.Instances[id] != nil {
		return nil, fmt.Errorf("rail ID %q must be nonempty and unused", id)
	}
	r.declareFrameworkSettings(railSettings...)
	is := instanceSpec{parent: "app"}
	for _, opt := range opts {
		opt(&is)
	}
	if r.Instances[is.parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", is.parent)
	}
	st := spec.Stack
	for _, d := range []struct {
		name *string
		def  string
	}{{&st.Enabled, RailShowSetting}, {&st.Order, RailOrderSetting}, {&st.Hidden, RailHiddenSetting}, {&st.Collapsed, RailCollapsedSetting},
		{&st.Counts, RailCountsSetting}, {&st.Empty, RailEmptySetting}, {&st.Groups, RailGroupsSetting}, {&st.Interval, RailRefreshSetting}, {&st.Ends, RailEndsSetting}} {
		if *d.name == "" {
			*d.name = d.def
		}
	}
	st.Plain = true
	rl := &Rail{r: r, id: id, title: spec.Title}
	if rl.title == "" {
		rl.title = "rail"
	}
	target, noun, key := st.Target, st.Noun, st.Key
	st.Target = func() (string, string) {
		if n, k, ok := SplitAnyRef(rl.pinned); ok {
			return n, k
		}
		switch {
		case target != nil:
			return target()
		case key != nil:
			return noun, key()
		}
		return noun, ""
	}
	r.Instance(id, Parent(is.parent), Kind(RailNoun), FocusOnClose(is.closeFocus))
	r.Instances[id].Component = rl
	stack, err := r.AddRelated(id+"/stack", st, Parent(id))
	if err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	rl.stack = stack
	return rl, nil
}

// Rail is the rail with this id, or nil.
func (r *Runtime) Rail(id string) *Rail { return componentAt[*Rail](r, id) }

// ID is the rail's instance id.
func (rl *Rail) ID() string { return rl.id }

// Stack is the related stack the rail lists.
func (rl *Rail) Stack() *Related { return rl.stack }

// HandleMessage: the rail does no off-loop work.
func (rl *Rail) HandleMessage(tea.Msg) bool { return false }

// PendingCommand: nothing is ever scheduled.
func (rl *Rail) PendingCommand() tea.Cmd { return nil }

// Close: the stack under the rail closes with it.
func (rl *Rail) Close() {}

// Shown reports whether the rail is on (rail.show).
func (rl *Rail) Shown() bool { return rl.r.Settings().Bool(RailShowSetting) }

// RailFit is where a rail goes in an area of Width x Height cells: its
// look, the main area's outer size and first row, and the rail's outer
// size and place. Look "" draws no rail.
type RailFit struct {
	Look string
	// Narrow says the look stepped down from the one the setting asks for
	// (the full column did not fit).
	Narrow                bool
	Width, Height         int
	MainWidth, MainHeight int
	MainTop               int
	RailWidth, RailHeight int
	RailLeft, RailTop     int
}

// RailParams are what FitRail reads: the look and width settings' values,
// rail.min-main and rail.float-height, the rail body's natural height
// unfolded and as headings alone, and whether the rail holds the focus.
type RailParams struct {
	Look, Width          string
	MinMain, FloatHeight int
	Lines, HeadLines     int
	Focused              bool
}

// railWidth is the rail's preferred width in an area width cells wide:
// cells or a percent of width (kept to RailMinWidth..RailMaxWidth), an
// unreadable value the default 25%, and never over 2/5 of width.
func railWidth(setting string, width int) int {
	setting = strings.TrimSpace(setting)
	pct := 25
	cells := 0
	if p, ok := strings.CutSuffix(setting, "%"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n > 0 {
			pct = n
		}
	} else if n, err := strconv.Atoi(setting); err == nil && n > 0 {
		cells = n
	}
	if cells == 0 {
		cells = min(max(width*pct/100, RailMinWidth), RailMaxWidth)
	}
	return min(cells, width*2/5)
}

// FitRail places a rail in an area of width x height cells. The full look
// steps down rather than starve the main (tuios's sidebarResponsiveWidth
// steps): the preferred width while the main keeps MinMain cells, then
// RailMinWidth, then the float (Narrow), then, when even the float would
// be under RailFloatMinWidth cells, a strip of counts on the main's first
// row; a focused strip is drawn as the float. The float is as tall as
// what it shows (the headings alone unless focused), at most FloatHeight
// percent of the area.
func FitRail(width, height int, p RailParams) RailFit {
	fit := RailFit{Width: width, Height: height, MainWidth: width, MainHeight: height}
	if width <= 0 || height <= 0 {
		return fit
	}
	if p.Look == RailLookOnly {
		fit.Look, fit.RailWidth, fit.RailHeight, fit.MainWidth, fit.MainHeight = RailLookOnly, width, height, 0, 0
		return fit
	}
	pref := railWidth(p.Width, width)
	full := func(w int, narrow bool) RailFit {
		fit.Look, fit.Narrow = RailLookFull, narrow
		fit.RailWidth, fit.RailHeight, fit.RailLeft = w, height, width-w
		fit.MainWidth = width - w - 1
		return fit
	}
	fits := func(w int) bool { return w > 0 && width-w-1 >= p.MinMain }
	if p.Look != RailLookFloat {
		switch {
		case fits(pref):
			return full(pref, false)
		case pref > RailMinWidth && fits(RailMinWidth):
			return full(RailMinWidth, true)
		}
		fit.Narrow = true
	}
	fw := pref
	if fw < RailFloatMinWidth {
		if !p.Focused || width < 5 {
			fit.Look, fit.MainTop, fit.MainHeight = RailLookStrip, 1, max(0, height-1)
			fit.RailWidth, fit.RailHeight = width, 1
			fit.Narrow = true
			return fit
		}
		fw, fit.Narrow = min(max(pref, RailFloatMinWidth), width-2), true
	}
	lines := p.HeadLines
	if p.Focused {
		lines = p.Lines
	}
	most := max(3, height*max(1, p.FloatHeight)/100)
	fit.Look = RailLookFloat
	fit.RailWidth = fw
	fit.RailHeight = min(max(lines+2, 3), most, height)
	fit.RailLeft = width - fw
	return fit
}

// Fit places the rail in an area of width x height cells, by the rail
// settings and the focus: nothing while rail.show is off (and then
// nothing is read), the focused rail alone under pane.only.
func (rl *Rail) Fit(width, height int) RailFit { return rl.FitWith(width, height, "") }

// FitWith is Fit with a look in place of rail.look's (an application
// fitting the rail over a zoomed terminal as a float); "" reads the
// setting.
func (rl *Rail) FitWith(width, height int, look string) RailFit {
	r := rl.r
	if !rl.Shown() {
		return RailFit{Width: width, Height: height, MainWidth: width, MainHeight: height}
	}
	if look == "" {
		look = r.Settings().String(RailLookSetting)
	}
	if page := r.pageAt(rl.id); page != nil && page.only != "" && slices.Contains(r.Ancestry(page.only), rl.id) {
		look = RailLookOnly
	}
	lines, heads := rl.stack.lineCounts()
	return FitRail(width, height, RailParams{Look: look, Width: r.Settings().String(RailWidthSetting),
		MinMain: r.Settings().Int(RailMinMainSetting), FloatHeight: r.Settings().Int(RailFloatHeightSetting),
		Lines: lines, HeadLines: heads, Focused: r.focusInside(rl.id)})
}

// lineCounts are the drawn sections' lines: every row, and the headings
// alone.
func (rel *Related) lineCounts() (all, heads int) {
	rel.sync()
	for _, s := range rel.visible() {
		n := s.list.Len()
		if len(s.list.columns) > 0 {
			n++
		}
		all += n
		if s.group == nil {
			heads++
		}
	}
	return all, heads
}

// Compose draws the rail where fit puts it and joins it with main, the
// main area already drawn at fit's MainWidth x MainHeight: a column right
// of it (full), a panel over its top-right corner (float, through the
// non-modal OverlayBlock), a line of counts above it (strip), or the rail
// alone (only). The rail is drawn after the main, so its sections are
// placed after the main's stops. Drawing reads only what the stack holds.
func (rl *Rail) Compose(l Layout, fit RailFit, main Block) Block {
	r := rl.r
	if page := r.pageAt(rl.id); page != nil && page.rendering && fit.Look != "" {
		page.splitting = true // ctrl+w o (pane.only) answers while a rail is drawn
	}
	focused := r.focusInside(rl.id)
	switch fit.Look {
	case RailLookOnly:
		return rl.panel(l, fit, fit.RailWidth, fit.RailHeight, true, false)
	case RailLookFull:
		rail := rl.panel(l, fit, fit.RailWidth, fit.RailHeight, focused, false).Offset(fit.MainWidth+1, 0)
		mainRows, railRows := strings.Split(main.Text, "\n"), strings.Split(rail.Text, "\n")
		rows := make([]string, fit.Height)
		for i := range rows {
			rows[i] = layoutRow(mainRows, i, fit.MainWidth) + " " + layoutRow(railRows, i, fit.RailWidth)
		}
		cursor := firstCursor(main, rail)
		if focused {
			cursor = firstCursor(rail, main)
		}
		return Block{Text: strings.Join(rows, "\n"), Cursor: cursor}
	case RailLookFloat:
		rail := rl.panel(l, fit, fit.RailWidth, fit.RailHeight, focused, !focused)
		return OverlayBlock(main, rail, fit.RailLeft, fit.RailTop, fit.Width, fit.Height, focused)
	case RailLookStrip:
		strip := rl.strip(l, fit.Width)
		return Block{Text: strip + "\n" + strings.Join(layoutRows(main.Text, fit.Width, fit.MainHeight), "\n"), Cursor: main.Offset(0, 1).Cursor}
	}
	return main
}

// panel is the rail's bordered panel: its sections, or (heads) their
// headings alone, each section still placed so the focus can enter it.
func (rl *Rail) panel(l Layout, fit RailFit, w, h int, focused, heads bool) Block {
	cw, ch := PanelContentSize(w, h)
	var body string
	if heads {
		body = rl.stack.headingsText(cw)
	} else {
		body = rl.stack.View(cw, ch)
	}
	title := rl.title
	if fit.Narrow && fit.Look == RailLookFloat {
		title += " · float (narrow)"
	}
	return l.PanelBlock(title, Text(body), w, h, focused)
}

// headingsText is the drawn sections' headings, one a line, width cells
// wide; each section is placed (Runtime.Placed) so } and tab reach it.
func (rel *Related) headingsText(width int) string {
	rel.sync()
	var lines []string
	for _, s := range rel.headed() {
		rel.r.Placed(s.list.id)
		lines = append(lines, ansi.Truncate(s.headLabel(), width, "…"))
	}
	return strings.Join(lines, "\n")
}

// strip is the strip look's line: each drawn heading's label and count,
// and the key that shows the rail whole (the binding of
// ochazuke.rail.enter, else ochazuke.rail.toggle). The first section is
// placed so } and tab reach it, which draws the float.
func (rl *Rail) strip(l Layout, width int) string {
	r := rl.r
	rel := rl.stack
	rel.sync()
	var parts []string
	for i, s := range rel.headed() {
		if i == 0 {
			r.Placed(s.list.id)
		}
		label := strings.TrimLeft(s.ref.Label, "▾▸ ")
		parts = append(parts, fmt.Sprintf("%s %d", strings.ToLower(label), s.count))
	}
	text := rl.title + ": " + strings.Join(parts, " · ")
	if len(parts) == 0 {
		text = rl.title + ": " + EmptyText("things")
	}
	if key := rl.bindingKey(); key != "" {
		text += " · " + key
	}
	return l.Styles.Muted.Render(fitCells(ansi.Truncate(text, width, "…"), width, ""))
}

// bindingKey is the key the focus answers with ochazuke.rail.enter (else
// toggle), "" when none.
func (rl *Rail) bindingKey() string {
	r := rl.r
	found := map[string]string{}
	for _, row := range r.ListBindings(r.Focus) {
		key := r.CanonicalKey(row.Request.Key())
		if (key == RailNoun+".enter" || key == RailNoun+".toggle") && row.Keys != "" && found[key] == "" {
			found[key] = row.Keys
		}
	}
	if k := found[RailNoun+".enter"]; k != "" {
		return k
	}
	return found[RailNoun+".toggle"]
}
