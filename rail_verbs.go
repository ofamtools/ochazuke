package ochazuke

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The ochazuke.rail verbs: the rail's visibility, look and width (each a
// write of its setting), the focus into it, a pinned thing, a reread, and
// what it lists for an agent or a script (rows). Agents may run them all:
// they read or change view state. Its sections keep the
// ochazuke.related-section verbs and its rows their own nouns'.

// railWidthStep is how far one grow or shrink moves rail.width: percent
// points of a percent width, cells of a cell width.
const (
	railWidthStepPercent = 5
	railWidthStepCells   = 4
)

func registerRail(r *Runtime) {
	b := r.Define(RailNoun).Region().About("The rail: a region beside or over a page's main area that lists what relates to the thing the page shows, as folded sections with counts; its rows are their nouns' own rows")
	railParam := map[string]Parameter{"rail": {Type: "string", Named: true, Noun: RailNoun, Description: "The rail (default: the one at the focus, else the first)"}}
	with := func(extra map[string]Parameter) map[string]Parameter {
		out := map[string]Parameter{}
		for k, v := range railParam {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	verb := func(name, desc string, params map[string]Parameter, run func(r *Runtime, c Context, a Arguments, rl *Rail) (any, error)) {
		b.Verb(name, func(r *Runtime, c Context, a Arguments) (any, error) {
			rl, err := railOf(r, c, StringArg(a, "rail", ""))
			if err != nil {
				return nil, err
			}
			return run(r, c, a, rl)
		}, desc).Params(with(params))
	}
	show := func(on bool) func(*Runtime, Context, Arguments, *Rail) (any, error) {
		return func(r *Runtime, _ Context, _ Arguments, rl *Rail) (any, error) {
			return rl.state(), r.Settings().Set(RailShowSetting, on)
		}
	}
	verb("show", "Show the rail (rail.show on)", nil, show(true))
	verb("hide", "Hide the rail (rail.show off): nothing is drawn or read; a focus inside moves back to the main", nil, show(false))
	verb("toggle", "Show the rail, or hide it", nil, func(r *Runtime, c Context, a Arguments, rl *Rail) (any, error) {
		return show(!rl.Shown())(r, c, a, rl)
	})
	verb("enter", "Move the focus into the rail (its first section's selected row), showing it first when it is hidden", nil, func(r *Runtime, _ Context, _ Arguments, rl *Rail) (any, error) {
		if !rl.Shown() {
			if err := r.Settings().Set(RailShowSetting, true); err != nil {
				return nil, err
			}
		}
		return rl.state(), rl.stack.Focus()
	})
	verb("look", "Set the rail's look: full (a column that narrows the main) or float (a panel over the main's corner)", map[string]Parameter{
		"look": {Type: "string", Required: true, Enum: []string{RailLookFull, RailLookFloat}, Description: "full or float"},
	}, func(r *Runtime, _ Context, a Arguments, rl *Rail) (any, error) {
		return rl.state(), r.Settings().Set(RailLookSetting, StringArg(a, "look", ""))
	})
	verb("resize", "Set the rail's width: cells (36) or a percent of the area (25%)", map[string]Parameter{
		"width": {Type: "string", Required: true, Description: "Cells, or a percent ending in %"},
	}, func(r *Runtime, _ Context, a Arguments, rl *Rail) (any, error) {
		w := strings.TrimSpace(StringArg(a, "width", ""))
		if _, err := parseRailWidth(w); err != nil {
			return nil, err
		}
		return rl.state(), r.Settings().Set(RailWidthSetting, w)
	})
	steps := map[string]Parameter{"steps": {Type: "integer", Description: fmt.Sprintf("How many steps (%d percent points, or %d cells; the count; default 1)", railWidthStepPercent, railWidthStepCells)}}
	resize := func(sign int) func(*Runtime, Context, Arguments, *Rail) (any, error) {
		return func(r *Runtime, _ Context, a Arguments, rl *Rail) (any, error) {
			next := stepRailWidth(r.Settings().String(RailWidthSetting), sign*max(1, IntArg(a, "steps", 1)))
			return rl.state(), r.Settings().Set(RailWidthSetting, next)
		}
	}
	verb("grow", "Widen the rail by a step (writes rail.width)", steps, resize(1))
	verb("shrink", "Narrow the rail by a step (writes rail.width)", steps, resize(-1))
	verb("refresh", "Read every section of the rail again, the ones of nouns without a store of their own included", nil, func(_ *Runtime, _ Context, _ Arguments, rl *Rail) (any, error) {
		rl.stack.Refill()
		rl.stack.read = rl.stack.now()
		return rl.state(), nil
	})
	thing := map[string]Parameter{"thing": {Type: "string", Description: "The thing, noun:key (default: the thing the rail shows)"}}
	verb("inspect", "The rail's state (shown, look, width, the pinned thing, the thing it lists) and its sections with their things, as it lists them for a thing (default the one it shows): what the owner sees, for an agent or a script", thing, func(_ *Runtime, _ Context, a Arguments, rl *Rail) (any, error) {
		return rl.inspect(StringArg(a, "thing", ""))
	})
	verb("pin", "Make the rail list a fixed thing instead of the one the page shows", map[string]Parameter{
		"thing": {Type: "string", Required: true, Description: "The thing, noun:key"},
	}, func(r *Runtime, _ Context, a Arguments, rl *Rail) (any, error) {
		t := StringArg(a, "thing", "")
		if noun, _, ok := SplitAnyRef(t); !ok || r.Noun(noun) == nil {
			return nil, fmt.Errorf("thing %q is not noun:key of a known noun", t)
		}
		rl.pinned = t
		return rl.state(), nil
	})
	verb("unpin", "Make the rail follow the thing the page shows again", nil, func(_ *Runtime, _ Context, _ Arguments, rl *Rail) (any, error) {
		rl.pinned = ""
		return rl.state(), nil
	})
	verb("yank", "Yank the rail's state and sections (inspect's answer, as JSON) into the active register", nil, func(r *Runtime, _ Context, _ Arguments, rl *Rail) (any, error) {
		state, err := rl.inspect("")
		if err != nil {
			return nil, err
		}
		text, err := json.Marshal(state)
		if err != nil {
			return nil, err
		}
		return state, r.StoreYank(Yank{Text: string(text), Shape: "line"}, "", false)
	})
}

// railOf is the rail a verb acts on: the one named, else the one at the
// focus, else the first registered.
func railOf(r *Runtime, c Context, id string) (*Rail, error) {
	if id != "" {
		if rl := r.Rail(id); rl != nil {
			return rl, nil
		}
		return nil, fmt.Errorf("unknown rail %q", id)
	}
	if rl := nearestComponent[*Rail](r, focusOf(c)); rl != nil {
		return rl, nil
	}
	var first *Rail
	var seq uint64
	for _, inst := range r.Instances {
		if rl, ok := inst.Component.(*Rail); ok && (first == nil || inst.seq < seq) {
			first, seq = rl, inst.seq
		}
	}
	if first == nil {
		return nil, errors.New("no rail")
	}
	return first, nil
}

// parseRailWidth reads a width setting value: cells, or a percent.
func parseRailWidth(w string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSuffix(w, "%"))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("width %q is not cells (36) or a percent (25%%)", w)
	}
	return n, nil
}

// stepRailWidth moves a width setting value by steps: percent points of
// a percent (kept to 10..40), cells of a cell width (kept to 16..120).
func stepRailWidth(w string, steps int) string {
	w = strings.TrimSpace(w)
	n, err := parseRailWidth(w)
	if strings.HasSuffix(w, "%") || err != nil {
		if err != nil {
			n = 25
		}
		return strconv.Itoa(min(max(n+steps*railWidthStepPercent, 10), 40)) + "%"
	}
	return strconv.Itoa(min(max(n+steps*railWidthStepCells, PaneMinWidth), 120))
}

// state is what the rail's verbs answer: the settings it draws by, the
// pinned thing, the thing it lists, and its sections in display order.
func (rl *Rail) state() map[string]any {
	r := rl.r
	out := map[string]any{"id": rl.id, "show": rl.Shown(), "look": r.Settings().String(RailLookSetting),
		"width": r.Settings().String(RailWidthSetting), "pinned": rl.pinned}
	if !rl.Shown() {
		return out // off reads nothing
	}
	noun, key := rl.stack.target()
	out["noun"], out["key"] = noun, key
	rl.stack.sync()
	var sections []RelatedSection
	for _, s := range rl.stack.order() {
		var section RelatedSection
		if err := s.thing().Into(&section); err == nil {
			sections = append(sections, section)
		}
	}
	out["sections"] = sections
	return out
}

// inspect is what inspect answers: the state, and each section in display
// order with its things (folded ones too), for thing (noun:key) or the
// thing shown. Off, it reads nothing and lists no section.
func (rl *Rail) inspect(thing string) (map[string]any, error) {
	if !rl.Shown() {
		return rl.state(), nil
	}
	rows, err := rl.rows(thing)
	if err != nil {
		return nil, err
	}
	state := rl.state()
	state["rows"] = rows
	return state, nil
}

// rows are inspect's sections with their things.
func (rl *Rail) rows(thing string) ([]map[string]any, error) {
	if thing != "" {
		if _, _, ok := SplitAnyRef(thing); !ok {
			return nil, fmt.Errorf("thing %q is not noun:key", thing)
		}
		was := rl.pinned
		rl.pinned = thing
		defer func() { rl.pinned = was; rl.stack.sync() }()
	}
	rel := rl.stack
	rel.sync()
	out := []map[string]any{}
	for _, s := range rel.order() {
		if rel.hiddenBy(s.ref.ID()) != "" {
			continue
		}
		row := map[string]any{"id": s.ref.ID(), "label": s.ref.Label, "count": s.count, "noun": s.ref.Noun}
		if s.group != nil {
			row["group"] = s.group.ref.ID()
		}
		if !s.grouping {
			things := s.shown
			if things == nil {
				things = []Thing{}
			}
			row["things"] = things
		}
		out = append(out, row)
	}
	return out, nil
}
