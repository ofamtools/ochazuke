package ochazuke

import (
	"cmp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The in-context settings modal (setting.pick, plan L4): the settings that
// apply where the focus is, grouped by the focus walk innermost first, the
// way the keys modal groups key rows. Groups come from setting.list {at}
// (settingPlaces): each setting sits at the innermost level one of its
// context points matches. Its rows are the Settings page's rows
// (settingItem, noun ochazuke.setting), so the settings-editor keys work
// on them unchanged: space, h and l cycle or toggle, enter edits, r
// resets, each change written at once. A row's heading is the level and
// its section ("pane: Turns", "Inbox page: Inbox"). Where the framework's
// own settings go (settings.pick-framework) and whether the settings on no
// level follow (settings.pick-rest) are settings; settings.pick off opens
// the Settings page instead.
//
// Compared with Vim: :options opens every option (setting.open, , , here);
// this is :options narrowed to the context the cursor is in, the way
// filetype plugins group options by filetype. Values stay global (no
// :setlocal).
const (
	settingPickNoun      = FrameworkNamespace + ".setting-pick"
	settingPickID        = settingPickNoun
	settingPickListID    = "__setting-pick/list"
	settingPickRowPrefix = "__setting-pick/row/"
)

// The modal's settings (settings/ochazuke.json, section settings).
const (
	SettingsPickSetting          = "settings.pick"
	SettingsPickFrameworkSetting = "settings.pick-framework"
	SettingsPickRestSetting      = "settings.pick-rest"
)

// settings.pick-framework's values: where the framework owner's settings
// go. walk keeps strict walk order; after puts them, level by level, after
// every other owner's groups; rest moves them to the elsewhere groups.
const (
	PickFrameworkWalk  = "walk"
	PickFrameworkAfter = "after"
	PickFrameworkRest  = "rest"
)

// settings.pick-rest's values: the settings on no level of the walk after
// the levels (shown), or left out (off).
const (
	PickRestShown = "shown"
	PickRestOff   = "off"
)

// SettingPickGroup is one heading of the modal and its settings, in the
// order the modal lists them; Level is -1 for an elsewhere group.
type SettingPickGroup struct {
	Heading  string   `json:"heading"`
	Level    int      `json:"level"`
	Role     string   `json:"role,omitempty"`
	Settings []string `json:"settings"`
}

// registerSettingPick keeps the open modal's rows current. The modal's
// noun is made when the modal first opens (its id is the noun).
func registerSettingPick(r *Runtime) {
	r.Settings().Observe("", func(string, any) { refreshSettingPick(r) })
}

// runSettingPick is setting.pick: the modal over the focus it runs at (its
// origin again when it runs inside the modal, so it re-targets in place),
// answering its groups.
func runSettingPick(r *Runtime, c Context, a Arguments) (any, error) {
	s := r.Settings()
	if !s.Bool(SettingsPickSetting) {
		return settingsOpen(r, c, a)
	}
	origin := focusOf(c)
	if _, open := r.Instances[settingPickID]; open {
		origin, _ = r.ModalReturn(settingPickID)
	}
	groups := r.SettingPickGroups(origin)
	inst, err := r.OpenModal(settingPickID, Payload{}, ReplaceOpen())
	if err != nil {
		return nil, err
	}
	inst.Component = settingPickBody{r}
	list, err := r.AddList(settingPickListID, nil, Parent(settingPickID), NoMarks())
	if err != nil {
		_ = r.Pop()
		return nil, err
	}
	setSettingPickItems(list, settingPickItems(s, groups))
	if err := list.Focus(); err != nil {
		_ = r.Pop()
		return nil, err
	}
	return groups, nil
}

// refreshSettingPick redraws the open modal's rows after a setting
// changed: the values, and the groups when a pick setting moved them; the
// selection stays on its row.
func refreshSettingPick(r *Runtime) {
	list := r.List(settingPickListID)
	if list == nil {
		return
	}
	origin, _ := r.ModalReturn(settingPickID)
	setSettingPickItems(list, settingPickItems(r.Settings(), r.SettingPickGroups(origin)))
}

// setSettingPickItems gives the list its rows in the Settings page's two
// columns (the name as wide as the longest, up to 30 cells, then the
// value), so the headings draw and a change never moves the values.
func setSettingPickItems(list *Collection, items []ListItem) {
	names := 0
	for _, item := range items {
		names = max(names, len([]rune(item.Cells[0])))
	}
	list.SetColumns(Column{Width: min(names+2, 30)}, Column{Min: 6, Priority: 1})
	_ = list.SetItems(items)
}

// SettingPickGroups are the modal's groups for the focus at: every
// declared setting placed on the walk from at, the levels innermost first,
// each level's sections in the Settings page's order, the framework's
// where settings.pick-framework says, then (settings.pick-rest shown) the
// settings on no level by owner and section.
func (r *Runtime) SettingPickGroups(at string) []SettingPickGroup {
	s := r.Settings()
	w := r.SettingWalkAt(at)
	places := r.settingPlaces(w)
	framework := s.String(SettingsPickFrameworkSetting)
	rest := s.String(SettingsPickRestSetting) != PickRestOff
	type key struct {
		pass, level int
		owner, id   string
	}
	var keys []key
	groups := map[key]*SettingPickGroup{}
	for _, tab := range s.tabs() {
		for _, sec := range tab.Sections {
			for _, name := range sec.Settings {
				place := places[name]
				pass := 0
				if tab.Owner.ID == OwnerFramework {
					switch framework {
					case PickFrameworkAfter:
						pass = 1
					case PickFrameworkRest:
						place = SettingPlace{Level: -1}
					}
				}
				if place.Level < 0 {
					if !rest {
						continue
					}
					pass = 2
				}
				k := key{pass, place.Level, tab.Owner.ID, sec.ID}
				g := groups[k]
				if g == nil {
					heading := "elsewhere: " + tab.Owner.Title + ", " + sec.Title
					if place.Level >= 0 {
						heading = r.levelHeading(w, place.Level) + ": " + sec.Title
					}
					g = &SettingPickGroup{Heading: heading, Level: place.Level, Role: place.Role}
					groups[k] = g
					keys = append(keys, k)
				}
				g.Settings = append(g.Settings, name)
			}
		}
	}
	slices.SortStableFunc(keys, func(a, b key) int {
		return cmp.Or(cmp.Compare(a.pass, b.pass), compareLevels(a.level, b.level))
	})
	out := make([]SettingPickGroup, 0, len(keys))
	for _, k := range keys {
		out = append(out, *groups[k])
	}
	return out
}

// levelHeading names a walk level for a heading: a page by its title, the
// root (app) by the application's title, any other level by its noun.
func (r *Runtime) levelHeading(w HelpWalk, i int) string {
	l := w.Levels[i]
	if p := r.PageOf(l.At); p != nil && p.Title != "" {
		return p.Title + " page"
	}
	if inst := r.Instances[l.At]; inst != nil && inst.Parent == "" {
		return r.Settings().ownerTitle(SettingOwner{ID: OwnerApp})
	}
	return l.Label()
}

// settingPickItems are the modal's rows: each group's settings as the
// Settings page draws them, under the group's heading.
func settingPickItems(s *Settings, groups []SettingPickGroup) []ListItem {
	var items []ListItem
	for _, g := range groups {
		for _, name := range g.Settings {
			v, err := s.Get(name)
			if err != nil {
				continue
			}
			item := settingItem(s, v)
			item.ID, item.Section = settingPickRowPrefix+name, g.Heading
			items = append(items, item)
		}
	}
	return items
}

// settingPickBody is the modal's body: it draws itself (ModalBody) and
// holds no jobs.
type settingPickBody struct{ r *Runtime }

func (settingPickBody) HandleMessage(tea.Msg) bool { return false }
func (settingPickBody) PendingCommand() tea.Cmd    { return nil }
func (settingPickBody) Close()                     {}
func (settingPickBody) ModalTitle() string         { return "Settings here" }

// ModalBlock draws the modal: the rows, and under them the selected
// setting's description.
func (b settingPickBody) ModalBlock(_ Styles, width, height int) Block {
	r := b.r
	list := r.List(settingPickListID)
	if list == nil || width <= 0 || height <= 0 {
		return Text("")
	}
	if list.Len() == 0 {
		return Text("No settings apply here.")
	}
	var detail []string
	if item, ok := list.Selected(); ok {
		if name, ok := item.Data.(string); ok {
			if one := r.Settings().find(name); one != nil && one.Description != "" {
				detail = strings.Split(ansi.Wrap(one.Description, width, ""), "\n")
			}
		}
	}
	rows := height
	if len(detail) > 0 && height > 4 {
		rows = max(2, height-1-min(len(detail), height/3))
	}
	lines := strings.Split(list.View(width, rows), "\n")
	if room := height - len(lines) - 1; room > 0 && len(detail) > 0 {
		lines = append(lines, strings.Repeat("─", width))
		lines = append(lines, detail[:min(room, len(detail))]...)
	}
	return Text(strings.Join(lines, "\n"))
}
