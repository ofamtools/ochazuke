// Package cli is the thin, opt-in Tier 3 bridge connecting ofam.dev/oden with
// Ochazuke v2 (core, ui, tea, widgets/picker, widgets/bindings, widgets/input).
package cli

import (
	"context"
	"os"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/core"
	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/picker"
	"ofam.dev/oden"
)

const pickerNodeID = "cli.picker"

// PickerModel is a lightweight Bubble Tea model wrapping Ochazuke's
// widgets/picker over a list of oden.Candidate items. It can be run directly
// via tea.NewProgram or driven headlessly in unit tests.
type PickerModel struct {
	shell     *otea.Shell
	picker    *picker.Model
	selected  *oden.Candidate
	cancelled bool
	done      bool
}

// NewPickerModel constructs a pure Bubble Tea model for interactively picking
// one oden.Candidate with Readline query editing and Ochazuke theme rendering.
func NewPickerModel(title, query string, items []oden.Candidate, theme ui.Theme) *PickerModel {
	if title == "" {
		title = "Select item"
	}
	pItems := make([]picker.Item, len(items))
	for i, c := range items {
		label := c.Title
		if label == "" {
			label = c.ID
		}
		var badges []ui.Badge
		if c.ID != "" && c.ID != label {
			badges = append(badges, ui.Badge{Text: shortID(c.ID), Tone: "dim"})
		}
		for _, b := range c.Badges {
			if b != "" {
				badges = append(badges, ui.Badge{Text: b, Tone: "badge"})
			}
		}
		keywords := make([]string, 0, 1+len(c.Badges))
		if c.ID != "" {
			keywords = append(keywords, c.ID)
		}
		keywords = append(keywords, c.Badges...)
		pItems[i] = picker.Item{
			ID:       c.ID,
			Title:    label,
			Subtitle: c.Subtitle,
			Badges:   badges,
			Keywords: keywords,
			Data:     c,
		}
	}

	pm := picker.New(title, pItems)
	pm.Placeholder = "Type to filter..."
	if query != "" {
		pm.SetQuery(query)
	}

	modes := []core.Mode{{Name: "picker", Passthrough: true}}
	binds := append([]core.Binding{}, input.ReadlineBindings("picker", "picker")...)
	binds = append(binds,
		core.Binding{Mode: "picker", Scope: "picker", Keys: []string{"down"}, Command: "motion.down", Hint: "down"},
		core.Binding{Mode: "picker", Scope: "picker", Keys: []string{"ctrl+n"}, Command: "motion.down", Hint: "down", Hidden: true},
		core.Binding{Mode: "picker", Scope: "picker", Keys: []string{"up"}, Command: "motion.up", Hint: "up"},
		core.Binding{Mode: "picker", Scope: "picker", Keys: []string{"ctrl+p"}, Command: "motion.up", Hint: "up", Hidden: true},
		core.Binding{Mode: "picker", Scope: "picker", Keys: []string{"pgdown"}, Command: "motion.page.down", Hidden: true},
		core.Binding{Mode: "picker", Scope: "picker", Keys: []string{"pgup"}, Command: "motion.page.up", Hidden: true},
		core.Binding{Mode: "picker", Scope: "picker", Keys: []string{"enter"}, Command: picker.CmdConfirm, Hint: "select"},
		core.Binding{Mode: "picker", Scope: "picker", Keys: []string{"tab"}, Command: picker.CmdModeCycle, Hint: "match mode"},
		core.Binding{Mode: "picker", Scope: "picker", Keys: []string{"esc"}, Command: core.CmdAppQuit, Hint: "cancel"},
		core.Binding{Mode: "picker", Scope: "picker", Keys: []string{"ctrl+c"}, Command: core.CmdAppQuit, Hint: "cancel", Hidden: true},
		core.Binding{Mode: "picker", Scope: "picker", Class: core.ClassPrintable, Command: input.CmdInsert, Args: map[string]any{"text": "$text"}, Hidden: true},
		core.Binding{Mode: "picker", Scope: "picker", Class: core.ClassPaste, Command: input.CmdInsert, Args: map[string]any{"text": "$text"}, Hidden: true},
	)

	specs := append([]core.CommandSpec{}, input.ReadlineSpecs()...)
	specs = append(specs,
		core.CommandSpec{ID: input.CmdInsert, Description: "Insert text at the query cursor"},
		core.CommandSpec{ID: picker.CmdConfirm, Description: "Select the focused candidate"},
		core.CommandSpec{ID: picker.CmdModeCycle, Description: "Cycle picker match mode"},
	)

	km, _ := core.NewKeymap(modes, binds, specs...)
	eng := core.NewEngine(km)
	eng.DefaultMode = "picker"

	state := core.NewState(core.Frame{
		ID:      "picker",
		FocusID: pickerNodeID,
		Mode:    "picker",
	})

	m := &PickerModel{picker: pm}
	sh := otea.New(eng, state, func(core.State) ui.ViewNode {
		return pm.View(pickerNodeID)
	}, theme)
	sh.MaxFPS = 0
	sh.W, sh.H = 80, 24
	sh.OnEmit = func(cmd core.Command) tea.Cmd {
		if cmd.ID == input.CmdInsert && cmd.Arg("text") == "" && cmd.Source == "key:space" {
			if cmd.Args == nil {
				cmd.Args = map[string]any{}
			}
			cmd.Args["text"] = " "
		}
		picked, _ := pm.HandleCommand(cmd)
		if picked != nil {
			if cand, ok := picked.Data.(oden.Candidate); ok {
				c := cand
				m.selected = &c
			} else {
				m.selected = &oden.Candidate{ID: picked.ID, Title: picked.Title, Subtitle: picked.Subtitle}
			}
			m.done = true
			return tea.Quit
		}
		return nil
	}
	m.shell = sh
	return m
}

// Init implements tea.Model.
func (m *PickerModel) Init() tea.Cmd { return m.shell.Init() }

// Update implements tea.Model.
func (m *PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.shell.Update(msg)
	for _, eff := range m.shell.LastEffects {
		if _, ok := eff.(core.QuitEffect); ok {
			if m.selected == nil {
				m.cancelled = true
			}
			m.done = true
		}
	}
	return m, cmd
}

// View implements tea.Model.
func (m *PickerModel) View() tea.View { return m.shell.View() }

// Selected returns the confirmed candidate and true, or the zero value and false.
func (m *PickerModel) Selected() (oden.Candidate, bool) {
	if m.selected == nil {
		return oden.Candidate{}, false
	}
	return *m.selected, true
}

// Cancelled reports whether the user cancelled selection (esc / ctrl+c).
func (m *PickerModel) Cancelled() bool { return m.cancelled }

// Done reports whether selection has finished (confirmed or cancelled).
func (m *PickerModel) Done() bool { return m.done }

// Query returns the current filter query text.
func (m *PickerModel) Query() string { return m.picker.Query }

// Picker returns the underlying Ochazuke picker widget model.
func (m *PickerModel) Picker() *picker.Model { return m.picker }

// NewPickerFunc returns an oden.PickerFunc that runs an interactive Ochazuke
// picker on os.Stderr (keeping os.Stdout clean for JSON/piping).
func NewPickerFunc(theme ui.Theme) oden.PickerFunc {
	return func(ctx context.Context, title, query string, items []oden.Candidate) (oden.Candidate, bool, error) {
		m := NewPickerModel(title, query, items, theme)
		p := tea.NewProgram(m, tea.WithOutput(os.Stderr), tea.WithContext(ctx))
		finalModel, err := p.Run()
		if err != nil {
			return oden.Candidate{}, false, err
		}
		if pm, ok := finalModel.(*PickerModel); ok {
			if sel, ok := pm.Selected(); ok {
				return sel, true, nil
			}
		}
		return oden.Candidate{}, false, nil
	}
}

// DefaultPicker returns an interactive oden.PickerFunc using ui.DefaultDarkTheme().
func DefaultPicker() oden.PickerFunc {
	return NewPickerFunc(ui.DefaultDarkTheme())
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
