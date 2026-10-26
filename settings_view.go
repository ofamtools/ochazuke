package ochazuke

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// The settings editor: settings.edit opens a modal over the focus holding
// a table of the declared settings, one row per setting of the "setting"
// noun. Every change is saved as it is made; there is no save key. On a
// row, enter edits (a boolean flips, an enum takes its next value, any
// other type opens a one-line prompt prefilled with the value: enter sets
// it, esc leaves it), l/h and space step a boolean, an enum or a number,
// r resets to the default, esc closes. A value another process sets
// (the poll) redraws the open table.
const (
	settingsEditorID = "settings-editor"
	settingsListID   = "__settings/list"
	settingPromptID  = "setting-prompt"
	settingValueID   = "__settings/value"
	settingRowPrefix = "__settings/"
)

func registerSettingsEditor(r *Runtime) {
	r.Register(Operation{Verb: "edit", Noun: "settings", Description: "Open the settings editor: every change is saved as it is made", Result: "setting[]", Run: func(r *Runtime, c Context, a Arguments) (any, error) {
		s := r.Settings()
		if len(s.Declared()) == 0 {
			return nil, errors.New("this application declares no settings")
		}
		if _, err := r.OpenModal(settingsEditorID, Payload{}); err != nil {
			return nil, err
		}
		list, err := r.AddList(settingsListID, settingItems(s), Parent(settingsEditorID), Columns(
			Column{Title: "Setting", Priority: 3},
			Column{Title: "Value", Priority: 3},
			Column{Title: "Default", Priority: 1},
			Column{Title: "Description", Priority: 2},
		))
		if err != nil {
			_ = r.Pop()
			return nil, err
		}
		return s.List(), list.Focus()
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
			refreshSettingsEditor(r)
			return r.Settings().Get(one.Name)
		}, "Reset the setting to its default").
		Mode("normal").
		Bind("enter", Do("setting.edit")).
		Bind("l", Do("setting.cycle", Arguments{"step": 1}).Guard("setting-steps")).
		Bind("right", Do("setting.cycle", Arguments{"step": 1}).Guard("setting-steps")).
		Bind("space", Do("setting.cycle", Arguments{"step": 1}).Guard("setting-steps")).
		Bind("h", Do("setting.cycle", Arguments{"step": -1}).Guard("setting-steps")).
		Bind("left", Do("setting.cycle", Arguments{"step": -1}).Guard("setting-steps")).
		Bind("r", Do("setting.reset"))
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
			refreshSettingsEditor(r)
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
	r.Settings().Observe("", func(string, any) { refreshSettingsEditor(r) })
}

// settingItems is one table row per declared setting.
func settingItems(s *Settings) []ListItem {
	values := s.List()
	items := make([]ListItem, len(values))
	for i, v := range values {
		value := settingCell(v.Value)
		if v.Set {
			value += " *"
		}
		items[i] = ListItem{
			ID: settingRowPrefix + v.Name, Label: v.Name + "  " + value, Noun: "setting", Data: v.Name,
			Cells: []string{v.Name, value, settingCell(v.Default), settingDescription(v.Setting)},
		}
	}
	return items
}

func settingCell(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

// settingDescription is the description with an enum's choices.
func settingDescription(one Setting) string {
	if len(one.Enum) == 0 {
		return one.Description
	}
	return strings.TrimSpace(one.Description + " (" + strings.Join(one.Enum, ", ") + ")")
}

// refreshSettingsEditor redraws the open table; the selection stays on
// its setting.
func refreshSettingsEditor(r *Runtime) {
	if list := r.List(settingsListID); list != nil {
		_ = list.SetItems(settingItems(r.Settings()))
	}
}

// focusedSetting is the declared setting of the focused editor row.
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
	refreshSettingsEditor(r)
	return s.Get(one.Name)
}

// openSettingPrompt opens the one-line value prompt over the editor.
func openSettingPrompt(r *Runtime, one Setting) error {
	current, _ := r.Settings().Get(one.Name)
	in := NewInput(settingValueID, settingCell(current.Value), false)
	in.Cursor = len(in.Text)
	prompt := fmt.Sprintf("%s (%s)", one.Name, one.Type)
	request := &Request{Noun: "settings", Verb: "set", Args: Arguments{"name": one.Name}}
	_, err := r.OpenModal(settingPromptID, Payload{Prompt: prompt, Request: request}, WithInput(in, Kind("setting-value")))
	return err
}

// settingsEditorView draws the table at the modal's size.
func settingsEditorView(r *Runtime, _ *Instance, width, height int) Block {
	list := r.List(settingsListID)
	if list == nil {
		return Text("")
	}
	return Text(list.View(width, height))
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
