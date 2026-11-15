package ochazuke

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The binding help page: ? opens it (help.style page, the default) over the
// focus it was pressed at. A narrow rail on the left draws the walk as a
// graph, the focused thing first and app last, each level opening onto its
// tables, its other modes and the places beside the focus; the rest of the
// width lists every binding row of the program and every verb with no key,
// each with its status from the focus. The page is a thin layer: the walk,
// the rows, the query, the arrangement and the traces are values
// (help_walk.go, help_rows.go, help_query.go, help_display.go), and the
// page places their answers. Its keys are rows of bindings/help-page.json,
// its actions the ochazuke.help-page verbs. help.style modal keeps the ?
// modal (help.go) exactly as it was.

// HelpPageID is the binding help page; its rail is a tree, its rows a list.
const (
	HelpPageID    = "__binding-help"
	helpPageTitle = "Binding help"
	helpPageNoun  = "ochazuke.help-page"
	helpRailID    = HelpPageID + "/rail"
	helpRowsID    = HelpPageID + "/rows"
	helpRowNoun   = "ochazuke.help-row"
	helpNodeNoun  = "ochazuke.help-node"
	helpStateID   = "__binding-help-state"
)

// HelpStyleSetting chooses what ? opens: page (the binding help page, the
// default) or modal (the ? modal as it was).
const HelpStyleSetting = "help.style"

// The help.style values.
const (
	HelpStylePage  = "page"
	HelpStyleModal = "modal"
)

// HelpFocus is one focus the page walked from: a live instance (Focus) or
// a place, in a mode.
type HelpFocus struct {
	Focus string    `json:"focus,omitempty"`
	Place NounPlace `json:"place"`
	Mode  string    `json:"mode"`
}

// same reports whether two foci are one: the instance, the place and the
// mode.
func (f HelpFocus) same(g HelpFocus) bool {
	return f.Focus == g.Focus && f.Mode == g.Mode && f.Place.key() == g.Place.key()
}

// HelpPage is the page's state: where it was opened from, the focus it
// walks from and the ones before it, the grouping and sort, the walk and
// rows it shows, the detail lines and the rail level last followed.
type HelpPage struct {
	r         *Runtime
	From      string        `json:"from"`
	Current   HelpFocus     `json:"current"`
	Back      []HelpFocus   `json:"back,omitempty"`
	Group     string        `json:"group"`
	Sort      string        `json:"sort"`
	Walk      HelpWalk      `json:"walk"`
	Rows      []HelpPageRow `json:"-"`
	Lines     []HelpLine    `json:"-"`
	Detail    []string      `json:"detail,omitempty"`
	RailLevel int           `json:"railLevel"`
	Built     uint64        `json:"built"`
}

// The delivery contract: the page runs no jobs.
func (p *HelpPage) HandleMessage(tea.Msg) bool { return false }
func (p *HelpPage) PendingCommand() tea.Cmd    { return nil }
func (p *HelpPage) Close()                     {}

// helpPage is the runtime's page state, made on first use.
func (r *Runtime) helpPage() *HelpPage {
	if p := componentAt[*HelpPage](r, helpStateID); p != nil {
		return p
	}
	p := &HelpPage{r: r, Group: HelpGroupings[0].Name, Sort: HelpSorts[0].Name, RailLevel: -1}
	r.Instance(helpStateID, Kind("ochazuke.help-page-state")).Component = p
	return p
}

// helpStyles are the help.style values and whether ? opens the page.
var helpStyles = []struct {
	Name string
	Page bool
}{{HelpStylePage, true}, {HelpStyleModal, false}}

// helpOnPage reports whether ? opens the page: help.style page, or a value
// it does not know (the page is the default).
func (r *Runtime) helpOnPage() bool {
	v, _ := r.Settings().Value(HelpStyleSetting)
	for _, s := range helpStyles {
		if s.Name == v {
			return s.Page
		}
	}
	return true
}

// openHelpPage opens the page (or walks from focus when it is open), the
// focus in mode. It reports false when the page cannot show (an
// application page holds its title), and ? then opens the modal.
func (r *Runtime) openHelpPage(focus string, mode ModeState) (bool, error) {
	if err := r.ensureHelpPage(); err != nil {
		return false, nil
	}
	p := r.helpPage()
	next := HelpFocus{Focus: focus, Mode: mode.Name}
	if inst := r.Instances[focus]; inst != nil {
		next.Place = r.placementOf(focus, inst.Noun)
	}
	if r.Page != HelpPageID {
		p.From = r.Page
		p.Back, p.Walk, p.Current = nil, HelpWalk{}, HelpFocus{}
	}
	if err := r.OpenPage(HelpPageID); err != nil {
		return false, err
	}
	p.walkTo(next)
	return true, nil
}

// ensureHelpPage registers the page, its rail and its list when they are
// not yet; closing the page removes them.
func (r *Runtime) ensureHelpPage() error {
	if r.PageOf(HelpPageID) != nil {
		return nil
	}
	r.Instance(HelpPageID, Parent("app"), Kind(helpPageNoun))
	page := &Page{ID: HelpPageID, Title: helpPageTitle, Focus: HelpPageID, Render: helpPageView}
	page.OnOpen = func(r *Runtime, from string) {
		if list := r.List(helpRowsID); list != nil && r.Focus == HelpPageID {
			r.changeFocus(list.focusTarget(), focusMove{cause: focusSettle})
		}
	}
	if err := r.RegisterPage(page, Kind(helpPageNoun)); err != nil {
		_ = r.RemoveInstance(HelpPageID)
		return err
	}
	tree, err := r.AddTree(helpRailID, nil, Parent(HelpPageID))
	if err != nil {
		_ = r.RemoveInstance(HelpPageID)
		return err
	}
	tree.Guides = true
	list, err := r.AddList(helpRowsID, nil, Parent(HelpPageID), NoMarks(), Columns(helpPageColumns()...))
	if err != nil {
		_ = r.RemoveInstance(HelpPageID)
		return err
	}
	list.QueryFilter = r.helpPage().filter
	list.OnFilter(func(*Collection) { r.helpPage().filtered() })
	return nil
}

// helpPageColumns are the list's columns: the key, what it runs, the mode
// or table, where it is defined, the guard and the status here.
func helpPageColumns() []Column {
	return []Column{
		{Title: "key", Min: 5, Max: 16, Pinned: true},
		{Title: "runs", Min: 12, Max: 40, Priority: 1},
		{Title: "mode", Min: 6, Max: 14, Priority: 4},
		{Title: "defined in", Min: 8, Max: 28, Priority: 3},
		{Title: "guard", Min: 5, Max: 18, Priority: 5},
		{Title: "status here", Min: 8, Max: 24, Priority: 2},
	}
}

// closeHelpPage goes back to the page the help was opened from and
// removes the page; with nowhere to go back to it stays and says so.
func (r *Runtime) closeHelpPage() error {
	if r.PageOf(HelpPageID) == nil {
		r.UI().Info("The binding help page is not open.")
		return nil
	}
	p := r.helpPage()
	back := p.From
	if back == HelpPageID || r.PageOf(back) == nil {
		back = ""
		for _, page := range r.PageList() {
			if page.ID != HelpPageID {
				back = page.ID
				break
			}
		}
	}
	if back == "" {
		r.UI().Info("There is no other page to go back to.")
		return nil
	}
	if r.Page == HelpPageID {
		if err := r.OpenPage(back); err != nil {
			return err
		}
	}
	p.From, p.Back, p.Current, p.Detail = "", nil, HelpFocus{}, nil
	return r.RemoveInstance(HelpPageID)
}

// walkTo makes next the walked focus: the walk and rows are read again,
// chips naming the old walk move to their counterpart or go, the mode
// follows the new focus when the old one has no rows for it, and the
// status line says what changed.
func (p *HelpPage) walkTo(next HelpFocus) {
	r := p.r
	old := p.Walk
	hadWalk := len(old.Levels) > 0
	asked := cmp.Or(next.Mode, "normal")
	next.Mode = r.ModeFor(next.Place, asked)
	if next.Focus != "" && r.Instances[next.Focus] == nil {
		next.Focus = ""
	}
	if hadWalk && !p.Current.same(next) {
		p.Back = append(p.Back, p.Current)
	}
	p.Current, p.Detail = next, nil
	p.read()
	notes := []string{"Walking from " + r.DisplayNoun(next.Place.Noun) + placeOn(next.Place) + " in " + next.Mode + "."}
	if list := r.List(helpRowsID); list != nil && hadWalk {
		f := ParseHelpQuery(list.Filter()).Refocus(old, p.Walk)
		if note := f.Note(); note != "" {
			notes = append(notes, note)
		}
		if f.Query.String() != list.Filter() {
			list.SetFilter(f.Query.String())
		}
	}
	if next.Mode != asked {
		notes = append(notes, "Mode "+next.Mode+": "+r.DisplayNoun(next.Place.Noun)+" has no rows in "+asked+".")
	}
	p.selectLevel(0)
	r.UI().Info(strings.Join(notes, " "))
}

// placeOn names a place's page for the status line.
func placeOn(place NounPlace) string {
	if title := cmp.Or(place.PageTitle, place.Page); title != "" {
		return " on " + title
	}
	return ""
}

// read builds the walk, the rows and the arrangement for the current focus
// and gives the rail and the list them.
func (p *HelpPage) read() {
	r := p.r
	mode := ModeState{Name: p.Current.Mode}
	if p.Current.Focus != "" {
		p.Walk = r.HelpWalkAt(p.Current.Focus, mode, nil)
	} else {
		p.Walk = r.HelpWalkAtPlace(p.Current.Place, mode)
	}
	p.Rows = r.HelpRows(p.Walk)
	p.arrange()
	// After the rows are placed: placing them must not read as a change of
	// the bindings next update.
	p.Built = r.renderCache.generation
}

// arrange gives the list the rows grouped and sorted (the filter narrows
// them in the list) and the rail its nodes.
func (p *HelpPage) arrange() {
	r := p.r
	p.Lines = Arrange(p.Rows, p.Walk, HelpQuery{}, GroupingNamed(p.Group), SortNamed(p.Sort)).Lines
	if list := r.List(helpRowsID); list != nil {
		if err := list.SetItems(p.listItems()); err != nil {
			r.UI().Error("binding help rows: " + err.Error())
		}
		p.filtered()
	}
	p.railNodes()
}

// listItems are the arranged lines as list rows: each row under its
// group's header (the row's Section); a walk level gets a row of its own
// that shows only when the filter leaves it none, so no level of the walk
// loses its header; a note row says a level's rows are in other modes.
func (p *HelpPage) listItems() []ListItem {
	var out []ListItem
	section := ""
	for _, line := range p.Lines {
		switch line.Kind {
		case HelpLineGroup:
			section = p.groupHeader(line.Group)
			if line.Group.Level >= 0 {
				out = append(out, ListItem{ID: "fill:" + line.Group.ID, Noun: helpRowNoun, Section: section, Data: line,
					Cells: []string{"", "no row here matches the filter", "", "", "", ""}})
			}
		case HelpLineNote:
			out = append(out, ListItem{ID: fmt.Sprintf("note:%d", line.Level), Noun: helpRowNoun, Section: section, Data: line,
				Cells: []string{"", line.NoteText(), "", "", "", "enter walks in " + modeLabel(line.Mode)}})
		case HelpLineRow:
			v := *line.Row
			runs := v.Operation
			if v.Args != "" {
				runs += " " + v.Args
			}
			mode := v.Mode
			if v.Unbound {
				mode = ""
			}
			out = append(out, ListItem{ID: v.ID, Label: v.Key, Noun: helpRowNoun, Section: section, Data: line,
				Cells: []string{v.Key, runs, mode, v.Defined(), v.Guard, v.StatusText()}, Search: []string{v.Help}})
		}
	}
	return out
}

// groupHeader is a group's section line: its number and label, what it is
// and its counts.
func (p *HelpPage) groupHeader(g *HelpGroup) string {
	if g.Level < 0 || g.Level >= len(p.Walk.Levels) {
		return fmt.Sprintf("%s  %s  %d rows", g.Label, g.Sub, g.Total)
	}
	l := p.Walk.Levels[g.Level]
	return fmt.Sprintf("%d %s  %s  %d rows in %s, %d no key", g.Level+1, l.Label(), g.Sub, l.Counts.Rows, p.Walk.Mode, l.Counts.NoKey)
}

// filter is the list's QueryFilter: the query's test of each line, a walk
// level's own row shown only while no row of that level matches.
func (p *HelpPage) filter(text string) func(ListItem) bool {
	q := ParseHelpQuery(text)
	matched := map[int]bool{}
	for _, v := range q.Matching(p.Rows) {
		matched[v.Level] = true
	}
	return func(item ListItem) bool {
		line, ok := item.Data.(HelpLine)
		if !ok {
			return false
		}
		switch line.Kind {
		case HelpLineRow:
			return q.Matches(*line.Row)
		case HelpLineNote:
			return true
		}
		return !matched[line.Level]
	}
}

// filtered follows a change of the filter: the empty list says which chips
// emptied it, and the rail's match counts follow.
func (p *HelpPage) filtered() {
	r := p.r
	list := r.List(helpRowsID)
	if list == nil {
		return
	}
	q := ParseHelpQuery(list.Filter())
	pl := list.Placeholder()
	pl.Empty = "No rows."
	if !q.Empty() {
		pl.Empty = q.ExplainText(p.Rows)
	}
	list.SetPlaceholder(pl)
	p.railNodes()
}

// railNodes gives the rail the walk as a graph: each level with its counts
// (and its matches under a filter), opening onto its tables in resolution
// order and its other modes; then the places beside the focus and the way
// back.
func (p *HelpPage) railNodes() {
	r := p.r
	tree := r.Tree(helpRailID)
	if tree == nil {
		return
	}
	q := HelpQuery{}
	if list := r.List(helpRowsID); list != nil {
		q = ParseHelpQuery(list.Filter())
	}
	matches := map[int]int{}
	for _, v := range q.Matching(p.Rows) {
		matches[v.Level]++
	}
	var nodes []TreeNode
	for _, l := range p.Walk.Levels {
		id := fmt.Sprintf("level/%d", l.Index)
		label := fmt.Sprintf("%s  %d✓ %d× %d−", l.Label(), l.Counts.Answers, l.Counts.Shadowed, l.Counts.NoKey)
		if !q.Empty() {
			label += fmt.Sprintf(" · %d match", matches[l.Index])
		}
		nodes = append(nodes, TreeNode{ID: id, Label: label, Noun: helpNodeNoun, Data: HelpNode{Kind: HelpNodeLevel, Level: l.Index}})
		for t, table := range l.Tables {
			mark := "· "
			if table.Kind == TableExtends {
				mark = "↳ "
			}
			name := modeLabel(table.Mode)
			if table.Instance != "" {
				name = "instance " + name
			}
			nodes = append(nodes, TreeNode{ID: fmt.Sprintf("%s/table/%d", id, t), Parent: id, Label: fmt.Sprintf("%s%s  %d", mark, name, len(table.Rows)), Noun: helpNodeNoun,
				Data: HelpNode{Kind: HelpNodeTable, Level: l.Index, Table: table.Label()}})
		}
		for _, mc := range p.otherModes(l) {
			nodes = append(nodes, TreeNode{ID: id + "/mode/" + mc.Mode, Parent: id, Label: fmt.Sprintf("mode %s  %d", mc.Mode, mc.Count), Noun: helpNodeNoun,
				Data: HelpNode{Kind: HelpNodeMode, Level: l.Index, Mode: mc.Mode}})
		}
	}
	samePage, sameNoun := p.neighbours()
	if len(samePage) > 0 {
		nodes = append(nodes, TreeNode{ID: "near/page", Label: "Also on " + cmp.Or(p.Walk.Place.PageTitle, p.Walk.Place.Page), Noun: helpNodeNoun, Data: HelpNode{Kind: HelpNodeHead, Level: -1}, SpaceAbove: true})
		for i, pl := range samePage {
			nodes = append(nodes, TreeNode{ID: fmt.Sprintf("near/page/%d", i), Parent: "near/page", Label: "→ " + r.DisplayNoun(pl.Noun), Noun: helpNodeNoun, Data: HelpNode{Kind: HelpNodePlace, Level: -1, Place: pl}})
		}
	}
	if len(sameNoun) > 0 {
		nodes = append(nodes, TreeNode{ID: "near/noun", Label: r.DisplayNoun(p.Walk.Place.Noun) + " elsewhere", Noun: helpNodeNoun, Data: HelpNode{Kind: HelpNodeHead, Level: -1}, SpaceAbove: true})
		for i, pl := range sameNoun {
			nodes = append(nodes, TreeNode{ID: fmt.Sprintf("near/noun/%d", i), Parent: "near/noun", Label: "→ " + cmp.Or(pl.PageTitle, pl.Page, "no page"), Noun: helpNodeNoun, Data: HelpNode{Kind: HelpNodePlace, Level: -1, Place: pl}})
		}
	}
	if n := len(p.Back); n > 0 {
		nodes = append(nodes, TreeNode{ID: "back", Label: "← back to " + r.DisplayNoun(p.Back[n-1].Place.Noun), Noun: helpNodeNoun, Data: HelpNode{Kind: HelpNodeBack, Level: -1}, SpaceAbove: true})
	}
	known := map[string]bool{}
	for _, n := range tree.Nodes() {
		known[n.ID] = true
	}
	for _, n := range nodes {
		if !known[n.ID] && n.Parent != "" && n.Parent != "level/0" {
			tree.collapsed[n.Parent] = true
		}
	}
	_ = tree.SetNodes(nodes)
}

// HelpNodeKind is what a rail node stands for.
type HelpNodeKind int

const (
	// HelpNodeLevel is a level of the walk.
	HelpNodeLevel HelpNodeKind = iota
	// HelpNodeTable is one of a level's tables.
	HelpNodeTable
	// HelpNodeMode is another mode a level's noun has rows in.
	HelpNodeMode
	// HelpNodePlace is a place beside the focus.
	HelpNodePlace
	// HelpNodeHead heads a run of places.
	HelpNodeHead
	// HelpNodeBack is the way back to the previous focus.
	HelpNodeBack
)

// HelpNode is a rail node's data: what it stands for and the level, table,
// mode or place it names.
type HelpNode struct {
	Kind  HelpNodeKind `json:"kind"`
	Level int          `json:"level"`
	Table string       `json:"table,omitempty"`
	Mode  string       `json:"mode,omitempty"`
	Place NounPlace    `json:"place,omitzero"`
}

// otherModes are the modes a level's noun (or instance) has rows in besides
// the walk's, busiest first.
func (p *HelpPage) otherModes(l HelpLevel) []ModeCount {
	counts := map[string]int{}
	for _, v := range p.Rows {
		if v.Level == l.Index && v.State == StatusOtherMode && v.Row.Mode != "" {
			counts[v.Row.Mode]++
		}
	}
	var out []ModeCount
	for m, n := range counts {
		out = append(out, ModeCount{Mode: m, Count: n})
	}
	slices.SortFunc(out, func(a, b ModeCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Mode, b.Mode))
	})
	return out
}

// neighbours are the places beside the walked one: other nouns on its
// page, and its noun on other pages (six each).
func (p *HelpPage) neighbours() (samePage, sameNoun []NounPlace) {
	at := p.Walk.Place
	seen := map[string]bool{at.Noun + "\x00" + at.Page: true}
	for _, pl := range p.r.AllPlaces() {
		k := pl.Noun + "\x00" + pl.Page
		if seen[k] || pl.Modal != "" {
			continue
		}
		switch {
		case pl.Page == at.Page && pl.Noun != at.Noun && len(samePage) < 6:
			seen[k] = true
			samePage = append(samePage, pl)
		case pl.Noun == at.Noun && pl.Page != at.Page && len(sameNoun) < 6:
			seen[k] = true
			sameNoun = append(sameNoun, pl)
		}
	}
	return samePage, sameNoun
}

// selectLevel moves the list to a level's first line and the rail to its
// node.
func (p *HelpPage) selectLevel(level int) {
	r := p.r
	if list := r.List(helpRowsID); list != nil {
		for _, item := range list.Items() {
			if line, ok := item.Data.(HelpLine); ok && line.Level == level && list.shows(item.ID) {
				_ = list.Select(item.ID)
				break
			}
		}
	}
	if tree := r.Tree(helpRailID); tree != nil {
		_ = tree.Select(fmt.Sprintf("level/%d", level))
	}
	p.RailLevel = level
}

// syncHelpPage follows what moved since the last update: a rail node moved
// onto scrolls the list to its level (moving never narrows), and a change
// of any binding table reads the walk again.
func (r *Runtime) syncHelpPage() {
	if r.PageOf(HelpPageID) == nil {
		return
	}
	p := r.helpPage()
	if p.Built != r.renderCache.generation && len(p.Walk.Levels) > 0 {
		p.read()
	}
	tree, list := r.Tree(helpRailID), r.List(helpRowsID)
	if tree == nil || list == nil || !r.focusInside(helpRailID) {
		return
	}
	node, ok := tree.Selected()
	if !ok {
		return
	}
	data, _ := node.Data.(HelpNode)
	if data.Level < 0 || data.Level == p.RailLevel {
		return
	}
	p.RailLevel = data.Level
	for _, item := range list.Items() {
		if line, ok := item.Data.(HelpLine); ok && line.Level == data.Level && list.shows(item.ID) {
			_ = list.Select(item.ID)
			return
		}
	}
}

// selectedLine is the list's selected line.
func (p *HelpPage) selectedLine() (HelpLine, bool) {
	if list := p.r.List(helpRowsID); list != nil {
		if item, ok := list.Selected(); ok {
			line, ok := item.Data.(HelpLine)
			return line, ok
		}
	}
	return HelpLine{}, false
}

// selectedNode is the rail's selected node.
func (p *HelpPage) selectedNode() (HelpNode, bool) {
	if tree := p.r.Tree(helpRailID); tree != nil {
		if node, ok := tree.Selected(); ok {
			data, ok := node.Data.(HelpNode)
			return data, ok
		}
	}
	return HelpNode{}, false
}

// setQuery puts the query on the list's filter.
func (p *HelpPage) setQuery(q HelpQuery) {
	if list := p.r.List(helpRowsID); list != nil {
		list.SetFilter(q.String())
	}
}

// query is the list's filter, parsed.
func (p *HelpPage) query() HelpQuery {
	if list := p.r.List(helpRowsID); list != nil {
		return ParseHelpQuery(list.Filter())
	}
	return HelpQuery{}
}

// helpPageView draws the page: the title line, the chips line, the rail
// beside the rows, the detail lines when there are any, and the line of the
// grouping, sort and position.
func helpPageView(r *Runtime, width, height int) Block {
	p := r.helpPage()
	tree, list := r.Tree(helpRailID), r.List(helpRowsID)
	if tree == nil || list == nil || width <= 0 || height <= 0 {
		return Block{}
	}
	styles := NewStyles(Appearance{Theme: "adaptive", Dark: true})
	if view := r.Settings().page; view.styles != nil {
		styles = view.styles(HelpPageID)
	}
	layout := r.PaneLayout(HelpPageID, Layout{Styles: styles})
	head := []string{
		styles.Muted.Render(ansi.Truncate(p.title(), width, "…")),
		ansi.Truncate(p.chipsLine(), width, "…"),
	}
	foot := []string{styles.Muted.Render(ansi.Truncate(p.footLine(), width, "…"))}
	detail := p.Detail
	if room := height - len(head) - len(foot) - PaneMinHeight; len(detail) > room {
		detail = detail[:max(0, room)]
	}
	body := height - len(head) - len(foot) - len(detail)
	if body < PaneMinHeight {
		return Text(strings.Join(append(head, foot...), "\n"))
	}
	rail := min(max(width/5, PaneMinWidth), width/4)
	rw, rh := PanelContentSize(rail, body)
	lw, lh := PanelContentSize(width-rail-1, body)
	focused := SplitNone
	switch {
	case list.DrawnFocused():
		focused = SplitRight
	case tree.List().DrawnFocused():
		focused = SplitLeft
	}
	pair := layout.panelPair("stack, focus first", Text(tree.View(rw, rh)), "bindings", Text(list.View(lw, lh)), width, body, rail, false, focused)
	rows := append(head, pair.Text)
	for _, line := range detail {
		rows = append(rows, ansi.Truncate(line, width, "…"))
	}
	rows = append(rows, foot...)
	return Block{Text: strings.Join(rows, "\n"), Cursor: pair.Offset(0, len(head)).Cursor}
}

// title is the page's first line: the focus walked from, its page, the
// levels and the mode.
func (p *HelpPage) title() string {
	at := p.Walk.Place
	return fmt.Sprintf("%s  focus %s%s  %d levels  mode %s", helpPageTitle, p.r.DisplayNoun(at.Noun), placeOn(at), len(p.Walk.Levels), p.Walk.Mode)
}

// chipsLine is the filter as numbered chips and how many rows match.
func (p *HelpPage) chipsLine() string {
	q := p.query()
	matched := len(q.Matching(p.Rows))
	if q.Empty() {
		return fmt.Sprintf("/ filter: words, key: noun: verb: mode: source: guard: level: table: status: is:   %d rows", len(p.Rows))
	}
	chips := make([]string, len(q.Terms))
	for i, t := range q.Terms {
		chips[i] = fmt.Sprintf("%d %s", i+1, t.Text)
	}
	return fmt.Sprintf("/ %s   %d of %d", strings.Join(chips, "  "), matched, len(p.Rows))
}

// footLine is the grouping, the sort and the list's position.
func (p *HelpPage) footLine() string {
	pos := ""
	if list := p.r.List(helpRowsID); list != nil && list.Shown() > 0 {
		pos = fmt.Sprintf("  %d/%d", list.selected+1, list.Shown())
	}
	return "group " + p.Group + "  sort " + p.Sort + pos
}
