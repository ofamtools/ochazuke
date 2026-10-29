package ochazuke

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Inside a card. An open card's body is a walk of steps: l on the open
// card steps inside, onto its first step, and the focus moves to the
// timeline's body instance (id+"/body", noun TimelineBodyNoun, under the
// card list), a part of the list: it holds the list's focus as a row does,
// so the card keeps its focused border, the mode stays, ? help and the
// footer read the body's rows, and a selection that moves (G, a filter, a
// removed card) takes the focus to the new card. Inside, j/k move by step
// (a step taller than the window by half a window first), past the last
// step j spills to the next card and past the first k goes back to the
// card, l opens the step's folded section, h folds the open section the
// step is in and otherwise (and esc) goes back to the card, y yanks the
// step. The current step is marked in the left padding of its
// lines, never by reversed text, and the window scrolls to keep it shown.

// TimelineBodyNoun is the noun of a timeline's body: the place inside the
// selected card while the focus walks its steps.
const TimelineBodyNoun = "ochazuke.timeline-body"

// stepMarker marks the current step's lines inside a card, in the left
// padding cell of each line.
const stepMarker = "▌"

// CardStep is one place inside an open card (Card.Steps), in order.
//
//   - Header: the step is the card's header (a prompt): it draws nothing
//     of its own, its lines are the header's and its text is Header.
//   - A Title draws a bold title line; with a Body the step folds like a
//     section ("▾ 2 tool Bash · summary", z 2, timeline.fold {section}
//     by ID, title or number), Folded its first state, and its body is
//     indented under the title. Folded, the title is the whole step; l
//     inside opens it.
//   - Paragraphs splits the Body into steps of its own, one per paragraph
//     (blank-line separated, a fenced code block one paragraph); under a
//     Title the title line belongs to the first.
//   - Without a Title the Body draws at the full width in Role.
//
// Steps follow each other with a blank line between them, except two
// titled steps in a row (they stack as sections do). ID keys the fold.
type CardStep struct {
	ID         string                 `json:"id,omitempty"`
	Title      string                 `json:"title,omitempty"`
	Summary    string                 `json:"summary,omitempty"`
	Role       CardRole               `json:"role,omitempty"`
	Folded     bool                   `json:"folded,omitempty"`
	Header     bool                   `json:"header,omitempty"`
	Paragraphs bool                   `json:"paragraphs,omitempty"`
	Body       func(width int) string `json:"-"`
}

func (s CardStep) foldable() bool { return !s.Header && s.Title != "" && s.Body != nil }

// ParentStop names where h on a closed card of the timeline goes (an
// instance; a stop lands on its remembered or selected child). Without it
// h on a closed card stays.
func ParentStop(id string) InstanceOption { return func(s *instanceSpec) { s.parentStop = id } }

// SetParentStop changes where h on a closed card goes ("" stays).
func (t *Timeline) SetParentStop(id string) { t.parentStop = id }

// BodyID is the id of the instance that holds the focus inside a card.
func (t *Timeline) BodyID() string { return t.bodyID }

// stepSpan is one step of a built body: body lines [start, end), or the
// header's lines; fold is the fold unit whose title it holds (-1 none) and
// folded its state; unit is the fold unit it lies in (its title or one of
// its paragraphs, -1 none).
type stepSpan struct {
	start, end int
	header     bool
	fold       int
	unit       int
	folded     bool
	text       func() string
}

// chunk is a paragraph of a text, or one blank line between paragraphs.
type chunk struct {
	text  string
	blank bool
}

// paragraphChunks splits text into paragraphs at blank lines, a fenced
// code block (``` or ~~~) one paragraph whatever it holds, keeping every
// blank line between them.
func paragraphChunks(text string) []chunk {
	text = strings.TrimRight(strings.ReplaceAll(text, "\t", "    "), "\n")
	if text == "" {
		return nil
	}
	var out []chunk
	var cur []string
	fence := ""
	flush := func() {
		if len(cur) > 0 {
			out = append(out, chunk{text: strings.Join(cur, "\n")})
			cur = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		plain := strings.TrimSpace(ansi.Strip(line))
		if fence != "" {
			cur = append(cur, line)
			if strings.HasPrefix(plain, fence) {
				fence = ""
			}
			continue
		}
		if plain == "" {
			flush()
			out = append(out, chunk{blank: true})
			continue
		}
		if strings.HasPrefix(plain, "```") || strings.HasPrefix(plain, "~~~") {
			fence = plain[:3]
		}
		cur = append(cur, line)
	}
	flush()
	return out
}

// buildBody is an open card's body lines and its steps: Steps, else the
// Body's paragraphs and then each section.
func (t *Timeline) buildBody(c *Card, units []foldUnit) ([]cardLine, []stepSpan) {
	w := t.innerOf(c)
	var out []cardLine
	var spans []stepSpan
	// paras adds text's paragraphs, each a step; from >= 0 is a title
	// line above that belongs to the first (fold its unit).
	paras := func(text string, width int, indent string, role CardRole, from, fold int) {
		first := true
		for _, ch := range paragraphChunks(text) {
			if ch.blank {
				out = append(out, cardLine{})
				continue
			}
			start := len(out)
			if first && from >= 0 {
				start = from
			}
			f, unit := -1, -1
			if first {
				f = fold
			}
			if from >= 0 {
				unit = fold
			}
			first = false
			for _, line := range wrapText(ch.text, width) {
				out = append(out, cardLine{text: indent + line, role: role})
			}
			par := ansi.Strip(ch.text)
			spans = append(spans, stepSpan{start: start, end: len(out), fold: f, unit: unit, text: func() string { return par }})
		}
		if first && from >= 0 {
			spans = append(spans, stepSpan{start: from, end: len(out), fold: fold, unit: fold, text: func() string { return "" }})
		}
	}
	if len(c.Steps) == 0 {
		if c.Body != nil {
			paras(c.Body(w), w, "", CardRoleText, -1, -1)
		}
		for k, s := range c.Sections {
			folded := t.folded(c.ID, units[k])
			mark := "▾"
			if folded {
				mark = "▸"
			}
			title := mark + " " + strconv.Itoa(k+1) + " " + s.Title
			if s.Summary != "" {
				title += " · " + s.Summary
			}
			start := len(out)
			for _, line := range wrapText(title, w) {
				out = append(out, cardLine{text: line, title: true})
			}
			if !folded && s.Body != nil {
				for _, line := range wrapText(s.Body(max(1, w-2)), max(1, w-2)) {
					out = append(out, cardLine{text: "  " + line, role: s.Role})
				}
			}
			spans = append(spans, stepSpan{start: start, end: len(out), fold: k, unit: k, folded: folded, text: titledText(s.Title, s.Summary, s.Body, max(1, w-2))})
		}
		return out, spans
	}
	n, prevTitled := 0, false
	for _, s := range c.Steps {
		if s.Header {
			header := c.Header
			spans = append(spans, stepSpan{header: true, fold: -1, unit: -1, text: func() string { return header }})
			continue
		}
		titled := s.Title != ""
		if !titled && s.Body == nil {
			continue
		}
		sep := len(out)
		if sep > 0 && !(titled && prevTitled) {
			out = append(out, cardLine{})
		}
		mark := len(out)
		had := len(spans)
		prevTitled = titled
		switch {
		case !titled && s.Paragraphs:
			paras(s.Body(w), w, "", s.Role, -1, -1)
		case !titled:
			start := len(out)
			for _, line := range wrapText(s.Body(w), w) {
				out = append(out, cardLine{text: line, role: s.Role})
			}
			if len(out) > start {
				spans = append(spans, stepSpan{start: start, end: len(out), fold: -1, unit: -1, text: titledText("", "", s.Body, w)})
			}
		default:
			title, fold, folded := s.Title, -1, false
			if s.foldable() {
				fold, folded = n, t.folded(c.ID, units[n])
				n++
				m := "▾"
				if folded {
					m = "▸"
				}
				title = m + " " + strconv.Itoa(n) + " " + s.Title
			}
			if s.Summary != "" {
				title += " · " + s.Summary
			}
			start := len(out)
			for _, line := range wrapText(title, w) {
				out = append(out, cardLine{text: line, title: true})
			}
			switch {
			case folded || s.Body == nil:
				spans = append(spans, stepSpan{start: start, end: len(out), fold: fold, unit: fold, folded: folded, text: titledText(s.Title, s.Summary, s.Body, max(1, w-2))})
			case s.Paragraphs:
				paras(s.Body(max(1, w-2)), max(1, w-2), "  ", s.Role, start, fold)
			default:
				for _, line := range wrapText(s.Body(max(1, w-2)), max(1, w-2)) {
					out = append(out, cardLine{text: "  " + line, role: s.Role})
				}
				spans = append(spans, stepSpan{start: start, end: len(out), fold: fold, unit: fold, text: titledText(s.Title, s.Summary, s.Body, max(1, w-2))})
			}
		}
		if len(spans) == had && len(out) == mark {
			out = out[:sep] // a step that drew nothing takes no blank line
		}
	}
	return out, spans
}

// titledText is a step's text for a yank: its title line and its body.
func titledText(title, summary string, body func(int) string, width int) func() string {
	return func() string {
		var parts []string
		if title != "" {
			if summary != "" {
				title += " · " + summary
			}
			parts = append(parts, title)
		}
		if body != nil {
			if text := strings.TrimRight(ansi.Strip(body(width)), "\n"); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
}

// InBody reports whether the focus is inside the selected card.
func (t *Timeline) InBody() bool {
	return t.in != "" && t.r.Focus == t.bodyID && t.in == t.SelectedID() && t.open[t.in]
}

// Step is the card the focus is inside and its current step (from 0);
// false outside a card.
func (t *Timeline) Step() (card string, step int, ok bool) {
	if !t.InBody() {
		return "", 0, false
	}
	spans := t.currentSpans()
	return t.in, min(max(t.step, 0), max(0, len(spans)-1)), true
}

// StepText is the current step's text (what y yanks); false outside a card.
func (t *Timeline) StepText() (string, bool) {
	if !t.InBody() {
		return "", false
	}
	spans := t.currentSpans()
	if len(spans) == 0 {
		return "", false
	}
	return spans[min(max(t.step, 0), len(spans)-1)].text(), true
}

// spansOf is an open card's steps at the current width (none closed).
func (t *Timeline) spansOf(c *Card) []stepSpan {
	if !t.open[c.ID] {
		return nil
	}
	cc := t.measured(c)
	t.body(c, cc)
	return cc.steps
}

// currentSpans are the steps of the card the focus is inside.
func (t *Timeline) currentSpans() []stepSpan {
	i, ok := t.byID[t.in]
	if !ok {
		return nil
	}
	return t.spansOf(&t.cards[i])
}

// cardOffsets are shown card i's drawing lines before its parts: g the
// group rule's lines (0 or 1), the header's rows (from g+1), and start,
// the first body line (after the meta line and the divider).
func (t *Timeline) cardOffsets(i int) (g, rows, start int) {
	c := t.shown(i)
	if t.groupStarts(i) {
		g = 1
	}
	rows = t.headerRows(c, t.measured(c))
	start = g + 1 + rows + 1
	if c.Meta != "" {
		start++
	}
	return g, rows, start
}

// stepLines is the current step's first and last line in shown card i's
// drawing (its group rule, border, header, meta and divider counted); on
// a row (gj/gk) that row alone.
func (t *Timeline) stepLines(i int) (int, int, bool) {
	if t.rowLine >= 0 {
		if line, _, ok := t.rowAt(i); ok {
			return line, line, true
		}
	}
	c := t.shown(i)
	spans := t.spansOf(c)
	if len(spans) == 0 {
		return 0, 0, false
	}
	return t.spanLines(i, min(max(t.step, 0), len(spans)-1))
}

// spanLines is step k's first and last line in shown card i's drawing.
func (t *Timeline) spanLines(i, k int) (int, int, bool) {
	spans := t.spansOf(t.shown(i))
	if k < 0 || k >= len(spans) {
		return 0, 0, false
	}
	sp := spans[k]
	g, rows, start := t.cardOffsets(i)
	if sp.header {
		return g + 1, g + rows, true
	}
	if sp.end <= sp.start {
		return start + sp.start, start + sp.start, true
	}
	return start + sp.start, start + sp.end - 1, true
}

// stepRow is a screen row inside an open card that gj/gk land on: its
// line in the card's drawing and the step it belongs to.
type stepRow struct{ line, step int }

// stepRows are shown card i's rows in order: the header step's lines and
// every body line of a step, blank lines left out.
func (t *Timeline) stepRows(i int) []stepRow {
	c := t.shown(i)
	spans := t.spansOf(c)
	if len(spans) == 0 {
		return nil
	}
	body := t.body(c, t.measured(c))
	g, rows, start := t.cardOffsets(i)
	var out []stepRow
	for k, sp := range spans {
		if sp.header {
			for l := 0; l < rows; l++ {
				out = append(out, stepRow{g + 1 + l, k})
			}
			continue
		}
		if sp.end <= sp.start {
			out = append(out, stepRow{start + sp.start, k})
			continue
		}
		for l := sp.start; l < sp.end; l++ {
			if l < len(body) && body[l].text == "" && !body[l].title {
				continue
			}
			out = append(out, stepRow{start + l, k})
		}
	}
	return out
}

// rowAt is the row the cursor is on in shown card i (the first at or
// after rowLine, the last when the card shrank), its index in stepRows.
func (t *Timeline) rowAt(i int) (line, index int, ok bool) {
	rows := t.stepRows(i)
	if len(rows) == 0 {
		return 0, 0, false
	}
	for k, r := range rows {
		if r.line >= t.rowLine {
			return r.line, k, true
		}
	}
	return rows[len(rows)-1].line, len(rows) - 1, true
}

// stepShown is the current step's lines in shown card i's lines (the
// frame keeps the part on screen); false outside the card.
func (t *Timeline) stepShown(i int) (int, int, bool) {
	if i != t.list.selected || !t.InBody() {
		return 0, 0, false
	}
	return t.stepLines(i)
}

// showStep is the anchor that shows the current step of shown card sel
// after an l (a step entered or unfolded): the window is kept when the
// step is on screen, else moved the least (a step taller than the window
// shows its top).
func (t *Timeline) showStep(sel, avail int) timelineAnchor {
	lo, hi, ok := t.stepShown(sel)
	if !ok || !t.seek && t.indexOf(t.top.id) >= 0 {
		return t.top
	}
	hi = min(hi, lo+avail-1)
	id := t.list.items[sel].ID
	top := t.top
	if t.indexOf(top.id) < 0 || t.after(top, timelineAnchor{id, lo}) {
		return timelineAnchor{id, lo}
	}
	if end := t.bottomAt(sel, hi, avail); t.after(end, top) {
		top = end
	}
	return top
}

// enter is timeline.enter on card id: from the card, open it and step
// onto its first step; inside it, open the current step's folded section.
func (t *Timeline) enter(id string) error {
	if t.InBody() && id == t.in {
		spans := t.currentSpans()
		if len(spans) == 0 {
			return nil
		}
		sp := spans[min(max(t.step, 0), len(spans)-1)]
		if sp.fold < 0 || !sp.folded {
			return nil
		}
		i := t.byID[id]
		units := foldUnits(&t.cards[i])
		if err := t.SetFolded(id, units[sp.fold].name, false); err != nil {
			return err
		}
		t.seek, t.rowLine = true, -1
		return nil
	}
	if !t.open[id] {
		if err := t.SetOpen(id, true); err != nil {
			return err
		}
	}
	if t.SelectedID() != id {
		if err := t.list.Select(id); err != nil {
			return err
		}
	}
	if t.width > 0 && len(t.spansOf(&t.cards[t.byID[id]])) == 0 {
		return nil // nothing inside to step onto
	}
	t.in, t.step, t.seek, t.rowLine = id, 0, true, -1
	t.follow = false
	t.placeBody()
	return t.r.SetFocus(t.bodyID)
}

// placeBody parents the body under the card it walks (t.in), else under
// the list: inside a card the focus path is body, card, list, so the rows
// of the card's noun answer there as they do on the card (a key the body
// does not bind goes on to the card, then the list and the timeline).
func (t *Timeline) placeBody() {
	inst := t.r.Instances[t.bodyID]
	if inst == nil {
		return
	}
	inst.Parent = t.list.id
	if t.in != "" && t.r.rows[t.in] == t.list.id {
		inst.Parent = t.in
	}
}

// openUnit is the open fold unit the current step lies in (its title or
// one of its paragraphs): a section or titled step shown unfolded; -1
// when there is none.
func (t *Timeline) openUnit() int {
	if !t.InBody() {
		return -1
	}
	spans := t.currentSpans()
	if len(spans) == 0 {
		return -1
	}
	sp := spans[min(max(t.step, 0), len(spans)-1)]
	if sp.unit < 0 {
		return -1
	}
	units := foldUnits(&t.cards[t.byID[t.in]])
	if sp.unit >= len(units) || t.folded(t.in, units[sp.unit]) {
		return -1
	}
	return sp.unit
}

// foldStep is timeline.fold-step: fold the open section the current step
// lies in; the step becomes its title line (the cursor stays on it).
func (t *Timeline) foldStep() error {
	u := t.openUnit()
	if u < 0 {
		return errors.New("the current step is not in an open section")
	}
	spans := t.currentSpans()
	for k, sp := range spans {
		if sp.unit == u {
			t.step = k
			break
		}
	}
	units := foldUnits(&t.cards[t.byID[t.in]])
	if err := t.SetFolded(t.in, units[u].name, true); err != nil {
		return err
	}
	t.seek, t.rowLine = true, -1
	return nil
}

// leaveBody gives the body's focus (and any page or modal remembering it)
// back to the selected card.
func (t *Timeline) leaveBody() {
	to := t.list.focusTarget()
	t.r.RetargetFocus(func(id string) bool { return id == t.bodyID }, to)
	t.in, t.rowLine = "", -1
	t.placeBody()
}

// leave is timeline.leave: inside a card, back to the card (still open);
// on a card, to the parent stop when there is one.
func (t *Timeline) leave() error {
	if t.r.Focus == t.bodyID {
		t.leaveBody()
		return nil
	}
	if t.parentStop == "" || t.r.Instances[t.parentStop] == nil {
		return nil
	}
	return t.r.SetFocus(t.r.landing(t.parentStop))
}

// stepBy moves count steps (down when delta is positive) by the half-page
// rule (R3.14): when showing the next step would scroll more than half the
// window, the window scrolls half and the step moves once the next step
// shows, so a step taller than the window is read half a window per key.
// Past the last step j goes to the next card the same way (past the last
// card the window first shows the card's end, then the list spills or,
// NoSpill, stays); past the first k goes back to the card. A count stops
// at the edge; only a move from the edge spills.
func (t *Timeline) stepBy(delta, count int) {
	t.settle()
	t.rowLine = -1 // from a row (gj/gk), on by step from the row's step
	for k := 0; k < max(1, count); k++ {
		if !t.InBody() || !t.stepOnce(delta, k == 0) {
			return
		}
	}
}

func (t *Timeline) stepOnce(delta int, edge bool) bool {
	spans := t.currentSpans()
	n := len(spans)
	if n == 0 {
		if edge {
			t.leaveBody()
		}
		return false
	}
	t.step = min(max(t.step, 0), n-1)
	next := t.step + delta
	switch {
	case next >= n:
		return edge && t.spillDown()
	case next < 0:
		if edge {
			t.leaveBody()
		}
		return false
	}
	i := t.indexOf(t.in)
	if i < 0 || !t.drawn() {
		t.step, t.seek = next, true
		return true
	}
	t.seek = false // the motion places the window itself
	at := t.relStart(i)
	was := t.step
	t.step = next
	from, to, _ := t.stepLines(i)
	t.step = was
	a, b := at+from, at+to
	// j into a tall step shows its top, k its end.
	moved := t.motionShift(minimalReveal(a, b, t.avail, delta))
	if SpanInView(a-moved, b-moved, t.avail) {
		t.step = next
	}
	return true
}

// rowBy is gj/gk inside a card: count screen rows (down when delta is
// positive); the step under the row is the current one (y yanks it, l and
// h act on it), and the marker covers the row alone. Past the last row
// gj goes on as j does past the last step, before the first gk back to
// the card; a count stops at the edge. j/k go on by step from the row's
// step.
func (t *Timeline) rowBy(delta, count int) {
	t.settle()
	for k := 0; k < max(1, count); k++ {
		if !t.InBody() || !t.rowOnce(delta, k == 0) {
			return
		}
	}
}

func (t *Timeline) rowOnce(delta int, edge bool) bool {
	i := t.indexOf(t.in)
	if i < 0 {
		return false
	}
	rows := t.stepRows(i)
	if len(rows) == 0 {
		if edge {
			t.leaveBody()
		}
		return false
	}
	cur := 0
	if t.rowLine >= 0 {
		_, cur, _ = t.rowAt(i)
	} else {
		// From the step's first row on screen (its first row when none is).
		step := min(max(t.step, 0), rows[len(rows)-1].step)
		first := -1
		at := t.relStart(i)
		for k, r := range rows {
			if r.step != step {
				continue
			}
			if first < 0 {
				first = k
			}
			if !t.drawn() || SpanInView(at+r.line, at+r.line, t.avail) {
				first = k
				break
			}
		}
		cur = max(first, 0)
	}
	next := cur + delta
	switch {
	case next >= len(rows):
		if !edge {
			return false
		}
		t.rowLine = -1
		t.step = rows[len(rows)-1].step
		return t.spillDown()
	case next < 0:
		if edge {
			t.leaveBody()
		}
		return false
	}
	t.seek = false // the motion places the window itself
	if t.drawn() {
		a := t.relStart(i) + rows[next].line
		t.motionShift(minimalReveal(a, a, t.avail, delta))
	}
	t.rowLine, t.step = rows[next].line, rows[next].step
	return true
}

// keepStepInView is J/K inside a card: when the scroll took the current
// step (or row) off screen, the cursor moves to the nearest one on
// screen, so the next step key goes on from what is shown instead of
// scrolling back.
func (t *Timeline) keepStepInView(down bool) {
	if !t.InBody() || !t.drawn() {
		return
	}
	i := t.indexOf(t.in)
	if i < 0 {
		return
	}
	at := t.relStart(i)
	if lo, hi, ok := t.stepLines(i); !ok || SpanInView(at+lo, at+hi, t.avail) {
		return
	}
	inView := func(lo, hi int) bool { return SpanInView(at+lo, at+hi, t.avail) }
	if t.rowLine >= 0 {
		rows := t.stepRows(i)
		pick := -1
		for k, r := range rows {
			if inView(r.line, r.line) {
				pick = k
				if down {
					break
				}
			}
		}
		if pick >= 0 {
			t.rowLine, t.step = rows[pick].line, rows[pick].step
		}
		return
	}
	pick := -1
	for k := range t.spansOf(t.shown(i)) {
		if lo, hi, ok := t.spanLines(i, k); ok && inView(lo, hi) {
			pick = k
			if down {
				break
			}
		}
	}
	if pick >= 0 {
		t.step = pick
	}
}

// spillDown is j past the last step: the next card (open or closed) by the
// half-page rule, or on the last card its end first and then the next stop
// as the list spills; at the page's last stop, or NoSpill, the step stays.
func (t *Timeline) spillDown() bool {
	l := t.list
	if l.selected < len(l.items)-1 {
		// The selection moves (once the card shows): the list hands the
		// focus to the new card.
		return t.cardOnce(1, true)
	}
	if t.drawn() {
		if want := t.edgeWant(1); want != 0 {
			t.motionShift(want)
			return true
		}
	}
	if l.noSpill {
		return false
	}
	if _, ok := l.neighbour(1); !ok {
		return false
	}
	t.r.setFocus(l.items[l.selected].ID)
	return l.spill(1)
}

// timelineCard is the timeline and card a focus is on, when it is a card.
func (r *Runtime) timelineCard(focus string) (*Timeline, string) {
	t := r.timelineOf(focus)
	if t == nil || r.rows[focus] != t.list.id {
		return nil, ""
	}
	return t, focus
}

// timelineTarget is the timeline and card an action judged at ev would act
// on, as the verbs resolve them: the "id" argument's timeline, else the
// focused one; the "card" argument, else the focused card, else the
// selected card ("" when there is none). A row on the timeline noun runs
// with the timeline instance as its target, so this is the card t.target
// resolves in the verb. named reports an argument naming a
// timeline or card that does not exist: the verb's own error says so, so
// availability does not refuse it first.
func (r *Runtime) timelineTarget(ev KeyEvent) (t *Timeline, card string, named bool) {
	if id := StringArg(ev.Args, "id", ""); id != "" {
		if t = r.Timeline(id); t == nil {
			return nil, "", true
		}
	} else if t = r.timelineOf(ev.Focus); t == nil {
		return nil, "", false
	}
	if id := StringArg(ev.Args, "card", ""); id != "" {
		if _, ok := t.byID[id]; !ok {
			return t, "", true
		}
		return t, id, false
	}
	if r.rows[ev.Focus] == t.list.id {
		return t, ev.Focus, false
	}
	return t, t.SelectedID(), false
}

// registerTimelineSteps defines the availability, verbs and body rows of
// the steps inside a card. What a card can do depends on its state, and the
// actions say so (Available): a closed card can open, an open card can
// close or be entered, inside a card the steps move, fold and leave. The
// rows (bindings/timeline.json) bind l and h to several actions each, and
// the first available answers.
func registerTimelineSteps(r *Runtime) {
	cardIs := func(open bool) Guard {
		return func(r *Runtime, ev KeyEvent) bool {
			t, id, named := r.timelineTarget(ev)
			return named || id != "" && t.open[id] == open
		}
	}
	// timeline.open: the card is closed; timeline.close: it is open. As
	// row guards (the older form, kept for user rows) they read the focused
	// card the same way.
	r.DefineGuard("timeline-card-closed", cardIs(false))
	r.DefineGuard("timeline-card-open", cardIs(true))
	// timeline.enter: there is a card to enter.
	r.DefineGuard("timeline-has-card", func(r *Runtime, ev KeyEvent) bool {
		_, id, named := r.timelineTarget(ev)
		return named || id != ""
	})
	// timeline.leave: the focus is on a card or inside one. leave acts on
	// the live focus (the body, else the parent stop), so that is what it
	// judges; a call naming a timeline is not refused here.
	r.DefineGuard("timeline-on-card", func(r *Runtime, ev KeyEvent) bool {
		if StringArg(ev.Args, "id", "") != "" {
			return true
		}
		t := r.timelineOf(ev.Focus)
		return t != nil && (r.Focus == t.bodyID || r.rows[r.Focus] == t.list.id)
	})
	r.DefineGuard("timeline-in-body", func(r *Runtime, ev KeyEvent) bool {
		t := r.timelineOf(ev.Focus)
		return t != nil && ev.Focus == t.bodyID
	})
	r.DefineGuard("timeline-step-open", func(r *Runtime, ev KeyEvent) bool {
		t := r.timelineOf(ev.Focus)
		return t != nil && ev.Focus == t.bodyID && t.openUnit() >= 0
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
		run               func(*Timeline, Context, Arguments) (any, error)
	}{
		{"enter", "Step inside the card onto its first step (opening it); inside, open the current step's folded section", false, map[string]Parameter{"id": id, "card": card}, func(t *Timeline, c Context, a Arguments) (any, error) {
			k := t.in
			if focusOf(c) != t.bodyID || StringArg(a, "card", "") != "" {
				var err error
				if k, err = t.target(c, a); err != nil {
					return nil, err
				}
			}
			return t.state(), t.enter(k)
		}},
		{"fold-step", "Inside a card, fold the open section the current step is in (a tool call, thinking, an answer); the step becomes its title", false, map[string]Parameter{"id": id}, func(t *Timeline, _ Context, _ Arguments) (any, error) {
			if !t.InBody() {
				return nil, errors.New("not inside a card")
			}
			return t.state(), t.foldStep()
		}},
		{"leave", "Inside a card, back to the card (it stays open); on a card, to the timeline's parent stop", false, map[string]Parameter{"id": id}, func(t *Timeline, _ Context, _ Arguments) (any, error) {
			return t.state(), t.leave()
		}},
		{"step", "Move by steps inside the card, half a window at a time through a step taller than the window; past the last step on to the next card, before the first back to the card", true, map[string]Parameter{"id": id, "delta": {Type: "integer", Required: true, Description: "Steps to move, down when positive"}, "count": {Type: "integer", Description: "Repeat count (3j)"}}, func(t *Timeline, _ Context, a Arguments) (any, error) {
			if !t.InBody() {
				return nil, errors.New("not inside a card")
			}
			delta := IntArg(a, "delta", 1)
			switch {
			case delta > 0:
				t.stepBy(1, delta*max(1, IntArg(a, "count", 1)))
			case delta < 0:
				t.stepBy(-1, -delta*max(1, IntArg(a, "count", 1)))
			}
			return t.state(), nil
		}},
		{"row", "Move by screen rows inside the card (gj/gk): the step under the row is the current one; past the last row on to the next card, before the first back to the card", true, map[string]Parameter{"id": id, "delta": {Type: "integer", Required: true, Description: "Rows to move, down when positive"}, "count": {Type: "integer", Description: "Repeat count (3gj)"}}, func(t *Timeline, _ Context, a Arguments) (any, error) {
			if !t.InBody() {
				return nil, errors.New("not inside a card")
			}
			delta := IntArg(a, "delta", 1)
			switch {
			case delta > 0:
				t.rowBy(1, delta*max(1, IntArg(a, "count", 1)))
			case delta < 0:
				t.rowBy(-1, -delta*max(1, IntArg(a, "count", 1)))
			}
			return t.state(), nil
		}},
		{"yank-step", "Yank the current step's text into the register (the active one, else the unnamed)", false, map[string]Parameter{"id": id, "register": {Type: "string", Description: "The register (default: the active one, else the unnamed)"}}, func(t *Timeline, _ Context, a Arguments) (any, error) {
			text, ok := t.StepText()
			if !ok {
				return nil, errors.New("not inside a card")
			}
			y := Yank{Text: text, Shape: "char"}
			if strings.Contains(text, "\n") {
				y = Yank{Text: text + "\n", Shape: "line"}
			}
			if err := r.StoreYank(y, StringArg(a, "register", ""), false); err != nil {
				return nil, err
			}
			out := t.state()
			out["text"] = text
			return out, nil
		}},
	} {
		r.Register(Operation{Verb: spec.verb, Noun: timelineNoun, Description: spec.description, Motion: spec.motion, Parameters: spec.parameters, Result: "timeline state", Run: func(_ *Runtime, c Context, args Arguments) (any, error) {
			t, err := resolve(c, args)
			if err != nil {
				return nil, err
			}
			return spec.run(t, c, args)
		}})
	}
}
