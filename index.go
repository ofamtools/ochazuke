package ochazuke

import (
	"fmt"
	"slices"
	"strings"
)

// The index component (noun scaffold plan, a): ochazuke.index is a list of
// any stored noun built at run time from the noun's declaration: a list From
// its store (windowed when the store pages, list_window.go), its columns
// derived from its props unless the noun or the user names them, the list
// widget's sort, filter, search, marks and paging, the empty and loading
// lines, and the archive toggle (collection.show-archived, z a). It is
// usable on its own (AddIndex, or a catalog list {"kind": "index"}); the
// generated index page of a noun (nouns/NOUN, OpenIndex) is the list with
// the selected thing's props beside it. Index pages are never tabs unless
// pages.pinned lists them: they open from the Nouns page (enter), the page
// picker, the palette and noun.show.

// IndexColumnsSetting is the prefix of the per-noun setting naming an
// index's columns (index.columns.task: a list of prop names, empty for
// the derived columns). It is declared for a noun when its index is
// built, and at startup for every noun settings.json names.
const IndexColumnsSetting = "index.columns."

// indexNoun is the noun of an index page's instance.
const indexNoun = "ochazuke.index"

// IndexPagePrefix starts a noun's generated index page id (nouns/task).
const IndexPagePrefix = "nouns/"

// indexMaxDerived is how many columns the derived rule picks beside the
// label: the table drops the lowest priority ones that do not fit.
const indexMaxDerived = 6

// IndexPageID is the id of a noun's generated index page.
func IndexPageID(noun string) string { return IndexPagePrefix + noun }

// IndexColumns are the columns an index of the noun draws: the noun's
// index.columns.NOUN setting when it names props, else the columns the
// noun declares (NounBuilder.Columns), else the derived rule: the label
// prop (else the key) first, then the props whose values are short and
// choosable (an enum, a boolean, a noun reference, a number, a time), up
// to indexMaxDerived of them, then updated when the noun has it; never a
// secret, computed, multi-line, list or object prop. A later column has a
// lower priority, so a narrow table keeps the first ones.
func (r *Runtime) IndexColumns(noun string) []Column {
	n := r.Nouns[r.CanonicalNoun(noun)]
	if n == nil {
		return nil
	}
	if names := r.Settings().Strings(IndexColumnsSetting + n.Name); len(names) > 0 {
		var cols []Column
		for i, name := range names {
			if p := n.prop(name); p != nil {
				cols = append(cols, indexColumn(*p, len(names)-i))
			}
		}
		if len(cols) > 0 {
			return cols
		}
	}
	if len(n.Columns) > 0 {
		return slices.Clone(n.Columns)
	}
	var picked []PropSpec
	first := n.roleProp(RoleLabel)
	if first == nil {
		first = n.keyProp()
	}
	if first != nil {
		picked = append(picked, *first)
	}
	var updated *PropSpec
	for i := range n.Props {
		p := n.Props[i]
		if first != nil && p.Name == first.Name || len(picked) > indexMaxDerived {
			continue
		}
		if p.Name == "updated" {
			updated = &n.Props[i]
			continue
		}
		if shortProp(p) {
			picked = append(picked, p)
		}
	}
	if updated != nil && shortProp(*updated) || updated != nil && updated.Type == SettingString {
		picked = append(picked, *updated)
	}
	cols := make([]Column, len(picked))
	for i, p := range picked {
		cols[i] = indexColumn(p, len(picked)-i)
	}
	return cols
}

// shortProp reports whether a prop's values are short and choosable, so a
// derived index shows it as a column.
func shortProp(p PropSpec) bool {
	if p.Secret || p.computed() || p.Multiline || p.Key {
		return false
	}
	if len(p.Enum) > 0 || p.Noun != "" {
		return true
	}
	switch p.Type {
	case SettingBoolean, SettingInteger, SettingNumber, PropTime, SettingDuration:
		return true
	}
	return false
}

// indexColumn is a prop's column: titled by the prop (columnField reads
// it back), the label wide, the rest narrow.
func indexColumn(p PropSpec, priority int) Column {
	col := Column{Title: p.Name, Priority: priority, Min: max(4, len(p.Name))}
	if p.Role == RoleLabel || p.Key {
		col.Min = 12
	}
	return col
}

// AddIndex adds the index component for a stored noun: a list From the
// noun's store, in its IndexColumns, with the empty and loading lines
// naming its things. The options are AddList's (Parent, NoSpill, ...).
func (r *Runtime) AddIndex(id, noun string, opts ...InstanceOption) (*Collection, error) {
	n := r.Nouns[r.CanonicalNoun(noun)]
	if n == nil {
		return nil, fmt.Errorf("index of %q: no such noun", noun)
	}
	r.declareIndexColumns(n.Name)
	opts = append([]InstanceOption{From(n.Name), Columns(r.IndexColumns(n.Name)...)}, opts...)
	list, err := r.AddList(id, nil, opts...)
	if err != nil {
		return nil, err
	}
	p := list.Placeholder()
	p.Plural = pluralWord(r.DisplayNoun(n.Name))
	list.SetPlaceholder(p)
	return list, nil
}

// declareIndexColumns declares index.columns.NOUN unless it is.
func (r *Runtime) declareIndexColumns(noun string) {
	name := IndexColumnsSetting + noun
	if r.Settings().find(name) != nil {
		return
	}
	_ = r.declareBuiltinSetting(Setting{Name: name, Section: "lists", Type: SettingList, Default: []string{}, Description: "The props the " + noun + " index shows as columns, in order; empty shows the derived columns (the label, then short choosable props, then updated)"})
}

// declareSavedIndexColumns declares index.columns.NOUN for every noun the
// settings file names one for, so the file's value is read and not
// reported unknown. Model.Init calls it with EnsureNounsPage.
func (r *Runtime) declareSavedIndexColumns() {
	for name := range r.Settings().raw {
		if noun, ok := strings.CutPrefix(name, IndexColumnsSetting); ok && r.Nouns[noun] != nil {
			r.declareIndexColumns(noun)
		}
	}
}

// indexPage is an index page instance's data: the noun it lists.
type indexPage struct {
	Noun string `json:"noun"`
	// Form is the key of the thing the show form holds ("" none).
	Form string `json:"form,omitempty"`
	// syncing is set while the form is made (its get is a request, which
	// syncs again after it runs).
	syncing bool
}

// DetailShowFormSetting draws an index page's detail as the edit form over
// the selected thing's props (read-only until a field is edited, commit on
// change, u undoes); off, its props as text.
const DetailShowFormSetting = "detail.show-form"

// syncIndexForm gives the current index page's show form the selected
// thing: the noun's EditSpec placed as a record editor under the page,
// made again when the selection names another thing. It runs after each
// request and when the page opens, never while a frame draws.
func (r *Runtime) syncIndexForm() {
	inst := r.Instances[r.Page]
	if inst == nil {
		return
	}
	ref, ok := inst.Data.(*indexPage)
	if !ok || ref.syncing {
		return
	}
	ref.syncing = true
	defer func() { ref.syncing = false }()
	list := r.List(r.Page + "/list")
	formID := r.Page + "/form"
	key := ""
	if item, ok := list.Selected(); ok && list != nil && r.Settings().Bool(DetailShowFormSetting) {
		key = strings.TrimPrefix(item.ID, r.Page+"/list/")
	}
	if key == ref.Form && (key == "" || r.RecordEditor(formID) != nil) {
		return
	}
	if r.Instances[formID] != nil {
		if inside := slices.Contains(r.InstancePath(r.Focus), formID); inside {
			r.changeFocus(list.focusTarget(), focusMove{cause: focusSettle})
		}
		_ = r.RemoveInstance(formID)
	}
	ref.Form = ""
	if key == "" {
		return
	}
	spec, err := r.EditSpec(ref.Noun, key)
	if err != nil {
		return // a noun the form cannot hold keeps the text detail
	}
	if _, err := r.AddRecordEditor(formID, spec, Parent(r.Page)); err == nil {
		ref.Form = key
	}
}

// OpenIndex opens the noun's generated index page (made on first use),
// its row for key selected when key is given. The page is out of the tabs
// unless pages.pinned lists it, so opening and leaving it leaves the tab
// row as it was.
func (r *Runtime) OpenIndex(noun, key string) error {
	n := r.Nouns[r.CanonicalNoun(noun)]
	if n == nil {
		return fmt.Errorf("no noun %q", noun)
	}
	if !StoredKind(r.NounKind(n.Name)) {
		return fmt.Errorf("%s is a %s noun: it has no things to list (its verbs: %s)", r.DisplayNoun(n.Name), r.NounKind(n.Name), strings.Join(r.Operations.ActionNames(n), ", "))
	}
	id := IndexPageID(n.Name)
	if r.PageOf(id) == nil {
		if err := r.addIndexPage(n, id); err != nil {
			return err
		}
	}
	if err := r.OpenPage(id); err != nil {
		return err
	}
	list := r.List(id + "/list")
	if list == nil {
		return nil
	}
	if key != "" {
		_ = list.Select(id + "/list/" + key)
	}
	if r.Focus == id {
		r.changeFocus(list.focusTarget(), focusMove{cause: focusSettle})
	}
	r.syncIndexForm()
	return nil
}

// addIndexPage registers the noun's index page: its list and detail.
func (r *Runtime) addIndexPage(n *Noun, id string) error {
	inst := r.Instance(id, Parent("app"), Kind(indexNoun))
	inst.Data = &indexPage{Noun: n.Name}
	list, err := r.AddIndex(id+"/list", n.Name, Parent(id))
	if err != nil {
		_ = r.RemoveInstance(id)
		return err
	}
	detail := id + "/detail"
	page := &Page{ID: id, Title: id, Focus: list.FocusTarget(), Render: func(r *Runtime, w, h int) Block { return indexPageView(r, id, w, h) }, TabSetting: PagesPinnedSetting}
	if err := r.RegisterPage(page, Kind(indexNoun)); err != nil {
		_ = r.RemoveInstance(id)
		return err
	}
	if _, err := r.AddDetail(detail, func() string {
		if item, ok := list.Selected(); ok {
			return item.ID
		}
		return ""
	}, Parent(id)); err != nil {
		_ = r.RemoveInstance(id)
		return err
	}
	return nil
}

// indexDetail is the selected thing's props as key: value lines, in prop
// order (secrets left out).
func (r *Runtime) indexDetail(n *Noun, item ListItem) string {
	thing, _ := item.Data.(Thing)
	var b strings.Builder
	width := 0
	for _, p := range n.Props {
		width = max(width, len(p.Name))
	}
	for _, p := range n.Props {
		if p.Secret || p.computed() {
			continue
		}
		v, ok := thing[p.Name]
		if !ok || v == nil {
			continue
		}
		fmt.Fprintf(&b, "%-*s  %s\n", width, p.Name, cellValueText(v))
	}
	return strings.TrimRight(b.String(), "\n")
}

// indexPageView draws an index page: the list and the selected thing's
// detail, side by side or stacked as the page split does.
func indexPageView(r *Runtime, id string, width, height int) Block {
	inst := r.Instances[id]
	list := r.List(id + "/list")
	if inst == nil || list == nil || width <= 0 || height <= 0 {
		return Block{}
	}
	ref, _ := inst.Data.(*indexPage)
	if ref == nil {
		return Block{}
	}
	n := r.Nouns[ref.Noun]
	styles := NewStyles(Appearance{Theme: "adaptive", Dark: true})
	if view := r.Settings().page; view.styles != nil {
		styles = view.styles(id)
	}
	layout := r.PaneLayout(id, Layout{Styles: styles})
	th := layout.Divide(max(min(list.Height()+headingLines(list)+columnHeaderLines(list)+2, height-6), PaneMinHeight), height, PaneMinHeight)
	tw, tth := PanelContentSize(width, th)
	tableBody := Text(list.View(tw, tth))
	dw, dch := PanelContentSize(width, height-th)
	title, text := "Detail", ""
	if item, ok := list.Selected(); ok && n != nil {
		title, text = item.Label, r.indexDetail(n, item)
	}
	content, marker := "", ""
	var body Block
	focused := SplitNone
	if list.DrawnFocused() {
		focused = SplitLeft
	}
	if form := r.RecordEditor(id + "/form"); form != nil && !form.Closed() && r.Settings().Bool(DetailShowFormSetting) {
		form.Styles = styles
		body = form.Block(dw, dch)
		if slices.Contains(r.InstancePath(r.DrawnFocus(id)), id+"/form") {
			focused = SplitRight
		}
	} else {
		if detail := r.Detail(id + "/detail"); detail != nil {
			content, marker = detail.Window(text, dw, dch)
		}
		body = Text(content)
	}
	if marker != "" {
		title += " · " + marker
	}
	heading := r.DisplayNoun(ref.Noun)
	if total := list.WindowTotal(); list.Windowed() {
		heading = fmt.Sprintf("%s · %d", heading, total)
	}
	return layout.panelPair(heading, tableBody, title, body, width, height, th, true, focused)
}

// columnHeaderLines is the line a table's column titles take, 0 for a
// plain list.
func columnHeaderLines(list *Collection) int {
	if len(list.columns) > 0 {
		return 1
	}
	return 0
}

// registerIndex registers the index component's verbs and noun.show.
func registerIndex(r *Runtime) {
	r.Define(indexNoun)
	r.Register(Operation{Key: KeyOf(indexNoun, "columns"), Description: "Choose the columns of a noun's index, remembered as index.columns.NOUN: props in order, none for the derived columns", Parameters: map[string]Parameter{
		"noun":  {Type: "string", Noun: "ochazuke.noun", Description: "The noun (default the index page's)"},
		"props": {Type: AnyParameterType, Description: "The props to show, in order (a list, or names separated by commas); empty for the derived columns"},
	}, Positional: []string{"props"}, Result: "string[]", HistoryKind: HistoryChange, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		noun := StringArg(a, "noun", "")
		if noun == "" {
			if inst := r.Instances[r.Page]; inst != nil {
				if ref, ok := inst.Data.(*indexPage); ok {
					noun = ref.Noun
				}
			}
		}
		n := r.Nouns[r.CanonicalNoun(noun)]
		if n == nil {
			return nil, &ArgumentError{fmt.Errorf("columns of which noun? pass noun=NAME or run it on an index page")}
		}
		props := listArg(a, "props")
		for _, p := range props {
			if n.prop(p) == nil {
				return nil, &ArgumentError{fmt.Errorf("%s has no prop %s", r.DisplayNoun(n.Name), p)}
			}
		}
		r.declareIndexColumns(n.Name)
		if err := r.Settings().Set(IndexColumnsSetting+n.Name, props); err != nil {
			return nil, err
		}
		if list := r.List(IndexPageID(n.Name) + "/list"); list != nil {
			list.SetColumns(r.IndexColumns(n.Name)...)
			_ = list.Refill()
		}
		return props, nil
	}})
	r.Define("ochazuke.noun").Verb("show", func(c Ctx) Result {
		noun := StringArg(c.Args, "noun", "")
		if err := r.OpenIndex(noun, StringArg(c.Args, "key", "")); err != nil {
			return Failed(err)
		}
		return Done(map[string]any{"page": IndexPageID(r.CanonicalNoun(noun))})
	}, "Show a noun's things: open its index page (nouns/NOUN), every noun's own even when an application page lists it too").Params(map[string]Parameter{
		"noun": {Type: "string", Required: true, Noun: "ochazuke.noun", From: "row:noun", Description: "The noun whose things to show"},
		"key":  {Type: "string", Description: "Select this thing's row"},
	})
	op := r.Operations.At(Key("ochazuke.noun.show"))
	op.HistoryKind, op.Agent, op.Positional = HistoryAction, AgentAllow, []string{"noun", "key"}
	r.Operations.Set(op)
}

// listArg is a list argument as words: a list, or one comma-separated text.
func listArg(a Arguments, name string) []string {
	switch v := a[name].(type) {
	case []string:
		return slices.Clone(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, w := range v {
			out = append(out, fmt.Sprint(w))
		}
		return out
	case string:
		return listWords(v)
	}
	return []string{}
}
