package ochazuke

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// The noun browser (noun scaffold plan, section f): the Nouns page lists
// every noun the program has, its own and its mounted extensions', with
// its kind, how many things it holds, how many of them are archived and
// what its store can do; command nouns are listed with their actions.
// Everything is read from the registry and the catalog data a noun
// declares, at run time: no noun has code of its own here.
//
// Counts are never read while a frame draws. They are read when the page
// opens and on noun-browser.refresh, one read per noun: a store that pages
// (Store.Query) answers a one-row page and its total, a remote store's
// read is a job off the loop, the registry's facts and live instances are
// counted where they are; a store that can only list everything is not
// counted (nouns.count-lists turns that on). A store with a ChangeToken is
// not read again while its token is unchanged.
const (
	// NounsPageID is the Nouns page's instance id.
	NounsPageID = "__nouns"
	// NounsListID is the Nouns page's list: a row per noun, its id
	// NounsListID + "/" + the noun.
	NounsListID = "__nouns/list"
	// NounsPageSetting puts the Nouns page in the tabs (default on; off it
	// is still opened by noun-browser.open, the page picker and the
	// palette).
	NounsPageSetting = "nouns.page"
	// NounsKindsSetting lists the noun kinds the Nouns page shows, in
	// group order (default every kind but part).
	NounsKindsSetting = "nouns.kinds"
	// NounsCountListsSetting counts a noun whose store can only list all
	// its things (no Query) by listing them (default off: such a count
	// shows –).
	NounsCountListsSetting = "nouns.count-lists"

	nounsPageNoun  = "ochazuke.noun-browser"
	nounsStateID   = "__nouns-state"
	nounsDetailID  = "__nouns/detail"
	nounsRowPrefix = NounsListID + "/"
)

// NounBrowser is the Nouns page's state: the counts read so far, by noun,
// and the page it was opened from.
type NounBrowser struct {
	counts map[string]*nounCount
	from   string
}

// nounCount is what is known of one noun's count.
type nounCount struct {
	count, archived int
	known, hasArch  bool
	reading         bool
	token           string
	err             string
}

// nounBrowser is the runtime's noun browser state, made on first use.
func (r *Runtime) nounBrowser() *NounBrowser {
	if inst := r.Instances[nounsStateID]; inst != nil {
		if b, ok := inst.Component.(*NounBrowser); ok {
			return b
		}
	}
	b := &NounBrowser{counts: map[string]*nounCount{}}
	r.Instance(nounsStateID, Kind("ochazuke.noun-browser")).Component = b
	return b
}

// NounCount is a noun's count as the noun browser last read it: its things,
// the archived ones among them (hasArchived false when the noun has no
// archive prop) and whether a count is known at all.
func (r *Runtime) NounCount(noun string) (count, archived int, known, hasArchived bool) {
	c := r.nounBrowser().counts[r.CanonicalNoun(noun)]
	if c == nil {
		return 0, 0, false, false
	}
	return c.count, c.archived, c.known, c.hasArch
}

// storeText says what a noun's store can do, for the Nouns page: where it
// lives (local or remote) and whether it pages and keeps a change token.
func storeText(s *Store) string {
	if s == nil {
		return ""
	}
	parts := []string{"local"}
	if s.Remote {
		parts[0] = "remote"
	}
	if s.Query != nil {
		parts = append(parts, "pages")
	}
	if s.ChangeToken != nil {
		parts = append(parts, "token")
	}
	return strings.Join(parts, " · ")
}

// countText is a count cell: the number, … while it is read, – when it is
// not known.
func (c *nounCount) countText(archived bool) string {
	switch {
	case c == nil || !c.known && !c.reading:
		return "–"
	case !c.known:
		return "…"
	case archived && !c.hasArch:
		return ""
	case archived:
		return strconv.Itoa(c.archived)
	}
	return strconv.Itoa(c.count)
}

// shownKinds are the kinds the Nouns page lists, in group order.
func (r *Runtime) shownKinds() []string {
	if r.Settings().find(NounsKindsSetting) == nil {
		return slices.Clone(NounKindList)
	}
	return r.Settings().Strings(NounsKindsSetting)
}

// nounBrowserRows are the Nouns page's rows: every noun of a shown kind, grouped
// by kind in nouns.kinds order, the application's and its extensions'
// nouns before the framework's, by name.
func (r *Runtime) nounBrowserRows() []ListItem {
	kinds := r.AllNounKinds()
	b := r.nounBrowser()
	var items []ListItem
	for _, kind := range r.shownKinds() {
		var names []string
		for name, k := range kinds {
			if k == kind {
				names = append(names, name)
			}
		}
		slices.SortFunc(names, func(a, c string) int {
			if ab, cb := r.Nouns[a].Builtin, r.Nouns[c].Builtin; ab != cb {
				if ab {
					return 1
				}
				return -1
			}
			return strings.Compare(a, c)
		})
		for _, name := range names {
			n := r.Nouns[name]
			fact := r.nounFactOf(n, kind)
			thing, _ := ThingOf(fact)
			c := b.counts[name]
			actions := ""
			if !StoredKind(kind) {
				actions = strings.Join(fact.Verbs, " ")
			}
			cells := []string{r.DisplayNoun(name), c.countText(false), c.countText(true), storeText(n.Store), actions}
			if !StoredKind(kind) {
				cells[1], cells[2] = "", ""
			}
			items = append(items, ListItem{ID: nounsRowPrefix + name, Label: name, Noun: "ochazuke.noun", Section: nounKindTable[kind].group, Data: thing, Cells: cells})
		}
	}
	return items
}

// nounFactOf is a noun as the ochazuke.noun facts noun lists it.
func (r *Runtime) nounFactOf(n *Noun, kind string) nounFact {
	fact := nounFact{Noun: n.Name, Verbs: r.Operations.ActionNames(n), Kind: kind, Store: storeText(n.Store)}
	if c := r.nounBrowser().counts[n.Name]; c != nil && c.known {
		count := c.count
		fact.Count = &count
		if c.hasArch {
			archived := c.archived
			fact.Archived = &archived
		}
	}
	return fact
}

// refreshNounsPage gives the page's list its rows again (no store is
// read); it does nothing while another page is current.
func refreshNounsPage(r *Runtime) {
	if r.Instances[NounsPageID] == nil || r.Page != NounsPageID {
		return
	}
	list := r.List(NounsListID)
	if list == nil {
		var err error
		if list, err = r.AddList(NounsListID, nil, Parent(NounsPageID), Columns(
			Column{Title: "Noun", Min: 8, Priority: 3},
			Column{Title: "Count", Align: AlignRight, Width: 7},
			Column{Title: "Archived", Align: AlignRight, Width: 8},
			Column{Title: "Store", Min: 5, Priority: 1},
			Column{Title: "Actions", Min: 7, Priority: 0},
		)); err != nil {
			return
		}
	}
	_ = list.SetItems(r.nounBrowserRows())
}

// countNouns reads the count of every shown stored noun whose count may
// have changed (its ChangeToken moved, or it has none), each off the frame:
// a job for a remote store, at once for the rest.
func countNouns(r *Runtime) {
	kinds := r.AllNounKinds()
	shown := map[string]bool{}
	for _, k := range r.shownKinds() {
		shown[k] = true
	}
	for _, name := range slices.Sorted(maps.Keys(kinds)) {
		if kind := kinds[name]; StoredKind(kind) && shown[kind] {
			countNoun(r, r.Nouns[name])
		}
	}
	refreshNounsPage(r)
}

// countNoun reads one noun's count, unless its store's ChangeToken says
// nothing changed since the last read or a read is under way.
func countNoun(r *Runtime, n *Noun) {
	b := r.nounBrowser()
	c := b.counts[n.Name]
	if c == nil {
		c = &nounCount{}
		b.counts[n.Name] = c
	}
	if c.reading {
		return
	}
	s := n.Store
	ctx := Ctx{actor: r.Actor(), Runtime: r, Context: r.Context(), Noun: n.Name}
	token := storeToken(s, ctx)
	if token != "" && token == c.token && c.known {
		return // nothing changed since it was counted
	}
	if s.Kind == Singleton {
		c.count, c.known, c.token = 1, true, token
		return
	}
	if s.Query == nil && s.List == nil && s.Kind != Instances {
		c.known = false
		return // a store that cannot list cannot be counted
	}
	if s.Query == nil && !s.Facts && s.Kind != Instances && !s.Remote && !r.Settings().Bool(NounsCountListsSetting) {
		c.known = false
		return // a store that can only list everything is not read to count it
	}
	arch := n.roleProp(RoleArchive)
	var total, archived int
	unmoved := false
	body := func(cx Ctx) (any, error) {
		if token, unmoved = jobToken(s, cx, token, c.token, c.known); unmoved {
			return nil, nil
		}
		// Every: the whole store, whatever its default list arguments
		// (ListQuery.Every); archived things in, then counted apart.
		page, err := r.queryThings(cx, n, ListQuery{Archived: true, Every: true, Page: 1, PerPage: 1})
		if err != nil {
			return nil, err
		}
		total = page.Total
		if arch != nil {
			page, err := r.queryThings(cx, n, ListQuery{Archived: true, Every: true, Page: 1, PerPage: 1, Filters: []PropFilter{{Prop: arch.Name, Value: true}}})
			if err != nil {
				return nil, err
			}
			archived = page.Total
		}
		return nil, nil
	}
	finish := func(_ any, err error) (any, error) {
		c.reading = false
		if err != nil {
			c.known, c.err = false, err.Error()
			refreshNounsPage(r)
			return nil, nil // a count that cannot be read is shown as unknown, never an error
		}
		if !unmoved {
			c.count, c.archived, c.known, c.hasArch, c.token, c.err = total, archived, true, arch != nil, token, ""
		}
		refreshNounsPage(r)
		return nil, nil
	}
	c.reading = true
	res := r.runSteppedBody(ctx, n.Name+".list", body, finish)
	if res.err != nil {
		c.reading, c.known = false, false
		return
	}
	if res.work == nil {
		return
	}
	if _, err := r.Launch("app", n.Name+".count", res.Then(func(Ctx, any, error) {}), ctx); err != nil {
		c.reading = false
	}
}

// recountOpenNounsPage reads the counts again after a change while the
// Nouns page is open: a store whose change token did not move is not read
// (countNoun), so this costs one token call per counted noun.
func recountOpenNounsPage(r *Runtime) {
	if r.Page == NounsPageID && r.Instances[NounsPageID] != nil {
		countNouns(r)
	}
}

// nounDetail is the Nouns page's detail of the selected noun: what it is,
// what its store can do, its count, its props and its verbs.
func (r *Runtime) nounDetail(name string) (title, text string) {
	n := r.Nouns[name]
	if n == nil {
		return "Detail", ""
	}
	kind := r.NounKind(name)
	var b strings.Builder
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%-9s %s\n", label, value)
		}
	}
	line("noun", name)
	line("about", n.About)
	line("kind", kind)
	line("store", storeText(n.Store))
	if c := r.nounBrowser().counts[name]; StoredKind(kind) {
		line("count", c.countText(false))
		if c != nil && c.hasArch {
			line("archived", c.countText(true))
		}
		if c != nil && c.err != "" {
			line("unread", c.err)
		}
	}
	if len(n.Props) > 0 {
		b.WriteString("\nprops\n")
		for _, p := range n.Props {
			typ := cmp.Or(p.Type, SettingString)
			if p.Key {
				typ += ", key"
			}
			if p.Role != "" {
				typ += ", " + p.Role
			}
			fmt.Fprintf(&b, "  %-16s %s\n", p.Name, typ)
		}
	}
	if verbs := r.Operations.ActionNames(n); len(verbs) > 0 {
		b.WriteString("\nverbs\n  " + strings.Join(verbs, " ") + "\n")
	}
	return r.DisplayNoun(name), strings.TrimRight(b.String(), "\n")
}

// EnsureNounsPage registers the Nouns page when it is not yet. Model.Init
// calls it after the application's pages and the About page, so it is the
// last tab (nouns.page off keeps it out of the tabs). Its rows are made and
// its counts read when it opens, never at startup. A page already titled
// Nouns keeps the title and the page is not registered.
func (r *Runtime) EnsureNounsPage() error {
	if r.PageOf(NounsPageID) != nil {
		return nil
	}
	for _, p := range r.PageList() {
		if p.Title == "Nouns" {
			return nil
		}
	}
	b := r.nounBrowser()
	r.Instance(NounsPageID, Parent("app"), Kind(nounsPageNoun))
	page := &Page{ID: NounsPageID, Title: "Nouns", Focus: NounsPageID, Render: nounsPageView, TabSetting: NounsPageSetting}
	page.OnOpen = func(r *Runtime, from string) {
		b.from = from
		refreshNounsPage(r)
		countNouns(r)
		if list := r.List(NounsListID); list != nil && r.Focus == NounsPageID {
			r.changeFocus(list.focusTarget(), focusMove{cause: focusSettle})
		}
	}
	if err := r.RegisterPage(page, Kind(nounsPageNoun)); err != nil {
		_ = r.RemoveInstance(NounsPageID)
		return err
	}
	if _, err := r.AddDetail(nounsDetailID, func() string { return selectedNounRow(r) }, Parent(NounsPageID)); err != nil {
		_ = r.RemoveInstance(NounsPageID)
		return err
	}
	return nil
}

// openNounsPage opens (or switches to) the Nouns page.
func openNounsPage(r *Runtime) error {
	if err := r.EnsureNounsPage(); err != nil {
		return err
	}
	if r.PageOf(NounsPageID) == nil {
		return fmt.Errorf("a page titled Nouns already exists")
	}
	if r.Page == NounsPageID {
		countNouns(r)
		return nil
	}
	return r.OpenPage(NounsPageID)
}

// selectedNounRow is the Nouns list's selected row id.
func selectedNounRow(r *Runtime) string {
	if list := r.List(NounsListID); list != nil {
		if item, ok := list.Selected(); ok {
			return item.ID
		}
	}
	return ""
}

// nounsPageView draws the Nouns page: the nouns list and the selected
// noun's detail, side by side or stacked as the page split does.
func nounsPageView(r *Runtime, width, height int) Block {
	if r.Instances[NounsPageID] == nil || width <= 0 || height <= 0 {
		return Block{}
	}
	styles := NewStyles(Appearance{Theme: "adaptive", Dark: true})
	if view := r.Settings().page; view.styles != nil {
		styles = view.styles(NounsPageID)
	}
	layout := r.PaneLayout(NounsPageID, Layout{Styles: styles})
	list := r.List(NounsListID)
	if list == nil {
		return Text("")
	}
	th := layout.Divide(max(min(list.Height()+headingLines(list)+columnHeaderLines(list)+2, height-6), PaneMinHeight), height, PaneMinHeight)
	tw, tth := PanelContentSize(width, th)
	tableBody := Text(list.View(tw, tth))
	dw, dch := PanelContentSize(width, height-th)
	title, text := "Detail", ""
	if id := selectedNounRow(r); id != "" {
		title, text = r.nounDetail(strings.TrimPrefix(id, nounsRowPrefix))
	}
	content, marker := "", ""
	if detail := r.Detail(nounsDetailID); detail != nil {
		content, marker = detail.Window(text, dw, dch)
	}
	if marker != "" {
		title += " · " + marker
	}
	focused := SplitNone
	if list.DrawnFocused() {
		focused = SplitLeft
	}
	return layout.panelPair("Nouns", tableBody, title, Text(content), width, height, th, true, focused)
}

// registerNounBrowser registers the noun browser's verbs.
func registerNounBrowser(r *Runtime) {
	r.nounBrowser()
	r.Register(Operation{Key: KeyOf(nounsPageNoun, "open"), Description: "Open the Nouns page: every noun with its kind, count, archived count and store; command nouns with their actions", Result: "context", HistoryKind: HistoryAction, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Context(), openNounsPage(r)
	}})
	r.Register(Operation{Key: KeyOf(nounsPageNoun, "refresh"), Description: "Read the Nouns page's counts again (a store whose change token is unchanged is not read)", Result: "context", HistoryKind: HistoryFetch, Agent: AgentAllow, Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		countNouns(r)
		return r.Context(), nil
	}})
}
