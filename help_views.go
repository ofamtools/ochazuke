package ochazuke

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The ? help's views. Here is what the focus answers to now (BindingHelp,
// the default). Pick is the same table for a chosen noun and mode, as if
// the focus were on that noun in that mode (NounHelp). Table is a chosen
// noun's keys by mode: a row per key or key class, a column per mode with
// rows for the noun or app, each cell the operation that answers there.
// S (bindings-help.view) cycles the views the settings leave on; n
// (bindings-help.noun) chooses the noun of pick and table, m
// (bindings-help.mode) the mode of pick, p (bindings-help.place) the place
// of pick and table: one of the noun's recorded places (NounPlaces), whose
// chain gives the walk the levels between the noun and app, or no place.
// All four are rows on the help modal's noun, guarded so they answer, and
// are hinted, only where they do something.
const (
	HelpHere  = "here"
	HelpPick  = "pick"
	HelpTable = "table"
)

// The help's settings, declared by EnsureConfigSettings (section help):
// whether pick and table are in S's cycle (both default on; with both off
// S does nothing and is not offered) and whether the pending modes (counts,
// operators, registers, the terminal leader, key-sequence prefixes) are
// pick's mode choices and table's columns (default off).
const (
	HelpPickSetting    = "help.pick-view"
	HelpTableSetting   = "help.table-view"
	HelpPendingSetting = "help.pending-modes"
)

// helpViews is the order S cycles in.
var helpViews = []string{HelpHere, HelpPick, HelpTable}

// helpNounKind, helpModeKind and helpPlaceKind are the choosers n, m and
// p open.
const (
	helpNounKind  = "bindings-help-noun"
	helpModeKind  = "bindings-help-mode"
	helpPlaceKind = "bindings-help-place"
)

// HelpCell is one mode's rows for a key in the help's table view: the
// first answers, the ones after answer when its guard is false.
type HelpCell struct {
	Mode string    `json:"mode"`
	Rows []HelpRow `json:"rows"`
}

// HelpKey is one row of the help's table view: a key (or key class) and
// its rows per mode, in column order; a mode where nothing answers has no
// cell.
type HelpKey struct {
	Key   string     `json:"key"`
	Modes []HelpCell `json:"modes"`
}

// helpState is the help modal's Component: the view shown, the focus and
// rows it opened over (here), and the noun and mode of the hypothetical
// views, and their place (nil: no place, the noun and app alone). It draws
// the modal (ModalBody).
type helpState struct {
	r      *Runtime
	view   string
	origin string
	here   []HelpRow
	noun   string
	mode   ModeState
	place  *NounPlace
	home   *NounPlace // the origin focus's place, nil when it has no noun
	modes  []string   // the table's mode columns, as last shown
}

// newHelpState opens the help over focus; home is focus's place (helpHome),
// read before the help modal opened.
func newHelpState(r *Runtime, focus string, home *NounPlace, mode ModeState, rows []HelpRow) *helpState {
	noun := "app"
	if inst := r.Instances[focus]; inst != nil && inst.Noun != "" {
		noun = inst.Noun
	}
	h := &helpState{r: r, view: HelpHere, origin: focus, here: rows, noun: noun, mode: mode, home: home}
	h.setNoun(noun)
	return h
}

// helpHome is the place of the focus the help opens over, as the placements
// recorder computes it (placementOf), whether or not the record holds it
// yet; nil for a focus without a noun.
func helpHome(r *Runtime, focus string) *NounPlace {
	inst := r.Instances[focus]
	if inst == nil || inst.Noun == "" {
		return nil
	}
	home := r.placementOf(focus, inst.Noun)
	return &home
}

// setNoun chooses the hypothetical views' noun and resets the place to its
// default: for the origin focus's noun the place the help was opened at,
// for another noun its most seen place, else no place.
func (h *helpState) setNoun(noun string) {
	noun = h.r.CanonicalNoun(noun)
	h.noun, h.place = noun, nil
	if h.home != nil && h.home.Noun == noun {
		home := *h.home
		h.place = &home
		return
	}
	if places := h.r.NounPlaces(noun); len(places) > 0 {
		h.place = &places[0]
	}
}

// at is the place the hypothetical views resolve at: the chosen place, or
// the noun alone.
func (h *helpState) at() NounPlace {
	if h.place != nil {
		return *h.place
	}
	return NounPlace{Noun: h.noun}
}

// placeLabel names a place as its chooser row and the header do: the page
// title (else its id), then the ancestor nouns outermost first, and the
// modal: "Context › page › list", "Context › ask · modal ask".
func placeLabel(r *Runtime, p NounPlace) string {
	var parts []string
	if title := cmp.Or(p.PageTitle, p.Page); title != "" {
		parts = append(parts, title)
	}
	for _, a := range p.Ancestors {
		parts = append(parts, r.DisplayNoun(a))
	}
	text := strings.Join(parts, " › ")
	if p.Modal != "" {
		text = strings.TrimPrefix(text+" · modal "+r.DisplayNoun(p.Modal), " · ")
	}
	if text == "" {
		return "(top level)"
	}
	return text
}

// placeText is the header's place: the label, or none.
func (h *helpState) placeText() string {
	if h.place == nil {
		return "none"
	}
	return placeLabel(h.r, *h.place)
}

// The delivery contract: the help runs no jobs.
func (h *helpState) HandleMessage(tea.Msg) bool { return false }
func (h *helpState) PendingCommand() tea.Cmd    { return nil }
func (h *helpState) Close()                     {}

// helpOpen is the open help's state, nil when the help is closed.
func (r *Runtime) helpOpen() *helpState {
	if inst := r.Instances[helpID]; inst != nil {
		if h, ok := inst.Component.(*helpState); ok {
			return h
		}
	}
	return nil
}

// helpFlag is a boolean help setting, def when it is not declared.
func (r *Runtime) helpFlag(name string, def bool) bool {
	v, ok := r.Settings().Value(name)
	b, isBool := v.(bool)
	if !ok || !isBool {
		return def
	}
	return b
}

// ensureHelpSettings declares the help settings the application did not.
func (r *Runtime) ensureHelpSettings() {
	s := r.Settings()
	if s.find(HelpPickSetting) == nil {
		_ = r.DeclareSetting(Setting{Name: HelpPickSetting, Type: SettingBoolean, Default: true, Description: "S in the ? help shows the keys for a noun and mode you pick"})
	}
	if s.find(HelpTableSetting) == nil {
		_ = r.DeclareSetting(Setting{Name: HelpTableSetting, Type: SettingBoolean, Default: true, Description: "S in the ? help shows a noun's keys as a table, a column per mode"})
	}
	if s.find(HelpPendingSetting) == nil {
		_ = r.DeclareSetting(Setting{Name: HelpPendingSetting, Type: SettingBoolean, Default: false, Description: "The ? help's pick and table views offer the pending modes too (counts, operators, the terminal leader, key-sequence prefixes)"})
	}
}

// views are the views S cycles through, read from the settings each time.
func (h *helpState) views() []string {
	out := []string{HelpHere}
	if h.r.helpFlag(HelpPickSetting, true) {
		out = append(out, HelpPick)
	}
	if h.r.helpFlag(HelpTableSetting, true) {
		out = append(out, HelpTable)
	}
	return out
}

// cycle moves to the next view in helpViews order that is on.
func (h *helpState) cycle() {
	on := h.views()
	i := slices.Index(helpViews, h.view)
	for step := 1; step <= len(helpViews); step++ {
		if next := helpViews[(i+step)%len(helpViews)]; slices.Contains(on, next) {
			h.view = next
			return
		}
	}
}

// show draws the current view, made again: its rows and columns differ
// from view to view. Here and pick are a tree (helpNodes, R6.10: l and
// h, enter, open and close a row; z l and z h scroll the columns), the
// table view a plain table.
func (h *helpState) show() error {
	r := h.r
	for _, id := range []string{helpTreeID, helpListID} {
		if r.Instances[id] != nil {
			if err := r.RemoveInstance(id); err != nil {
				return err
			}
		}
	}
	var nodes []TreeNode
	var collapsed []string
	switch h.view {
	case HelpPick:
		nodes, collapsed = helpNodes(PlaceHelp(r, h.at(), h.mode))
	case HelpTable:
		// The table view is a plain table: a row per key, h and l scroll
		// its mode columns.
		var keys []HelpKey
		h.modes, keys = r.HelpTable(h.at(), r.helpFlag(HelpPendingSetting, false))
		list, err := r.AddList(helpListID, helpTableItems(h.r.DisplayNoun(h.noun), h.modes, keys), Parent(helpID), Columns(helpTableColumns(h.modes)...))
		if err != nil {
			return err
		}
		return list.Focus()
	default:
		nodes, collapsed = helpNodes(h.here)
	}
	tree, err := r.AddTree(helpTreeID, nil, Parent(helpID), Columns(helpColumns...))
	if err != nil {
		return err
	}
	for _, id := range collapsed {
		tree.collapsed[id] = true
	}
	if err := tree.SetNodes(nodes); err != nil {
		return err
	}
	return tree.Focus()
}

// ModalTitle names the view: "Keys · pick". With only here on, "Keys".
func (h *helpState) ModalTitle() string {
	if len(h.views()) == 1 && h.view == HelpHere {
		return "Keys"
	}
	return "Keys · " + h.view
}

// header is the line above the list in the hypothetical views: what they
// are about, and in the table what its marks mean.
func (h *helpState) header() string {
	switch h.view {
	case HelpPick:
		return "noun: " + h.r.DisplayNoun(h.noun) + " · mode: " + h.mode.Name + " · place: " + h.placeText()
	case HelpTable:
		noun := h.r.DisplayNoun(h.noun)
		return "noun: " + noun + " · place: " + h.placeText() + " · .verb is " + noun + ".verb · (op) answered by app · op? guarded"
	}
	return ""
}

// ModalBlock draws the view: the header, the list, and in the table the
// selected key's rows in every mode under a rule.
func (h *helpState) ModalBlock(s Styles, width, height int) Block {
	list := h.r.List(helpListID)
	if list == nil || width <= 0 || height <= 0 {
		return Text("")
	}
	var top []string
	if text := h.header(); text != "" && height > 1 {
		top = append(top, s.Muted.Render(ansi.Truncate(text, width, "…")))
	}
	var detail []string
	if h.view == HelpTable {
		if item, ok := list.Selected(); ok {
			if key, ok := item.Data.(HelpKey); ok {
				detail = helpKeyDetail(h.r, key, width)
			}
		}
		room := height - len(top) - 4 // at least three rows and the header
		if len(detail) > room-1 {
			detail = detail[:max(room-1, 0)]
		}
		if len(detail) > 0 {
			detail = append([]string{s.Muted.Render(strings.Repeat("─", width))}, detail...)
		}
	}
	body := list.View(width, max(height-len(top)-len(detail), 1))
	parts := append(top, body)
	return Text(strings.Join(append(parts, detail...), "\n"))
}

// helpKeyDetail is the selected table row's rows mode by mode: the mode,
// the operation with its arguments and guard, and the answering level;
// the rows that answer when a guard is false follow under "then".
func helpKeyDetail(r *Runtime, key HelpKey, width int) []string {
	widest := 0
	for _, cell := range key.Modes {
		widest = max(widest, ansi.StringWidth(cell.Mode))
	}
	var lines []string
	for _, cell := range key.Modes {
		for i, row := range cell.Rows {
			mode := cell.Mode
			if i > 0 {
				mode = ""
			}
			text := row.Operation
			if args := helpArgsText(row.Args); args != "" {
				text += " " + args
			}
			if row.Guard != "" {
				text += " when " + row.Guard
			}
			text += " · " + r.DisplayNoun(row.Noun)
			if i > 0 {
				text = "then " + text
			}
			line := fmt.Sprintf("%-*s  %s", widest, mode, text)
			lines = append(lines, ansi.Truncate(line, width, "…"))
		}
	}
	return lines
}

// sourceArgs is a hypothetical row's arguments: its own, and each one it
// takes from the key, the payload or the row as "$source".
func sourceArgs(b Binding) Arguments {
	args := Arguments{}
	for name, value := range b.Request.Args {
		args[name] = value
	}
	for name, source := range b.From {
		args[name] = "$" + strings.Join(strings.Fields(source), " $")
	}
	return args
}

// helpBaseModes are the registered modes, normal and insert first.
func (r *Runtime) helpBaseModes() []string {
	names := slices.Sorted(maps.Keys(r.Modes))
	var out []string
	for _, first := range []string{"normal", "insert"} {
		if r.Modes[first] {
			out = append(out, first)
		}
	}
	for _, name := range names {
		if name != "normal" && name != "insert" {
			out = append(out, name)
		}
	}
	return out
}

// helpPendingModes are the pending modes a key can be pressed in (the
// macro recording shadow is not one).
func (r *Runtime) helpPendingModes() []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(r.pendingModes)) {
		if name != "macro_recording" {
			out = append(out, name)
		}
	}
	return out
}

// HelpModes are the modes the help's pick view offers for place's noun:
// the registered modes, and with pending the pending modes and every
// key-sequence prefix the place's walk binds ("normal g" after g).
// NounPlace{Noun: noun} is the noun with no place.
func (r *Runtime) HelpModes(place NounPlace, pending bool) []ModeState {
	place = r.canonicalPlace(place)
	var out []ModeState
	bases := r.helpBaseModes()
	for _, name := range bases {
		out = append(out, ModeState{Name: name})
	}
	if !pending {
		return out
	}
	for _, name := range r.helpPendingModes() {
		out = append(out, ModeState{Name: name})
	}
	for _, base := range bases {
		seen := map[string]bool{}
		for _, row := range r.nounRows(place, ModeState{Name: base}) {
			parts := strings.Fields(row.Keys)
			for i := 1; i < len(parts); i++ {
				prefix := strings.Join(parts[:i], " ")
				if !seen[prefix] {
					seen[prefix] = true
					out = append(out, ModeState{Name: base + " " + prefix, Payload: Payload{Prefix: prefix}})
				}
			}
		}
	}
	return out
}

// modeHasRows reports whether a mode's own table (not the any-mode one)
// holds a row on place's walk.
func (r *Runtime) modeHasRows(place NounPlace, mode ModeState) bool {
	base, _ := baseAndPrefix(mode)
	for _, lv := range r.nounWalk(place, mode) {
		if lv.inst != nil {
			for _, b := range lv.inst.Bindings {
				if b.Mode == base && !b.Unbind {
					return true
				}
			}
		}
		if lv.noun != nil {
			for _, rows := range lv.noun.tables(base) {
				for _, b := range rows {
					if !b.Unbind && !isPrefixCancel(b) {
						return true
					}
				}
			}
		}
	}
	return false
}

// HelpTable is the help's table view for place's noun (NounPlace{Noun:
// noun} for no place): the modes whose own tables hold a row on its walk (the registered modes, and with pending
// the pending ones; a key sequence is a row of its base mode, so prefixes
// add no column), and a row per key or key class with its rows per mode
// (nounRows; a generated prefix cancel left out). The keys the noun or a
// level of its place answers come first, then app's, each sorted with key
// classes last.
func (r *Runtime) HelpTable(place NounPlace, pending bool) ([]string, []HelpKey) {
	place = r.canonicalPlace(place)
	candidates := r.helpBaseModes()
	if pending {
		candidates = append(candidates, r.helpPendingModes()...)
	}
	var modes []string
	byKey := map[string]*HelpKey{}
	var order []string
	for _, name := range candidates {
		mode := ModeState{Name: name}
		if !r.modeHasRows(place, mode) {
			continue
		}
		modes = append(modes, name)
		for _, row := range r.nounRows(place, mode) {
			if isPrefixCancel(row.Binding) {
				continue
			}
			key := rowKey(row.Binding)
			k := byKey[key]
			if k == nil {
				k = &HelpKey{Key: key}
				byKey[key] = k
				order = append(order, key)
			}
			if n := len(k.Modes); n == 0 || k.Modes[n-1].Mode != name {
				k.Modes = append(k.Modes, HelpCell{Mode: name})
			}
			help := r.helpRow(row, sourceArgs(row.Binding))
			if row.Request.Verb != "" {
				help.Operation = r.DisplayKey(row.Request.Key())
			}
			cell := &k.Modes[len(k.Modes)-1]
			cell.Rows = append(cell.Rows, help)
		}
	}
	own := func(k *HelpKey) bool {
		for _, cell := range k.Modes {
			if cell.Rows[0].Noun != "app" {
				return true
			}
		}
		return false
	}
	slices.SortStableFunc(order, func(a, b string) int {
		ka, kb := byKey[a], byKey[b]
		if oa, ob := own(ka), own(kb); oa != ob {
			if oa {
				return -1
			}
			return 1
		}
		_, la := helpKey(a)
		_, lb := helpKey(b)
		if la != lb {
			if la {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
	keys := make([]HelpKey, len(order))
	for i, key := range order {
		keys[i] = *byKey[key]
	}
	return modes, keys
}

// helpTableColumns are the table view's columns: the key, kept when the
// columns scroll (z l, z h), then a column per mode, later modes dropped
// first when narrow.
func helpTableColumns(modes []string) []Column {
	cols := []Column{{Title: "key"}}
	for i, mode := range modes {
		cols = append(cols, Column{Title: mode, Max: 24, Priority: i + 1})
	}
	return cols
}

// helpTableItems is a list row per key, a cell per mode column.
func helpTableItems(noun string, modes []string, keys []HelpKey) []ListItem {
	items := make([]ListItem, len(keys))
	for i, k := range keys {
		label, _ := helpKey(k.Key)
		cells := make([]string, len(modes)+1)
		cells[0] = label
		for _, cell := range k.Modes {
			if j := slices.Index(modes, cell.Mode); j >= 0 {
				cells[j+1] = helpCellText(noun, cell)
			}
		}
		items[i] = ListItem{ID: fmt.Sprintf("__bindings-help/%d", i), Label: strings.Join(cells, "  "), Noun: "ochazuke.binding", Data: k, Cells: cells}
	}
	return items
}

// helpCellText is a table cell: the answering row's operation, ".verb"
// for the noun's own, "?" after it when guarded, in parentheses when app
// answers rather than the noun.
func helpCellText(noun string, cell HelpCell) string {
	if len(cell.Rows) == 0 {
		return ""
	}
	row := cell.Rows[0]
	text := row.Operation
	if text == "" {
		text = "-"
	}
	if verb, ok := strings.CutPrefix(text, noun+"."); ok && noun != "app" {
		text = "." + verb
	}
	if row.Guard != "" {
		text += "?"
	}
	if row.Noun == "app" && noun != "app" {
		text = "(" + text + ")"
	}
	return text
}

// placeRows are the place chooser's rows: no place first, then the noun's
// places most seen first, each with its count; the current one is marked.
func (h *helpState) placeRows() []ChooserRow {
	current := func(p *NounPlace) string {
		if p == nil && h.place == nil || p != nil && h.place != nil && p.key() == h.place.key() {
			return "current"
		}
		return ""
	}
	rows := []ChooserRow{{ID: "none", Label: "no place (noun + app)", Cells: []string{"no place (noun + app)", "", current(nil)}}}
	for i, p := range h.r.NounPlaces(h.noun) {
		label := placeLabel(h.r, p)
		rows = append(rows, ChooserRow{ID: fmt.Sprintf("place/%d", i), Label: label, Data: p, Cells: []string{label, fmt.Sprintf("%d×", p.Count), current(&p)}})
	}
	return rows
}

// helpChooserRows are a chooser's rows with the current one first, marked.
func helpChooserRows(names []string, current string, data func(string) any) []ChooserRow {
	var rows []ChooserRow
	for _, name := range names {
		row := ChooserRow{ID: name, Label: name, Data: data(name), Cells: []string{name, ""}}
		if name == current {
			row.Cells[1] = "current"
			rows = append([]ChooserRow{row}, rows...)
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func registerHelpViews(r *Runtime) {
	r.Define(helpID)
	view := func(fn func(*helpState) error) func(*Runtime, Context, Arguments) (any, error) {
		return func(r *Runtime, _ Context, _ Arguments) (any, error) {
			h := r.helpOpen()
			if h == nil {
				return nil, fmt.Errorf("the help is not open")
			}
			if err := fn(h); err != nil {
				return nil, err
			}
			return h.view, nil
		}
	}
	r.Register(Operation{Verb: "view", Noun: helpID, Description: "Show the help's next view: here (the focus), pick (a noun and mode you choose), table (a noun's keys by mode)", Result: "view", Run: view(func(h *helpState) error {
		h.cycle()
		return h.show()
	})})
	r.Register(Operation{Verb: "noun", Noun: helpID, Description: "Choose the noun the help's pick and table views describe", Result: "view", Run: view(func(h *helpState) error {
		names := slices.SortedFunc(maps.Keys(h.r.Nouns), func(a, b string) int { return strings.Compare(h.r.DisplayNoun(a), h.r.DisplayNoun(b)) })
		rows := helpChooserRows(names, h.noun, func(string) any { return nil })
		for i := range rows {
			rows[i].Label = h.r.DisplayNoun(rows[i].ID)
			rows[i].Cells[0] = rows[i].Label
		}
		_, err := h.r.OpenChooser(ChooserSpec{Kind: helpNounKind, Title: "Noun", Keep: []int{0}, Rows: rows, Choose: func(row ChooserRow) Result {
			h.setNoun(row.ID)
			if err := h.show(); err != nil {
				return Failed(err)
			}
			return Done(h.noun)
		}})
		return err
	})})
	r.Register(Operation{Verb: "mode", Noun: helpID, Description: "Choose the mode the help's pick view describes", Result: "view", Run: view(func(h *helpState) error {
		modes := h.r.HelpModes(h.at(), h.r.helpFlag(HelpPendingSetting, false))
		byName := map[string]ModeState{}
		names := make([]string, len(modes))
		for i, mode := range modes {
			names[i] = mode.Name
			byName[mode.Name] = mode
		}
		_, err := h.r.OpenChooser(ChooserSpec{Kind: helpModeKind, Title: "Mode", Rows: helpChooserRows(names, h.mode.Name, func(name string) any { return byName[name] }), Choose: func(row ChooserRow) Result {
			h.mode, _ = row.Data.(ModeState)
			if err := h.show(); err != nil {
				return Failed(err)
			}
			return Done(h.mode.Name)
		}})
		return err
	})})
	r.Register(Operation{Verb: "place", Noun: helpID, Description: "Choose where the help's pick and table views put the noun: one of the places it was seen (the page, the levels above it, the modal), or none (the noun and app)", Result: "place", Run: view(func(h *helpState) error {
		_, err := h.r.OpenChooser(ChooserSpec{Kind: helpPlaceKind, Title: "Place", Keep: []int{0}, Rows: h.placeRows(), Choose: func(row ChooserRow) Result {
			h.place = nil
			if p, ok := row.Data.(NounPlace); ok {
				h.place = &p
			}
			if err := h.show(); err != nil {
				return Failed(err)
			}
			return Done(h.placeText())
		}})
		return err
	})})
	r.DefineGuard("help-views", func(r *Runtime, _ KeyEvent) bool {
		h := r.helpOpen()
		return h != nil && len(h.views()) > 1
	})
	r.DefineGuard("help-hypothetical", func(r *Runtime, _ KeyEvent) bool {
		h := r.helpOpen()
		return h != nil && (h.view == HelpPick || h.view == HelpTable)
	})
	r.DefineGuard("help-pick", func(r *Runtime, _ KeyEvent) bool {
		h := r.helpOpen()
		return h != nil && h.view == HelpPick
	})
	r.DefineGuard("help-places", func(r *Runtime, _ KeyEvent) bool {
		h := r.helpOpen()
		return h != nil && (h.view == HelpPick || h.view == HelpTable) && len(r.NounPlaces(h.noun)) > 0
	})
	n := r.Noun(helpID)
	if n.Guards == nil {
		n.Guards = map[string]string{}
	}
	n.Guards["view"] = "help-views"
	n.Guards["noun"] = "help-hypothetical"
	n.Guards["mode"] = "help-pick"
	n.Guards["place"] = "help-places"
}

// inputNoun reports whether a focus on noun is an input, so the editor
// noun is its fallback (stack's editing): the editor noun itself, a noun
// that pins insert mode (prompt, chooser), or the noun of a live input.
func (r *Runtime) inputNoun(noun string) bool {
	if noun == "ochazuke.editor" {
		return true
	}
	if n := r.Nouns[noun]; n != nil && n.Pin == "insert" {
		return true
	}
	for id, inst := range r.Instances {
		if inst.Noun == noun && r.Input(id) != nil {
			return true
		}
	}
	return false
}
