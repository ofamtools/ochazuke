package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// A chooser is a command-palette-shaped modal: a search input focused in
// insert mode over a filtered list of rows. Enter takes the top match unless
// the highlight was moved, and creates from the query when nothing matches.
// No key creates: with CreateRow, creating is a row the highlight moves
// onto. It knows nothing of the application: rows, the filter and the
// choose/create callbacks come from the caller, and its keys are rows on the
// chooser noun, so an application overrides them like any other row.
//
// Lifted from mtm (cmd/mtm/chooser.go). The Chooser is the modal instance's
// Component, so Runtime holds no chooser state: Runtime.Chooser(kind) reads
// it back and the Model draws it.

// ChooserRow is one choice. Trailing rows (a "browse…" entry) follow the
// matches. Enter takes one without moving onto it only when the query is
// not empty and no ordinary row matches, so their filter should be strict
// (a substring of the label); an empty query with no ordinary rows creates.
// Fixed rows (a "no pin" entry) sit at the bottom whatever the query: the
// filter never sees them and Enter takes one only once the highlight is
// moved onto it, or when the query spells it.
type ChooserRow struct {
	// Noun names the row's declared props and saved filters; empty uses ChooserSpec.Noun.
	Noun string
	// Props supplies typed values when Data is an opaque handle, as on ListItem.
	Props    Thing
	ID       string
	Label    string
	Data     any
	Trailing bool
	Fixed    bool
	// Cells, when set, draw the row as aligned columns (each column as
	// wide as its widest cell among the rows); Label stays what the
	// filter reads.
	Cells []string
	// Opens marks a row its source can open (ChooserSource.Open: a
	// folder to browse into): chooser.open opens it, and so does enter
	// unless the setting chooser.enter is "pick". NoPick marks a row that
	// only opens (a folder in a file chooser): picking it opens it.
	Opens  bool
	NoPick bool
	// Refuse marks a row that cannot be picked and says why: picking it
	// fails with Refuse and the chooser stays open (a typed path whose
	// folder is missing, R7.8).
	Refuse string
	// Tones colour runs of the row through the theme's tones, as a list
	// row's ToneSpan does (tone.go): Cell indexes Cells, or is 0 for the
	// Label when the row has no Cells; From and To are rune offsets into
	// that text (an app dims a path prefix, "parent › "). The highlighted
	// row keeps them over its reverse bar, as a list's selected row does.
	Tones []ToneSpan
}

// ChooserSpec opens a chooser. Kind is the modal's kind (one open chooser
// per kind). Title is the modal's title. Filter defaults to a fuzzy match
// on Label, case as the search.case setting says (CaseFor; a Filter of the
// application's own can read Runtime.CaseFor too); matches whose label
// holds the query whole come first, otherwise in Rows order. Create is optional; without it Enter on no match fails.
// CreateLabel names what Enter would create from the query. CreateRow shows
// that as a "+ <label>" row while the query is not empty, after the matches
// and trailing rows and before the fixed rows; typing never matches or
// ranks it, it is the default only when nothing else matches, and Enter on
// it creates. Empty is the line shown while there are no rows at all
// ("loading…"). Keep names the cell columns cut last. Preview draws a
// preview of the highlighted row beside the rows when there is room.
type ChooserSpec struct {
	// Noun is the default row noun for prop filtering.
	Noun string
	// NoPropFilters opts this chooser out of prop filters and their keys.
	NoPropFilters bool

	Kind        string
	Title       string
	Rows        []ChooserRow
	Filter      func(query string, row ChooserRow) bool
	Choose      func(ChooserRow) Result
	Create      func(query string) Result
	CreateLabel func(query string) string
	CreateRow   bool
	Empty       string
	// Keep names the columns (indexes into ChooserRow.Cells) cut last when
	// the cells do not fit (a directory chooser's path): the others narrow
	// to their minimum first, then the kept ones, widest first.
	Keep []int
	// Preview, when set, draws the highlighted row's text beside the rows
	// (wrapped, cut at the bottom) once the chooser is ChooserPreviewMin
	// cells wide; narrower, the rows take the whole width.
	Preview func(ChooserRow) string
	// Sources, when set, supply the rows one source at a time (see
	// ChooserSource; Rows is then the current source's). Source names
	// the one to open on; else (or when this open has no source of that
	// name) the one last used for the kind (chooser.remember-source),
	// else the first.
	Sources []ChooserSource
	Source  string
}

// ChooserPreviewMin is the width from which a chooser with a Preview
// draws it beside the rows.
const ChooserPreviewMin = 60

// chooserNoun is the noun of every chooser's search input; its rows are
// the chooser's keys.
const chooserNoun = "ochazuke.chooser"

// chooserCreateRow is the ID of the create row (see ChooserSpec.CreateRow).
const chooserCreateRow = "chooser:create"

// Chooser is one open chooser, the Component of its modal instance.
type Chooser struct {
	r       *Runtime
	spec    ChooserSpec
	input   *Input
	seen    string
	matches []ChooserRow
	// cursor indexes matches: the highlighted row, -1 for none. Until the
	// user moves it, it is the top match (see ChooserRow on trailing rows).
	cursor int
	moved  bool
	// The sources (chooser_sources.go): the current one, the query its
	// rows were loaded for, a load in flight, its error, and the load
	// generation (an older load's rows never land).
	source      int
	loadedQuery string
	loading     bool
	loadErr     string
	gen         uint64
	// tallest is the most lines the rows view has drawn while open: with
	// chooser.stable-height it pads to it, so typing never resizes it.
	tallest int
	// match is the match mode search.cycle-match or search.set-match
	// chose while open; "" is the chooser.match setting (search_match.go).
	match string

	// filters is the shared list prop-filter controller, without a registered
	// list or row instances: the chooser retains its own ranking and focus.
	filters      *List
	propsEnabled bool
}

// ChooserStableHeightSetting keeps an open chooser at the height it
// reached (settings/ochazuke.json).
const ChooserStableHeightSetting = "chooser.stable-height"

// The delivery contract: a chooser runs no jobs of its own (Choose and
// Create return their own results). Pending work they return belongs to the
// focus the chooser opened over (runApplication), since the chooser and its
// input are closed before the callback runs.
func (c *Chooser) HandleMessage(tea.Msg) bool { return false }
func (c *Chooser) Close()                     {}

// PendingCommand follows the query after every event: a Live source loads
// again for the edited query, and the work it queues is handed over here
// (the runtime's queue was drained before the components were asked).
func (c *Chooser) PendingCommand() tea.Cmd {
	if c.r.Chooser(c.spec.Kind) != c {
		return nil
	}
	c.sync()
	if len(c.r.commands) == 0 {
		return nil
	}
	commands := c.r.commands
	c.r.commands = nil
	return tea.Batch(commands...)
}

func registerChooser(r *Runtime) {
	verb := func(fn func(*Chooser, Ctx) Result) AppHandler {
		return func(ctx Ctx) Result {
			c := r.chooserAt(ctx.Instance)
			if c == nil {
				return Failed(errors.New("no chooser is open"))
			}
			c.sync()
			return fn(c, ctx)
		}
	}
	r.Define(chooserNoun).Pin("insert").
		Verb("pick", verb(func(c *Chooser, _ Ctx) Result { return c.pick() }), "Take the highlighted row, else the top match, else create from the query").
		Verb("move", verb(func(c *Chooser, ctx Ctx) Result {
			c.move(IntArg(ctx.Args, "delta", 0))
			return Done(c.state())
		}), "Move the highlight; from none, down selects first and up follows chooser.up-from-none").Motion().
		Verb("cancel", verb(func(c *Chooser, _ Ctx) Result {
			if err := c.close(); err != nil {
				return Failed(err)
			}
			return Done(nil)
		}), "Close without choosing")
	move := r.Operations["ochazuke.chooser.move"]
	move.Parameters = map[string]Parameter{"delta": {Type: "integer", Required: true}}
	r.Operations["ochazuke.chooser.move"] = move
	registerChooserSources(r)
}

// OpenChooser opens a chooser modal with its search input focused in
// insert mode. Like OpenModal, it opens only from the top level.
func (r *Runtime) OpenChooser(spec ChooserSpec) (*Chooser, error) {
	if spec.Kind == "" {
		return nil, errors.New("chooser kind is required")
	}
	c := &Chooser{r: r, spec: spec, input: NewInput(spec.Kind+"/query", "", false)}
	if len(spec.Sources) > 0 {
		c.source = c.startSource()
		c.spec.Rows = nil
	}
	c.initPropFilters()
	c.refilter()
	inst, err := r.OpenModal(spec.Kind, Payload{}, WithInput(c.input, Kind(chooserNoun)))
	if err != nil {
		return nil, err
	}
	inst.Component = c
	c.load()
	return c, nil
}

// Chooser is the open chooser of kind, nil when none is open.
func (r *Runtime) Chooser(kind string) *Chooser {
	if inst := r.Instances[kind]; inst != nil {
		if c, ok := inst.Component.(*Chooser); ok {
			return c
		}
	}
	return nil
}

// chooserAt is the chooser whose input inst is, else the top modal's.
func (r *Runtime) chooserAt(inst *Instance) *Chooser {
	if inst != nil {
		if c := r.Chooser(inst.Parent); c != nil {
			return c
		}
		if c := r.Chooser(inst.ID); c != nil {
			return c
		}
	}
	return r.Chooser(r.TopModal())
}

// MatchMode is the chooser's match mode: the one search.cycle-match or
// search.set-match chose while it is open, else the chooser.match
// setting. It decides the default filter only (not a Filter of the
// application's own, not a Live source's own ranking).
func (c *Chooser) MatchMode() string {
	if c.match != "" {
		return c.match
	}
	return c.r.ChooserMatch()
}

// matcher is the query compiled in the chooser's match mode.
func (c *Chooser) matcher() Matcher { return c.r.Matcher(c.MatchMode(), c.Query()) }

// The matchSearcher contract (search_match.go).
func (c *Chooser) matchMode() string  { return c.MatchMode() }
func (c *Chooser) matchApplies() bool { return c.spec.Filter == nil && !c.live() }
func (c *Chooser) setMatchMode(mode string) {
	c.match = mode
	c.refilter()
}

// Kind is the chooser's modal kind.
func (c *Chooser) Kind() string { return c.spec.Kind }

// Title is the chooser modal's title; with sources, the current source
// in brackets and the others after it.
func (c *Chooser) Title() string { return c.sourceTitle() }

// SetQuery replaces the search text (cursor at its end), as typing would.
func (c *Chooser) SetQuery(query string) {
	_ = c.input.Replace(0, len(c.input.Text), query)
	c.input.Cursor = len(c.input.Text)
	c.sync()
}

// ModalTitle and ModalBlock make a chooser a self-drawing modal body
// (ModalBody), drawn like a record editor and its chooser.
func (c *Chooser) ModalTitle() string                           { return c.Title() }
func (c *Chooser) ModalBlock(_ Styles, width, height int) Block { return c.View(width, height) }

// close pops the chooser's modal so a callback may open the next one.
func (c *Chooser) close() error {
	if c.r.TopModal() != c.spec.Kind {
		return fmt.Errorf("chooser %s is not the top modal", c.spec.Kind)
	}
	return c.r.Pop()
}

func (c *Chooser) pick() Result {
	row, ok := c.Highlighted()
	if !ok || row.ID == chooserCreateRow {
		return c.create()
	}
	if row.NoPick && c.opens() {
		if err := c.open(); err != nil {
			return Failed(err)
		}
		return Done(c.state())
	}
	if row.Refuse != "" {
		return Failed(errors.New(row.Refuse))
	}
	if err := c.close(); err != nil {
		return Failed(err)
	}
	if c.spec.Choose == nil {
		return Done(row.ID)
	}
	// The callback belongs to the restored focus, including when another
	// modal remains below this chooser.
	defer c.r.actingFor(c.r.Focus)()
	return c.spec.Choose(row)
}

func (c *Chooser) create() Result {
	create := c.createFunc()
	if create == nil {
		return Failed(errors.New("nothing matches"))
	}
	query := c.Query()
	if err := c.close(); err != nil {
		return Failed(err)
	}
	defer c.r.actingFor(c.r.Focus)()
	return create(query)
}

// SetRows replaces the rows (a background load landing); the highlight
// stays on the same row id when the user had moved it.
func (c *Chooser) SetRows(rows []ChooserRow) {
	keep := ""
	if row, ok := c.Highlighted(); ok && c.moved {
		keep = row.ID
	}
	c.spec.Rows = rows
	c.cachePropRows()
	c.refilter()
	for i, row := range c.matches {
		if keep != "" && row.ID == keep {
			c.cursor, c.moved = i, true
		}
	}
}

// Query is the search input's text.
func (c *Chooser) Query() string { return string(c.input.Text) }

// Matches are the rows the query keeps: matches first, then trailing rows,
// then the create row, then the fixed rows.
func (c *Chooser) Matches() []ChooserRow {
	c.sync()
	return append([]ChooserRow(nil), c.matches...)
}

// Moved reports whether the user moved the highlight since the last edit.
func (c *Chooser) Moved() bool { c.sync(); return c.moved }

// Highlighted is the row Enter takes: the moved-to row, else the top
// match; none means Enter creates.
func (c *Chooser) Highlighted() (ChooserRow, bool) {
	c.sync()
	if c.cursor < 0 || c.cursor >= len(c.matches) {
		return ChooserRow{}, false
	}
	return c.matches[c.cursor], true
}

// sync refilters when the query changed; typing resets the highlight to the
// top match.
func (c *Chooser) sync() {
	if c.Query() == c.seen && c.propsEnabled == c.propFiltersEnabled() {
		return
	}
	if c.live() && c.Query() != c.loadedQuery {
		c.load()
	}
	c.refilter()
}

func (c *Chooser) refilter() {
	c.seen = c.Query()
	c.propsEnabled = c.propFiltersEnabled()
	cm := c.r.CaseFor(c.seen)
	filter := c.spec.Filter
	if filter == nil {
		m := c.matcher()
		filter = func(_ string, row ChooserRow) bool { return m.Match(row.Label) }
	}
	var head, tail, fixed []ChooserRow
	for _, row := range c.spec.Rows {
		if c.propsEnabled && !c.filters.matchesProps(c.propItem(row)) {
			continue
		}
		if row.Fixed {
			fixed = append(fixed, row)
			continue
		}
		if !c.live() && !filter(c.seen, row) {
			continue
		}
		if row.Trailing {
			tail = append(tail, row)
		} else {
			head = append(head, row)
		}
	}
	// A row whose label holds the query whole outranks a scattered fuzzy
	// match, so the top match is the one the query spells.
	// A Live source ranked its rows itself.
	if !c.live() {
		slices.SortStableFunc(head, func(x, y ChooserRow) int {
			return cmpBool(cm.Contains(y.Label, c.seen), cm.Contains(x.Label, c.seen))
		})
	}
	// A trailing row the query spells from the start of one of its words
	// ("unp", "no pin") is what the user asked for: it leads, unless an
	// ordinary row is spelled as well ("no" for a project "notes").
	spelled := false
	for _, row := range head {
		spelled = spelled || spellsWords(cm, c.seen, row.Label)
	}
	for i, row := range tail {
		if !spelled && spellsWords(cm, c.seen, row.Label) {
			head = append([]ChooserRow{row}, head...)
			tail = append(tail[:i:i], tail[i+1:]...)
			break
		}
	}
	var create []ChooserRow
	if label, ok := c.createRow(); ok {
		create = []ChooserRow{{ID: chooserCreateRow, Label: label}}
	}
	c.matches = slices.Concat(head, tail, create, fixed)
	c.filters.items = nil
	for _, row := range c.matches {
		if row.ID != chooserCreateRow {
			c.filters.items = append(c.filters.items, c.propItem(row))
		}
	}
	c.moved = false
	c.cursor = -1
	// A fixed row the query spells ("unp", "unpin", "no pin" for "no pin
	// (unpin)") is what was asked for, ahead of the create row; it keeps
	// its place at the end.
	spelledFixed := -1
	for i, row := range fixed {
		if !spelled && spellsWords(cm, c.seen, row.Label) {
			spelledFixed = len(head) + len(tail) + len(create) + i
			break
		}
	}
	switch {
	case spelledFixed >= 0:
		c.cursor = spelledFixed
	case len(create) > 0 && !c.holds(head):
		// Only scattered matches ("unpin" in "tuios-inbox ~/proj/…"):
		// Enter creates what was typed rather than guess.
		c.cursor = len(head) + len(tail)
	case len(head) > 0 || c.seen != "" && len(tail) > 0:
		c.cursor = 0
	}
}

// holds reports whether the first row is a strong match for the query: it
// holds the query as a substring (case as search.case says) and, failing that,
// punctuation and spaces ("tuiosinbox" in "tuios-inbox"), spells it from
// word starts, or matches it compactly inside one word (compactMatch:
// "chz" in "ochazuke"). A match strewn across words ("unpin" in
// "tuios-inbox  ~/proj/tuios-inbox") is not.
func (c *Chooser) holds(rows []ChooserRow) bool {
	if len(rows) == 0 {
		return false
	}
	if c.spec.Filter == nil && !c.matcher().Scattered() {
		// A substring or regex match is spelled in the row, never strewn.
		return true
	}
	label := rows[0].Label
	cm := c.r.CaseFor(c.seen)
	if cm.Contains(label, c.seen) || spellsWords(cm, c.seen, label) {
		return true
	}
	squash := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return cm.Rune(r)
			}
			return -1
		}, s)
	}
	q := squash(c.seen)
	return q != "" && strings.Contains(squash(label), q) || compactMatch(cm, c.seen, label)
}

// compactMatch reports whether query's runes appear in order inside one
// whitespace-separated word of label, case as cm compares it, spanning at
// most twice the query's length plus one.
func compactMatch(cm CaseMatch, query, label string) bool {
	q := []rune(cm.String(strings.TrimSpace(query)))
	if len(q) == 0 {
		return false
	}
	for _, word := range strings.Fields(cm.String(label)) {
		w := []rune(word)
		for start := range w {
			if w[start] != q[0] {
				continue
			}
			i, j := 1, start+1
			for ; i < len(q) && j < len(w); j++ {
				if w[j] == q[i] {
					i++
				}
			}
			if i == len(q) && j-start <= 2*len(q)+1 {
				return true
			}
		}
	}
	return false
}

// createRow is the create row's label, when the chooser shows one.
func (c *Chooser) createRow() (string, bool) {
	label := c.createLabel()
	if !c.spec.CreateRow || c.createFunc() == nil || label == nil || strings.TrimSpace(c.seen) == "" {
		return "", false
	}
	return "+ " + label(c.seen), true
}

// move steps the highlight, clamped at both ends (no wrap).
func (c *Chooser) move(delta int) {
	c.cursor = c.r.moveHighlight(c.cursor, delta, len(c.matches), false)
	if delta != 0 && c.cursor >= 0 {
		c.moved = true
	}
}

func (c *Chooser) state() map[string]any {
	row, _ := c.Highlighted()
	state := map[string]any{"propFilters": c.PropFilters(), "savedFilter": c.filters.savedFilter, "query": c.Query(), "matches": len(c.matches), "highlighted": row.ID, "moved": c.moved}
	if len(c.spec.Sources) > 0 {
		state["source"] = c.Source()
		state["sources"] = c.sourceNames()
	}
	return state
}

// View draws the search line, then the rows with the highlighted one in
// reverse video, with what Enter would create when nothing is highlighted
// after the matches (before the fixed rows); with a Preview and the
// width for it, the highlighted row's preview on the right. Its cursor is
// the search input's.
func (c *Chooser) View(width, height int) Block {
	c.sync()
	if c.spec.Preview == nil || width < ChooserPreviewMin {
		return c.rowsView(width, height)
	}
	// The preview column keeps its width whatever it holds, so the panel
	// does not change size as the highlight moves.
	listWidth := width * 3 / 5
	previewWidth := width - listWidth - 3
	left := c.rowsView(listWidth, height)
	text := ""
	if row, ok := c.Highlighted(); ok {
		text = c.spec.Preview(row)
	}
	right := previewLines(text, previewWidth, height)
	lines := strings.Split(left.Text, "\n")
	for len(lines) < len(right) {
		lines = append(lines, "")
	}
	for i, line := range lines {
		side := ""
		if i < len(right) {
			side = right[i]
		}
		lines[i] = line + strings.Repeat(" ", max(0, listWidth-ansi.StringWidth(line))) + " │ " + side + strings.Repeat(" ", max(0, previewWidth-ansi.StringWidth(side)))
	}
	return left.WithText(strings.Join(lines, "\n"))
}

// previewLines is a preview's text wrapped to width, at most height lines,
// the last one marking a cut with ….
func previewLines(text string, width, height int) []string {
	if width < 1 || height < 1 || strings.TrimSpace(text) == "" {
		return nil
	}
	text = strings.ReplaceAll(strings.TrimRight(ansi.Strip(text), "\n"), "\t", "  ")
	wrapped := strings.Split(ansi.Wrap(text, width, " -/"), "\n")
	if len(wrapped) > height {
		wrapped = wrapped[:height]
		wrapped[height-1] = "…"
	}
	return wrapped
}

// rowsView is View's search line and rows, each line width cells wide.
func (c *Chooser) rowsView(width, height int) Block {
	const prefix = "› "
	// The search line's right end says why nothing can match (an invalid
	// regex) and the match mode (search.show-match), while they apply.
	var right []string
	if c.matchApplies() {
		if err := c.matcher().Err(); err != nil {
			right = append(right, err.Error())
		}
		if tag := c.r.matchTagFor(c.MatchMode()); tag != "" {
			right = append(right, tag)
		}
	}
	tail := strings.Join(right, "  ")
	inputWidth := max(8, width-ansi.StringWidth(prefix))
	if tail != "" && inputWidth-ansi.StringWidth(tail)-1 >= 8 {
		inputWidth -= ansi.StringWidth(tail) + 1
	} else {
		tail = ""
	}
	line := InputBlock(c.input, inputWidth, 1).Offset(ansi.StringWidth(prefix), 0)
	search := prefix + line.Text
	if tail != "" {
		search = searchLine(search, tail, width)
	}
	lines := []string{search}
	if c.propsEnabled {
		if strip := c.filters.propStrip(); strip != "" {
			lines = append(lines, searchLine(strip, fmt.Sprintf("%d of %d", len(c.filters.items), len(c.spec.Rows)), width))
		}
	}
	rowsHeight := max(1, height-2)
	switch {
	case len(c.spec.Rows) == 0 && c.emptyLine() != "":
		// Padded to the width the rows will take, so the panel does not
		// jump wider when they land.
		lines = append(lines, ansi.Truncate("  "+c.emptyLine()+strings.Repeat(" ", width), width, ""))
	case c.matched() == 0:
		lines = append(lines, "  no matches")
	}
	hint := ""
	if label := c.createLabel(); c.cursor < 0 && c.createFunc() != nil && label != nil {
		hint = ansi.Truncate("  enter: "+label(c.Query()), width, "…")
	}
	// The fixed rows ("no pin") always show at the bottom: the rows above
	// them scroll in what is left, keeping the highlight in view.
	fixedFrom := len(c.matches)
	for fixedFrom > 0 && c.matches[fixedFrom-1].Fixed {
		fixedFrom--
	}
	scroll := max(1, rowsHeight-(len(lines)-1)-(len(c.matches)-fixedFrom))
	if hint != "" {
		scroll = max(1, scroll-1)
	}
	offset := 0
	switch {
	case c.cursor >= fixedFrom:
		offset = max(0, fixedFrom-scroll)
	case c.cursor >= scroll:
		offset = c.cursor - scroll + 1
	}
	widths := fitWidths(c.cellWidths(), width-2, c.keep()...)
	toned := c.toneStyles()
	draw := func(i int) {
		text := c.matches[i].Label
		if cells := c.matches[i].Cells; len(cells) > 0 {
			text = alignCells(cells, widths)
		}
		row := ansi.Truncate("  "+modalLabel(text), width, "…")
		if segs := chooserSegs(toned, c.matches[i], widths, ansi.StringWidth(row)); len(segs) > 0 {
			lines = append(lines, paintLine(row, width, segs, toned, i == c.cursor, false, span{}))
			return
		}
		if i == c.cursor {
			row = lipgloss.NewStyle().Reverse(true).Render(ansi.Truncate(row+strings.Repeat(" ", max(0, width-ansi.StringWidth(row))), width, ""))
		}
		lines = append(lines, row)
	}
	for i := offset; i < fixedFrom && i < offset+scroll; i++ {
		draw(i)
	}
	if hint != "" {
		lines = append(lines, hint)
	}
	for i := fixedFrom; i < len(c.matches); i++ {
		draw(i)
	}
	// The panel keeps the tallest it has been while open
	// (chooser.stable-height): as typing narrows the matches, blank lines
	// hold its height, so a modal centred on it keeps the search line on
	// its row (R4.9). It grows with more rows, and never past height.
	if c.r == nil || c.r.Settings().Bool(ChooserStableHeightSetting) {
		c.tallest = max(c.tallest, len(lines))
		for len(lines) < min(c.tallest, max(1, height)) {
			lines = append(lines, "")
		}
	}
	// Every line fills the width, so the panel is the modal's width and
	// does not resize as typing narrows the rows.
	for i, l := range lines {
		lines[i] = l + strings.Repeat(" ", max(0, width-ansi.StringWidth(l)))
	}
	return line.WithText(strings.Join(lines, "\n"))
}

// matched counts the rows the query kept, leaving out the create and fixed
// rows.
func (c *Chooser) matched() int {
	n := 0
	for _, row := range c.matches {
		if !row.Fixed && row.ID != chooserCreateRow {
			n++
		}
	}
	return n
}

// cellWidths is each column's width over every row with cells, so the
// columns do not shift as the query narrows the rows.
func (c *Chooser) cellWidths() []int {
	var widths []int
	for _, row := range c.spec.Rows {
		for i, cell := range row.Cells {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], ansi.StringWidth(cell))
		}
	}
	return widths
}

// fitWidths narrows the widest columns, one cell at a time and never
// below 6, until the columns and their two-space gaps fit in avail, so no
// one wide column pushes the rest off. The keep columns narrow only once
// every other column is at its minimum.
func fitWidths(widths []int, avail int, keep ...int) []int {
	widths = slices.Clone(widths)
	total := func() int {
		n := 2 * max(0, len(widths)-1)
		for _, w := range widths {
			n += w
		}
		return n
	}
	widest := func(skipKept bool) int {
		at := -1
		for i, w := range widths {
			if skipKept && slices.Contains(keep, i) {
				continue
			}
			if w > 6 && (at < 0 || w > widths[at]) {
				at = i
			}
		}
		return at
	}
	for total() > avail {
		at := widest(true)
		if at < 0 {
			at = widest(false)
		}
		if at < 0 {
			break
		}
		widths[at]--
	}
	return widths
}

// toneStyles is the theme's tones for drawing rows' Tones, nil without
// a Model's styles or any tone.
func (c *Chooser) toneStyles() *Styles {
	if c.r == nil {
		return nil
	}
	return c.r.toneStyles()
}

// chooserSegs places a row's Tones on its drawn line (drawn cells wide,
// after the two-space indent): on the Label's runes, or on each cell where
// alignCells puts it (cut cells keep their tones on what shows).
func chooserSegs(st *Styles, row ChooserRow, widths []int, drawn int) []toneSeg {
	if st == nil || len(row.Tones) == 0 {
		return nil
	}
	const indent = 2
	if len(row.Cells) == 0 {
		return cellSegs(spansOf(row.Tones, 0), modalLabel(row.Label), indent, drawn-indent)
	}
	var out []toneSeg
	x := indent
	for i, cell := range row.Cells {
		text := modalLabel(cell)
		w := ansi.StringWidth(text)
		if i < len(widths) {
			w = min(w, widths[i])
		}
		if shown := min(w, drawn-x); shown > 0 {
			out = append(out, cellSegs(spansOf(row.Tones, i), text, x, shown)...)
		}
		if i < len(widths) {
			x += widths[i] + 2
		} else {
			x += w + 2
		}
	}
	return out
}

// alignCells pads each cell but the last to its column's width, two
// spaces apart, cutting a cell longer than its column with …; trailing
// empty cells are dropped.
func alignCells(cells []string, widths []int) string {
	for len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	var b strings.Builder
	for i, cell := range cells {
		if i < len(widths) {
			cell = ansi.Truncate(cell, widths[i], "…")
		}
		b.WriteString(cell)
		if i < len(cells)-1 && i < len(widths) {
			b.WriteString(strings.Repeat(" ", max(0, widths[i]-ansi.StringWidth(cell))+2))
		}
	}
	return b.String()
}

// spellsWords reports whether query, ignoring punctuation and comparing
// case as cm does, starts at the beginning of one of label's words and
// runs on through the words that follow ("unp" and "no pin" spell "no pin
// (unpin)").
func spellsWords(cm CaseMatch, query, label string) bool {
	norm := func(s string) []string {
		return strings.FieldsFunc(cm.String(s), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})
	}
	q := strings.Join(norm(query), " ")
	if q == "" {
		return false
	}
	words := norm(label)
	for i := range words {
		if strings.HasPrefix(strings.Join(words[i:], " "), q) {
			return true
		}
	}
	return false
}

// cmpBool orders false before true.
func cmpBool(x, y bool) int {
	switch {
	case x == y:
		return 0
	case x:
		return 1
	}
	return -1
}

// fuzzyMatch reports whether query's runes appear in text in order, case
// as cm compares it (fuzzyScore's rule; an application's Filter may call
// it through a Matcher instead).
func fuzzyMatch(cm CaseMatch, query, text string) bool {
	_, ok := fuzzyScore(text, []rune(cm.String(query)), cm)
	return ok
}
