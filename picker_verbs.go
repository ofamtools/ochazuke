package ochazuke

import (
	"errors"
	"maps"
	"slices"
)

// One picker noun (pickers PK2): the verbs every open picker answers,
// whichever component draws it. A chooser (OpenPicker, the path picker
// with its find source, a registered picker) implements pickerModal, so
// a key row on ochazuke.picker runs the same verb on any of them.

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
	PickerCompletesGuard    = "picker-completes"     // complete fills the highlighted row in (a source with Complete: the path picker's find)
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

// pickerVerbAliases are the path finder's old verbs (the finder is the
// path picker's find source since v0.96.0), now the picker noun's shared
// ones.
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
		Params(map[string]Parameter{"current": {Type: "boolean", Description: "Take the source's current row instead of the highlight (the path picker's find: the folder it searches)"}}).
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

func (c *Chooser) pickerPick(args Arguments) Result {
	c.sync()
	if current, _ := args["current"].(bool); current {
		return c.pickerCurrent()
	}
	return c.pick()
}

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
	if err := c.complete(); err != nil {
		return Failed(err)
	}
	return Done(c.state())
}

func (c *Chooser) pickerOpens() bool { return c.opens() }

// Enter waits for the rows of the query as typed (pick holds it), so it
// never opens an older query's row.
func (c *Chooser) pickerEnterOpens() bool { return !c.waiting() && c.enterOpens() }
func (c *Chooser) pickerCanUp() bool      { return c.canUp() }
func (c *Chooser) pickerUpEmpty() bool    { return c.canUp() && c.Query() == "" }
func (c *Chooser) pickerCompletes() bool  { return c.completes() }
