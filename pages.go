package ochazuke

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Page is the component a page instance fronts: a title, the instance it
// starts focused on (Focus), the focus it remembers while another page is
// open (Remembered, restored with its mode by OpenPage), and a renderer. Render returns the
// page body for the given cell bounds as a Block; its cursor, when the page
// holds the focused input, is relative to the body's top-left cell. Pages
// without inputs return Text(body).
type Page struct {
	ID         string                         `json:"id"`
	Title      string                         `json:"title"`
	Focus      string                         `json:"focus"`
	Remembered string                         `json:"remembered"`
	Render     func(*Runtime, int, int) Block `json:"-"`
	// OnOpen, when set, runs on the loop each time OpenPage makes this page
	// current from another page (or from none: the first page opened), after
	// the focus and mode are restored; from is the page left, "" for none.
	// Reopening the current page does not run it. An application refreshes
	// what the page shows here (SetItems, or Launch a reload job owned by
	// one of its instances) instead of polling. Every way a page opens goes
	// through OpenPage: page.open, page.next/previous, ctrl+j/k, the page
	// picker.
	OnOpen func(r *Runtime, from string) `json:"-"`
	// Scrolloff is how many lines the page keeps visible above and below
	// the focused element when it scrolls to follow the focus: 0 means the
	// default, 2; a negative value keeps no margin.
	Scrolloff int `json:"scrolloff,omitempty"`
	mode      ModeState
	// visited orders the page picker: the stamp of the last OpenPage that
	// made this page current (larger is more recent), 0 for never.
	visited uint64
	// drawn is the order the last frame drew the page's focus stops in
	// (Placed calls during Render), with where each landed in the body;
	// drawing collects the frame in progress.
	drawn, drawing []Placement
	rendering      bool
	// split is set when the last frame drew a list/detail split through
	// Runtime.PaneLayout (splitting collects the frame in progress); the
	// pane keys answer only then.
	split, splitting bool
}

// Placement is one Placed call of a frame: the instance drawn and, when its
// view reported them (PlacedAt), the lines it covers in the page body and
// the lines inside it that hold the focus (a list's selected row, a grid's
// cursor row, an input's whole box, a terminal's top line).
type Placement struct {
	ID string `json:"id"`
	// Line is the body line the view's first line landed on, -1 when the
	// view reported no text or the page did not draw it; Height is its line
	// count.
	Line   int `json:"line"`
	Height int `json:"height"`
	// Focus and FocusHeight are the focused lines, relative to Line.
	Focus       int `json:"focus"`
	FocusHeight int `json:"focusHeight"`
}

// placeMark opens the zero-width marker PlacedAt puts on a view's first
// line (an APC string, which width and truncation treat as no cells), so
// the page finds the line the view landed on however it was composed; the
// page strips every marker before the body is shown.
const placeMark, placeEnd = "\x1b_ochazuke-placed:", "\x1b\\"

// render draws the page through Render and records the order its stops
// were drawn in, which is the order focus jumps and list spills walk, and
// the body line each view landed on.
func (p *Page) render(r *Runtime, width, height int) Block {
	p.rendering, p.drawing, p.splitting = true, nil, false
	defer func() {
		p.rendering, p.drawn, p.drawing = false, p.drawing, nil
		p.split, p.splitting = p.splitting, false
	}()
	block := p.Render(r, width, height)
	return block.WithText(p.resolve(block.Text))
}

// resolve sets each marked placement's Line from where its marker sits in
// the body and returns the body without markers.
func (p *Page) resolve(text string) string {
	if !strings.Contains(text, placeMark) {
		return text
	}
	var out strings.Builder
	line := 0
	for {
		at := strings.Index(text, placeMark)
		if at < 0 {
			out.WriteString(text)
			return out.String()
		}
		line += strings.Count(text[:at], "\n")
		out.WriteString(text[:at])
		text = text[at+len(placeMark):]
		end := strings.Index(text, placeEnd)
		if end < 0 {
			return out.String()
		}
		if n, err := strconv.Atoi(text[:end]); err == nil && n >= 0 && n < len(p.drawing) && p.drawing[n].Line < 0 {
			p.drawing[n].Line = line
		}
		text = text[end+len(placeEnd):]
	}
}

// scrolloff is the margin the page keeps around the focused element.
func (p *Page) scrolloff() int {
	switch {
	case p.Scrolloff == 0:
		return 2
	case p.Scrolloff < 0:
		return 0
	}
	return p.Scrolloff
}

// focusLines are the body lines [top, bottom) holding the focus in the last
// frame: the focused part of the nearest placed instance on the focus path.
func (p *Page) focusLines(r *Runtime, focus string) (top, bottom int, ok bool) {
	for _, id := range r.Ancestry(focus) {
		for _, at := range p.drawn {
			if at.ID == id && at.Line >= 0 {
				return at.Line + at.Focus, at.Line + at.Focus + max(1, at.FocusHeight), true
			}
		}
	}
	return 0, 0, false
}

// Placed records that the instance id is being drawn now, during its page's
// render: the order of these calls in a frame is the page's drawing order,
// which focus jumps (focus.next, focus.previous) and list spills walk, so
// motions follow the screen as drawn and cannot drift from it. List, Input
// (InputBlock), Terminal and grid views call it; a component of the
// application's own calls it from its view. Outside a page render it does
// nothing. There is no other way to order stops: before the first frame the
// order is the tree's, and once a frame drew any stop, a stop the last
// frame did not draw is skipped (it is not on screen). A stop drawn by an
// application view that never calls Placed is therefore skipped too.
func (r *Runtime) Placed(id string) {
	if page := r.renderingPage(id); page != nil {
		page.drawing = append(page.drawing, Placement{ID: id, Line: -1})
	}
}

// PlacedAt records where the view text of instance id holds the focus
// (lines focus to focus+height of text; a list's selected row, an input's
// whole box) and returns text marked so its page finds the body line it
// lands on, which is how the page scrolls to keep the focus visible. Call
// it with the view's final text after Placed; it amends that placement (or
// places id, if Placed was not called). Outside a page render it returns
// text unchanged. List, Input, Terminal and grid views call it.
func (r *Runtime) PlacedAt(id, text string, focus, height int) string {
	page := r.renderingPage(id)
	if page == nil || text == "" {
		return text
	}
	n := len(page.drawing) - 1
	for n >= 0 && page.drawing[n].ID != id {
		n--
	}
	if n < 0 {
		page.drawing = append(page.drawing, Placement{ID: id, Line: -1})
		n = len(page.drawing) - 1
	}
	at := &page.drawing[n]
	at.Height, at.Focus, at.FocusHeight = strings.Count(text, "\n")+1, focus, height
	return placeMark + strconv.Itoa(n) + placeEnd + text
}

// renderingPage is the page holding id while that page renders, else nil.
func (r *Runtime) renderingPage(id string) *Page {
	if r == nil {
		return nil
	}
	for _, at := range r.Ancestry(id) {
		if page := r.PageOf(at); page != nil {
			if page.rendering {
				return page
			}
			return nil
		}
	}
	return nil
}

// AddPage registers a page instance of kind page under app with a title and
// a renderer. Options: Focus(id) names where the page starts (the page
// itself or an instance beneath it, registered first) and Kind(noun) a noun
// of the application's own. Registration does not activate the page; see
// OpenPage. It is RegisterPage for callers that do not hold a *Page.
func (r *Runtime) AddPage(id, title string, render func(*Runtime, int, int) Block, opts ...InstanceOption) (*Page, error) {
	spec := instanceSpec{parent: "app", noun: "page"}
	for _, opt := range opts {
		opt(&spec)
	}
	if spec.parent != "app" {
		return nil, fmt.Errorf("page %q must be registered under app", id)
	}
	page := &Page{ID: id, Title: title, Focus: spec.focus, Render: render}
	if err := r.RegisterPage(page, Kind(spec.noun)); err != nil {
		return nil, err
	}
	return page, nil
}

// RegisterPage registers a page as an instance of kind page under app (the
// instance may already exist, registered through Instance with the inputs
// and lists it holds). Focus names where the page starts: the page itself or
// an instance beneath it. Registration does not activate the page.
func (r *Runtime) RegisterPage(page *Page, opts ...InstanceOption) error {
	if page == nil || strings.TrimSpace(page.ID) == "" || strings.TrimSpace(page.Title) == "" {
		return errors.New("page ID and title must be nonempty")
	}
	if r.PageOf(page.ID) != nil {
		return fmt.Errorf("page ID %q is already registered", page.ID)
	}
	for _, other := range r.PageList() {
		if other.Title == page.Title {
			return fmt.Errorf("page title %q is already registered", page.Title)
		}
	}
	if page.Render == nil {
		return errors.New("page renderer is required")
	}
	if page.Focus == "" {
		page.Focus = page.ID
	}
	if inst := r.Instances[page.ID]; inst != nil {
		if inst.Parent != "app" {
			return fmt.Errorf("page %q must be registered under app", page.ID)
		}
	} else {
		path := r.InstancePath(page.Focus)
		if page.Focus != page.ID && (len(path) < 2 || path[1] != page.ID) {
			return fmt.Errorf("page focus %q must sit beneath page %q", page.Focus, page.ID)
		}
	}
	if page.Focus != page.ID {
		if path := r.InstancePath(page.Focus); len(path) < 2 || path[1] != page.ID {
			return fmt.Errorf("page focus %q must sit beneath page %q", page.Focus, page.ID)
		}
	}
	inst := r.Instance(page.ID, append([]InstanceOption{Kind("page")}, opts...)...)
	inst.Component = page
	page.Remembered = page.Focus
	// A page that starts on a row starts with that row selected.
	r.selectFocusedRow(page.Focus)
	if list := r.List(page.Focus); list != nil {
		// A page that starts on a list starts on its selected row.
		page.Remembered = list.focusTarget()
	}
	return nil
}

// PageOf returns the page an instance fronts, or nil.
func (r *Runtime) PageOf(id string) *Page {
	if inst := r.Instances[id]; inst != nil {
		if page, ok := inst.Component.(*Page); ok {
			return page
		}
	}
	return nil
}

// PageList lists pages in registration order: app's children of kind page.
func (r *Runtime) PageList() []*Page {
	var out []*Page
	for _, inst := range r.Children("app") {
		if page, ok := inst.Component.(*Page); ok {
			out = append(out, page)
		}
	}
	return out
}

// OpenPage makes a page current and restores the focus and mode it was left
// in; an open modal blocks navigation. Runtime.Page is always an ancestor of
// the focus.
func (r *Runtime) OpenPage(id string) error {
	page := r.PageOf(id)
	if page == nil {
		return fmt.Errorf("unknown page %q", id)
	}
	if len(r.modals) > 0 {
		return errors.New("close the active modal before opening a page")
	}
	from := r.Focus
	stop, path := r.stopOf(from), r.Ancestry(from)
	r.CancelPendingBinding()
	if current := r.PageOf(r.Page); current != nil {
		// The page being left remembers its focus and mode.
		current.Remembered, current.mode = from, ModeState{Name: r.BaseMode()}
	}
	if r.Instances[page.Remembered] == nil {
		page.Remembered, page.mode = page.ID, ModeState{}
	}
	r.Focus = page.Remembered
	r.selectFocusedRow(r.Focus)
	left := r.Page
	r.Page = id
	page.visited = r.lastPageVisit() + 1
	if page.mode.Name != "" {
		r.rememberLeaving(path, r.Focus)
		r.restoreMode(page.mode, path)
	} else if r.Focus != from {
		r.focusMoved(stop, path, r.Focus)
	}
	if page.OnOpen != nil && left != id {
		page.OnOpen(r, left)
	}
	return nil
}

func (r *Runtime) movePage(delta int) error {
	pages := r.PageList()
	if len(pages) == 0 {
		return errors.New("no pages registered")
	}
	index := -1
	for i, page := range pages {
		if page.ID == r.Page {
			index = i
			break
		}
	}
	if index == -1 {
		if delta > 0 {
			return r.OpenPage(pages[0].ID)
		}
		return r.OpenPage(pages[len(pages)-1].ID)
	}
	index = (index + delta + len(pages)) % len(pages)
	return r.OpenPage(pages[index].ID)
}

func registerPages(r *Runtime) {
	r.Define("page")
	r.Register(Operation{Verb: "list", Noun: "pages", Description: "List pages in registration order", Result: "page[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		pages := make([]map[string]any, 0)
		for _, page := range r.PageList() {
			pages = append(pages, map[string]any{"id": page.ID, "title": page.Title, "active": page.ID == r.Page})
		}
		return pages, nil
	}})
	r.Register(Operation{Verb: "open", Noun: "page", Description: "Open a registered page", Parameters: map[string]Parameter{"id": {Type: "string", Required: true}}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.OpenPage(StringArg(a, "id", ""))
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "pick", Noun: "page", Description: "Choose a page, most recently visited first and the current one last: type to filter, enter opens", Result: "chooser state", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		chooser, err := r.OpenPagePicker()
		if err != nil {
			return nil, err
		}
		return chooser.state(), nil
	}})
	r.Register(Operation{Verb: "next", Noun: "page", Description: "Open the next page, wrapping in registration order", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.movePage(1)
		return r.Context(), err
	}})
	r.Register(Operation{Verb: "previous", Noun: "page", Description: "Open the previous page, wrapping in registration order", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		err := r.movePage(-1)
		return r.Context(), err
	}})
}

// lastPageVisit is the newest page visit stamp, 0 before any page opened.
func (r *Runtime) lastPageVisit() uint64 {
	var last uint64
	for _, page := range r.PageList() {
		last = max(last, page.visited)
	}
	return last
}

// pagePickerOrder is the page picker's row order: visited pages but the
// current one by recency, never-visited pages in registration order, the
// current page last.
func (r *Runtime) pagePickerOrder() []*Page {
	var visited, fresh []*Page
	var current *Page
	for _, page := range r.PageList() {
		switch {
		case page.ID == r.Page:
			current = page
		case page.visited > 0:
			visited = append(visited, page)
		default:
			fresh = append(fresh, page)
		}
	}
	slices.SortStableFunc(visited, func(a, b *Page) int { return cmp.Compare(b.visited, a.visited) })
	out := append(visited, fresh...)
	if current != nil {
		out = append(out, current)
	}
	return out
}

// pagePickerID is the page picker's modal kind.
const pagePickerID = "page-picker"

// OpenPagePicker opens the page chooser (page.pick): the pages visited
// before, most recent first, then the pages never visited in registration
// order, then the current page, marked "current", so the highlighted first
// row is the page last left and enter goes back to it. Typing filters by
// title; enter opens the highlighted page, esc closes.
func (r *Runtime) OpenPagePicker() (*Chooser, error) {
	pages := r.pagePickerOrder()
	rows := make([]ChooserRow, 0, len(pages))
	for _, page := range pages {
		label := page.Title
		if label == "" {
			label = page.ID
		}
		row := ChooserRow{ID: page.ID, Label: label}
		if page.ID == r.Page {
			row.Cells = []string{label, "current"}
		}
		rows = append(rows, row)
	}
	return r.OpenChooser(ChooserSpec{
		Kind:  pagePickerID,
		Title: "Pages",
		Rows:  rows,
		Empty: "no pages",
		Choose: func(row ChooserRow) Result {
			if err := r.OpenPage(row.ID); err != nil {
				return Failed(err)
			}
			return Done(r.Context())
		},
	})
}
