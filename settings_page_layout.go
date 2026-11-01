package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The Settings page's layout (settings.layout): which owners and sections
// (settings_owners.go) are tabs across the top and which are rows of the
// section list on the left.
//
//   - tabs+sections (the default): a tab per owner (drawn with more than
//     one), the shown tab's sections in the section list, the shown
//     section's settings over the detail on the right. Below
//     settings.narrow-width columns the section list folds into a selector
//     line at the top of the settings panel.
//   - tabs: a tab per section, every owner's, and no section list (the
//     page before owners).
//   - sections: no tabs; the section list holds every owner's sections,
//     each owner's under a heading row (Vim's :options contents).
//
// The section list (settingsSectionsID) is an ordinary List whose rows are
// the ochazuke.settings-section noun; it is drawn before the settings
// list, so j past its last row spills into the settings (R3.4). A section
// row's verbs (open, reset, write, revert, copy, pick) have twins at owner
// level on ochazuke.settings-tab, sharing the handlers. ] and [
// (settings.tab) step the tabs; tab and shift+tab (settings.section
// {across}) walk every section in drawing order.
const (
	// SettingsLayoutSetting is the Settings page's layout:
	// SettingsLayoutTabsSections (the default), SettingsLayoutTabs or
	// SettingsLayoutSections.
	SettingsLayoutSetting      = "settings.layout"
	SettingsLayoutTabsSections = "tabs+sections"
	SettingsLayoutTabs         = "tabs"
	SettingsLayoutSections     = "sections"
	// SettingsNarrowWidthSetting is the page width below which the section
	// list folds into a selector line; 0 never folds.
	SettingsNarrowWidthSetting = "settings.narrow-width"
	// SettingsSearchValuesSetting makes the page's / filters search every
	// setting: a section row matches by its settings' names, descriptions,
	// values, defaults and choices, the section list's filter spans every
	// tab, and the settings pane shows the shown section's matches. Off,
	// a section row matches by its title alone (the page before it).
	SettingsSearchValuesSetting = "settings.search-values"
	// SettingsAllSectionSetting lists an All section first in the section
	// list (and an All tab with settings.layout tabs): every setting in one
	// list, each under its tab and section. A / search in the section list
	// (with settings.search-values) lands on it, so every match shows at
	// once. Off, there is no All section and a search shows the shown
	// section's matches (the page before it).
	SettingsAllSectionSetting = "settings.all-section"

	settingsSectionNoun  = "ochazuke.settings-section"
	settingsTabNoun      = "ochazuke.settings-tab"
	settingsSectionsID   = "__settings/sections"
	settingSectionPrefix = "__settings/section/"
	settingsPickKind     = "settings-section-pick"
	// settingsAllID is the All section's id; it belongs to no owner, so its
	// unit key (/all) never clashes with a declared section's.
	settingsAllID = "all"
	// settingsSectionMin and settingsSectionMax clamp the section list's
	// panel width (its widest title and count, borders included).
	settingsSectionMin = 18
	settingsSectionMax = 26
	// settingsMark marks a tab or a section holding a session value
	// (settings.mark-session).
	settingsMark = "◦"
)

// settingsUnit is one section of one owner, as the page shows it.
type settingsUnit struct {
	OwnerTitle string
	SettingSectionInfo
}

func (u settingsUnit) key() string { return unitKey(u.Owner, u.ID) }

// gathers reports whether the unit is a view over other sections' settings
// (no owner: the All section) rather than their home. A gathering unit
// shows in every tab, its rows carry their home section as a heading and
// have ids of their own (rowPrefix), so a setting is a row in its home
// list and in the view at once.
func (u settingsUnit) gathers() bool { return u.Owner == "" }

// rowPrefix starts the ids of the unit's rows: __settings/row/NAME and
// __settings/item/NAME/I in a setting's home list, __settings/ID/row/NAME
// in a gathering unit's.
func (u settingsUnit) rowPrefix() string {
	if u.gathers() {
		return "__settings/" + u.ID + "/"
	}
	return "__settings/"
}

// unitKey names an owner's section: the page's settings list of it is
// __settings/list/KEY, its section row __settings/section/KEY.
func unitKey(owner, id string) string { return owner + "/" + id }

func settingsListID(key string) string { return "__settings/list/" + key }

// settingUnits is every owner's sections in drawing order, after the All
// section (settings.all-section) that gathers every setting.
func settingUnits(s *Settings) []settingsUnit {
	var out []settingsUnit
	all := settingsUnit{SettingSectionInfo: SettingSectionInfo{SettingSection: SettingSection{
		ID: settingsAllID, Title: "All", Description: "Every setting in one list, each under its tab and section",
	}}}
	for _, tab := range s.tabs() {
		for _, sec := range tab.Sections {
			out = append(out, settingsUnit{OwnerTitle: tab.Owner.Title, SettingSectionInfo: sec})
			all.Settings = append(all.Settings, sec.Settings...)
			all.Session += sec.Session
		}
	}
	if len(out) > 0 && s.Bool(SettingsAllSectionSetting) {
		out = append([]settingsUnit{all}, out...)
	}
	return out
}

// homeUnits maps each setting to its home section (the unit that owns it).
func homeUnits(units []settingsUnit) map[string]settingsUnit {
	out := map[string]settingsUnit{}
	for _, u := range units {
		if u.gathers() {
			continue
		}
		for _, name := range u.Settings {
			out[name] = u
		}
	}
	return out
}

// shownOwner is the owner whose tab shows with the shown unit: its own, or
// for a gathering unit the tab last shown (else the first).
func shownOwner(s *Settings, shown settingsUnit) string {
	if !shown.gathers() {
		return shown.Owner
	}
	tabs := s.tabs()
	if slices.ContainsFunc(tabs, func(t settingsOwnerTab) bool { return t.Owner.ID == s.page.tab }) {
		return s.page.tab
	}
	if len(tabs) > 0 {
		return tabs[0].Owner.ID
	}
	return ""
}

// show records u as the shown unit (and its owner as the shown tab).
func (view *settingsPage) show(u settingsUnit) {
	view.section = u.key()
	if !u.gathers() {
		view.tab = u.Owner
	}
}

func unitOf(units []settingsUnit, key string) (settingsUnit, bool) {
	i := slices.IndexFunc(units, func(u settingsUnit) bool { return u.key() == key })
	if i < 0 {
		return settingsUnit{}, false
	}
	return units[i], true
}

// settingsLayout is the settings.layout value in effect.
func settingsLayout(s *Settings) string {
	switch v := s.String(SettingsLayoutSetting); v {
	case SettingsLayoutTabs, SettingsLayoutSections:
		return v
	}
	return SettingsLayoutTabsSections
}

// marks reports whether the page marks session values
// (settings.mark-session).
func settingsMarks(s *Settings) bool { return s.Bool(SettingsMarkSessionSetting) }

// listedUnits are the units the section list holds for the shown unit: the
// shown owner's (tabs+sections), every owner's (sections), none (tabs); a
// gathering unit (All) in every tab.
func listedUnits(s *Settings, units []settingsUnit, shown settingsUnit) []settingsUnit {
	switch settingsLayout(s) {
	case SettingsLayoutTabs:
		return nil
	case SettingsLayoutSections:
		return units
	}
	owner := shownOwner(s, shown)
	return slices.DeleteFunc(slices.Clone(units), func(u settingsUnit) bool { return !u.gathers() && u.Owner != owner })
}

// currentSection is the unit the page shows: the focused settings list's,
// the section list's selected row while it has the focus, else the one last
// shown, else the first home section (the page opens on a section, not on
// the All view).
func currentSection(r *Runtime) string {
	s := r.Settings()
	units := settingUnits(s)
	if len(units) == 0 {
		return ""
	}
	for _, u := range units {
		if list := r.List(settingsListID(u.key())); list != nil && list.Focused() {
			s.page.show(u)
			return u.key()
		}
	}
	if list := r.List(settingsSectionsID); list != nil && list.Focused() {
		if item, ok := list.Selected(); ok {
			if key, _ := item.Data.(string); key != "" {
				if u, ok := unitOf(units, key); ok {
					s.page.show(u)
					return key
				}
			}
		}
	}
	if _, ok := unitOf(units, s.page.section); ok {
		return s.page.section
	}
	if i := slices.IndexFunc(units, func(u settingsUnit) bool { return !u.gathers() }); i >= 0 {
		return units[i].key()
	}
	return units[0].key()
}

// settingRowID is the id of name's row (or of its item index, when index
// >= 0) in the list the page shows when that list holds it (the All view's
// own row), else in its home list: where a change made from a row puts the
// focus back.
func settingRowID(r *Runtime, name string, index int) string {
	prefix := "__settings/"
	if u, ok := unitOf(settingUnits(r.Settings()), currentSection(r)); ok && slices.Contains(u.Settings, name) {
		prefix = u.rowPrefix()
	}
	if index >= 0 {
		return fmt.Sprintf("%sitem/%s/%d", prefix, name, index)
	}
	return prefix + "row/" + name
}

// selectedSettingRow is the detail's item: the shown list's selected row
// ("" while the section list has the focus: the detail is the section's).
func selectedSettingRow(r *Runtime) string {
	if list := r.List(settingsSectionsID); list != nil && list.Focused() {
		return ""
	}
	if list := r.List(settingsListID(currentSection(r))); list != nil {
		if item, ok := list.Selected(); ok {
			return item.ID
		}
	}
	return ""
}

// refreshSettingsPage brings the open page up to date: a list per owner's
// section (a section declared since gets one), each list's rows, and the
// section list's rows; selections stay on their rows.
func refreshSettingsPage(r *Runtime) {
	if r.Instances[SettingsPageID] == nil {
		return
	}
	s := r.Settings()
	if r.List(settingsSectionsID) == nil {
		// The section list first, so the tree order (the stops' order
		// before the first frame) is the drawing order.
		if _, err := r.AddList(settingsSectionsID, nil, Parent(SettingsPageID), NoMarks(), SpillToSelection()); err != nil {
			return
		}
	}
	values := map[string]SettingValue{}
	for _, v := range s.List() {
		values[v.Name] = v
	}
	units := settingUnits(s)
	homes := homeUnits(units)
	// The gathering lists (All) are added after the home ones, so before
	// the first frame (tree order) k from the first section's settings
	// still spills to the section list.
	ordered := slices.Concat(
		slices.DeleteFunc(slices.Clone(units), settingsUnit.gathers),
		slices.DeleteFunc(slices.Clone(units), func(u settingsUnit) bool { return !u.gathers() }),
	)
	for _, u := range ordered {
		var items []ListItem
		names := 0
		for _, name := range u.Settings {
			v := values[name]
			row := settingItem(s, v)
			heading := v.Group
			if u.gathers() {
				// A gathered row sits under its home: tab › section (›
				// group).
				home := homes[name]
				heading = home.OwnerTitle + " › " + home.Title
				if v.Group != "" {
					heading += " › " + v.Group
				}
			}
			row.ID, row.Section = u.rowPrefix()+"row/"+name, heading
			names = max(names, ansi.StringWidth(strings.TrimSuffix(row.Cells[0], " •")))
			items = append(items, row)
			if list, ok := v.Value.([]string); ok {
				for i, text := range list {
					items = append(items, ListItem{
						ID: fmt.Sprintf("%sitem/%s/%d", u.rowPrefix(), v.Name, i), Label: text, Noun: settingItemNoun, Section: heading,
						Data: settingItemRef{Name: v.Name, Index: i}, Cells: []string{"", textCell(text)},
					})
				}
			}
		}
		list := r.List(settingsListID(u.key()))
		if list == nil {
			var err error
			if list, err = r.AddList(settingsListID(u.key()), nil, Parent(SettingsPageID), NoMarks()); err != nil {
				continue
			}
		}
		list.SetMatchNoun("settings")
		// No titles, so no header. The name column is as wide as the
		// longest name and its dot (up to 30 cells, then a name is cut),
		// whether a dot shows or not, so a change never moves the values;
		// the value takes the rest.
		list.SetColumns(
			Column{Width: min(names+2, 30)},
			Column{Min: 6, Priority: 1},
		)
		_ = list.SetItems(items)
	}
	syncSectionList(r, currentSection(r))
}

// sectionCell is a section's title as its row and the tabs show it, with
// the session mark when one of its settings holds a session value.
func sectionCell(s *Settings, title string, session int) string {
	if session > 0 && settingsMarks(s) {
		return title + " " + settingsMark
	}
	return title
}

// syncSectionList gives the section list the rows of the shown unit's
// (key's) owner (or every owner's, or none, by settings.layout) and selects
// the shown unit's row.
func syncSectionList(r *Runtime, key string) {
	if r.Instances[SettingsPageID] == nil {
		return
	}
	s := r.Settings()
	list := r.List(settingsSectionsID)
	if list == nil {
		var err error
		if list, err = r.AddList(settingsSectionsID, nil, Parent(SettingsPageID), NoMarks(), SpillToSelection()); err != nil {
			return
		}
	}
	values := s.searchValues()
	list.OnFilter(settingsSectionFiltered)
	list.SetMatchNoun("sections")
	units := settingUnits(s)
	shown, _ := unitOf(units, key)
	listed := listedUnits(s, units, shown)
	headings := settingsLayout(s) == SettingsLayoutSections
	if values && list.Filter() != "" && settingsLayout(s) == SettingsLayoutTabsSections {
		// A search spans every tab: every owner's sections, each under its
		// owner's heading when there is more than one.
		listed, headings = units, len(s.tabs()) > 1
	}
	var search map[string][]string
	if values {
		list.SetMatchNoun("settings")
		search = map[string][]string{}
		for _, v := range s.List() {
			row := settingItem(s, v)
			search[v.Name] = append(append([]string{row.Label}, row.Cells...), row.Search...)
			if items, ok := v.Value.([]string); ok {
				search[v.Name] = append(search[v.Name], items...)
			}
		}
	}
	items := make([]ListItem, 0, len(listed))
	titles, counts := 0, 0
	for _, u := range listed {
		title := sectionCell(s, u.Title, u.Session)
		count := strconv.Itoa(len(u.Settings))
		titles, counts = max(titles, ansi.StringWidth(title)), max(counts, len(count))
		item := ListItem{ID: settingSectionPrefix + u.key(), Label: u.Title, Noun: settingsSectionNoun, Data: u.key(), Cells: []string{title, count}}
		if headings {
			item.Section = u.OwnerTitle
		}
		switch {
		case values && u.gathers():
			// A gathering row (All) holds every setting, whose text
			// together matches almost any query of several words: it
			// matches by the settings that match one by one, so it shows
			// exactly when its list has a match.
			if query := list.Filter(); query != "" {
				m := list.matcher()
				for _, name := range u.Settings {
					if m.Match(strings.Join(search[name], "\n")) {
						item.Search = append(item.Search, search[name]...)
					}
				}
			}
		case values:
			item.Search = append(item.Search, u.ID, u.Description)
			for _, name := range u.Settings {
				item.Search = append(item.Search, search[name]...)
			}
		}
		items = append(items, item)
	}
	list.SetColumns(Column{Min: max(titles, 4), Priority: 1}, Column{Width: max(counts, 1), Align: AlignRight})
	_ = list.SetItems(items)
	if !list.Focused() && shown.ID != "" && slices.ContainsFunc(items, func(it ListItem) bool { return it.ID == settingSectionPrefix+key }) {
		_ = list.Select(settingSectionPrefix + key)
	}
}

// searchValues reports whether the page's filters search every setting
// (settings.search-values).
func (s *Settings) searchValues() bool { return s.Bool(SettingsSearchValuesSetting) }

// settingsSectionFiltered follows the section list's filter (its OnFilter,
// with settings.search-values on): the list spans every tab while it
// filters and the shown tab again when it clears, and each section's
// settings list takes the same query and match mode, so the settings pane
// shows the shown section's matching settings. A section that shows by
// its own title or line, none of its settings matching, shows all of
// them. A settings list the user filtered itself keeps its filter.
func settingsSectionFiltered(sections *List) {
	r := sections.r
	s := r.Settings()
	if r.Instances[SettingsPageID] == nil || !s.searchValues() {
		return
	}
	query := sections.Filter()
	key := currentSection(r)
	if query == "" && sections.back != "" {
		// esc in the line: the list narrows to the tab of the row it opened on.
		key = strings.TrimPrefix(sections.back, settingSectionPrefix)
	}
	syncSectionList(r, key)
	previous := s.page.searched
	s.page.searched = query
	var gather string
	for _, u := range settingUnits(s) {
		list := r.List(settingsListID(u.key()))
		if list == nil || list.Filter() != "" && list.Filter() != previous {
			continue
		}
		list.match = sections.match
		list.SetFilter(query)
		if query != "" && list.Shown() == 0 && sections.shows(settingSectionPrefix+u.key()) {
			list.SetFilter("")
		}
		if gather == "" && u.gathers() && list.Shown() > 0 {
			gather = settingSectionPrefix + u.key()
		}
	}
	// A search lands on the All section (settings.all-section), so the
	// settings pane shows every match at once; j and k still step to a
	// section's own matches.
	if query != "" && gather != "" && sections.shows(gather) {
		_ = sections.Select(gather)
	}
}

// showUnit makes key the shown unit and brings the section list to it,
// without moving the focus.
func showUnit(r *Runtime, key string) {
	s := r.Settings()
	unit, ok := unitOf(settingUnits(s), key)
	if !ok {
		return
	}
	s.page.show(unit)
	if s.page.lastOf == nil {
		s.page.lastOf = map[string]string{}
	}
	if !unit.gathers() {
		s.page.lastOf[unit.Owner] = key
	}
	syncSectionList(r, key)
}

// focusUnit shows key and moves the focus to it: to its section row while
// the section list has the focus, else to its settings list's selected row.
func focusUnit(r *Runtime, key string) error {
	inSections := false
	if list := r.List(settingsSectionsID); list != nil && list.Focused() {
		inSections = true
	}
	showUnit(r, key)
	if inSections && !r.Settings().page.narrow {
		if list := r.List(settingsSectionsID); list != nil && list.Select(settingSectionPrefix+key) == nil {
			return r.SetFocus(settingSectionPrefix + key)
		}
	}
	if inSections && r.Settings().page.narrow {
		return r.SetFocus(settingSectionPrefix + key)
	}
	list := r.List(settingsListID(key))
	if list == nil {
		return nil
	}
	if item, ok := list.Selected(); ok {
		return r.SetFocus(item.ID)
	}
	return r.SetFocus(settingsListID(key))
}

// findUnit is the unit name names in units (its key, its ID or its title,
// case ignored), else in every owner's.
func findUnit(s *Settings, units []settingsUnit, name string) (settingsUnit, error) {
	match := func(in []settingsUnit) (settingsUnit, bool) {
		for _, u := range in {
			if u.key() == name || u.ID == name || strings.EqualFold(u.Title, name) {
				return u, true
			}
		}
		return settingsUnit{}, false
	}
	if u, ok := match(units); ok {
		return u, nil
	}
	all := settingUnits(s)
	if u, ok := match(all); ok {
		return u, nil
	}
	titles := make([]string, len(all))
	for i, u := range all {
		titles[i] = u.Title
	}
	return settingsUnit{}, fmt.Errorf("no settings section %q: one of %s", name, strings.Join(titles, ", "))
}

// showSettingsSection shows the named section, else the one step away from
// the shown one (wrapping): within the shown tab, or with across (and in
// the layouts with no owner tabs) over every owner's sections in drawing
// order.
func showSettingsSection(r *Runtime, name string, step int, across bool) error {
	if r.PageOf(SettingsPageID) == nil {
		return errors.New("the settings page is not open")
	}
	s := r.Settings()
	units := settingUnits(s)
	if len(units) == 0 {
		return errors.New("this application declares no settings")
	}
	key := currentSection(r)
	shown, _ := unitOf(units, key)
	seq := units
	if !across && settingsLayout(s) == SettingsLayoutTabsSections {
		seq = listedUnits(s, units, shown)
	}
	if name != "" {
		u, err := findUnit(s, seq, name)
		if err != nil {
			return err
		}
		return focusUnit(r, u.key())
	}
	i := slices.IndexFunc(seq, func(u settingsUnit) bool { return u.key() == key })
	n := len(seq)
	return focusUnit(r, seq[((i+step)%n+n)%n].key())
}

// SettingsTabLink is a tab of the Settings page that is a page of its own
// (the About page): drawn after the owners' tabs while Setting (a boolean
// setting; "" always) is on and settings.layout draws tabs. ] and [ step
// onto it by running Open, and from its page back onto the Settings page;
// a step leaving either page runs the page's close first (Close, or
// settings.close), so the page the pair was opened from stays the one q
// goes back to. The linked page draws the tab row itself
// (SettingsTabTitles).
type SettingsTabLink struct {
	Title   string  `json:"title"`
	Page    string  `json:"page"`
	Open    Request `json:"open"`
	Close   Request `json:"close,omitzero"`
	Setting string  `json:"setting,omitempty"`
}

// LinkSettingsTab adds a tab to the Settings page that opens another page;
// a link to the same page again replaces it where it stands.
func (r *Runtime) LinkSettingsTab(link SettingsTabLink) {
	view := &r.Settings().page
	if i := slices.IndexFunc(view.links, func(l SettingsTabLink) bool { return l.Page == link.Page }); i >= 0 {
		view.links[i] = link
		return
	}
	view.links = append(view.links, link)
}

// settingsLinkKey is a link's key among the tabs' unit keys.
func settingsLinkKey(page string) string { return "link:" + page }

// shownLinks are the links drawn as tabs: their setting on, in a layout
// with tabs, and with settings to show beside them.
func (s *Settings) shownLinks() []SettingsTabLink {
	if settingsLayout(s) == SettingsLayoutSections || len(s.declared) == 0 {
		return nil
	}
	var out []SettingsTabLink
	for _, l := range s.page.links {
		if l.Setting == "" || s.Bool(l.Setting) {
			out = append(out, l)
		}
	}
	return out
}

// SettingsTabTitles are the Settings page's tab titles and the index of
// the one linked to page (-1 when page is not a shown link): what a linked
// page draws as its tab row.
func (r *Runtime) SettingsTabTitles(page string) ([]string, int) {
	s := r.Settings()
	titles, keys := settingsTabs(s, settingUnits(s))
	for i, ks := range keys {
		if len(ks) == 1 && ks[0] == settingsLinkKey(page) {
			return titles, i
		}
	}
	return titles, -1
}

// settingsTabs are the tabs the page draws: one per owner, or one per
// section with settings.layout tabs; none with sections; then the shown
// links. Each tab is the units it holds (a link's: its link key).
func settingsTabs(s *Settings, units []settingsUnit) (titles []string, keys [][]string) {
	titles, keys = settingsOwnerTabs(s, units)
	if len(titles) == 0 {
		return nil, nil
	}
	for _, l := range s.shownLinks() {
		titles = append(titles, l.Title)
		keys = append(keys, []string{settingsLinkKey(l.Page)})
	}
	return titles, keys
}

// activeTab is the index of the tab showing the unit key: the tab holding
// it, else (a gathering unit in tabs+sections) the shown owner's.
func activeTab(s *Settings, keys [][]string, key string) int {
	if i := slices.IndexFunc(keys, func(ks []string) bool { return slices.Contains(ks, key) }); i >= 0 {
		return i
	}
	shown, ok := unitOf(settingUnits(s), key)
	if !ok || !shown.gathers() {
		return -1
	}
	owner := shownOwner(s, shown)
	return slices.IndexFunc(keys, func(ks []string) bool {
		return slices.ContainsFunc(ks, func(k string) bool { return strings.HasPrefix(k, owner+"/") })
	})
}

// settingsOwnerTabs are the owners' (or the sections') tabs.
func settingsOwnerTabs(s *Settings, units []settingsUnit) (titles []string, keys [][]string) {
	layout := settingsLayout(s)
	if layout == SettingsLayoutSections {
		return nil, nil
	}
	if layout == SettingsLayoutTabs {
		seen := map[string]int{}
		for _, u := range units {
			seen[u.Title]++
		}
		for _, u := range units {
			title := u.Title
			if seen[u.Title] > 1 {
				title = u.OwnerTitle + " › " + u.Title
			}
			titles = append(titles, sectionCell(s, title, u.Session))
			keys = append(keys, []string{u.key()})
		}
		return titles, keys
	}
	for _, tab := range s.tabs() {
		var ks []string
		for _, sec := range tab.Sections {
			ks = append(ks, unitKey(tab.Owner.ID, sec.ID))
		}
		titles = append(titles, sectionCell(s, tab.Owner.Title, tab.Owner.Session))
		keys = append(keys, ks)
	}
	return titles, keys
}

// showSettingsTab shows the named tab (an owner's ID or title; a section's
// with settings.layout tabs), else the one step away (wrapping), on the
// section it last showed. With settings.layout sections it steps the
// owners, landing on an owner's first section.
func showSettingsTab(r *Runtime, name string, step int) error {
	s := r.Settings()
	if done, err := stepSettingsLink(r, name, step); done {
		return err
	}
	if r.PageOf(SettingsPageID) == nil {
		return errors.New("the settings page is not open")
	}
	units := settingUnits(s)
	if len(units) == 0 {
		return errors.New("this application declares no settings")
	}
	if settingsLayout(s) == SettingsLayoutTabs {
		return showSettingsSection(r, name, step, true)
	}
	key := currentSection(r)
	shown, _ := unitOf(units, key)
	tabs := s.tabs()
	owner := shownOwner(s, shown)
	at := slices.IndexFunc(tabs, func(t settingsOwnerTab) bool { return t.Owner.ID == owner })
	if name != "" {
		at = slices.IndexFunc(tabs, func(t settingsOwnerTab) bool { return t.Owner.ID == name || strings.EqualFold(t.Owner.Title, name) })
		if at < 0 {
			return s.unknownOwner(name)
		}
	} else {
		n := len(tabs)
		at = ((at+step)%n + n) % n
	}
	tab := tabs[at]
	target := unitKey(tab.Owner.ID, tab.Sections[0].ID)
	if last := s.page.lastOf[tab.Owner.ID]; last != "" && settingsLayout(s) == SettingsLayoutTabsSections {
		if _, ok := unitOf(units, last); ok {
			target = last
		}
	}
	return focusUnit(r, target)
}

// stepSettingsLink runs settings.tab where a link (LinkSettingsTab) is
// involved: a name naming a link, a name or step from a linked page, or a
// step from the Settings page that lands past its owners' tabs. done is
// false when no link is involved, for showSettingsTab's own steps.
func stepSettingsLink(r *Runtime, name string, step int) (bool, error) {
	s := r.Settings()
	links := s.shownLinks()
	if len(links) == 0 {
		return false, nil
	}
	_, keys := settingsOwnerTabs(s, settingUnits(s))
	n := len(keys)
	cur := slices.IndexFunc(links, func(l SettingsTabLink) bool { return l.Page == r.Page })
	if cur >= 0 {
		cur += n
	} else if r.Page == SettingsPageID {
		cur = activeTab(s, keys, currentSection(r))
	}
	if cur < 0 {
		// Neither the Settings page nor a link is current: only a
		// link's name is ours.
		if i := linkNamed(links, name); i >= 0 {
			return true, switchSettingsTab(r, n+i, links, n)
		}
		return false, nil
	}
	target := -1
	if name != "" {
		if i := linkNamed(links, name); i >= 0 {
			target = n + i
		} else if cur < n {
			return false, nil
		}
	} else {
		total := n + len(links)
		target = ((cur+step)%total + total) % total
		if cur < n && target < n {
			return false, nil
		}
	}
	if target < 0 {
		// A name from a linked page: an owner's or a section's, which
		// the Settings page resolves once it is open.
		if err := leaveSettingsTab(r, links, n, cur); err != nil {
			return true, err
		}
		if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "edit"}); err != nil {
			return true, err
		}
		return true, showSettingsTab(r, name, 0)
	}
	if target == cur {
		return true, nil
	}
	if err := leaveSettingsTab(r, links, n, cur); err != nil {
		return true, err
	}
	return true, switchSettingsTab(r, target, links, n)
}

// linkNamed is the index of the link named name (its page or title), -1
// for none.
func linkNamed(links []SettingsTabLink, name string) int {
	if name == "" {
		return -1
	}
	return slices.IndexFunc(links, func(l SettingsTabLink) bool { return l.Page == name || strings.EqualFold(l.Title, name) })
}

// leaveSettingsTab closes the tab at cur (the Settings page or a link's
// page) when it is current, so the next one opens from the page the pair
// was opened from. A close that cannot go back (no other page) leaves the
// page open.
func leaveSettingsTab(r *Runtime, links []SettingsTabLink, n, cur int) error {
	switch {
	case cur < 0:
		return nil
	case cur < n:
		if r.Page == SettingsPageID {
			_ = closeSettingsPage(r)
		}
	default:
		if l := links[cur-n]; r.Page == l.Page && l.Close.Noun != "" {
			_, _ = r.Execute(l.Close)
		}
	}
	return nil
}

// switchSettingsTab shows tab target: a link's page by its Open, else the
// Settings page on that owner's (or section's) tab.
func switchSettingsTab(r *Runtime, target int, links []SettingsTabLink, n int) error {
	if target >= n {
		_, err := r.Execute(links[target-n].Open)
		return err
	}
	s := r.Settings()
	if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "edit"}); err != nil {
		return err
	}
	if settingsLayout(s) == SettingsLayoutTabs {
		return showSettingsSection(r, settingUnits(s)[target].key(), 0, true)
	}
	return showSettingsTab(r, s.tabs()[target].Owner.ID, 0)
}

// unitsOfArgs are the units a section or tab verb acts on: the named
// section (section) or owner (owner), else the focused section row's, else
// the shown one's.
func unitsOfArgs(r *Runtime, c Context, a Arguments, owner bool) ([]settingsUnit, error) {
	s := r.Settings()
	units := settingUnits(s)
	if len(units) == 0 {
		return nil, errors.New("this application declares no settings")
	}
	key := ""
	if inst := r.Instances[focusOf(c)]; inst != nil && inst.Noun == settingsSectionNoun {
		key, _ = inst.Data.(string)
	}
	if key == "" {
		key = currentSection(r)
	}
	shown, _ := unitOf(units, key)
	if owner {
		id := StringArg(a, "owner", shownOwner(s, shown))
		var out []settingsUnit
		for _, u := range units {
			if u.Owner == id || strings.EqualFold(u.OwnerTitle, id) {
				out = append(out, u)
			}
		}
		if len(out) == 0 {
			return nil, s.unknownOwner(id)
		}
		return out, nil
	}
	if name := StringArg(a, "section", ""); name != "" {
		u, err := findUnit(s, listedUnits(s, units, shown), name)
		if err != nil {
			return nil, err
		}
		return []settingsUnit{u}, nil
	}
	if shown.ID == "" {
		return nil, errors.New("no settings section is shown")
	}
	return []settingsUnit{shown}, nil
}

func unitNames(units []settingsUnit) []string {
	var out []string
	for _, u := range units {
		out = append(out, u.Settings...)
	}
	return out
}

// settingsScopeVerbs are the verbs a section row and a tab share: each
// handler acts on every setting of the units.
var settingsScopeVerbs = []struct {
	verb, section, tab string
	run                func(r *Runtime, c Context, a Arguments, units []settingsUnit) (any, error)
	scoped             bool
}{
	{"open", "Show the section and focus its settings", "Show the tab on the section it last showed and focus its settings", runScopeOpen, false},
	{"reset", "Reset every setting of the section to its default, one undo step", "Reset every setting of the tab to its default, one undo step", runScopeReset, true},
	{"write", "Write the session values of the section's settings to settings.json", "Write the session values of the tab's settings to settings.json", runScopeWrite, false},
	{"revert", "Drop the session values of the section's settings, so the saved ones show again", "Drop the session values of the tab's settings, so the saved ones show again", runScopeRevert, false},
	{"copy", "Yank the section's settings that are not at their default as :set lines", "Yank the tab's settings that are not at their default as :set lines", runScopeCopy, false},
}

func runScopeOpen(r *Runtime, c Context, a Arguments, units []settingsUnit) (any, error) {
	if r.PageOf(SettingsPageID) == nil {
		if _, err := r.Execute(Request{Noun: "ochazuke.settings", Verb: "edit"}); err != nil {
			return nil, err
		}
	}
	key := units[0].key()
	if last := r.Settings().page.lastOf[units[0].Owner]; len(units) > 1 && last != "" {
		if _, ok := unitOf(units, last); ok {
			key = last
		}
	}
	showUnit(r, key)
	list := r.List(settingsListID(key))
	if list == nil {
		return key, nil
	}
	if item, ok := list.Selected(); ok {
		return key, r.SetFocus(item.ID)
	}
	return key, r.SetFocus(settingsListID(key))
}

func runScopeReset(r *Runtime, c Context, a Arguments, units []settingsUnit) (any, error) {
	scope, err := settingScope(a)
	if err != nil {
		return nil, err
	}
	reset, _ := settingVerbOf("reset")
	s := r.Settings()
	var done []string
	err = settingsGroup(r, func() error {
		for _, name := range unitNames(units) {
			one := s.find(name)
			if one == nil || !settingDiffers(s, name) && !s.differsInSession(*one) {
				continue
			}
			if _, err := runSettingVerb(reset)(r, c, Arguments{"name": name, "scope": scope}); err != nil {
				return err
			}
			done = append(done, name)
		}
		return nil
	})
	if done == nil {
		done = []string{}
	}
	return done, err
}

func runScopeWrite(r *Runtime, c Context, a Arguments, units []settingsUnit) (any, error) {
	done := []string{}
	for _, name := range unitNames(units) {
		wrote, err := r.Settings().Write(name)
		if err != nil {
			return done, err
		}
		if wrote {
			done = append(done, name)
		}
	}
	return done, nil
}

func runScopeRevert(r *Runtime, c Context, a Arguments, units []settingsUnit) (any, error) {
	s := r.Settings()
	done := []string{}
	for _, name := range unitNames(units) {
		if _, ok := s.session[name]; !ok {
			continue
		}
		if err := s.Revert(name); err != nil {
			return done, err
		}
		done = append(done, name)
	}
	return done, nil
}

func runScopeCopy(r *Runtime, c Context, a Arguments, units []settingsUnit) (any, error) {
	s := r.Settings()
	var lines []string
	for _, name := range unitNames(units) {
		if !settingDiffers(s, name) {
			continue
		}
		word := settingWord(s.current(*s.find(name)))
		if strings.ContainsAny(word, " \t") {
			word = strconv.Quote(word)
		}
		lines = append(lines, "set "+name+"="+word)
	}
	if len(lines) == 0 {
		return []string{}, errors.New("every setting here is at its default: nothing to copy")
	}
	if err := r.storeYank(Yank{Text: strings.Join(lines, "\n"), Shape: "line"}, "", false); err != nil {
		return nil, err
	}
	return lines, nil
}

// openSectionPicker opens a chooser of the shown tab's sections (the
// narrow selector's s); its pick shows the section.
func openSectionPicker(r *Runtime) error {
	s := r.Settings()
	units := settingUnits(s)
	shown, _ := unitOf(units, currentSection(r))
	listed := listedUnits(s, units, shown)
	if len(listed) == 0 {
		listed = units
	}
	rows := make([]ChooserRow, len(listed))
	for i, u := range listed {
		rows[i] = ChooserRow{ID: u.key(), Label: u.Title, Cells: []string{sectionCell(s, u.Title, u.Session), strconv.Itoa(len(u.Settings))}}
	}
	inSections := false
	if list := r.List(settingsSectionsID); list != nil && list.Focused() {
		inSections = true
	}
	_, err := r.OpenChooser(ChooserSpec{Kind: settingsPickKind, Title: "Sections", Rows: rows, Choose: func(row ChooserRow) Result {
		showUnit(r, row.ID)
		if inSections {
			_ = r.SetFocus(settingSectionPrefix + row.ID)
		}
		return Done(row.ID)
	}})
	return err
}

// registerSettingsLayoutView registers settings.tab, settings.section, the
// section and tab nouns' verbs and the settings-narrow guard.
func registerSettingsLayoutView(r *Runtime) {
	r.Register(Operation{Verb: "section", Noun: "ochazuke.settings", Description: "Show the next or previous section of the Settings page (wrapping), or the one named: within the shown tab, or across every tab in drawing order", Parameters: map[string]Parameter{
		"step":   {Type: "integer", Description: "1 for the next section, -1 for the previous"},
		"name":   {Type: "string", Description: "The section to show: its title or id (General for the settings without one)"},
		"across": {Type: "boolean", Description: "Step over every tab's sections in drawing order, crossing into the next tab after a tab's last section"},
	}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		across, _ := a["across"].(bool)
		return r.Context(), showSettingsSection(r, StringArg(a, "name", ""), IntArg(a, "step", 1), across)
	}})
	r.Register(Operation{Verb: "tab", Noun: "ochazuke.settings", Description: "Show the next or previous tab of the Settings page (wrapping), or the one named, on the section it last showed", Parameters: map[string]Parameter{
		"step": {Type: "integer", Description: "1 for the next tab, -1 for the previous"},
		"name": {Type: "string", Description: "The tab to show: an owner's id or title (app, ochazuke, ...)"},
	}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Context(), showSettingsTab(r, StringArg(a, "name", ""), IntArg(a, "step", 1))
	}})
	r.Register(Operation{Verb: "all", Noun: "ochazuke.settings", Description: "Show the Settings page's All section: every setting in one list, each under its tab and section (opens the page when it is closed)", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		if !r.Settings().Bool(SettingsAllSectionSetting) {
			return nil, errors.New("there is no All section: settings.all-section is off")
		}
		if _, err := r.Execute(Request{Noun: settingsSectionNoun, Verb: "open", Args: Arguments{"section": unitKey("", settingsAllID)}}); err != nil {
			return nil, err
		}
		return r.Context(), nil
	}})
	r.DefineGuard("settings-all-section", func(r *Runtime, ev KeyEvent) bool {
		return r.Settings().Bool(SettingsAllSectionSetting)
	})
	r.SetAvailability("ochazuke.settings.all", "settings-all-section")
	r.DefineGuard("settings-narrow", func(r *Runtime, ev KeyEvent) bool {
		return r.Settings().page.narrow
	})
	scope := Parameter{Type: "string", Enum: []string{ScopeSession, ScopeFile}, Description: "Where the reset is written: session (the default) or file"}
	for _, v := range settingsScopeVerbs {
		for _, owner := range []bool{false, true} {
			noun, params := settingsSectionNoun, map[string]Parameter{
				"section": {Type: "string", Description: "The section: its title or id (default: the focused section row, else the shown section)"},
			}
			what := v.section
			if owner {
				noun, params = settingsTabNoun, map[string]Parameter{
					"owner": {Type: "string", Description: "The tab: an owner's id or title (default: the shown tab; settings.owners lists them)"},
				}
				what = v.tab
			}
			if v.scoped {
				params["scope"] = scope
			}
			run := v.run
			r.Register(Operation{Verb: v.verb, Noun: noun, Description: what, Parameters: params, Result: "string[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
				units, err := unitsOfArgs(r, c, a, owner)
				if err != nil {
					return nil, err
				}
				return run(r, c, a, units)
			}})
		}
	}
	r.Register(Operation{Verb: "pick", Noun: settingsSectionNoun, Description: "Pick a section of the shown tab from a chooser and show it", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Context(), openSectionPicker(r)
	}})
}

// settingsPageView draws the page by settings.layout: the tabs (when there
// is more than one), the section list (or its selector line when narrow),
// and the shown section's rows over the selected setting's description (the
// divider follows the pane keys).
func settingsPageView(r *Runtime, width, height int) Block {
	if r.Instances[SettingsPageID] == nil || width <= 0 || height <= 0 {
		return Block{}
	}
	s := r.Settings()
	styles := NewStyles(Appearance{Theme: "adaptive", Dark: true})
	if view := s.page; view.styles != nil {
		styles = view.styles()
	}
	layout := r.PaneLayout(SettingsPageID, Layout{Styles: styles})
	units := settingUnits(s)
	key := currentSection(r)
	shown, ok := unitOf(units, key)
	if !ok {
		return Block{}
	}
	var rows []string
	if titles, keys := settingsTabs(s, units); len(titles) > 1 && height > PaneMinHeight {
		rows = append(rows, layout.tabs(titles, activeTab(s, keys, key), width))
	}
	body := height - len(rows)
	sections := r.List(settingsSectionsID)
	listed := listedUnits(s, units, shown)
	narrowAt := s.Int(SettingsNarrowWidthSetting)
	narrow := len(listed) > 0 && narrowAt > 0 && width < narrowAt
	// The section keys read it (settings-narrow) until the next frame.
	s.page.narrow = narrow
	var pair Block
	switch {
	case len(listed) == 0 || sections == nil:
		pair = settingsPanels(r, layout, shown, Block{}, width, body)
	case narrow:
		pair = settingsPanels(r, layout, shown, settingsSelector(r, styles, listed, shown, width), width, body)
	default:
		lw, counts := sectionListWidth(sections, width)
		// The counts flush right against the border; a title too long
		// for the panel is cut.
		sections.SetColumns(Column{Width: max(1, lw-4-tableGap-counts)}, Column{Width: counts, Align: AlignRight, Priority: 1})
		sectionsBody := Text(sections.View(PanelContentSize(lw, body)))
		left := layout.PanelBlock("Sections", sectionsBody, lw, body, sections.Focused())
		right := settingsPanels(r, layout, shown, Block{}, width-lw-1, body).Offset(lw+1, 0)
		lr, rr := strings.Split(left.Text, "\n"), strings.Split(right.Text, "\n")
		joined := make([]string, body)
		for i := range joined {
			joined[i] = layoutRow(lr, i, lw) + " " + layoutRow(rr, i, width-lw-1)
		}
		cursor := firstCursor(left, right)
		if !sections.Focused() {
			cursor = firstCursor(right, left)
		}
		pair = Block{Text: strings.Join(joined, "\n"), Cursor: cursor}
	}
	if len(rows) == 0 {
		return pair
	}
	pair = pair.Offset(0, len(rows))
	return Block{Text: strings.Join(rows, "\n") + "\n" + pair.Text, Cursor: pair.Cursor}
}

// sectionListWidth is the section list's panel width: its widest title and
// count with the borders, clamped to settingsSectionMin..settingsSectionMax
// and to a third of the page; and the widest count.
func sectionListWidth(list *List, width int) (int, int) {
	titles, counts, headings := 0, 1, 0
	for _, item := range list.Items() {
		counts = max(counts, ansi.StringWidth(item.Cells[1]))
		titles = max(titles, ansi.StringWidth(item.Cells[0]))
		if item.Section != "" {
			headings = max(headings, ansi.StringWidth(item.Section))
		}
	}
	// The widest title beside the widest count (they are columns), so a
	// long count (All's) never cuts a title.
	widest := max(titles+tableGap+counts, headings)
	return min(max(widest+4, settingsSectionMin), settingsSectionMax, max(width/3, settingsSectionMin)), counts
}

// settingsSelector is the narrow page's selector line: the shown section
// among the listed ones, ‹ Title ◦  i/n ›, reversed while it has the focus
// and kept marked, faded, without it (Styles.Selection, UX R5.1).
// It stands for the section list as its stop (Placed), so j from it and k
// to it move as from and to the list.
func settingsSelector(r *Runtime, styles Styles, listed []settingsUnit, shown settingsUnit, width int) Block {
	r.Placed(settingsSectionsID)
	at := slices.IndexFunc(listed, func(u settingsUnit) bool { return u.key() == shown.key() })
	line := fmt.Sprintf("Section ‹ %s  %d/%d ›", sectionCell(r.Settings(), shown.Title, shown.Session), at+1, len(listed))
	if list := r.List(settingsSectionsID); list != nil {
		line = styles.Selection(list.Focused()).Render(line)
	}
	return Text(line)
}

// settingsPanels draws the shown section's settings over the detail;
// selector, when set, is the narrow page's selector line above the rows.
func settingsPanels(r *Runtime, layout Layout, shown settingsUnit, selector Block, width, height int) Block {
	s := r.Settings()
	table := r.List(settingsListID(shown.key()))
	if table == nil || width <= 0 || height <= 0 {
		return Block{}
	}
	extra := 0
	if selector.Text != "" {
		extra = 2
	}
	// The rows take what they need (border included) up to three fifths
	// of the height; the description the rest.
	th := layout.Divide(min(max(table.Height()+headingLines(table)+2+extra, PaneMinHeight), height*3/5), height, PaneMinHeight)
	tw, tth := PanelContentSize(width, th)
	var tableBody Block
	if selector.Text != "" {
		rule := strings.Repeat("─", max(0, tw))
		tableBody = Text(selector.Text + "\n" + layout.Styles.Muted.Render(rule) + "\n" + table.View(tw, max(0, tth-2)))
	} else {
		tableBody = Text(table.View(tw, tth))
	}
	dw, dch := PanelContentSize(width, height-th)
	detail := r.Detail(settingsDetailID)
	title, text := "Detail", ""
	sectionsFocused := false
	if list := r.List(settingsSectionsID); list != nil && list.Focused() {
		sectionsFocused = true
	}
	switch name, index := settingOfRow(r, selectedSettingRow(r)); {
	case sectionsFocused:
		title, text = sectionDetail(shown)
	case name != "":
		title, text = settingDetail(s, name, index)
	}
	content, marker := "", ""
	if detail != nil {
		content, marker = detail.Window(text, dw, dch)
	}
	if marker != "" {
		title += " · " + marker
	}
	focused := SplitNone
	if table.Focused() || sectionsFocused && selector.Text != "" {
		focused = SplitLeft
	}
	tableTitle := fmt.Sprintf("%s (%d)", shown.Title, len(shown.Settings))
	if selector.Text != "" && settingsLayout(s) != SettingsLayoutSections && shown.OwnerTitle != "" {
		tableTitle = shown.OwnerTitle + " › " + tableTitle
	}
	return layout.panelPair(tableTitle, tableBody, title, Text(content), width, height, th, true, focused)
}

// headingLines counts the group headings a list draws above its rows (a
// line above each run of rows with the same non-empty section), which
// List.Height leaves out.
func headingLines(list *List) int {
	n, last := 0, ""
	for _, item := range list.Items() {
		if item.Section != "" && item.Section != last {
			n++
		}
		last = item.Section
	}
	return n
}

// sectionDetail is the detail while the section list has the focus: the
// section's line and what it holds.
func sectionDetail(u settingsUnit) (title, text string) {
	var lines []string
	if u.Description != "" {
		lines = append(lines, u.Description+".", "")
	}
	n := "settings"
	if len(u.Settings) == 1 {
		n = "setting"
	}
	where := u.Title
	if u.OwnerTitle != "" {
		where = u.OwnerTitle + " › " + where
	}
	lines = append(lines, fmt.Sprintf("%d %s in %s.", len(u.Settings), n, where))
	if u.Session > 0 {
		lines = append(lines, fmt.Sprintf("%d set this session (setting.write saves them, settings-section.write all of them).", u.Session))
	}
	return u.Title, strings.Join(lines, "\n")
}
