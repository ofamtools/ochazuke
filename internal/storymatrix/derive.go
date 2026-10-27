package storymatrix

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/unixsuperhero/ochazuke"
)

// Action channels.
const (
	ChannelKey     = "key"     // a binding row (rows running one request collapse into one action)
	ChannelPalette = "palette" // the operation typed as a palette line
	ChannelCLI     = "cli"     // the operation as NOUN VERB on the command line
	ChannelParity  = "parity"  // the same verb by key, palette line and CLI agree
	ChannelHosted  = "hosted"  // the component drawn in each placement
	ChannelSpill   = "spill"   // focus spilling into a neighbour
	ChannelHints   = "hints"   // the footer hints and ? help agree with the rows
	ChannelJourney = "journey" // a hand-authored path
)

// Action is one user-facing behaviour of a component.
type Action struct {
	ID          string   `json:"id"`
	Channel     string   `json:"channel"`
	Operation   string   `json:"operation,omitempty"`
	Keys        []string `json:"keys,omitempty"`  // "MODE KEYS" of every row that runs it
	Modes       []string `json:"modes,omitempty"` // mode axis values its rows live in
	Guards      []string `json:"guards,omitempty"`
	Count       bool     `json:"count,omitempty"`       // takes a count
	Repeatable  bool     `json:"repeatable,omitempty"`  // dot-repeat runs it again
	Application bool     `json:"application,omitempty"` // runs off the loop (Pending)
	Confirm     bool     `json:"confirm,omitempty"`
}

// Inventory is every component's actions, from one runtime.
type Inventory struct {
	Actions map[string][]Action // component id -> actions, sorted by id
}

// countSources are the From sources that carry a count.
var countSources = []string{ochazuke.SourceCount, ochazuke.SourceSteps, ochazuke.SourceLine, ochazuke.SourceOpCount}

// countParams are parameter names a count fills.
var countParams = []string{"steps", "count", "lines"}

// Universe is the set of nouns the framework registers in r: every
// defined noun and the noun of every builtin operation, minus the
// fixture's own nouns.
func Universe(r *ochazuke.Runtime, fixture []string) []string {
	set := map[string]bool{}
	for n := range r.Nouns {
		set[n] = true
	}
	for _, op := range r.Operations {
		if op.Builtin {
			set[op.Noun] = true
		}
	}
	for _, n := range fixture {
		delete(set, n)
	}
	return slices.Sorted(maps.Keys(set))
}

// CheckRuntime reports what the map gets wrong about the runtime: a noun
// of the universe mapped to no component, a mapped noun the runtime does
// not have (other than a declared kind), a claimed operation that is not
// registered, a runtime mode with no mode axis value, a mode owned that
// the runtime does not register.
func (m *Map) CheckRuntime(r *ochazuke.Runtime, fixture []string) []string {
	var problems []string
	o := m.owners()
	universe := Universe(r, fixture)
	modes := map[string]bool{}
	for md := range r.Modes {
		modes[md] = true
	}
	if res, err := r.Execute(ochazuke.Request{Noun: "bindings", Verb: "list", Args: ochazuke.Arguments{"all": true}}); err == nil {
		rows, _ := res.([]ochazuke.Binding)
		for _, row := range rows {
			if row.Mode != "" {
				modes[row.Mode] = true // a pending mode is registered by its rows
			}
		}
	}
	for _, n := range universe {
		if o.noun[n] == "" {
			problems = append(problems, fmt.Sprintf("noun %q is mapped to no component", n))
		}
	}
	for _, c := range m.Components {
		for _, n := range c.Nouns {
			if !slices.Contains(universe, n) {
				problems = append(problems, fmt.Sprintf("%s: noun %q is not registered by the framework (a kind belongs in kinds)", c.ID, n))
			}
		}
		for _, op := range c.Operations {
			if _, ok := r.Operations[op]; !ok {
				problems = append(problems, fmt.Sprintf("%s: operation %q is not registered", c.ID, op))
			}
		}
		for _, md := range c.Modes {
			if !modes[md] {
				problems = append(problems, fmt.Sprintf("%s: mode %q is not registered", c.ID, md))
			}
		}
	}
	for md := range modes {
		if _, ok := m.Modes[md]; !ok {
			problems = append(problems, fmt.Sprintf("runtime mode %q has no mode axis value in modes", md))
		}
	}
	sort.Strings(problems)
	return problems
}

// Derive collects every component's actions from r's stored rows
// (bindings list --all) and builtin operations. instanceOwners maps the
// first segment of a fixture instance id to the component that owns its
// instance rows.
func (m *Map) Derive(r *ochazuke.Runtime, instanceOwners map[string]string) (*Inventory, error) {
	o := m.owners()
	res, err := r.Execute(ochazuke.Request{Noun: "bindings", Verb: "list", Args: ochazuke.Arguments{"all": true}})
	if err != nil {
		return nil, fmt.Errorf("bindings list --all: %w", err)
	}
	rows, _ := res.([]ochazuke.Binding)
	opOwner := func(key string, args ochazuke.Arguments) string {
		if c := o.op[key]; c != "" {
			return c
		}
		noun, _, _ := strings.Cut(key, ".")
		if noun == "mode" {
			if target, _ := args["mode"].(string); o.mode[target] != "" {
				return o.mode[target]
			}
		}
		return o.noun[noun]
	}
	byComp := map[string]map[string]*Action{}
	keyed := map[string]bool{} // operations some row runs
	var problems []string
	for _, row := range rows {
		key := row.Request.Key()
		op, ok := r.Operations[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("row %s %s runs unregistered %s", row.Mode, rowKeys(row), key))
			continue
		}
		table, pin := row.Noun, ""
		if row.Instance != "" {
			inst := r.Instances[row.Instance]
			if inst == nil {
				problems = append(problems, fmt.Sprintf("row on unknown instance %q", row.Instance))
				continue
			}
			table = inst.Noun
		}
		if n := r.Nouns[table]; n != nil {
			pin = n.Pin
		}
		tableComp := o.noun[table]
		if row.Instance != "" {
			// A fixture instance's rows are the component that built it
			// (the catalog page's back row sits on a list instance).
			first, _, _ := strings.Cut(row.Instance, "/")
			if c := instanceOwners[first]; c != "" {
				tableComp = c
			}
		}
		var comp string
		switch {
		case o.mode[row.Mode] != "":
			comp = o.mode[row.Mode]
		case o.op[key] != "":
			comp = o.op[key]
		case o.router[tableComp]:
			comp = opOwner(key, row.Request.Args)
		default:
			comp = tableComp
		}
		if comp == "" {
			problems = append(problems, fmt.Sprintf("row %s %s on %s belongs to no component", row.Mode, rowKeys(row), table))
			continue
		}
		mode := row.Mode
		if mode == "" {
			mode = pin
		}
		if mode == "" {
			mode = "normal"
		}
		axisMode, ok := m.Modes[mode]
		if !ok {
			problems = append(problems, fmt.Sprintf("row mode %q has no mode axis value", mode))
			continue
		}
		id := key + argsLabel(row.Request.Args)
		if byComp[comp] == nil {
			byComp[comp] = map[string]*Action{}
		}
		a := byComp[comp][id]
		if a == nil {
			a = newOpAction(r, op, ChannelKey, id)
			byComp[comp][id] = a
		}
		addUnique(&a.Keys, strings.TrimSpace(row.Mode+" "+rowKeys(row)))
		addUnique(&a.Modes, axisMode)
		if strings.Contains(row.Keys, " ") {
			addUnique(&a.Modes, "pending-prefix")
		}
		if row.When != "" {
			addUnique(&a.Guards, row.When)
		}
		for _, v := range row.From {
			for _, s := range strings.Fields(v) {
				if slices.Contains(countSources, s) {
					a.Count = true
				}
			}
		}
		keyed[key] = true
	}
	for _, key := range slices.Sorted(maps.Keys(r.Operations)) {
		op := r.Operations[key]
		if !op.Builtin {
			continue
		}
		comp := opOwner(key, nil)
		if comp == "" {
			problems = append(problems, fmt.Sprintf("operation %s belongs to no component", key))
			continue
		}
		if byComp[comp] == nil {
			byComp[comp] = map[string]*Action{}
		}
		channels := []string{ChannelPalette, ChannelCLI}
		if keyed[key] {
			channels = append(channels, ChannelParity)
		}
		for _, ch := range channels {
			id := ch + ":" + key
			byComp[comp][id] = newOpAction(r, op, ch, id)
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	inv := &Inventory{Actions: map[string][]Action{}}
	for _, c := range m.Components {
		var out []Action
		hasKeys := false
		for _, id := range slices.Sorted(maps.Keys(byComp[c.ID])) {
			a := *byComp[c.ID][id]
			slices.Sort(a.Keys)
			a.Modes = m.inAxisOrder("mode", a.Modes)
			slices.Sort(a.Guards)
			out = append(out, a)
			if a.Channel == ChannelKey {
				hasKeys = true
			}
		}
		out = append(out, Action{ID: "~hosted", Channel: ChannelHosted})
		if len(c.Neighbours) > 0 {
			out = append(out, Action{ID: "~spill", Channel: ChannelSpill})
		}
		if hasKeys {
			out = append(out, Action{ID: "~hints", Channel: ChannelHints})
		}
		for _, j := range c.Journeys {
			out = append(out, Action{ID: "~journey", Channel: ChannelJourney, Operation: j.ID})
		}
		inv.Actions[c.ID] = out
	}
	return inv, nil
}

func newOpAction(r *ochazuke.Runtime, op ochazuke.Operation, channel, id string) *Action {
	a := &Action{ID: id, Channel: channel, Operation: op.Key(), Repeatable: op.Repeatable, Application: op.Kind == ochazuke.KindApplication, Confirm: op.Confirm != ""}
	for _, p := range countParams {
		if _, ok := op.Parameters[p]; ok {
			a.Count = true
		}
	}
	if n := r.Nouns[op.Noun]; n != nil && n.Guards[op.Verb] != "" {
		a.Guards = append(a.Guards, n.Guards[op.Verb])
	}
	return a
}

func (m *Map) inAxisOrder(axis string, vals []string) []string {
	var out []string
	for _, v := range m.Axes.Values[axis] {
		if slices.Contains(vals, v) {
			out = append(out, v)
		}
	}
	return out
}

func addUnique(list *[]string, v string) {
	if !slices.Contains(*list, v) {
		*list = append(*list, v)
	}
}

func rowKeys(b ochazuke.Binding) string {
	if b.Keys != "" {
		return b.Keys
	}
	return "<" + b.Pattern + ">"
}

// argsLabel is a row request's scalar arguments as "(k=v;k=v)", sorted;
// "" when it has none. Values are made glob-safe: an instance id (holding
// a slash) reads <id>, other reserved characters read _.
func argsLabel(args ochazuke.Arguments) string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(args)) {
		var v string
		switch x := args[k].(type) {
		case string:
			switch {
			case x == "":
				v = `""`
			case strings.Contains(x, "/"):
				v = "<id>"
			case x == " ":
				v = "space"
			case x == "\n":
				v = "newline"
			case x == "\t":
				v = "tab"
			default:
				v = x
			}
		case bool, float64, int, int64:
			b, _ := json.Marshal(x)
			v = string(b)
		default:
			continue
		}
		parts = append(parts, k+"="+safe(v))
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, ";") + ")"
}

func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r <= ' ' || strings.ContainsRune("*?[]\\/,=()", r) {
			return '_'
		}
		return r
	}, s)
}
