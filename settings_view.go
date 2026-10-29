package ochazuke

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// The Settings page: settings.edit opens (or switches to) a framework page
// of the declared settings. Its sections are tabs across the top (the
// settings without a section first, under SettingsGeneral, then each
// section in the order its first setting was declared; tab, shift+tab, ]
// and [ step through them, wrapping). Below the tabs the shown section is
// one list of rows, a setting a row: its name on the left (a • after a
// name whose value is not the default), its value on the right (‹ value ›
// for one h/l steps). A list setting's row reads its item count, and each
// item is a row beneath it with the name column blank. Under the list the
// selected setting's description says what its default is, where its
// value comes from (the default, settings.json, or the default because
// settings.json holds an invalid value) and what it takes; J/K scroll it.
//
// Every change is saved as it is made; there is no save key. On a setting
// row enter edits (a boolean flips, an enum takes its next value, a list
// takes a new item, any other type opens a one-line prompt prefilled with
// the value), l/h and space step a boolean, an enum or a number, n adds an
// item to a list, r (or backspace, delete) resets to the default. On an
// item row enter or i edits the item, n adds one after it, d d deletes
// it, r resets the whole list. u (or ctrl+z) takes back the last change
// made on the page. q or esc (settings.close) goes back to the page the
// Settings page was opened from, with its remembered focus and mode; the
// page reopens on the section and row it was left on. A value another
// process sets (the poll) redraws the open lists.
//
// settings.edit registers the page on first use and settings.close
// removes it again, so it is a tab only while it is open; an application
// that wants it as a permanent tab calls AddSettingsPage.
const (
	// SettingsPageID is the Settings page's instance id.
	SettingsPageID = "__settings"
	// SettingsGeneral titles the section of the settings without one.
	SettingsGeneral = "General"

	settingsPageNoun  = "ochazuke.settings-page"
	settingItemNoun   = "ochazuke.setting-item"
	settingsDetailID  = "__settings/detail"
	settingPromptID   = "setting-prompt"
	settingValueID    = "__settings/value"
	settingRowPrefix  = "__settings/row/"
	settingItemPrefix = "__settings/item/"
	// settingsUndoLimit bounds the page's undo list.
	settingsUndoLimit = 50
)

// settingsPage is the Settings page's state, kept with the settings.
type settingsPage struct {
	// styles draws the page in the Model's appearance (NewModel sets it).
	styles func() Styles
	// from is the page settings.close returns to; owned is set when
	// settings.edit registered the page, so settings.close removes it.
	from  string
	owned bool
	// section is the shown section; last the row the page was left on,
	// where it reopens.
	section string
	last    string
	// undo is every change made on the page, oldest first; group is the
	// step a settings.apply line is gathering (settingsGroup).
	undo  []settingUndo
	group *settingUndo
}

// settingUndo is one step of the Settings page's undo: what each setting
// it changed held before, put back together (a :set line of several
// settings is one step).
type settingUndo struct {
	snaps []settingSnap
}

// settingSnap is what one setting held at a moment: settings.json's raw
// value (had false: no key) and its session value (inSession false: none).
type settingSnap struct {
	name      string
	raw       json.RawMessage
	had       bool
	session   sessionValue
	inSession bool
}

// settingItemRef is an item row's Data: the list setting and the item.
type settingItemRef struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
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
	r.Register(Operation{Verb: "edit", Noun: "ochazuke.settings", Description: "Open the Settings page (or switch to it): every change is saved as it is made", Result: "setting[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
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
	r.Register(Operation{Verb: "close", Noun: "ochazuke.settings", Description: "Leave the Settings page for the page it was opened from", Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Context(), closeSettingsPage(r)
	}})
	r.Register(Operation{Verb: "section", Noun: "ochazuke.settings", Description: "Show the next or previous section of the Settings page (wrapping), or the one named", Parameters: map[string]Parameter{
		"step": {Type: "integer", Description: "1 for the next section, -1 for the previous"},
		"name": {Type: "string", Description: "The section to show (General for the settings without one)"},
	}, Result: "context", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return r.Context(), showSettingsSection(r, StringArg(a, "name", ""), IntArg(a, "step", 1))
	}})
	r.Register(Operation{Verb: "undo", Noun: "ochazuke.settings", Description: "Take back the last change made on the Settings page", Result: "setting", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		return undoSettingChange(r)
	}})
	r.DefineGuard("setting-steps", func(r *Runtime, ev KeyEvent) bool {
		one := focusedSetting(r, ev.Focus)
		return one != nil && settingSteps(*one)
	})
	r.DefineGuard("setting-list", func(r *Runtime, ev KeyEvent) bool {
		one := focusedSetting(r, ev.Focus)
		return one != nil && one.Type == SettingList
	})
	r.DefineGuard("settings-undo", func(r *Runtime, ev KeyEvent) bool {
		return len(r.Settings().page.undo) > 0
	})
	registerSettingVerbs(r)
	registerSettingsApply(r)
	focused := func(c Context) (*Setting, error) {
		if one := focusedSetting(r, focusOf(c)); one != nil {
			return one, nil
		}
		return nil, errors.New("no setting is focused")
	}
	editParams := map[string]Parameter{
		"name":  {Type: "string", Description: "Setting name (default: the focused Settings row)", Source: "ochazuke.settings.names"},
		"scope": {Type: "string", Enum: []string{ScopeSession, ScopeFile}, Description: "Where the value is written: session (the default) or file (the Settings page's keys)"},
	}
	r.Register(Operation{Verb: "edit", Noun: "ochazuke.setting", Description: "Edit the setting: flip a boolean, take an enum's next value, add an item to a list, else type a new value", Parameters: editParams, Positional: []string{"name"}, Result: "setting", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		one, err := settingOfArgs(r, c, a)
		if err != nil {
			return nil, err
		}
		scope, err := settingScope(a)
		if err != nil {
			return nil, err
		}
		switch {
		case one.Type == SettingList:
			return nil, openItemPrompt(r, *one, -1, len(r.Settings().Strings(one.Name)))
		case one.Type == SettingBoolean || len(one.Enum) > 0:
			cycle, _ := settingVerbOf("cycle")
			return runSettingVerb(cycle)(r, c, Arguments{"name": one.Name, "scope": scope})
		}
		return nil, openSettingPrompt(r, *one, scope)
	}})
	r.Define("ochazuke.setting").
		Verbs("edit").
		Verb("add", func(r *Runtime, c Context, a Arguments) (any, error) {
			one, err := focused(c)
			if err != nil {
				return nil, err
			}
			if one.Type != SettingList {
				return nil, fmt.Errorf("%s is not a list", one.Name)
			}
			return nil, openItemPrompt(r, *one, -1, len(r.Settings().Strings(one.Name)))
		}, "Add an item at the end of a list setting")
	item := func(c Context) (*Setting, settingItemRef, error) {
		inst := r.Instances[focusOf(c)]
		if inst == nil || inst.Noun != settingItemNoun {
			return nil, settingItemRef{}, errors.New("no list item is focused")
		}
		ref, _ := inst.Data.(settingItemRef)
		one := r.Settings().find(ref.Name)
		if one == nil {
			return nil, ref, r.Settings().unknown(ref.Name)
		}
		return one, ref, nil
	}
	r.Define(settingItemNoun).
		Verb("edit", func(r *Runtime, c Context, a Arguments) (any, error) {
			one, ref, err := item(c)
			if err != nil {
				return nil, err
			}
			return nil, openItemPrompt(r, *one, ref.Index, -1)
		}, "Edit the list item").
		Verb("add", func(r *Runtime, c Context, a Arguments) (any, error) {
			one, ref, err := item(c)
			if err != nil {
				return nil, err
			}
			return nil, openItemPrompt(r, *one, -1, ref.Index+1)
		}, "Add an item to the list after this one").
		Verb("delete", func(r *Runtime, c Context, a Arguments) (any, error) {
			one, ref, err := item(c)
			if err != nil {
				return nil, err
			}
			items := r.Settings().Strings(one.Name)
			if ref.Index < 0 || ref.Index >= len(items) {
				return nil, fmt.Errorf("%s has no item %d", one.Name, ref.Index+1)
			}
			items = slices.Delete(items, ref.Index, ref.Index+1)
			if err := pageChange(r, one.Name, func() error { return r.Settings().Set(one.Name, items) }); err != nil {
				return nil, err
			}
			// The focus stays at the same place: the next item, else the
			// one before, else the setting's row.
			switch {
			case ref.Index < len(items):
				_ = r.SetFocus(settingItemID(one.Name, ref.Index))
			case len(items) > 0:
				_ = r.SetFocus(settingItemID(one.Name, len(items)-1))
			default:
				_ = r.SetFocus(settingRowPrefix + one.Name)
			}
			return r.Settings().Get(one.Name)
		}, "Delete the list item").
		Verb("reset", func(r *Runtime, c Context, a Arguments) (any, error) {
			one, _, err := item(c)
			if err != nil {
				return nil, err
			}
			if err := pageChange(r, one.Name, func() error { return r.Settings().Reset(one.Name) }); err != nil {
				return nil, err
			}
			if r.Instances[r.Focus] == nil || r.Instances[r.Focus].Noun != settingItemNoun {
				_ = r.SetFocus(settingRowPrefix + one.Name)
			}
			return r.Settings().Get(one.Name)
		}, "Reset the whole list to its default")
	r.Define("ochazuke.setting-value").Pin("insert").
		Verb("commit", func(r *Runtime, c Context, a Arguments) (any, error) {
			body := r.Instances[r.TopModal()]
			in := r.Input(settingValueID)
			if body == nil || body.ID != settingPromptID || in == nil || body.Payload.Request == nil {
				return nil, errors.New("no setting prompt is open")
			}
			args := body.Payload.Request.Args
			name := StringArg(args, "name", "")
			// A value that does not validate keeps the prompt open with
			// the error, so it can be corrected.
			focus, err := commitSettingPrompt(r, name, args, string(in.Text))
			if err != nil {
				return nil, err
			}
			if err := r.Pop(); err != nil {
				return nil, err
			}
			if focus != "" {
				_ = r.SetFocus(focus)
			}
			return r.Settings().Get(name)
		}, "Set the setting to the typed value and close the prompt").
		Verb("cancel", func(r *Runtime, c Context, a Arguments) (any, error) {
			if r.TopModal() != settingPromptID {
				return nil, errors.New("no setting prompt is open")
			}
			return nil, r.Pop()
		}, "Close the prompt and keep the value")
	r.Settings().Observe("", func(string, any) { refreshSettingsPage(r) })
	registerSettingsSession(r)
	registerSettingsReview(r)
	// bindings/settings-editor.json: one hint for every step key (h/l
	// setting.cycle).
}

// commitSettingPrompt applies the prompt's text: the value of a setting,
// an edited item (index) or a new one (insert); it returns the row to
// focus once the prompt closes ("" keeps the one it opened over).
func commitSettingPrompt(r *Runtime, name string, args Arguments, text string) (string, error) {
	s := r.Settings()
	index, edit := args["index"]
	insert, add := args["insert"]
	if !edit && !add {
		set, _ := settingVerbOf("set")
		one := s.find(name)
		if one == nil {
			return "", s.unknown(name)
		}
		p, err := planSetting(set, *one, s.current(*one), Arguments{"value": text, "scope": StringArg(args, "scope", ScopeSession)})
		if err != nil {
			return "", err
		}
		return "", pageChange(r, name, func() error { _, err := p.write(s); return err })
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("an item cannot be empty")
	}
	items := s.Strings(name)
	at := 0
	if edit {
		at, _ = index.(int)
		if at < 0 || at >= len(items) {
			return "", fmt.Errorf("%s has no item %d", name, at+1)
		}
		items[at] = text
	} else {
		at, _ = insert.(int)
		at = min(max(0, at), len(items))
		items = slices.Insert(items, at, text)
	}
	if err := pageChange(r, name, func() error { return s.Set(name, items) }); err != nil {
		return "", err
	}
	return settingItemID(name, at), nil
}

// pageChange runs a change to one setting (a page key, a setting verb)
// and puts what the setting held before, in settings.json and the session
// layer, on the page's undo list when it moved. Inside settingsGroup the
// change joins the group's step instead.
func pageChange(r *Runtime, name string, change func() error) error {
	s := r.Settings()
	s.ensure()
	view := &s.page
	if g := view.group; g != nil {
		if !slices.ContainsFunc(g.snaps, func(sn settingSnap) bool { return sn.name == name }) {
			g.snaps = append(g.snaps, s.snap(name))
		}
		return change()
	}
	before := s.snap(name)
	if err := change(); err != nil {
		return err
	}
	if s.moved(before) {
		view.push(settingUndo{snaps: []settingSnap{before}})
	}
	return nil
}

// settingsGroup runs fn with every change it makes folded into one undo
// step; when fn fails, what it changed is put back (all or nothing).
func settingsGroup(r *Runtime, fn func() error) error {
	s := r.Settings()
	s.ensure()
	view := &s.page
	if view.group != nil {
		return fn()
	}
	view.group = &settingUndo{}
	err := fn()
	g := view.group
	view.group = nil
	if err != nil {
		if back := s.restoreStep(*g); back != nil {
			return errors.Join(err, back)
		}
		return err
	}
	g.snaps = slices.DeleteFunc(g.snaps, func(sn settingSnap) bool { return !s.moved(sn) })
	if len(g.snaps) > 0 {
		view.push(*g)
	}
	return nil
}

// push adds an undo step, keeping the last settingsUndoLimit.
func (view *settingsPage) push(step settingUndo) {
	view.undo = append(view.undo, step)
	if len(view.undo) > settingsUndoLimit {
		view.undo = view.undo[len(view.undo)-settingsUndoLimit:]
	}
}

// snap is what a setting holds now.
func (s *Settings) snap(name string) settingSnap {
	sn := settingSnap{name: name}
	sn.raw, sn.had = s.raw[name]
	sn.session, sn.inSession = s.session[name]
	return sn
}

// moved reports whether a setting holds something else than the snap:
// another settings.json value, or another session value.
func (s *Settings) moved(sn settingSnap) bool {
	if now, has := s.raw[sn.name]; has != sn.had || !bytes.Equal(now, sn.raw) {
		return true
	}
	v, in := s.session[sn.name]
	return in != sn.inSession || in && (v.reset != sn.session.reset || !sameSetting(v.value, sn.session.value))
}

// restoreStep puts back every setting of an undo step, the last changed
// first.
func (s *Settings) restoreStep(step settingUndo) error {
	var errs []error
	for _, sn := range slices.Backward(step.snaps) {
		errs = append(errs, s.restore(sn))
	}
	return errors.Join(errs...)
}

// undoSettingChange takes back the page's last undo step and shows the
// first setting it changed.
func undoSettingChange(r *Runtime) (any, error) {
	s := r.Settings()
	view := &s.page
	if len(view.undo) == 0 {
		return nil, errors.New("no change on the settings page to undo")
	}
	last := view.undo[len(view.undo)-1]
	if err := s.restoreStep(last); err != nil {
		return nil, err
	}
	view.undo = view.undo[:len(view.undo)-1]
	name := last.snaps[0].name
	if one := s.find(name); one != nil && r.PageOf(SettingsPageID) != nil {
		view.section = one.SectionName()
		_ = r.SetFocus(settingRowPrefix + name)
	}
	return s.Get(name)
}

// restore puts back what a setting held (settingSnap): settings.json's
// value as it was, even one that does not validate, and the session value.
func (s *Settings) restore(sn settingSnap) error {
	s.ensure()
	one := s.find(sn.name)
	if one == nil {
		return s.unknown(sn.name)
	}
	old, _ := s.Value(sn.name)
	if now, has := s.raw[sn.name]; has != sn.had || !bytes.Equal(now, sn.raw) {
		next := maps.Clone(s.raw)
		if sn.had {
			next[sn.name] = sn.raw
		} else {
			delete(next, sn.name)
		}
		if err := s.write(next); err != nil {
			return err
		}
		delete(s.values, sn.name)
		s.withdrawFor(sn.name)
		s.parse(*one)
	}
	if sn.inSession {
		if s.session == nil {
			s.session = map[string]sessionValue{}
		}
		s.session[sn.name] = sn.session
	} else {
		delete(s.session, sn.name)
	}
	s.changed(sn.name, old)
	s.relayer()
	return nil
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
// settings.edit does) and brings its lists up to date. A new page opens on
// the row the last one was left on.
func ensureSettingsPage(r *Runtime, owned bool) (*Page, error) {
	view := &r.Settings().page
	if page := r.PageOf(SettingsPageID); page != nil {
		refreshSettingsPage(r)
		return page, nil
	}
	r.Instance(SettingsPageID, Parent("app"), Kind(settingsPageNoun))
	refreshSettingsPage(r)
	sections := settingSections(r.Settings())
	if !slices.Contains(sections, view.section) && len(sections) > 0 {
		view.section = sections[0]
	}
	focus := settingsListID(view.section)
	if inst := r.Instances[view.last]; inst != nil && view.last != "" {
		focus = view.last
	}
	page := &Page{ID: SettingsPageID, Title: "Settings", Focus: focus, Render: settingsPageView}
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
// registered it, remembering the row it was left on.
func closeSettingsPage(r *Runtime) error {
	if r.PageOf(SettingsPageID) == nil {
		return errors.New("the settings page is not open")
	}
	view := &r.Settings().page
	if r.Page == SettingsPageID {
		if inst := r.Instances[r.BaseFocus()]; inst != nil && (inst.Noun == "ochazuke.setting" || inst.Noun == settingItemNoun) {
			view.last = inst.ID
		}
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

// showSettingsSection shows the named section, else the one step away
// from the shown one (wrapping), and focuses its selected row.
func showSettingsSection(r *Runtime, name string, step int) error {
	if r.PageOf(SettingsPageID) == nil {
		return errors.New("the settings page is not open")
	}
	sections := settingSections(r.Settings())
	if len(sections) == 0 {
		return errors.New("this application declares no settings")
	}
	key := currentSection(r)
	if name != "" {
		key = ""
		if name != SettingsGeneral {
			key = name
		}
		if !slices.Contains(sections, key) {
			titles := make([]string, len(sections))
			for i, k := range sections {
				titles[i] = sectionTitle(k)
			}
			return fmt.Errorf("no settings section %q: one of %s", name, strings.Join(titles, ", "))
		}
	} else {
		i := slices.Index(sections, key)
		n := len(sections)
		key = sections[((i+step)%n+n)%n]
	}
	r.Settings().page.section = key
	list := r.List(settingsListID(key))
	if list == nil {
		return nil
	}
	if item, ok := list.Selected(); ok {
		return r.SetFocus(item.ID)
	}
	return r.SetFocus(settingsListID(key))
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

func sectionTitle(key string) string {
	if key == "" {
		return SettingsGeneral
	}
	return key
}

func settingsListID(key string) string {
	if key == "" {
		return "__settings/list"
	}
	return "__settings/list/" + key
}

func settingItemID(name string, index int) string {
	return fmt.Sprintf("%s%s/%d", settingItemPrefix, name, index)
}

// refreshSettingsPage brings the open page up to date: a list per section
// (a section declared since gets one), each list's rows; selections stay
// on their rows.
func refreshSettingsPage(r *Runtime) {
	if r.Instances[SettingsPageID] == nil {
		return
	}
	s := r.Settings()
	values := s.List()
	for _, key := range settingSections(s) {
		var items []ListItem
		names := 0
		for _, v := range values {
			if v.SectionName() == key {
				row := settingItem(s, key, v)
				names = max(names, ansi.StringWidth(strings.TrimSuffix(row.Cells[0], " •")))
				items = append(items, row)
				if list, ok := v.Value.([]string); ok {
					for i, text := range list {
						items = append(items, ListItem{
							ID: settingItemID(v.Name, i), Label: text, Noun: settingItemNoun,
							Data: settingItemRef{Name: v.Name, Index: i}, Cells: []string{"", textCell(text)},
						})
					}
				}
			}
		}
		list := r.List(settingsListID(key))
		if list == nil {
			var err error
			if list, err = r.AddList(settingsListID(key), nil, Parent(SettingsPageID), NoMarks()); err != nil {
				continue
			}
		}
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
}

// settingItem is one setting's row: the name without its section's prefix
// (a • after it when the value is not the default) and the value.
func settingItem(s *Settings, section string, v SettingValue) ListItem {
	name := strings.TrimPrefix(v.Name, section+".")
	if section == "" || name == "" {
		name = v.Name
	}
	if settingDiffers(s, v.Name) {
		name += " •"
	}
	value := settingValueCell(v)
	var tones []ToneSpan
	if one := s.find(v.Name); one != nil && s.differsInSession(*one) && s.Bool(SettingsMarkSessionSetting) {
		// A session value: bold, the saved value beside it.
		tones = []ToneSpan{SpanTone(1, 0, len([]rune(value)), ToneSession)}
		value += "  (saved: " + savedCell(v) + ")"
	}
	return ListItem{
		ID: settingRowPrefix + v.Name, Label: name + "  " + value, Noun: "ochazuke.setting", Data: v.Name,
		Cells: []string{name, value}, Tones: tones,
	}
}

// savedCell is the persisted value beneath a session value as the
// marker shows it: a list's item count, (empty) for empty text.
func savedCell(v SettingValue) string {
	if list, ok := v.Saved.([]string); ok {
		return itemCount(len(list))
	}
	return textCell(settingCell(v.Saved))
}

// settingDiffers reports whether a setting's value is not its default.
func settingDiffers(s *Settings, name string) bool {
	one := s.find(name)
	return one != nil && !sameSetting(s.current(*one), one.Default)
}

// settingValueCell is a setting's value as its row shows it: ‹ value ›
// for one h/l steps, a list's item count, (empty) for empty text.
func settingValueCell(v SettingValue) string {
	if list, ok := v.Value.([]string); ok {
		return itemCount(len(list))
	}
	text := textCell(settingCell(v.Value))
	if settingSteps(v.Setting) {
		return "‹ " + text + " ›"
	}
	return text
}

func itemCount(n int) string {
	switch n {
	case 0:
		return "(none)"
	case 1:
		return "1 item"
	}
	return fmt.Sprintf("%d items", n)
}

// textCell is text as a cell shows it: (empty) for none.
func textCell(text string) string {
	if text == "" {
		return "(empty)"
	}
	return text
}

func settingCell(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []string:
		return strings.Join(t, ", ")
	}
	return fmt.Sprint(v)
}

// currentSection is the section the page shows: the focused list's (the
// focus moves the tabs with it), else the one last shown.
func currentSection(r *Runtime) string {
	s := r.Settings()
	sections := settingSections(s)
	for _, key := range sections {
		if list := r.List(settingsListID(key)); list != nil && list.Focused() {
			s.page.section = key
			return key
		}
	}
	if slices.Contains(sections, s.page.section) || len(sections) == 0 {
		return s.page.section
	}
	return sections[0]
}

// selectedSettingRow is the detail's item: the shown list's selected row.
func selectedSettingRow(r *Runtime) string {
	if list := r.List(settingsListID(currentSection(r))); list != nil {
		if item, ok := list.Selected(); ok {
			return item.ID
		}
	}
	return ""
}

// settingOfRow is the setting and item (-1 for the setting's own row) a
// row id names.
func settingOfRow(r *Runtime, id string) (string, int) {
	if name, ok := strings.CutPrefix(id, settingRowPrefix); ok {
		return name, -1
	}
	if inst := r.Instances[id]; inst != nil {
		if ref, ok := inst.Data.(settingItemRef); ok {
			return ref.Name, ref.Index
		}
	}
	return "", -1
}

// settingDetail is the description of one setting below the list: what it
// does, its default and where its value comes from, and what it takes; on
// an item row, which item it is first.
func settingDetail(s *Settings, name string, index int) (title, text string) {
	v, err := s.Get(name)
	if err != nil {
		return "Detail", "no setting selected"
	}
	var lines []string
	if list, ok := v.Value.([]string); ok && index >= 0 {
		lines = append(lines, fmt.Sprintf("Item %d of %d.", index+1, len(list)))
	}
	if v.Description != "" {
		lines = append(lines, v.Description)
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	def := textCell(settingCell(v.Default))
	if list, ok := v.Default.([]string); ok && len(list) == 0 {
		def = "(none)"
	}
	lines = append(lines,
		"Default is "+def+". "+settingSource(s, v),
		"Takes "+settingAllowed(v.Setting)+".")
	return v.Name, strings.Join(lines, "\n")
}

// settingSource says where a setting's value comes from: settings.json,
// or the default (also when the file holds a value that does not validate,
// with the reason).
func settingSource(s *Settings, v SettingValue) string {
	if v.Session {
		o, _ := s.Origin(v.Name)
		saved := SettingsFile + " holds " + textCell(settingCell(v.Saved))
		if !v.Set {
			saved = "the default applies beneath it"
		}
		line := o.String()
		return strings.ToUpper(line[:1]) + line[1:] + "; " + saved + ". setting.write saves it, setting.revert drops it."
	}
	switch {
	case v.Set:
		return "Set in " + SettingsFile + "."
	case s.raw[v.Name] != nil:
		prefix := SettingsFile + ": " + v.Name + ": "
		for _, p := range s.problems {
			if why, ok := strings.CutPrefix(p, prefix); ok {
				why = strings.TrimSuffix(why, ", using the default (kept)")
				return SettingsFile + " holds an invalid value (" + why + "), so the default applies."
			}
		}
		return SettingsFile + " holds an invalid value, so the default applies."
	}
	return "Not in " + SettingsFile + ", so the default applies."
}

// settingAllowed says what values a setting takes.
func settingAllowed(one Setting) string {
	var out string
	switch {
	case len(one.Enum) > 0:
		out = "one of " + strings.Join(one.Enum, ", ")
	case one.Type == SettingBoolean:
		out = "true or false"
	case one.Type == SettingInteger:
		out = "a whole number"
	case one.Type == SettingNumber:
		out = "a number"
	case one.Type == SettingDuration:
		out = "a duration such as 400ms or 2s"
	case one.Type == SettingList:
		out = "a list of text items"
	default:
		out = "any text"
	}
	if one.Validate != nil {
		out += ", checked by the application"
	}
	return out
}

// focusedSetting is the declared setting of the focused setting row.
func focusedSetting(r *Runtime, focus string) *Setting {
	inst := r.Instances[focus]
	if inst == nil || inst.Noun != "ochazuke.setting" {
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

// openSettingPrompt opens the one-line value prompt over the page.
func openSettingPrompt(r *Runtime, one Setting, scope string) error {
	current, _ := r.Settings().Get(one.Name)
	in := NewInput(settingValueID, settingCell(current.Value), false)
	in.Cursor = len(in.Text)
	prompt := fmt.Sprintf("%s (%s)", one.Name, one.Type)
	request := &Request{Noun: "ochazuke.setting", Verb: "set", Args: Arguments{"name": one.Name, "scope": scope}}
	_, err := r.OpenModal(settingPromptID, Payload{Prompt: prompt, Request: request}, WithInput(in, Kind("ochazuke.setting-value")))
	return err
}

// openItemPrompt opens the value prompt for a list's item: index >= 0
// edits that item (prefilled), else a new item goes in at insert.
func openItemPrompt(r *Runtime, one Setting, index, insert int) error {
	items := r.Settings().Strings(one.Name)
	text, args := "", Arguments{"name": one.Name}
	prompt := fmt.Sprintf("%s (list): new item %d", one.Name, insert+1)
	if index >= 0 {
		if index >= len(items) {
			return fmt.Errorf("%s has no item %d", one.Name, index+1)
		}
		text, args["index"] = items[index], index
		prompt = fmt.Sprintf("%s (list): item %d of %d", one.Name, index+1, len(items))
	} else {
		args["insert"] = insert
	}
	in := NewInput(settingValueID, text, false)
	in.Cursor = len(in.Text)
	request := &Request{Noun: "ochazuke.settings", Verb: "set", Args: args}
	_, err := r.OpenModal(settingPromptID, Payload{Prompt: prompt, Request: request}, WithInput(in, Kind("ochazuke.setting-value")))
	return err
}

// settingsPageView draws the page: the sections as tabs (when there is
// more than one), the shown section's rows over the selected setting's
// description (the divider follows the pane keys).
func settingsPageView(r *Runtime, width, height int) Block {
	if r.Instances[SettingsPageID] == nil || width <= 0 || height <= 0 {
		return Block{}
	}
	styles := NewStyles(Appearance{Theme: "adaptive", Dark: true})
	if view := r.Settings().page; view.styles != nil {
		styles = view.styles()
	}
	layout := r.PaneLayout(SettingsPageID, Layout{Styles: styles})
	key := currentSection(r)
	sections := settingSections(r.Settings())
	var rows []string
	if len(sections) > 1 && height > PaneMinHeight {
		titles := make([]string, len(sections))
		for i, k := range sections {
			titles[i] = sectionTitle(k)
		}
		rows = append(rows, layout.tabs(titles, slices.Index(sections, key), width))
	}
	body := height - len(rows)
	table := r.List(settingsListID(key))
	if table == nil {
		return Text(strings.Join(rows, "\n"))
	}
	// The rows take what they need (border included) up to three fifths
	// of the height; the description the rest.
	th := layout.Divide(min(max(table.Height()+2, PaneMinHeight), body*3/5), body, PaneMinHeight)
	tw, tth := PanelContentSize(width, th)
	tableBody := Text(table.View(tw, tth))
	dw, dch := PanelContentSize(width, body-th)
	detail := r.Detail(settingsDetailID)
	title, text := "Detail", ""
	if name, index := settingOfRow(r, selectedSettingRow(r)); name != "" {
		title, text = settingDetail(r.Settings(), name, index)
	}
	content, marker := "", ""
	if detail != nil {
		content, marker = detail.Window(text, dw, dch)
	}
	if marker != "" {
		title += " · " + marker
	}
	focused := SplitNone
	if table.Focused() {
		focused = SplitLeft
	}
	count := 0
	for _, one := range r.Settings().declared {
		if one.SectionName() == key {
			count++
		}
	}
	tableTitle := fmt.Sprintf("%s (%d)", sectionTitle(key), count)
	pair := layout.panelPair(tableTitle, tableBody, title, Text(content), width, body, th, true, focused)
	rows = append(rows, strings.Split(pair.Text, "\n")...)
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
