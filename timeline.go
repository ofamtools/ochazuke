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
type Card struct {
	ID       string                 `json:"id"`
	Noun     string                 `json:"noun,omitempty"`
	Label    string                 `json:"label,omitempty"`
	Data     any                    `json:"data,omitempty"`
	Header   string                 `json:"header"`
	Meta     string                 `json:"meta,omitempty"`
	Body     func(width int) string `json:"-"`
	Sections []CardSection          `json:"sections,omitempty"`
	Open     bool                   `json:"open,omitempty"`
	Active   bool                   `json:"active,omitempty"`
	Muted    bool                   `json:"muted,omitempty"`
	Group    string                 `json:"group,omitempty"`
	Revision uint64                 `json:"revision,omitempty"`
}

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
// cards. Options: FollowTail, HeaderLines(n), NoSpill (motions stay in it).
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
	t := &Timeline{r: r, id: id, byID: map[string]int{}, open: map[string]bool{}, folds: map[string]map[string]bool{}, cache: map[string]*cardCache{}, limit: spec.headerLines, tail: spec.followTail, follow: spec.followTail}
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
	return nil
}

// SetAllOpen opens or closes every card.
func (t *Timeline) SetAllOpen(open bool) {
	for _, c := range t.cards {
		t.open[c.ID] = open
	}
	t.reveal = open
}

// Folded reports whether section (its Name, Title or number from 1) of the
// card with id is folded.
func (t *Timeline) Folded(id, section string) bool {
	i, ok := t.byID[id]
	if !ok {
		return false
	}
	c := &t.cards[i]
	k := sectionIndex(c, section)
	if k < 0 {
		return false
	}
	return t.folded(c, k)
}

// SetFolded folds or unfolds a section of the card with id (section is its
// Name, Title or number from 1).
func (t *Timeline) SetFolded(id, section string, folded bool) error {
	i, ok := t.byID[id]
	if !ok {
		return fmt.Errorf("unknown card %q", id)
	}
	c := &t.cards[i]
	k := sectionIndex(c, section)
	if k < 0 {
		return fmt.Errorf("card %s has no section %q", id, section)
	}
	if t.folds[id] == nil {
		t.folds[id] = map[string]bool{}
	}
	t.folds[id][sectionName(c, k)] = folded
	return nil
}

func sectionName(c *Card, k int) string {
	s := c.Sections[k]
	switch {
	case s.Name != "":
		return s.Name
	case s.Title != "":
		return s.Title
	}
	return strconv.Itoa(k + 1)
}

func sectionIndex(c *Card, section string) int {
	if n, err := strconv.Atoi(section); err == nil && n >= 1 && n <= len(c.Sections) {
		return n - 1
	}
	for k := range c.Sections {
		if sectionName(c, k) == section || c.Sections[k].Title == section {
			return k
		}
	}
	return -1
}

func (t *Timeline) folded(c *Card, k int) bool {
	if f, ok := t.folds[c.ID][sectionName(c, k)]; ok {
		return f
	}
	return c.Sections[k].Folded
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
	return map[string]any{"id": t.id, "selectedId": t.SelectedID(), "open": open, "following": t.Following(), "top": t.top.id, "topLine": t.top.line, "cards": len(t.cards)}
}

// shown is the i-th card the list shows (the filter's matches).
func (t *Timeline) shown(i int) *Card { return &t.cards[t.byID[t.list.items[i].ID]] }

func (t *Timeline) indexOf(id string) int {
	return slices.IndexFunc(t.list.items, func(item ListItem) bool { return item.ID == id })
}

// innerWidth is the text width inside a card's border and padding.
func (t *Timeline) innerWidth() int { return max(1, t.width-4) }

// measured is the card's cache at the current width, its header wrapped.
func (t *Timeline) measured(c *Card) *cardCache {
	cc := t.cache[c.ID]
	if cc == nil {
		cc = &cardCache{width: -1}
		t.cache[c.ID] = cc
	}
	w := t.innerWidth()
	if cc.width != w || cc.header != c.Header || cc.rev != c.Revision {
		cc.width, cc.header, cc.rev, cc.hasBody = w, c.Header, c.Revision, false
		cc.lines = wrapText(c.Header, max(1, w-2))
	}
	return cc
}

// body is the open card's body lines, built on first need and kept until
// the width, the revision or the folds change.
func (t *Timeline) body(c *Card, cc *cardCache) []cardLine {
	var key strings.Builder
	for k := range c.Sections {
		if t.folded(c, k) {
			key.WriteByte('1')
		} else {
			key.WriteByte('0')
		}
	}
	if cc.hasBody && cc.bodyKey == key.String() {
		return cc.body
	}
	w := t.innerWidth()
	var out []cardLine
	if c.Body != nil {
		for _, line := range wrapText(c.Body(w), w) {
			out = append(out, cardLine{text: line})
		}
	}
	for k, s := range c.Sections {
		folded := t.folded(c, k)
		mark := "▾"
		if folded {
			mark = "▸"
		}
		title := mark + " " + strconv.Itoa(k+1) + " " + s.Title
		if s.Summary != "" {
			title += " · " + s.Summary
		}
		for _, line := range wrapText(title, w) {
			out = append(out, cardLine{text: line, title: true})
		}
		if folded || s.Body == nil {
			continue
		}
		for _, line := range wrapText(s.Body(max(1, w-2)), max(1, w-2)) {
			out = append(out, cardLine{text: "  " + line, role: s.Role})
		}
	}
	cc.body, cc.bodyKey, cc.hasBody = out, key.String(), true
	return out
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
	w := t.innerWidth()
	open := t.open[c.ID]
	focusedBorder, selectedBorder, border := s.CardBorders()
	h, v, tl, tr, bl, br, ml, mr := "─", "│", "╭", "╮", "╰", "╯", "├", "┤"
	switch {
	case selected && t.list.Focused():
		border = focusedBorder
		h, v, tl, tr, bl, br, ml, mr = "━", "┃", "┏", "┓", "┗", "┛", "┣", "┫"
	case selected && t.list.marksSelection():
		border = selectedBorder
		h, v, ml, mr = "━", "┃", "┣", "┫"
	}
	var out []string
	if t.groupStarts(i) {
		label := " " + modalLabel(c.Group) + " "
		rule := "──" + label
		rule += strings.Repeat("─", max(0, t.width-ansi.StringWidth(rule)))
		out = append(out, s.Muted.Render(ansi.Truncate(rule, t.width, "")))
	}
	top := tl + strings.Repeat(h, max(0, t.width-2)) + tr
	if c.Active && t.width >= 6 {
		top = tl + h + " ● " + strings.Repeat(h, max(0, t.width-6)) + tr
	}
	out = append(out, border.Render(top))
	row := func(text string, style lipgloss.Style) string {
		if ansi.StringWidth(text) > w {
			text = ansi.Truncate(text, w, "")
		}
		text += strings.Repeat(" ", max(0, w-ansi.StringWidth(text)))
		return border.Render(v) + " " + style.Render(text) + " " + border.Render(v)
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
			out = append(out, border.Render(ml+strings.Repeat(h, max(0, t.width-2))+mr))
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
	return append(out, border.Render(bl+strings.Repeat(h, max(0, t.width-2))+br))
}

// bottomOf is the anchor that puts shown card i's last line on the
// window's last line (the start when the cards above do not fill it).
func (t *Timeline) bottomOf(i, avail int) timelineAnchor {
	rem := avail
	for j := i; j >= 0; j-- {
		h := t.height(j)
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
	switch {
	case (t.tail && t.follow || t.toBottom) && sel == n-1:
		t.top = t.bottomOf(n-1, avail)
	case t.indexOf(t.top.id) < 0 || selID != t.lastSel || t.reveal:
		t.top = t.revealed(sel, avail)
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
		t.lastSel, t.reveal, t.toBottom = l.items[l.selected].ID, false, false
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
	if lines < 0 {
		t.follow = false
	}
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
	// The cards now on screen: the selection stays among them.
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
		return ok && len(c.Sections) > 0
	})
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
	noun := r.Nouns[timelineNoun]
	if noun.Guards == nil {
		noun.Guards = map[string]string{}
	}
	noun.Guards["scroll"] = "timeline-scrolls"
	noun.Guards["fold"] = "timeline-sections"
	scroll := func(lines int) Binding {
		return Do(timelineNoun+".scroll", Arguments{"lines": lines}).FromPayload("count", SourceCount)
	}
	b := r.Define(timelineNoun).Mode("normal").
		Bind("enter", Do(timelineNoun+".toggle")).
		Bind("space", Do(timelineNoun+".toggle").Hide()).
		Bind("l", Do(timelineNoun+".open")).
		Bind("right", Do(timelineNoun+".open").Hide()).
		Bind("h", Do(timelineNoun+".close")).
		Bind("left", Do(timelineNoun+".close").Hide()).
		Bind("z o", Do(timelineNoun+".open").Hide()).
		Bind("z c", Do(timelineNoun+".close").Hide()).
		Bind("z a", Do(timelineNoun+".toggle").Hide()).
		Bind("z R", Do(timelineNoun+".open-all")).
		Bind("z M", Do(timelineNoun+".close-all")).
		Bind("J", scroll(1).Hinted("J/K scroll")).
		Bind("K", scroll(-1).Hide()).
		Bind("ctrl+e", scroll(1).Hide()).
		Bind("ctrl+y", scroll(-1).Hide())
	for k := 1; k <= 9; k++ {
		n := strconv.Itoa(k)
		row := Do(timelineNoun+".fold", Arguments{"section": n})
		if k == 1 {
			row = row.Hinted("fold section")
		} else {
			row = row.Hide()
		}
		b = b.Bind("z "+n, row)
	}
	b.Mode("count_pending").Extends("normal")
}
