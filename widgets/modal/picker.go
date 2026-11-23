package modal

import (
	"fmt"
	"slices"
	"strings"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/picker"
)

// PickerCreateItemID is the synthetic item ID used for the dynamic
// "+ Create new" row appended by PickerCreateHook.
const PickerCreateItemID = "__modal_create_new__"

// PickerCreateHook enables a dynamic "+ Create new" row whenever the picker's
// query is non-empty and does not exactly match any base item's title.
type PickerCreateHook struct {
	Kind     string                         // optional domain kind (keyword + fallback label)
	Label    string                         // display noun label, e.g. "Project"
	Subtitle string                         // optional row subtitle
	Item     func(query string) picker.Item // optional custom row builder
	OnCreate func(query string) Result      // invoked when the create row is confirmed
}

// PickerOptions configures a PickerModal.
type PickerOptions struct {
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
	PageSize      int
	Badges        func(pm *picker.Model) []ui.Badge
	Facts         func(pm *picker.Model) map[string]any
	Ref           *core.Ref
	Create        *PickerCreateHook
	OnCommand     func(pm *picker.Model, cmd core.Command) (Result, bool)
	OnConfirm     func(item picker.Item) Result
	OnCreate      func(query string) Result
	OnClose       func(reason CloseReason)
}

// PickerModal adapts picker.Model to the Modal interface.
type PickerModal struct {
	pm         *picker.Model
	opts       PickerOptions
	id         string
	scope      string
	normalMode string
	filterMode string
	focusID    string
	baseItems  []picker.Item
}

// NewPickerModal wraps pm in a PickerModal using opts.
func NewPickerModal(pm *picker.Model, opts PickerOptions) *PickerModal {
	if pm == nil {
		pm = picker.New(opts.Title, nil)
	}
	if opts.PageSize > 0 {
		pm.PageSize = opts.PageSize
	} else if pm.PageSize == 0 {
		pm.PageSize = picker.DefaultPageSize
	}
	pm.Filtering = opts.StartInFilter
	id := opts.ID
	if id == "" {
		id = "picker"
	}
	scope := opts.Scope
	if scope == "" {
		scope = id
	}
	normalMode := opts.Mode
	if normalMode == "" {
		normalMode = ModePicker
	}
	filterMode := opts.FilterMode
	if filterMode == "" {
		filterMode = ModePickerFilter
	}
	focusID := opts.FocusID
	if focusID == "" {
		focusID = id + ".list"
	}
	m := &PickerModal{
		pm:         pm,
		opts:       opts,
		id:         id,
		scope:      scope,
		normalMode: normalMode,
		filterMode: filterMode,
		focusID:    focusID,
	}
	if opts.Create != nil {
		m.baseItems = slices.Clone(pm.Items)
		m.syncCreateItem()
	}
	return m
}

// Model returns the underlying picker model.
func (m *PickerModal) Model() *picker.Model { return m.pm }

// ID returns the modal identifier.
func (m *PickerModal) ID() string { return m.id }

// Mode returns the current modal input mode.
func (m *PickerModal) Mode() string {
	if m.pm != nil && m.pm.Filtering {
		return m.filterMode
	}
	return m.normalMode
}

// SetMode updates the modal's input mode.
func (m *PickerModal) SetMode(mode string) {
	if mode == "" {
		mode = ModePicker
	}
	if mode == m.filterMode {
		if m.pm != nil {
			m.pm.Filtering = true
		}
		return
	}
	if m.pm != nil {
		m.pm.Filtering = false
	}
	m.normalMode = mode
}

// Scope returns the modal overlay scope.
func (m *PickerModal) Scope() string { return m.scope }

// FocusID returns the focused ViewNode ID inside the picker modal.
func (m *PickerModal) FocusID() string { return m.focusID }

// SetItems replaces the base items and re-syncs any dynamic create row.
func (m *PickerModal) SetItems(items []picker.Item) {
	if m.opts.Create != nil {
		m.baseItems = slices.Clone(items)
		m.syncCreateItem()
		return
	}
	m.pm.SetItems(items)
}

// View renders the picker inside an Overlay.
func (m *PickerModal) View(w, h int) ui.ViewNode {
	title := m.opts.Title
	if title == "" {
		title = m.pm.Title
	}
	if title == "" {
		title = m.id
	}
	wp := m.opts.WidthPct
	if wp <= 0 {
		wp = 64
	}
	hp := m.opts.HeightPct
	if hp <= 0 {
		hp = 70
	}
	var badges []ui.Badge
	if m.opts.Badges != nil {
		badges = m.opts.Badges(m.pm)
	}
	var facts map[string]any
	if m.opts.Facts != nil {
		facts = m.opts.Facts(m.pm)
	}
	body := m.pm.View(m.focusID)
	return Overlay(OverlaySpec{
		ID:        m.id,
		Scope:     m.scope,
		Title:     title,
		Tone:      m.opts.Tone,
		WidthPct:  wp,
		HeightPct: hp,
		Badges:    badges,
		Ref:       m.opts.Ref,
		Facts:     facts,
		Children:  []ui.ViewNode{body},
	})
}

// Handle dispatches a command to the picker modal.
func (m *PickerModal) Handle(cmd core.Command) Result {
	if m.opts.OnCommand != nil {
		if res, ok := m.opts.OnCommand(m.pm, cmd); ok {
			return res
		}
	}
	switch cmd.ID {
	case CmdCancel:
		return Result{Handled: true, Close: true, CloseReason: CloseCancel}
	case CmdPickerFilterStart:
		m.pm.Filtering = true
		return Result{Handled: true, Mode: m.Mode()}
	case CmdPickerFilterStop:
		m.pm.Filtering = false
		return Result{Handled: true, Mode: m.Mode()}
	case CmdPickerFilterClear:
		m.pm.SetQuery("")
		m.syncCreateItem()
		m.pm.Filtering = false
		return Result{Handled: true, Mode: m.Mode()}
	case CmdPickerInsert, input.CmdInsert:
		m.pm.AppendQuery(cmd.Arg("text"))
		m.syncCreateItem()
		return Result{Handled: true, Mode: m.Mode()}
	case CmdPickerBackspace, input.CmdBackspace:
		m.pm.BackspaceQuery()
		m.syncCreateItem()
		return Result{Handled: true, Mode: m.Mode()}
	case "motion.scroll.down":
		cmd.ID = "motion.down"
	case "motion.scroll.up":
		cmd.ID = "motion.up"
	}

	picked, handled := m.pm.HandleCommand(cmd)
	if !handled {
		return Result{}
	}
	if picked == nil {
		if input.IsReadlineCommand(cmd.ID) {
			m.syncCreateItem()
		}
		return Result{Handled: true, Mode: m.Mode()}
	}
	if picked.ID == PickerCreateItemID {
		q := strings.TrimSpace(m.pm.Query)
		if s, ok := picked.Data.(string); ok && s != "" {
			q = s
		}
		switch {
		case m.opts.OnCreate != nil:
			res := m.opts.OnCreate(q)
			res.Handled = true
			if !res.Close && res.Push == nil && res.Mode == "" {
				res.Mode = m.Mode()
			}
			return res
		case m.opts.Create != nil && m.opts.Create.OnCreate != nil:
			res := m.opts.Create.OnCreate(q)
			res.Handled = true
			if !res.Close && res.Push == nil && res.Mode == "" {
				res.Mode = m.Mode()
			}
			return res
		default:
			return Result{Handled: true, Close: true, CloseReason: CloseSubmit}
		}
	}
	if m.opts.OnConfirm != nil {
		res := m.opts.OnConfirm(*picked)
		res.Handled = true
		if !res.Close && res.Push == nil && res.Mode == "" {
			res.Mode = m.Mode()
		}
		return res
	}
	return Result{Handled: true, Close: true, CloseReason: CloseSubmit}
}

// OnClose notifies the configured close hook.
func (m *PickerModal) OnClose(reason CloseReason) {
	if m.opts.OnClose != nil {
		m.opts.OnClose(reason)
	}
}

func (m *PickerModal) syncCreateItem() {
	hook := m.opts.Create
	if hook == nil || m.pm == nil {
		return
	}
	q := strings.TrimSpace(m.pm.Query)
	items := slices.Clone(m.baseItems)
	if q != "" && !hasExactTitle(m.baseItems, q) {
		var row picker.Item
		if hook.Item != nil {
			row = hook.Item(q)
			if row.ID == "" {
				row.ID = PickerCreateItemID
			}
		} else {
			label := hook.Label
			if label == "" {
				label = hook.Kind
			}
			title := fmt.Sprintf("+ Create new: %q", q)
			sub := hook.Subtitle
			if label != "" {
				title = fmt.Sprintf("+ Create new %s: %q", label, q)
				if sub == "" {
					sub = "create " + strings.ToLower(label) + " and select it"
				}
			} else if sub == "" {
				sub = "create and select it"
			}
			keywords := []string{q, "create", "new"}
			if label != "" {
				keywords = append(keywords, label)
			}
			if hook.Kind != "" && hook.Kind != label {
				keywords = append(keywords, hook.Kind)
			}
			row = picker.Item{
				ID:       PickerCreateItemID,
				Title:    title,
				Subtitle: sub,
				Badges:   []ui.Badge{{Text: "new", Tone: "accent"}},
				Keywords: keywords,
				Data:     q,
			}
		}
		items = append(items, row)
	}
	m.pm.SetItems(items)
}

func hasExactTitle(items []picker.Item, q string) bool {
	for _, it := range items {
		if strings.EqualFold(strings.TrimSpace(it.Title), q) {
			return true
		}
	}
	return false
}
