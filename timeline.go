package ochazuke

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Card is one entry of a timeline: a box with a header that always shows
// (wrapped onto as many lines as it needs) and a meta line under it, and a
// body shown only while the card is open. Like a ListItem, a card is an
// instance of its noun (Noun, default TimelineCardNoun) under the
// timeline's list, so the app's verbs and rows on that noun answer on it
// and Data is what a verb reads as Ctx.Instance.Data. Label is the
// instance's label (palette, {{label}}, the / filter), default the header
// on one line.
//
// Body and every section's Body are asked lazily, with the inner width,
// only while the card is open and drawn (or scrolled across); the result
// is kept until the width, the folds or Revision change, so an app bumps
// Revision whenever a body's text changes (a streaming answer). Open is the
// card's state the first time the timeline sees its id; after that the
// user's open and close follow the id. Active draws the card brighter (its
// header bold, a "●" in its top border, which reads in mono too); Muted
// draws its header and meta in the muted style (an older card). A card
// whose Group differs from the card above it is preceded by a full-width
// rule with the group as its label (a session boundary).
//
// Steps are the places inside an open card that l steps onto and j/k walk
// (timeline_steps.go). A card with Steps draws its body from them, and
// Body and Sections are not drawn; a card without them steps through its
// Body's paragraphs (blank-line separated, a fenced code block one
// paragraph) and then each section, title and text, as one step.
type Card struct {
	ID       string                 `json:"id"`
	Noun     string                 `json:"noun,omitempty"`
	Label    string                 `json:"label,omitempty"`
	Data     any                    `json:"data,omitempty"`
	Header   string                 `json:"header"`
	Meta     string                 `json:"meta,omitempty"`
	Body     func(width int) string `json:"-"`
	Sections []CardSection          `json:"sections,omitempty"`
	Steps    []CardStep             `json:"steps,omitempty"`
	Open     bool                   `json:"open,omitempty"`
	Active   bool                   `json:"active,omitempty"`
	Muted    bool                   `json:"muted,omitempty"`
	Group    string                 `json:"group,omitempty"`
	Revision uint64                 `json:"revision,omitempty"`
	// Event draws the card as something that happened rather than
	// something said: indented one level (eventIndent cells), its border
	// dashed and muted until it is selected. A task's notification is one.
	Event bool `json:"event,omitempty"`
}

// eventIndent is how far an event card is indented.
const eventIndent = 2

// CardSection is one foldable part of an open card's body, drawn after
// Body: a title line ("▾ 2 thinking · Summary") and, unfolded, its text
// indented in its Role's style. Name keys its fold state (default the
// title, else its number); Folded is its state until the user folds it.
type CardSection struct {
	Name    string                 `json:"name,omitempty"`
	Title   string                 `json:"title"`
	Summary string                 `json:"summary,omitempty"`
	Role    CardRole               `json:"role,omitempty"`
	Folded  bool                   `json:"folded,omitempty"`
	Body    func(width int) string `json:"-"`
}

// CardRole is the style a section's text draws in.
type CardRole string

// Card roles: the theme's text, muted (thinking), accent, error.
const (
	CardRoleText   CardRole = ""
	CardRoleMuted  CardRole = "muted"
	CardRoleAccent CardRole = "accent"
	CardRoleError  CardRole = "error"
)

// TextBody is a Body that is always text (wrapped by the timeline).
func TextBody(text string) func(int) string { return func(int) string { return text } }

// TimelineCardNoun is the noun of a card that names none.
const TimelineCardNoun = "ochazuke.timeline-card"

// timelineNoun is the noun of every timeline instance.
const timelineNoun = "ochazuke.timeline"

// FollowTail makes a timeline follow its end: the window sticks to the
// bottom while the last card is selected and the bottom is in view; a card
// appended then is selected and a growing last card keeps its bottom in
// view. Moving or scrolling up stops it; G or reaching the bottom again
// resumes it.
func FollowTail() InstanceOption { return func(s *instanceSpec) { s.followTail = true } }

// HeaderLines caps a closed card's header at n lines, the rest read
// "… N more lines"; an open card shows its whole header. 0 shows every line.
func HeaderLines(n int) InstanceOption { return func(s *instanceSpec) { s.headerLines = max(0, n) } }

// Timeline is a scrolling column of cards of varying height, each a box
// (Card). The selection, motions, counts, spills, the / filter and the
// card instances are those of a list child (id+"/list") whose rows are the
// cards, so j/k, g g, ctrl+d/ctrl+u and 3j behave as on any page list; the
// timeline draws the cards itself, in a window of its own measured in
// lines (top, the first card drawn and how many of its lines are above
// the window), and reports the selected card's lines with PlacedAt so the
// page keeps it on screen.
//
// Selection rule (the cards' exception to a page list's reverse video):
// the selection is the border, and no text on a card is ever reversed.
// The selected card of a focused timeline has a heavy border with heavy
// corners in the accent, bold; the selected card of a timeline without
// the focus a heavy border with rounded corners halfway from the accent
// to the border role; every other card a light rounded border in the
// border role faded by dim-unfocused (Styles.CardBorders). Header, meta
// and body text keep their own styles whether selected or not. A
// timeline without the focus is also faded as a whole (dim-unfocused),
// as a page list is.
type Timeline struct {
	r    *Runtime
	id   string
	list *List

	cards  []Card
	byID   map[string]int
	open   map[string]bool
	folds  map[string]map[string]bool
	cache  map[string]*cardCache
	limit  int  // HeaderLines
	tail   bool // FollowTail
	follow bool // following the end now

	top       timelineAnchor // the window's first line
	lastSel   string         // the card selected at the last frame
	reveal    bool           // the next frame shows the selected card whole (an open)
	toBottom  bool           // the next frame shows the last card's end (G)
	width     int            // the last frame's width
	avail     int            // and the lines it had for cards
	overflow  bool           // the last frame did not show every line
	focusLine int            // the selected card's first line in the last frame
	focusRows int            // and its lines on screen
	styles    Styles

	// Inside a card (timeline_steps.go): bodyID is the body instance
	// (id+"/body", under the list) that holds the focus there; in is the
	// card it walks, step its current step; parentStop is where h on a closed
	// card goes (ParentStop).
	bodyID     string
	in         string
	step       int
	seek       bool // the next frame shows the current step (it moved)
	parentStop string
}

// timelineAnchor is a line of the cards: card id's line-th line.
type timelineAnchor struct {
	id   string
	line int
}

// cardCache is a card's measured text at one inner width: the header
// wrapped, and the body while it was last open with the same folds.
type cardCache struct {
	width   int
	header  string
	rev     uint64
	lines   []string
	bodyKey string
	body    []cardLine
	steps   []stepSpan // the body's steps, built with it
	hasBody bool
}

// cardLine is a body line and the role it draws in; title marks a section
// title line.
type cardLine struct {
	text  string
	role  CardRole
	title bool
}

// AddTimeline registers a timeline under Parent (default app) with its
// cards. Options: FollowTail, HeaderLines(n), NoSpill (motions stay in it),
// ParentStop(id) (where h on a closed card goes).
func (r *Runtime) AddTimeline(id string, cards []Card, opts ...InstanceOption) (*Timeline, error) {
	if id == "" || r.Instances[id] != nil {
		return nil, fmt.Errorf("timeline ID %q must be nonempty and unused", id)
	}
	spec := instanceSpec{parent: "app"}
	for _, opt := range opts {
		opt(&spec)
	}
	if r.Instances[spec.parent] == nil {
		return nil, fmt.Errorf("unknown parent %q", spec.parent)
	}
	t := &Timeline{r: r, id: id, byID: map[string]int{}, open: map[string]bool{}, folds: map[string]map[string]bool{}, cache: map[string]*cardCache{}, limit: spec.headerLines, tail: spec.followTail, follow: spec.followTail, bodyID: id + "/body", parentStop: spec.parentStop}
	r.Instance(id, Parent(spec.parent), Kind(timelineNoun), FocusOnClose(spec.closeFocus))
	r.Instances[id].Component = t
	listOpts := []InstanceOption{Parent(id), NoMarks()}
	if spec.noSpill {
		listOpts = append(listOpts, NoSpill())
	}
	list, err := r.AddList(id+"/list", nil, listOpts...)
	if err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	t.list = list
	// The body: the place inside the selected card, a part of the list (it
	// holds the list's focus as a row does, timeline_steps.go).
	r.Instance(t.bodyID, Parent(list.id), Kind(TimelineBodyNoun))
	r.Instances[t.bodyID].Label = "body"
	list.part = t.bodyID
	list.motion = t.cardMotion
	// G on the list instance, above the list noun's list.goto: it also
	// brings the last card's end into view and resumes following.
	for _, mode := range []string{"normal", "count_pending"} {
		row := Do(timelineNoun+".goto", Arguments{"position": "last"}).FromPayload("row", SourceLine)
		row.Instance, row.Mode, row.Keys = list.id, mode, "G"
		if err := r.Bind(row); err != nil {
			_ = r.RemoveInstance(id)
			return nil, err
		}
	}
	if err := t.SetCards(cards); err != nil {
		_ = r.RemoveInstance(id)
		return nil, err
	}
	return t, nil
}

// Timeline is the timeline with this id, or nil.
func (r *Runtime) Timeline(id string) *Timeline {
	if inst := r.Instances[id]; inst != nil {
		if t, ok := inst.Component.(*Timeline); ok {
			return t
		}
	}
	return nil
}

// timelineOf is the timeline on a focus's ancestry, or nil.
func (r *Runtime) timelineOf(focus string) *Timeline {
	for _, id := range r.Ancestry(focus) {
		if t := r.Timeline(id); t != nil {
			return t
		}
	}
	return nil
}

// ID is the timeline's instance id.
func (t *Timeline) ID() string { return t.id }

// List is the list whose rows are the cards (selection, motions, filter).
func (t *Timeline) List() *List { return t.list }

// FocusTarget is where a jump to the timeline lands: its selected card.
func (t *Timeline) FocusTarget() string { return t.list.FocusTarget() }

// Focus focuses the selected card.
func (t *Timeline) Focus() error { return t.list.Focus() }

// Cards is a copy of the cards in order.
func (t *Timeline) Cards() []Card { return slices.Clone(t.cards) }

// Selected is the selected card; an empty timeline has none.
func (t *Timeline) Selected() (Card, bool) {
	item, ok := t.list.Selected()
	if !ok {
		return Card{}, false
	}
	return t.cards[t.byID[item.ID]], true
}

// SelectedID is the selected card's id, or "".
func (t *Timeline) SelectedID() string {
	item, _ := t.list.Selected()
	return item.ID
}

// Select selects the card with id without taking the focus.
func (t *Timeline) Select(id string) error { return t.list.Select(id) }

// Following reports whether the timeline follows its end (FollowTail).
func (t *Timeline) Following() bool { return t.tail && t.follow }

// IsOpen reports whether the card with id is open.
func (t *Timeline) IsOpen(id string) bool { return t.open[id] }

// SetOpen opens or closes the card with id.
func (t *Timeline) SetOpen(id string, open bool) error {
	if _, ok := t.byID[id]; !ok {
		return fmt.Errorf("unknown card %q", id)
	}
	t.open[id] = open
	if open && id == t.SelectedID() {
		t.reveal = true
	}
	if !open && id == t.in {
		t.leaveBody()
	}
	return nil
}

// SetAllOpen opens or closes every card.
func (t *Timeline) SetAllOpen(open bool) {
	for _, c := range t.cards {
		t.open[c.ID] = open
	}
	t.reveal = open
	if !open {
		t.leaveBody()
	}
}

// Folded reports whether section (its Name, Title or number from 1) of the
// card with id is folded.
func (t *Timeline) Folded(id, section string) bool {
	i, ok := t.byID[id]
	if !ok {
		return false
	}
	c := &t.cards[i]
	units := foldUnits(c)
	k := unitIndex(units, section)
	if k < 0 {
		return false
	}
	return t.folded(c.ID, units[k])
}

// SetFolded folds or unfolds a section of the card with id (section is its
// Name, Title or number from 1).
func (t *Timeline) SetFolded(id, section string, folded bool) error {
	i, ok := t.byID[id]
	if !ok {
		return fmt.Errorf("unknown card %q", id)
	}
	c := &t.cards[i]
	units := foldUnits(c)
	k := unitIndex(units, section)
	if k < 0 {
		return fmt.Errorf("card %s has no section %q", id, section)
	}
	if t.folds[id] == nil {
		t.folds[id] = map[string]bool{}
	}
	t.folds[id][units[k].name] = folded
	return nil
}

// foldUnit is a part of a card that folds: a section, or a step with a
// title and a body. name keys its fold state (its Name or ID, else its
// title, else its number from 1).
type foldUnit struct {
	name, title string
	folded      bool
}

// foldUnits are the card's foldable parts in order: its sections, or, a
// card with Steps, its titled steps with a body.
func foldUnits(c *Card) []foldUnit {
	var out []foldUnit
	add := func(name, title string, folded bool) {
		switch {
		case name != "":
		case title != "":
			name = title
		default:
			name = strconv.Itoa(len(out) + 1)
		}
		out = append(out, foldUnit{name: name, title: title, folded: folded})
	}
	if len(c.Steps) == 0 {
		for _, s := range c.Sections {
			add(s.Name, s.Title, s.Folded)
		}
		return out
	}
	for _, s := range c.Steps {
		if s.foldable() {
			add(s.ID, s.Title, s.Folded)
		}
	}
	return out
}

// unitIndex is the fold unit named section (its name, title or number
// from 1), or -1.
func unitIndex(units []foldUnit, section string) int {
	if n, err := strconv.Atoi(section); err == nil && n >= 1 && n <= len(units) {
		return n - 1
	}
	for k, u := range units {
		if u.name == section || u.title == section {
			return k
		}
	}
	return -1
}

func (t *Timeline) folded(card string, u foldUnit) bool {
	if f, ok := t.folds[card][u.name]; ok {
		return f
	}
	return u.folded
}

// SetCards replaces the cards, keyed by id: the selection, open cards and
// section folds follow their ids (as List.SetItems keeps the selection),
// a new card starts as its Open says. While the timeline follows its end
// (FollowTail) and the last card is selected, the new last card is
// selected. Invalid cards leave the timeline unchanged.
func (t *Timeline) SetCards(cards []Card) error {
	cards = slices.Clone(cards)
	items := make([]ListItem, len(cards))
	byID := make(map[string]int, len(cards))
	for i := range cards {
		if cards[i].Noun == "" {
			cards[i].Noun = TimelineCardNoun
		}
		items[i] = cardItem(cards[i])
		byID[cards[i].ID] = i
	}
	return t.apply(cards, byID, items)
}

// Patch updates the cards with these ids in place and appends the others
// at the end, in order: the cheap path for a streaming last card (bump its
// Revision) or a new one. The list's rows are rebuilt only when a card is
// added or its noun, label or data changed.
func (t *Timeline) Patch(cards ...Card) error {
	if len(cards) == 0 {
		return nil
	}
	cards = slices.Clone(cards) // the default noun is filled in on a copy
	rebuild := false
	for i := range cards {
		c := &cards[i]
		if c.Noun == "" {
			c.Noun = TimelineCardNoun
		}
		if t.r.Noun(c.Noun) == nil {
			return fmt.Errorf("card %q: unknown noun %q", c.ID, c.Noun)
		}
		at, ok := t.byID[c.ID]
		if !ok {
			rebuild = true
			continue
		}
		old := t.cards[at]
		if old.Noun != c.Noun || cardLabel(old) != cardLabel(*c) || !reflect.DeepEqual(old.Data, c.Data) {
			rebuild = true
		}
	}
	if !rebuild {
		// Only text changed: the rows stay, the cards are updated in
		// place (Cards hands out copies, so no caller holds this slice).
		for _, c := range cards {
			t.cards[t.byID[c.ID]] = c
		}
		return nil
	}
	next := slices.Clone(t.cards)
	byID := make(map[string]int, len(t.byID)+len(cards))
	for id, i := range t.byID {
		byID[id] = i
	}
	for _, c := range cards {
		if i, ok := byID[c.ID]; ok {
			next[i] = c
			continue
		}
		byID[c.ID] = len(next)
		next = append(next, c)
	}
	items := make([]ListItem, len(next))
	for i := range next {
		items[i] = cardItem(next[i])
	}
	return t.apply(next, byID, items)
}

func (t *Timeline) apply(cards []Card, byID map[string]int, items []ListItem) error {
	lastID := ""
	if n := len(t.list.all); n > 0 {
		lastID = t.list.all[n-1].ID
	}
	wasLast := t.list.Len() == 0 || t.SelectedID() == lastID
	// The body sits under the list while the rows change (a removed card
	// takes its subtree with it), then under its card again if it stays.
	if inst := t.r.Instances[t.bodyID]; inst != nil {
		inst.Parent = t.list.id
	}
	defer t.placeBody()
	if err := t.list.SetItems(items); err != nil {
		return err
	}
	for _, c := range cards {
		if _, seen := t.byID[c.ID]; !seen {
			if _, set := t.open[c.ID]; !set {
				t.open[c.ID] = c.Open
			}
		}
	}
	for id := range t.byID {
		if _, ok := byID[id]; !ok {
			delete(t.open, id)
			delete(t.folds, id)
			delete(t.cache, id)
		}
	}
	t.cards, t.byID = cards, byID
	if t.tail && t.follow && wasLast && len(items) > 0 && t.list.query == "" {
		_ = t.list.Select(items[len(items)-1].ID)
	}
	return nil
}

func cardItem(c Card) ListItem {
	return ListItem{ID: c.ID, Noun: c.Noun, Label: cardLabel(c), Data: c.Data}
}

// cardLabel is the card's instance label: Label, else the header on one line.
func cardLabel(c Card) string {
	if c.Label != "" {
		return c.Label
	}
	return strings.Join(strings.Fields(c.Header), " ")
}

// state is a timeline verb's result.
func (t *Timeline) state() map[string]any {
	var open []string
	for _, c := range t.cards {
		if t.open[c.ID] {
			open = append(open, c.ID)
		}
	}
	out := map[string]any{"id": t.id, "selectedId": t.SelectedID(), "open": open, "following": t.Following(), "top": t.top.id, "topLine": t.top.line, "cards": len(t.cards)}
	if t.InBody() {
		out["in"], out["step"] = t.in, t.step
	}
	return out
}

// shown is the i-th card the list shows (the filter's matches).
func (t *Timeline) shown(i int) *Card { return &t.cards[t.byID[t.list.items[i].ID]] }

func (t *Timeline) indexOf(id string) int {
	return slices.IndexFunc(t.list.items, func(item ListItem) bool { return item.ID == id })
}

// cardWidth is the width card c draws at: an event card is indented.
func (t *Timeline) cardWidth(c *Card) int {
	if c.Event && t.width > eventIndent+6 {
		return t.width - eventIndent
	}
	return t.width
}

// innerOf is the text width inside card c's border and padding.
func (t *Timeline) innerOf(c *Card) int { return max(1, t.cardWidth(c)-4) }

// measured is the card's cache at the current width, its header wrapped.
func (t *Timeline) measured(c *Card) *cardCache {
	cc := t.cache[c.ID]
	if cc == nil {
		cc = &cardCache{width: -1}
		t.cache[c.ID] = cc
	}
	w := t.innerOf(c)
	if cc.width != w || cc.header != c.Header || cc.rev != c.Revision {
		cc.width, cc.header, cc.rev, cc.hasBody = w, c.Header, c.Revision, false
		cc.lines = wrapText(c.Header, max(1, w-2))
	}
	return cc
}

// body is the open card's body lines, built on first need and kept until
// the width, the revision or the folds change (timeline_steps.go builds
// them with the steps).
func (t *Timeline) body(c *Card, cc *cardCache) []cardLine {
	units := foldUnits(c)
	var key strings.Builder
	for _, u := range units {
		if t.folded(c.ID, u) {
			key.WriteByte('1')
		} else {
			key.WriteByte('0')
		}
	}
	if cc.hasBody && cc.bodyKey == key.String() {
		return cc.body
	}
	cc.body, cc.steps = t.buildBody(c, units)
	cc.bodyKey, cc.hasBody = key.String(), true
	return cc.body
}

// wrapText wraps text to width cells (words, then long words), never
// cutting it: the timeline's text is never ellipsised.
func wrapText(text string, width int) []string {
	text = strings.TrimRight(strings.ReplaceAll(text, "\t", "    "), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(ansi.Wrap(text, width, ""), "\n")
}

// groupStarts reports whether shown card i draws a group rule above it.
func (t *Timeline) groupStarts(i int) bool {
	g := t.shown(i).Group
	return g != "" && (i == 0 || t.shown(i-1).Group != g)
}

// headerRows is the header lines a card shows: every line when open,
// else up to the HeaderLines cap and its "… N more lines" line.
func (t *Timeline) headerRows(c *Card, cc *cardCache) int {
	n := len(cc.lines)
	if !t.open[c.ID] && t.limit > 0 && n > t.limit {
		return t.limit + 1
	}
	return max(1, n)
}

// height is shown card i's lines: its group rule, the border, the header,
// the meta line, and, open, a divider and the body.
func (t *Timeline) height(i int) int {
	c := t.shown(i)
	cc := t.measured(c)
	h := 2 + t.headerRows(c, cc)
	if c.Meta != "" {
		h++
	}
	if t.groupStarts(i) {
		h++
	}
	if t.open[c.ID] {
		if body := t.body(c, cc); len(body) > 0 {
			h += 1 + len(body)
		}
	}
	return h
}

// draw is shown card i's lines, styled.
func (t *Timeline) draw(i int, selected bool) []string {
	c := t.shown(i)
	cc := t.measured(c)
	s := t.styles
	w := t.innerOf(c)
	cw, indent := t.cardWidth(c), strings.Repeat(" ", t.width-t.cardWidth(c))
	open := t.open[c.ID]
	focusedBorder, selectedBorder, border := s.CardBorders()
	h, v, tl, tr, bl, br, ml, mr := "─", "│", "╭", "╮", "╰", "╯", "├", "┤"
	if c.Event {
		// Something that happened: a dashed, muted border.
		h, v, border = "┄", "┆", s.Muted
	}
	switch {
	case selected && t.list.Focused():
		border = focusedBorder
		h, v, tl, tr, bl, br, ml, mr = "━", "┃", "┏", "┓", "┗", "┛", "┣", "┫"
	case selected && t.list.marksSelection():
		border = selectedBorder
		h, v, ml, mr = "━", "┃", "┣", "┫"
	}
	// The current step's lines inside the card take a marker in the left
	// padding (never reversed text: cards reverse nothing).
	markFrom, markTo := -1, -2
	if selected && t.InBody() {
		markFrom, markTo, _ = t.stepLines(i)
	}
	var out []string
	if t.groupStarts(i) {
		label := " " + modalLabel(c.Group) + " "
		rule := "──" + label
		rule += strings.Repeat("─", max(0, t.width-ansi.StringWidth(rule)))
		out = append(out, s.Muted.Render(ansi.Truncate(rule, t.width, "")))
	}
	top := tl + strings.Repeat(h, max(0, cw-2)) + tr
	if c.Active && cw >= 6 {
		top = tl + h + " ● " + strings.Repeat(h, max(0, cw-6)) + tr
	}
	out = append(out, indent+border.Render(top))
	row := func(text string, style lipgloss.Style) string {
		if ansi.StringWidth(text) > w {
			text = ansi.Truncate(text, w, "")
		}
		text += strings.Repeat(" ", max(0, w-ansi.StringWidth(text)))
		pad := " "
		if at := len(out); at >= markFrom && at <= markTo {
			pad = s.Accent.Bold(true).Render(stepMarker)
		}
		return indent + border.Render(v) + pad + style.Render(text) + " " + border.Render(v)
	}
	header := lipgloss.NewStyle()
	switch {
	case c.Active:
		header = header.Bold(true)
	case c.Muted:
		header = s.Muted
	}
	mark := "▸ "
	if open {
		mark = "▾ "
	}
	lines := cc.lines
	more := 0
	if !open && t.limit > 0 && len(lines) > t.limit {
		lines, more = lines[:t.limit], len(lines)-t.limit
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	for k, line := range lines {
		prefix := "  "
		if k == 0 {
			prefix = mark
		}
		out = append(out, row(prefix+line, header))
	}
	if more > 0 {
		word := "lines"
		if more == 1 {
			word = "line"
		}
		out = append(out, row(fmt.Sprintf("  … %d more %s", more, word), s.Muted))
	}
	if c.Meta != "" {
		out = append(out, row("  "+modalLabel(c.Meta), s.Muted))
	}
	if open {
		if body := t.body(c, cc); len(body) > 0 {
			out = append(out, indent+border.Render(ml+strings.Repeat(h, max(0, cw-2))+mr))
			for _, line := range body {
				style := lipgloss.NewStyle()
				switch {
				case line.title:
					style = style.Bold(true)
				case line.role == CardRoleMuted:
					style = s.Muted
				case line.role == CardRoleAccent:
					style = s.Accent
				case line.role == CardRoleError:
					style = s.Error
				}
				out = append(out, row(line.text, style))
			}
		}
	}
	return append(out, indent+border.Render(bl+strings.Repeat(h, max(0, cw-2))+br))
}

// bottomOf is the anchor that puts shown card i's last line on the
// window's last line (the start when the cards above do not fill it).
func (t *Timeline) bottomOf(i, avail int) timelineAnchor {
	return t.bottomAt(i, t.height(i)-1, avail)
}

// bottomAt is the anchor that puts line of shown card i on the window's
// last line (the start when what is above does not fill it).
func (t *Timeline) bottomAt(i, line, avail int) timelineAnchor {
	rem := avail
	for j := i; j >= 0; j-- {
		h := t.height(j)
		if j == i {
			h = line + 1
		}
		if h >= rem {
			return timelineAnchor{t.list.items[j].ID, h - rem}
		}
		rem -= h
	}
	return timelineAnchor{t.list.items[0].ID, 0}
}

// after reports whether anchor a is below anchor b.
func (t *Timeline) after(a, b timelineAnchor) bool {
	ai, bi := t.indexOf(a.id), t.indexOf(b.id)
	return ai > bi || ai == bi && a.line > b.line
}

// revealed is the anchor that shows shown card sel: kept when sel is on
// screen whole, else moved by whole cards the least (cardsEnding), sel's
// top when it is taller than the window or above it.
func (t *Timeline) revealed(sel, avail int) timelineAnchor {
	at := timelineAnchor{t.list.items[sel].ID, 0}
	ti := t.indexOf(t.top.id)
	if ti < 0 || sel < ti || sel == ti && t.top.line > 0 || t.height(sel) >= avail {
		return at
	}
	if b := t.cardsEnding(sel, avail); t.after(b, t.top) {
		return b
	}
	return t.top
}

// cardsEnding is the anchor of the first whole card from which the cards
// through sel fit in avail lines: revealing a card scrolls by whole
// cards, so no sliver of the card above is left at the top.
func (t *Timeline) cardsEnding(sel, avail int) timelineAnchor {
	j, used := sel, t.height(sel)
	for j > 0 {
		h := t.height(j - 1)
		if used+h > avail {
			break
		}
		used += h
		j--
	}
	return timelineAnchor{t.list.items[j].ID, 0}
}

// render draws avail lines from the anchor down: the rows, the first and
// last cards on screen and whether the last card's end is on screen.
func (t *Timeline) render(avail int) (rows []string, first, last int, end bool) {
	n, sel := len(t.list.items), t.list.selected
	ti := t.indexOf(t.top.id)
	if ti < 0 {
		ti, t.top = 0, timelineAnchor{t.list.items[0].ID, 0}
	}
	first, last = ti, ti
	t.focusLine, t.focusRows = 0, 0
	for i := ti; i < n && len(rows) < avail; i++ {
		block := t.draw(i, i == sel)
		if i == ti {
			if t.top.line >= len(block) {
				t.top.line = len(block) - 1
			}
			block = block[t.top.line:]
		}
		take := min(len(block), avail-len(rows))
		if i == sel {
			t.focusLine, t.focusRows = len(rows), take
			if lo, hi, ok := t.stepShown(i); ok {
				// Inside the card the page keeps the current step on screen.
				cut := 0
				if i == ti {
					cut = t.top.line
				}
				lo, hi = max(lo-cut, 0), min(hi-cut, take-1)
				if hi >= lo {
					t.focusLine, t.focusRows = len(rows)+lo, hi-lo+1
				}
			}
		}
		last = i
		rows = append(rows, block[:take]...)
		if i == n-1 && take == len(block) {
			end = true
		}
	}
	return rows, first, last, end
}

// frame positions the window for avail lines and draws it: following the
// end, else showing a newly selected card, else keeping the window (pulled
// back when it would end short of the window, moved when the selection
// left it). It reports whether the cards overflow the window.
func (t *Timeline) frame(avail int) ([]string, bool) {
	n, sel := len(t.list.items), t.list.selected
	selID := t.list.items[sel].ID
	inBody := t.InBody()
	switch {
	case !inBody && (t.tail && t.follow || t.toBottom) && sel == n-1:
		t.top = t.bottomOf(n-1, avail)
	case t.indexOf(t.top.id) < 0 || selID != t.lastSel || t.reveal:
		t.top = t.revealed(sel, avail)
	}
	if inBody {
		t.top = t.showStep(sel, avail)
	}
	rows, first, last, end := t.render(avail)
	if end && len(rows) < avail && (t.indexOf(t.top.id) > 0 || t.top.line > 0) {
		t.top = t.bottomOf(n-1, avail)
		rows, first, last, end = t.render(avail)
	}
	if sel < first || sel > last {
		t.top = t.revealed(sel, avail)
		rows, first, last, end = t.render(avail)
	}
	if t.tail {
		t.follow = sel == n-1 && end
	}
	t.list.window, t.list.windowed = max(1, last-first+1), !(first == 0 && t.top.line == 0 && end)
	return rows, t.list.windowed
}

// View draws the cards in a width by height window and records the
// timeline in its page's drawing order (Placed) with the selected card's
// lines (PlacedAt). When the cards overflow, the last line reads which
// cards are on screen, "3–5 / 40". A filter line (/) takes the first line.
func (t *Timeline) View(width, height int) string {
	l := t.list
	t.r.Placed(l.id)
	if width <= 0 || height <= 0 {
		return ""
	}
	t.styles = NewStyles(Appearance{Theme: "adaptive", Dark: true})
	if styles := t.r.Settings().page.styles; styles != nil {
		t.styles = styles()
	}
	t.width = width
	line := ""
	if height > 1 {
		if line = l.filterLine(width); line != "" {
			height--
		}
	}
	var rows []string
	focus, focusRows := 0, height
	if len(l.items) == 0 {
		rows = layoutRows(l.emptyLine(), width, height)
		t.overflow, t.avail = false, height
	} else {
		top, follow := t.top, t.follow
		var over bool
		rows, over = t.frame(height)
		t.avail = height
		if over && height > 1 {
			t.top, t.follow = top, follow
			rows, _ = t.frame(height - 1)
			t.avail = height - 1
			first := l.offsetOf(t.top.id)
			rows = append(rows, fmt.Sprintf("%d–%d / %d", first+1, first+l.window, len(l.items)))
		}
		t.overflow = over
		for len(rows) < height {
			rows = append(rows, "")
		}
		if t.focusRows > 0 {
			focus, focusRows = t.focusLine, t.focusRows
		}
		t.lastSel, t.reveal, t.toBottom, t.seek = l.items[l.selected].ID, false, false, false
	}
	text := strings.Join(rows, "\n")
	if line != "" {
		text = line + "\n" + text
		if focusRows == height {
			focusRows++
		} else {
			focus++
		}
	}
	if l.faded() {
		text = dim(text, t.styles.Unfocused)
	}
	return t.r.PlacedAt(l.id, text, focus, focusRows)
}

// offsetOf is the shown index of row id, or 0.
func (l *List) offsetOf(id string) int {
	return max(0, slices.IndexFunc(l.items, func(item ListItem) bool { return item.ID == id }))
}

// scroll moves the window by lines (down when positive) without moving
// the selection unless it would leave the window (it then lands on the
// nearest card on screen). Scrolling up stops following the end.
func (t *Timeline) scroll(lines int) {
	n := len(t.list.items)
	if n == 0 || t.width <= 0 || t.avail <= 0 {
		return
	}
	t.settle()
	t.shiftTop(lines)
	// The cards now on screen: the selection stays among them.
	at := t.top
	first := t.indexOf(at.id)
	last, used := first, -at.line
	for j := first; j < n && used < t.avail; j++ {
		used += t.height(j)
		last = j
	}
	if sel := t.list.selected; sel < first {
		t.list.Move(first - sel)
	} else if sel > last {
		t.list.Move(last - sel)
	}
	t.lastSel = t.SelectedID()
}

// shiftTop moves the window's first line by lines (down when positive),
// clamped to the first card's top and to the window that ends on the last
// card's end, and reports how far it moved. Moving up stops following.
func (t *Timeline) shiftTop(lines int) int {
	n := len(t.list.items)
	if n == 0 || lines == 0 {
		return 0
	}
	if lines < 0 {
		t.follow = false
	}
	old := t.top
	i := max(0, t.indexOf(t.top.id))
	line := t.top.line + lines
	for line < 0 && i > 0 {
		i--
		line += t.height(i)
	}
	line = max(0, line)
	for i < n-1 && line >= t.height(i) {
		line -= t.height(i)
		i++
	}
	at := timelineAnchor{t.list.items[i].ID, line}
	if end := t.bottomOf(n-1, t.avail); t.after(at, end) {
		at = end
	}
	t.top = at
	if t.indexOf(old.id) < 0 {
		return lines
	}
	return -t.relOf(old)
}

// relStart is shown card i's first line counted from the window's first
// line (negative above it); it measures only the cards between them.
func (t *Timeline) relStart(i int) int {
	ti := max(0, t.indexOf(t.top.id))
	s := -t.top.line
	for j := ti; j < i; j++ {
		s += t.height(j)
	}
	for j := ti - 1; j >= i; j-- {
		s -= t.height(j)
	}
	return s
}

// relOf is anchor a's line counted from the window's first line.
func (t *Timeline) relOf(a timelineAnchor) int { return t.relStart(t.indexOf(a.id)) + a.line }

// motionShift scrolls the window toward want lines by the half-page rule
// (MotionScroll) and reports how far it moved.
func (t *Timeline) motionShift(want int) int {
	return t.shiftTop(MotionScroll(want, t.avail))
}

// drawn reports whether the timeline has a last frame to move in.
func (t *Timeline) drawn() bool {
	return t.width > 0 && t.avail > 0 && t.indexOf(t.top.id) >= 0
}

// settle places the window as the next frame would when something since
// the last frame asked it to move (an open, G, a new selection, a step
// entered or unfolded), so a motion pressed before that frame is drawn
// measures from where the window will be, not where it was.
func (t *Timeline) settle() {
	if t.width <= 0 || t.avail <= 0 || len(t.list.items) == 0 {
		return
	}
	if !t.reveal && !t.toBottom && !t.seek && t.SelectedID() == t.lastSel && t.indexOf(t.top.id) >= 0 {
		return
	}
	t.frame(t.avail)
	t.lastSel, t.reveal, t.toBottom, t.seek = t.SelectedID(), false, false, false
}

// cardMotion is j/k on the cards (list.move on the card list), count
// times: each press goes to the neighbouring card by the half-page rule
// (R3.14): when showing it would scroll more than half the window, the
// window scrolls half and the selection moves once the card shows. Past
// the last card the window first scrolls to the last card's end (past
// the first, to the first card's top), then the list spills (R3.4) or, a
// NoSpill timeline, stays. A count stops at the edge; only a move from
// the edge spills.
func (t *Timeline) cardMotion(delta, count int) {
	t.settle()
	for k := 0; k < max(1, count); k++ {
		if !t.cardOnce(delta, k == 0) {
			return
		}
	}
}

func (t *Timeline) cardOnce(delta int, edge bool) bool {
	l := t.list
	n, sel := len(l.items), l.selected
	if sel < 0 || delta == 0 {
		return false
	}
	next := sel + delta
	if !t.drawn() {
		if next >= 0 && next < n {
			l.Move(delta)
			return true
		}
		return edge && l.spill(delta)
	}
	if next < 0 || next >= n {
		if want := t.edgeWant(delta); want != 0 {
			t.motionShift(want)
			return true
		}
		return edge && l.spill(delta)
	}
	t.reveal, t.seek = false, false // the motion places the window itself
	a := t.relStart(next)
	b := a + t.height(next) - 1
	want := t.relOf(t.revealed(next, t.avail))
	if MotionScroll(want, t.avail) != want {
		// Too far for one press: the least scroll that shows the card
		// (its top when taller than the window), not the whole-card
		// alignment, so the press shows as much of it as it can.
		want = minimalReveal(a, b, t.avail, delta)
	}
	moved := t.motionShift(want)
	if SpanInView(a-moved, b-moved, t.avail) {
		l.Move(delta)
		t.lastSel = t.SelectedID() // the window stays where the motion put it
	}
	return true
}

// minimalReveal is the least scroll that shows lines a through b (counted
// from the window's first line) in a window of avail lines, moving in
// delta's direction: a span taller than the window shows its top going
// down and its end going up.
func minimalReveal(a, b, avail, delta int) int {
	tall := b-a+1 > avail
	switch {
	case delta > 0 && tall:
		return max(0, a)
	case delta > 0:
		return max(0, b-avail+1)
	case tall:
		return min(0, b-avail+1)
	}
	return min(0, a)
}

// edgeWant is the scroll that shows the timeline's end (delta > 0: the
// last card's last line on the window's last line) or its start; 0 when
// it shows.
func (t *Timeline) edgeWant(delta int) int {
	n := len(t.list.items)
	if delta > 0 {
		return max(0, t.relStart(n-1)+t.height(n-1)-t.avail)
	}
	return min(0, t.relStart(0))
}

// target is the card a verb acts on: the id argument, else the focused
// card, else the selection.
func (t *Timeline) target(c Context, args Arguments) (string, error) {
	if id := StringArg(args, "card", ""); id != "" {
		if _, ok := t.byID[id]; !ok {
			return "", fmt.Errorf("unknown card %q", id)
		}
		return id, nil
	}
	if focus := focusOf(c); t.r.rows[focus] == t.list.id {
		return focus, nil
	}
	if id := t.SelectedID(); id != "" {
		return id, nil
	}
	return "", errors.New("timeline has no cards")
}

// registerTimelines defines the timeline nouns, verbs and default rows.
func registerTimelines(r *Runtime) {
	r.Define(TimelineCardNoun)
	r.Define(timelineNoun)
	r.DefineGuard("timeline-scrolls", func(r *Runtime, ev KeyEvent) bool {
		t := r.timelineOf(ev.Focus)
		return t != nil && t.overflow
	})
	r.DefineGuard("timeline-sections", func(r *Runtime, ev KeyEvent) bool {
		t := r.timelineOf(ev.Focus)
		if t == nil {
			return false
		}
		c, ok := t.Selected()
		return ok && len(foldUnits(&c)) > 0
	})
	registerTimelineSteps(r)
	resolve := func(c Context, args Arguments) (*Timeline, error) {
		if id := StringArg(args, "id", ""); id != "" {
			if t := r.Timeline(id); t != nil {
				return t, nil
			}
			return nil, fmt.Errorf("unknown timeline %q", id)
		}
		if t := r.timelineOf(focusOf(c)); t != nil {
			return t, nil
		}
		return nil, errors.New("no focused timeline")
	}
	id := Parameter{Type: "string", Description: "The timeline (default: the focused one)", Noun: timelineNoun}
	card := Parameter{Type: "string", Description: "The card (default: the selected one)"}
	for _, spec := range []struct {
		verb, description string
		motion            bool
		parameters        map[string]Parameter
		run               func(*Timeline, Context, Arguments) error
	}{
		{"toggle", "Open the card, or close it when it is open", false, map[string]Parameter{"id": id, "card": card}, func(t *Timeline, c Context, a Arguments) error {
			k, err := t.target(c, a)
			if err != nil {
				return err
			}
			return t.SetOpen(k, !t.open[k])
		}},
		{"open", "Open the card: its body shows", false, map[string]Parameter{"id": id, "card": card}, func(t *Timeline, c Context, a Arguments) error {
			k, err := t.target(c, a)
			if err != nil {
				return err
			}
			return t.SetOpen(k, true)
		}},
		{"close", "Close the card to its header", false, map[string]Parameter{"id": id, "card": card}, func(t *Timeline, c Context, a Arguments) error {
			k, err := t.target(c, a)
			if err != nil {
				return err
			}
			return t.SetOpen(k, false)
		}},
		{"open-all", "Open every card", false, map[string]Parameter{"id": id}, func(t *Timeline, _ Context, _ Arguments) error {
			t.SetAllOpen(true)
			return nil
		}},
		{"close-all", "Close every card", false, map[string]Parameter{"id": id}, func(t *Timeline, _ Context, _ Arguments) error {
			t.SetAllOpen(false)
			return nil
		}},
		{"fold", "Fold or unfold a section of the card (opening a closed card shows the section)", false, map[string]Parameter{"id": id, "card": card, "section": {Type: "string", Required: true, Description: "The section's name, title or number from 1"}}, func(t *Timeline, c Context, a Arguments) error {
			k, err := t.target(c, a)
			if err != nil {
				return err
			}
			section := StringArg(a, "section", "")
			if !t.open[k] {
				if err := t.SetFolded(k, section, false); err != nil {
					return err
				}
				return t.SetOpen(k, true)
			}
			return t.SetFolded(k, section, !t.Folded(k, section))
		}},
		{"scroll", "Scroll the timeline by lines times count; the selection stays unless it would leave the window; up stops following the end", true, map[string]Parameter{"id": id, "lines": {Type: "integer", Description: "Lines to move, down when positive (default 1)"}, "count": {Type: "integer", Description: "Repeat count (default 1)"}}, func(t *Timeline, _ Context, a Arguments) error {
			t.scroll(IntArg(a, "lines", 1) * max(1, IntArg(a, "count", 1)))
			return nil
		}},
		{"goto", "Select the first or last card, or the card numbered row (5G); the last shows its end and resumes following", true, map[string]Parameter{"id": id, "position": {Type: "string", Enum: []string{"first", "last"}, Description: "Where to go without a row number (default last)"}, "row": {Type: "integer", Description: "The card number, 1 the first (a count: 5G)"}}, func(t *Timeline, _ Context, a Arguments) error {
			l := t.list
			switch row := IntArg(a, "row", 0); {
			case row > 0:
				l.goTo(row - 1)
			case StringArg(a, "position", "last") == "first":
				l.goTo(0)
			default:
				l.goTo(len(l.items) - 1)
				t.toBottom = true
				t.follow = t.tail
			}
			return nil
		}},
	} {
		r.Register(Operation{Verb: spec.verb, Noun: timelineNoun, Description: spec.description, Motion: spec.motion, Parameters: spec.parameters, Result: "timeline state", Run: func(_ *Runtime, c Context, args Arguments) (any, error) {
			t, err := resolve(c, args)
			if err != nil {
				return nil, err
			}
			if err := spec.run(t, c, args); err != nil {
				return nil, err
			}
			return t.state(), nil
		}})
	}
	for verb, guard := range map[string]string{
		"scroll":    "timeline-scrolls",
		"fold":      "timeline-sections",
		"open":      "timeline-card-closed",
		"close":     "timeline-card-open",
		"enter":     "timeline-has-card",
		"leave":     "timeline-on-card",
		"step":      "timeline-in-body",
		"yank-step": "timeline-in-body",
		"fold-step": "timeline-step-open",
	} {
		r.SetAvailability(timelineNoun+"."+verb, guard)
	}
	// The rows of the timeline and its body are bindings/timeline.json,
	// with no conditions: l is open then enter (a closed card opens, an
	// open one is entered), h is close then leave (an open card closes, a
	// closed one goes to the parent stop); inside a card the body's rows
	// answer first, h folding the open section the step is in, else
	// leaving to the card.
}
