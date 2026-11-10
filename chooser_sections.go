package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Sections, marks, making and navigation shortcuts of a picker: the
// parts a directory picker (path_hybrid.go, path_columns.go) is built from,
// each general, driven by data on the rows, the spec and the source.

// withoutHeaders is rows without their section headings.
func withoutHeaders(rows []ChooserRow) []ChooserRow {
	return slices.DeleteFunc(slices.Clone(rows), func(r ChooserRow) bool { return r.Header })
}

// rankSections runs rank over the rows of each section and drops a heading
// no row of its section survived under, keeping the sections' order.
func rankSections(rows []ChooserRow, rank func([]ChooserRow) []ChooserRow) []ChooserRow {
	var out []ChooserRow
	flush := func(head *ChooserRow, part []ChooserRow) {
		part = rank(part)
		if len(part) == 0 {
			return
		}
		if head != nil {
			out = append(out, *head)
		}
		out = append(out, part...)
	}
	var head *ChooserRow
	var part []ChooserRow
	for _, row := range rows {
		if row.Header {
			flush(head, part)
			h := row
			head, part = &h, nil
			continue
		}
		part = append(part, row)
	}
	flush(head, part)
	return out
}

// skipHeaders moves the highlight off a section heading in direction dir
// (1 down, -1 up); it stays on the heading when no row lies that way.
func (c *Chooser) skipHeaders(dir int) {
	for c.cursor >= 0 && c.cursor < len(c.matches) && c.matches[c.cursor].Header {
		next := c.cursor + dir
		if next < 0 || next >= len(c.matches) {
			return
		}
		c.cursor = next
	}
}

// landOn puts the highlight on the first row of the section a navigation
// asked for (PickerSource.Landing) once its rows have arrived.
func (c *Chooser) landOn() {
	if c.land == "" || c.making {
		return
	}
	id, byID := strings.CutPrefix(c.land, "row:")
	for i, row := range c.matches {
		if row.Header || row.ID == chooserCreateRow {
			continue
		}
		if byID && row.ID == id || !byID && row.Section == c.land {
			c.cursor, c.moved, c.land = i, true, ""
			return
		}
	}
}

// sectionIndexes are the indexes of the section headings in the matches.
func (c *Chooser) sectionIndexes() []int {
	var out []int
	for i, row := range c.matches {
		if row.Header {
			out = append(out, i)
		}
	}
	return out
}

// section moves the highlight to the first row of the next (delta 1) or
// previous (-1) section; the ends clamp.
func (c *Chooser) section(delta int) error {
	c.sync()
	heads := c.sectionIndexes()
	if len(heads) == 0 {
		return errors.New("this picker has no sections")
	}
	at := -1
	for j, h := range heads {
		if h <= c.cursor {
			at = j
		}
	}
	to := min(max(at+delta, 0), len(heads)-1)
	for i := heads[to] + 1; i < len(c.matches); i++ {
		if !c.matches[i].Header {
			c.cursor, c.moved, c.pending = i, true, false
			return nil
		}
	}
	return errors.New("no row in that section")
}

// markable reports whether row can be marked: a row of the source, not a
// heading, the create row, a refused or open-only one.
func markable(row ChooserRow) bool {
	return !row.Header && !row.Fixed && row.ID != chooserCreateRow && row.Refuse == "" && !row.NoPick
}

// marked reports whether row is marked.
func (c *Chooser) marked(id string) bool { _, ok := c.marks[id]; return ok }

// Marked are the marked rows, in the order they were marked.
func (c *Chooser) Marked() []ChooserRow {
	out := make([]ChooserRow, 0, len(c.markOrder))
	for _, id := range c.markOrder {
		out = append(out, c.marks[id])
	}
	return out
}

func (c *Chooser) setMark(row ChooserRow, on bool) {
	if on {
		if _, ok := c.marks[row.ID]; ok {
			return
		}
		if c.marks == nil {
			c.marks = map[string]ChooserRow{}
		}
		c.marks[row.ID] = row
		c.markOrder = append(c.markOrder, row.ID)
		return
	}
	delete(c.marks, row.ID)
	c.markOrder = slices.DeleteFunc(c.markOrder, func(id string) bool { return id == row.ID })
}

// mark toggles the highlighted row's mark and steps the highlight down.
func (c *Chooser) mark() error {
	if !c.spec.Many {
		return errors.New("this picker takes one row; nothing to mark")
	}
	row, ok := c.Highlighted()
	if !ok || !markable(row) {
		return errors.New("that row cannot be marked")
	}
	c.setMark(row, !c.marked(row.ID))
	c.pending = false
	return nil
}

// markAll marks every row the query kept that can be marked; when they
// all are, it clears the marks.
func (c *Chooser) markAll() error {
	if !c.spec.Many {
		return errors.New("this picker takes one row; nothing to mark")
	}
	c.sync()
	var rows []ChooserRow
	for _, row := range c.matches {
		if markable(row) {
			rows = append(rows, row)
		}
	}
	all := len(rows) > 0
	for _, row := range rows {
		all = all && c.marked(row.ID)
	}
	for _, row := range rows {
		c.setMark(row, !all)
	}
	c.pending = false
	return nil
}

// gutter is the two cells before a row: its mark in a picker that takes
// several, else the list gutter.
func (c *Chooser) gutter(id string) string {
	if c.spec.Many {
		if c.marked(id) {
			return "◉ "
		}
		return "· "
	}
	return c.filters.rowGutter(id)
}

// pickMarked takes the marked rows: ChooseMany with them, else Choose for
// each in turn (the last result answers; the first failure stops).
func (c *Chooser) pickMarked() Result {
	rows := c.Marked()
	accepted := c.r.acceptModal(c.spec.Kind)
	if !c.spec.Stay {
		if err := c.close(); err != nil {
			return Failed(err)
		}
	}
	accepted()
	defer c.r.actingFor(c.r.Focus)()
	if c.spec.ChooseMany != nil {
		return c.spec.ChooseMany(rows)
	}
	if c.spec.Choose == nil {
		return Done(len(rows))
	}
	res := Done(nil)
	for _, row := range rows {
		res = c.spec.Choose(row)
		if res.err != nil {
			return res
		}
	}
	return res
}

// toggle flips a named option of the picker and loads its source again.
func (c *Chooser) toggle(name string) error {
	i := slices.IndexFunc(c.spec.Toggles, func(t PickerToggle) bool { return t.Name == name })
	if i < 0 {
		return fmt.Errorf("this picker has no option %q", name)
	}
	say := c.spec.Toggles[i].Flip()
	c.filters.linger.reset()
	c.spec.Rows = nil
	c.cachePropRows()
	c.load()
	c.refilter()
	if say != "" {
		c.r.UI().Info(say)
	}
	return nil
}

// gotoSource sends the current source to a named place ("~", "/", or
// "first-col"/"last-col") and loads it again.
func (c *Chooser) gotoSource(to string) error {
	src := c.current()
	if src == nil || src.Goto == nil {
		return errors.New("this picker cannot jump")
	}
	query, ok := src.Goto(to)
	if !ok {
		return fmt.Errorf("nowhere to go for %q", to)
	}
	c.land = src.landing()
	c.navigated(query)
	return nil
}

// canMake reports whether the current source makes new things.
func (c *Chooser) canMake() bool {
	src := c.current()
	return src != nil && src.Make != nil
}

// beginMake starts naming a new thing (PickerSource.Make): the search
// line is its name until enter makes it or escape drops it.
func (c *Chooser) beginMake() error {
	if !c.canMake() {
		return errors.New("this picker has nothing to make here")
	}
	if label := c.current().MakeLabel; label != nil && label() == "" {
		return errors.New("open a folder first: nothing can be made here")
	}
	if c.making {
		return nil
	}
	c.sync()
	c.making, c.madeQuery = true, c.Query()
	_ = c.input.Replace(0, len(c.input.Text), "")
	c.input.Cursor = 0
	c.pending = false
	c.refilter()
	// The name is typed: the search line takes keys.
	return c.r.SetMode("insert")
}

// endMake drops the name being typed and restores the search line.
func (c *Chooser) endMake() {
	if !c.making {
		return
	}
	c.making = false
	_ = c.input.Replace(0, len(c.input.Text), c.madeQuery)
	c.input.Cursor = len(c.input.Text)
	c.madeQuery = ""
	c.refilter()
}

// makeNow makes the thing named in the search line (enter while making):
// the source's work runs as a job of the picker, and on success the
// picker leaves making and loads the source again (the source has moved
// into what it made).
func (c *Chooser) makeNow() Result {
	name := strings.TrimSpace(c.Query())
	if name == "" {
		return Failed(errors.New("type a name first"))
	}
	src := c.current()
	if src == nil || src.Make == nil {
		return Failed(errors.New("this picker has nothing to make here"))
	}
	res := src.Make(name)
	done := func(err error) {
		if err != nil {
			c.loadErr = ""
			return
		}
		c.making, c.madeQuery = false, ""
		c.land = src.landing()
		c.navigated("")
		if c.spec.StartNormal {
			_ = c.r.SetMode("normal")
		}
	}
	if res.work == nil {
		if res.err != nil {
			return Failed(res.err)
		}
		done(nil)
		return Done(c.state())
	}
	gen := c.gen
	res = res.Replacing("make").also(func(ctx Ctx, _ any, err error) {
		if c.r.Chooser(c.spec.Kind) != c || gen != c.gen {
			return
		}
		if err != nil {
			ctx.Error(err.Error())
			return
		}
		done(nil)
	})
	return res
}

// makeLabel is the line before the search input while naming: what the
// source makes and where.
func (c *Chooser) makeLabel() string {
	if src := c.current(); src != nil && src.MakeLabel != nil {
		return src.MakeLabel()
	}
	return "new"
}

// registerSectionVerbs registers the verbs of marks, new things, options and
// jumps on the picker noun (sections are move's, picker.move {sections}).
// Every one is a verb of the chooser noun that a binding row reaches; none
// has a key in code.
func registerSectionVerbs(r *Runtime, verb func(fn func(*Chooser, Ctx) Result) AppHandler) {
	state := func(err error, c *Chooser) Result {
		if err != nil {
			return Failed(err)
		}
		return Done(c.state())
	}
	r.Define(chooserNoun).
		Verb("mark", verb(func(c *Chooser, ctx Ctx) Result {
			if boolArg(ctx.Args, "all") {
				return state(c.markAll(), c)
			}
			return state(c.mark(), c)
		}), "Mark or unmark the highlighted row (a picker that takes several); enter then takes the marked rows; all marks every row the query kept, or clears the marks when they all are").
		Params(map[string]Parameter{"all": {Type: "boolean", Description: "Every row the query kept"}}).
		Verb("toggle", verb(func(c *Chooser, ctx Ctx) Result {
			return state(c.toggle(StringArg(ctx.Args, "name", "")), c)
		}), "Switch a named option of this picker (hidden: hidden files) and list again").
		Params(map[string]Parameter{"name": {Type: "string", Required: true, Free: true, Description: "The option: hidden"}}).
		Verb("goto", verb(func(c *Chooser, ctx Ctx) Result {
			return state(c.gotoSource(StringArg(ctx.Args, "to", "")), c)
		}), "Send the source to a place: ~ (home), / (the root), first-col (home), last-col (the deepest place visited), own (the source's own rows again)").
		Params(map[string]Parameter{"to": {Type: "string", Required: true, Enum: []string{"~", "/", "first-col", "last-col", "own"}, Description: "Where to"}}).
		Verb("new", verb(func(c *Chooser, _ Ctx) Result { return state(c.beginMake(), c) }), "Name a new folder to make in the folder the picker shows; enter makes it, escape drops it").
		Verb("open-with", verb(func(c *Chooser, ctx Ctx) Result {
			return c.pathRow(func(p string) error {
				if app := strings.TrimSpace(StringArg(ctx.Args, "with", "")); app != "" {
					return c.r.Opener().OpenWith(app, p)
				}
				_, err := c.r.Open(p)
				return err
			})
		}), "Open the highlighted path through the opener table (open.openers; the OS by default), or with a named application").History(HistoryAction).
		Params(map[string]Parameter{"with": {Type: "string", Free: true, Description: "The application to open it with (macOS: its name, as open -a takes it; elsewhere a command); omitted, the opener table decides"}}).
		agentRight(AgentConfirm).
		Verb("reveal", verb(func(c *Chooser, _ Ctx) Result {
			return c.pathRow(c.r.Opener().Reveal)
		}), "Show the highlighted path in the file manager").History(HistoryAction).
		agentRight(AgentAllow).
		Verb("yank-path", verb(func(c *Chooser, _ Ctx) Result {
			return c.pathRow(func(p string) error { return c.r.storeYank(Yank{Text: p, Shape: "char"}, "", false) })
		}), "Copy the highlighted path into the active register").History(HistoryChange).
		agentRight(AgentAllow)
}

// agentRight sets the last declared verb's agent right.
func (b *NounBuilder) agentRight(right string) *NounBuilder {
	return b.lastOp("agentRight", func(o *Operation) { o.Agent = right })
}

// pathRow runs act on the highlighted row's path (the free path verbs).
func (c *Chooser) pathRow(act func(path string) error) Result {
	c.sync()
	row, ok := c.Highlighted()
	if !ok || row.Header || row.ID == chooserCreateRow {
		return Failed(errors.New("no path is highlighted"))
	}
	p, ok := row.Data.(string)
	if !ok || strings.TrimSpace(p) == "" {
		return Failed(errors.New(row.Label + " is not a path"))
	}
	if !c.r.Settings().Bool(PathFreeVerbsSetting) {
		return Failed(errors.New("the free path verbs are off (setting paths.free-verbs)"))
	}
	p = expandPath(p, "")
	if err := act(p); err != nil {
		return Failed(err)
	}
	return Done(p)
}

// Settled reports whether nothing the picker shows is still loading: its
// source's rows, the highlighted row's preview and the side column. A test
// or a host waits on it after a key before reading the rows (a
// Runtime.PendingCommands loop until it holds).
func (c *Chooser) Settled() bool {
	return !c.loading && !c.Preview.Loading && !c.side.loading
}
