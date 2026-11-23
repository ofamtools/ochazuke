package modal

import (
	"fmt"
	"maps"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/markdown"
)

// MarkdownPage is one saved entry in a MarkdownModal's back-navigation stack.
type MarkdownPage struct {
	Title  string
	Source string
	Path   string
	Offset int
}

// MarkdownOptions configures a MarkdownModal.
type MarkdownOptions struct {
	ID              string
	Scope           string
	Mode            string
	FocusID         string
	Path            string
	Tone            string
	WidthPct        int
	HeightPct       int
	ShowStatsBadges bool
	Badges          func(md *markdown.Model, path string, backLen int) []ui.Badge
	Facts           func(md *markdown.Model, path string, backLen int) map[string]any
	OnOpenLink      func(href string) Result
	OnYankCode      func(code string) Result
	OnCommand       func(md *markdown.Model, cmd core.Command) (Result, bool)
	OnClose         func(reason CloseReason)
}

// MarkdownModal adapts markdown.Model to the Modal interface with responsive
// PageSize sizing and a built-in back-navigation history stack.
type MarkdownModal struct {
	md      *markdown.Model
	opts    MarkdownOptions
	id      string
	scope   string
	mode    string
	focusID string
	path    string
	back    []MarkdownPage
}

// NewMarkdownModal wraps md in a MarkdownModal using opts.
func NewMarkdownModal(md *markdown.Model, opts MarkdownOptions) *MarkdownModal {
	if md == nil {
		md = markdown.New("", "")
	}
	id := opts.ID
	if id == "" {
		id = "reader"
	}
	scope := opts.Scope
	if scope == "" {
		scope = "reader"
	}
	mode := opts.Mode
	if mode == "" {
		mode = ModeReader
	}
	focusID := opts.FocusID
	if focusID == "" {
		focusID = id + ".body"
	}
	return &MarkdownModal{
		md:      md,
		opts:    opts,
		id:      id,
		scope:   scope,
		mode:    mode,
		focusID: focusID,
		path:    opts.Path,
	}
}

// Model returns the underlying markdown reader model.
func (m *MarkdownModal) Model() *markdown.Model { return m.md }

// ID returns the modal identifier.
func (m *MarkdownModal) ID() string { return m.id }

// Mode returns the modal input mode.
func (m *MarkdownModal) Mode() string { return m.mode }

// Scope returns the modal overlay scope.
func (m *MarkdownModal) Scope() string { return m.scope }

// FocusID returns the focused ViewNode ID.
func (m *MarkdownModal) FocusID() string { return m.focusID }

// Path returns the current document path.
func (m *MarkdownModal) Path() string { return m.path }

// BackLen returns the depth of the back-navigation stack.
func (m *MarkdownModal) BackLen() int { return len(m.back) }

// Navigate pushes the current page onto the back stack and loads a new document.
func (m *MarkdownModal) Navigate(title, source, path string) {
	m.back = append(m.back, MarkdownPage{
		Title:  m.md.Title,
		Source: m.md.Source,
		Path:   m.path,
		Offset: m.md.Offset,
	})
	m.md.Title = title
	m.path = path
	m.md.SetSource(source)
	m.md.Offset = 0
	m.md.ActiveLinkIdx = 0
}

// Back restores the previous page from the back stack, reporting whether one existed.
func (m *MarkdownModal) Back() bool {
	if len(m.back) == 0 {
		return false
	}
	last := m.back[len(m.back)-1]
	m.back = m.back[:len(m.back)-1]
	m.md.Title = last.Title
	m.path = last.Path
	m.md.SetSource(last.Source)
	m.md.Offset = last.Offset
	return true
}

// View adjusts PageSize to fit h and renders the reader inside an Overlay.
func (m *MarkdownModal) View(w, h int) ui.ViewNode {
	wp := m.opts.WidthPct
	if wp <= 0 {
		wp = 92
	}
	hp := m.opts.HeightPct
	if hp <= 0 {
		hp = 90
	}
	if h > 12 {
		if ps := h*hp/100 - 6; ps > 0 {
			m.md.PageSize = ps
		}
	}

	body := m.md.View(m.focusID)
	var badges []ui.Badge
	if m.opts.ShowStatsBadges {
		badges = append(badges,
			ui.Badge{Text: fmt.Sprintf("%d headings", len(m.md.Headings))},
			ui.Badge{Text: fmt.Sprintf("%d links", len(m.md.Links))},
			ui.Badge{Text: fmt.Sprintf("%d code", len(m.md.CodeBlocks))},
		)
		if m.path != "" {
			badges = append(badges, ui.Badge{Text: m.path, Tone: "muted"})
		}
		if n := len(m.back); n > 0 {
			badges = append(badges, ui.Badge{Text: fmt.Sprintf("%d back", n), Tone: "accent"})
		}
	}
	if m.opts.Badges != nil {
		badges = append(badges, m.opts.Badges(m.md, m.path, len(m.back))...)
	}

	facts := map[string]any{
		"backLen": len(m.back),
	}
	if m.path != "" {
		facts["path"] = m.path
	}
	if m.opts.Facts != nil {
		maps.Copy(facts, m.opts.Facts(m.md, m.path, len(m.back)))
	}

	title := m.md.Title
	if title == "" {
		title = m.id
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

// Handle dispatches a command to the markdown reader modal.
func (m *MarkdownModal) Handle(cmd core.Command) Result {
	if m.opts.OnCommand != nil {
		if res, ok := m.opts.OnCommand(m.md, cmd); ok {
			return res
		}
	}
	if cmd.ID == CmdCancel {
		return Result{Handled: true, Close: true, CloseReason: CloseCancel}
	}
	action, handled := m.md.HandleCommand(cmd)
	if !handled {
		return Result{}
	}
	switch action.Kind {
	case markdown.ActionOpen:
		if m.opts.OnOpenLink != nil {
			res := m.opts.OnOpenLink(action.Target)
			res.Handled = true
			return res
		}
		return Result{Handled: true}
	case markdown.ActionYank:
		if m.opts.OnYankCode != nil {
			res := m.opts.OnYankCode(action.Text)
			res.Handled = true
			return res
		}
		return Result{Handled: true}
	default:
		switch cmd.ID {
		case markdown.CmdLinkOpen:
			return Result{Handled: true, Status: "no link here to open", SetStatus: true}
		case markdown.CmdCodeYank:
			return Result{Handled: true, Status: "no code block in this document", SetStatus: true}
		}
	}
	return Result{Handled: true}
}

// OnClose notifies the configured close hook.
func (m *MarkdownModal) OnClose(reason CloseReason) {
	if m.opts.OnClose != nil {
		m.opts.OnClose(reason)
	}
}
