package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/form"
)

// formSpecs is a command's parameter schema. widgets/form infers one control
// per type: text, number, enum, toggle, duration, path and list.
var formSpecs = []core.ParamSpec{
	{Name: "name", Type: core.ParamString, Required: true, Description: "service name"},
	{Name: "port", Type: core.ParamInt, Default: 8080},
	{Name: "level", Type: core.ParamString, Enum: []string{"debug", "info", "warn", "error"}, Default: "info"},
	{Name: "tls", Type: core.ParamBool},
	{Name: "timeout", Type: core.ParamDuration, Default: "30s"},
	{Name: "root", Type: core.ParamPath, Description: "a folder"},
	{Name: "tags", Type: core.ParamList, Description: "comma separated"},
}

func newForm() *form.Model {
	m := form.New("Deploy a service", formSpecs, nil)
	m.SubmitLabel = "Deploy"
	return m
}

func formBindings() []core.Binding {
	f := func(mode string, keys []string, cmd, hint string, args map[string]any) core.Binding {
		return core.Binding{Scope: "tab.form", Mode: mode, Keys: keys, Command: cmd, Hint: hint, Args: args}
	}
	return []core.Binding{
		f("normal", k("enter"), "form.edit", "edit", nil),
		f("normal", k("i"), "form.edit", "", nil),
		f("normal", k("space"), form.CmdToggle, "toggle", nil),
		f("normal", k("h"), form.CmdCycle, "prev option", map[string]any{"dir": -1}),
		f("normal", k("l"), form.CmdCycle, "next option", map[string]any{"dir": 1}),
		f("normal", k("S"), form.CmdSubmit, "submit", nil),
		f("form.edit", k("enter"), form.CmdStop, "done", nil),
		f("form.edit", k("esc"), form.CmdStop, "", nil),
		f("form.edit", k("backspace"), form.CmdBackspace, "", nil),
		{Scope: "tab.form", Mode: "form.edit", Class: core.ClassPrintable, Command: form.CmdInsert, Args: map[string]any{"text": "$text"}},
	}
}

func (a *app) formHandlers() {
	m := a.form
	a.route("tab.form", func(s core.State, c core.Command) core.State {
		switch c.ID {
		case "form.edit":
			// enter edits a text field and toggles a bool or an enum.
			if !m.Editable() {
				m.ToggleBool()
				return s
			}
			m.HandleCommand(core.Command{ID: form.CmdEdit})
			return setMode(s, "form.edit")
		case form.CmdStop:
			m.HandleCommand(c)
			return setMode(s, "normal")
		}
		res, _ := m.HandleCommand(c)
		switch {
		case res.Submitted:
			var parts []string
			for _, name := range slices.Sorted(maps.Keys(res.Values)) {
				parts = append(parts, fmt.Sprintf("%s=%v", name, res.Values[name]))
			}
			a.note = "submitted " + strings.Join(parts, " ")
		case len(res.ErrorFields) > 0:
			a.note = "fix " + strings.Join(res.ErrorFields, ", ")
		}
		return s
	}, slices.Concat(motions, k("form.edit", form.CmdStop, form.CmdToggle, form.CmdCycle, form.CmdSubmit, form.CmdInsert, form.CmdBackspace))...)
}

func (a *app) formView(core.State) []ui.ViewNode {
	fv := a.form.View("form")
	fv.Box = flex1
	return []ui.ViewNode{
		text(" Controls come from the schema. d cycles density, the default balanced style draws 3-line controls.", "muted"),
		fv,
	}
}
