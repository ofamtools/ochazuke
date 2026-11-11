package ochazuke

import (
	"slices"
	"strings"
)

// PaletteReachSetting makes the palette offer, and run, every registered
// action from wherever it opened: a verb whose guard is false at the focus
// (its noun is on another page, nothing of its noun is focused) is listed
// like any other and, when chosen, runs at a thing of its noun (reach) after
// asking for the target it lacks (missingTarget). Off keeps the palette to
// the verbs the focus and mode allow.
const PaletteReachSetting = "palette.reach"

// unavailableReason is why op cannot run whatever is chosen or focused: a
// disabled operation, a feature setting that is off, or a noun whose store
// cannot do it. "" when nothing of that kind stops it. The palette shows
// the reason on the verb and still lists it.
func (r *Runtime) unavailableReason(op Operation) string {
	switch {
	case op.Disabled:
		return "disabled"
	case op.EnabledSetting != "" && !r.Settings().Bool(op.EnabledSetting):
		return "turn on the setting " + op.EnabledSetting
	}
	return r.unavailableFor(op, nil)
}

// targetParams are the parameters of op that name a thing of the verb's own
// noun and are filled from the focus when omitted (Parameter.Focus with
// that Noun: a standard verb's subject), in line order: the targets a
// request may need to be given. A reference to another noun filled from
// the focus (a create's project) is optional and not one.
func (r *Runtime) targetParams(op Operation) []string {
	var out []string
	for _, name := range paramOrder(op) {
		if param := op.Parameters[name]; param.Focus && param.Noun != "" && r.CanonicalNoun(param.Noun) == r.CanonicalNoun(op.Noun()) {
			out = append(out, name)
		}
	}
	return out
}

// missingTarget names the first target parameter (targetParams) the line
// leaves unfilled while nothing of its noun is focused to fill it: a
// parameter to ask for with the noun's picker, never a reason to refuse.
// "" when the line has every target or the focus supplies it.
func (p *Palette) missingTarget(c commandLine) string {
	if !c.known() || !p.r.Settings().Bool(PaletteReachSetting) || c.Op.covered(c.Args) {
		return ""
	}
	for _, name := range p.r.targetParams(c.Op) {
		if v, set := c.Args[name]; set && !emptyProp(v) {
			continue
		}
		if _, ok := p.r.sourceValue(c.Op, name, focusOf(p.origin)); ok {
			continue
		}
		if _, ok := p.r.focusValue(c.Op, name, focusOf(p.origin)); ok {
			continue
		}
		return name
	}
	return ""
}

// missingTargets names the target parameters (targetParams) of op that q
// leaves unfilled while nothing of their noun is at focus; the args form
// asks for them with the missing required ones.
func (r *Runtime) missingTargets(op Operation, args Arguments, focus string) []string {
	if op.covered(args) {
		return nil
	}
	var out []string
	for _, name := range r.targetParams(op) {
		if v, set := args[name]; set && !emptyProp(v) {
			continue
		}
		if _, ok := r.focusValue(op, name, focus); !ok {
			out = append(out, name)
		}
	}
	return out
}

// reach gives q a target to run at when its action is guarded off at the
// palette's origin: the nearest thing of its noun on the focus path would
// have answered already, so this is the noun's first live instance (by id)
// where the action is available, or the instance a target parameter names.
// A request that already has a target, or whose action is available at
// the origin, is returned as it is.
func (p *Palette) reach(q Request) Request {
	if q.Target != "" || !p.r.Settings().Bool(PaletteReachSetting) {
		return q
	}
	focus := focusOf(p.origin)
	op, ok := p.r.Operation(q.Key())
	if !ok {
		return q
	}
	if args, err := p.r.parameterSources(op, q, focus); err == nil {
		q.Args = args
	}
	if p.r.availableWithin(q, p.r.rowInstance(level{}, q, focus), p.originMode, nil) {
		return q
	}
	var homes []string
	for _, name := range p.r.targetParams(op) {
		if id := StringArg(q.Args, name, ""); id != "" && p.r.Instances[id] != nil && p.r.Instances[id].Noun == p.r.CanonicalNoun(op.Parameters[name].Noun) {
			homes = append(homes, id)
		}
	}
	noun := p.r.CanonicalNoun(op.Noun())
	var live []string
	for id, inst := range p.r.Instances {
		if inst.Noun == noun {
			live = append(live, id)
		}
	}
	slices.Sort(live)
	for _, id := range append(homes, live...) {
		if p.r.availableWithin(q, id, p.originMode, nil) {
			q.Target = id
			return q
		}
	}
	return q
}

// styleBoard is the board palette style.
func (p *Palette) styleBoard() bool {
	return strings.EqualFold(p.r.Settings().String(PaletteStyleSetting), PaletteStyleBoard)
}

// Palette styles (palette.style).
const (
	PaletteStyleSetting  = "palette.style"
	PaletteStyleClassic  = "classic"
	PaletteStyleBoard    = "board"
	PaletteMarkerSetting = "palette.marker"
	PaletteMarkerLine    = "line"
	PaletteMarkerArrow   = "arrow"
)
