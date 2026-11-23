package modal

import (
	"fmt"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/layout"
	"ofam.dev/ochazuke/ui"
)

// ConfirmSpecs returns the command specs for ConfirmModal actions.
func ConfirmSpecs() []core.CommandSpec {
	tags := []string{"modal", "confirm"}
	return []core.CommandSpec{
		{ID: CmdConfirmAccept, Description: "Accept the confirmation dialog", Tags: tags},
		{ID: CmdConfirmCancel, Description: "Cancel the confirmation dialog", Tags: tags},
	}
}

// ConfirmOptions configures a ConfirmModal.
type ConfirmOptions struct {
	ID           string
	Scope        string
	Mode         string
	Title        string
	Message      string
	ConfirmLabel string
	CancelLabel  string
	Tone         string
	WidthPct     int
	HeightPct    int
	Badges       []ui.Badge
	Facts        map[string]any
	OnConfirm    func() Result
	OnCancel     func() Result
	OnClose      func(reason CloseReason)
}

// ConfirmModal is a reusable yes/no confirmation overlay.
type ConfirmModal struct {
	opts    ConfirmOptions
	id      string
	scope   string
	mode    string
	focusID string
}

// NewConfirmModal creates a ConfirmModal with sensible defaults.
func NewConfirmModal(opts ConfirmOptions) *ConfirmModal {
	id := opts.ID
	if id == "" {
		id = "confirm"
	}
	scope := opts.Scope
	if scope == "" {
		scope = "confirm"
	}
	mode := opts.Mode
	if mode == "" {
		mode = ModeConfirm
	}
	return &ConfirmModal{
		opts:    opts,
		id:      id,
		scope:   scope,
		mode:    mode,
		focusID: id + ".body",
	}
}

// ID returns the modal identifier.
func (m *ConfirmModal) ID() string { return m.id }

// Mode returns the modal input mode.
func (m *ConfirmModal) Mode() string { return m.mode }

// Scope returns the modal overlay scope.
func (m *ConfirmModal) Scope() string { return m.scope }

// FocusID returns the focus target node ID.
func (m *ConfirmModal) FocusID() string { return m.focusID }

// View renders the confirmation message and action hint inside an Overlay.
func (m *ConfirmModal) View(w, h int) ui.ViewNode {
	title := m.opts.Title
	if title == "" {
		title = "Confirm"
	}
	confirmLabel := m.opts.ConfirmLabel
	if confirmLabel == "" {
		confirmLabel = "Confirm"
	}
	cancelLabel := m.opts.CancelLabel
	if cancelLabel == "" {
		cancelLabel = "Cancel"
	}
	tone := m.opts.Tone
	if tone == "" {
		tone = "warning"
	}
	wp := m.opts.WidthPct
	if wp <= 0 {
		wp = 70
	}

	body := ui.ViewNode{
		ID:        m.focusID,
		Kind:      ui.KindVStack,
		Focusable: true,
		Gap:       1,
		Children: []ui.ViewNode{
			{
				ID:    m.id + ".message",
				Kind:  ui.KindText,
				Props: map[string]any{"text": m.opts.Message},
			},
			{
				ID:    m.id + ".actions",
				Kind:  ui.KindText,
				Tone:  "accent",
				Box:   layout.Constraints{FixedH: 1},
				Props: map[string]any{"text": fmt.Sprintf("[y/enter] %s   [n/esc] %s", confirmLabel, cancelLabel)},
			},
		},
	}

	return Overlay(OverlaySpec{
		ID:        m.id,
		Scope:     m.scope,
		Title:     title,
		Tone:      tone,
		WidthPct:  wp,
		HeightPct: m.opts.HeightPct,
		Badges:    m.opts.Badges,
		Facts:     m.opts.Facts,
		Children:  []ui.ViewNode{body},
	})
}

// Handle handles CmdConfirmAccept, CmdConfirmCancel, and CmdCancel.
func (m *ConfirmModal) Handle(cmd core.Command) Result {
	switch cmd.ID {
	case CmdConfirmAccept:
		var res Result
		if m.opts.OnConfirm != nil {
			res = m.opts.OnConfirm()
		}
		res.Handled = true
		res.Close = true
		res.CloseReason = CloseSubmit
		return res
	case CmdConfirmCancel, CmdCancel:
		if m.opts.OnCancel != nil {
			res := m.opts.OnCancel()
			res.Handled = true
			res.Close = true
			res.CloseReason = CloseCancel
			return res
		}
		return Result{
			Handled:     true,
			Close:       true,
			CloseReason: CloseCancel,
			Status:      "cancelled",
			SetStatus:   true,
		}
	}
	return Result{}
}

// OnClose notifies the configured close hook.
func (m *ConfirmModal) OnClose(reason CloseReason) {
	if m.opts.OnClose != nil {
		m.opts.OnClose(reason)
	}
}
