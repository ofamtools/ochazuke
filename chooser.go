package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
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

// PickerRow is one choice (ChooserRow before v0.96.0). Trailing rows (a "browse…" entry) follow the
// matches. Enter takes one without moving onto it only when the query is
// not empty and no ordinary row matches, so their filter should be strict
// (a substring of the label); an empty query with no ordinary rows creates.
// Fixed rows (a "no pin" entry) sit at the bottom whatever the query: the
// filter never sees them and Enter takes one only once the highlight is
// moved onto it, or when the query spells it.
type PickerRow struct {
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
// it creates. Empty is the line shown while there are no rows at all, and
// Loading the line while LoadingJob runs and there are none yet; Plural
// names the rows (Placeholder, one format: "no directories", "loading
// directories…", "no directories match 'x'"). Keep names the cell columns cut last. Preview draws a
// preview of the highlighted row beside the rows when there is room.
// (ChooserSpec before v0.96.0.)
type PickerSpec struct {
	// Noun is the default row noun for prop filtering.
	Noun string
	// NoPropFilters opts this chooser out of prop filters and their keys.
	NoPropFilters bool
	// NoSort keeps the source order when these rows are only a partial view.
	NoSort bool
	// Stay keeps the chooser open after a pick (for embedded or repeated choices).
	Stay bool
	// NormalMode allows Escape to leave search editing; i resumes insert.
	// The input still starts in insert; default choosers remain pinned there.
	NormalMode bool

	// Match is the match mode this chooser opens in (fuzzy, substring or
	// regex); it wins over the chooser.match setting, "" is the setting.
	// search.cycle-match still steps it while the chooser is open.
	Match string

	Kind        string
	Title       string
	Rows        []ChooserRow
	Filter      func(query string, row ChooserRow) bool
	Choose      func(ChooserRow) Result
	Create      func(query string) Result
	CreateLabel func(query string) string
	CreateRow   bool
	Empty       string
	// Loading is the line while LoadingJob runs and there are no rows yet
	// ("" is LoadingText(Plural)); so a chooser opened before its rows
	// arrive never says it is empty while they load.
	Loading    string
	LoadingJob JobSlot
	// Plural names the rows, plural ("directories"), for the default
	// Empty and Loading lines and the no-match line; "" is "rows".
	Plural string
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
	// Hint, when set, ends the empty and no-match lines and the refusal
	// of a pick nothing matches ("no known directories; type a path to
	// pick one"), whatever the source's own Empty says: one place for
	// the way out. Lower case, no parentheses (R5.19).
	Hint string
	// Start, when set, picks the row the highlight starts on while the
	// query is empty and the user has not moved it (a field's current
	// value), as rows arrive; else the top row.
	Start func(ChooserRow) bool
	// Hold, when set, holds an enter the user made no choice for (an
	// empty query, the highlight not moved) while it reports true, as
	// enter waits for a Live source's rows: what the highlight starts on
	// is not known yet (Start's value is still loading). The owner calls
	// Release once Hold turns false; the held enter then takes the
	// highlight as it is. A typed query or a moved highlight is a choice
	// and is taken at once.
	Hold func() bool
}

// ChooserPreviewMin is the width from which a chooser with a Preview
// draws it beside the rows.
const ChooserPreviewMin = 60

// hasOwnRows reports whether the chooser has a row besides its fixed ones.
func (c *Chooser) hasOwnRows() bool {
	return slices.ContainsFunc(c.spec.Rows, func(row ChooserRow) bool { return !row.Fixed })
}

// chooserNoun is the noun of every chooser's search input: the picker
// noun (it was ochazuke.chooser before v0.96.0, now a deprecated name of
// it); its rows are the picker's keys (bindings/picker.json).
const chooserNoun = PickerNoun

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
	// A source that claims the query is shown in place of base (the
	// source the user chose) while claimed; suppressed after a hand
	// switch, until the query is no longer claimed.
	base       int
	claimed    bool
	suppressed bool
	// pending is an enter held until the load for the query lands.
	pending bool
	loading bool
	loadErr string
	gen     uint64
	// LoadedMode is the match mode the current load ranked in (a source
	// with LoadMode loads again when it changes); Preview is the current
	// source's preview of the highlighted row (ChooserSource.Preview).
	LoadedMode string
	Preview    ChooserPreview
	// tallest is the most lines the rows view has drawn while open: with
	// chooser.stable-height it pads to it, so typing never resizes it.
	tallest int
	// hidden is the count of rows below the drawn ones in the last View
	// (0 when none or when chooser.hidden-count is off), for a host that
	// draws the query line itself.
	hidden int
	// match is the match mode search.cycle-match or search.set-match
	// chose while open; "" is the chooser.match setting (search_match.go).
	match    string
	embedded bool // AddChooser: a placed chooser, not its own modal

	// filters is the shared list prop-filter controller, without a registered
	// list or row instances: the chooser retains its own ranking and focus.
	filters      *Collection
	propsEnabled bool
}

// ChooserStableHeightSetting keeps an open chooser at the height it
// reached (settings/ochazuke.json).
const ChooserStableHeightSetting = "picker.stable-height"

// ChooserHiddenCountSetting shows how many matching rows are off screen.
const ChooserHiddenCountSetting = "picker.hidden-count"

// ChooserShowIDsSetting draws the dim short id after a choice row's label
// (ToneChoiceID); off hides it, and the filter still reads it.
const ChooserShowIDsSetting = "picker.show-ids"

// ChooserColumnsSetting draws a row's Cells as aligned columns; off draws the
// label alone.
const ChooserColumnsSetting = "picker.columns"

// The delivery contract: a chooser runs no jobs of its own (Choose and
// Create return their own results). Pending work they return belongs to the
// focus the chooser opened over (runApplication), since the chooser and its
// input are closed before the callback runs.
func (c *Chooser) HandleMessage(msg tea.Msg) bool { return c.filters.HandleMessage(msg) }
func (c *Chooser) Close()                         { c.pending = false; c.filters.Close(); c.closeSources() }

// PendingCommand follows the query after every event: a Live source loads
// again for the edited query, and the work it queues is handed over here
// (the runtime's queue was drained before the components were asked).
func (c *Chooser) PendingCommand() tea.Cmd {
	if c.r.Chooser(c.spec.Kind) != c {
		return nil
	}
	c.sync()
	c.queuePreview()
	commands := []tea.Cmd{c.filters.PendingCommand()}
	commands = append(commands, c.r.commands...)
	c.r.commands = nil
	return tea.Batch(commands...)
}

func registerChooser(r *Runtime) {
	r.DefineGuard(PickerNormalModeGuard, func(r *Runtime, ev KeyEvent) bool {
		c := r.chooserAt(r.Instances[ev.Focus])
		return c != nil && (c.spec.NormalMode || c.input.pin == "")
	})
	r.DefineGuard(PickerPropControlsGuard, func(r *Runtime, ev KeyEvent) bool {
		c := r.chooserAt(r.Instances[ev.Focus])
		return c != nil && c.propFiltersEnabled() && (len(c.filters.vs.Filters) > 0 || c.filters.vs.Saved != "")
	})
	verb := func(fn func(*Chooser, Ctx) Result) AppHandler {
		return func(ctx Ctx) Result {
			c := r.chooserAt(ctx.Instance)
			if c == nil {
				return Failed(errors.New("no picker is open"))
			}
			c.sync()
			return fn(c, ctx)
		}
	}
	registerPickerVerbs(r)
	r.Define(chooserNoun).
		Verb("clear-props", verb(func(c *Chooser, _ Ctx) Result {
			if err := c.filters.SetPropFilters(nil); err != nil {
				return Failed(err)
			}
			return Done(nil)
		}), "Clear this picker's prop conditions").When(PickerPropControlsGuard).
		Verb("normal", verb(func(c *Chooser, _ Ctx) Result {
			if err := c.r.SetMode("normal"); err != nil {
				return Failed(err)
			}
			return Done(nil)
		}), "Leave search editing in a picker that supports normal mode").When(PickerNormalModeGuard)
	registerChooserSources(r)
	registerPickerGuardAliases(r)
}

// OpenChooser is OpenPicker's old name.
//
// Deprecated: use OpenPicker; kept for one release (v0.96.0).
func (r *Runtime) OpenChooser(spec ChooserSpec) (*Chooser, error) { return r.OpenPicker(spec) }

// ChooserSpec is PickerSpec's old name.
//
// Deprecated: use PickerSpec; kept for one release (v0.96.0).
type ChooserSpec = PickerSpec

// ChooserRow is PickerRow's old name.
//
// Deprecated: use PickerRow; kept for one release (v0.96.0).
type ChooserRow = PickerRow

// OpenPicker opens a picker modal (OpenChooser before v0.96.0) with its
// search input focused in insert mode. Like OpenModal, it opens only from
// the top level.
func (r *Runtime) OpenPicker(spec PickerSpec) (*Chooser, error) {
	if spec.Kind == "" {
		return nil, errors.New("picker kind is required")
	}
	c := &Chooser{r: r, spec: spec, input: NewInput(spec.Kind+"/query", "", false)}
	if len(spec.Sources) > 0 {
		c.source = c.startSource()
		c.spec.Rows = nil
	}
	c.initPropFilters()
	c.refilter()
	inst, err := r.OpenModal(spec.Kind, Payload{}, WithInput(c.input, Kind(chooserNoun)), CloseWith(Request{Noun: chooserNoun, Verb: "cancel"}))
	if err != nil {
		return nil, err
	}
	inst.Component, inst.accepts = c, true
	if spec.NormalMode {
		c.input.pin = ""
	}
	c.load()
	return c, nil
}

// AddChooser places the existing chooser under a parent rather than opening
// a modal. Its search, ranking, keys and rendering are the same. Stay permits
// several choices; the host draws View and owns the surrounding layout.
func (r *Runtime) AddChooser(spec ChooserSpec, opts ...InstanceOption) (*Chooser, error) {
	if spec.Kind == "" || r.Instances[spec.Kind] != nil {
		return nil, errors.New("picker kind must be nonempty and unused")
	}
	options := instanceSpec{parent: "app", noun: chooserNoun}
	for _, opt := range opts {
		opt(&options)
	}
	if r.Instances[options.parent] == nil || r.Noun(options.noun) == nil {
		return nil, errors.New("unknown picker parent or noun")
	}
	c := &Chooser{r: r, spec: spec, input: NewInput(spec.Kind+"/query", "", false), embedded: true}
	if len(spec.Sources) > 0 {
		c.source = c.startSource()
		c.spec.Rows = nil
	}
	c.initPropFilters()
	c.refilter()
	r.Instance(spec.Kind, Parent(options.parent), Kind(options.noun)).Component = c
	if err := r.AddInput(c.input, Parent(spec.Kind), Kind(chooserNoun)); err != nil {
		_ = r.RemoveInstance(spec.Kind)
		return nil, err
	}
	if spec.NormalMode {
		c.input.pin = ""
	}
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
// search.set-match chose while it is open, else the spec's Match, else
// the chooser.match setting. It decides the default filter only (not a
// Filter of the application's own, not a Live source's own ranking).
func (c *Chooser) MatchMode() string {
	if c.match != "" {
		return c.match
	}
	if c.spec.Match != "" {
		return matchOr(c.spec.Match, c.r.ChooserMatch())
	}
	return c.r.ChooserMatch()
}

// ranked reports whether the current source ranks and filters its rows
// itself (a Live source that did not ask for the chooser's match mode,
// ChooserSource.Matched): the mode then does not apply.
func (c *Chooser) ranked() bool {
	src := c.current()
	return src != nil && src.Live && !src.Matched
}

// matcher is the query compiled in the chooser's match mode.
func (c *Chooser) matcher() Matcher { return c.r.Matcher(c.MatchMode(), c.Query()) }

// The matchSearcher contract (search_match.go).
func (c *Chooser) matchMode() string { return c.MatchMode() }
func (c *Chooser) matchApplies() bool {
	return c.spec.Filter == nil && (!c.ranked() || c.modeRanked())
}
func (c *Chooser) setMatchMode(mode string) {
	c.filters.linger.reset()
	c.match = mode
	if c.modeRanked() {
		c.load()
	}
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
	if c.embedded {
		return c.r.RemoveInstance(c.spec.Kind)
	}
	if c.r.TopModal() != c.spec.Kind {
		return fmt.Errorf("picker %s is not the top modal", c.spec.Kind)
	}
	return c.r.Pop()
}

func (c *Chooser) pick() Result {
	if c.waiting() || c.holding() {
		// Enter on a query whose rows are still loading waits for them
		// (runPending), as one with no choice made waits for Hold
		// (Release); typing more, moving the highlight, a source switch
		// or esc drops it.
		c.pending = true
		return Done(c.state())
	}
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
	accepted := c.r.acceptModal(c.spec.Kind)
	if !c.spec.Stay {
		if err := c.close(); err != nil {
			return Failed(err)
		}
	}
	accepted()
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
		return Failed(errors.New(c.hinted("nothing matches")))
	}
	query := c.Query()
	accepted := c.r.acceptModal(c.spec.Kind)
	if !c.spec.Stay {
		if err := c.close(); err != nil {
			return Failed(err)
		}
	}
	accepted()
	defer c.r.actingFor(c.r.Focus)()
	return create(query)
}

// holding reports whether enter is held for the spec's Hold: no choice
// made (an empty query, the highlight not moved) and Hold true.
func (c *Chooser) holding() bool {
	c.sync()
	return c.spec.Hold != nil && c.Query() == "" && !c.moved && c.spec.Hold()
}

// Release runs an enter held for the spec's Hold (or for a load) once
// nothing holds it any more; with none held it does nothing.
func (c *Chooser) Release() {
	if c.pending && !c.loading && !c.holding() {
		c.runPending()
	}
}

// SetRows replaces the rows (a background load landing); the highlight
// stays on the same row id when the user had moved it.
func (c *Chooser) SetRows(rows []ChooserRow) {
	keep := ""
	wasMoved := c.moved
	if row, ok := c.Highlighted(); ok {
		keep = row.ID
	}
	c.spec.Rows = rows
	c.cachePropRows()
	c.refilter()
	for i, row := range c.matches {
		if keep != "" && row.ID == keep && (wasMoved || c.filters.Lingering(keep)) {
			c.cursor, c.moved = i, wasMoved
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
	c.pending = false
	c.claim()
	if c.live() && c.Query() != c.loadedQuery {
		c.load()
	}
	c.refilter()
}

// pickerCurrent picks the current source's Current row (pick {current:
// true}).
func (c *Chooser) pickerCurrent() Result {
	src := c.current()
	if src == nil || src.Current == nil {
		return Failed(errors.New("this picker has no current row"))
	}
	row, ok := src.Current()
	if !ok {
		return Failed(errors.New("this picker has no current row"))
	}
	if row.Refuse != "" {
		return Failed(errors.New(row.Refuse))
	}
	accepted := c.r.acceptModal(c.spec.Kind)
	if !c.spec.Stay {
		if err := c.close(); err != nil {
			return Failed(err)
		}
	}
	accepted()
	if c.spec.Choose == nil {
		return Done(row.ID)
	}
	defer c.r.actingFor(c.r.Focus)()
	return c.spec.Choose(row)
}

func (c *Chooser) refilter() {
	if c.seen != c.Query() || c.propsEnabled != c.propFiltersEnabled() {
		c.filters.linger.reset()
	}
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
		if !c.ranked() && !filter(c.seen, row) {
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
	if !c.live() && (len(c.filters.vs.Sort) == 0 || !c.filters.canSort()) {
		slices.SortStableFunc(head, func(x, y ChooserRow) int {
			return cmpBool(cm.Contains(y.Label, c.seen), cm.Contains(x.Label, c.seen))
		})
	}
	head = c.sortedRows(head)
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
	c.lingerRows()
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
	c.startAt()
}

// startAt puts the highlight on the spec's Start row while the query is
// empty: a pick opens on what is already there.
func (c *Chooser) startAt() {
	if c.spec.Start == nil || c.seen != "" {
		return
	}
	if i := slices.IndexFunc(c.matches, c.spec.Start); i >= 0 {
		c.cursor = i
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

// move steps the highlight, clamped at both ends (no wrap). A move is a
// new choice: it drops an enter held for a load or Hold, as typing does.
func (c *Chooser) move(delta int) {
	c.cursor = c.r.moveHighlight(c.cursor, delta, len(c.matches), false)
	if delta != 0 {
		c.pending = false
	}
	if delta != 0 && c.cursor >= 0 {
		c.moved = true
	}
}

func (c *Chooser) state() map[string]any {
	row, _ := c.Highlighted()
	state := map[string]any{"propFilters": c.PropFilters(), "savedFilter": c.filters.vs.Saved, "query": c.Query(), "matches": len(c.matches), "highlighted": row.ID, "moved": c.moved}
	if len(c.filters.vs.Sort) > 0 {
		state["sort"] = c.filters.Sort()
	}
	if len(c.spec.Sources) > 0 {
		state["source"] = c.Source()
		state["sources"] = c.sourceNames()
	}
	return state
}

// HiddenRows is the count of matched rows the last View left off the
// bottom; a host that draws its own query line shows it there.
func (c *Chooser) HiddenRows() int { return c.hidden }

// View draws the search line, then the rows with the highlighted one in
// reverse video, with what Enter would create when nothing is highlighted
// after the matches (before the fixed rows); with a Preview and the
// width for it, the highlighted row's preview on the right. Its cursor is
// the search input's.
func (c *Chooser) View(width, height int) Block {
	c.sync()
	if !c.previewing() || width < ChooserPreviewMin {
		return c.rowsView(width, height)
	}
	// The preview column keeps its width whatever it holds, so the panel
	// does not change size as the highlight moves.
	listWidth := width * 3 / 5
	previewWidth := width - listWidth - 3
	left := c.rowsView(listWidth, height)
	text := ""
	if row, ok := c.Highlighted(); ok {
		text = c.previewText(row)
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
	if summary := c.filters.sortSummary(); summary != "" {
		lines = append(lines, ansi.Truncate(summary, width, "…"))
	}
	rowsHeight := max(1, height-2)
	switch {
	case !c.hasOwnRows() && c.emptyLine() != "":
		// No rows but the fixed ones ("(none)"): the empty line still
		// says why (a failed source's error, R5.19). Padded to the width the rows will take, so the panel does not
		// jump wider when they land.
		lines = append(lines, ansi.Truncate("  "+c.emptyLine()+strings.Repeat(" ", width), width, ""))
	case c.matched() == 0:
		lines = append(lines, ansi.Truncate("  "+c.hinted(NoMatchText(c.spec.Plural, c.Query())), width, "…"))
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
	c.hidden = 0
	if hidden := max(0, fixedFrom-scroll); hidden > 0 && c.r.Settings().Bool(ChooserHiddenCountSetting) {
		c.hidden = hidden
		count := fmt.Sprintf("%d more", hidden)
		if tail != "" {
			count = tail + " · " + count
		}
		lines[0] = searchLine(prefix+line.Text, count, width)
	}
	widths := fitWidths(c.cellWidths(), width-2, c.keep()...)
	toned := c.toneStyles()
	focused := c.r.DrawnFocus(c.input.ID) == c.input.ID
	var matcher Matcher
	if toned != nil && c.matchApplies() && toned.HasChrome("match") {
		matcher = c.matcher()
	}
	columns := c.r.Settings().Bool(ChooserColumnsSetting)
	showIDs := c.r.Settings().Bool(ChooserShowIDsSetting)
	draw := func(i int) {
		item := c.matches[i]
		if !columns {
			item.Cells = nil
		}
		if !showIDs && len(item.Cells) == 0 {
			item.Label, item.Tones = dropToneRuns(item.Label, item.Tones, ToneChoiceID)
		}
		text := item.Label
		if len(item.Cells) > 0 {
			text = alignCells(item.Cells, widths)
		}
		item.Tones = c.filters.rowTones(ListItem{ID: item.ID, Cells: item.Cells, Tones: item.Tones})
		if toned != nil && c.matchApplies() {
			// Copy before appending: match marks never mutate caller tones.
			marks := toned.matchSpans(matcher, modalLabel(item.Label), "match")
			if len(item.Cells) > 0 {
				marks = nil
				for cell, text := range item.Cells {
					for _, mark := range toned.matchSpans(matcher, modalLabel(text), "match") {
						mark.Cell = cell
						marks = append(marks, mark)
					}
				}
			}
			if len(marks) > 0 {
				item.Tones = append(slices.Clone(item.Tones), marks...)
			}
		}
		row := ansi.Truncate(c.filters.rowGutter(item.ID)+modalLabel(text), width, "…")
		if segs := chooserSegs(toned, item, widths, ansi.StringWidth(row)); len(segs) > 0 {
			lines = append(lines, paintLine(row, width, segs, toned, i == c.cursor, !focused, span{}))
			return
		}
		if i == c.cursor {
			row = toned.Selection(focused).Render(ansi.Truncate(row+strings.Repeat(" ", max(0, width-ansi.StringWidth(row))), width, ""))
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
	if !c.r.Settings().Bool(ChooserColumnsSetting) {
		return nil
	}
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
	return c.r.toneStyles(c.input.ID)
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

// dropToneRuns cuts the runs of a label that tone covers on the label itself
// (cell 0), with the two spaces that set them off, and moves the other
// spans to match: the dim id of a choice row when picker.show-ids is off.
func dropToneRuns(label string, tones []ToneSpan, tone string) (string, []ToneSpan) {
	runes := []rune(label)
	cut := make([]bool, len(runes))
	found := false
	for _, t := range tones {
		if t.Cell != 0 || t.Tone != tone {
			continue
		}
		from, to := max(t.From, 0), t.To
		if to <= 0 || to > len(runes) {
			to = len(runes)
		}
		for ; from > 0 && from <= len(runes) && runes[from-1] == ' ' && from > t.From-2; from-- {
		}
		for i := from; i < to; i++ {
			cut[i], found = true, true
		}
	}
	if !found {
		return label, tones
	}
	// at[i] is where rune offset i lands once the cut runes are gone.
	at := make([]int, len(runes)+1)
	var kept []rune
	for i, r := range runes {
		at[i] = len(kept)
		if !cut[i] {
			kept = append(kept, r)
		}
	}
	at[len(runes)] = len(kept)
	var out []ToneSpan
	for _, t := range tones {
		if t.Cell != 0 {
			out = append(out, t)
			continue
		}
		if t.Tone == tone {
			continue
		}
		from, to := max(t.From, 0), t.To
		if to <= 0 || to > len(runes) {
			to = len(runes)
		}
		if from >= len(runes) {
			continue
		}
		t.From, t.To = at[from], at[to]
		if t.From >= t.To {
			continue
		}
		out = append(out, t)
	}
	return string(kept), out
}
