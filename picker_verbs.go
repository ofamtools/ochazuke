package ochazuke

import (
	"errors"
	"maps"
	"path/filepath"
	"slices"
)

// One picker noun (pickers PK2): the verbs every open picker answers,
// whichever component draws it. A chooser (OpenPicker, the path picker,
// a registered picker) and the path finder (NewPicker, until it becomes
// the path picker's find source) both implement pickerModal, so a key row
// on ochazuke.picker runs the same verb on either.

// pickerModal is what the picker noun's shared verbs act on.
type pickerModal interface {
	pickerPick(args Arguments) Result
	pickerCancel() Result
	pickerMove(delta int, to string) Result
	pickerInto() Result
	pickerUp() Result
	pickerEnter() Result
	pickerComplete() Result
	// The capabilities the verbs' guards read.
	pickerOpens() bool      // the highlighted row can be browsed into
	pickerEnterOpens() bool // enter browses into it rather than picking it
	pickerCanUp() bool      // the picker can step back out
	pickerUpEmpty() bool    // it can, and its query is empty (backspace)
	pickerCompletes() bool  // complete fills the highlighted row into the query
}

// The picker guards: the rows of bindings/picker.json name them. Each
// old chooser-* name is registered too for one release (Keep actions).
const (
	PickerSourcesGuard      = "picker-sources"       // more than one source
	PickerOpensGuard        = "picker-opens"         // the highlighted row opens
	PickerEnterOpensGuard   = "picker-enter-opens"   // enter opens it (PickerEnterSetting, or a row that only opens)
	PickerUpGuard           = "picker-up"            // the picker can step up
	PickerUpEmptyGuard      = "picker-up-empty"      // it can, and the query is empty (backspace)
	PickerCompletesGuard    = "picker-completes"     // complete fills the highlighted row in (the path finder)
	PickerNormalModeGuard   = "picker-normal-mode"   // the picker has a normal mode (ChooserSpec.NormalMode)
	PickerPropControlsGuard = "picker-prop-controls" // prop conditions or a saved filter are set
)

// Deprecated: the old guard names, kept for one release.
const (
	ChooserSourcesGuard    = "chooser-sources"
	ChooserOpensGuard      = "chooser-opens"
	ChooserEnterOpensGuard = "chooser-enter-opens"
	ChooserUpGuard         = "chooser-up"
	ChooserUpEmptyGuard    = "chooser-up-empty"
)

// pickerGuardAliases are the old guard names, each the new guard's.
var pickerGuardAliases = map[string]string{
	ChooserSourcesGuard:     PickerSourcesGuard,
	ChooserOpensGuard:       PickerOpensGuard,
	ChooserEnterOpensGuard:  PickerEnterOpensGuard,
	ChooserUpGuard:          PickerUpGuard,
	ChooserUpEmptyGuard:     PickerUpEmptyGuard,
	"chooser-normal-mode":   PickerNormalModeGuard,
	"chooser-prop-controls": PickerPropControlsGuard,
}

// pickerRename is the old chooser noun as a deprecated name of the picker
// noun, and the old verbs whose names changed (chooser.open browses into
// a row: picker.into, since picker.open opens a picker by name).
var pickerRename = NounRename{
	From: "ochazuke.chooser", To: PickerNoun, Since: "v0.96.0", RemoveAfter: "v0.97.0",
	Verbs: map[string]string{"open": "into"},
}

// pickerVerbAliases are the path finder's old verbs, now the picker
// noun's shared ones.
var pickerVerbAliases = map[string]string{
	"choose":  "pick",
	"close":   "cancel",
	"descend": "into",
	"parent":  "up",
}

// pickerAt is the picker inst belongs to: inst's own component (the path
// finder's instance), its parent's (a chooser's search input), else the
// top modal's; nil when none is a picker.
func (r *Runtime) pickerAt(inst *Instance) pickerModal {
	if inst != nil {
		if m, ok := inst.Component.(pickerModal); ok {
			return m
		}
		if parent := r.Instances[inst.Parent]; parent != nil {
			if m, ok := parent.Component.(pickerModal); ok {
				return m
			}
		}
	}
	if top := r.Instances[r.TopModal()]; top != nil {
		if m, ok := top.Component.(pickerModal); ok {
			return m
		}
	}
	return nil
}

// registerPickerVerbs registers the shared verbs and their guards.
func registerPickerVerbs(r *Runtime) {
	verb := func(fn func(pickerModal, Ctx) Result) AppHandler {
		return func(ctx Ctx) Result {
			m := r.pickerAt(ctx.Instance)
			if m == nil {
				return Failed(errors.New("no picker is open"))
			}
			return fn(m, ctx)
		}
	}
	r.Define(PickerNoun).Pin("insert").
		Verb("pick", verb(func(m pickerModal, ctx Ctx) Result { return m.pickerPick(ctx.Args) }), "Take the highlighted row, else the top match, else create from the query").
		Params(map[string]Parameter{"current": {Type: "boolean", Description: "The path finder: choose its current folder instead of the highlight"}}).
		Verb("move", verb(func(m pickerModal, ctx Ctx) Result {
			return m.pickerMove(IntArg(ctx.Args, "delta", 0), StringArg(ctx.Args, "to", ""))
		}), "Move the highlight; from none, down selects first and up follows picker.up-from-none").Motion().Params(map[string]Parameter{"delta": {Type: "integer"}, "to": {Type: "string", Enum: []string{"first", "last"}}}).
		Verb("cancel", verb(func(m pickerModal, _ Ctx) Result { return m.pickerCancel() }), "Close without choosing").History(HistoryAction).
		Verb("into", verb(func(m pickerModal, _ Ctx) Result { return m.pickerInto() }), "Browse into the highlighted row inside its source (a folder)").When(PickerOpensGuard).
		Verb("up", verb(func(m pickerModal, _ Ctx) Result { return m.pickerUp() }), "Step the source back out (the parent folder)").When(PickerUpGuard).
		Verb("enter", verb(func(m pickerModal, _ Ctx) Result { return m.pickerEnter() }), "Browse into the highlighted row when it opens, else pick it").History(HistoryAction).
		Verb("complete", verb(func(m pickerModal, _ Ctx) Result { return m.pickerComplete() }), "Complete the highlighted path into the query; nothing is confirmed").When(PickerCompletesGuard).History(HistoryAction)
	at := func(ev KeyEvent) pickerModal { return r.pickerAt(r.Instances[ev.Focus]) }
	guards := map[string]func(pickerModal) bool{
		PickerOpensGuard:      pickerModal.pickerOpens,
		PickerEnterOpensGuard: pickerModal.pickerEnterOpens,
		PickerUpGuard:         pickerModal.pickerCanUp,
		PickerUpEmptyGuard:    pickerModal.pickerUpEmpty,
		PickerCompletesGuard:  pickerModal.pickerCompletes,
	}
	for name, test := range guards {
		r.DefineGuard(name, func(r *Runtime, ev KeyEvent) bool {
			m := at(ev)
			return m != nil && test(m)
		})
	}
}

// registerPickerAliases keeps the old names one release (Keep actions):
// the path finder's old verbs and the chooser noun with its renamed verb.
// It runs once settings exist, as registerModalAliases does: an alias's
// checks read the noun display setting.
func registerPickerAliases(r *Runtime) {
	for _, old := range slices.Sorted(maps.Keys(pickerVerbAliases)) {
		if err := r.AddActionAlias(ActionAlias{Alias: PickerNoun + "." + old, Action: PickerNoun + "." + pickerVerbAliases[old], Since: pickerRename.Since, RemoveAfter: pickerRename.RemoveAfter}); err != nil {
			panic(err)
		}
	}
	if err := r.RenameNoun(pickerRename); err != nil {
		panic(err)
	}
}

// registerPickerGuardAliases gives each old guard name its new guard's
// test, once every picker guard is defined.
func registerPickerGuardAliases(r *Runtime) {
	for old, name := range pickerGuardAliases {
		if fn, ok := r.guards[name]; ok {
			r.DefineGuard(old, fn)
		}
	}
}

// The chooser's pickerModal.

func (c *Chooser) pickerPick(Arguments) Result { c.sync(); return c.pick() }

func (c *Chooser) pickerCancel() Result {
	c.sync()
	if err := c.close(); err != nil {
		return Failed(err)
	}
	return Done(nil)
}

// pickerMoveDelta is move's step over n rows: to first or last is a step
// past either end, else delta.
func pickerMoveDelta(delta int, to string, n int) int {
	switch to {
	case "first":
		return -n
	case "last":
		return n
	}
	return delta
}

func (c *Chooser) pickerMove(delta int, to string) Result {
	c.sync()
	c.move(pickerMoveDelta(delta, to, len(c.matches)))
	return Done(c.state())
}

func (c *Chooser) pickerInto() Result {
	if err := c.open(); err != nil {
		return Failed(err)
	}
	return Done(c.state())
}

func (c *Chooser) pickerUp() Result {
	if err := c.up(); err != nil {
		return Failed(err)
	}
	return Done(c.state())
}

func (c *Chooser) pickerEnter() Result {
	if c.pickerEnterOpens() {
		return c.pickerInto()
	}
	return c.pickerPick(nil)
}

func (c *Chooser) pickerComplete() Result {
	return Failed(errors.New("this picker has nothing to complete"))
}

func (c *Chooser) pickerOpens() bool { return c.opens() }

// Enter waits for the rows of the query as typed (pick holds it), so it
// never opens an older query's row.
func (c *Chooser) pickerEnterOpens() bool { return !c.waiting() && c.enterOpens() }
func (c *Chooser) pickerCanUp() bool      { return c.canUp() }
func (c *Chooser) pickerUpEmpty() bool    { return c.canUp() && c.Query() == "" }
func (c *Chooser) pickerCompletes() bool  { return false }

// The path finder's pickerModal: its old verbs (choose, close, descend,
// parent, enter, complete) as the shared ones.

// result is a finder verb's answer: its error is the finder's error line.
func (p *PathFinder) result(v any, err error) Result {
	if err != nil {
		p.Error = err.Error()
	}
	p.sync()
	if err != nil {
		return Failed(err)
	}
	return Done(v)
}

func (p *PathFinder) pickerPick(args Arguments) Result {
	if err := p.require(); err != nil {
		return p.result(nil, err)
	}
	if current, _ := args["current"].(bool); current {
		return p.result(p.choose(p.Directory))
	}
	entry, err := p.selected()
	if err != nil {
		return p.result(nil, err)
	}
	return p.result(p.choose(filepath.Join(p.Directory, filepath.FromSlash(entry.Path))))
}

func (p *PathFinder) pickerCancel() Result {
	if err := p.require(); err != nil {
		return p.result(nil, err)
	}
	p.stop()
	if err := p.Runtime.Pop(); err != nil {
		return p.result(nil, err)
	}
	return p.result(p.Runtime.Context(), nil)
}

func (p *PathFinder) pickerMove(delta int, to string) Result {
	if err := p.require(); err != nil {
		return p.result(nil, err)
	}
	delta = pickerMoveDelta(delta, to, len(p.Entries))
	p.Selected = min(max(0, p.Selected+delta), max(0, len(p.Entries)-1))
	return p.result(p.state(), nil)
}

func (p *PathFinder) pickerInto() Result  { return p.result(p.enter(true)) }
func (p *PathFinder) pickerEnter() Result { return p.result(p.enter(false)) }

func (p *PathFinder) pickerUp() Result {
	if err := p.require(); err != nil {
		return p.result(nil, err)
	}
	return p.result(p.browse(filepath.Dir(p.Directory)))
}

func (p *PathFinder) pickerComplete() Result {
	if err := p.require(); err != nil {
		return p.result(nil, err)
	}
	entry, err := p.selected()
	if err != nil {
		return p.result(nil, err)
	}
	path := filepath.Join(p.Directory, filepath.FromSlash(entry.Path))
	if entry.Dir {
		return p.result(p.browse(path))
	}
	if _, err := p.Runtime.Execute(Request{Verb: "replace", Noun: "ochazuke.input", Target: p.input.ID, Args: Arguments{"start": 0, "end": len(p.input.Text), "text": path}}); err != nil {
		return p.result(nil, err)
	}
	return p.result(p.state(), nil)
}

// pickerOpens: the highlighted entry is a folder.
func (p *PathFinder) pickerOpens() bool {
	entry, err := p.selected()
	return p.IsOpen() && err == nil && entry.Dir
}

func (p *PathFinder) pickerEnterOpens() bool { return p.pickerOpens() }
func (p *PathFinder) pickerCanUp() bool      { return p.IsOpen() }
func (p *PathFinder) pickerUpEmpty() bool    { return false }
func (p *PathFinder) pickerCompletes() bool  { return p.IsOpen() }
