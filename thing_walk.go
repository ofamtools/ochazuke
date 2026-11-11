package ochazuke

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ThingWalkKind is the standalone relation graph picker's modal kind.
const ThingWalkKind = "thing-walk"

const thingWalkPageSize = 80

// A graph cursor belongs to the source, never to Runtime or PickerRow.Data.
// Frames retain their filter, window and landing row when h retraces a walk.
type thingWalkNode struct {
	kind    int
	noun    string
	key     string
	label   string
	props   Thing
	prop    string
	section string
	page    int
}

const (
	walkNouns = iota
	walkNoun
	walkThing
	walkValue
	walkPage
)

type thingWalkFrame struct {
	node  thingWalkNode
	query string
	pages map[string]int
	rows  []PickerRow
	land  string
}

type thingWalk struct {
	r       *Runtime
	want    Parameter
	origin  Context
	row     func(Choice) PickerRow
	trail   []*thingWalkFrame
	at      int
	landing string
	focused string
}

func registerThingWalk(r *Runtime) {
	r.Define("ochazuke.thing").Verb("walk", func(c Ctx) Result {
		src := r.walkSource(Parameter{}, c.Context, nil)
		_, err := r.OpenPicker(PickerSpec{
			Kind: ThingWalkKind, Title: "Walk", Plural: "things", Sources: []PickerSource{src},
			StartNormal: true, NoSort: true, NoPropFilters: true,
			Choose: func(row PickerRow) Result {
				node := row.walk
				if node == nil {
					return Failed(fmt.Errorf("this row is not a graph thing or value"))
				}
				if node.kind == walkValue {
					n := r.Noun(node.noun)
					args := Arguments{pathPropParam: node.prop}
					if kp := n.keyProp(); kp != nil {
						args[kp.Name] = node.key
					}
					value, err := r.ExecuteAt(c.Context, Request{Noun: node.noun, Verb: "open", Args: args})
					if err != nil {
						return Failed(err)
					}
					return Done(value)
				}
				if err := r.OpenIndex(node.noun, node.key); err != nil {
					return Failed(err)
				}
				return Done(nil)
			},
		})
		if err != nil {
			return Failed(err)
		}
		return Done(nil)
	}, "Walk nouns, their things, values and relations; enter goes to a thing or opens its value").Agent(AgentAllow)
}

// walkSource is the common thing/value subject picker. Fitting rows keep the
// caller's argument and opaque handle; non-fitting things remain traversable.
func (r *Runtime) walkSource(p Parameter, origin Context, row func(Choice) PickerRow) PickerSource {
	w := &thingWalk{r: r, want: p, origin: origin.Clone(), row: row}
	w.trail = []*thingWalkFrame{newWalkFrame(thingWalkNode{kind: walkNouns, label: "Nouns"})}
	if node, ok := r.walkOrigin(origin); ok {
		w.focused = node.noun
		w.trail = append(w.trail, newWalkFrame(thingWalkNode{kind: walkNoun, noun: node.noun, label: r.DisplayNoun(node.noun)}), newWalkFrame(node))
		w.at = 2
	}
	return PickerSource{
		Name: "walk", Live: true, Columns: true, StartNormal: true, Empty: "no things or values here",
		Load: w.load, Open: w.open, Up: w.up, Goto: w.gotoPlace,
		CanOpen: func(row PickerRow) bool { return row.walk != nil && row.walk.kind == walkThing },
		Landing: func() string { return w.landing },
		Label:   w.breadcrumb, Breadcrumb: w.breadcrumb,
		ColumnCount: func() int {
			if _, ok := r.settingValue(WalkColumnsSetting); ok {
				return min(3, max(1, r.Settings().Int(WalkColumnsSetting)))
			}
			return 3
		},
		Side: w.side, Preview: w.preview,
	}
}

func newWalkFrame(node thingWalkNode) *thingWalkFrame {
	return &thingWalkFrame{node: node, pages: map[string]int{}}
}

func (r *Runtime) walkOrigin(origin Context) (thingWalkNode, bool) {
	for _, id := range slices.Backward(origin.Path) {
		inst := r.Instances[id]
		if inst == nil {
			continue
		}
		n := r.Noun(inst.Noun)
		if n == nil || n.Store == nil || n.keyProp() == nil {
			continue
		}
		key, err := r.rowKey(n, inst)
		if err != nil {
			continue
		}
		props := Thing{}
		for _, p := range n.Props {
			if v := r.rowValue(inst, p.Name); v != nil {
				props[p.Name] = v
			}
		}
		label := inst.Label
		if label == "" {
			label = key
		}
		return thingWalkNode{kind: walkThing, noun: n.Name, key: key, label: label, props: props}, true
	}
	return thingWalkNode{}, false
}

func (w *thingWalk) context() Ctx {
	return Ctx{actor: w.r.Actor(), Runtime: w.r, Context: w.origin, Instance: w.r.Instances[focusOf(w.origin)]}
}

func (w *thingWalk) load(query string) Result {
	frame := w.trail[w.at]
	if frame.query != query {
		frame.pages = map[string]int{}
	}
	frame.query = query
	node, pages := frame.node, maps.Clone(frame.pages)
	return w.r.runSteppedBody(w.context(), "ochazuke.thing.walk", func(c Ctx) (any, error) {
		return w.read(c, node, query, pages)
	}, func(value any, err error) (any, error) {
		if err == nil {
			frame.rows, _ = value.([]PickerRow)
		}
		return value, err
	}).OwnedBy("")
}

func (w *thingWalk) read(c Ctx, node thingWalkNode, query string, pages map[string]int) ([]PickerRow, error) {
	switch node.kind {
	case walkNouns:
		return w.nouns(c, query)
	case walkNoun:
		n := w.r.Noun(node.noun)
		c.Noun = n.Name
		q := walkQuery(query, pages[""])
		page, err := w.r.queryThings(c, n, q)
		if err != nil {
			return nil, err
		}
		return w.pageRows(n, page, q, "", ""), nil
	case walkThing:
		return w.related(c, node, query, pages)
	}
	return nil, nil
}

func walkQuery(text string, page int) ListQuery {
	return ListQuery{Text: text, Page: max(1, page), PerPage: thingWalkPageSize, Every: true, Archived: true}
}

func (w *thingWalk) nouns(c Ctx, query string) ([]PickerRow, error) {
	var names []string
	for name, n := range w.r.Nouns {
		if n.Store != nil && n.keyProp() != nil && (n.Store.Query != nil || n.Store.List != nil) {
			names = append(names, name)
		}
	}
	recent := slices.Clone(w.r.chooserMemory().recentNouns)
	rank := func(name string) int {
		if name == w.focused {
			return -2
		}
		if i := slices.Index(recent, name); i >= 0 {
			return i
		}
		return len(recent) + 1
	}
	slices.SortFunc(names, func(a, b string) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	var rows []PickerRow
	for _, name := range names {
		label := w.r.DisplayNoun(name)
		if !TextMatches(Thing{"name": name, "label": label}, []string{"name", "label"}, query) {
			continue
		}
		n := w.r.Noun(name)
		c.Noun = n.Name
		page, err := w.r.queryThings(c, n, ListQuery{Page: 1, PerPage: 1, Every: true, Archived: true})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		if page.Total == 0 {
			continue
		}
		node := &thingWalkNode{kind: walkNoun, noun: name, label: label}
		rows = append(rows, PickerRow{ID: "walk:noun:" + name, Label: fmt.Sprintf("%s (%d)", label, page.Total), Opens: true, NoPick: true, walk: node})
	}
	return rows, nil
}

func (w *thingWalk) pageRows(n *Noun, page ListPage, q ListQuery, section, title string) []PickerRow {
	var rows []PickerRow
	if title != "" && page.Total > 0 {
		rows = append(rows, PickerRow{ID: "walk:head:" + section, Label: fmt.Sprintf("%s (%d)", title, page.Total), Header: true, Section: section})
	}
	if q.Page > 1 {
		rows = append(rows, w.pageRow(section, q.Page-1, "Previous page"))
	}
	for _, t := range page.Items {
		key := fmt.Sprint(t[n.keyProp().Name])
		label := n.displayLabel(func(name string) any { return t[name] })
		if label == "" {
			label = key
		}
		node := &thingWalkNode{kind: walkThing, noun: n.Name, key: key, label: label, props: t}
		id := key
		if w.want.Noun == AnyNoun {
			id = AnyRef(n.Name, key)
		}
		choice := Choice{ID: id, Label: label, Noun: n.Name, Props: t, Data: t}
		row := w.thingRow(choice, node, w.want.Type != PathParameterType && (w.want.Noun == "" || w.want.Noun == AnyNoun || w.r.CanonicalNoun(w.want.Noun) == n.Name))
		row.Section = section
		rows = append(rows, row)
	}
	if q.Page*max(1, q.PerPage) < page.Total {
		rows = append(rows, w.pageRow(section, q.Page+1, "Next page"))
	}
	return rows
}

func (w *thingWalk) pageRow(section string, page int, label string) PickerRow {
	return PickerRow{ID: fmt.Sprintf("walk:page:%s:%d", section, page), Label: label, Section: section, Opens: true, NoPick: true, walk: &thingWalkNode{kind: walkPage, section: section, page: page}}
}

func (w *thingWalk) thingRow(choice Choice, node *thingWalkNode, fits bool) PickerRow {
	row := choice.Row()
	if fits && w.row != nil {
		row = w.row(choice)
	}
	row.walk = node
	row.identity = fmt.Sprintf("%d:%s:%s:%s", node.kind, node.noun, node.key, node.prop)
	row.Noun, row.Props = node.noun, node.props
	row.NoPick, row.Opens = !fits, !fits && node.kind == walkThing
	if fits && (w.want.Noun != "" || w.want.Type == PathParameterType) {
		row.Label = "✓ " + row.Label
		for i := range row.Tones {
			row.Tones[i].From += 2
			if row.Tones[i].To > 0 {
				row.Tones[i].To += 2
			}
		}
		if len(row.Cells) > 0 {
			row.Cells = append([]string{"✓"}, row.Cells...)
		}
	}
	if !fits && node.kind == walkValue {
		row.Refuse = "choose a value of the required kind; other things can still be walked through"
	}
	return row
}

func (w *thingWalk) related(c Ctx, node thingWalkNode, query string, pages map[string]int) ([]PickerRow, error) {
	n := w.r.Noun(node.noun)
	c.Noun = n.Name
	t := node.props
	if n.Store != nil && n.Store.Get != nil {
		got, err := n.Store.Get(c, node.key)
		if err != nil {
			return nil, err
		}
		if got != nil {
			t = got
		}
	}
	rows := w.values(n, node.key, t, query)
	for _, p := range refProps(n) {
		if p.Relation != "" || emptyProp(t[p.Name]) {
			continue // relation-backed refs are read once through Related's reader
		}
		other, key := p.Noun, fmt.Sprint(t[p.Name])
		if other == AnyNoun {
			var ok bool
			if other, key, ok = SplitAnyRef(key); !ok {
				continue
			}
		}
		other = w.r.CanonicalNoun(other)
		target := w.r.Noun(other)
		if target == nil || target.keyProp() == nil {
			continue
		}
		thing := Thing{target.keyProp().Name: key}
		c.Noun = target.Name
		if target.Store != nil && target.Store.Get != nil {
			got, err := target.Store.Get(c, key)
			if err != nil {
				return nil, err
			}
			if got != nil {
				thing = got
			}
		}
		if !TextMatches(thing, textProps(target), query) {
			continue
		}
		section := "out:" + p.Name
		rows = append(rows, w.pageRows(target, ListPage{Items: []Thing{thing}, Total: 1}, walkQuery("", 1), section, titleWord(p.Name))...)
	}
	refs, err := w.r.relatedGraphRefs(node.noun, node.key)
	if err != nil {
		return nil, err
	}
	for _, ref := range refs {
		section := fmt.Sprintf("%s:%s:%t", ref.ID(), ref.Relation, ref.Outward)
		q := walkQuery(query, pages[section])
		page, err := w.r.readRelatedThings(c, RelatedSpec{}, node.noun, node.key, ref, q)
		if err != nil {
			return nil, err
		}
		rows = append(rows, w.pageRows(w.r.Noun(ref.Noun), page, q, section, ref.Label)...)
	}
	return rows, nil
}

func (w *thingWalk) values(n *Noun, key string, t Thing, query string) []PickerRow {
	var rows []PickerRow
	for _, p := range openProps(n) {
		if emptyProp(t[p.Name]) {
			continue
		}
		value := fmt.Sprint(t[p.Name])
		label := p.Name + ": " + value
		if !TextMatches(Thing{"value": label}, []string{"value"}, query) {
			continue
		}
		if len(rows) == 0 {
			rows = append(rows, PickerRow{ID: "walk:values", Label: "Values", Header: true, Section: "values"})
		}
		node := &thingWalkNode{kind: walkValue, noun: n.Name, key: key, prop: p.Name, label: label, props: t}
		fits := w.want.Noun == "" && w.want.Type == ""
		if w.want.Type == PathParameterType && p.IsPath() {
			fits = w.want.Path == PathAny || (w.want.Path == PathDir) == (p.Path == PathDir)
		}
		row := w.thingRow(Choice{ID: value, Label: label, Noun: n.Name, Props: t, Data: value, RowFlags: RowFlags{Bare: true}}, node, fits)
		row.Section = "values"
		rows = append(rows, row)
	}
	return rows
}

func (w *thingWalk) open(row PickerRow) (string, bool) {
	if row.walk == nil {
		return "", false
	}
	frame := w.trail[w.at]
	node := *row.walk
	if node.kind == walkPage {
		frame.pages[node.section] = node.page
		w.landing = ""
		return frame.query, true
	}
	if node.kind != walkNoun && node.kind != walkThing {
		return "", false
	}
	frame.land = "row:" + pickerRowKey(row)
	w.trail = append(w.trail[:w.at+1], newWalkFrame(node))
	w.at++
	w.landing = ""
	memory := w.r.chooserMemory()
	memory.recentNouns = append([]string{node.noun}, slices.DeleteFunc(slices.Clone(memory.recentNouns), func(name string) bool { return name == node.noun })...)
	memory.recentNouns = memory.recentNouns[:min(20, len(memory.recentNouns))]
	return "", true
}

func (w *thingWalk) up(_ string) (string, bool) {
	if w.at == 0 {
		return "", false
	}
	w.at--
	frame := w.trail[w.at]
	w.landing = frame.land
	return frame.query, true
}

func (w *thingWalk) gotoPlace(to string) (string, bool) {
	places := []string{"first-col", "last-col", "~", "/", "own"}
	index := slices.Index(places, to)
	if index < 0 {
		return "", false
	}
	w.at = 0
	if index == 1 {
		w.at = len(w.trail) - 1
	}
	frame := w.trail[w.at]
	w.landing = frame.land
	return frame.query, true
}

func (w *thingWalk) breadcrumb() string {
	parts := make([]string, w.at+1)
	for i := range parts {
		parts[i] = w.trail[i].node.label
	}
	return strings.Join(parts, " › ")
}

func (w *thingWalk) side() (string, func() Result) {
	at := w.at
	if at == 0 {
		return "walk:root", func() Result { return Done(PickerSide{Title: "Nouns"}) }
	}
	parent, child := w.trail[at-1], w.trail[at].node
	node, query, pages := parent.node, parent.query, maps.Clone(parent.pages)
	return fmt.Sprintf("%d:%s:%s:%s:%s:%s", at, node.noun, node.key, child.noun, child.key, query), func() Result {
		if parent.rows != nil {
			return Done(walkSideRows(parent.rows, node, child))
		}
		return w.r.runSteppedBody(w.context(), "ochazuke.thing.walk", func(c Ctx) (any, error) {
			return w.read(c, node, query, pages)
		}, func(value any, err error) (any, error) {
			if err != nil {
				return nil, err
			}
			rows, _ := value.([]PickerRow)
			return walkSideRows(rows, node, child), nil
		}).OwnedBy("")
	}
}

func walkSideRows(rows []PickerRow, parent, child thingWalkNode) PickerSide {
	side := PickerSide{Title: parent.label}
	for _, row := range rows {
		side.Lines = append(side.Lines, row.Label)
		if row.walk != nil && row.walk.noun == child.noun && row.walk.key == child.key && row.walk.kind == child.kind {
			side.At = row.Label
			side.AtRow = len(side.Lines)
		}
	}
	return side
}

func (w *thingWalk) preview(row PickerRow) Result {
	if row.walk == nil || row.walk.kind == walkPage {
		return Done("")
	}
	node := *row.walk
	if node.kind == walkValue {
		return Done(node.label)
	}
	return w.r.runSteppedBody(w.context(), "ochazuke.thing.walk", func(c Ctx) (any, error) {
		return w.read(c, node, "", nil)
	}, func(value any, err error) (any, error) {
		if err != nil {
			return nil, err
		}
		rows, _ := value.([]PickerRow)
		lines := []string{node.label}
		for _, row := range rows {
			lines = append(lines, row.Label)
		}
		return strings.Join(lines, "\n"), nil
	}).OwnedBy("")
}
