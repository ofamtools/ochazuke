package modal

import (
	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/form"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/palette"
	"ofam.dev/ochazuke/widgets/picker"
)

// PaletteOptions configures a PaletteModal.
type PaletteOptions struct {
	ID               string
	Scope            string
	Title            string
	Tone             string
	WidthPct         int
	ExpandedWidthPct int
	HeightPct        int
	PickerMode       string
	PickerFilterMode string
	StartInFilter    bool
	FormMode         string
	FormEditMode     string
	Badges           func(pal *palette.Model) []ui.Badge
	Facts            func(pal *palette.Model) map[string]any
	OnRawQuery       func(query string) (Result, bool)
	OnSubmit         func(entry palette.Entry, args map[string]any) Result
	OnPickPath       func(req form.PathRequest, current any) Result
	OnPickRef        func(req form.RefRequest) Result
	OnCommand        func(pal *palette.Model, cmd core.Command) (Result, bool)
	OnClose          func(reason CloseReason)
}

// PaletteModal adapts palette.Model to the Modal interface, synchronizing focus
// and mode transitions between the command picker and live arguments form.
type PaletteModal struct {
	pal              *palette.Model
	opts             PaletteOptions
	id               string
	scope            string
	pickerMode       string
	pickerFilterMode string
	formMode         string
	formEditMode     string
}

// NewPaletteModal wraps pal in a PaletteModal using opts.
func NewPaletteModal(pal *palette.Model, opts PaletteOptions) *PaletteModal {
	if pal == nil {
		pal = palette.New(opts.Title, nil)
	}
	if pal.Picker != nil {
		pal.Picker.Filtering = opts.StartInFilter
	}
	id := opts.ID
	if id == "" {
		id = "palette"
	}
	scope := opts.Scope
	if scope == "" {
		scope = "palette"
	}
	pickerMode := opts.PickerMode
	if pickerMode == "" {
		pickerMode = ModePicker
	}
	pickerFilterMode := opts.PickerFilterMode
	if pickerFilterMode == "" {
		pickerFilterMode = ModePickerFilter
	}
	formMode := opts.FormMode
	if formMode == "" {
		formMode = ModeForm
	}
	formEditMode := opts.FormEditMode
	if formEditMode == "" {
		formEditMode = ModeFormEdit
	}
	return &PaletteModal{
		pal:              pal,
		opts:             opts,
		id:               id,
		scope:            scope,
		pickerMode:       pickerMode,
		pickerFilterMode: pickerFilterMode,
		formMode:         formMode,
		formEditMode:     formEditMode,
	}
}

// Model returns the underlying palette model.
func (m *PaletteModal) Model() *palette.Model { return m.pal }

// ID returns the modal identifier.
func (m *PaletteModal) ID() string { return m.id }

// Mode returns the effective mode for the palette's current focus state.
func (m *PaletteModal) Mode() string {
	switch {
	case m.pal.EditingForm():
		return m.formEditMode
	case m.pal.FocusForm && m.pal.ActiveForm != nil:
		return m.formMode
	case m.pal.Picker != nil && m.pal.Picker.Filtering:
		return m.pickerFilterMode
	default:
		return m.pickerMode
	}
}

// Scope returns the modal overlay scope.
func (m *PaletteModal) Scope() string { return m.scope }

// FocusID returns the ViewNode ID currently focused inside the palette.
func (m *PaletteModal) FocusID() string { return m.pal.FocusID(m.id) }

// SetPickedValue applies a picked value to the active argument form.
func (m *PaletteModal) SetPickedValue(field string, value any) bool {
	return m.pal.SetPickedValue(field, value)
}

// View renders the command palette inside a width-adaptive Overlay.
func (m *PaletteModal) View(w, h int) ui.ViewNode {
	body := m.pal.View(m.id)

	wp := m.opts.WidthPct
	if wp <= 0 {
		wp = 70
	}
	if m.pal.ActiveForm != nil {
		ewp := m.opts.ExpandedWidthPct
		if ewp <= 0 {
			ewp = 90
		}
		wp = ewp
	}
	hp := m.opts.HeightPct
	if hp <= 0 {
		hp = 75
	}

	title := m.opts.Title
	if title == "" {
		title = m.pal.Title
	}
	if title == "" {
		title = "Palette"
	}

	var badges []ui.Badge
	if m.opts.Badges != nil {
		badges = m.opts.Badges(m.pal)
	}
	var facts map[string]any
	if m.opts.Facts != nil {
		facts = m.opts.Facts(m.pal)
	}

	return Overlay(OverlaySpec{
		ID:        m.id,
		Scope:     m.scope,
		Title:     title,
		Tone:      m.opts.Tone,
		WidthPct:  wp,
		HeightPct: hp,
		Badges:    badges,
		Facts:     facts,
		Children:  []ui.ViewNode{body},
	})
}

// Handle dispatches a command to the palette modal.
func (m *PaletteModal) Handle(cmd core.Command) Result {
	if m.opts.OnCommand != nil {
		if res, ok := m.opts.OnCommand(m.pal, cmd); ok {
			return res
		}
	}
	if cmd.ID == CmdCancel {
		return Result{Handled: true, Close: true, CloseReason: CloseCancel}
	}

	m.pal.Sync()
	if !m.pal.FocusForm {
		switch cmd.ID {
		case CmdPickerFilterStart:
			if m.pal.Picker != nil {
				m.pal.Picker.Filtering = true
			}
			m.pal.Sync()
			return Result{Handled: true, Mode: m.Mode(), FocusID: m.FocusID()}
		case CmdPickerFilterStop:
			if m.pal.Picker != nil {
				m.pal.Picker.Filtering = false
			}
			m.pal.Sync()
			return Result{Handled: true, Mode: m.Mode(), FocusID: m.FocusID()}
		case CmdPickerFilterClear:
			if m.pal.Picker != nil {
				m.pal.Picker.SetQuery("")
				m.pal.Picker.Filtering = false
			}
			m.pal.Sync()
			return Result{Handled: true, Mode: m.Mode(), FocusID: m.FocusID()}
		case CmdPickerInsert, input.CmdInsert:
			if m.pal.Picker != nil {
				m.pal.Picker.AppendQuery(cmd.Arg("text"))
			}
			m.pal.Sync()
			return Result{Handled: true, Mode: m.Mode(), FocusID: m.FocusID()}
		case CmdPickerBackspace, input.CmdBackspace:
			if m.pal.Picker != nil {
				m.pal.Picker.BackspaceQuery()
			}
			m.pal.Sync()
			return Result{Handled: true, Mode: m.Mode(), FocusID: m.FocusID()}
		case picker.CmdConfirm:
			if m.opts.OnRawQuery != nil && m.pal.Picker != nil {
				if res, ok := m.opts.OnRawQuery(m.pal.Picker.Query); ok {
					res.Handled = true
					if !res.Close && res.Push == nil {
						if res.Mode == "" {
							res.Mode = m.Mode()
						}
						if res.FocusID == "" {
							res.FocusID = m.FocusID()
						}
					}
					return res
				}
			}
		case "motion.scroll.down":
			cmd.ID = "motion.down"
		case "motion.scroll.up":
			cmd.ID = "motion.up"
		}
	} else if cmd.ID == CmdFormActivate && m.pal.ActiveForm != nil {
		cmd.ID = activateFormCommand(m.pal.ActiveForm)
	}

	pr, handled := m.pal.HandleCommand(cmd)
	if !handled {
		return Result{}
	}

	switch {
	case pr.PickPath != nil && m.pal.ActiveForm != nil:
		if m.opts.OnPickPath != nil {
			res := m.opts.OnPickPath(*pr.PickPath, m.pal.ActiveForm.Values[pr.PickPath.Field])
			res.Handled = true
			return res
		}
		return Result{Handled: true, Mode: m.Mode(), FocusID: m.FocusID()}
	case pr.PickRef != nil:
		if m.opts.OnPickRef != nil {
			res := m.opts.OnPickRef(*pr.PickRef)
			res.Handled = true
			return res
		}
		return Result{Handled: true, Mode: m.Mode(), FocusID: m.FocusID()}
	case pr.Submitted:
		if m.opts.OnSubmit != nil {
			res := m.opts.OnSubmit(pr.Entry, pr.Args)
			res.Handled = true
			return res
		}
		return Result{
			Handled:     true,
			Close:       true,
			CloseReason: CloseSubmit,
			Emit: []core.Command{{
				ID:     pr.Entry.Spec.ID,
				Args:   pr.Args,
				Source: "palette",
			}},
		}
	case pr.Cancelled:
		return Result{Handled: true, Close: true, CloseReason: CloseCancel}
	}

	return Result{Handled: true, Mode: m.Mode(), FocusID: m.FocusID()}
}

// OnClose notifies the configured close hook.
func (m *PaletteModal) OnClose(reason CloseReason) {
	if m.opts.OnClose != nil {
		m.opts.OnClose(reason)
	}
}
