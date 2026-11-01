package ochazuke

import (
	"context"
	"errors"
	"fmt"
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

// ChooserSource is one source of a chooser's rows.
type ChooserSource struct {
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
	// Empty is the line shown while the source has no rows ("no recent
	// paths"); the spec's Empty (or "loading…" while a load runs) else.
	Empty string
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
}

// Chooser settings: rows of settings/ochazuke.json.
const (
	// ChooserRememberSourceSetting reopens a chooser with sources on the
	// source last used for its kind (on), or on its first source (off).
	ChooserRememberSourceSetting = "chooser.remember-source"
	// ChooserEnterSetting is what enter does on a row that can be both
	// opened and picked (a folder in a folder chooser): "open" (the
	// default; alt+enter picks) or "pick" (ctrl+l opens).
	ChooserEnterSetting = "chooser.enter"
)

// Chooser guards: the rows of chooser.json name them.
const (
	ChooserSourcesGuard    = "chooser-sources"     // more than one source
	ChooserOpensGuard      = "chooser-opens"       // the highlighted row opens
	ChooserEnterOpensGuard = "chooser-enter-opens" // enter opens it (ChooserEnterSetting, or a row that only opens)
	ChooserUpGuard         = "chooser-up"          // the source can step up
	ChooserUpEmptyGuard    = "chooser-up-empty"    // it can, and the query is empty (backspace)
)

// chooserLoadVerb names a source's load job.
const chooserLoadVerb = "ochazuke.chooser.load"

// chooserStateID is the instance holding what choosers remember between
// opens (the last source per kind) and the path sources (path_picker.go).
const chooserStateID = "__chooser-state"

// chooserState is the chooser memory component.
type chooserState struct {
	lastSource  map[string]string
	pathSources []namedPathSource
	pathBase    func(PathPick) string
	recentPaths func(PathPick) []string
	picked      []string // paths picked through a path chooser, newest first
	walks       *walkCache
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
				return Failed(errors.New("no chooser is open"))
			}
			if err := c.cycle(delta); err != nil {
				return Failed(err)
			}
			return Done(c.state())
		}
	}
	r.Define(chooserNoun).
		Verb("next-source", step(1), "Show the next source's rows; the query stays").When(ChooserSourcesGuard).
		Verb("previous-source", step(-1), "Show the previous source's rows; the query stays").When(ChooserSourcesGuard).
		Verb("open", func(ctx Ctx) Result {
			c := r.chooserAt(ctx.Instance)
			if c == nil {
				return Failed(errors.New("no chooser is open"))
			}
			if err := c.open(); err != nil {
				return Failed(err)
			}
			return Done(c.state())
		}, "Open the highlighted row inside its source (browse into a folder)").When(ChooserOpensGuard).
		Verb("up", func(ctx Ctx) Result {
			c := r.chooserAt(ctx.Instance)
			if c == nil {
				return Failed(errors.New("no chooser is open"))
			}
			if err := c.up(); err != nil {
				return Failed(err)
			}
			return Done(c.state())
		}, "Step the source back out (the parent folder)").When(ChooserUpGuard)
	at := func(ev KeyEvent) *Chooser { return r.chooserAt(r.Instances[ev.Focus]) }
	r.DefineGuard(ChooserSourcesGuard, func(r *Runtime, ev KeyEvent) bool {
		c := at(ev)
		return c != nil && len(c.spec.Sources) > 1
	})
	r.DefineGuard(ChooserOpensGuard, func(r *Runtime, ev KeyEvent) bool {
		c := at(ev)
		return c != nil && c.opens()
	})
	r.DefineGuard(ChooserEnterOpensGuard, func(r *Runtime, ev KeyEvent) bool {
		c := at(ev)
		if c == nil || !c.opens() {
			return false
		}
		row, _ := c.Highlighted()
		v, _ := r.settingValue(ChooserEnterSetting)
		return row.NoPick || v != "pick"
	})
	r.DefineGuard(ChooserUpGuard, func(r *Runtime, ev KeyEvent) bool {
		c := at(ev)
		return c != nil && c.canUp()
	})
	r.DefineGuard(ChooserUpEmptyGuard, func(r *Runtime, ev KeyEvent) bool {
		c := at(ev)
		return c != nil && c.canUp() && c.Query() == ""
	})
}

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
		return errors.New("the chooser has one source")
	}
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
type ChooserBatch struct {
	Rows []ChooserRow
	More Result
}

// chooserRowsOf reads a load's value: its rows and, for a ChooserBatch,
// the work that loads the rest.
func chooserRowsOf(value any) ([]ChooserRow, Result) {
	switch v := value.(type) {
	case ChooserBatch:
		return v.Rows, v.More
	case *ChooserBatch:
		if v != nil {
			return v.Rows, v.More
		}
	case []ChooserRow:
		return v, Result{}
	}
	return nil, Result{}
}

// load asks the current source for its rows at the query.
func (c *Chooser) load() {
	src := c.current()
	if src == nil || src.Load == nil {
		return
	}
	query := c.Query()
	c.loadedQuery = query
	c.loadErr = ""
	c.gen++
	gen := c.gen
	var launch func(res Result, first bool)
	// land shows one part of the load: the first replaces the rows, a
	// later one (a ChooserBatch's More) appends to them.
	land := func(value any, err error, first bool) {
		rows, more := chooserRowsOf(value)
		if err != nil && !errors.Is(err, context.Canceled) {
			c.loadErr = err.Error()
			rows, more = nil, Result{}
		}
		if !first {
			rows = append(slices.Clone(c.spec.Rows), rows...)
		}
		c.loading = more.work != nil
		c.SetRows(rows)
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
		inner := res.then
		res = res.Replacing("source").Then(func(ctx Ctx, value any, err error) {
			if inner != nil {
				inner(ctx, value, err)
			}
			if c.r.Chooser(c.spec.Kind) != c || gen != c.gen {
				return
			}
			land(value, err, first)
		})
		if _, err := c.r.Launch(c.spec.Kind, chooserLoadVerb, res, Ctx{actor: c.r.Actor(), Runtime: c.r, Context: c.r.contextOf(c.spec.Kind), Instance: c.r.Instances[c.spec.Kind]}); err != nil {
			land(nil, err, first)
		}
	}
	launch(src.Load(query), true)
}

// opens reports whether the highlighted row opens inside its source.
func (c *Chooser) opens() bool {
	src := c.current()
	row, ok := c.Highlighted()
	return ok && row.Opens && src != nil && src.Open != nil
}

func (c *Chooser) canUp() bool {
	src := c.current()
	return src != nil && src.Up != nil
}

// open navigates into the highlighted row and reloads the source.
func (c *Chooser) open() error {
	if !c.opens() {
		return errors.New("the highlighted row does not open")
	}
	row, _ := c.Highlighted()
	query, ok := c.current().Open(row)
	if !ok {
		return fmt.Errorf("cannot open %s", row.Label)
	}
	c.navigated(query)
	return nil
}

// up steps the source back out and reloads it.
func (c *Chooser) up() error {
	if !c.canUp() {
		return errors.New("the source cannot step up")
	}
	query, ok := c.current().Up(c.Query())
	if !ok {
		return errors.New("nowhere to go up to")
	}
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

// emptyLine is what the chooser says while it has no rows.
func (c *Chooser) emptyLine() string {
	switch src := c.current(); {
	case c.loadErr != "":
		return "! " + c.loadErr
	case c.loading:
		return "loading…"
	case src != nil && src.Empty != "":
		return src.Empty
	}
	return c.spec.Empty
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
