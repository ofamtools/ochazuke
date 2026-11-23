package modal

import (
	"fmt"
	"maps"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/form"
)

// FormOptions configures a FormModal.
type FormOptions struct {
	ID             string
	Scope          string
	NormalMode     string
	EditMode       string
	FocusID        string
	Title          string
	Tone           string
	WidthPct       int
	HeightPct      int
	DisableDrafts  bool
	DisableSubmit  bool
	DraftBadges    bool
	Ref            *core.Ref
	Badges         func(fm *form.Model) []ui.Badge
	Facts          func(fm *form.Model) map[string]any
	RenderBody     func(fm *form.Model, focusID string) ui.ViewNode
	OnSubmit       func(values map[string]any) Result
	OnChange       func(fm *form.Model, field string) Result
	OnPickPath     func(req form.PathRequest, current any) Result
	OnPickRef      func(req form.RefRequest) Result
	OnDraftDiscard func(idx int) Result
	OnCommand      func(fm *form.Model, cmd core.Command) (Result, bool)
	OnClose        func(reason CloseReason)
}

// FormModal adapts form.Model to the Modal interface, synchronizing NormalMode
// and EditMode with form.Model.Editing.
type FormModal struct {
	fm         *form.Model
	opts       FormOptions
	id         string
	scope      string
	normalMode string
	editMode   string
	focusID    string
}

// NewFormModal wraps fm in a FormModal using opts.
func NewFormModal(fm *form.Model, opts FormOptions) *FormModal {
	if fm == nil {
		fm = form.New(opts.Title, nil, nil)
	}
	id := opts.ID
	if id == "" {
		id = "form"
	}
	scope := opts.Scope
	if scope == "" {
		scope = id
	}
	normalMode := opts.NormalMode
	if normalMode == "" {
		normalMode = ModeForm
	}
	editMode := opts.EditMode
	if editMode == "" {
		editMode = ModeFormEdit
	}
	focusID := opts.FocusID
	if focusID == "" {
		focusID = id + ".form"
	}
	return &FormModal{
		fm:         fm,
		opts:       opts,
		id:         id,
		scope:      scope,
		normalMode: normalMode,
		editMode:   editMode,
		focusID:    focusID,
	}
}

// Model returns the underlying form model.
func (m *FormModal) Model() *form.Model { return m.fm }

// ID returns the modal identifier.
func (m *FormModal) ID() string { return m.id }

// Mode returns EditMode while editing a text field, otherwise NormalMode.
func (m *FormModal) Mode() string {
	if m.fm != nil && m.fm.Editing {
		return m.editMode
	}
	return m.normalMode
}

// Scope returns the modal overlay scope.
func (m *FormModal) Scope() string { return m.scope }

// FocusID returns the focused ViewNode ID inside the form modal.
func (m *FormModal) FocusID() string { return m.focusID }

// SetValue updates a field on the underlying form and notifies OnChange when set.
func (m *FormModal) SetValue(field string, value any) bool {
	if m.fm == nil {
		return false
	}
	if !m.fm.SetValue(field, value) {
		return false
	}
	if m.opts.OnChange != nil {
		_ = m.opts.OnChange(m.fm, field)
	}
	return true
}

// FocusField moves ActiveIdx to the named field and stops text editing.
func (m *FormModal) FocusField(field string) bool {
	if m.fm == nil {
		return false
	}
	return m.fm.FocusField(field)
}

// View renders the form inside an Overlay.
func (m *FormModal) View(w, h int) ui.ViewNode {
	title := m.opts.Title
	scope := m.scope
	ref := m.opts.Ref
	var children []ui.ViewNode
	var badges []ui.Badge
	var facts map[string]any

	if m.opts.RenderBody != nil {
		body := m.opts.RenderBody(m.fm, m.focusID)
		if body.Kind == ui.KindPanel {
			if title == "" {
				title = body.Title
			}
			if m.opts.Scope == "" && body.Scope != "" {
				scope = body.Scope
			}
			if ref == nil && body.Ref != nil {
				ref = body.Ref
			}
			if len(body.Badges) > 0 {
				badges = append(badges, body.Badges...)
			}
			if len(body.Facts) > 0 {
				facts = maps.Clone(body.Facts)
			}
			children = body.Children
		} else {
			children = []ui.ViewNode{body}
		}
	} else {
		children = []ui.ViewNode{m.fm.View(m.focusID)}
	}

	if title == "" {
		title = m.fm.Title
	}
	if title == "" {
		title = m.id
	}
	if m.opts.DraftBadges {
		badges = append(badges, formDraftBadges(m.fm)...)
	}
	if m.opts.Badges != nil {
		badges = append(badges, m.opts.Badges(m.fm)...)
	}
	if m.opts.Facts != nil {
		if extra := m.opts.Facts(m.fm); len(extra) > 0 {
			if facts == nil {
				facts = make(map[string]any, len(extra))
			}
			maps.Copy(facts, extra)
		}
	}

	wp := m.opts.WidthPct
	if wp <= 0 {
		wp = 70
	}

	return Overlay(OverlaySpec{
		ID:        m.id,
		Scope:     scope,
		Title:     title,
		Tone:      m.opts.Tone,
		WidthPct:  wp,
		HeightPct: m.opts.HeightPct,
		Badges:    badges,
		Ref:       ref,
		Facts:     facts,
		Children:  children,
	})
}

// Handle dispatches a command to the form modal.
func (m *FormModal) Handle(cmd core.Command) Result {
	if m.opts.OnCommand != nil {
		if res, ok := m.opts.OnCommand(m.fm, cmd); ok {
			return res
		}
	}
	if cmd.ID == CmdCancel {
		return Result{Handled: true, Close: true, CloseReason: CloseCancel}
	}
	if m.opts.DisableDrafts {
		switch cmd.ID {
		case form.CmdDraftPrev, form.CmdDraftNext, form.CmdDraftDiscard:
			return Result{Handled: true, Mode: m.Mode()}
		}
	}
	if m.opts.DisableSubmit && cmd.ID == form.CmdSubmit {
		return Result{Handled: true, Mode: m.Mode()}
	}

	id := cmd.ID
	if id == CmdFormActivate {
		id = activateFormCommand(m.fm)
		if m.opts.DisableSubmit && id == form.CmdSubmit {
			return Result{Handled: true, Mode: m.Mode()}
		}
		cmd.ID = id
	}

	wasEditing := m.fm.Editing
	prevDraftIdx := m.fm.ActiveDraftIndex()
	fr, handled := m.fm.HandleCommand(cmd)
	if !handled {
		return Result{}
	}

	switch {
	case fr.PickPath != nil:
		if m.opts.OnPickPath != nil {
			res := m.opts.OnPickPath(*fr.PickPath, currentFieldValue(m.fm, fr.PickPath.Field))
			res.Handled = true
			return res
		}
		return Result{Handled: true, Mode: m.Mode()}
	case fr.PickRef != nil:
		if m.opts.OnPickRef != nil {
			res := m.opts.OnPickRef(*fr.PickRef)
			res.Handled = true
			return res
		}
		return Result{Handled: true, Mode: m.Mode()}
	case fr.HasDiscardedDraft:
		if m.opts.OnDraftDiscard != nil {
			res := m.opts.OnDraftDiscard(fr.DiscardedDraftIdx)
			res.Handled = true
			if !res.Close && res.Push == nil && res.Mode == "" {
				res.Mode = m.Mode()
			}
			return res
		}
		return Result{Handled: true, Mode: m.Mode(), Status: "discarded draft", SetStatus: true}
	case fr.Submitted:
		if m.opts.OnSubmit != nil {
			res := m.opts.OnSubmit(fr.Values)
			res.Handled = true
			if !res.Close && res.Push == nil && res.Mode == "" {
				res.Mode = m.Mode()
			}
			return res
		}
		return Result{Handled: true, Close: true, CloseReason: CloseSubmit}
	case len(fr.ErrorFields) > 0:
		return Result{
			Handled:   true,
			Mode:      m.Mode(),
			Status:    "still needed: " + strings.Join(fr.ErrorFields, ", "),
			SetStatus: true,
		}
	case fr.Cancelled:
		return Result{Handled: true, Close: true, CloseReason: CloseCancel}
	}

	var status string
	var setStatus bool
	switch id {
	case form.CmdDraftPrev:
		if idx := m.fm.ActiveDraftIndex(); idx >= 0 {
			status, setStatus = fmt.Sprintf("draft %d of %d", idx+1, m.fm.DraftCount()), true
		} else if m.fm.DraftCount() == 0 {
			status, setStatus = "no drafts saved", true
		}
	case form.CmdDraftNext:
		if idx := m.fm.ActiveDraftIndex(); idx >= 0 {
			status, setStatus = fmt.Sprintf("draft %d of %d", idx+1, m.fm.DraftCount()), true
		} else if prevDraftIdx >= 0 {
			status, setStatus = "back to current edits", true
		}
	}

	if m.opts.OnChange != nil && isFormCommitChange(id, wasEditing) {
		field := ""
		if id == form.CmdSet {
			field = cmd.Arg("field")
		} else {
			field = m.fm.ActiveFieldName()
		}
		res := m.opts.OnChange(m.fm, field)
		res.Handled = true
		if !res.Close && res.Push == nil && res.Mode == "" {
			res.Mode = m.Mode()
		}
		if setStatus && !res.SetStatus {
			res.Status, res.SetStatus = status, true
		}
		return res
	}

	return Result{
		Handled:   true,
		Mode:      m.Mode(),
		Status:    status,
		SetStatus: setStatus,
	}
}

// OnClose notifies the configured close hook.
func (m *FormModal) OnClose(reason CloseReason) {
	if m.opts.OnClose != nil {
		m.opts.OnClose(reason)
	}
}

func currentFieldValue(fm *form.Model, field string) any {
	v := fm.Values[field]
	if items, ok := v.([]string); ok {
		if idx := fm.ActiveItemIndex(); idx >= 0 && idx < len(items) {
			return items[idx]
		}
		return ""
	}
	return v
}

func isFormCommitChange(cmdID string, wasEditing bool) bool {
	switch cmdID {
	case form.CmdToggle, form.CmdCycle, form.CmdSet, form.CmdItemAdd, form.CmdItemRemove:
		return true
	case form.CmdStop:
		return wasEditing
	}
	return false
}

// activateFormCommand maps CmdFormActivate to the concrete form command for
// the currently focused row (submit row, add row, bool/enum toggle, path/ref pick, or edit).
func activateFormCommand(fm *form.Model) string {
	if fm == nil {
		return form.CmdEdit
	}
	if fm.OnSubmitRow() {
		return form.CmdSubmit
	}
	if fm.OnAddRow() {
		return form.CmdItemAdd
	}
	switch fm.ActiveKind() {
	case form.KindBool, form.KindEnum:
		return form.CmdToggle
	case form.KindPath, form.KindRef:
		return form.CmdPick
	default:
		return form.CmdEdit
	}
}

func formDraftBadges(fm *form.Model) []ui.Badge {
	if fm == nil {
		return nil
	}
	var out []ui.Badge
	if idx := fm.ActiveDraftIndex(); idx >= 0 {
		out = append(out, ui.Badge{
			Text: fmt.Sprintf("draft %d/%d · [ / ] · D discard", idx+1, fm.DraftCount()),
			Tone: "warning",
		})
	} else if n := fm.DraftCount(); n > 0 {
		out = append(out, ui.Badge{
			Text: fmt.Sprintf("%d draft(s) · [ restores", n),
			Tone: "info",
		})
	}
	if fm.Dirty {
		out = append(out, ui.Badge{Text: "unsaved · ctrl+s submits", Tone: "accent"})
	}
	return out
}
