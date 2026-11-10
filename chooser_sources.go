package ochazuke

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// A chooser with Sources lists the rows of one source at a time: ctrl+o and
// alt+o (chooser.next-source, chooser.previous-source) step to the next
// or previous one, the query carries over, and the title names the current
// source and the others. Each source loads its rows for the query (Load):
// a Live source again on every edit of the query, as completion does, the
// others once when they become current, the chooser's Filter narrowing
// their rows as the query changes. A source may navigate inside itself: a
// row with Opens is opened (chooser.open: a folder is browsed into) and Up
// steps back out (chooser.up: the parent folder). The last source used per
// chooser kind is remembered for the next open while the setting
// chooser.remember-source is on.

// PickerSource is one source of a picker's rows (ChooserSource before
// v0.96.0).
type PickerSource struct {
	// Name is what the title calls the source ("browse", "recent").
	Name string
	// Label, when set, is what the title shows for the current source in
	// place of Name ("browse ~/proj").
	Label func() string
	// Load lists the source's rows for the query: Done([]ChooserRow),
	// Failed (the error is the empty line), or Pending work whose value is
	// a []ChooserRow, run off the loop as a job of the chooser (a newer
	// load replaces it; closing the chooser cancels it).
	Load func(query string) Result
	// Live sources match the query themselves: Load runs again on every
	// edit and its rows are shown as they come, in their order. Other
	// sources load once when they become current (and after Open or Up)
	// and the chooser's Filter narrows their rows.
	Live bool
	// Matched, on a Live source, lets the chooser's match mode
	// (chooser.match, ChooserSpec.Match, tab) narrow the rows Load
	// answered by their labels, as it narrows any source's; the source
	// then over-fetches and tab cycles fuzzy, substring and regex over
	// what it streamed. Without it a Live source's rows are shown as
	// they came and the mode does not apply.
	Matched bool
	// Empty is the line shown while the source has no rows ("no recent
	// paths"); the spec's Empty else (its Loading while a load runs).
	Empty string
	// Loading and LoadingJob, when set, stand in for the spec's while the
	// source is current: the line while the job that fills the source runs
	// (a scan whose completion reloads it), so its Empty never shows then.
	Loading    string
	LoadingJob JobSlot
	// Open navigates into a row marked Opens (browse into a folder) and
	// returns the query to show next; false leaves everything as it is.
	// The source then loads again.
	Open func(row ChooserRow) (query string, ok bool)
	// Up navigates back out (the parent folder) given the query and
	// returns the query to show next; false when there is nowhere to go.
	Up func(query string) (string, bool)
	// Create and CreateLabel, when set, stand in for the spec's while the
	// source is current (a browse source creates in its folder).
	Create      func(query string) Result
	CreateLabel func(query string) string
	// Keep, when set, stands in for the spec's Keep while the source is
	// current: the cell columns cut last (a known-folder source's path,
	// its branch and project columns narrowing first).
	Keep []int
	// Claims, when set, says the query is this source's to answer (a path
	// typed into a path chooser: "/", "~", "./", "../"). While
	// chooser.claim-source is on, the chooser shows the first source
	// whose Claims holds for the query, whichever is current, and goes
	// back to the one it left when the query stops being claimed; no key
	// switches. Stepping to another source by hand (chooser.next-source)
	// ends the claim until the query is no longer claimed.
	Claims func(query string) bool
	// LoadMode, when set on a Live source, stands in for Load: the source
	// ranks its rows in the picker's match mode itself (the path picker's
	// find source), so the mode's tag and match marks show, tab and
	// search.cycle-match change it, and a change loads again.
	LoadMode func(query, mode string) Result
	// Preview, when set, is the highlighted row's preview, drawn beside
	// the rows as the spec's Preview is: Done(text), or Pending work whose
	// value is the text, run off the loop as a replacing job of the
	// picker (a newer highlight replaces it; closing cancels it).
	Preview func(row ChooserRow) Result
	// Complete, when set, fills the highlighted row into the query
	// (picker.complete, tab) without picking it, and returns the query to
	// show; false leaves everything as it is.
	Complete func(row ChooserRow) (query string, ok bool)
	// Current, when set, is the row picker.pick {current: true} takes
	// (the path picker's find: the folder it searches).
	Current func() (ChooserRow, bool)
	// Close, when set, runs when the picker closes: the source stops work
	// of its own (an index running between loads).
	Close func()
	// CanOpen, when set, says a row without Opens can still be browsed
	// into with picker.into (a row of a list of folders): Open then takes
	// it. Enter still opens only a row marked Opens.
	CanOpen func(row ChooserRow) bool
	// UpFrom, when set, stands in for Up: it also knows the highlighted
	// row (a list of places goes up from the place under the highlight).
	UpFrom func(row ChooserRow, query string) (string, bool)
	// Goto, when set, sends the source to a named place (picker.goto: "~",
	// "/", "first-col", "last-col") and returns the query to show next.
	Goto func(to string) (query string, ok bool)
	// Landing, when set, names where the highlight goes after a navigation
	// (Open, Up, Goto, Make) once the rows arrive: a section name, or
	// "row:ID" for one row (going up lands on the folder just left).
	Landing func() string
	// Make, when set, makes a new thing named in the search line
	// (picker.make, a new folder in the folder shown) as work off the loop;
	// MakeLabel says what and where while it is being named. The source
	// has moved into the new thing when the work succeeds. A MakeLabel
	// that answers "" says nothing can be made here now.
	Make      func(name string) Result
	MakeLabel func() string
	// Side, when set, gives the side column drawn beside the rows
	// (ChooserSpec.Columns): a key naming what it shows, and the work that
	// loads it (a PickerSide) when the key changed.
	Side func() (key string, load func() Result)
	// Section, with SectionLimit, declares the source as a section of a
	// sectioned picker (the directory picker's hybrid style): its title and
	// the most rows it shows while the query is empty. "" leaves the source
	// out of the sections.
	Section      string
	SectionLimit int
	// Stands names the sources this one stands for: a picker that offers
	// it leaves them out (the hybrid style's one list is browse and recent).
	Stands []string
}

// PickerSide is a source's side column (PickerSource.Side): a title, the
// lines listed under it, and the one to mark as the place the rows are in.
type PickerSide struct {
	Title string
	Lines []string
	At    string
}

// pickerSide is the chooser's loaded side column.
type pickerSide struct {
	key     string
	value   PickerSide
	loading bool
	gen     uint64
}

// Chooser settings: rows of settings/ochazuke.json.
const (
	// ChooserRememberSourceSetting reopens a chooser with sources on the
	// source last used for its kind (on), or on its first source (off).
	ChooserRememberSourceSetting = "picker.remember-source"
	// ChooserClaimSourceSetting lets a source that claims the query
	// (ChooserSource.Claims) answer for it at once (on, the default), or
	// leaves the source switch to the user (off).
	ChooserClaimSourceSetting = "picker.claim-source"
	// ChooserEnterSetting is what enter does on a row that can be both
	// opened and picked (a folder in a folder chooser): "open" (the
	// default; alt+enter picks) or "pick" (ctrl+l opens).
	ChooserEnterSetting = "picker.enter"
)

// chooserLoadVerb names a source's load job.
const chooserLoadVerb = "ochazuke.picker.load"

// chooserStateID is the instance holding what choosers remember between
// opens (the last source per kind) and the path sources (path_picker.go).
const chooserStateID = "__picker-state"

// chooserState is the chooser memory component.
type chooserState struct {
	lastSource  map[string]string
	pathSources []namedPathSource
	pathBase    func(PathPick) string
	recentPaths func(PathPick) []string
	recentUses  func(PathPick) []PathUse
	picked      []string // paths picked through a path chooser, newest first
	walks       *walkCache
	// pickers are the declared pickers (AddPicker).
	pickers []PickerDef
	// findIndex and findStat stand in for the find source's index and
	// stat (tests hold or fail them); nil is the file system's.
	findIndex func(ctx context.Context, root string, recursive bool) ([]PickerEntry, error)
	findStat  func(string) (fs.FileInfo, error)
	// find is the find source made last (the finder shim reads it) and
	// findOrigin the context picker.open opens it from.
	find       *pathFind
	findOrigin Context
}

func (s *chooserState) HandleMessage(tea.Msg) bool { return false }
func (s *chooserState) PendingCommand() tea.Cmd    { return nil }
func (s *chooserState) Close()                     {}

func (r *Runtime) chooserMemory() *chooserState {
	inst := r.Instances[chooserStateID]
	if inst == nil {
		inst = r.Instance(chooserStateID)
	}
	s, ok := inst.Component.(*chooserState)
	if !ok {
		s = &chooserState{lastSource: map[string]string{}}
		inst.Component = s
	}
	return s
}

// ChooserLastSource is the source last used by a chooser of kind ("" when
// none was).
func (r *Runtime) ChooserLastSource(kind string) string {
	return r.chooserMemory().lastSource[kind]
}

func registerChooserSources(r *Runtime) {
	r.chooserMemory()
	step := func(delta int) AppHandler {
		return func(ctx Ctx) Result {
			c := r.chooserAt(ctx.Instance)
			if c == nil {
				return Failed(errors.New("no picker is open"))
			}
			if err := c.cycle(delta); err != nil {
				return Failed(err)
			}
			return Done(c.state())
		}
	}
	r.Define(chooserNoun).
		Verb("next-source", step(1), "Show the next source's rows; the query stays").When(PickerSourcesGuard).
		Verb("previous-source", step(-1), "Show the previous source's rows; the query stays").When(PickerSourcesGuard)
	r.DefineGuard(PickerSourcesGuard, func(r *Runtime, ev KeyEvent) bool {
		c := r.chooserAt(r.Instances[ev.Focus])
		return c != nil && len(c.spec.Sources) > 1
	})
}

// ChooserSource is PickerSource's old name.
//
// Deprecated: use PickerSource; kept for one release (v0.96.0).
type ChooserSource = PickerSource

// Source is the current source's name, "" for a chooser without sources.
func (c *Chooser) Source() string {
	if s := c.current(); s != nil {
		return s.Name
	}
	return ""
}

// Sources are the names of the chooser's sources, in order.
func (c *Chooser) Sources() []string {
	out := make([]string, len(c.spec.Sources))
	for i, s := range c.spec.Sources {
		out[i] = s.Name
	}
	return out
}

// Loading reports whether the current source's load is in flight.
func (c *Chooser) Loading() bool { return c.loading }

func (c *Chooser) current() *ChooserSource {
	if c.source < 0 || c.source >= len(c.spec.Sources) {
		return nil
	}
	return &c.spec.Sources[c.source]
}

// startSource picks the source a chooser opens on: the spec's Source,
// else (no Source, or one this open does not have) the remembered one
// (ChooserRememberSourceSetting), else the first.
func (c *Chooser) startSource() int {
	find := func(name string) (int, bool) {
		for i, s := range c.spec.Sources {
			if name != "" && s.Name == name {
				return i, true
			}
		}
		return 0, false
	}
	if i, ok := find(c.spec.Source); ok {
		return i
	}
	if v, _ := c.r.settingValue(ChooserRememberSourceSetting); v != false {
		if i, ok := find(c.r.chooserMemory().lastSource[c.spec.Kind]); ok {
			return i
		}
	}
	return 0
}

// keep is the cell columns cut last: the current source's Keep, else the
// spec's.
func (c *Chooser) keep() []int {
	if s := c.current(); s != nil && s.Keep != nil {
		return s.Keep
	}
	return c.spec.Keep
}

// cycle steps to another source, wrapping; the query stays.
func (c *Chooser) cycle(delta int) error {
	c.filters.linger.reset()
	n := len(c.spec.Sources)
	if n < 2 {
		return errors.New("the picker has one source")
	}
	c.pending = false
	c.suppressed = c.claimedBy(c.Query()) >= 0
	c.claimed = false
	c.source = ((c.source+delta)%n + n) % n
	c.r.chooserMemory().lastSource[c.spec.Kind] = c.Source()
	c.spec.Rows = nil
	c.cachePropRows()
	c.load()
	return nil
}

// ChooserBatch is a streaming load's value: a source's Load (or the
// Pending work it returns) gives one part of its rows, shown at once, and
// More, pending work whose value is the next ChooserBatch (or the last
// []ChooserRow). The chooser appends each part as it lands and stays
// loading until a part has no More; a newer load or closing the chooser
// cancels the rest. A walk over a folder tree streams this way, so the
// first rows show before it finishes and the list stays responsive.
//
// A part with Replace stands in for the rows so far rather than adding to
// them: a fuller pass over the same rows (the find source's recursive
// index after its shallow one).
type ChooserBatch struct {
	Rows    []ChooserRow
	More    Result
	Replace bool
}

// chooserRowsOf reads a load's value: its rows, for a ChooserBatch the
// work that loads the rest, and whether the rows replace the ones so far.
func chooserRowsOf(value any) ([]ChooserRow, Result, bool) {
	switch v := value.(type) {
	case ChooserBatch:
		return v.Rows, v.More, v.Replace
	case *ChooserBatch:
		if v != nil {
			return v.Rows, v.More, v.Replace
		}
	case []ChooserRow:
		return v, Result{}, false
	}
	return nil, Result{}, false
}

// claimedBy is the index of the first source claiming query, -1 for none.
func (c *Chooser) claimedBy(query string) int {
	for i, s := range c.spec.Sources {
		if s.Claims != nil && s.Claims(query) {
			return i
		}
	}
	return -1
}

// claim shows the source that claims the query (and goes back to the one
// it left when none does), loading it; it reports whether it switched.
func (c *Chooser) claim() bool {
	if len(c.spec.Sources) < 2 || !c.r.settingOn(ChooserClaimSourceSetting) {
		return false
	}
	want := c.claimedBy(c.Query())
	if want < 0 {
		c.suppressed = false
	}
	to := -1
	switch {
	case c.suppressed:
		return false
	case want >= 0 && want != c.source:
		if !c.claimed {
			c.base, c.claimed = c.source, true
		}
		to = want
	case want < 0 && c.claimed:
		to, c.claimed = c.base, false
	}
	if to < 0 {
		return false
	}
	c.filters.linger.reset()
	c.source = to
	c.spec.Rows = nil
	c.cachePropRows()
	c.load()
	return true
}

// load asks the current source for its rows at the query.
func (c *Chooser) load() {
	src := c.current()
	if src == nil || src.Load == nil && src.LoadMode == nil {
		return
	}
	query := c.Query()
	c.LoadedMode = c.MatchMode()
	c.loadedQuery = query
	c.loadErr = ""
	c.gen++
	gen := c.gen
	var launch func(res Result, first bool)
	// land shows one part of the load: the first replaces the rows, a
	// later one (a ChooserBatch's More) appends to them.
	land := func(value any, err error, first bool) {
		rows, more, replace := chooserRowsOf(value)
		if err != nil && !errors.Is(err, context.Canceled) {
			c.loadErr = err.Error()
			rows, more = nil, Result{}
		}
		if !first && !replace {
			rows = append(slices.Clone(c.spec.Rows), rows...)
		}
		c.loading = more.work != nil
		c.SetRows(rows)
		if c.pending && !c.loading {
			if c.loadErr != "" {
				c.pending = false
			} else {
				c.runPending()
				return
			}
		}
		if more.work != nil || more.value != nil || more.err != nil {
			launch(more, false)
		}
	}
	launch = func(res Result, first bool) {
		if res.work == nil {
			land(res.value, res.err, first)
			return
		}
		c.loading = true
		res = res.Replacing("source").also(func(ctx Ctx, value any, err error) {
			if c.r.Chooser(c.spec.Kind) != c || gen != c.gen {
				return
			}
			land(value, err, first)
		})
		if _, err := c.r.Launch(c.spec.Kind, chooserLoadVerb, res, Ctx{actor: c.r.Actor(), Runtime: c.r, Context: c.r.contextOf(c.spec.Kind), Instance: c.r.Instances[c.spec.Kind]}); err != nil {
			land(nil, err, first)
		}
	}
	if src.LoadMode != nil {
		launch(src.LoadMode(query, c.LoadedMode), true)
		return
	}
	launch(src.Load(query), true)
}

// modeRanked reports whether the current source ranks its rows in the
// picker's match mode itself (ChooserSource.LoadMode).
func (c *Chooser) modeRanked() bool {
	src := c.current()
	return src != nil && src.LoadMode != nil
}

// ChooserPreview is a source's preview (ChooserSource.Preview) of the
// highlighted row: the row it is for, its text, whether its job runs, and
// its generation (a newer one drops an older job's answer).
type ChooserPreview struct {
	Row     string
	Text    string
	Loading bool
	Gen     uint64
}

// previewing reports whether the picker draws a preview: the spec's, or
// the current source's.
func (c *Chooser) previewing() bool {
	if c.spec.Preview != nil {
		return true
	}
	src := c.current()
	return src != nil && src.Preview != nil
}

// previewText is the highlighted row's preview.
func (c *Chooser) previewText(row ChooserRow) string {
	if c.spec.Preview != nil {
		return c.spec.Preview(row)
	}
	if c.Preview.Row != row.ID {
		return ""
	}
	if c.Preview.Loading {
		return LoadingText("preview")
	}
	return c.Preview.Text
}

// queuePreview starts the current source's preview of the highlighted
// row when the highlight moved to another row, as a replacing job of the
// picker; a newer one drops it, closing cancels it.
func (c *Chooser) queuePreview() {
	src := c.current()
	if src == nil || src.Preview == nil || c.spec.Preview != nil {
		return
	}
	row, ok := c.Highlighted()
	if !ok || row.ID == chooserCreateRow {
		row = ChooserRow{}
	}
	if row.ID == c.Preview.Row {
		return
	}
	gen := c.Preview.Gen + 1
	c.Preview = ChooserPreview{Row: row.ID, Gen: gen}
	if row.ID == "" {
		return
	}
	res := src.Preview(row)
	if res.work == nil {
		if res.err != nil {
			c.Preview.Text = res.err.Error()
		} else if text, ok := res.value.(string); ok {
			c.Preview.Text = text
		}
		return
	}
	c.Preview.Loading = true
	res = res.Replacing("preview").also(func(_ Ctx, value any, err error) {
		if c.r.Chooser(c.spec.Kind) != c || c.Preview.Gen != gen {
			return
		}
		c.Preview.Loading = false
		if err != nil {
			c.Preview.Text = err.Error()
		} else if text, ok := value.(string); ok {
			c.Preview.Text = text
		}
	})
	if _, err := c.r.Launch(c.spec.Kind, chooserPreviewVerb, res, Ctx{actor: c.r.Actor(), Runtime: c.r, Context: c.r.contextOf(c.spec.Kind), Instance: c.r.Instances[c.spec.Kind]}); err != nil {
		c.Preview.Loading, c.Preview.Text = false, err.Error()
	}
}

// chooserPreviewVerb names a source's preview job.
const chooserPreviewVerb = "ochazuke.picker.preview"

// completes reports whether the current source fills the highlighted row
// into the query (ChooserSource.Complete).
func (c *Chooser) completes() bool {
	src := c.current()
	if src == nil || src.Complete == nil {
		return false
	}
	row, ok := c.Highlighted()
	return ok && row.ID != chooserCreateRow
}

// complete fills the highlighted row into the query; the source stays.
func (c *Chooser) complete() error {
	if !c.completes() {
		return errors.New("this picker has nothing to complete")
	}
	row, _ := c.Highlighted()
	query, ok := c.current().Complete(row)
	if !ok {
		return fmt.Errorf("cannot complete %s", row.Label)
	}
	c.filters.linger.reset()
	_ = c.input.Replace(0, len(c.input.Text), query)
	c.input.Cursor = len(c.input.Text)
	c.sync()
	return nil
}

// closeSources runs each source's Close.
func (c *Chooser) closeSources() {
	for _, s := range c.spec.Sources {
		if s.Close != nil {
			s.Close()
		}
	}
}

// opens reports whether the highlighted row opens inside its source.
func (c *Chooser) opens() bool {
	src := c.current()
	row, ok := c.Highlighted()
	return ok && row.Opens && src != nil && src.Open != nil
}

// canInto reports whether picker.into can browse into the highlighted row:
// one that opens, or one the source says it can open (CanOpen).
func (c *Chooser) canInto() bool {
	if c.opens() {
		return true
	}
	src := c.current()
	row, ok := c.Highlighted()
	return ok && src != nil && src.Open != nil && src.CanOpen != nil && row.ID != chooserCreateRow && src.CanOpen(row)
}

func (c *Chooser) canUp() bool {
	src := c.current()
	return src != nil && (src.Up != nil || src.UpFrom != nil)
}

// open navigates into the highlighted row and reloads the source.
func (c *Chooser) open() error {
	if !c.canInto() {
		return errors.New("the highlighted row does not open")
	}
	row, _ := c.Highlighted()
	query, ok := c.current().Open(row)
	if !ok {
		return fmt.Errorf("cannot open %s", row.Label)
	}
	c.land = c.current().landing()
	c.navigated(query)
	return nil
}

// up steps the source back out and reloads it.
func (c *Chooser) up() error {
	if !c.canUp() {
		return errors.New("the source cannot step up")
	}
	src := c.current()
	var query string
	var ok bool
	if src.UpFrom != nil {
		row, _ := c.Highlighted()
		query, ok = src.UpFrom(row, c.Query())
	} else {
		query, ok = src.Up(c.Query())
	}
	if !ok {
		return errors.New("nowhere to go up to")
	}
	c.land = src.landing()
	c.navigated(query)
	return nil
}

// navigated shows query (cursor at its end) and loads the source again.
func (c *Chooser) navigated(query string) {
	c.filters.linger.reset()
	_ = c.input.Replace(0, len(c.input.Text), query)
	c.input.Cursor = len(c.input.Text)
	c.spec.Rows = nil
	c.cachePropRows()
	c.load()
	c.refilter()
}

// sourceTitle is the title with the sources: the current one in brackets
// (its Label when it has one), the others by name.
func (c *Chooser) sourceTitle() string {
	if len(c.spec.Sources) < 2 {
		return c.spec.Title
	}
	parts := make([]string, len(c.spec.Sources))
	for i, s := range c.spec.Sources {
		parts[i] = s.Name
		if i == c.source {
			label := s.Name
			if s.Label != nil {
				label = s.Label()
			}
			parts[i] = "[" + label + "]"
		}
	}
	title := strings.Join(parts, " ")
	if c.spec.Title == "" {
		return title
	}
	return c.spec.Title + " · " + title
}

// emptyLine is what the chooser says while it has no rows, in the one
// format (Placeholder): the load's error, the source's or the spec's
// Loading while a source load or their LoadingJob runs, else the source's
// or the spec's Empty; "" when the spec names neither Empty nor Plural.
func (c *Chooser) emptyLine() string {
	p := Placeholder{Plural: c.spec.Plural, Empty: c.spec.Empty, Loading: c.spec.Loading, LoadingJob: c.spec.LoadingJob}
	src := c.current()
	if src != nil && src.LoadingJob.Slot != "" {
		p.LoadingJob = src.LoadingJob
	}
	if src != nil && src.Loading != "" {
		p.Loading = src.Loading
	}
	switch {
	case c.loadErr != "":
		return "! " + c.loadErr
	case c.loading || p.LoadingJob.Running(c.r):
		if p.Loading != "" {
			return p.Loading
		}
		return LoadingText(p.Plural)
	case src != nil && src.Empty != "":
		return c.hinted(src.Empty)
	case p.Empty == "" && p.Plural == "":
		return ""
	}
	return c.hinted(p.Text(c.r))
}

// hinted ends an empty or no-match line with the spec's Hint.
func (c *Chooser) hinted(line string) string {
	if c.spec.Hint == "" || strings.Contains(line, c.spec.Hint) {
		return line
	}
	return line + "; " + c.spec.Hint
}

// waiting reports whether enter must wait: a Live source's rows are still
// being loaded for the query, so the rows on show may be an older query's.
func (c *Chooser) waiting() bool {
	c.sync()
	return c.live() && c.loading
}

// enterOpens reports whether enter opens the highlighted row (a row that
// only opens, else chooser.enter) rather than picking it.
func (c *Chooser) enterOpens() bool {
	if !c.opens() {
		return false
	}
	row, _ := c.Highlighted()
	v, _ := c.r.settingValue(ChooserEnterSetting)
	return row.NoPick || v != "pick"
}

// enterNow is enter on the rows as they are: open or pick.
func (c *Chooser) enterNow() Result {
	if c.enterOpens() {
		if err := c.open(); err != nil {
			return Failed(err)
		}
		return Done(c.state())
	}
	return c.pick()
}

// runPending runs the enter that waited for a load, now that the rows of
// the query as typed have landed. Its result is delivered as a verb's
// would be: pending work becomes a job of the focus the chooser opened
// over, an error is shown and the chooser stays.
func (c *Chooser) runPending() {
	c.pending = false
	inst := c.r.Instances[c.spec.Kind]
	if inst == nil {
		return
	}
	ancestry := c.r.Ancestry(inst.ID)
	ctx := Ctx{actor: c.r.Actor(), Runtime: c.r, Context: c.r.contextOf(c.spec.Kind), Instance: inst}
	res := c.enterNow()
	switch {
	case res.err != nil:
		ctx.Error(res.err.Error())
	case res.work != nil || res.fg != nil:
		owner := inst.ID
		if c.r.Instances[owner] == nil {
			owner = c.r.survivor(ancestry)
		}
		if _, err := c.r.Launch(owner, "ochazuke.picker.pick", res, ctx); err != nil {
			ctx.Error(err.Error())
		}
	}
}

// createFunc and createLabel are the current source's create, else the
// spec's.
func (c *Chooser) createFunc() func(string) Result {
	if src := c.current(); src != nil && src.Create != nil {
		return src.Create
	}
	return c.spec.Create
}

func (c *Chooser) createLabel() func(string) string {
	if src := c.current(); src != nil && src.CreateLabel != nil {
		return src.CreateLabel
	}
	return c.spec.CreateLabel
}

// live reports whether the current source matches the query itself.
func (c *Chooser) live() bool {
	src := c.current()
	return src != nil && src.Live
}

// sourceNames is a helper for state(): the names, in order.
func (c *Chooser) sourceNames() []string { return slices.Clone(c.Sources()) }

// landing is where the highlight goes after a navigation ("" for the top).
func (s *PickerSource) landing() string {
	if s == nil || s.Landing == nil {
		return ""
	}
	return s.Landing()
}
