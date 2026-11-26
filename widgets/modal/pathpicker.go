package modal

import (
	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/pathpicker"
)

// PathPickerOptions configures a PathPickerModal.
type PathPickerOptions struct {
	ID            string
	Scope         string
	Mode          string
	FilterMode    string
	StartInFilter bool
	FocusID       string
	Title         string
	Tone          string
	WidthPct      int
	HeightPct     int
	Badges        func(pp *pathpicker.Model) []ui.Badge
	Facts         func(pp *pathpicker.Model) map[string]any
	OnPick        func(entry pathpicker.DirEntry) Result
	OnCommand     func(pp *pathpicker.Model, cmd core.Command) (Result, bool)
	OnClose       func(reason CloseReason)
}

// PathPickerModal adapts pathpicker.Model to the Modal interface.
type PathPickerModal struct {
	pp         *pathpicker.Model
	opts       PathPickerOptions
	id         string
	scope      string
	normalMode string
	filterMode string
	focusID    string
}

// NewPathPickerModal wraps pp in a PathPickerModal using opts.
func NewPathPickerModal(pp *pathpicker.Model, opts PathPickerOptions) *PathPickerModal {
	id, scope, focusID := resolveModalIdentity(opts.ID, opts.Scope, opts.FocusID, "pathpicker", "pathpicker", "")
	normalMode := opts.Mode
	if normalMode == "" {
		normalMode = ModePicker
	}
	filterMode := opts.FilterMode
	if filterMode == "" {
		filterMode = ModePickerFilter
	}
	if pp != nil {
		pp.SetFiltering(opts.StartInFilter)
	}
	return &PathPickerModal{
		pp:         pp,
		opts:       opts,
		id:         id,
		scope:      scope,
		normalMode: normalMode,
		filterMode: filterMode,
		focusID:    focusID,
	}
}

// Model returns the underlying pathpicker model.
func (m *PathPickerModal) Model() *pathpicker.Model { return m.pp }

// ID returns the modal identifier.
func (m *PathPickerModal) ID() string { return m.id }

// Mode returns the modal input mode.
func (m *PathPickerModal) Mode() string {
	if m.pp != nil && m.pp.Filtering() {
		return m.filterMode
	}
	return m.normalMode
}

// Scope returns the modal overlay scope.
func (m *PathPickerModal) Scope() string { return m.scope }

// FocusID returns the focused ViewNode ID.
func (m *PathPickerModal) FocusID() string { return m.focusID }

// View wraps pathpicker.Model.View's panel children in an Overlay, mirroring
// inner facts onto the overlay for scope-targeted bindings.
func (m *PathPickerModal) View(w, h int) ui.ViewNode {
	frame := Frame{
		ID:        m.id,
		Scope:     m.scope,
		Title:     m.opts.Title,
		Tone:      m.opts.Tone,
		WidthPct:  m.opts.WidthPct,
		HeightPct: m.opts.HeightPct,
	}.WithDefaults(Frame{WidthPct: 70, HeightPct: 70})
	if m.pp == nil {
		return frame.WithDefaults(Frame{Title: m.id}).Overlay()
	}

	panel := m.pp.View(m.focusID)
	frame = frame.WithDefaults(Frame{Title: panel.Title})
	badges := append([]ui.Badge(nil), panel.Badges...)
	if m.opts.Badges != nil {
		badges = append(badges, m.opts.Badges(m.pp)...)
	}
	frame.Badges = badges
	if m.opts.Facts != nil {
		frame.Facts = m.opts.Facts(m.pp)
	}
	return frame.Overlay(panel.Children...)
}

// Handle dispatches a command to the path picker modal.
func (m *PathPickerModal) Handle(cmd core.Command) Result {
	if m.opts.OnCommand != nil {
		if res, ok := m.opts.OnCommand(m.pp, cmd); ok {
			return res
		}
	}
	if cmd.ID == CmdCancel {
		return Result{Handled: true, Close: true, CloseReason: CloseCancel}
	}
	if m.pp == nil {
		return Result{}
	}
	switch cmd.ID {
	case CmdPickerFilterStart:
		m.pp.SetFiltering(true)
		return Result{Handled: true, Mode: m.Mode()}
	case CmdPickerFilterStop:
		m.pp.SetFiltering(false)
		return Result{Handled: true, Mode: m.Mode()}
	case CmdPickerFilterClear:
		m.pp.HandleCommand(cmd)
		return Result{Handled: true, Mode: m.Mode()}
	case CmdPickerInsert, input.CmdInsert:
		m.pp.AppendQuery(cmd.Arg("text"))
		return Result{Handled: true, Mode: m.Mode()}
	case CmdPickerBackspace, input.CmdBackspace:
		m.pp.BackspaceQuery()
		return Result{Handled: true, Mode: m.Mode()}
	}

	picked, handled := m.pp.HandleCommand(cmd)
	if !handled {
		return Result{}
	}
	if picked != nil {
		if m.opts.OnPick != nil {
			res := m.opts.OnPick(*picked)
			res.Handled = true
			if !res.Close && res.Push == nil && res.Mode == "" {
				res.Mode = m.Mode()
			}
			return res
		}
		return Result{Handled: true, Close: true, CloseReason: CloseSubmit}
	}
	return Result{Handled: true, Mode: m.Mode()}
}

// OnClose notifies the configured close hook.
func (m *PathPickerModal) OnClose(reason CloseReason) {
	if m.opts.OnClose != nil {
		m.opts.OnClose(reason)
	}
}
