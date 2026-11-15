package ochazuke

import (
	"cmp"
	"fmt"
	"strings"
)

// The ochazuke.help-page verbs: every action of the binding help page, from
// its keys (bindings/help-page.json), the palette and the CLI alike. Each
// opens the page first when it is not open (over the focus), so a verb with
// nothing to act on does the nearest thing or says so on the status line;
// none refuses.

// helpPageVerb is one verb of the page: its name, description, parameters
// and what it does on the open page (nil: opening is all it does).
type helpPageVerb struct {
	name, desc string
	params     map[string]Parameter
	run        func(p *HelpPage, a Arguments) error
}

// helpPageVerbs are the page's verbs.
func helpPageVerbs() []helpPageVerb {
	step := map[string]Parameter{"step": {Type: "integer", Description: "1 for the next (the default), -1 for the previous"}}
	return []helpPageVerb{
		{"open", "Open the binding help page walking from the focus (on the page: walk from the page's own focus, its own keys)", nil, nil},
		{"close", "Leave the binding help page for the page it was opened from", nil, func(p *HelpPage, _ Arguments) error { return p.r.closeHelpPage() }},
		{"refocus", "Choose another focus to walk from: every place the application has, filtered as you type", nil, (*HelpPage).pickPlace},
		{"back", "Walk from the previous focus again", nil, func(p *HelpPage, _ Arguments) error { return p.back() }},
		{"cycle-mode", "Walk the stack in the next or previous mode the walk's nouns have rows in", step, (*HelpPage).stepMode},
		{"group", "Group the list by the next of level, noun and verb (by: one of them)", map[string]Parameter{"by": {Type: "string", Enum: groupNames(), Description: "level, noun or verb; default the next"}}, (*HelpPage).regroup},
		{"sort", "Sort inside the groups by the next of walk order, key, verb and noun (by: one of them)", map[string]Parameter{"by": {Type: "string", Enum: sortNames(), Description: "walk, key, verb or noun; default the next"}}, (*HelpPage).resort},
		{"toggle", "Add a filter chip, or remove it when the filter has it (is:unbound, is:shadowed, source:user...)", map[string]Parameter{"term": {Type: "string", Required: true, Description: "The chip, as the filter line writes it"}}, (*HelpPage).toggle},
		{"drop", "Remove a filter chip by its number (default the last)", map[string]Parameter{"nth": {Type: "integer", Description: "The chip's number, 1 first; default the last"}}, (*HelpPage).drop},
		{"clear", "Remove every filter chip", nil, func(p *HelpPage, _ Arguments) error { p.setQuery(HelpQuery{}); return nil }},
		{"choose", "Act on the rail's node: show only a level or table, walk in a mode, walk from a place, or go back", nil, func(p *HelpPage, _ Arguments) error { return p.choose() }},
		{"where", "Show where the selected row is defined and why it has its status (on a note: walk in that mode)", nil, func(p *HelpPage, _ Arguments) error { return p.where() }},
		{"trace", "What the selected row's key does from this focus: every row on the walk that takes it, and why", nil, (*HelpPage).traceRow},
		{"trace-key", "Trace any key or sequence from this focus (key: \"g d\")", map[string]Parameter{"key": {Type: "string", Required: true, Description: "A key or a sequence, keys separated by spaces"}}, (*HelpPage).traceKey},
		{"narrow", "Filter to the selected row's verb on every noun, its key or its noun, or to the rail's level", map[string]Parameter{"field": {Type: "string", Required: true, Enum: narrowFields(), Description: "verb, key, noun or level"}}, (*HelpPage).narrow},
		{"yank", "Copy the selected row as a bindings.json row (for a verb with no key: with a key free here)", nil, func(p *HelpPage, _ Arguments) error { return p.yank() }},
		{"reveal", "Go to the selected row's level in the rail", nil, func(p *HelpPage, _ Arguments) error { return p.reveal() }},
		{"jump", "Move to the next or previous level of the walk, in the rail and the list", step, (*HelpPage).jump},
	}
}

func registerHelpPage(r *Runtime) {
	r.Define(helpPageNoun).About("The binding help page: the walk a key press takes from a focus, as a graph, beside every binding row and verb with no key, each with its status from that focus")
	r.Define(helpRowNoun).About("A line of the binding help page's list: a binding row, a verb with no key, a level's own line or a note")
	r.Define(helpNodeNoun).About("A node of the binding help page's rail: a level of the walk, one of its tables or other modes, a place beside the focus, or the way back")
	for _, v := range helpPageVerbs() {
		v := v
		r.Register(Operation{Key: KeyOf(helpPageNoun, v.name), Description: v.desc, Parameters: v.params, Result: "help-page", HistoryKind: HistoryAction, Agent: AgentAllow,
			Run: func(r *Runtime, c Context, a Arguments) (any, error) {
				p := r.helpPage()
				if v.run == nil || r.Page != HelpPageID {
					if _, err := r.openHelpPage(focusOf(c), ModeState{Name: r.BaseMode()}); err != nil || v.run == nil {
						return p.state(), err
					}
				}
				return p.state(), v.run(p, a)
			}})
	}
}

// HelpPageState is what a help-page verb answers: the focus walked from,
// the levels with their counts, the filter and the arrangement.
type HelpPageState struct {
	Focus   HelpFocus   `json:"focus"`
	Levels  []HelpLevel `json:"levels"`
	Filter  string      `json:"filter"`
	Matched int         `json:"matched"`
	Rows    int         `json:"rows"`
	Group   string      `json:"group"`
	Sort    string      `json:"sort"`
	Detail  []string    `json:"detail,omitempty"`
}

func (p *HelpPage) state() HelpPageState {
	q := p.query()
	return HelpPageState{Focus: p.Current, Levels: p.Walk.Levels, Filter: q.String(), Matched: len(q.Matching(p.Rows)), Rows: len(p.Rows), Group: p.Group, Sort: p.Sort, Detail: p.Detail}
}

// pickPlace opens the chooser of every place; picking one walks from it.
func (p *HelpPage) pickPlace(_ Arguments) error {
	cur := p.Walk.Place
	_, err := p.r.OpenChooser(ChooserSpec{Kind: helpPlaceKind, Title: "Walk from", Keep: []int{0}, Rows: placeChoiceRows(p.r, cur.Noun, &cur, p.Current.Mode), Choose: func(row ChooserRow) Result {
		pick, _ := row.Data.(helpPlaceChoice)
		next := HelpFocus{Mode: cmp.Or(pick.Mode, p.Current.Mode), Place: NounPlace{Noun: pick.Noun}}
		if pick.Place != nil {
			next.Place = *pick.Place
		}
		p.walkTo(next)
		return Done(p.state())
	}})
	return err
}

// back walks from the previous focus; with none it says so.
func (p *HelpPage) back() error {
	n := len(p.Back)
	if n == 0 {
		p.r.UI().Info("No previous focus: this is the first walked from.")
		return nil
	}
	prev := p.Back[n-1]
	keep := p.Back[:n-1]
	p.walkTo(prev)
	p.Back = keep
	p.railNodes()
	return nil
}

// stepMode walks in the next or previous mode the walk's nouns have rows
// in (the resting mode always among them).
func (p *HelpPage) stepMode(a Arguments) error {
	modes := []string{cmp.Or(firstOf(p.r.helpBaseModes()), "normal")}
	for _, v := range p.Rows {
		if v.Level >= 0 && v.Row.Mode != "" && !slicesContains(modes, v.Row.Mode) && p.r.IsBaseMode(v.Row.Mode) {
			modes = append(modes, v.Row.Mode)
		}
	}
	if !slicesContains(modes, p.Walk.Mode) {
		modes = append(modes, p.Walk.Mode)
	}
	next := p.Current
	next.Mode = nextOf(modes, p.Walk.Mode, IntArg(a, "step", 1))
	p.walkTo(next)
	return nil
}

// regroup groups by the named grouping, else the next.
func (p *HelpPage) regroup(a Arguments) error {
	p.Group = choiceOrNext(groupNames(), p.Group, StringArg(a, "by", ""))
	p.arrange()
	p.r.UI().Info("Grouped by " + p.Group + ".")
	return nil
}

// resort sorts by the named sort, else the next.
func (p *HelpPage) resort(a Arguments) error {
	p.Sort = choiceOrNext(sortNames(), p.Sort, StringArg(a, "by", ""))
	p.arrange()
	p.r.UI().Info("Sorted by " + p.Sort + ".")
	return nil
}

// choiceOrNext is want when list holds it, else the one after current.
func choiceOrNext(list []string, current, want string) string {
	if slicesContains(list, want) {
		return want
	}
	return nextOf(list, current, 1)
}

// toggle adds or removes a chip.
func (p *HelpPage) toggle(a Arguments) error {
	p.setQuery(p.query().Toggle(StringArg(a, "term", "")))
	return nil
}

// quoteTerm quotes a chip's value that holds a space.
func quoteTerm(v string) string {
	if strings.ContainsAny(v, " \t") {
		return `"` + v + `"`
	}
	return v
}

// drop removes chip N, default the last; a chip the filter does not have
// is said so.
func (p *HelpPage) drop(a Arguments) error {
	q := p.query()
	n := IntArg(a, "nth", len(q.Terms))
	if n < 1 || n > len(q.Terms) {
		p.r.UI().Info(fmt.Sprintf("There is no chip %d; the filter has %d.", n, len(q.Terms)))
		return nil
	}
	p.setQuery(q.Without(n))
	return nil
}

// helpNodeActions are what choose does on each kind of rail node.
var helpNodeActions = map[HelpNodeKind]func(p *HelpPage, node HelpNode) error{
	HelpNodeLevel: func(p *HelpPage, node HelpNode) error {
		p.setQuery(p.query().Toggle("level:" + quoteTerm(p.Walk.Levels[node.Level].Label())))
		return nil
	},
	HelpNodeTable: func(p *HelpPage, node HelpNode) error {
		p.setQuery(p.query().Toggle("table:" + quoteTerm(node.Table)))
		return nil
	},
	HelpNodeMode: func(p *HelpPage, node HelpNode) error {
		next := p.Current
		next.Mode = node.Mode
		p.walkTo(next)
		return nil
	},
	HelpNodePlace: func(p *HelpPage, node HelpNode) error {
		p.walkTo(HelpFocus{Place: node.Place, Mode: p.Current.Mode})
		return nil
	},
	HelpNodeBack: func(p *HelpPage, _ HelpNode) error { return p.back() },
}

// choose acts on the rail's node; a head opens or closes its places.
func (p *HelpPage) choose() error {
	node, ok := p.selectedNode()
	if !ok {
		p.r.UI().Info("Nothing is selected in the rail.")
		return nil
	}
	if act := helpNodeActions[node.Kind]; act != nil {
		return act(p, node)
	}
	tree := p.r.Tree(helpRailID)
	sel, _ := tree.Selected()
	return tree.SetCollapsed(sel.ID, !tree.Collapsed(sel.ID))
}

// where shows the selected row's definition; on a note it walks in the
// note's mode; on a level's own line it names the chips that hide its rows.
func (p *HelpPage) where() error {
	line, ok := p.selectedLine()
	switch {
	case !ok:
		p.r.UI().Info("No row is selected.")
	case line.Kind == HelpLineNote:
		next := p.Current
		next.Mode = line.Mode
		p.walkTo(next)
	case line.Kind == HelpLineGroup:
		p.Detail = strings.Split(p.query().ExplainText(p.Rows), "\n")
	default:
		p.Detail = line.Row.Where()
	}
	return nil
}

// traceRow traces the selected row's key.
func (p *HelpPage) traceRow(Arguments) error {
	line, ok := p.selectedLine()
	if !ok || !line.IsRow() {
		p.r.UI().Info("Select a row with a key to trace it; T traces a key you type.")
		return nil
	}
	return p.traceKey(Arguments{"key": rowKey(line.Row.Row)})
}

// traceKey traces a key or sequence from the walked focus.
func (p *HelpPage) traceKey(a Arguments) error {
	p.Detail = p.Walk.Trace(StringArg(a, "key", "")).Lines(p.r)
	return nil
}

// helpNarrow is one thing narrow filters to: the chip it adds for the
// selected line (or the rail's level).
type helpNarrow struct {
	Field string
	// Toggles takes the chip out again when the filter has it.
	Toggles bool
	Term    func(p *HelpPage, line HelpLine, ok bool) string
}

// helpNarrows are narrow's fields.
var helpNarrows = []helpNarrow{
	{"verb", false, func(_ *HelpPage, line HelpLine, ok bool) string {
		return rowTerm(line, ok, func(v HelpPageRow) string { return "verb:" + quoteTerm(v.Verb) })
	}},
	{"key", false, func(_ *HelpPage, line HelpLine, ok bool) string {
		return rowTerm(line, ok, func(v HelpPageRow) string {
			if v.Unbound {
				return "is:unbound"
			}
			return "key:" + quoteTerm(v.Key)
		})
	}},
	{"noun", false, func(_ *HelpPage, line HelpLine, ok bool) string {
		return rowTerm(line, ok, func(v HelpPageRow) string { return "noun:" + quoteTerm(v.Noun) })
	}},
	{"level", true, func(p *HelpPage, _ HelpLine, _ bool) string {
		level := p.RailLevel
		if node, ok := p.selectedNode(); ok && node.Level >= 0 {
			level = node.Level
		}
		if level < 0 || level >= len(p.Walk.Levels) {
			level = 0
		}
		return "level:" + quoteTerm(p.Walk.Levels[level].Label())
	}},
}

// narrowFields are narrow's field names.
func narrowFields() []string {
	out := make([]string, len(helpNarrows))
	for i, n := range helpNarrows {
		out[i] = n.Field
	}
	return out
}

// rowTerm is a chip read off the selected row, "" off a row.
func rowTerm(line HelpLine, ok bool, term func(HelpPageRow) string) string {
	if !ok || line.Kind != HelpLineRow {
		return ""
	}
	return term(*line.Row)
}

// narrow adds the chip of the field for the selected line; the level chip
// is taken out again when the filter has it (s l shows every level again).
func (p *HelpPage) narrow(a Arguments) error {
	field := StringArg(a, "field", "")
	line, ok := p.selectedLine()
	for _, n := range helpNarrows {
		if n.Field != field {
			continue
		}
		term := n.Term(p, line, ok)
		switch {
		case term == "":
			p.r.UI().Info("Select a row first.")
		case n.Toggles:
			p.setQuery(p.query().Toggle(term))
		default:
			p.setQuery(p.query().With(term))
		}
		return nil
	}
	p.r.UI().Info("narrow takes " + strings.Join(narrowFields(), ", ") + ".")
	return nil
}

// yank copies the selected row as a bindings.json row.
func (p *HelpPage) yank() error {
	line, ok := p.selectedLine()
	if !ok || line.Kind != HelpLineRow {
		p.r.UI().Info("Nothing to copy on this line; select a row.")
		return nil
	}
	text, err := line.Row.BindingsLine(firstOf(p.Walk.FreeKeys()))
	if err != nil {
		return err
	}
	if err := p.r.storeYank(Yank{Text: text, Shape: "line"}, "", false); err != nil {
		return err
	}
	p.r.UI().Info("Copied the row as a bindings.json row.")
	return nil
}

// reveal moves the focus to the selected row's level in the rail.
func (p *HelpPage) reveal() error {
	level := 0
	if line, ok := p.selectedLine(); ok && line.Level >= 0 {
		level = line.Level
	}
	p.selectLevel(level)
	return p.r.Tree(helpRailID).Focus()
}

// jump moves to the next or previous level, in the rail and the list.
func (p *HelpPage) jump(a Arguments) error {
	level := p.RailLevel
	if p.r.focusInside(helpRowsID) {
		if line, ok := p.selectedLine(); ok && line.Level >= 0 {
			level = line.Level
		}
	}
	if n := len(p.Walk.Levels); n > 0 {
		p.selectLevel(min(max(level+IntArg(a, "step", 1), 0), n-1))
	}
	return nil
}
