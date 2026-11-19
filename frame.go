package ochazuke

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The frame (noun pages plan, lane 1): a noun's page (nouns/NOUN) as a
// site with a fixed nav. When pages.frame lists the noun its page has a
// body of one of three kinds, each a place the page goes back through:
//
//   - index: the noun's index list, drawn as wide as the page;
//   - show: the list drawn narrow as a rail on the left (the same list
//     instance, so its filter, sort, scope and cursor stay as they are),
//     and the rail's selected thing beside it: its props at the top in
//     show.prop-columns columns, then its related things, one boxed
//     section per kind (the page's related stack), show.section-columns
//     boxes to a row, fewer when narrow, in show.section-order first; the
//     focused box is show.focus-grow cells wider and drawn in the focused
//     panel's border;
//   - edit: the thing's edit form (the page's show form: commit on change,
//     u undoes) in the same body; leaving the form shows the thing again.
//
// enter on a related row (related-section.jump) moves the frame to that
// thing when pages.frame lists its noun: its page's show body, the rail
// filtered by the relation the row came through (an ordinary prop filter
// on the rail list: project = the project it came from), and - comes back
// to the page and row it left. Opened any other way (show, the palette),
// the rail is the noun's list as its store scopes it (Store.ListScope: the
// pinned project unless all), with no relation filter.

// FrameSetting lists the nouns whose page is a frame (* every stored noun);
// empty keeps today's index page, and show opens it with the thing selected.
const FrameSetting = "pages.frame"

// The show body's settings.
const (
	ShowPropColumnsSetting    = "show.prop-columns"
	ShowSectionColumnsSetting = "show.section-columns"
	ShowSectionOrderSetting   = "show.section-order"
	ShowHistorySetting        = "show.history"
	ShowFocusGrowSetting      = "show.focus-grow"
)

// frameNoun holds the frame's verbs (open, back, edit-here): the noun page's
// own noun, so they answer on the page and no command noun is added.
const frameNoun = indexNoun

// The frame's body kinds.
const (
	FrameIndex = "index"
	FrameShow  = "show"
	FrameEdit  = "edit"
)

// frameBody is what a body kind draws and does, as data: Rail draws the
// list narrow beside the body (else the list is the body), Sections
// unfolds the related sections it draws as boxes, Form draws the edit
// form, and Leave is the body a left form shows.
type frameBody struct {
	Rail, Sections, Form bool
	Leave                string
}

// frameBodies are the body kinds.
var frameBodies = map[string]frameBody{
	FrameIndex: {},
	FrameShow:  {Rail: true, Sections: true},
	FrameEdit:  {Rail: true, Form: true, Leave: FrameShow},
}

// FrameOnGuard holds on a noun page that is a frame.
const FrameOnGuard = "frame-on"

// frameRailWidth is the rail's width beside the body (Layout.Sidebar's).
const frameRailWidth = 28

// frameBoxMin is the narrowest a related box is drawn: a body narrower than
// the configured boxes draws fewer to a row.
const frameBoxMin = 24

// frameStep is a place the frame goes back to: a body, the thing it showed
// and the rail's prop filters then.
type frameStep struct {
	Body    string
	Key     string
	Filters []PropFilter
}

// framePlace is a noun page's frame: the body shown, the way back, the
// relation filter it put on the rail (removed when the frame is opened
// another way), the show body's scroll and whether its sections were
// unfolded.
type framePlace struct {
	Body   string
	back   []frameStep
	link   *PropFilter
	top    int
	opened bool
}

// FrameOn reports whether pages.frame lists the noun (or holds *).
func (r *Runtime) FrameOn(noun string) bool {
	noun = r.CanonicalNoun(noun)
	for _, name := range r.Settings().Strings(FrameSetting) {
		if name = strings.TrimSpace(name); name == "*" || r.CanonicalNoun(name) == noun {
			return true
		}
	}
	return false
}

// frameOf is the noun page id's data and its frame state, nil for another
// instance.
func (r *Runtime) frameOf(id string) (*indexPage, *framePlace) {
	inst := r.Instances[id]
	if inst == nil {
		return nil, nil
	}
	ref, ok := inst.Data.(*indexPage)
	if !ok {
		return nil, nil
	}
	return ref, &ref.frame
}

// framed reports whether the noun page id draws as a frame now.
func (r *Runtime) framed(id string) bool {
	ref, _ := r.frameOf(id)
	return ref != nil && r.FrameOn(ref.Noun)
}

// OpenFrame opens the noun's page as a frame on body (show or edit; index
// for the list) with the thing key selected in the rail. link, when given,
// is the relation filter the rail takes (enter on a related row); without
// one, a relation filter an earlier link put on the rail is taken off.
func (r *Runtime) OpenFrame(noun, key, body string, link *PropFilter) error {
	n := r.Nouns[r.CanonicalNoun(noun)]
	if n == nil {
		return r.OpenIndex(noun, key) // says there is no such noun
	}
	id := IndexPageID(n.Name)
	from := r.Page
	if r.PageOf(id) == nil {
		if err := r.addIndexPage(n, id); err != nil {
			return err
		}
	}
	ref, fs := r.frameOf(id)
	list := r.List(id + "/list")
	if ref == nil || list == nil {
		return r.OpenIndex(n.Name, key)
	}
	here := from == id
	switch {
	case !here:
		fs.back = nil
	case fs.bodyKind() != body || r.frameKey(id) != key || link != nil:
		fs.back = append(fs.back, frameStep{Body: fs.bodyKind(), Key: r.frameKey(id), Filters: list.PropFilters()})
	}
	// A link filters the rail by its relation; opened from another page
	// without one, the rail drops the relation filter an earlier link set.
	if link != nil || !here {
		if err := r.frameFilter(list, fs, link); err != nil {
			return err
		}
	}
	if !here {
		if err := r.OpenPage(id); err != nil {
			return err
		}
	}
	r.refreshFedIndex(id)
	if key != "" {
		_ = list.Select(id + "/list/" + key)
	}
	return r.enterBody(id, body)
}

// frameFilter puts link on the rail list in place of the relation filter
// the frame put there before (nil takes that one off): the user's own
// filters stay.
func (r *Runtime) frameFilter(list *Collection, fs *framePlace, link *PropFilter) error {
	filters := list.PropFilters()
	if fs.link != nil {
		filters = slices.DeleteFunc(filters, func(f PropFilter) bool {
			return f.Prop == fs.link.Prop && f.Op == fs.link.Op && fmt.Sprint(f.Value) == fmt.Sprint(fs.link.Value)
		})
	}
	fs.link = nil
	if link != nil {
		filters = slices.DeleteFunc(filters, func(f PropFilter) bool { return f.Prop == link.Prop })
		filters = append(filters, *link)
		l := *link
		fs.link = &l
	}
	if slices.EqualFunc(filters, list.PropFilters(), func(a, b PropFilter) bool { return fmt.Sprint(a) == fmt.Sprint(b) }) {
		return nil
	}
	return list.SetPropFilters(filters)
}

// bodyKind is the body shown, index when none is.
func (fs *framePlace) bodyKind() string {
	if fs.Body == "" {
		return FrameIndex
	}
	return fs.Body
}

// frameKey is the key of the rail's selected thing, "" for none.
func (r *Runtime) frameKey(id string) string {
	if list := r.List(id + "/list"); list != nil {
		if item, ok := list.Selected(); ok {
			return strings.TrimPrefix(item.ID, id+"/list/")
		}
	}
	return ""
}

// enterBody shows body on the frame id: the focus on the rail's row, or
// on the form's first field for a body that draws the form.
func (r *Runtime) enterBody(id, body string) error {
	ref, fs := r.frameOf(id)
	list := r.List(id + "/list")
	if ref == nil || list == nil {
		return nil
	}
	kind := frameBodies[body]
	fs.Body = body
	r.syncShowForms()
	if kind.Sections && !fs.opened {
		// The show body draws every section's things: unfold them once
		// (the index's sections start folded, index.related-collapsed).
		if rel := r.ShowFormOf(id + "/show").Related(); rel != nil {
			rel.sync()
			for _, s := range rel.sections {
				s.setCollapsed(false)
			}
			fs.opened = true
		}
	}
	if kind.Form {
		sf := r.ShowFormOf(id + "/show")
		if form := sf.Form(); form == nil || form.Closed() {
			sf.noun, sf.key = "", ""
			sf.Sync()
		}
		if form := sf.Form(); form != nil {
			r.changeFocus(r.focusTargetOf(form.FocusTarget()), focusMove{cause: focusSettle})
			return nil
		}
		// Nothing to edit with (no get and set): the thing stays shown.
		r.UI().Info(fmt.Sprintf("%s %s has no edit form (it needs a get and a set)", r.DisplayNoun(ref.Noun), r.frameKey(id)))
		fs.Body = kind.Leave
	}
	fs.top = 0
	r.changeFocus(list.focusTarget(), focusMove{cause: focusSettle})
	return nil
}

// frameBack goes back one place on the current frame (edit to show, show
// to index, a link's thing to the one before), else to the page this one
// was opened from (page.back).
func (r *Runtime) frameBack() error {
	id := r.Page
	_, fs := r.frameOf(id)
	list := r.List(id + "/list")
	if fs == nil || list == nil || len(fs.back) == 0 || !r.framed(id) {
		return r.pageBack()
	}
	step := fs.back[len(fs.back)-1]
	fs.back = fs.back[:len(fs.back)-1]
	if frameBodies[fs.Body].Form {
		if form := r.RecordEditor(id + "/form"); form != nil && !form.Closed() {
			_ = form.Leave()
		}
	}
	if fmt.Sprint(step.Filters) != fmt.Sprint(list.PropFilters()) {
		if err := list.SetPropFilters(step.Filters); err != nil {
			return err
		}
		fs.link = nil
	}
	if step.Key != "" {
		_ = list.Select(id + "/list/" + step.Key)
	}
	return r.enterBody(id, step.Body)
}

// syncFrame keeps the current frame in step after a request: a form that
// was left (esc, q) shows the body before it (Leave when there is none).
func (r *Runtime) syncFrame() {
	_, fs := r.frameOf(r.Page)
	if fs == nil || !frameBodies[fs.Body].Form {
		return
	}
	if form := r.RecordEditor(r.Page + "/form"); form != nil && !form.Closed() {
		return
	}
	if len(fs.back) > 0 {
		_ = r.frameBack()
		return
	}
	_ = r.enterBody(r.Page, frameBodies[fs.Body].Leave)
}

// frameView draws a framed noun page: the list alone (index), or the rail
// and the body side by side, stacked when narrow.
func frameView(r *Runtime, id string, ref *indexPage, list *Collection, layout Layout, heading string, width, height int) Block {
	fs := &ref.frame
	focused := list.DrawnFocused()
	kind := frameBodies[fs.bodyKind()]
	if !kind.Rail {
		w, h := PanelContentSize(width, height)
		return layout.PanelBlock(heading, Text(list.View(w, h)), width, height, focused)
	}
	stacked := width < layoutStackAt
	first := layout.Divide(frameRailWidth, width-1, PaneMinWidth)
	bw, bh := width-1-first, height
	rw, rh := PanelContentSize(first, height)
	if stacked {
		first = layout.Divide(min(list.Len()+2, height*2/5), height, PaneMinHeight)
		bw, bh = width, height-first
		rw, rh = PanelContentSize(width, first)
	}
	rail := Text(list.View(rw, rh))
	cw, ch := PanelContentSize(bw, bh)
	sf := r.ShowFormOf(id + "/show")
	title := r.DisplayNoun(ref.Noun)
	if item, ok := list.Selected(); ok {
		title = item.Label
	}
	var body Block
	side := SplitNone
	if focused {
		side = SplitLeft
	}
	if form := sf.Form(); kind.Form && form != nil && !form.Closed() {
		form.Styles = layout.Styles
		body, title = form.Block(cw, ch), "Edit · "+title
	} else {
		body = Text(r.showBody(fs, sf, layout.Styles, cw, ch))
	}
	if path := r.InstancePath(r.DrawnFocus(id)); slices.Contains(path, id+"/form") || slices.Contains(path, id+"/show/related") {
		side = SplitRight
	}
	return layout.panelPair(heading, rail, title, body, width, height, first, stacked, side)
}

// showBody is the show body: the props in columns, then the related
// sections boxed in rows, scrolled to keep the focused row in view.
func (r *Runtime) showBody(fs *framePlace, sf *ShowForm, s Styles, width, height int) string {
	lines := frameProps(r.showProps(sf), s, width, max(1, r.Settings().Int(ShowPropColumnsSetting)))
	focus := -1
	if rel := sf.Related(); rel != nil {
		grid, at := r.showBoxes(rel, s, width)
		if len(grid) > 0 {
			if at >= 0 {
				focus = len(lines) + 1 + at
			}
			lines = append(append(lines, ""), grid...)
		}
	}
	if focus >= 0 {
		fs.top = scrollToShow(fs.top, focus, focus+1, height, len(lines), 2)
	}
	fs.top = min(max(0, fs.top), max(0, len(lines)-height))
	return strings.Join(lines[fs.top:min(len(lines), fs.top+height)], "\n")
}

// showProps are the shown thing's props as label and value: the show
// form's fields (a choice drawn as its label, so a reference reads as the
// thing it names).
func (r *Runtime) showProps(sf *ShowForm) []ThingProp {
	form := sf.Form()
	if form == nil {
		return nil
	}
	var out []ThingProp
	for _, f := range form.fields {
		if f.Hidden {
			continue
		}
		value := f.text()
		for _, c := range f.choices {
			if c.ID == value && c.Label != "" {
				value = c.Label
			}
		}
		out = append(out, ThingProp{Name: f.label(), Value: value, Declared: true, Secret: f.Secret})
	}
	return out
}

// frameProps draws props as label and value in cols columns (fewer when
// narrow), down the first column, then the next.
func frameProps(props []ThingProp, s Styles, width, cols int) []string {
	if len(props) == 0 {
		return nil
	}
	cols = max(1, min(cols, len(props), (width+2)/(frameBoxMin+2)))
	colW := (width - 2*(cols-1)) / cols
	per := (len(props) + cols - 1) / cols
	labelW := 0
	for _, p := range props {
		labelW = max(labelW, ansi.StringWidth(p.Name))
	}
	labelW = min(labelW, colW/3)
	cell := func(p ThingProp) string {
		value := strings.Join(strings.Fields(p.Value), " ")
		switch {
		case p.Secret:
			value = "(secret)"
		case value == "":
			value = s.Muted.Render("–")
		}
		label := ansi.Truncate(p.Name, labelW, "…")
		label += strings.Repeat(" ", labelW-ansi.StringWidth(label))
		return fitCells(s.Muted.Render(label)+"  "+value, colW, "…")
	}
	lines := make([]string, per)
	for row := range per {
		var parts []string
		for c := range cols {
			if i := c*per + row; i < len(props) {
				parts = append(parts, cell(props[i]))
			}
		}
		lines[row] = strings.Join(parts, "  ")
	}
	return lines
}

// showBoxes draws the related stack's sections as boxes in rows of
// show.section-columns (fewer when narrow), and the line of the focus in
// them (-1 none). A box is its section's list at its natural height; the
// row's boxes share the tallest one's height; the focused box is
// show.focus-grow cells wider, taken from the others in its row, and drawn
// with the focused panel's border.
func (r *Runtime) showBoxes(rel *Related, s Styles, width int) ([]string, int) {
	rel.markDrawn()
	secs := rel.visible()
	if !r.Settings().Bool(ShowHistorySetting) {
		secs = slices.DeleteFunc(secs, func(sec *relatedSection) bool { return sec.ref.History })
	}
	if len(secs) == 0 {
		return nil, -1
	}
	cols := max(1, min(r.Settings().Int(ShowSectionColumnsSetting), (width+1)/(frameBoxMin+1)))
	grow := max(0, r.Settings().Int(ShowFocusGrowSetting))
	layout := Layout{Styles: s}
	var lines []string
	focus := -1
	for start := 0; start < len(secs); start += cols {
		row := secs[start:min(len(secs), start+cols)]
		widths := boxWidths(row, cols, width, grow)
		texts, rowH := make([]string, len(row)), 0
		for i, sec := range row {
			inner, _ := PanelContentSize(widths[i], 3)
			text, at := sec.draw(inner)
			texts[i] = text
			rowH = max(rowH, strings.Count(text, "\n")+1)
			if at >= 0 {
				focus = len(lines) + 1 + at
			}
		}
		boxes := make([][]string, len(row))
		for i, sec := range row {
			boxes[i] = strings.Split(layout.Panel("", texts[i], widths[i], rowH+2, sec.list.Focused()), "\n")
		}
		for y := range rowH + 2 {
			parts := make([]string, len(boxes))
			for i := range boxes {
				parts[i] = layoutRow(boxes[i], y, widths[i])
			}
			lines = append(lines, strings.Join(parts, " "))
		}
	}
	return lines, focus
}

// boxWidths are the widths of a row's boxes: width shared by cols, the
// focused box grow cells wider at its neighbours' cost (never below
// frameBoxMin), a short last row keeping the full rows' widths.
func boxWidths(row []*relatedSection, cols, width, grow int) []int {
	base := (width - (cols - 1)) / cols
	out := make([]int, len(row))
	for i := range out {
		out[i] = base
	}
	focused := slices.IndexFunc(row, func(s *relatedSection) bool { return s.list.Focused() })
	if focused < 0 || len(row) < 2 || grow == 0 {
		return out
	}
	share := min(grow/(len(row)-1), base-frameBoxMin)
	if share <= 0 {
		return out
	}
	for i := range out {
		if i == focused {
			out[i] += share * (len(row) - 1)
		} else {
			out[i] -= share
		}
	}
	return out
}

// frameLink is the rail filter a jump from section s takes to its thing:
// the referring prop equal to what names the section's thing (project =
// the project the row came from); nil for a section no prop filter
// matches (the thing's own references, a relation kind, the history, a
// list section).
func frameLink(s *relatedSection) *PropFilter {
	ref := s.ref
	if ref.Prop == "" || ref.own() || ref.Relation != "" || ref.History || ref.List || ref.Filter != "" || ref.Multi {
		return nil
	}
	return &PropFilter{Prop: ref.Prop, Value: s.value()}
}

// registerFrame registers the frame's verbs (on the noun page's own noun)
// and its guard.
func registerFrame(r *Runtime) {
	r.DefineGuard(FrameOnGuard, func(r *Runtime, ev KeyEvent) bool { return r.framed(r.pageOfFocus(ev.Focus)) })
	params := map[string]Parameter{
		"noun": {Type: "string", Required: true, Description: "The thing's noun"},
		"key":  {Type: "string", Description: "The thing to select in the rail"},
		"body": {Type: "string", Enum: []string{FrameShow, FrameEdit, FrameIndex}, Description: "The body: show (default), edit or index"},
	}
	r.Register(Operation{Key: KeyOf(frameNoun, "open"), Description: "Open a noun's page as a frame on a thing: its body show (props and related things), edit (its form) or index (the list)",
		Parameters: params, Positional: []string{"noun", "key", "body"}, HistoryKind: HistoryAction, Agent: AgentAllow, Result: "context",
		Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			return r.Context(), r.OpenFrame(StringArg(a, "noun", ""), StringArg(a, "key", ""), StringArg(a, "body", FrameShow), nil)
		}})
	r.Register(Operation{Key: KeyOf(frameNoun, "back"), Motion: true, HistoryKind: HistoryAction, Result: "context",
		Description: "Go back one place: on a frame edit to show, show to the index, a followed link to the thing before; else the page this one was opened from (page.back)",
		Run:         func(r *Runtime, c Context, a Arguments) (any, error) { return r.Context(), r.frameBack() }})
	r.Register(Operation{Key: KeyOf(frameNoun, "edit-here"), HistoryKind: HistoryAction, Agent: AgentAllow, Result: "context",
		Description: "Edit the frame's thing in place: its form in the body (commit on change, u undoes); leaving the form shows the thing again",
		Run: func(r *Runtime, c Context, a Arguments) (any, error) {
			ref, _ := r.frameOf(r.Page)
			if ref == nil {
				r.UI().Info("edit in place works on a noun's page (pages.frame)")
				return r.Context(), nil
			}
			return r.Context(), r.OpenFrame(ref.Noun, r.frameKey(r.Page), FrameEdit, nil)
		}})
}

// pageOfFocus is the page holding focus, "" for none.
func (r *Runtime) pageOfFocus(focus string) string {
	if page := r.pageAt(focus); page != nil {
		return page.ID
	}
	return ""
}
