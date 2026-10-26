package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The Settings page: settings.edit opens (or switches to) a framework page
// of the declared settings. A rail of sections on the left (the settings
// without a section first, under SettingsGeneral, then each section in the
// order its first setting was declared), the selected section's table on
// the right (setting, value, default, source) and below it a detail of the
// selected setting (description, type, allowed values; J/K scroll it).
// Every change is saved as it is made; there is no save key. On a row,
// enter edits (a boolean flips, an enum takes its next value, any other
// type opens a one-line prompt prefilled with the value: enter sets it, esc
// leaves it), l/h and space step a boolean, an enum or a number, r resets
// to the default. q or esc (settings.close) goes back to the page the
// Settings page was opened from, with its remembered focus and mode. A
// value another process sets (the poll) redraws the open tables.
//
// settings.edit registers the page on first use and settings.close
// removes it again, so it is a tab only while it is open; an application
// that wants it as a permanent tab calls AddSettingsPage.
const (
	// SettingsPageID is the Settings page's instance id.
	SettingsPageID = "__settings"
	// SettingsGeneral titles the section of the settings without one.
	SettingsGeneral = "General"

	settingsPageNoun    = "settings-page"
	settingsSectionNoun = "settings-section"
	settingsRailID      = "__settings/sections"
	settingsDetailID    = "__settings/detail"
	settingPromptID     = "setting-prompt"
	settingValueID      = "__settings/value"
	settingRowPrefix    = "__settings/row/"
)

// settingsPage is the Settings page's state, kept with the settings.
type settingsPage struct {
	// styles draws the page in the Model's appearance (NewModel sets it).
	styles func() Styles
	// from is the page settings.close returns to; owned is set when
	// settings.edit registered the page, so settings.close removes it.
	from  string
	owned bool
}

// SectionName is the section the Settings page files the setting under:
// Section when declared, else the name before its last dot
// ("ext.ai.poll-every" is in "ext.ai"), else "" (SettingsGeneral).
func (s Setting) SectionName() string {
	if s.Section != "" {
		return s.Section
	}
	if i := strings.LastIndex(s.Name, "."); i > 0 {
		return s.Name[:i]
	}
	return ""
}

func registerSettingsEditor(r *Runtime) {
	r.Register(Operation{Verb: "edit", Noun: "settings", Description: "Open the Settings page (or switch to it): every change is saved as it is made", Result: "setting[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		s := r.Settings()
		if len(s.Declared()) == 0 {
			return nil, errors.New("this application declares no settings")
		}
		if r.Page == SettingsPageID {
			return s.List(), nil
		}
		fresh := r.PageOf(SettingsPageID) == nil
		if _, err := ensureSettingsPage(r, fresh); err != nil {
			return nil, err
		}
		if err := r.OpenPage(SettingsPageID); err != nil {
			if fresh {
				_ = r.RemoveInstance(SettingsPageID)
			}
			return nil, err
		}
		return s.List(), nil
	}})
	r.Register(Operation{Verb: "close", Noun: "settings", Description: "Leave the Settings page for the page it was opened from", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Context(), closeSettingsPage(r)
	}})
	r.DefineGuard("setting-steps", func(r *Runtime, ev KeyEvent) bool {
		one := focusedSetting(r, ev.Focus)
		return one != nil && settingSteps(*one)
	})
	r.Register(Operation{Verb: "cycle", Noun: "setting", Description: "Step a boolean, an enum or a number to its next or previous value", Parameters: map[string]Parameter{"step": {Type: "integer", Description: "1 for the next value, -1 for the previous"}}, Result: "setting", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		one := focusedSetting(r, focusOf(c))
		if one == nil {
			return nil, errors.New("no setting is focused")
		}
		return stepSetting(r, *one, IntArg(a, "step", 1))
	}})
	r.Define("setting").
		Verb("edit", func(r *Runtime, c Context, a Arguments) (any, error) {
			one := focusedSetting(r, focusOf(c))
			if one == nil {
				return nil, errors.New("no setting is focused")
			}
			if one.Type == SettingBoolean || len(one.Enum) > 0 {
				return stepSetting(r, *one, 1)
			}
			return nil, openSettingPrompt(r, *one)
		}, "Edit the setting: flip a boolean, take an enum's next value, else type a new value").
		Verbs("cycle").
		Verb("reset", func(r *Runtime, c Context, a Arguments) (any, error) {
			one := focusedSetting(r, focusOf(c))
			if one == nil {
				return nil, errors.New("no setting is focused")
			}
			if err := r.Settings().Reset(one.Name); err != nil {
				return nil, err
			}
			refreshSettingsPage(r)
			return r.Settings().Get(one.Name)
		}, "Reset the setting to its default").
		Mode("normal").
		Bind("enter", Do("setting.edit")).
		// One hint for every step key: h/l setting.cycle.
		Bind("l", Do("setting.cycle", Arguments{"step": 1}).Guard("setting-steps").Hide()).
		Bind("right", Do("setting.cycle", Arguments{"step": 1}).Guard("setting-steps").Hide()).
		Bind("space", Do("setting.cycle", Arguments{"step": 1}).Guard("setting-steps").Hide()).
		Bind("h", Do("setting.cycle", Arguments{"step": -1}).Guard("setting-steps").Hinted("h/l setting.cycle")).
		Bind("left", Do("setting.cycle", Arguments{"step": -1}).Guard("setting-steps").Hide()).
		Bind("r", Do("setting.reset"))
	// The rail's rows move into the section's table, as a groups rail
	// does (cli.MountPage); tab, shift+tab, } and { move between them too.
	r.Define(settingsSectionNoun).Mode("normal").
		Bind("enter", Do("focus.next")).
		Bind("l", Do("focus.next")).
		Bind("right", Do("focus.next"))
	r.Define(settingsPageNoun).Mode("normal").
		Bind("q", Do("settings.close")).
		Bind("esc", Do("settings.close"))
	r.Define("setting-value").Pin("insert").
		Verb("commit", func(r *Runtime, c Context, a Arguments) (any, error) {
			body := r.Instances[r.TopModal()]
			in := r.Input(settingValueID)
			if body == nil || body.ID != settingPromptID || in == nil || body.Payload.Request == nil {
				return nil, errors.New("no setting prompt is open")
			}
			name := StringArg(body.Payload.Request.Args, "name", "")
			// A value that does not validate keeps the prompt open with
			// the error, so it can be corrected.
			if err := r.Settings().Set(name, string(in.Text)); err != nil {
				return nil, err
			}
			if err := r.Pop(); err != nil {
				return nil, err
			}
			refreshSettingsPage(r)
			return r.Settings().Get(name)
		}, "Set the setting to the typed value and close the prompt").
		Verb("cancel", func(r *Runtime, c Context, a Arguments) (any, error) {
			if r.TopModal() != settingPromptID {
				return nil, errors.New("no setting prompt is open")
			}
			return nil, r.Pop()
		}, "Close the prompt and keep the value").
		Mode("insert").
		Bind("enter", Do("setting-value.commit")).
		Bind("esc", Do("setting-value.cancel"))
	r.Settings().Observe("", func(string, any) { refreshSettingsPage(r) })
}

// AddSettingsPage registers the Settings page as a permanent page, a tab
// in registration order like the application's own; settings.edit then
// switches to it and settings.close goes back without removing it.
// Without it, settings.edit registers the page when it opens and
// settings.close removes it.
func (r *Runtime) AddSettingsPage() (*Page, error) {
	r.Settings().page.owned = false
	return ensureSettingsPage(r, false)
}

// ensureSettingsPage registers the page when it is not yet (owned when
// settings.edit does) and brings its sections and rows up to date.
func ensureSettingsPage(r *Runtime, owned bool) (*Page, error) {
	view := &r.Settings().page
	if page := r.PageOf(SettingsPageID); page != nil {
		refreshSettingsPage(r)
		return page, nil
	}
	r.Instance(SettingsPageID, Parent("app"), Kind(settingsPageNoun))
	if _, err := r.AddList(settingsRailID, nil, Parent(SettingsPageID), Kind("list"), NoSpill(), NoMarks()); err != nil {
		_ = r.RemoveInstance(SettingsPageID)
		return nil, err
	}
	refreshSettingsPage(r)
	page := &Page{ID: SettingsPageID, Title: "Settings", Focus: settingsTableID(firstSection(r)), Render: settingsPageView}
	page.OnOpen = func(r *Runtime, from string) {
		view.from = from
		refreshSettingsPage(r)
	}
	if err := r.RegisterPage(page, Kind(settingsPageNoun)); err != nil {
		_ = r.RemoveInstance(SettingsPageID)
		return nil, err
	}
	if _, err := r.AddDetail(settingsDetailID, func() string { return selectedSettingRow(r) }, Parent(SettingsPageID)); err != nil {
		_ = r.RemoveInstance(SettingsPageID)
		return nil, err
	}
	view.owned = owned
	return page, nil
}

// closeSettingsPage goes back to the page the Settings page was opened
// from (else the first other page) and removes the page when settings.edit
// registered it.
func closeSettingsPage(r *Runtime) error {
	if r.PageOf(SettingsPageID) == nil {
		return errors.New("the settings page is not open")
	}
	view := &r.Settings().page
	if r.Page == SettingsPageID {
		back := view.from
		if back == SettingsPageID || r.PageOf(back) == nil {
			back = ""
			for _, page := range r.PageList() {
				if page.ID != SettingsPageID {
					back = page.ID
					break
				}
			}
		}
		if back == "" {
			return errors.New("there is no page to go back to")
		}
		if err := r.OpenPage(back); err != nil {
			return err
		}
	}
	if view.owned {
		view.from = ""
		return r.RemoveInstance(SettingsPageID)
	}
	return nil
}

// settingSections is every section in order: "" (SettingsGeneral) first
// when it has settings, then the others in the order their first setting
// was declared.
func settingSections(s *Settings) []string {
	var out []string
	general := false
	for _, one := range s.declared {
		key := one.SectionName()
		if key == "" {
			general = true
		} else if !slices.Contains(out, key) {
			out = append(out, key)
		}
	}
	if general {
		out = append([]string{""}, out...)
	}
	return out
}

func firstSection(r *Runtime) string {
	if sections := settingSections(r.Settings()); len(sections) > 0 {
		return sections[0]
	}
	return ""
}

func sectionTitle(key string) string {
	if key == "" {
		return SettingsGeneral
	}
	return key
}

func settingsTableID(key string) string {
	if key == "" {
		return "__settings/table"
	}
	return "__settings/table/" + key
}

func settingsSectionRowID(key string) string {
	if key == "" {
		return "__settings/section"
	}
	return "__settings/section/" + key
}

// refreshSettingsPage brings the open page up to date: a rail row and a
// table per section (a section declared since gets both), each table's
// rows; selections stay on their settings.
func refreshSettingsPage(r *Runtime) {
	rail := r.List(settingsRailID)
	if rail == nil {
		return
	}
	s := r.Settings()
	sections := settingSections(s)
	rows := make([]ListItem, len(sections))
	for i, key := range sections {
		rows[i] = ListItem{ID: settingsSectionRowID(key), Label: sectionTitle(key), Noun: settingsSectionNoun, Data: key}
	}
	_ = rail.SetItems(rows)
	values := s.List()
	for _, key := range sections {
		var items []ListItem
		for _, v := range values {
			if v.SectionName() == key {
				items = append(items, settingItem(s, key, v))
			}
		}
		list := r.List(settingsTableID(key))
		if list == nil {
			var err error
			if list, err = r.AddList(settingsTableID(key), nil, Parent(SettingsPageID), NoMarks()); err != nil {
				continue
			}
		}
		// The names keep their width (up to 24 cells) and the source fits
		// settings.json; the default narrows and goes first.
		names := 0
		for _, item := range items {
			names = max(names, ansi.StringWidth(item.Cells[0]))
		}
		list.SetColumns(
			Column{Title: "Setting", Priority: 3, Min: min(names, 24)},
			Column{Title: "Value", Priority: 3},
			Column{Title: "Default", Priority: 1},
			Column{Title: "Source", Priority: 2, Min: len(SettingsFile)},
		)
		_ = list.SetItems(items)
	}
}

// settingItem is one table row: the name without its section's prefix,
// the value, the default and where the value comes from.
func settingItem(s *Settings, section string, v SettingValue) ListItem {
	name := strings.TrimPrefix(v.Name, section+".")
	if section == "" || name == "" {
		name = v.Name
	}
	value := settingCell(v.Value)
	return ListItem{
		ID: settingRowPrefix + v.Name, Label: name + "  " + value, Noun: "setting", Data: v.Name,
		Cells: []string{name, value, settingCell(v.Default), settingSource(s, v)},
	}
}

// settingSource is where a setting's value comes from: settings.json, or
// the default (also when the file holds a value that does not validate).
func settingSource(s *Settings, v SettingValue) string {
	switch {
	case v.Set:
		return SettingsFile
	case s.raw[v.Name] != nil:
		return "default (file value invalid)"
	}
	return "default"
}

func settingCell(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// currentSection is the section the page shows: the focused table's, else
// the rail's selected row's. Focus in a table moves the rail with it.
func currentSection(r *Runtime) string {
	rail := r.List(settingsRailID)
	if rail == nil {
		return ""
	}
	for _, key := range settingSections(r.Settings()) {
		if list := r.List(settingsTableID(key)); list != nil && list.Focused() {
			_ = rail.Select(settingsSectionRowID(key))
			return key
		}
	}
	if item, ok := rail.Selected(); ok {
		key, _ := item.Data.(string)
		return key
	}
	return firstSection(r)
}

// selectedSettingRow is the detail's item: the shown table's selected row.
func selectedSettingRow(r *Runtime) string {
	if list := r.List(settingsTableID(currentSection(r))); list != nil {
		if item, ok := list.Selected(); ok {
			return item.ID
		}
	}
	return ""
}

// settingDetail is the detail of one setting: its description, then its
// name, type, allowed values, value, default and source.
func settingDetail(s *Settings, name string) (title, text string) {
	v, err := s.Get(name)
	if err != nil {
		return "Detail", "no setting selected"
	}
	kind := v.Type
	if len(v.Enum) > 0 {
		kind = "enum (string)"
	}
	facts := [][2]string{
		{"name", v.Name},
		{"type", kind},
		{"allowed", settingAllowed(v.Setting)},
		{"value", settingCell(v.Value)},
		{"default", settingCell(v.Default)},
		{"source", settingSource(s, v)},
	}
	width := 0
	for _, f := range facts {
		width = max(width, len(f[0]))
	}
	var lines []string
	if v.Description != "" {
		lines = append(lines, v.Description, "")
	}
	for _, f := range facts {
		lines = append(lines, f[0]+strings.Repeat(" ", width-len(f[0]))+"  "+f[1])
	}
	return v.Name, strings.Join(lines, "\n")
}

// settingAllowed says what values a setting takes.
func settingAllowed(one Setting) string {
	var out string
	switch {
	case len(one.Enum) > 0:
		out = strings.Join(one.Enum, ", ")
	case one.Type == SettingBoolean:
		out = "true, false"
	case one.Type == SettingInteger:
		out = "a whole number"
	case one.Type == SettingNumber:
		out = "a number"
	case one.Type == SettingDuration:
		out = "a duration such as 400ms or 2s"
	default:
		out = "any text"
	}
	if one.Validate != nil {
		out += ", checked by the application"
	}
	return out
}

// focusedSetting is the declared setting of the focused table row.
func focusedSetting(r *Runtime, focus string) *Setting {
	inst := r.Instances[focus]
	if inst == nil || inst.Noun != "setting" {
		return nil
	}
	name, _ := inst.Data.(string)
	return r.Settings().find(name)
}

// settingSteps reports whether h/l step the setting.
func settingSteps(one Setting) bool {
	switch one.Type {
	case SettingBoolean, SettingInteger, SettingNumber:
		return true
	}
	return len(one.Enum) > 0
}

// stepSetting sets a boolean, enum or number to its next value (step 1) or
// previous one (step -1); an enum wraps.
func stepSetting(r *Runtime, one Setting, step int) (any, error) {
	s := r.Settings()
	current, _ := s.Value(one.Name)
	var next any
	switch {
	case one.Type == SettingBoolean:
		next = !current.(bool)
	case len(one.Enum) > 0:
		i := slices.Index(one.Enum, current.(string))
		n := len(one.Enum)
		next = one.Enum[((i+step)%n+n)%n]
	case one.Type == SettingInteger:
		next = current.(int) + step
	case one.Type == SettingNumber:
		next = current.(float64) + float64(step)
	default:
		return nil, fmt.Errorf("%s is a %s: enter edits it", one.Name, one.Type)
	}
	if err := s.Set(one.Name, next); err != nil {
		return nil, err
	}
	refreshSettingsPage(r)
	return s.Get(one.Name)
}

// openSettingPrompt opens the one-line value prompt over the page.
func openSettingPrompt(r *Runtime, one Setting) error {
	current, _ := r.Settings().Get(one.Name)
	in := NewInput(settingValueID, settingCell(current.Value), false)
	in.Cursor = len(in.Text)
	prompt := fmt.Sprintf("%s (%s)", one.Name, one.Type)
	request := &Request{Noun: "settings", Verb: "set", Args: Arguments{"name": one.Name}}
	_, err := r.OpenModal(settingPromptID, Payload{Prompt: prompt, Request: request}, WithInput(in, Kind("setting-value")))
	return err
}

// settingsPageView draws the page: the rail of sections on the left, the
// shown section's table over the selected setting's detail on the right
// (the divider follows the pane keys).
func settingsPageView(r *Runtime, width, height int) Block {
	rail := r.List(settingsRailID)
	if rail == nil || width <= 0 || height <= 0 {
		return Block{}
	}
	styles := NewStyles(Appearance{Theme: "adaptive", Dark: true})
	if view := r.Settings().page; view.styles != nil {
		styles = view.styles()
	}
	layout := r.PaneLayout(SettingsPageID, Layout{Styles: styles})
	key := currentSection(r)
	railWidth := min(24, max(12, (width-1)/4))
	rw, rh := PanelContentSize(railWidth, height)
	left := layout.PanelBlock("Sections", Text(rail.View(rw, rh)), railWidth, height, rail.Focused())

	right := Block{}
	mainWidth := width - railWidth - 1
	if table := r.List(settingsTableID(key)); table != nil && mainWidth > 0 {
		// The table takes its rows (header and border included) up to
		// three fifths of the height; the detail the rest.
		th := layout.Divide(min(max(table.Len()+3, PaneMinHeight), height*3/5), height, PaneMinHeight)
		tw, tth := PanelContentSize(mainWidth, th)
		tableBody := Text(table.View(tw, tth))
		dw, dch := PanelContentSize(mainWidth, height-th)
		detail := r.Detail(settingsDetailID)
		title, text := "Detail", ""
		if name := strings.TrimPrefix(selectedSettingRow(r), settingRowPrefix); name != "" {
			title, text = settingDetail(r.Settings(), name)
		}
		body, marker := "", ""
		if detail != nil {
			body, marker = detail.Window(text, dw, dch)
		}
		if marker != "" {
			title += " · " + marker
		}
		focused := SplitNone
		if table.Focused() {
			focused = SplitLeft
		}
		tableTitle := fmt.Sprintf("%s (%d)", sectionTitle(key), table.Len())
		right = layout.panelPair(tableTitle, tableBody, title, Text(body), mainWidth, height, th, true, focused)
	}
	leftRows, rightRows := strings.Split(left.Text, "\n"), strings.Split(right.Text, "\n")
	rows := make([]string, height)
	for i := range rows {
		rows[i] = layoutRow(leftRows, i, railWidth) + " " + layoutRow(rightRows, i, max(0, mainWidth))
	}
	return Text(strings.Join(rows, "\n"))
}

// settingPromptView is the prompt line over the input.
func settingPromptView(r *Runtime, body *Instance, width, height int) Block {
	in := r.Input(settingValueID)
	if in == nil {
		return Text(body.Payload.Prompt)
	}
	field := InputBlock(in, max(1, width), 1)
	return field.Offset(0, 2).WithText(body.Payload.Prompt + "\n\n" + field.Text)
}
