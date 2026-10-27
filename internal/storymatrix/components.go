// Package storymatrix is o-story's coverage model: a component map (which
// framework nouns each component owns, its story areas, neighbours and
// axis values, cmd/o-story/components.json), the actions derived from a
// runtime's binding rows and operations, the cells (action × axis values:
// a full product over the axes that change behaviour most, a
// deterministic pairwise covering array over the rest) and the coverage
// of those cells by the stories' covers patterns. Everything is
// deterministic: the same map, runtime and stories give the same bytes.
package storymatrix

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Map is components.json.
type Map struct {
	About      string             `json:"about,omitempty"`
	Axes       Axes               `json:"axes"`
	Modes      map[string]string  `json:"modes"`    // runtime mode name -> mode axis value
	Channels   map[string]Channel `json:"channels"` // action channel -> the axes its cells vary, each with why
	Exclude    []Exclude          `json:"exclude"`  // impossible combinations for every component
	Components []Component        `json:"components"`
}

// Axes are defined once: their order (the order of a cell id's values),
// the axes taken in full product, and every axis's values.
type Axes struct {
	Order  []string            `json:"order"`
	Full   []string            `json:"full"`
	Values map[string][]string `json:"values"`
}

// Channel says which axes an action channel's cells vary. Every axis of
// the order is in Keep (it changes what the channel does) or in Drop (it
// cannot), each with its reason, so the choice is data. A component's
// override (Component.Channels) names only the axes it moves.
type Channel struct {
	Why  string            `json:"why,omitempty"`
	Keep map[string]string `json:"keep,omitempty"` // axis -> why it changes the channel
	Drop map[string]string `json:"drop,omitempty"` // axis -> why it cannot
}

// channelNames are the channels the map must define: every action
// channel but journeys, which have no axes.
var channelNames = []string{ChannelKey, ChannelPalette, ChannelCLI, ChannelParity, ChannelHosted, ChannelSpill, ChannelHints}

// Exclude is one impossible combination: axis -> value, and why. A cell
// whose values include every pair of an exclude is dropped. Channel, when
// set, scopes the exclude to that channel's actions; a scoped exclude may
// name a single axis (a value the channel can never reach, such as a key
// outcome its guard rules out), an unscoped one names two at least.
type Exclude struct {
	Match   map[string]string
	Channel string
	Why     string
}

func (e *Exclude) UnmarshalJSON(b []byte) error {
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	e.Why, e.Channel = m["why"], m["channel"]
	delete(m, "why")
	delete(m, "channel")
	e.Match = m
	return nil
}

func (e Exclude) MarshalJSON() ([]byte, error) {
	m := map[string]string{"why": e.Why}
	if e.Channel != "" {
		m["channel"] = e.Channel
	}
	for k, v := range e.Match {
		m[k] = v
	}
	return json.Marshal(m)
}

// excludesFor is the excludes that apply to a channel's actions: the
// unscoped ones and the ones scoped to it.
func excludesFor(excludes []Exclude, channel string) []Exclude {
	var out []Exclude
	for _, e := range excludes {
		if e.Channel == "" || e.Channel == channel {
			out = append(out, e)
		}
	}
	return out
}

// Component is one user-facing component.
type Component struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	// Nouns the component owns: its rows and operations are its actions.
	Nouns []string `json:"nouns"`
	// Kinds are instance or modal kinds of the component that are not
	// registered nouns until opened (palette-result-row).
	Kinds []string `json:"kinds,omitempty"`
	// Operations are noun.verb keys the component claims from its noun's
	// owner (tables claims list.column from lists).
	Operations []string `json:"operations,omitempty"`
	// Modes are runtime modes the component owns: every row in them is
	// the component's, whatever noun it is bound on (list_filter).
	Modes []string `json:"modes,omitempty"`
	// Router marks the component whose nouns' rows belong to the
	// component of the operation they run (app's rows: ? is help's).
	Router     bool                `json:"router,omitempty"`
	Areas      []string            `json:"areas"`
	Neighbours []string            `json:"neighbours"`
	Axes       map[string][]string `json:"axes,omitempty"` // restricted values; an axis left out takes every value
	Exclude    []Exclude           `json:"exclude,omitempty"`
	// Channels override the map's channels for this component: an axis
	// in Keep is varied, one in Drop is not, each with its reason.
	Channels map[string]Channel `json:"channels,omitempty"`
	Journeys []Journey          `json:"journeys"`
}

// Journey is a hand-authored multi-step path through the component.
type Journey struct {
	ID    string `json:"id"`
	Steps string `json:"steps,omitempty"`
}

// ParseMap reads components.json, rejecting unknown fields.
func ParseMap(data []byte) (*Map, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Map
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("components.json: %w", err)
	}
	if err := m.check(); err != nil {
		return nil, fmt.Errorf("components.json: %w", err)
	}
	return &m, nil
}

// Component returns the component with the id, or nil.
func (m *Map) Component(id string) *Component {
	for i := range m.Components {
		if m.Components[i].ID == id {
			return &m.Components[i]
		}
	}
	return nil
}

// check validates the map on its own (no runtime): axes, values,
// excludes, unique ids, nouns, claims and neighbours.
func (m *Map) check() error {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	order := map[string]int{}
	for i, a := range m.Axes.Order {
		order[a] = i
		if _, ok := m.Axes.Values[a]; !ok && a != "to" {
			add("axis %q has no values", a)
		}
	}
	for a := range m.Axes.Values {
		if _, ok := order[a]; !ok {
			add("axis %q is not in the order", a)
		}
	}
	for i, a := range m.Axes.Full {
		if order[a] != i {
			add("full axis %q must be at position %d of the order", a, i)
		}
	}
	for mode, v := range m.Modes {
		if !slices.Contains(m.Axes.Values["mode"], v) {
			add("mode %q maps to %q, not a mode axis value", mode, v)
		}
	}
	for _, ch := range slices.Sorted(maps.Keys(m.Channels)) {
		if !slices.Contains(channelNames, ch) {
			add("channel %q is not an action channel", ch)
		}
	}
	for _, ch := range channelNames {
		c, ok := m.Channels[ch]
		if !ok {
			add("channel %s is not defined", ch)
			continue
		}
		if c.Why == "" {
			add("channel %s: no why", ch)
		}
		checkAxes := func(side string, axes map[string]string) {
			for _, a := range slices.Sorted(maps.Keys(axes)) {
				if _, ok := order[a]; !ok {
					add("channel %s %s: unknown axis %q", ch, side, a)
				}
				if axes[a] == "" {
					add("channel %s %s: axis %s without a why", ch, side, a)
				}
			}
		}
		checkAxes("keep", c.Keep)
		checkAxes("drop", c.Drop)
		for _, a := range m.Axes.Order {
			_, kept := c.Keep[a]
			_, dropped := c.Drop[a]
			switch {
			case kept && dropped:
				add("channel %s: axis %s is both kept and dropped", ch, a)
			case !kept && !dropped:
				add("channel %s: axis %s is neither kept nor dropped", ch, a)
			}
		}
	}
	ids := map[string]bool{}
	for _, c := range m.Components {
		if ids[c.ID] {
			add("component %q appears twice", c.ID)
		}
		ids[c.ID] = true
		if c.ID == "" || strings.ContainsAny(c.ID, "/*?[]\\") {
			add("component id %q is empty or has a glob character", c.ID)
		}
	}
	checkExclude := func(where string, e Exclude) {
		if e.Why == "" {
			add("%s: an exclude without a why", where)
		}
		switch {
		case e.Channel != "" && !slices.Contains(channelNames, e.Channel):
			add("%s: exclude channel %q is not an action channel", where, e.Channel)
		case e.Channel != "" && len(e.Match) < 1:
			add("%s: a channel exclude must name an axis", where)
		case e.Channel == "" && len(e.Match) < 2:
			add("%s: an exclude must name at least two axes", where)
		}
		for a, v := range e.Match {
			if a == "to" {
				if !ids[v] {
					add("%s: exclude to=%q is not a component", where, v)
				}
				continue
			}
			if !slices.Contains(m.Axes.Values[a], v) {
				add("%s: exclude %s=%q is not a value", where, a, v)
			}
		}
	}
	for _, e := range m.Exclude {
		checkExclude("exclude", e)
	}
	nouns, ops, modes := map[string]string{}, map[string]string{}, map[string]string{}
	claim := func(seen map[string]string, what, key, comp string) {
		if prev, ok := seen[key]; ok {
			add("%s %q is mapped to %s and %s", what, key, prev, comp)
		}
		seen[key] = comp
	}
	for _, c := range m.Components {
		for _, n := range c.Nouns {
			claim(nouns, "noun", n, c.ID)
		}
		for _, n := range c.Kinds {
			claim(nouns, "noun", n, c.ID)
		}
		for _, o := range c.Operations {
			claim(ops, "operation", o, c.ID)
		}
		for _, md := range c.Modes {
			claim(modes, "mode", md, c.ID)
			if _, ok := m.Modes[md]; !ok {
				add("%s: mode %q is not in modes", c.ID, md)
			}
		}
		for _, n := range c.Neighbours {
			if !ids[n] || n == c.ID {
				add("%s: neighbour %q is not another component", c.ID, n)
			}
		}
		for a, vals := range c.Axes {
			if a == "mode" || a == "to" {
				add("%s: axis %s is derived, not restricted", c.ID, a)
				continue
			}
			if len(vals) == 0 {
				add("%s: axis %s restricted to nothing", c.ID, a)
			}
			for _, v := range vals {
				if !slices.Contains(m.Axes.Values[a], v) {
					add("%s: %s=%q is not a value", c.ID, a, v)
				}
			}
		}
		for _, e := range c.Exclude {
			checkExclude(c.ID+" exclude", e)
		}
		for _, ch := range slices.Sorted(maps.Keys(c.Channels)) {
			o := c.Channels[ch]
			def, ok := m.Channels[ch]
			if !ok {
				add("%s: channel %q is not defined at the top", c.ID, ch)
				continue
			}
			if o.Why != "" {
				add("%s: channel %s: an override's reasons go beside its axes", c.ID, ch)
			}
			sides := map[string][2]map[string]string{"keep": {o.Keep, def.Keep}, "drop": {o.Drop, def.Drop}}
			for _, side := range []string{"keep", "drop"} {
				axes, defaults := sides[side][0], sides[side][1]
				for _, a := range slices.Sorted(maps.Keys(axes)) {
					if _, known := order[a]; !known {
						add("%s: channel %s %s: unknown axis %q", c.ID, ch, side, a)
						continue
					}
					if axes[a] == "" {
						add("%s: channel %s %s: axis %s without a why", c.ID, ch, side, a)
					}
					if _, same := defaults[a]; same {
						add("%s: channel %s already %ss axis %s", c.ID, ch, side, a)
					}
				}
			}
			for _, a := range slices.Sorted(maps.Keys(o.Keep)) {
				if _, both := o.Drop[a]; both {
					add("%s: channel %s: axis %s is both kept and dropped", c.ID, ch, a)
				}
			}
		}
		jids := map[string]bool{}
		for _, j := range c.Journeys {
			if j.ID == "" || jids[j.ID] || strings.ContainsAny(j.ID, "/*?[]\\,=") {
				add("%s: journey id %q is empty, repeated or has a reserved character", c.ID, j.ID)
			}
			jids[j.ID] = true
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// owners indexes the map: noun, claimed operation and owned mode to
// component id.
type owners struct {
	noun, op, mode map[string]string
	router         map[string]bool
}

func (m *Map) owners() owners {
	o := owners{noun: map[string]string{}, op: map[string]string{}, mode: map[string]string{}, router: map[string]bool{}}
	for _, c := range m.Components {
		for _, n := range c.Nouns {
			o.noun[n] = c.ID
		}
		for _, n := range c.Kinds {
			o.noun[n] = c.ID
		}
		for _, op := range c.Operations {
			o.op[op] = c.ID
		}
		for _, md := range c.Modes {
			o.mode[md] = c.ID
		}
		if c.Router {
			o.router[c.ID] = true
		}
	}
	return o
}

// channelAxes are the axes a component's cells of a channel vary, in the
// order: the map's kept axes, moved by the component's override.
func (m *Map) channelAxes(c *Component, channel string) []string {
	def := m.Channels[channel]
	o := c.Channels[channel]
	var out []string
	for _, a := range m.Axes.Order {
		_, kept := def.Keep[a]
		if _, ok := o.Keep[a]; ok {
			kept = true
		}
		if _, ok := o.Drop[a]; ok {
			kept = false
		}
		if kept {
			out = append(out, a)
		}
	}
	return out
}

// values is the component's values of a (restricted) axis, in the
// global order.
func (m *Map) values(c *Component, axis string) []string {
	all := m.Axes.Values[axis]
	keep, ok := c.Axes[axis]
	if !ok {
		return slices.Clone(all)
	}
	var out []string
	for _, v := range all {
		if slices.Contains(keep, v) {
			out = append(out, v)
		}
	}
	return out
}
