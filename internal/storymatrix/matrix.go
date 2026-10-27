package storymatrix

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Cell is one action under one assignment of its axes.
type Cell struct {
	ID        string            `json:"id"`
	Component string            `json:"component"`
	Action    string            `json:"action"`
	Channel   string            `json:"channel"`
	Operation string            `json:"operation,omitempty"`
	Axes      map[string]string `json:"axes,omitempty"`
}

// ComponentCells is one component's actions and cells.
type ComponentCells struct {
	ID      string   `json:"id"`
	Title   string   `json:"title,omitempty"`
	Areas   []string `json:"areas"`
	Actions []Action `json:"actions"`
	Cells   []Cell   `json:"cells"`
}

// Matrix is every component's cells, in the map's order.
type Matrix struct {
	Components []ComponentCells `json:"components"`
}

// Build crosses every component's actions with its axes.
func (m *Map) Build(inv *Inventory) *Matrix {
	mx := &Matrix{}
	for i := range m.Components {
		c := &m.Components[i]
		cc := ComponentCells{ID: c.ID, Title: c.Title, Areas: c.Areas, Actions: inv.Actions[c.ID]}
		if cc.Areas == nil {
			cc.Areas = []string{}
		}
		keyModes := []string{}
		for _, a := range cc.Actions {
			if a.Channel == ChannelKey {
				for _, md := range a.Modes {
					addUnique(&keyModes, md)
				}
			}
		}
		keyModes = m.inAxisOrder("mode", keyModes)
		excludes := append(slices.Clone(m.Exclude), c.Exclude...)
		for _, a := range cc.Actions {
			if a.Channel == ChannelJourney {
				cc.Cells = append(cc.Cells, Cell{ID: c.ID + "/~journey/" + a.Operation, Component: c.ID, Action: a.ID, Channel: a.Channel, Operation: a.Operation})
				continue
			}
			params, full := m.params(c, a, keyModes)
			for _, row := range Combos(params, full, excludes) {
				cell := Cell{Component: c.ID, Action: a.ID, Channel: a.Channel, Operation: a.Operation, Axes: map[string]string{}}
				var parts []string
				for j, p := range params {
					v := p.Values[row[j]]
					cell.Axes[p.Name] = v
					parts = append(parts, p.Name+"="+v)
				}
				tail := strings.Join(parts, ",")
				if tail == "" {
					tail = "default"
				}
				cell.ID = c.ID + "/" + a.ID + "/" + tail
				cc.Cells = append(cc.Cells, cell)
			}
		}
		slices.SortFunc(cc.Cells, func(x, y Cell) int { return strings.Compare(x.ID, y.ID) })
		cc.Cells = slices.CompactFunc(cc.Cells, func(x, y Cell) bool { return x.ID == y.ID })
		mx.Components = append(mx.Components, cc)
	}
	return mx
}

// params are an action's axes in the global order with the values that
// apply to it, and how many lead the order as a full product.
func (m *Map) params(c *Component, a Action, keyModes []string) ([]Param, int) {
	axes := m.Channels[a.Channel]
	var params []Param
	full := 0
	for _, axis := range m.Axes.Order {
		if !slices.Contains(axes, axis) {
			continue
		}
		var vals []string
		switch axis {
		case "to":
			vals = slices.Clone(c.Neighbours)
		case "mode":
			if a.Channel == ChannelKey {
				vals = a.Modes
			} else {
				vals = keyModes
			}
		case "placement":
			vals = m.values(c, axis)
			if a.Channel == ChannelSpill {
				vals = slices.DeleteFunc(vals, func(v string) bool { return v != "page" && v != "split" })
			}
		default:
			vals = m.values(c, axis)
		}
		vals = slices.DeleteFunc(slices.Clone(vals), func(v string) bool { return !m.applies(c, a, axis, v) })
		if len(vals) == 0 {
			continue
		}
		if slices.Contains(m.Axes.Full, axis) {
			full++
		}
		params = append(params, Param{Name: axis, Values: vals})
	}
	return params, full
}

// applies drops the values an action cannot take, from what it is: a
// guard-false outcome needs a guard, a landing or a closed owner needs
// application work, a cancel needs a confirm (or a component that
// cancels), a stale result needs application work, a count needs a verb
// that takes one, dot-repeat a repeatable verb, a macro or a register a
// key.
func (m *Map) applies(c *Component, a Action, axis, v string) bool {
	switch axis + "=" + v {
	case "outcome=guard-false":
		return len(a.Guards) > 0
	case "outcome=pending-lands", "outcome=owner-closed", "state=stale-result":
		return a.Application
	case "outcome=cancelled":
		return a.Confirm || slices.Contains(c.Axes["outcome"], "cancelled")
	case "modifiers=count":
		return a.Count
	case "modifiers=dot-repeat":
		return a.Repeatable
	case "modifiers=in-macro", "modifiers=register":
		return a.Channel == ChannelKey
	}
	return true
}

// Only keeps the named component (every one when id is "").
func (mx *Matrix) Only(id string) (*Matrix, error) {
	if id == "" {
		return mx, nil
	}
	for _, c := range mx.Components {
		if c.ID == id {
			return &Matrix{Components: []ComponentCells{c}}, nil
		}
	}
	var ids []string
	for _, c := range mx.Components {
		ids = append(ids, c.ID)
	}
	return nil, fmt.Errorf("no component %q (have %s)", id, strings.Join(ids, ", "))
}

// Total is the number of cells.
func (mx *Matrix) Total() int {
	n := 0
	for _, c := range mx.Components {
		n += len(c.Cells)
	}
	return n
}

// WriteText prints one cell id per line, by component.
func (mx *Matrix) WriteText(w io.Writer) error {
	for _, c := range mx.Components {
		for _, cell := range c.Cells {
			if _, err := fmt.Fprintln(w, cell.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteJSON prints the matrix as indented JSON.
func (mx *Matrix) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(mx)
}
