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
// card, l opens the step's folded section, h (and esc) go back to the card,
// y yanks the step. The current step is marked in the left padding of its
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
// header's lines; fold is its fold unit (-1 none) and folded its state.
type stepSpan struct {
	start, end int
	header     bool
	fold       int
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
	w := t.innerWidth()
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
			f := -1
			if first {
				f = fold
			}
			first = false
			for _, line := range wrapText(ch.text, width) {
				out = append(out, cardLine{text: indent + line, role: role})
			}
			par := ansi.Strip(ch.text)
			spans = append(spans, stepSpan{start: start, end: len(out), fold: f, text: func() string { return par }})
		}
		if first && from >= 0 {
			spans = append(spans, stepSpan{start: from, end: len(out), fold: fold, text: func() string { return "" }})
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
			spans = append(spans, stepSpan{start: start, end: len(out), fold: k, folded: folded, text: titledText(s.Title, s.Summary, s.Body, max(1, w-2))})
		}
		return out, spans
	}
	n, prevTitled := 0, false
	for _, s := range c.Steps {
		if s.Header {
			header := c.Header
			spans = append(spans, stepSpan{header: true, fold: -1, text: func() string { return header }})
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
				spans = append(spans, stepSpan{start: start, end: len(out), fold: -1, text: titledText("", "", s.Body, w)})
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
				spans = append(spans, stepSpan{start: start, end: len(out), fold: fold, folded: folded, text: titledText(s.Title, s.Summary, s.Body, max(1, w-2))})
			case s.Paragraphs:
				paras(s.Body(max(1, w-2)), max(1, w-2), "  ", s.Role, start, fold)
			default:
				for _, line := range wrapText(s.Body(max(1, w-2)), max(1, w-2)) {
					out = append(out, cardLine{text: "  " + line, role: s.Role})
				}
				spans = append(spans, stepSpan{start: start, end: len(out), fold: fold, text: titledText(s.Title, s.Summary, s.Body, max(1, w-2))})
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

// stepLines is the current step's first and last line in shown card i's
// drawing (its group rule, border, header, meta and divider counted).
func (t *Timeline) stepLines(i int) (int, int, bool) {
	c := t.shown(i)
	spans := t.spansOf(c)
	if len(spans) == 0 {
		return 0, 0, false
	}
	sp := spans[min(max(t.step, 0), len(spans)-1)]
	cc := t.measured(c)
	g := 0
	if t.groupStarts(i) {
		g = 1
	}
	rows := t.headerRows(c, cc)
	if sp.header {
		return g + 1, g + rows, true
	}
	start := g + 1 + rows + 1
	if c.Meta != "" {
		start++
	}
	if sp.end <= sp.start {
		return start + sp.start, start + sp.start, true
	}
	return start + sp.start, start + sp.end - 1, true
}

// stepOff is the lines of a step h lines tall scrolled past in a window
// of avail lines: 0 unless it is taller.
func (t *Timeline) stepOff(h, avail int) int {
	if avail <= 0 || h <= avail {
		return 0
	}
	return min(max(t.off, 0), h-avail)
}

// stepShown is the part of the current step a window of avail lines shows,
// in shown card i's lines; false outside the card.
func (t *Timeline) stepShown(i, avail int) (int, int, bool) {
	if i != t.list.selected || !t.InBody() {
		return 0, 0, false
	}
	from, to, ok := t.stepLines(i)
	if !ok {
		return 0, 0, false
	}
	lo := from + t.stepOff(to-from+1, avail)
	return lo, min(to, lo+avail-1), true
}

// showStep is the anchor that shows the current step of shown card sel:
// the window is kept when the step is on screen, else moved the least.
func (t *Timeline) showStep(sel, avail int) timelineAnchor {
	lo, hi, ok := t.stepShown(sel, avail)
	if !ok || !t.seek && t.indexOf(t.top.id) >= 0 {
		return t.top
	}
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
		t.seek = true
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
	t.in, t.step, t.off, t.seek = id, 0, 0, true
	t.follow = false
	return t.r.SetFocus(t.bodyID)
}

// leaveBody gives the body's focus (and any page or modal remembering it)
// back to the selected card.
func (t *Timeline) leaveBody() {
	to := t.list.focusTarget()
	t.r.RetargetFocus(func(id string) bool { return id == t.bodyID }, to)
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

// stepBy moves count steps (down when delta is positive): a step taller
// than the window moves half a window at a time first; past the last step
// j spills to the next card (or the next stop), past the first k goes back
// to the card. A count stops at the edge; only a move from the edge spills.
func (t *Timeline) stepBy(delta, count int) {
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
	avail := t.avail
	height := func(k int) int {
		i := t.indexOf(t.in)
		if i < 0 {
			return 1
		}
		was := t.step
		t.step = k
		from, to, _ := t.stepLines(i)
		t.step = was
		return to - from + 1
	}
	h := height(t.step)
	half := max(1, avail/2)
	t.off = t.stepOff(h, avail)
	t.seek = true
	if delta > 0 {
		if avail > 0 && h > avail && t.off < h-avail {
			t.off = min(t.off+half, h-avail)
			return true
		}
		if t.step < n-1 {
			t.step, t.off = t.step+1, 0
			return true
		}
		return edge && t.spillDown()
	}
	if t.off > 0 {
		t.off = max(0, t.off-half)
		return true
	}
	if t.step > 0 {
		t.step--
		t.off = 0
		if ph := height(t.step); avail > 0 && ph > avail {
			t.off = ph - avail // a tall step above shows its end first
		}
		return true
	}
	if edge {
		t.leaveBody()
	}
	return false
}

// spillDown is j past the last step: the next card (open or closed), or
// past the last card the next stop as the list spills; at the page's last
// stop, or NoSpill, the step stays.
func (t *Timeline) spillDown() bool {
	l := t.list
	if l.selected < len(l.items)-1 {
		l.Move(1) // the selection moves: the list hands the focus to the new card
		return true
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

// registerTimelineSteps defines the guards, verbs and body rows of the
// steps inside a card.
func registerTimelineSteps(r *Runtime) {
	r.DefineGuard("timeline-card-closed", func(r *Runtime, ev KeyEvent) bool {
		t, id := r.timelineCard(ev.Focus)
		return t != nil && !t.open[id]
	})
	r.DefineGuard("timeline-card-open", func(r *Runtime, ev KeyEvent) bool {
		t, id := r.timelineCard(ev.Focus)
		return t != nil && t.open[id]
	})
	r.DefineGuard("timeline-in-body", func(r *Runtime, ev KeyEvent) bool {
		t := r.timelineOf(ev.Focus)
		return t != nil && ev.Focus == t.bodyID
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
	step := func(delta int) Binding {
		return Do(timelineNoun+".step", Arguments{"delta": delta}).FromPayload("count", SourceCount)
	}
	r.Define(TimelineBodyNoun).Mode("normal").
		Bind("j", step(1)).
		Bind("down", step(1).Hide()).
		Bind("k", step(-1)).
		Bind("up", step(-1).Hide()).
		Bind("l", Do(timelineNoun+".enter")).
		Bind("right", Do(timelineNoun+".enter").Hide()).
		Bind("h", Do(timelineNoun+".leave")).
		Bind("left", Do(timelineNoun+".leave").Hide()).
		Bind("esc", Do(timelineNoun+".leave").Hide()).
		Bind("y", Do(timelineNoun+".yank-step")).
		Mode("count_pending").Extends("normal")
}
